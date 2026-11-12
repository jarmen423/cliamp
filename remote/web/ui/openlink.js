// Open a provider share link: pasted, or shared into the installed app
// (the manifest's share_target lands here with the text filled in). The
// server resolves the link; this page opens what it points at: a playlist
// or album as a list, an artist as their page, a song playing now.
import { html, useEffect, useRef, useState } from '../vendor/preact-htm.js'
import { Icon } from './icons.js'
import { haptic, useRemote } from './hooks.js'
import { Album, Playlist, Skeleton, albumPlace } from './lists.js'
import { toast } from './toast.js'

export function OpenLink({ remote, text: shared, back, backLabel, openArtist, go }) {
  const { providers } = useRemote(remote)
  const resolver = providers.find((p) => p.links)
  const [text, setText] = useState(shared || '')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [view, setView] = useState(null) // {list} or {album track}
  const input = useRef(null)
  const auto = useRef(false)

  const open = (value) => {
    value = (value || '').trim()
    if (!value || busy) return
    if (!resolver) return setError('This cliamp has no provider that opens links. Set up Spotify on the computer first.')
    setBusy(true)
    setError('')
    remote.openLink(resolver.key, value).then(
      (link) => {
        setBusy(false)
        if (!link) return setError('That link opened nothing.')
        if (link.kind === 'track' || link.kind === 'episode') {
          remote.playTrack(link.track).then(
            () => {
              toast('Playing ' + (link.name || 'track'))
              go('now')
            },
            (err) => setError(err.message),
          )
        } else if (link.kind === 'artist') {
          openArtist(resolver, { artist: link.id })
        } else if (link.kind === 'album') {
          setView({ album: albumPlace({ id: link.id, name: link.name, image_url: link.image_url }) })
        } else {
          setView({ list: { id: link.id, name: link.name || resolver.name + ' playlist', track_count: 0 } })
        }
      },
      (err) => {
        setBusy(false)
        setError(err.message)
      },
    )
  }
  // A shared link opens itself once the providers are known.
  useEffect(() => {
    if (shared && resolver && !auto.current) {
      auto.current = true
      open(shared)
    }
  }, [shared, resolver?.key])

  if (view?.list && resolver)
    return html`<${Playlist} remote=${remote} provider=${resolver} list=${view.list} back=${() => setView(null)} />`
  if (view?.album && resolver)
    return html`<${Album} remote=${remote} provider=${resolver} track=${view.album} backLabel="Open link"
      back=${() => setView(null)} openArtist=${openArtist} />`

  return html`<div>
    <button class="back" onClick=${back}>${Icon.back()} ${backLabel}</button>
    <h1 class="title">Open a link</h1>
    <p class="dim" style="font-size:14px;line-height:1.5">
      Paste a Spotify playlist, album, song, artist or podcast link.
      In Spotify, Share → Copy link brings one here; on Android the installed
      app also appears in the Share sheet as cliamp.
    </p>
    <form onSubmit=${(e) => { e.preventDefault(); open(text); input.current?.blur() }}>
      <label class="field" style="margin-top:16px">${Icon.link()}<span class="sr">Link</span>
        <input ref=${input} type="url" inputmode="url" autocomplete="off" autocorrect="off" spellcheck="false"
          placeholder="https://open.spotify.com/playlist/…" value=${text} onInput=${(e) => setText(e.currentTarget.value)} />
      </label>
      ${error && html`<p class="error-line" role="alert">${error}</p>`}
      <button class="pill" type="submit" style="margin-top:14px" disabled=${busy || !text.trim()} onClick=${haptic}>Open</button>
    </form>
    ${busy && html`<${Skeleton} rows=${3} />`}
  </div>`
}
