// Search, Library and Queue, and the track row they share.
import { html, useEffect, useRef, useState } from '../vendor/preact-htm.js'
import { PREFERRED_PROVIDERS } from '../core/api.js'
import { formatTime, trackArtist, trackTitle } from '../core/store.js'
import { Art } from './art.js'
import { Icon } from './icons.js'
import { haptic, useLatest, useRemote } from './hooks.js'
import { toast } from './toast.js'

const failed = (what) => (err) => toast(`${what}: ${err.message}`, 'error')

function Skeleton({ rows = 6 }) {
  return html`<div class="skel-wrap" aria-hidden="true">
    ${Array.from({ length: rows }, () => html`<div class="skel"><i></i><div><i></i><i></i></div></div>`)}
  </div>`
}

// load runs fn when deps change and keeps the latest answer only.
function useLoad(fn, deps) {
  const [st, setSt] = useState({ loading: true, data: null, error: null })
  const seq = useRef(0)
  const run = () => {
    const n = ++seq.current
    setSt((s) => ({ ...s, loading: true, error: null }))
    fn().then(
      (data) => n === seq.current && setSt({ loading: false, data, error: null }),
      (error) => n === seq.current && setSt({ loading: false, data: null, error }),
    )
  }
  useEffect(run, deps)
  return { ...st, reload: run }
}

function PlayNextButton({ onQueue }) {
  const [done, setDone] = useState(false)
  const click = () => {
    haptic()
    onQueue().then(() => {
      setDone(true)
      setTimeout(() => setDone(false), 1500)
    }, failed('Could not queue'))
  }
  return html`<button class="icon-btn small" aria-label=${done ? 'Queued to play next' : 'Play next'} onClick=${click}>
    ${done ? Icon.check() : Icon.playNext()}
  </button>`
}

export function TrackRow({ track, index, active, onPlay, onQueue, onRemove }) {
  const dur = track.duration_secs
  return html`<li class="row" data-active=${active ? '' : undefined}>
    <button class="row-main" onClick=${() => { haptic(); onPlay().catch(failed('Could not play')) }}
      aria-current=${active ? 'true' : undefined}>
      ${index !== undefined ? html`<span class="row-index mono">${index}</span>` : html`<${Art} track=${track} />`}
      <span class="row-text">
        <div class="row-title trunc">${trackTitle(track)}</div>
        <div class="row-sub"><span class="trunc">${trackArtist(track) || track.album || ''}</span>
          ${dur > 0 && html`<span class="mono">${formatTime(dur)}</span>`}</div>
      </span>
    </button>
    ${onQueue && html`<${PlayNextButton} onQueue=${onQueue} />`}
    ${onRemove && html`<button class="icon-btn small" aria-label=${'Remove ' + trackTitle(track)}
      onClick=${() => onRemove().catch(failed('Could not remove'))}>${Icon.close()}</button>`}
  </li>`
}

// mainProviders returns the providers the app leads with: Spotify and
// YouTube when cliamp has them, every other provider after them.
function mainProviders(providers, filter = () => true) {
  const list = providers.filter(filter)
  const preferred = list.filter((p) => PREFERRED_PROVIDERS.includes(p.key))
  return preferred.length ? [...preferred, ...list.filter((p) => !PREFERRED_PROVIDERS.includes(p.key))] : list
}

function Segments({ items, value, onChange, label }) {
  if (items.length < 2) return null
  return html`<div class="seg" role="group" aria-label=${label}>
    ${items.map((it) => html`<button aria-pressed=${it.key === value ? 'true' : 'false'} onClick=${() => onChange(it.key)}>${it.name}</button>`)}
  </div>`
}

// ---------- search ----------

const names = (ps) => {
  const n = ps.slice(0, 3).map((p) => p.name)
  return n.length > 1 ? n.slice(0, -1).join(', ') + ' and ' + n.at(-1) : n[0]
}

const RECENT_KEY = 'cliamp.recent'
const recent = () => {
  try {
    return JSON.parse(localStorage.getItem(RECENT_KEY)) || []
  } catch {
    return []
  }
}
const remember = (q) => localStorage.setItem(RECENT_KEY, JSON.stringify([q, ...recent().filter((r) => r !== q)].slice(0, 8)))

export function Search({ remote }) {
  const { providers } = useRemote(remote)
  const searchable = mainProviders(providers, (p) => p.searchable)
  const leading = searchable.filter((p) => PREFERRED_PROVIDERS.includes(p.key))
  const scopes = [...(leading.length > 1 ? [{ key: 'all', name: 'All' }] : []), ...searchable.slice(0, 4)]
  const [scope, setScope] = useState(null)
  const active = scope ?? scopes[0]?.key
  const [query, setQuery] = useState('')
  const [submitted, setSubmitted] = useState('')
  const [results, setResults] = useState(null) // [{provider, tracks, error}]
  const seq = useRef(0)
  const timer = useRef(0)
  const input = useRef(null)

  const targets = active === 'all' ? leading : searchable.filter((p) => p.key === active)
  // The debounce timer fires after later renders, so it reads the scope
  // through a ref rather than the closure of the keystroke.
  const latest = useLatest({ targets })
  const run = (q) => {
    q = q.trim()
    setSubmitted(q)
    const n = ++seq.current
    if (!q) return setResults(null)
    setResults((r) => (r && r.loading ? r : { loading: true, groups: r?.groups || [] }))
    Promise.all(
      latest.current.targets.map((p) => remote.search(p.key, q).then((tracks) => ({ provider: p, tracks }), (error) => ({ provider: p, tracks: [], error }))),
    ).then((groups) => n === seq.current && setResults({ loading: false, groups }))
  }
  useEffect(() => {
    if (submitted) run(submitted)
  }, [active])

  const onInput = (e) => {
    const q = e.currentTarget.value
    setQuery(q)
    clearTimeout(timer.current)
    timer.current = setTimeout(() => run(q), 350)
  }
  const onSubmit = (e) => {
    e.preventDefault()
    clearTimeout(timer.current)
    if (query.trim()) remember(query.trim())
    run(query)
    input.current?.blur()
  }
  const pick = (q) => {
    setQuery(q)
    run(q)
  }

  const play = (t) => {
    if (submitted) remember(submitted)
    return remote.playTrack(t)
  }

  return html`<div>
    <form class="search-form" role="search" onSubmit=${onSubmit}>
      <label class="field">${Icon.search()}<span class="sr">Search</span>
        <input ref=${input} type="search" enterkeyhint="search" autocomplete="off" autocorrect="off" spellcheck="false"
          placeholder=${leading.length > 1 ? 'Songs, artists, videos…' : 'Search…'} value=${query} onInput=${onInput} />
      </label>
      <${Segments} items=${scopes} value=${active} onChange=${setScope} label="Search in" />
    </form>
    ${!results && html`<div>
      ${recent().length > 0 && html`<div class="section-head"><span class="eyebrow">Recent</span></div>
        <div class="chips">${recent().map((q) => html`<button class="chip" onClick=${() => pick(q)}>${q}</button>`)}</div>`}
      ${searchable.length > 0 && !recent().length && html`<div class="empty"><strong>Find something to play</strong>Search ${names(leading.length ? leading : searchable)}.</div>`}
      ${!searchable.length && html`<div class="empty"><strong>No searchable providers</strong>Set up Spotify or YouTube in cliamp's config.toml.</div>`}
    </div>`}
    ${results?.loading && !results.groups.length && html`<${Skeleton} />`}
    ${results?.groups.map((g) => html`<section aria-label=${g.provider.name}>
      ${targets.length > 1 && html`<div class="section-head"><span class="eyebrow">${g.provider.name}</span></div>`}
      ${g.error && html`<p class="error-line">${g.error.message}</p>`}
      ${!g.error && !g.tracks.length && !results.loading && html`<p class="dim" style="padding:12px 0">No results for “${submitted}”.</p>`}
      <ul class="rows">${g.tracks.map((t) => html`<${TrackRow} track=${t} onPlay=${() => play(t)} onQueue=${() => remote.queueTrack(t)} />`)}</ul>
    </section>`)}
  </div>`
}

// ---------- library ----------

const playlistCache = new Map()

function Playlist({ remote, provider, list, back }) {
  const { data: tracks, loading, error } = useLoad(() => remote.tracks(provider.key, list.id), [provider.key, list.id])
  const [starting, setStarting] = useState(false)
  const playAt = async (i) => {
    setStarting(true)
    try {
      await remote.loadPlaylist(provider.key, list.id)
      if (i > 0) await remote.queuePlay(i)
    } finally {
      setStarting(false)
    }
  }
  return html`<div>
    <button class="back" onClick=${back}>${Icon.back()} ${provider.name}</button>
    <div class="pl-head">
      <div style="min-width:0">
        <h1 class="title">${list.name}</h1>
        <div class="dim mono" style="font-size:12px">${tracks ? tracks.length : list.track_count || ''} ${tracks || list.track_count ? 'tracks' : ''}</div>
      </div>
      <button class="pill" disabled=${starting || !tracks?.length} onClick=${() => { haptic(); playAt(0).catch(failed('Could not play')) }}>${Icon.play()} Play</button>
    </div>
    ${loading && html`<${Skeleton} />`}
    ${error && html`<p class="error-line">${error.message}</p>`}
    ${tracks && !tracks.length && html`<div class="empty"><strong>Empty playlist</strong></div>`}
    <ul class="rows">${(tracks || []).map((t, i) => html`<${TrackRow} track=${t} onPlay=${() => playAt(i)} onQueue=${() => remote.queueTrack(t)} />`)}</ul>
  </div>`
}

export function Library({ remote }) {
  const { providers } = useRemote(remote)
  const list = mainProviders(providers).slice(0, 5)
  const [key, setKey] = useState(null)
  const [open, setOpen] = useState(null)
  const provider = list.find((p) => p.key === (key ?? list[0]?.key))
  const { data, loading, error, reload } = useLoad(async () => {
    if (!provider) return []
    if (!playlistCache.has(provider.key)) playlistCache.set(provider.key, await remote.playlists(provider.key))
    return playlistCache.get(provider.key)
  }, [provider?.key])

  if (open && provider) return html`<${Playlist} remote=${remote} provider=${provider} list=${open} back=${() => setOpen(null)} />`
  return html`<div>
    <h1 class="title">Library</h1>
    <${Segments} items=${list} value=${provider?.key} onChange=${(k) => { setKey(k); setOpen(null) }} label="Provider" />
    ${!provider && html`<div class="empty"><strong>No providers yet</strong>cliamp has not reported its providers.</div>`}
    ${loading && html`<${Skeleton} />`}
    ${error && html`<p class="error-line">${error.message} <button class="chip" onClick=${() => { playlistCache.delete(provider.key); reload() }}>Retry</button></p>`}
    ${data && !data.length && !loading && html`<div class="empty"><strong>No playlists</strong>${provider?.name} has no playlists for this account.</div>`}
    <ul class="rows" style="margin-top:8px">
      ${(data || []).map((pl) => html`<li class="row">
        <button class="row-main" onClick=${() => setOpen(pl)}>
          <span class="row-text"><div class="row-title trunc">${pl.name}</div>
            ${pl.section && html`<div class="row-sub"><span class="trunc">${pl.section}</span></div>`}</span>
          ${pl.track_count > 0 && html`<span class="row-count mono">${pl.track_count}</span>`}
          <span class="row-chev">${Icon.chevron()}</span>
        </button>
      </li>`)}
    </ul>
  </div>`
}

// ---------- queue ----------

export function Queue({ remote }) {
  const { snapshot } = useRemote(remote)
  const rev = snapshot?.playlist_revision
  const { data, loading, error } = useLoad(
    () => Promise.all([remote.playNextList(), remote.queueList()]).then(([next, q]) => ({ next, q })),
    [rev],
  )
  const current = snapshot?.index ?? 0
  const hasNext = data?.next.length > 0
  return html`<div>
    <div class="pl-head"><h1 class="title">Queue</h1>
      ${data && html`<span class="dim mono" style="font-size:12px;padding-bottom:6px">${data.q.total} ${data.q.total === 1 ? 'track' : 'tracks'}</span>`}</div>
    ${loading && !data && html`<${Skeleton} />`}
    ${error && html`<p class="error-line">${error.message}</p>`}
    ${hasNext && html`<section aria-label="Up next">
      <div class="section-head"><span class="eyebrow">Up next</span></div>
      <ul class="rows">${data.next.map((t, i) => html`<${TrackRow} track=${t}
        onPlay=${() => remote.playTrack(t)} onRemove=${() => remote.playNextRemove(i)} />`)}</ul>
    </section>`}
    ${data && html`<section aria-label="Playlist">
      ${hasNext && html`<div class="section-head"><span class="eyebrow">Then</span></div>`}
      ${!data.q.tracks.length && html`<div class="empty"><strong>The queue is empty</strong>Play something from Search or Library.</div>`}
      <ul class="rows">${data.q.tracks.map((t, i) => html`<${TrackRow} track=${t} index=${i + 1} active=${i === current}
        onPlay=${() => remote.queuePlay(i)} onRemove=${() => remote.queueRemove(i)} />`)}</ul>
      ${data.q.total > data.q.tracks.length && html`<p class="dim" style="font-size:13px;padding:12px 0">Showing the first ${data.q.tracks.length} of ${data.q.total}.</p>`}
    </section>`}
  </div>`
}
