import { html, useEffect, useState, useRef } from '../vendor/preact-htm.js'

// useRemote re-renders on every store change and returns the state.
export function useRemote(remote) {
  const [state, setState] = useState(remote.store.get())
  useEffect(() => remote.store.subscribe(setState), [remote])
  return state
}

// useTick re-renders every ms while on is true, for clocks.
export function useTick(ms, on = true) {
  const [, setN] = useState(0)
  useEffect(() => {
    if (!on) return
    const id = setInterval(() => setN((n) => n + 1), ms)
    return () => clearInterval(id)
  }, [ms, on])
}

// useLatest keeps a ref to the latest value for callbacks and frame loops.
export function useLatest(value) {
  const ref = useRef(value)
  ref.current = value
  return ref
}

export const reducedMotion = () => globalThis.matchMedia?.('(prefers-reduced-motion: reduce)').matches

export function haptic() {
  navigator.vibrate?.(8)
}

// Swipe makes its box follow a horizontal drag. Past the threshold it runs
// onLeft or onRight, for the sides that have a handler; a shorter drag
// springs back. Vertical drags scroll as usual, and the click that ends a
// swipe is swallowed so a row does not also play. With fly, a committed
// swipe slides the box out and the next content in, for a track change.
export function Swipe({ onLeft, onRight, fly = false, threshold = 72, children, ...rest }) {
  const box = useRef(null)
  const latest = useLatest({ onLeft, onRight, fly, threshold })
  useEffect(() => {
    const el = box.current
    let g = null // {x, y, id, active, passed}
    let swiped = false
    const reduce = reducedMotion()
    const side = (dx) => (dx < 0 ? latest.current.onLeft : latest.current.onRight)
    const reset = () => {
      el.style.transition = reduce ? '' : 'transform 180ms cubic-bezier(.2,.8,.2,1), opacity 180ms'
      el.style.transform = ''
      el.style.opacity = ''
    }
    const down = (e) => {
      if (e.pointerType === 'mouse' && e.button !== 0) return
      g = { x: e.clientX, y: e.clientY, id: e.pointerId, active: false, passed: false }
    }
    const move = (e) => {
      if (!g || e.pointerId !== g.id) return
      const dx = e.clientX - g.x
      const dy = e.clientY - g.y
      if (!g.active) {
        if (Math.abs(dy) > 10 && Math.abs(dy) > Math.abs(dx)) return void (g = null)
        if (Math.abs(dx) < 10 || Math.abs(dx) < Math.abs(dy) * 1.5) return
        if (!side(dx)) return void (g = null)
        g.active = true
        try { el.setPointerCapture(e.pointerId) } catch {}
        el.style.transition = 'none'
      }
      // The box trails the finger and stiffens past the threshold.
      const t = latest.current.threshold
      const over = Math.max(Math.abs(dx) - t, 0)
      const pull = Math.sign(dx) * (Math.min(Math.abs(dx), t) + over * 0.3)
      if (!side(dx)) return reset()
      el.style.transform = `translateX(${pull}px)`
      el.style.opacity = latest.current.fly ? String(1 - Math.min(Math.abs(pull) / (t * 3), 0.4)) : ''
      const passed = Math.abs(dx) >= t
      if (passed !== g.passed) {
        g.passed = passed
        if (passed) haptic()
      }
      g.dx = dx
    }
    const up = () => {
      if (!g) return
      const { active, passed, dx } = g
      g = null
      if (!active) return
      swiped = true
      setTimeout(() => { swiped = false }, 0)
      const run = passed && side(dx)
      if (!run) return reset()
      const dir = Math.sign(dx)
      if (latest.current.fly && !reduce && el.animate) {
        const w = el.clientWidth
        el.style.transition = 'none'
        el.style.transform = ''
        el.style.opacity = ''
        el.animate([{ transform: `translateX(${dx}px)`, opacity: 1 }, { transform: `translateX(${dir * w * 0.4}px)`, opacity: 0 }], { duration: 120, easing: 'ease-in' })
          .finished.then(() => {
            side(dx)()
            el.animate([{ transform: `translateX(${-dir * w * 0.25}px)`, opacity: 0 }, { transform: 'none', opacity: 1 }], { duration: 220, easing: 'cubic-bezier(.2,.8,.2,1)' })
          }, () => {})
        return
      }
      side(dx)()
      reset()
    }
    const click = (e) => {
      if (!swiped) return
      e.preventDefault()
      e.stopPropagation()
    }
    el.addEventListener('pointerdown', down)
    el.addEventListener('pointermove', move)
    el.addEventListener('pointerup', up)
    el.addEventListener('pointercancel', up)
    el.addEventListener('click', click, true)
    return () => {
      el.removeEventListener('pointerdown', down)
      el.removeEventListener('pointermove', move)
      el.removeEventListener('pointerup', up)
      el.removeEventListener('pointercancel', up)
      el.removeEventListener('click', click, true)
    }
  }, [])
  return html`<div ref=${box} data-swipe="" ...${rest}>${children}</div>`
}
