import { useEffect, useState, useRef } from '../vendor/preact-htm.js'

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
