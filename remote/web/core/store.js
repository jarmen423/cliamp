// A tiny observable store for the remote state. No DOM.

export function createStore(initial) {
  let state = initial
  const subs = new Set()
  return {
    get: () => state,
    set(patch) {
      state = { ...state, ...(typeof patch === 'function' ? patch(state) : patch) }
      for (const fn of subs) fn(state)
    },
    subscribe(fn) {
      subs.add(fn)
      return () => subs.delete(fn)
    },
  }
}

// livePosition returns the playback position now. The server sends no event
// for position ticks, so the client counts forward from the last snapshot.
export function livePosition(state, now = Date.now()) {
  const snap = state.snapshot
  if (!snap) return 0
  let pos = state.anchor.position
  if (snap.state === 'playing') pos += ((now - state.anchor.at) / 1000) * (snap.speed || 1)
  return snap.duration > 0 ? Math.min(Math.max(pos, 0), snap.duration) : Math.max(pos, 0)
}

// formatTime renders seconds as m:ss or h:mm:ss.
export function formatTime(secs) {
  if (!Number.isFinite(secs) || secs < 0) secs = 0
  secs = Math.floor(secs)
  const h = Math.floor(secs / 3600)
  const m = Math.floor((secs % 3600) / 60)
  const s = String(secs % 60).padStart(2, '0')
  return h > 0 ? `${h}:${String(m).padStart(2, '0')}:${s}` : `${m}:${s}`
}

// trackTitle and trackArtist pick what a row shows. A radio stream names
// the song in stream_title.
export function trackTitle(t) {
  if (!t) return ''
  return t.stream_title || t.title || basename(t.path) || 'Untitled'
}

export function trackArtist(t) {
  if (!t) return ''
  if (t.stream_title) return t.station || t.title || ''
  return t.artist || t.station || ''
}

function basename(path) {
  if (!path) return ''
  const clean = path.split(/[?#]/)[0]
  return decodeURIComponent(clean.slice(clean.lastIndexOf('/') + 1))
}

// artSource tells how to load the album art of a track: a direct URL, or
// the name of a file in the gateway art cache, which needs the token.
export function artSource(t) {
  const url = t?.album_art_url
  if (!url) return null
  if (/^https?:\/\//i.test(url)) return { url }
  const m = /\/album-art\/([0-9a-f]{64}\.[a-z]+)$/i.exec(url)
  return m ? { cached: m[1] } : null
}
