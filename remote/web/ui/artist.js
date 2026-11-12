// The artist page: what the TUI artist screen shows, laid out for a phone.
// A photo header, the popular tracks, then the discography by release type.
import { html, useEffect, useState } from '../vendor/preact-htm.js'
import { compactCount, formatTime, trackTitle } from '../core/store.js'
import { Art } from './art.js'
import { Icon } from './icons.js'
import { haptic } from './hooks.js'
import { Album, PlayNextButton, Skeleton } from './lists.js'
import { toast } from './toast.js'

const POPULAR_SHOWN = 5
const TYPES = [
  { key: 'all', name: 'All' },
  { key: 'album', name: 'Albums' },
  { key: 'single', name: 'Singles and EPs' },
  { key: 'compilation', name: 'Compilations' },
]
const TYPE_LABEL = { album: 'Album', single: 'Single', compilation: 'Compilation' }
const LIKED_META = 'spotify.liked'

// ArtistPage opens an artist by id, or the artist of track.
export function ArtistPage({ remote, provider, artist, track, back, backLabel }) {
  const [state, setState] = useState({ loading: true })
  const [more, setMore] = useState(false)
  const [type, setType] = useState('all')
  const [album, setAlbum] = useState(null)
  const [starting, setStarting] = useState(false)

  useEffect(() => {
    let live = true
    setState({ loading: true })
    remote.artist(provider.key, { artist, track }).then(
      (data) => live && setState({ data }),
      (error) => live && setState({ error }),
    )
    return () => { live = false }
  }, [provider.key, artist, track?.path])

  const data = state.data
  const info = data?.info
  // Plays the popular tracks as the queue, from row i.
  const playFrom = async (i) => {
    setStarting(true)
    try {
      await remote.loadArtist(provider.key, info.id)
      if (i > 0) await remote.queuePlay(i)
    } catch (err) {
      toast('Could not play: ' + err.message, 'error')
    } finally {
      setStarting(false)
    }
  }

  if (album) {
    const placeholder = {
      title: album.name, artist: info?.name, album_art_url: album.image_url,
      provider_meta: { kind: 'album', albumID: album.id },
    }
    return html`<${Album} remote=${remote} provider=${provider} track=${placeholder}
      backLabel=${info?.name || 'Artist'} back=${() => setAlbum(null)} />`
  }

  const popular = data?.popular || []
  const shown = more ? popular : popular.slice(0, POPULAR_SHOWN)
  const types = TYPES.filter((t) => t.key === 'all' || (data?.albums || []).some((a) => a.type === t.key))
  const releases = (data?.albums || []).filter((a) => type === 'all' || a.type === type)
  const liked = popular.filter((t) => t.provider_meta?.[LIKED_META] === 'true').length

  return html`<div class="artist">
    <header class="artist-hero">
      ${info?.image_url && html`<${Photo} url=${info.image_url} />`}
      <button class="hero-back" onClick=${back} aria-label=${'Back to ' + backLabel}>${Icon.back()}</button>
      <div class="artist-head">
        <h1 class="artist-name">${info?.name || ''}</h1>
        ${info && html`<p class="artist-meta mono">
          ${info.followers > 0 && html`<span>${compactCount(info.followers)} followers</span>`}
          ${info.genres?.length > 0 && html`<span class="trunc">${info.genres.slice(0, 2).join(', ')}</span>`}
        </p>`}
      </div>
    </header>

    ${state.loading && html`<${Skeleton} rows=${5} />`}
    ${state.error && html`<p class="error-line">${state.error.message}</p>`}

    ${data && html`<div class="artist-actions">
      <button class="play play-sm" aria-label=${'Play ' + (info?.name || 'artist')} disabled=${starting || !popular.length}
        onClick=${() => { haptic(); playFrom(0) }}>${Icon.play()}</button>
      ${info?.id && html`<${FollowButton} remote=${remote} provider=${provider} artist=${info} />`}
      ${liked > 0 && html`<span class="dim artist-liked">${Icon.check()} You liked ${liked} of these</span>`}
    </div>`}

    ${popular.length > 0 && html`<section aria-labelledby="popular-h">
      <h2 id="popular-h" class="section-title">Popular</h2>
      <ol class="rows">${shown.map((t, i) => html`<${PopularRow} key=${t.path} track=${t} rank=${i + 1}
        onPlay=${() => playFrom(i)} onQueue=${() => remote.queueTrack(t)} />`)}</ol>
      ${popular.length > POPULAR_SHOWN && html`<button class="chip more" onClick=${() => setMore(!more)}>
        ${more ? 'Show less' : 'Show more'}</button>`}
    </section>`}

    ${data?.albums?.length > 0 && html`<section aria-labelledby="disco-h">
      <h2 id="disco-h" class="section-title">Discography</h2>
      ${types.length > 2 && html`<div class="chips" role="group" aria-label="Release type">
        ${types.map((t) => html`<button class="chip" aria-pressed=${t.key === type ? 'true' : 'false'} onClick=${() => setType(t.key)}>${t.name}</button>`)}
      </div>`}
      <ul class="shelf">${releases.map((a) => html`<li key=${a.id}>
        <button class="release" onClick=${() => setAlbum(a)}>
          <${Art} track=${{ album_art_url: a.image_url }} class="release-art" />
          <span class="release-name">${a.name}</span>
          <span class="release-sub mono">${[a.year, TYPE_LABEL[a.type]].filter(Boolean).join(' · ')}</span>
        </button>
      </li>`)}</ul>
    </section>`}
  </div>`
}

// FollowButton follows or unfollows the artist. No provider reports the
// current state, so like the TUI it remembers this session's choice and
// starts at Follow.
function FollowButton({ remote, provider, artist }) {
  const key = 'cliamp.follow:' + provider.key + ':' + artist.id
  const [following, setFollowing] = useState(() => sessionStorage.getItem(key) === '1')
  const [busy, setBusy] = useState(false)
  const toggle = () => {
    const next = !following
    setBusy(true)
    remote.followArtist(provider.key, artist.id, next).then(
      () => {
        sessionStorage.setItem(key, next ? '1' : '0')
        setFollowing(next)
        setBusy(false)
      },
      (err) => {
        setBusy(false)
        toast('Could not ' + (next ? 'follow' : 'unfollow') + ': ' + err.message, 'error')
      },
    )
  }
  return html`<button class="chip" aria-pressed=${following ? 'true' : 'false'} disabled=${busy} onClick=${toggle}>
    ${following ? 'Following' : 'Follow'}</button>`
}

function Photo({ url }) {
  const [ready, setReady] = useState(false)
  return html`<img class=${'artist-photo' + (ready ? ' ready' : '')} src=${url} alt="" decoding="async"
    referrerpolicy="no-referrer" ref=${(el) => el?.complete && el.naturalWidth && !ready && setReady(true)}
    onLoad=${() => setReady(true)} />`
}

function PopularRow({ track, rank, onPlay, onQueue }) {
  const liked = track.provider_meta?.[LIKED_META] === 'true'
  return html`<li class="row">
    <button class="row-main" onClick=${() => { haptic(); onPlay() }}>
      <span class="row-index mono">${rank}</span>
      <${Art} track=${track} />
      <span class="row-text">
        <div class="row-title trunc">${trackTitle(track)}</div>
        <div class="row-sub">
          ${liked && html`<span class="liked" aria-label="Liked">${Icon.check()}</span>`}
          <span class="trunc">${track.album || ''}</span>
          ${track.duration_secs > 0 && html`<span class="mono">${formatTime(track.duration_secs)}</span>`}
        </div>
      </span>
    </button>
    <${PlayNextButton} onQueue=${onQueue} />
  </li>`
}
