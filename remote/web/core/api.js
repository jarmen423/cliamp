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

  const submit = (op, params) => conn.submit(op, params)
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
    shuffle: () => optimistic({ shuffle: !snap()?.shuffle }, () => submit('shuffle', { name: 'toggle' })),
    repeat: () => optimistic(null, () => submit('repeat', { name: 'cycle' })),

    search: (provider, query, limit = 25) =>
      submit('provider.search', { provider, query, limit }).then((r) => r.result.tracks || []),
    playlists: (provider) => submit('provider.playlists', { provider }).then((r) => r.result.playlists || []),
    tracks: (provider, playlist) =>
      submit('provider.tracks', { provider, playlist, limit: 200 }).then((r) => r.result.tracks || []),
    loadPlaylist: (provider, playlist) => optimistic(null, () => submit('provider.load', { provider, playlist })),
    albumTracks: (provider, album) =>
      submit('provider.album_tracks', { provider, album, limit: 200 }).then((r) => r.result.tracks || []),
    loadAlbum: (provider, album) => optimistic(null, () => submit('provider.load_album', { provider, album })),
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
