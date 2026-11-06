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
  const [blob, setBlob] = useState({ key: '', url: null })
  // loaded holds the URL that finished loading. Readiness is per URL, so a
  // cached image that loads before an effect runs is never reset to hidden.
  const [loaded, setLoaded] = useState('')
  const [failed, setFailed] = useState('')
  useEffect(() => {
    if (!src?.cached || !auth) return
    let live = true
    cachedURL(src.cached).then((u) => live && setBlob({ key, url: u }), () => {})
    return () => { live = false }
  }, [key])
  const url = src?.url || (src?.cached && blob.key === key ? blob.url : null)
  const show = url && failed !== url
  const ready = show && loaded === url
  const onRef = (el) => {
    if (el?.complete && el.naturalWidth > 0 && loaded !== url) setLoaded(url)
  }
  return html`<div class="art ${cls}">
    ${!ready && html`<div class="art-ph">${Icon.note()}</div>`}
    ${show && html`<img key=${url} ref=${onRef} src=${url} alt="" loading="lazy" decoding="async" referrerpolicy="no-referrer"
      class=${ready ? 'ready' : ''} onLoad=${() => setLoaded(url)} onError=${() => setFailed(url)} />`}
  </div>`
}
