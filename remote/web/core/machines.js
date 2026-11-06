// The cliamp machines the app is paired with, and moving playback between
// them. No DOM: storage and fetch come from the caller, so a native app can
// pass its own.
//
// A machine is { base, token, name }. base is the http(s) origin of its
// gateway and also its id.

import { livePosition } from './store.js'

const MACHINES_KEY = 'cliamp.machines'
const ACTIVE_KEY = 'cliamp.active'
const LEGACY_PAIR_KEY = 'cliamp.pair' // one machine, before the list
const TOKEN_RE = /^[0-9a-f]{64}$/i

const origin = (u) => {
  try {
    const url = new URL(u)
    return /^https?:$/.test(url.protocol) ? url.origin : ''
  } catch {
    return ''
  }
}

// parsePairing reads a pairing link. It has two forms:
//   <base>/#t=<token>              pairs the gateway at base
//   <app>/#m=<base>&t=<token>      adds the gateway at base to the app
// It returns { base, token } or null.
export function parsePairing(link, appOrigin) {
  const hashAt = link.indexOf('#')
  if (hashAt < 0) return null
  const params = new URLSearchParams(link.slice(hashAt + 1))
  const token = params.get('t') || ''
  if (!TOKEN_RE.test(token)) return null
  const m = params.get('m')
  const base = origin(m || (hashAt > 0 ? link.slice(0, hashAt) : appOrigin))
  return base ? { base, token } : null
}

export function loadMachines(storage) {
  let list = []
  try {
    list = JSON.parse(storage.getItem(MACHINES_KEY)) || []
  } catch {}
  if (!list.length) {
    try {
      const old = JSON.parse(storage.getItem(LEGACY_PAIR_KEY))
      if (old?.base && old?.token) list = [{ base: old.base, token: old.token, name: '' }]
    } catch {}
  }
  return list.filter((m) => m && origin(m.base) && TOKEN_RE.test(m.token))
}

export function saveMachines(storage, list) {
  storage.setItem(MACHINES_KEY, JSON.stringify(list))
  storage.removeItem(LEGACY_PAIR_KEY)
}

export const loadActive = (storage) => storage.getItem(ACTIVE_KEY) || ''
export const saveActive = (storage, base) => storage.setItem(ACTIVE_KEY, base)

// addMachine returns the list with the machine added, or its token updated
// when the list already has it.
export function addMachine(list, { base, token, name = '' }) {
  const i = list.findIndex((m) => m.base === base)
  if (i < 0) return [...list, { base, token, name }]
  const next = [...list]
  next[i] = { ...next[i], token, name: name || next[i].name }
  return next
}

// blockedByScheme reports whether the page cannot reach base: an https
// page may not open http or ws connections (mixed content).
export const blockedByScheme = (base, pageProtocol) => pageProtocol === 'https:' && base.startsWith('http:')

// probe asks a machine for its name. It resolves with { name, version },
// or { error } when the machine is off, unreachable or refuses the token.
export async function probe(machine, { fetchImpl = globalThis.fetch, timeout = 3000 } = {}) {
  const ctl = new AbortController()
  const timer = setTimeout(() => ctl.abort(), timeout)
  try {
    const res = await fetchImpl(machine.base + '/info', {
      headers: { Authorization: 'Bearer ' + machine.token },
      signal: ctl.signal,
    })
    if (res.status === 401) return { error: 'unauthorized' }
    if (!res.ok) return { error: 'offline' }
    return await res.json()
  } catch {
    return { error: 'offline' }
  } finally {
    clearTimeout(timer)
  }
}

// waitOpen resolves when remote connects, or rejects after timeout.
export function waitOpen(remote, timeout = 8000) {
  if (remote.store.get().status === 'open') return Promise.resolve()
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => {
      off()
      reject(new Error('The machine did not connect'))
    }, timeout)
    const off = remote.store.subscribe((s) => {
      if (s.status === 'open') {
        clearTimeout(timer)
        off()
        resolve()
      } else if (s.status === 'unauthorized') {
        clearTimeout(timer)
        off()
        reject(new Error('The machine refused this phone; pair it again'))
      }
    })
  })
}

// movePlayback plays on to what plays on from, at the same position, and
// then pauses from. Both remotes must be connected. A provider playlist or
// album moves as the whole list, so the queue continues on to; anything
// else moves as the one track. It throws, leaving from playing, when to
// cannot play it, such as a local file that only from has.
export async function movePlayback(from, to) {
  const state = from.store.get()
  const snap = state.snapshot
  if (!snap?.track) throw new Error('Nothing is playing')
  const position = livePosition(state)
  const index = snap.index || 0
  const providers = await to.conn
    .submit('provider.list')
    .then((r) => new Set((r.result.providers || []).map((p) => p.key)), () => new Set())

  // A provider track, such as spotify:track:…, needs that provider on to.
  // Without it, to would take the track and then fail to play it.
  const source = from.store.get().providers || []
  const kind = /^([\w-]+):/.exec(snap.track.path || '')?.[1]
  const needs = kind && source.find((p) => p.key === kind)
  if (needs && !providers.has(kind)) throw new Error(`${to.name || 'That machine'} has no ${needs.name}`)

  const album = /^([\w-]+):album:(.+)$/.exec(snap.playlist || '')
  const artist = /^([\w-]+):artist:(.+)$/.exec(snap.playlist || '')
  const list = !album && !artist && /^([\w-]+):(.+)$/.exec(snap.playlist || '')
  if (artist && providers.has(artist[1])) {
    await to.loadArtist(artist[1], artist[2])
    if (index > 0) await to.queuePlay(index)
  } else if (album && providers.has(album[1])) {
    await to.loadAlbum(album[1], album[2])
    if (index > 0) await to.queuePlay(index)
  } else if (list && providers.has(list[1])) {
    await to.loadPlaylist(list[1], list[2])
    if (index > 0) await to.queuePlay(index)
  } else {
    await to.playTrack(snap.track)
  }

  // Only a track that plays on to counts as moved; until then from keeps
  // playing. A seek sent while the track still opens is dropped, so the
  // seek waits for this too.
  const path = snap.track.path
  const started = await waitFor(to, (s) => s.snapshot?.state === 'playing' && s.snapshot?.track?.path === path, 20000)
  if (!started) {
    await to.conn.submit('stop').catch(() => {})
    throw new Error(`${to.name || 'That machine'} could not play this track`)
  }
  if (snap.seekable && position > 3) {
    // The music already plays on to, so a failed seek is not a failed move.
    for (let tries = 0; tries < 3; tries++) {
      await to.seek(position).catch(() => {})
      await new Promise((r) => setTimeout(r, 1000))
      await to.refresh()
      if (Math.abs(livePosition(to.store.get()) - position) < 8) break
    }
  }
  if (snap.state === 'playing') await from.toggle().catch(() => {})
}

// waitFor resolves true when pred holds for the store of remote, refreshing
// it each second, or false after timeout.
async function waitFor(remote, pred, timeout) {
  const end = Date.now() + timeout
  while (Date.now() < end) {
    if (pred(remote.store.get())) return true
    await new Promise((r) => setTimeout(r, 1000))
    await remote.refresh()
  }
  return false
}
