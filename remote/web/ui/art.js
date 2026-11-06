// Album art. Provider art loads straight from its https URL. Art that
// cliamp cached from a local file comes from the gateway, which wants the
// token in a header, so it loads through fetch into a blob URL.
import { html, useEffect, useState } from '../vendor/preact-htm.js'
import { artSource } from '../core/store.js'
import { Icon } from './icons.js'

const blobs = new Map() // cache name -> Promise<blob URL>
let auth = null

export function setArtAuth(base, token) {
  auth = { base, token }
}

function cachedURL(name) {
  if (!blobs.has(name)) {
    const p = fetch(`${auth.base}/art/${name}`, { headers: { Authorization: 'Bearer ' + auth.token } })
      .then((r) => (r.ok ? r.blob() : Promise.reject(new Error(String(r.status)))))
      .then((b) => URL.createObjectURL(b))
    p.catch(() => blobs.delete(name))
    blobs.set(name, p)
    if (blobs.size > 300) blobs.delete(blobs.keys().next().value)
  }
  return blobs.get(name)
}

export function Art({ track, class: cls = '' }) {
  const src = artSource(track)
  const key = src?.url || src?.cached || ''
  const [url, setURL] = useState(src?.url || null)
  const [ready, setReady] = useState(false)
  useEffect(() => {
    setReady(false)
    if (!src) return setURL(null)
    if (src.url) return setURL(src.url)
    let live = true
    setURL(null)
    if (auth) cachedURL(src.cached).then((u) => live && setURL(u), () => {})
    return () => { live = false }
  }, [key])
  return html`<div class="art ${cls}">
    ${!ready && html`<div class="art-ph">${Icon.note()}</div>`}
    ${url && html`<img src=${url} alt="" loading="lazy" decoding="async" referrerpolicy="no-referrer"
      class=${ready ? 'ready' : ''} onLoad=${() => setReady(true)} onError=${() => setURL(null)} />`}
  </div>`
}
