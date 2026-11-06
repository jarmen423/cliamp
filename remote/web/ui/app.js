// The app shell: pairing, the tab bar and the routes.
import { html, render, useEffect, useState } from '../vendor/preact-htm.js'
import { createRemote } from '../core/api.js'
import { trackTitle } from '../core/store.js'
import { setArtAuth } from './art.js'
import { Icon } from './icons.js'
import { useRemote } from './hooks.js'
import { Library, Queue, Search } from './lists.js'
import { MiniPlayer, NowPlaying } from './now.js'
import { Toasts } from './toast.js'

const PAIR_KEY = 'cliamp.pair'
const ROUTES = ['now', 'search', 'library', 'queue']
const TABS = [
  { key: 'now', name: 'Playing', icon: Icon.now },
  { key: 'search', name: 'Search', icon: Icon.search },
  { key: 'library', name: 'Library', icon: Icon.library },
  { key: 'queue', name: 'Queue', icon: Icon.queue },
]
const TOKEN_RE = /^[0-9a-f]{64}$/i

// takePairing reads a token from the #t= fragment of a pairing URL, saves
// it, and removes it from the address bar and history.
function takePairing() {
  const m = /(?:^#|&)t=([0-9a-f]{64})/i.exec(location.hash)
  if (m) {
    savePairing(m[1])
    history.replaceState(null, '', location.pathname + '#/now')
  }
  try {
    const saved = JSON.parse(localStorage.getItem(PAIR_KEY))
    return saved?.token && saved.base === location.origin ? saved : null
  } catch {
    return null
  }
}

function savePairing(token) {
  localStorage.setItem(PAIR_KEY, JSON.stringify({ base: location.origin, token }))
}

const route = () => {
  const r = location.hash.replace(/^#\/?/, '')
  return ROUTES.includes(r) ? r : 'now'
}

// accentOf returns the accent of the cliamp theme when it reads on the dark
// canvas, so the phone wears the same color as the terminal.
function accentOf(snap) {
  const c = snap?.theme?.accent
  if (!c || !/^#[0-9a-f]{6}$/i.test(c)) return null
  const [r, g, b] = [1, 3, 5].map((i) => parseInt(c.slice(i, i + 2), 16) / 255)
  const luma = 0.2126 * r + 0.7152 * g + 0.0722 * b
  return luma > 0.28 ? c : null
}

function Pair({ reason }) {
  const [value, setValue] = useState('')
  const [error, setError] = useState('')
  const submit = (e) => {
    e.preventDefault()
    const v = value.trim()
    const m = /#t=([0-9a-f]{64})/i.exec(v)
    if (/^https?:\/\//i.test(v) && m) {
      const target = new URL(v)
      if (target.origin !== location.origin) return void (location.href = v)
      savePairing(m[1])
      return void location.reload()
    }
    if (TOKEN_RE.test(v)) {
      savePairing(v)
      return void location.reload()
    }
    setError('Paste the whole URL that cliamp remote pair printed.')
  }
  return html`<main class="pair">
    ${reason === 'unauthorized' && html`<p class="notice" role="alert">cliamp no longer accepts this phone's pairing. Pair it again.</p>`}
    <div class="wordmark" aria-label="cliamp">cliamp<span class="cursor" aria-hidden="true"></span></div>
    <p class="pair-lede">Control the music on your computer from this phone.</p>
    <ol class="pair-steps">
      <li><span>On the computer, run <code>cliamp remote pair</code>.</span></li>
      <li><span>Scan the code with this phone's camera.</span></li>
    </ol>
    <form onSubmit=${submit} novalidate>
      <label class="eyebrow" for="pair-url">Or paste the pairing URL</label>
      <div class="field"><input id="pair-url" type="url" inputmode="url" autocomplete="off" spellcheck="false"
        placeholder="http://192.168.1.20:7700/#t=…" value=${value} onInput=${(e) => { setValue(e.currentTarget.value); setError('') }} /></div>
      ${error && html`<p class="error-line" role="alert" style="padding:0">${error}</p>`}
      <button class="pill" type="submit">Pair</button>
    </form>
  </main>`
}

function ConnectionPill({ remote }) {
  const { status } = useRemote(remote)
  const [show, setShow] = useState(false)
  useEffect(() => {
    // Wait a beat, so a quick reconnect never flashes the pill.
    if (status === 'open') return setShow(false)
    const id = setTimeout(() => setShow(true), 1200)
    return () => clearTimeout(id)
  }, [status])
  if (!show) return null
  return html`<div class="conn" role="status"><i></i>Reconnecting to cliamp…</div>`
}

function Shell({ remote }) {
  const [r, setR] = useState(route())
  const state = useRemote(remote)
  useEffect(() => {
    const on = () => setR(route())
    addEventListener('hashchange', on)
    return () => removeEventListener('hashchange', on)
  }, [])
  const go = (to) => {
    location.hash = '#/' + to
  }

  const snap = state.snapshot
  const accent = accentOf(snap)
  useEffect(() => {
    if (accent) document.documentElement.style.setProperty('--accent', accent)
    else document.documentElement.style.removeProperty('--accent')
  }, [accent])
  useEffect(() => {
    document.title = snap?.track ? `${trackTitle(snap.track)} · cliamp` : 'cliamp'
  }, [snap?.track])

  const View = { now: NowPlaying, search: Search, library: Library, queue: Queue }[r]
  const mini = r !== 'now' && !!snap?.track
  return html`<div class="shell">
    <${ConnectionPill} remote=${remote} />
    <main class=${'view' + (mini ? ' has-mini' : '')} id="view" key=${r}>
      <${View} remote=${remote} go=${go} />
    </main>
    ${mini && html`<${MiniPlayer} remote=${remote} go=${go} />`}
    <${Toasts} />
    <nav class="tabs" aria-label="Sections">
      ${TABS.map((t) => html`<button class="tab" aria-current=${t.key === r ? 'page' : undefined} onClick=${() => go(t.key)}>
        ${t.icon()}<span>${t.name}</span></button>`)}
    </nav>
  </div>`
}

function App({ pairing }) {
  const [remote] = useState(() => pairing && createRemote(pairing))
  const [status, setStatus] = useState(remote ? remote.store.get().status : 'closed')
  useEffect(() => {
    if (!remote) return
    setArtAuth(pairing.base, pairing.token)
    const off = remote.store.subscribe((s) => setStatus(s.status))
    remote.start()
    // A phone suspends the page in the background; come back fresh.
    const onVisible = () => document.visibilityState === 'visible' && remote.refresh()
    document.addEventListener('visibilitychange', onVisible)
    return () => {
      off()
      document.removeEventListener('visibilitychange', onVisible)
      remote.stop()
    }
  }, [remote])
  if (!remote || status === 'unauthorized') {
    if (status === 'unauthorized') localStorage.removeItem(PAIR_KEY)
    return html`<${Pair} reason=${status} />`
  }
  return html`<${Shell} remote=${remote} />`
}

render(html`<${App} pairing=${takePairing()} />`, document.getElementById('app'))
