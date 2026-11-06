// The remote: one Connection, the store it feeds, and the operations the
// UI calls. No DOM.

import { Connection, RemoteError } from './conn.js'
import { createStore, livePosition } from './store.js'

export { RemoteError }

// The providers the app leads with. Others the server has still show.
export const PREFERRED_PROVIDERS = ['spotify', 'yt']

export function createRemote({ base, token, WebSocketImpl, fetchImpl }) {
  const store = createStore({
    status: 'closed',
    snapshot: null,
    anchor: { position: 0, at: 0 },
    caps: null, // Set of operation names, or null before the first answer
    providers: [],
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

  async function onOpen() {
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
  const ifRevision = () => ({ if_revision: snap()?.playlist_revision })

  return {
    store,
    conn,
    has: (op) => store.get().caps?.has(op) ?? true,
    start: () => conn.start(),
    stop: () => conn.stop(),
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
    playTrack: (track) => optimistic(null, () => submit('track.play', { track })),
    queueTrack: (track) => optimistic(null, () => submit('track.queue', { track })),

    queueList: (offset = 0, limit = 200) =>
      submit('queue.list', { offset, limit }).then((r) => ({ tracks: r.result.tracks || [], total: r.result.total || 0 })),
    playNextList: () => submit('playnext.list', {}).then((r) => r.result.tracks || []),
    queuePlay: (index) => optimistic(null, () => submit('queue.play', { index, ...ifRevision() })),
    queueRemove: (index) => optimistic(null, () => submit('queue.remove', { index, ...ifRevision() })),
    playNextRemove: (index) => optimistic(null, () => submit('playnext.remove', { index })),
  }
}

function sortProviders(list) {
  const rank = (p) => {
    const i = PREFERRED_PROVIDERS.indexOf(p.key)
    return i < 0 ? PREFERRED_PROVIDERS.length : i
  }
  return [...list].sort((a, b) => rank(a) - rank(b))
}
