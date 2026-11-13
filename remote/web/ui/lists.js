// Search, Library and Queue, and the track row they share.
import { html, useEffect, useRef, useState } from '../vendor/preact-htm.js'
import { PREFERRED_PROVIDERS } from '../core/api.js'
import { albumID, formatTime, isAlbum, trackArtist, trackTitle } from '../core/store.js'
import { Art } from './art.js'
import { Icon } from './icons.js'
import { haptic, Swipe, useLatest, useRemote } from './hooks.js'
import { machineLabel } from './machines.js'
import { toast } from './toast.js'

const failed = (what) => (err) => toast(`${what}: ${err.message}`, 'error')

export function Skeleton({ rows = 6 }) {
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

export function PlayNextButton({ onQueue }) {
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

export function TrackRow({ track, index, active, onPlay, onQueue, onRemove, onOpen }) {
  if (onOpen && isAlbum(track)) return html`<${AlbumRow} track=${track} onOpen=${onOpen} />`
  const dur = track.duration_secs
  // Swiping a row left queues it to play next, as the button does.
  const swipeQueue = onQueue && (() => onQueue().then(() => toast('Playing next'), failed('Could not queue')))
  const body = html`<button class="row-main" onClick=${() => { haptic(); onPlay().catch(failed('Could not play')) }}
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
      onClick=${() => onRemove().catch(failed('Could not remove'))}>${Icon.close()}</button>`}`
  return html`<li class="row" data-active=${active ? '' : undefined} data-swipe-queue=${onQueue ? '' : undefined}>
    ${onQueue && html`<span class="row-hint" aria-hidden="true">${Icon.playNext()}<span>Play next</span></span>`}
    <${Swipe} class="row-slide" onLeft=${swipeQueue}>${body}</${Swipe}>
  </li>`
}

function AlbumRow({ track, onOpen }) {
  return html`<li class="row">
    <button class="row-main" onClick=${onOpen}>
      <${Art} track=${track} />
      <span class="row-text">
        <div class="row-title trunc">${trackTitle(track)}</div>
        <div class="row-sub"><span class="mono">Album</span><span class="trunc">${[trackArtist(track), track.year].filter(Boolean).join(' · ')}</span></div>
      </span>
      <span class="row-chev">${Icon.chevron()}</span>
    </button>
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

// albumPlace turns an album result into the placeholder track the Album
// view opens: albumID reads the meta, trackTitle and Art the rest.
export function albumPlace(album) {
  return {
    title: album.name, artist: album.artist, year: album.year, album_art_url: album.image_url,
    provider_meta: { kind: 'album', albumID: album.id },
  }
}

export function ArtistRow({ artist, sub, onOpen }) {
  return html`<li class="row">
    <button class="row-main" onClick=${onOpen}>
      <${Art} track=${{ album_art_url: artist.image_url }} />
      <span class="row-text">
        <div class="row-title trunc">${artist.name}</div>
        <div class="row-sub"><span class="trunc">${sub || 'Artist'}</span></div>
      </span>
      <span class="row-chev">${Icon.chevron()}</span>
    </button>
  </li>`
}

export function Search({ remote, openArtist }) {
  const { providers } = useRemote(remote)
  const searchable = mainProviders(providers, (p) => p.searchable)
  const leading = searchable.filter((p) => PREFERRED_PROVIDERS.includes(p.key))
  const scopes = [...(leading.length > 1 ? [{ key: 'all', name: 'All' }] : []), ...searchable.slice(0, 4)]
  const [scope, setScope] = useState(null)
  const active = scope ?? scopes[0]?.key
  const [query, setQuery] = useState('')
  const [submitted, setSubmitted] = useState('')
  const [results, setResults] = useState(null) // [{provider, tracks, artists, albums, playlists, error, done, busy}]
  const [album, setAlbum] = useState(null) // {provider, track} while an album is open
  const [list, setList] = useState(null) // {provider, list} while a playlist is open
  const seq = useRef(0)
  const timer = useRef(0)
  const input = useRef(null)
  useEffect(() => () => clearTimeout(timer.current), [])

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
      latest.current.targets.map((p) =>
        remote.search(p.key, q).then(
          (r) => ({ provider: p, ...r, fetched: r.tracks.length, done: r.tracks.length < 10 }),
          (error) => ({ provider: p, tracks: [], artists: [], albums: [], playlists: [], error }),
        ),
      ),
    ).then((groups) => n === seq.current && setResults({ loading: false, groups }))
  }
  // more extends one group's songs. The server repeats the search, so
  // tracks already shown are dropped by path.
  const more = (g) => {
    setResults((r) => ({ ...r, groups: r.groups.map((x) => (x === g ? { ...x, busy: true } : x)) }))
    remote.searchMore(g.provider.key, submitted, g.fetched || 0).then(
      (page) =>
        setResults((r) => ({
          ...r,
          groups: r.groups.map((x) => {
            if (x.provider.key !== g.provider.key) return x
            const seen = new Set(x.tracks.map((t) => t.path))
            return { ...x, busy: false, fetched: (x.fetched || 0) + page.length, done: page.length < 25, tracks: [...x.tracks, ...page.filter((t) => !seen.has(t.path))] }
          }),
        })),
      failed('Could not load more'),
    )
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

  if (album) {
    return html`<${Album} remote=${remote} provider=${album.provider} track=${album.track}
      backLabel="Search" back=${() => setAlbum(null)} openArtist=${openArtist} />`
  }
  if (list) {
    return html`<${Playlist} remote=${remote} provider=${list.provider} list=${list.list} back=${() => setList(null)} />`
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
    ${results?.groups.map((g) => {
      const empty = !g.tracks.length && !g.artists.length && !g.albums.length && !g.playlists.length
      const sections = [g.artists.length, g.tracks.length, g.albums.length, g.playlists.length].filter(Boolean).length
      const head = (label) => sections > 1 && html`<div class="section-head"><span class="eyebrow">${label}</span></div>`
      return html`<section aria-label=${g.provider.name}>
      ${targets.length > 1 && html`<div class="section-head"><span class="eyebrow">${g.provider.name}</span></div>`}
      ${g.error && html`<p class="error-line">${g.error.message}</p>`}
      ${!g.error && empty && !results.loading && html`<p class="dim" style="padding:12px 0">No results for “${submitted}”.</p>`}
      ${g.artists.length > 0 && html`${head('Artists')}
        <ul class="rows">${g.artists.map((a) => html`<${ArtistRow} artist=${a}
          onOpen=${() => openArtist && g.provider.artist_profiles && openArtist(g.provider, { artist: a.id })} />`)}</ul>`}
      ${g.tracks.length > 0 && html`${head('Songs')}
        <ul class="rows">${g.tracks.map((t) => html`<${TrackRow} track=${t} onPlay=${() => play(t)} onQueue=${() => remote.queueTrack(t)}
          onOpen=${() => { if (submitted) remember(submitted); setAlbum({ provider: g.provider, track: t }) }} />`)}</ul>
        ${!g.done && html`<button class="chip more" disabled=${g.busy} onClick=${() => more(g)}>More songs</button>`}`}
      ${g.albums.length > 0 && html`${head('Albums')}
        <ul class="rows">${g.albums.map((a) => html`<${TrackRow} track=${albumPlace(a)} onPlay=${() => play(albumPlace(a))}
          onOpen=${() => { if (submitted) remember(submitted); setAlbum({ provider: g.provider, track: albumPlace(a) }) }} />`)}</ul>`}
      ${g.playlists.length > 0 && html`${head('Playlists')}
        <ul class="rows">${g.playlists.map((pl) => html`<${PlaylistRow} list=${pl} onOpen=${() => setList({ provider: g.provider, list: pl })} />`)}</ul>`}
    </section>`
    })}
  </div>`
}

export function PlaylistRow({ list, onOpen }) {
  return html`<li class="row">
    <button class="row-main" onClick=${onOpen}>
      <${Art} track=${{ album_art_url: list.image_url }} />
      <span class="row-text"><div class="row-title trunc">${list.name}</div>
        ${list.section && html`<div class="row-sub"><span class="trunc">${list.section}</span></div>`}</span>
      ${list.track_count > 0 && html`<span class="row-count mono">${list.track_count}</span>`}
      <span class="row-chev">${Icon.chevron()}</span>
    </button>
  </li>`
}

// ---------- library ----------

const playlistCache = new Map()
const SYNTHETIC_PLAYLIST_IDS = new Set(['YOUR MUSIC', 'TOP TRACKS', 'RECENTLY PLAYED'])
const isSyntheticPlaylist = (id) => !id || SYNTHETIC_PLAYLIST_IDS.has(id) || id.startsWith('spotify:album:')

// Collection shows the tracks of a playlist or an album. load returns
// either a track array or { tracks, total }; start(i) loads the list
// into cliamp and plays from track i.
function Collection({ remote, title, backLabel, count, numbered, load, deps, start, back, onArtist, actions, sortBar }) {
  const { data, loading, error } = useLoad(load, deps || [title])
  const [starting, setStarting] = useState(false)
  const tracks = Array.isArray(data) ? data : data?.tracks
  const playAt = async (i) => {
    setStarting(true)
    try {
      await start(i)
    } finally {
      setStarting(false)
    }
  }
  // The server sends at most 200 tracks; name the real count and say so.
  const totalCount = data?.total || count || 0
  const n = Math.max(tracks?.length || 0, totalCount)
  const truncated = tracks && totalCount > tracks.length
  return html`<div>
    <button class="back" onClick=${back}>${Icon.back()} ${backLabel}</button>
    <div class="pl-head">
      <div style="min-width:0">
        <h1 class="title">${title}</h1>
        ${onArtist && tracks?.[0]?.artist && html`<button class="artist-link" onClick=${() => onArtist(tracks[0])}>
          ${tracks[0].artist.split(', ')[0]}${Icon.chevron()}</button>`}
        ${n > 0 && html`<div class="dim mono" style="font-size:12px">${n} ${n === 1 ? 'track' : 'tracks'}</div>`}
      </div>
      <div class="pl-actions">
        ${actions}
        <button class="pill" disabled=${starting || loading || !tracks?.length} onClick=${() => { haptic(); playAt(0).catch(failed('Could not play')) }}>${Icon.play()} Play</button>
      </div>
    </div>
    ${sortBar}
    ${loading && !tracks && html`<${Skeleton} />`}
    ${error && html`<p class="error-line">${error.message}</p>`}
    ${tracks && !tracks.length && !loading && html`<div class="empty"><strong>Nothing here</strong></div>`}
    <ul class="rows" style=${loading ? 'opacity:0.55;pointer-events:none' : undefined}>${(tracks || []).map((t, i) => html`<${TrackRow} track=${t} index=${numbered ? i + 1 : undefined}
      onPlay=${() => playAt(i)} onQueue=${() => remote.queueTrack(t)} />`)}</ul>
    ${truncated && html`<p class="dim" style="font-size:13px;padding:12px 0">Showing the first ${tracks.length} of ${totalCount}. Play loads them all.</p>`}
  </div>`
}

// playFrom loads a list into cliamp, which starts at its first track, then
// jumps to track i.
const playFrom = (remote, load) => async (i) => {
  await load()
  if (i > 0) await remote.queuePlay(i)
}

export function Album({ remote, provider, track, back, backLabel, openArtist }) {
  const id = albumID(track)
  return html`<${Collection} remote=${remote} title=${trackTitle(track)} backLabel=${backLabel} numbered
    onArtist=${openArtist && provider.artist_profiles ? (t) => openArtist(provider, { track: t }) : null}
    load=${() => remote.albumTracks(provider.key, id)}
    deps=${[provider.key, id]}
    start=${playFrom(remote, () => remote.loadAlbum(provider.key, id))} back=${back} />`
}

export function Playlist({ remote, provider, list, back }) {
  const { epoch } = useRemote(remote)
  const cacheKey = epoch + ':' + provider.key
  const mayFollow = !!provider.follow_playlists && remote.has('provider.follow_playlist') &&
    !isSyntheticPlaylist(list.id) && !list.owned && list.section !== 'Your playlists'

  const findInLibrary = (lists) => (lists || []).find((pl) => pl.id === list.id)
  const [libState, setLibState] = useState(() => {
    if (!mayFollow) return { ready: true, saved: false, owned: !!list.owned || list.section === 'Your playlists' }
    if (playlistCache.has(cacheKey)) {
      const hit = findInLibrary(playlistCache.get(cacheKey))
      return { ready: true, saved: !!hit, owned: !!(hit?.owned || hit?.section === 'Your playlists') }
    }
    return { ready: false, saved: false, owned: false }
  })
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    if (!mayFollow) {
      setLibState({ ready: true, saved: false, owned: !!list.owned || list.section === 'Your playlists' })
      return
    }
    if (playlistCache.has(cacheKey)) {
      const hit = findInLibrary(playlistCache.get(cacheKey))
      setLibState({ ready: true, saved: !!hit, owned: !!(hit?.owned || hit?.section === 'Your playlists') })
      return
    }
    let live = true
    setLibState({ ready: false, saved: false, owned: false })
    remote.playlists(provider.key).then(
      (lists) => {
        playlistCache.set(cacheKey, lists)
        if (!live) return
        const hit = findInLibrary(lists)
        setLibState({ ready: true, saved: !!hit, owned: !!(hit?.owned || hit?.section === 'Your playlists') })
      },
      () => live && setLibState({ ready: true, saved: false, owned: false }),
    )
    return () => { live = false }
  }, [mayFollow, cacheKey, list.id])

  const toggleSave = () => {
    if (saving) return
    const next = !libState.saved
    setSaving(true)
    remote.followPlaylist(provider.key, list.id, next).then(
      () => {
        playlistCache.delete(cacheKey)
        setLibState((s) => ({ ...s, saved: next }))
        setSaving(false)
        toast(next ? 'Saved to library' : 'Removed from library')
      },
      (err) => {
        setSaving(false)
        failed(next ? 'Could not save' : 'Could not remove')(err)
      },
    )
  }

  const sortKey = `cliamp.plSort:${provider.key}:${list.id}`
  const [sort, setSort] = useState(() => {
    try { return localStorage.getItem(sortKey) || '' } catch { return '' }
  })
  const [sorts, setSorts] = useState([])
  const pickSort = (id) => {
    const next = id === 'default' ? '' : id
    setSort(next)
    try {
      if (next) localStorage.setItem(sortKey, next)
      else localStorage.removeItem(sortKey)
    } catch {}
  }

  const activeSort = sort || sorts[0]?.id || 'default'
  const saveAction = mayFollow && libState.ready && !libState.owned && html`
    <button class="pl-save" aria-pressed=${libState.saved ? 'true' : 'false'} disabled=${saving}
      aria-label=${libState.saved ? 'Saved in library' : 'Save to library'} onClick=${() => { haptic(); toggleSave() }}>
      ${libState.saved ? Icon.check() : Icon.plus()} ${libState.saved ? 'Saved' : 'Save'}
    </button>`
  const sortBar = sorts.length > 1 && html`<div class="chips" role="group" aria-label="Sort tracks" style="margin-top:0">
    ${sorts.map((s) => html`<button class="chip" aria-pressed=${activeSort === s.id ? 'true' : 'false'}
      onClick=${() => pickSort(s.id)}>${s.label}</button>`)}
  </div>`

  return html`<${Collection} key=${provider.key + ':' + list.id} remote=${remote} title=${list.name} backLabel=${provider.name} count=${list.track_count}
    actions=${saveAction} sortBar=${sortBar}
    load=${() =>
      remote.tracks(provider.key, list.id, sort || undefined).then(
        (r) => {
          if (r.sorts.length) {
            setSorts(r.sorts)
            if (sort && !r.sorts.some((s) => s.id === sort)) pickSort('default')
          }
          return r
        },
        (err) => {
          if (sort) pickSort('default')
          throw err
        },
      )}
    deps=${[provider.key, list.id, sort]}
    start=${playFrom(remote, () => remote.loadPlaylist(provider.key, list.id, sort || undefined))} back=${back} />`
}

// The library views a provider can fill. Songs and Podcasts are sections
// of its playlist list (Spotify calls them Library and Podcasts);
// Artists and Albums come from its browse endpoints when it has them.
const SECTION_CATS = [
  { key: 'songs', name: 'Songs', section: 'Library' },
  { key: 'podcasts', name: 'Podcasts', section: 'Podcasts' },
]

function libraryCats(provider, rows) {
  const cats = [{ key: 'playlists', name: 'Playlists' }]
  if (provider?.browse_artists) cats.push({ key: 'artists', name: 'Artists' })
  if (provider?.browse_albums) cats.push({ key: 'albums', name: 'Albums' })
  for (const cat of SECTION_CATS) {
    if (rows?.some((pl) => pl.section === cat.section)) cats.push(cat)
  }
  return cats
}

// PagedList appends pages from load(offset), which resolves {items,
// done}. deps reset it.
function usePaged(load, deps) {
  const [st, setSt] = useState({ items: [], loading: true, error: null, done: false })
  const seq = useRef(0)
  const page = (offset, append) => {
    const n = ++seq.current
    if (!append) setSt({ items: [], loading: true, error: null, done: false })
    load(offset).then(
      ({ items, done }) => n === seq.current && setSt((s) => ({ items: append ? [...s.items, ...items] : items, loading: false, error: null, done })),
      (error) => n === seq.current && setSt((s) => ({ ...s, loading: false, error })),
    )
  }
  useEffect(() => page(0, false), deps)
  return { ...st, more: () => page(st.items.length, true) }
}

function ArtistsView({ remote, provider, openArtist, onAlbums }) {
  const { items, loading, error, done, more } = usePaged(
    (offset) => remote.artists(provider.key, offset).then((r) => ({ items: r.artists, done: offset + r.artists.length >= r.total || !r.artists.length })),
    [provider.key],
  )
  const open = (a) => (openArtist && provider.artist_profiles ? openArtist(provider, { artist: a.id }) : onAlbums(a))
  return html`<div>
    ${loading && !items.length && html`<${Skeleton} />`}
    ${error && html`<p class="error-line">${error.message}</p>`}
    ${!loading && !error && !items.length && html`<div class="empty"><strong>No artists</strong>${provider.name} lists none for this account.</div>`}
    <ul class="rows">${items.map((a) => html`<${ArtistRow} artist=${a}
      sub=${a.album_count > 0 ? a.album_count + (a.album_count === 1 ? ' album' : ' albums') : 'Artist'} onOpen=${() => open(a)} />`)}</ul>
    ${!done && items.length > 0 && html`<button class="chip more" disabled=${loading} onClick=${more}>More artists</button>`}
  </div>`
}

function ArtistAlbumsView({ remote, provider, artist, back, onAlbum }) {
  const { data, loading, error } = useLoad(() => remote.artistAlbums(provider.key, artist.id).then((r) => r.albums), [provider.key, artist.id])
  return html`<div>
    <button class="back" onClick=${back}>${Icon.back()} Artists</button>
    <h1 class="title">${artist.name}</h1>
    ${loading && html`<${Skeleton} />`}
    ${error && html`<p class="error-line">${error.message}</p>`}
    <ul class="rows">${(data || []).map((a) => html`<${TrackRow} track=${albumPlace(a)} onPlay=${() => onAlbum(a)} onOpen=${() => onAlbum(a)} />`)}</ul>
  </div>`
}

function AlbumsView({ remote, provider, onAlbum }) {
  const [sort, setSort] = useState(null)
  const [sorts, setSorts] = useState([])
  const { items, loading, error, done, more } = usePaged(
    (offset) =>
      remote.albums(provider.key, sort || undefined, offset).then((r) => {
        if (r.sorts.length) setSorts(r.sorts)
        return { items: r.albums, done: r.albums.length < 100 }
      }),
    [provider.key, sort],
  )
  return html`<div>
    ${sorts.length > 1 && html`<div class="chips" role="group" aria-label="Sort albums" style="margin-top:0">
      ${sorts.map((s) => html`<button class="chip" aria-pressed=${(sort ?? sorts[0].id) === s.id ? 'true' : 'false'} onClick=${() => setSort(s.id)}>${s.label}</button>`)}
    </div>`}
    ${loading && !items.length && html`<${Skeleton} />`}
    ${error && html`<p class="error-line">${error.message}</p>`}
    ${!loading && !error && !items.length && html`<div class="empty"><strong>No albums</strong>${provider.name} lists none for this account.</div>`}
    <ul class="rows">${items.map((a) => html`<${TrackRow} track=${albumPlace(a)} onPlay=${() => onAlbum(a)} onOpen=${() => onAlbum(a)} />`)}</ul>
    ${!done && items.length > 0 && html`<button class="chip more" disabled=${loading} onClick=${more}>More albums</button>`}
  </div>`
}

// AccountRow shows who a provider is signed in as, and switches account.
// Signing in happens in the browser on the cliamp machine, so the wait
// says where to finish it.
function AccountRow({ remote, provider, machine, onSwitched }) {
  const { data, loading, error, reload } = useLoad(() => remote.account(provider.key), [provider.key])
  const [stage, setStage] = useState('idle') // idle | confirm | waiting
  const [switched, setSwitched] = useState(null)
  const account = switched || data
  const go = () => {
    setStage('waiting')
    remote.switchAccount(provider.key).then(
      (next) => {
        setSwitched(next)
        setStage('idle')
        onSwitched()
        toast('Signed in as ' + (next?.name || provider.name))
      },
      (err) => {
        setStage('idle')
        toast('Could not switch account: ' + err.message, 'error')
        reload()
      },
    )
  }
  if (error && !account) return null
  const signedOut = !loading && !account
  return html`<div class="account-row">
    ${Icon.user()}
    ${stage === 'waiting'
      ? html`<span class="row-text trunc">Finish signing in in the browser on ${machine}…</span>`
      : stage === 'confirm'
        ? html`<span class="row-text">Switch ${provider.name} account? Playback from ${provider.name} stops.</span>
          <button class="chip" onClick=${go}>Switch account</button>
          <button class="chip" onClick=${() => setStage('idle')}>Cancel</button>`
        : html`<span class="row-text trunc">${account ? 'Signed in as ' + account.name : signedOut ? 'Not signed in to ' + provider.name : 'Reading account…'}${account?.detail ? ' · ' + account.detail : ''}</span>
          ${signedOut && html`<button class="chip" onClick=${go}>Sign in</button>`}
          ${account && html`<button class="chip" onClick=${() => setStage('confirm')}>Switch</button>`}`}
  </div>`
}

export function Library({ remote, machines, openArtist, openLinks }) {
  const { providers, epoch } = useRemote(remote)
  const list = mainProviders(providers).slice(0, 5)
  const [key, setKey] = useState(null)
  const [open, setOpen] = useState(null)
  const [cat, setCat] = useState('playlists')
  const [album, setAlbum] = useState(null)
  const [artist, setArtist] = useState(null)
  const provider = list.find((p) => p.key === (key ?? list[0]?.key))
  const { data, loading, error, reload } = useLoad(async () => {
    if (!provider) return []
    // Cached per connection: reconnecting, or reopening the app, rereads it.
    const key = epoch + ':' + provider.key
    if (!playlistCache.has(key)) playlistCache.set(key, await remote.playlists(provider.key))
    return playlistCache.get(key)
  }, [provider?.key, epoch])

  if (open && provider) {
    return html`<${Playlist} remote=${remote} provider=${provider} list=${open}
      back=${() => { setOpen(null); if (!playlistCache.has(epoch + ':' + provider.key)) reload() }} />`
  }
  if (album && provider)
    return html`<${Album} remote=${remote} provider=${provider} track=${albumPlace(album)} backLabel="Library" back=${() => setAlbum(null)} openArtist=${openArtist} />`
  if (artist && provider)
    return html`<${ArtistAlbumsView} remote=${remote} provider=${provider} artist=${artist} back=${() => setArtist(null)} onAlbum=${setAlbum} />`

  const cats = libraryCats(provider, data)
  const activeCat = cats.some((c) => c.key === cat) ? cat : 'playlists'
  const sectionCat = SECTION_CATS.find((c) => c.key === activeCat)
  const rows = sectionCat ? (data || []).filter((pl) => pl.section === sectionCat.section) : data || []
  const grouped = new Set(rows.map((pl) => pl.section)).size > 1
  const resolver = providers.find((p) => p.links)
  let lastSection = null
  return html`<div>
    <div class="pl-head">
      <h1 class="title">Library</h1>
      ${openLinks && resolver && html`<button class="pill small" onClick=${() => openLinks('')}>${Icon.link()} Open link</button>`}
    </div>
    <${Segments} items=${list} value=${provider?.key} onChange=${(k) => { setKey(k); setOpen(null); setCat('playlists') }} label="Provider" />
    ${provider?.account && html`<${AccountRow} remote=${remote} provider=${provider} machine=${machineLabel(machines?.current)}
      onSwitched=${() => { playlistCache.delete(epoch + ':' + provider.key); reload() }} />`}
    ${provider && cats.length > 1 && html`<${Segments} items=${cats} value=${activeCat} onChange=${setCat} label="View" />`}
    ${!provider && html`<div class="empty"><strong>No providers yet</strong>cliamp has not reported its providers.</div>`}
    ${provider && activeCat === 'artists' && html`<${ArtistsView} remote=${remote} provider=${provider} openArtist=${openArtist} onAlbums=${setArtist} />`}
    ${provider && activeCat === 'albums' && html`<${AlbumsView} remote=${remote} provider=${provider} onAlbum=${setAlbum} />`}
    ${provider && activeCat !== 'artists' && activeCat !== 'albums' && html`<div>
      ${loading && html`<${Skeleton} />`}
      ${error && html`<p class="error-line">${error.message} <button class="chip" onClick=${() => { playlistCache.delete(epoch + ':' + provider.key); reload() }}>Retry</button></p>`}
      ${data && !rows.length && !loading && html`<div class="empty"><strong>Nothing here</strong>${provider?.name} has none for this account.</div>`}
      <ul class="rows" style="margin-top:8px">
        ${rows.map((pl) => {
          const head = !sectionCat && grouped && pl.section !== lastSection
          lastSection = pl.section
          return html`${head && html`<li class="section-head" style="margin-left:12px"><span class="eyebrow">${pl.section}</span></li>`}
            <${PlaylistRow} list=${pl} onOpen=${() => setOpen(pl)} />`
        })}
      </ul>
    </div>`}
  </div>`
}

// ---------- queue ----------

export function Queue({ remote }) {
  const { snapshot, epoch } = useRemote(remote)
  const rev = snapshot?.playlist_revision
  const { data, loading, error } = useLoad(
    () => Promise.all([remote.playNextList(), remote.queueList()]).then(([next, q]) => ({ next: next.tracks, nextRev: next.rev, q })),
    [rev, epoch],
  )
  const current = snapshot?.index ?? 0
  const hasNext = data?.next.length > 0
  // An Up next entry points at a row of the playlist. Playing it means
  // taking it off Up next and playing that row; track.play would add a
  // second copy of the track and leave the entry queued.
  const playUpNext = async (i, t) => {
    await remote.playNextRemove(i, data.nextRev)
    await remote.queuePlay(t.index ?? 0)
  }
  return html`<div>
    <div class="pl-head"><h1 class="title">Queue</h1>
      ${data && html`<span class="dim mono" style="font-size:12px;padding-bottom:6px">${data.q.total} ${data.q.total === 1 ? 'track' : 'tracks'}</span>`}</div>
    ${loading && !data && html`<${Skeleton} />`}
    ${error && html`<p class="error-line">${error.message}</p>`}
    ${hasNext && html`<section aria-label="Up next">
      <div class="section-head"><span class="eyebrow">Up next</span></div>
      <ul class="rows">${data.next.map((t, i) => html`<${TrackRow} track=${t}
        onPlay=${() => playUpNext(i, t)} onRemove=${() => remote.playNextRemove(i, data.nextRev)} />`)}</ul>
    </section>`}
    ${data && html`<section aria-label="Playlist">
      ${hasNext && html`<div class="section-head"><span class="eyebrow">Then</span></div>`}
      ${!data.q.tracks.length && html`<div class="empty"><strong>The queue is empty</strong>Play something from Search or Library.</div>`}
      <ul class="rows">${data.q.tracks.map((t, i) => html`<${TrackRow} track=${t} index=${i + 1} active=${i === current}
        onPlay=${() => remote.queuePlay(i, data.q.rev)} onRemove=${() => remote.queueRemove(i, data.q.rev)} />`)}</ul>
      ${data.q.total > data.q.tracks.length && html`<p class="dim" style="font-size:13px;padding:12px 0">Showing the first ${data.q.tracks.length} of ${data.q.total}.</p>`}
    </section>`}
  </div>`
}
