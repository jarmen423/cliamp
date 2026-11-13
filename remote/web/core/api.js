// The remote: one Connection, the store it feeds, and the operations the
// UI calls. No DOM.

import { Connection, RemoteError } from './conn.js'
import { createStore, livePosition } from './store.js'

export { RemoteError }

// The providers the app leads with. Others the server has still show.
export const PREFERRED_PROVIDERS = ['spotify', 'yt']

const POSITION_REFRESH_MS = 5000

export function createRemote({ base, token, WebSocketImpl, fetchImpl }) {
  const store = createStore({
    status: 'closed',
    snapshot: null,
    anchor: { position: 0, at: 0 },
    caps: null, // Set of operation names, or null before the first answer
    providers: [],
    epoch: 0, // counts connections; views that list server state reload on change
  })

  const conn = new Connection({
    base,
    token,
    WebSocketImpl,
    fetchImpl,
    onStatus(status) {
      store.set({ status })
      if (status === 'open') onOpen()
    },
    onEvent(event, data, envelope) {
      if (event !== 'runtime.state') return
      // A retained event can be old, and position ticks publish nothing, so
      // ask for a fresh position instead of trusting it.
      applySnapshot(data, { trustPosition: !envelope.retained })
      if (envelope.retained) refresh()
    },
  })

  // A seek from the TUI or another phone changes no runtime state that
  // cliamp publishes, so reread the position while the app is in view.
  let positionTimer = 0

  async function onOpen() {
    // A restarted cliamp counts revisions from 1 again. Forget the old
    // revision, or applySnapshot would drop every snapshot as stale.
    store.set((s) => ({ epoch: s.epoch + 1, snapshot: s.snapshot && { ...s.snapshot, revision: -1 } }))
    refresh()
    try {
      const caps = await conn.request('capabilities')
      store.set({ caps: new Set((caps.result || []).map((op) => op.name)) })
    } catch {}
    try {
      const { result } = await conn.submit('provider.list')
      store.set({ providers: sortProviders(result.providers || []) })
    } catch {}
  }

  function applySnapshot(snap, { trustPosition = true } = {}) {
    if (!snap) return
    const cur = store.get().snapshot
    if (cur && snap.revision < cur.revision) return
    store.set((s) => ({
      snapshot: snap,
      anchor: trustPosition || !cur ? { position: snap.position || 0, at: Date.now() } : s.anchor,
    }))
  }

  async function refresh() {
    try {
      const res = await conn.request('state.get')
      applySnapshot(res.snapshot)
    } catch {}
  }

  // optimistic changes the local snapshot at once and runs op. On failure
  // it resyncs from the server and rethrows.
  async function optimistic(patch, op) {
    const before = store.get()
    if (patch && before.snapshot) {
      const position = livePosition(before)
      store.set({ snapshot: { ...before.snapshot, ...patch }, anchor: { position: patch.position ?? position, at: Date.now() } })
    }
    try {
      const out = await op()
      applySnapshot(out?.snapshot)
      return out
    } catch (err) {
      refresh()
      throw err
    }
  }

  // Provider work goes over the network to Spotify or YouTube and can be
  // slow; give it longer before cancelling it.
  const submit = (op, params) => conn.submit(op, params, op.startsWith('provider.') ? { timeout: 90000 } : undefined)
  const snap = () => store.get().snapshot
  const ifRevision = (rev) => ({ if_revision: rev ?? snap()?.playlist_revision })

  return {
    store,
    conn,
    has: (op) => store.get().caps?.has(op) ?? true,
    start() {
      clearInterval(positionTimer)
      positionTimer = setInterval(() => {
        if (store.get().status === 'open' && globalThis.document?.visibilityState !== 'hidden') refresh()
      }, POSITION_REFRESH_MS)
      conn.start()
    },
    stop() {
      clearInterval(positionTimer)
      conn.stop()
    },
    refresh,
    spectrum: () => conn.request('spectrum.get').then((r) => r.result?.bands || []),

    toggle() {
      const playing = snap()?.state === 'playing'
      return optimistic({ state: playing ? 'paused' : 'playing' }, () => submit('toggle'))
    },
    next: () => optimistic(null, () => submit('next')),
    prev: () => optimistic(null, () => submit('prev')),
    seek: (position) => optimistic({ position }, () => submit('seek.absolute', { value: position })),
    volume: (db) => optimistic({ volume: db }, () => submit('volume', { value: db })),
    // systemVolume and systemMute drive the output volume of the computer
    // cliamp plays on: a percent from 0 to 100, and mute on or off.
    systemVolume: (percent) => optimistic({ system_volume: percent }, () => submit('system.volume', { value: percent })),
    systemMute: (muted) => optimistic({ system_muted: muted }, () => submit('system.mute', { name: muted ? 'on' : 'off' })),
    // setShuffle takes 'off', 'on' (plain shuffle) or 'smart' (Smart
    // Shuffle, which mixes in recommendations and implies shuffle).
    setShuffle: (mode) =>
      optimistic({ shuffle: mode !== 'off', smart_shuffle: mode === 'smart' }, () => submit('shuffle', { name: mode })),
    repeat: () => optimistic(null, () => submit('repeat', { name: 'cycle' })),

    // search returns every section the provider matched: songs, artists,
    // albums and playlists. Providers without multi-type search fill only
    // tracks.
    search: (provider, query, limit = 25) =>
      submit('provider.search', { provider, query, limit }).then((r) => ({
        tracks: r.result.tracks || [],
        artists: r.result.artists || [],
        albums: r.result.albums || [],
        playlists: r.result.playlists || [],
        total: r.result.total || 0,
      })),
    // searchMore extends a search's songs from offset. It repeats the
    // query, because the server keeps no search session.
    searchMore: (provider, query, offset, limit = 25) =>
      submit('provider.search', { provider, query, offset, limit }).then((r) => r.result.tracks || []),
    // openLink resolves a pasted or shared provider link: a playlist,
    // album or artist to open by id, or a track to play.
    openLink: (provider, text) => submit('provider.open_link', { provider, path: text }).then((r) => r.result.link),
    account: (provider) => submit('provider.account', { provider }).then((r) => r.result.account || null),
    // switchAccount waits out the browser sign-in on the cliamp machine,
    // which can take minutes, so it gets a budget of its own.
    switchAccount: (provider) =>
      conn.submit('provider.switch_account', { provider }, { timeout: 330000 }).then((r) => r.result.account || null),
    followArtist: (provider, artist, follow) => submit('provider.follow_artist', { provider, artist, follow }),
    followPlaylist: (provider, playlist, follow) => submit('provider.follow_playlist', { provider, playlist, follow }),
    artists: (provider, offset = 0, limit = 200) =>
      submit('provider.artists', { provider, offset, limit }).then((r) => ({ artists: r.result.artists || [], total: r.result.total || 0 })),
    artistAlbums: (provider, artist, offset = 0, limit = 200) =>
      submit('provider.artist_albums', { provider, artist, offset, limit }).then((r) => ({ albums: r.result.albums || [], total: r.result.total || 0 })),
    albums: (provider, sort, offset = 0, limit = 100) =>
      submit('provider.albums', { provider, sort, offset, limit }).then((r) => ({ albums: r.result.albums || [], sorts: r.result.sorts || [] })),
    // playlists walks every page: a library can run past the server's
    // 200-row page, and its later sections (saved albums, podcasts)
    // never reach the app otherwise. The list is cached server-side.
    playlists: async (provider) => {
      const all = []
      for (let offset = 0; ; offset += 200) {
        const r = await submit('provider.playlists', { provider, offset, limit: 200 })
        const page = r.result.playlists || []
        all.push(...page)
        if (!page.length || all.length >= (r.result.total || 0)) return all
      }
    },
    tracks: (provider, playlist, sort) =>
      submit('provider.tracks', { provider, playlist, sort: sort || undefined, limit: 200 }).then((r) => ({
        tracks: r.result.tracks || [],
        sorts: r.result.sorts || [],
        total: r.result.total || 0,
      })),
    loadPlaylist: (provider, playlist, sort) =>
      optimistic(null, () => submit('provider.load', { provider, playlist, sort: sort || undefined })),
    albumTracks: (provider, album) =>
      submit('provider.album_tracks', { provider, album, limit: 200 }).then((r) => r.result.tracks || []),
    loadAlbum: (provider, album) => optimistic(null, () => submit('provider.load_album', { provider, album })),
    // artist reads a profile by artist id, or the artist of a track.
    artist: (provider, { artist, track } = {}) =>
      submit('provider.artist', { provider, artist, track }).then((r) => ({
        info: r.result.artists?.[0] || null,
        popular: r.result.tracks || [],
        albums: r.result.albums || [],
      })),
    loadArtist: (provider, artist) => optimistic(null, () => submit('provider.load_artist', { provider, artist })),
    playTrack: (track) => optimistic(null, () => submit('track.play', { track })),
    queueTrack: (track) => optimistic(null, () => submit('track.queue', { track })),

    // The lists carry the playlist revision they were read at. Index edits
    // send it back, so an edit of a list that changed since fails with
    // conflict instead of hitting another track.
    queueList: (offset = 0, limit = 200) =>
      submit('queue.list', { offset, limit }).then((r) => ({
        tracks: r.result.tracks || [],
        total: r.result.total || 0,
        rev: r.snapshot?.playlist_revision,
      })),
    playNextList: () =>
      submit('playnext.list', {}).then((r) => ({ tracks: r.result.tracks || [], rev: r.snapshot?.playlist_revision })),
    queuePlay: (index, rev) => optimistic(null, () => submit('queue.play', { index, ...ifRevision(rev) })),
    queueRemove: (index, rev) => optimistic(null, () => submit('queue.remove', { index, ...ifRevision(rev) })),
    playNextRemove: (index, rev) => optimistic(null, () => submit('playnext.remove', { index, ...ifRevision(rev) })),
  }
}

function sortProviders(list) {
  const rank = (p) => {
    const i = PREFERRED_PROVIDERS.indexOf(p.key)
    return i < 0 ? PREFERRED_PROVIDERS.length : i
  }
  return [...list].sort((a, b) => rank(a) - rank(b))
}
