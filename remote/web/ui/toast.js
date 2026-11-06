// One toast at a time, announced politely. Errors that belong to a row show
// here because the row has no room to explain them.
import { html, useEffect, useState } from '../vendor/preact-htm.js'

let push = () => {}

export function toast(text, kind = 'info') {
  push({ text, kind, id: Date.now() })
}

export function Toasts() {
  const [t, setT] = useState(null)
  useEffect(() => {
    push = setT
    return () => { push = () => {} }
  }, [])
  useEffect(() => {
    if (!t) return
    const id = setTimeout(() => setT(null), t.kind === 'error' ? 4000 : 2400)
    return () => clearTimeout(id)
  }, [t])
  return html`<div class="toast-wrap" aria-live="polite">
    ${t && html`<div class="toast" data-kind=${t.kind} key=${t.id}>${t.text}</div>`}
  </div>`
}
