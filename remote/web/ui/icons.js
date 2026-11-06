// Inline icons: 24px grid, 1.75 stroke, currentColor. aria-hidden, so the
// button that holds one carries the label.
import { html } from '../vendor/preact-htm.js'

const svg = (body, filled) => html`<svg viewBox="0 0 24 24" aria-hidden="true" fill=${filled ? 'currentColor' : 'none'}
  stroke=${filled ? 'none' : 'currentColor'} stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round">${body}</svg>`

export const Icon = {
  play: () => svg(html`<path d="M8 5.5v13a1 1 0 0 0 1.5.86l10.6-6.5a1 1 0 0 0 0-1.72L9.5 4.64A1 1 0 0 0 8 5.5Z"/>`, true),
  pause: () => svg(html`<rect x="6" y="4.5" width="4" height="15" rx="1.2"/><rect x="14" y="4.5" width="4" height="15" rx="1.2"/>`, true),
  next: () => svg(html`<path d="M5 6.2v11.6a.8.8 0 0 0 1.25.66L15 12.66a.8.8 0 0 0 0-1.32L6.25 5.54A.8.8 0 0 0 5 6.2Z"/><rect x="16.5" y="5" width="2.5" height="14" rx="1"/>`, true),
  prev: () => svg(html`<path d="M19 6.2v11.6a.8.8 0 0 1-1.25.66L9 12.66a.8.8 0 0 1 0-1.32l8.75-5.8A.8.8 0 0 1 19 6.2Z"/><rect x="5" y="5" width="2.5" height="14" rx="1"/>`, true),
  shuffle: () => svg(html`<path d="M3 7h3.5c2 0 3.2 1 4.3 2.6l2.4 3.8C14.3 15 15.5 17 17.5 17H21"/><path d="M3 17h3.5c1.4 0 2.4-.6 3.2-1.5M13.3 8.5c.9-.9 2-1.5 4.2-1.5H21"/><path d="m18.5 4.5 2.5 2.5-2.5 2.5M18.5 14.5l2.5 2.5-2.5 2.5"/>`),
  repeat: () => svg(html`<path d="M4 11V9.5A3.5 3.5 0 0 1 7.5 6H20"/><path d="m17 3 3 3-3 3"/><path d="M20 13v1.5a3.5 3.5 0 0 1-3.5 3.5H4"/><path d="m7 21-3-3 3-3"/>`),
  now: () => svg(html`<path d="M4 18V10M8 18V6M12 18v-9M16 18V4M20 18v-6"/>`),
  search: () => svg(html`<circle cx="11" cy="11" r="6.5"/><path d="m20 20-4.2-4.2"/>`),
  library: () => svg(html`<path d="M5 4v16M10 4v16M14.5 4.5l4.5 15"/>`),
  queue: () => svg(html`<path d="M4 6h12M4 12h12M4 18h7"/><path d="m16 15.5 4.5 2.5-4.5 2.5Z" fill="currentColor"/>`),
  playNext: () => svg(html`<path d="M4 7h10M4 12h10M4 17h6"/><path d="M18 14v6M15 17h6"/>`),
  check: () => svg(html`<path d="m5 12.5 4.5 4.5L19 7.5"/>`),
  close: () => svg(html`<path d="M6 6l12 12M18 6 6 18"/>`),
  back: () => svg(html`<path d="m14.5 5-7 7 7 7"/>`),
  chevron: () => svg(html`<path d="m9.5 5 7 7-7 7"/>`),
  volLow: () => svg(html`<path d="M11 5 6.5 9H4v6h2.5L11 19Z"/>`),
  volHigh: () => svg(html`<path d="M11 5 6.5 9H4v6h2.5L11 19Z"/><path d="M15 9.5a3.5 3.5 0 0 1 0 5M17.5 7a7 7 0 0 1 0 10"/>`),
  speaker: () => svg(html`<rect x="6" y="3" width="12" height="18" rx="2.5"/><circle cx="12" cy="14" r="3"/><path d="M12 7.5h.01"/>`),
  chevronDown: () => svg(html`<path d="m6.5 9.5 5.5 5.5 5.5-5.5"/>`),
  note: () => svg(html`<path d="M9 18V6l10-2v12"/><circle cx="6.5" cy="18" r="2.5"/><circle cx="16.5" cy="16" r="2.5"/>`),
}
