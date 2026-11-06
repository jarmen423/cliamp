// The app shell: pairing, the tab bar and the routes.
import { html, render, useEffect, useMemo, useRef, useState } from '../vendor/preact-htm.js'
import { createRemote } from '../core/api.js'
import {
  addMachine, blockedByScheme, loadActive, loadMachines, movePlayback, parsePairing,
  probe, saveActive, saveMachines, waitOpen,
} from '../core/machines.js'
import { trackTitle } from '../core/store.js'
import { setArtAuth } from './art.js'
import { Icon } from './icons.js'
import { useRemote } from './hooks.js'
import { Library, Queue, Search } from './lists.js'
import { machineLabel } from './machines.js'
import { MiniPlayer, NowPlaying } from './now.js'
import { toast, Toasts } from './toast.js'
const ROUTES = ['now', 'search', 'library', 'queue']
const TABS = [
  { key: 'now', name: 'Playing', icon: Icon.now },
  { key: 'search', name: 'Search', icon: Icon.search },
  { key: 'library', name: 'Library', icon: Icon.library },
  { key: 'queue', name: 'Queue', icon: Icon.queue },
]
// takePairing adds the machine of a pairing link in the address bar, then
// removes the token from the address bar and history. It returns the base
// of the machine it added.
function takePairing() {
  const p = parsePairing(location.hash, location.origin)
  if (!p) return ''
  history.replaceState(null, '', location.pathname + '#/now')
  if (blockedByScheme(p.base, location.protocol)) return ''
  saveMachines(localStorage, addMachine(loadMachines(localStorage), p))
  return p.base
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

function Pair({ reason, onAdd }) {
  const [value, setValue] = useState('')
  const [error, setError] = useState('')
  const submit = (e) => {
    e.preventDefault()
    const p = parsePairing(value.trim(), location.origin)
    if (!p) return setError('Paste the whole URL that cliamp remote pair printed.')
    if (blockedByScheme(p.base, location.protocol)) {
      return setError('This app runs over https, so that machine needs https too. Pair it with --base.')
    }
    onAdd(p)
  }
  return html`<main class="pair">
    ${reason && html`<p class="notice" role="alert">${reason}</p>`}
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

function Shell({ remote, machines }) {
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
      <${View} remote=${remote} go=${go} machines=${machines} />
    </main>
    ${mini && html`<${MiniPlayer} remote=${remote} go=${go} />`}
    <${Toasts} />
    <nav class="tabs" aria-label="Sections">
      ${TABS.map((t) => html`<button class="tab" aria-current=${t.key === r ? 'page' : undefined} onClick=${() => go(t.key)}>
        ${t.icon()}<span>${t.name}</span></button>`)}
    </nav>
  </div>`
}

function App({ added }) {
  const [list, setList] = useState(() => loadMachines(localStorage))
  const [active, setActive] = useState(() => {
    const all = loadMachines(localStorage)
    const saved = added || loadActive(localStorage)
    return all.some((m) => m.base === saved) ? saved : all[0]?.base || ''
  })
  const [reason, setReason] = useState('')
  const remotes = useRef(new Map()) // base -> remote, made when first used
  const current = list.find((m) => m.base === active)

  const update = (fn) =>
    setList((prev) => {
      const next = typeof fn === 'function' ? fn(prev) : fn
      saveMachines(localStorage, next)
      return next
    })
  const choose = (base) => setActive(base)
  useEffect(() => saveActive(localStorage, active), [active])
  const remoteFor = (m) => {
    let r = remotes.current.get(m.base)
    if (!r || r.token !== m.token) {
      r?.stop()
      r = Object.assign(createRemote({ base: m.base, token: m.token }), { token: m.token })
      remotes.current.set(m.base, r)
    }
    return r
  }
  const remote = current && remoteFor(current)

  useEffect(() => {
    if (!current) return
    setArtAuth(current.base, current.token)
    const off = remote.store.subscribe((s) => {
      if (s.status !== 'unauthorized') return
      // This machine dropped the pairing: forget it and move on.
      const rest = list.filter((m) => m.base !== current.base)
      update((prev) => prev.filter((m) => m.base !== current.base))
      remotes.current.delete(current.base)
      const msg = machineLabel(current) + ' no longer accepts this phone. Pair it again.'
      if (rest.length) {
        toast(msg, 'error')
        choose(rest[0].base)
      } else {
        setReason(msg)
        choose('')
      }
    })
    remote.start()
    probe(current).then((info) => info.name && info.name !== current.name && machines.rename(current.base, info.name))
    // A phone suspends the page in the background; come back fresh.
    const onVisible = () => document.visibilityState === 'visible' && remote.refresh()
    document.addEventListener('visibilitychange', onVisible)
    return () => {
      off()
      document.removeEventListener('visibilitychange', onVisible)
      remote.stop()
    }
  }, [remote])

  const machines = useMemo(() => ({
    list,
    current,
    get playing() {
      return remote?.store.get().snapshot?.track && remote.store.get().snapshot.state === 'playing'
    },
    select(base) {
      if (base !== active) choose(base)
    },
    add(p) {
      update((prev) => addMachine(prev, p))
      toast('Added ' + p.base.replace(/^https?:\/\//, ''))
    },
    remove(base) {
      remotes.current.get(base)?.stop()
      remotes.current.delete(base)
      update((prev) => prev.filter((m) => m.base !== base))
    },
    rename(base, name) {
      update((prev) => prev.map((m) => (m.base === base ? { ...m, name } : m)))
    },
    // move plays what plays here on base, then switches the app to base.
    async move(base) {
      const target = list.find((m) => m.base === base)
      const to = remoteFor(target)
      to.name = machineLabel(target)
      to.start()
      try {
        await waitOpen(to)
        await movePlayback(remote, to)
      } catch (err) {
        to.stop()
        throw err
      }
      choose(base)
    },
  }), [list, active, remote])

  if (!current) {
    return html`<${Pair} reason=${reason} onAdd=${(p) => { update((prev) => addMachine(prev, p)); setReason(''); choose(p.base) }} />`
  }
  return html`<${Shell} key=${current.base} remote=${remote} machines=${machines} />`
}

render(html`<${App} added=${takePairing()} />`, document.getElementById('app'))

// The service worker keeps the app on the phone. Browsers allow it only on
// https (or localhost), the same pages that can install the app.
if ('serviceWorker' in navigator && globalThis.isSecureContext) {
  navigator.serviceWorker.register('sw.js').catch(() => {})
}
