/**
 * Colors configuration shared between frontends
 */

export const COLORS = [
  { id: 'rouge', name: 'Rouge', color: '#ef4444' },
  { id: 'vert', name: 'Vert', color: '#22c55e' },
  { id: 'bleu', name: 'Bleu', color: '#3b82f6' },
  { id: 'jaune', name: 'Jaune', color: '#eab308' },
  { id: 'orange', name: 'Orange', color: '#f97316' },
  { id: 'violet', name: 'Violet', color: '#a855f7' },
  { id: 'rose', name: 'Rose', color: '#ec4899' },
  { id: 'gris', name: 'Gris', color: '#6b7280' }
]

/**
 * Retrieves a color configuration by its ID.
 * @param {string} id - The color ID (e.g., 'rouge', 'vert', 'bleu')
 * @returns {{id: string, name: string, color: string}} The color object, or the first color (rouge) if not found
 */
export function getColorById(id) {
  return COLORS.find((c) => c.id === id) || COLORS[0]
}

// Strict hex pattern: only the CSS-valid hex colour lengths (3, 4, 6, 8
// hex digits). Anything else (5/7 digits, named colours, rgb()/hsl()
// expressions, url(...) chains, JS expressions) is rejected. Today
// COLORS is a constant so the helper is a tripwire; the moment a
// custom-palette feature lets user-supplied values reach a
// `style="background-color: ..."` interpolation, this strips the payload
// before it can reach the DOM.
const HEX_COLOR_RE = /^#[0-9a-fA-F]{3}$|^#[0-9a-fA-F]{4}$|^#[0-9a-fA-F]{6}$|^#[0-9a-fA-F]{8}$/
const SAFE_COLOR_FALLBACK = '#666666'

/**
 * Validate a CSS color value for safe interpolation into a `style` attribute.
 * Returns the input when it is a strict hex colour token (3, 4, 6, or 8
 * hex digits — the only valid CSS hex lengths); otherwise returns a
 * neutral fallback. The fallback itself is also validated so a future
 * caller can't poison the helper by passing a non-hex default.
 * @param {unknown} color
 * @param {string} [fallback] - Returned when `color` is rejected. Defaults to a mid-grey. Must itself be a hex colour or the default is used.
 * @returns {string}
 */
export function sanitizeColor(color, fallback = SAFE_COLOR_FALLBACK) {
  if (typeof color === 'string' && HEX_COLOR_RE.test(color)) return color
  return HEX_COLOR_RE.test(fallback) ? fallback : SAFE_COLOR_FALLBACK
}

export { escapeHtml } from './utils/sanitize.js'
