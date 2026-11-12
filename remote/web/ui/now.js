// Now Playing and the mini player.
//
// The scrubber is the signature piece: the seek bar is drawn from the live
// spectrum of the speaker cliamp plays on. The played part of the track
// lights in the accent. When playback stops, the bars settle into a quiet
// silhouette that is fixed for each track, so the timeline never goes flat.
import { html, useEffect, useRef, useState } from '../vendor/preact-htm.js'
import { formatTime, livePosition, providerOf, trackArtist, trackTitle } from '../core/store.js'
import { Art } from './art.js'
import { MachineChip } from './machines.js'
import { Icon } from './icons.js'
import { haptic, reducedMotion, Swipe, useLatest, useRemote, useTick } from './hooks.js'
import { toast } from './toast.js'

const BAR_W = 3
const BAR_GAP = 2
const FRAME_MS = 50 // spectrum.get rate while playing: 20 per second

// silhouette returns the idle shape of a track: n bar heights from 0.08 to
// 0.3, and for each bar the spectrum band that drives it. Both are seeded by
// key, so a track keeps its shape across renders and devices.
function silhouette(key, n) {
  let h = 2166136261
  for (let i = 0; i < key.length; i++) h = Math.imul(h ^ key.charCodeAt(i), 16777619)
  const idle = new Float32Array(n)
  const band = new Float32Array(n) // 0..1: where in the spectrum the bar listens
  for (let i = 0; i < n; i++) {
    h = Math.imul(h ^ (h >>> 15), 2246822507) >>> 0
    const r = (h % 1000) / 1000
    idle[i] = 0.08 + 0.22 * (0.55 * r + 0.45 * Math.abs(Math.sin(i * 0.37 + r)))
    // Squared, so most bars listen to the low bands, where music has its
    // energy; a uniform pick leaves most bars on near-silent high bands.
    const b = ((h >>> 10) % 1000) / 1000
    band[i] = b * b
  }
  return { idle, band }
}

// barLevel spreads the spectrum over the whole timeline. Mapped left to
// right, cliamp's few bands would pile up at the start, since most music
// lives in the low bands. Instead each bar follows one band picked by the
// silhouette and swells with the overall energy, so the shape of the track
// stays and the whole bar breathes with the music.
function barLevel(idle, bandPos, bands, energy) {
  if (!bands?.length) return idle
  const v = bands[Math.min(bands.length - 1, Math.floor(bandPos * bands.length))] || 0
  return Math.min(1, idle * (1 + 3 * energy) + 0.65 * v)
}

function Scrubber({ remote, state }) {
  const snap = state.snapshot
  const duration = snap?.duration || 0
  const seekable = !!snap?.seekable && duration > 0
  const wrap = useRef(null)
  const canvas = useRef(null)
  const drag = useRef(null)
  const wake = useRef(() => {})
  const [dragPos, setDragPos] = useState(null)
  const accent = getComputedStyle(document.documentElement).getPropertyValue('--accent').trim() || '#f2b33d'
  const latest = useLatest({ state, accent })
  useTick(500, dragPos === null)

  useEffect(() => {
    const el = canvas.current
    const ctx = el.getContext('2d')
    let raf = 0
    let n = 0
    let levels = new Float32Array(0)
    let shape = { idle: new Float32Array(0), band: new Uint8Array(0) }
    let idleKey = ''
    let bands = null
    let peak = 0.04
    let inflight = false
    let lastFetch = 0
    const reduce = reducedMotion()

    const resize = () => {
      const dpr = Math.min(globalThis.devicePixelRatio || 1, 3)
      const w = el.clientWidth
      el.width = Math.round(w * dpr)
      el.height = Math.round(el.clientHeight * dpr)
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0)
      n = Math.max(8, Math.floor((w + BAR_GAP) / (BAR_W + BAR_GAP)))
      levels = new Float32Array(n)
      idleKey = ''
    }
    const ro = new ResizeObserver(resize)
    ro.observe(el)
    resize()

    let idleTimer = 0
    let lastSig = ''
    const frame = (t) => {
      raf = 0
      idleTimer = 0
      const { state: s, accent } = latest.current
      const sn = s.snapshot
      const playing = sn?.state === 'playing' && s.status === 'open'
      const key = (sn?.track?.path || '') + n
      if (key !== idleKey) {
        shape = silhouette(key, n)
        idleKey = key
        if (reduce) levels.set(shape.idle)
      }
      if (playing && !reduce && !document.hidden && !inflight && t - lastFetch > FRAME_MS) {
        inflight = true
        lastFetch = t
        remote.spectrum().then((b) => {
          // The bands follow the volume, so normalize by a slowly falling
          // peak: the bars move the same at -20 dB as at 0 dB.
          const top = Math.max(...b, 0)
          peak = Math.max(top, peak * 0.985, 0.04)
          bands = b.map((v) => Math.min(1, (v || 0) / peak))
        }, () => { bands = null }).finally(() => { inflight = false })
      }
      if (!playing) bands = null

      const w = el.clientWidth
      const h = el.clientHeight
      const dur = sn?.duration || 0
      const pos = drag.current ? drag.current.pos : livePosition(s)
      const progress = dur > 0 ? pos / dur : 0
      const step = w / n
      let energy = 0
      if (bands?.length) energy = Math.min(1, bands.reduce((a, b) => a + (b || 0), 0) / bands.length)
      let moving = false
      for (let i = 0; i < n; i++) {
        const target = barLevel(shape.idle[i], shape.band[i], bands, energy)
        const delta = reduce ? 0 : (target - levels[i]) * (target > levels[i] ? 0.55 : 0.12)
        if (Math.abs(delta) > 0.0005) moving = true
        levels[i] += delta
      }
      // Paused and settled: draw only when something on it changed, and
      // check four times a second instead of every frame, to spare the
      // battery.
      const sig = [Math.floor(progress * n), accent, w, h, idleKey, !!drag.current].join('|')
      const idle = !playing && !drag.current && !moving
      if (!idle || sig !== lastSig) {
        lastSig = sig
        ctx.clearRect(0, 0, w, h)
        for (let i = 0; i < n; i++) {
          const bh = Math.max(2, levels[i] * h)
          const x = i * step + (step - BAR_W) / 2
          ctx.fillStyle = (i + 0.5) / n <= progress ? accent : 'rgba(255,255,255,0.16)'
          if (ctx.roundRect) {
            ctx.beginPath()
            ctx.roundRect(x, (h - bh) / 2, BAR_W, bh, 1.5)
            ctx.fill()
          } else {
            ctx.fillRect(x, (h - bh) / 2, BAR_W, bh) // Safari before 16
          }
        }
        if (drag.current) {
          ctx.fillStyle = accent
          ctx.fillRect(Math.round(progress * w) - 0.5, 0, 1, h)
        }
      }
      if (idle) idleTimer = setTimeout(() => { raf = requestAnimationFrame(frame) }, 250)
      else raf = requestAnimationFrame(frame)
    }
    raf = requestAnimationFrame(frame)
    // A drag starts while the loop sleeps; wake it for the first frame.
    wake.current = () => {
      if (raf || !idleTimer) return
      clearTimeout(idleTimer)
      idleTimer = 0
      raf = requestAnimationFrame(frame)
    }
    return () => {
      cancelAnimationFrame(raf)
      clearTimeout(idleTimer)
      ro.disconnect()
    }
  }, [remote])

  const posAt = (e) => {
    const r = wrap.current.getBoundingClientRect()
    return Math.min(Math.max((e.clientX - r.left) / r.width, 0), 1) * duration
  }
  const onDown = (e) => {
    if (!seekable) return
    try {
      e.currentTarget.setPointerCapture(e.pointerId)
    } catch {}
    drag.current = { pos: posAt(e) }
    setDragPos(drag.current.pos)
    wake.current()
  }
  const onMove = (e) => {
    if (!drag.current) return
    drag.current.pos = posAt(e)
    setDragPos(drag.current.pos)
  }
  const onUp = () => {
    if (!drag.current) return
    const pos = drag.current.pos
    drag.current = null
    setDragPos(null)
    remote.seek(pos).catch(() => {})
  }
  const onKey = (e) => {
    if (!seekable) return
    const cur = livePosition(state)
    const to = { ArrowLeft: cur - 5, ArrowRight: cur + 5, Home: 0, End: duration - 1 }[e.key]
    if (to === undefined) return
    e.preventDefault()
    remote.seek(Math.min(Math.max(to, 0), duration)).catch(() => {})
  }

  const pos = dragPos ?? livePosition(state)
  const remaining = Math.max(duration - pos, 0)
  return html`<div class="scrub" ref=${wrap} role="slider" tabindex=${seekable ? 0 : -1}
      aria-label="Seek" aria-disabled=${seekable ? 'false' : 'true'}
      aria-valuemin="0" aria-valuemax=${Math.round(duration)} aria-valuenow=${Math.round(pos)}
      aria-valuetext=${`${formatTime(pos)} of ${formatTime(duration)}`}
      data-dragging=${dragPos !== null ? '' : undefined}
      onPointerDown=${onDown} onPointerMove=${onMove} onPointerUp=${onUp} onPointerCancel=${onUp} onKeyDown=${onKey}>
    <canvas ref=${canvas}></canvas>
    <div class="scrub-times mono" aria-hidden="true">
      <span>${formatTime(pos)}</span>
      <span>${duration > 0 ? '-' + formatTime(remaining) : snap?.track?.stream ? 'LIVE' : ''}</span>
    </div>
  </div>`
}

function Volume({ remote, snap }) {
  const db = snap.volume ?? 0
  const [local, setLocal] = useState(null)
  const timer = useRef(0)
  const release = useRef(0)
  useEffect(() => () => {
    clearTimeout(timer.current)
    clearTimeout(release.current)
  }, [])
  const value = local ?? db
  // volume_min can go below the slider's -50 dB; widen the range to fit.
  const min = Math.min(-50, Math.floor(value))
  const pct = ((value - min) / (6 - min)) * 100
  const send = (v) => remote.volume(v).catch(() => {})
  const onInput = (e) => {
    const v = Number(e.currentTarget.value)
    clearTimeout(release.current) // a new drag keeps its own thumb position
    setLocal(v)
    clearTimeout(timer.current)
    timer.current = setTimeout(() => send(v), 120)
  }
  const onChange = (e) => {
    clearTimeout(timer.current)
    send(Number(e.currentTarget.value))
    // Hold the thumb where the finger left it until the snapshot catches up.
    clearTimeout(release.current)
    release.current = setTimeout(() => setLocal(null), 400)
  }
  return html`<div class="volume">
    ${Icon.volLow()}
    <input class="range" type="range" min=${min} max="6" step="1" value=${value} style=${`--pct:${pct}%`}
      aria-label="Volume" aria-valuetext=${`${value > 0 ? '+' : ''}${value} dB`} onInput=${onInput} onChange=${onChange} />
    ${Icon.volHigh()}
  </div>`
}

export function sourceLabel(snap, providers) {
  const list = snap?.playlist || ''
  if (!list) return ''
  const p = providers.find((x) => x.key === list.split(':')[0])
  return p ? p.name : list
}

// shuffleMode reads the snapshot as one of 'off', 'on' and 'smart'.
const shuffleMode = (snap) => (snap.smart_shuffle ? 'smart' : snap.shuffle ? 'on' : 'off')
const SHUFFLE_LABEL = { off: 'Shuffle off', on: 'Shuffle on', smart: 'Smart shuffle on' }

// cycleShuffle steps off, shuffle, smart shuffle and back. Smart shuffle
// only joins the cycle when the playing queue's provider can recommend.
function cycleShuffle(remote, snap) {
  const mode = shuffleMode(snap)
  const next = mode === 'off' ? 'on' : mode === 'on' && snap.smart_available ? 'smart' : 'off'
  haptic()
  remote.setShuffle(next).then(() => toast(SHUFFLE_LABEL[next]), (err) => toast(err.message, 'error'))
}

export function NowPlaying({ remote, go, machines, openArtist }) {
  const state = useRemote(remote)
  const snap = state.snapshot
  const track = snap?.track
  // Before the first snapshot nothing is known; an empty state would flash.
  const chip = machines && html`<div class="now-machine"><${MachineChip} machines=${machines} /></div>`
  if (!snap) return html`<div class="now">${chip}</div>`
  if (!track) {
    return html`<div class="now">${chip}<div class="now-empty">
      <div class="art now-art" style="width:120px;margin:0 auto"><div class="art-ph">${Icon.note()}</div></div>
      <p>Nothing playing</p>
      <p>Search Spotify or YouTube to start something.</p>
      <p style="margin-top:20px"><button class="pill" onClick=${() => go('search')}>${Icon.search()} Search</button></p>
    </div></div>`
  }
  const playing = snap.state === 'playing'
  const act = (fn) => () => {
    haptic()
    fn().catch(() => {})
  }
  const source = sourceLabel(snap, state.providers)
  const trackProvider = providerOf(track, state.providers)
  const artistProvider = trackProvider?.artist_profiles ? trackProvider : null
  return html`<div class="now">
    ${chip}
    <div class="now-source">${source && html`<span class="eyebrow trunc">Playing from ${source}</span>`}</div>
    <${Swipe} class="now-swipe" fly onLeft=${act(remote.next)} onRight=${act(remote.prev)}>
    <${Art} track=${track} class="now-art" />
    <div class="now-meta"><div>
      <div class="now-title trunc">${trackTitle(track)}</div>
      ${artistProvider && openArtist
        ? html`<button class="now-artist artist-link" onClick=${() => openArtist(artistProvider, { track })}>
            <span class="trunc">${trackArtist(track)}</span>${Icon.chevron()}</button>`
        : html`<div class="now-artist trunc">${trackArtist(track) || ' '}</div>`}
    </div></div>
    </${Swipe}>
    <${Scrubber} remote=${remote} state=${state} />
    <div class="transport">
      <button class="icon-btn" aria-label=${SHUFFLE_LABEL[shuffleMode(snap)]} aria-pressed=${snap.shuffle ? 'true' : 'false'}
        onClick=${() => cycleShuffle(remote, snap)}>${shuffleMode(snap) === 'smart' ? Icon.smartShuffle() : Icon.shuffle()}</button>
      <button class="icon-btn" aria-label="Previous" onClick=${act(remote.prev)}>${Icon.prev()}</button>
      <button class="play" aria-label=${playing ? 'Pause' : 'Play'} onClick=${act(remote.toggle)}>${playing ? Icon.pause() : Icon.play()}</button>
      <button class="icon-btn" aria-label="Next" onClick=${act(remote.next)}>${Icon.next()}</button>
      <button class=${'icon-btn' + (snap.repeat === 'one' ? ' repeat-one' : '')} aria-label=${'Repeat: ' + (snap.repeat || 'off')}
        aria-pressed=${snap.repeat && snap.repeat !== 'off' ? 'true' : 'false'} onClick=${act(remote.repeat)}>${Icon.repeat()}</button>
    </div>
    ${remote.has('volume') && html`<${Volume} remote=${remote} snap=${snap} />`}
    ${snap.stream_error && html`<p class="stream-error" role="status">${snap.stream_error}</p>`}
  </div>`
}

export function MiniPlayer({ remote, go }) {
  const state = useRemote(remote)
  const snap = state.snapshot
  useTick(1000, snap?.state === 'playing')
  if (!snap?.track) return null
  const playing = snap.state === 'playing'
  const pct = snap.duration > 0 ? livePosition(state) / snap.duration : 0
  return html`<div class="mini">
    <${Swipe} class="mini-swipe" fly onLeft=${() => { haptic(); remote.next().catch(() => {}) }} onRight=${() => { haptic(); remote.prev().catch(() => {}) }}>
    <button class="mini-open" onClick=${() => go('now')} aria-label=${'Now playing: ' + trackTitle(snap.track)}>
      <${Art} track=${snap.track} />
      <div class="mini-text">
        <div class="trunc">${trackTitle(snap.track)}</div>
        <div class="trunc">${trackArtist(snap.track)}</div>
      </div>
    </button>
    </${Swipe}>
    <button class="icon-btn" aria-label=${playing ? 'Pause' : 'Play'} onClick=${() => { haptic(); remote.toggle().catch(() => {}) }}>
      ${playing ? Icon.pause() : Icon.play()}
    </button>
    <div class="mini-bar" aria-hidden="true"><i style=${`transform:scaleX(${pct})`}></i></div>
  </div>`
}
