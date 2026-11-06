// The machine picker: which cliamp the app controls, and moving playback.
import { html, useEffect, useRef, useState } from '../vendor/preact-htm.js'
import { blockedByScheme, parsePairing, probe } from '../core/machines.js'
import { Icon } from './icons.js'
import { haptic } from './hooks.js'
import { toast } from './toast.js'

const host = (base) => base.replace(/^https?:\/\//, '')

export function machineLabel(m) {
  return m?.name || host(m?.base || '')
}

// MachineChip names the machine in control and opens the picker.
export function MachineChip({ machines }) {
  const [open, setOpen] = useState(false)
  return html`<button class="machine-chip" aria-haspopup="dialog" onClick=${() => setOpen(true)}>
      ${Icon.speaker()}<span class="trunc">${machineLabel(machines.current)}</span>${Icon.chevronDown()}
    </button>
    ${open && html`<${MachineSheet} machines=${machines} close=${() => setOpen(false)} />`}`
}

function MachineSheet({ machines, close }) {
  const dialog = useRef(null)
  const [status, setStatus] = useState({}) // base -> 'online' | 'offline' | 'unauthorized' | 'https'
  const [busy, setBusy] = useState('')
  const [link, setLink] = useState('')
  const [error, setError] = useState('')

  useEffect(() => {
    dialog.current?.showModal()
    let live = true
    for (const m of machines.list) {
      if (blockedByScheme(m.base, location.protocol)) {
        setStatus((s) => ({ ...s, [m.base]: 'https' }))
        continue
      }
      probe(m).then((info) => {
        if (!live) return
        setStatus((s) => ({ ...s, [m.base]: info.error || 'online' }))
        if (info.name && info.name !== m.name) machines.rename(m.base, info.name)
      })
    }
    return () => { live = false }
  }, [machines.list.length])

  // Errors show in the sheet: a toast would sit under the open dialog.
  const [failure, setFailure] = useState('')
  const run = async (base, fn, done) => {
    setBusy(base)
    setFailure('')
    try {
      await fn()
      if (done) toast(done)
      close()
    } catch (err) {
      setFailure(err.message)
    } finally {
      setBusy('')
    }
  }
  const add = (e) => {
    e.preventDefault()
    const p = parsePairing(link.trim(), location.origin)
    if (!p) return setError('Paste the whole link that cliamp remote pair printed.')
    if (blockedByScheme(p.base, location.protocol)) {
      return setError('This app runs over https, so that machine needs https too. On it, run tailscale serve and pair with --base.')
    }
    machines.add(p)
    setLink('')
    setError('')
  }
  const playing = !!machines.playing
  const appURL = location.origin

  return html`<dialog class="sheet" ref=${dialog} onClose=${close} onClick=${(e) => e.target === dialog.current && dialog.current.close()}
      aria-labelledby="machines-title">
    <div class="sheet-body">
      <div class="sheet-grip" aria-hidden="true"></div>
      <h2 id="machines-title" class="sheet-title">Machines</h2>
      <ul class="machines">
        ${machines.list.map((m) => {
          const active = m.base === machines.current?.base
          const st = active ? 'online' : status[m.base]
          const usable = st === 'online'
          return html`<li class="machine" data-active=${active ? '' : undefined}>
            <button class="machine-main" disabled=${active || busy !== ''}
              onClick=${() => run(m.base, () => machines.select(m.base))}>
              <span class="dot" data-status=${st || 'checking'} aria-hidden="true"></span>
              <span class="row-text">
                <div class="row-title trunc">${machineLabel(m)}</div>
                <div class="row-sub"><span class="trunc mono">${active ? 'Controlling' : statusText(st)} · ${host(m.base)}</span></div>
              </span>
            </button>
            ${!active && usable && playing && html`<button class="pill small" disabled=${busy !== ''}
              onClick=${() => { haptic(); run(m.base, () => machines.move(m.base), 'Playing on ' + machineLabel(m)) }}>
              ${busy === m.base ? 'Moving…' : 'Play here'}</button>`}
            ${!active && html`<button class="icon-btn small" aria-label=${'Remove ' + machineLabel(m)}
              onClick=${() => machines.remove(m.base)}>${Icon.close()}</button>`}
          </li>`
        })}
      </ul>
      ${failure && html`<p class="error-line" role="alert">${failure}</p>`}
      <form class="add-machine" onSubmit=${add} novalidate>
        <label class="eyebrow" for="add-link">Add a machine</label>
        <p class="dim add-help">On the other computer, run${' '}<code>cliamp remote pair --app ${appURL}</code>${' '}and
          scan the code, or paste its link here.</p>
        <div class="field"><input id="add-link" type="url" inputmode="url" autocomplete="off" spellcheck="false"
          placeholder="http://192.168.1.30:7700/#t=…" value=${link} onInput=${(e) => { setLink(e.currentTarget.value); setError('') }} /></div>
        ${error && html`<p class="error-line" role="alert" style="padding:0">${error}</p>`}
        <div class="sheet-actions">
          <button class="pill" type="submit" disabled=${!link.trim()}>Add</button>
          <button class="chip" type="button" onClick=${() => dialog.current.close()}>Done</button>
        </div>
      </form>
    </div>
  </dialog>`
}

function statusText(st) {
  switch (st) {
    case 'online': return 'Online'
    case 'offline': return 'Offline'
    case 'unauthorized': return 'Pair again'
    case 'https': return 'Needs https'
    default: return 'Checking…'
  }
}
