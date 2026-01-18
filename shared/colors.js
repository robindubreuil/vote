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
  return COLORS.find(c => c.id === id) || COLORS[0]
}

/**
 * Retrieves the hexadecimal color code by its ID.
 * @param {string} id - The color ID (e.g., 'rouge', 'vert', 'bleu')
 * @returns {string} The hexadecimal color code, or gray (#6b7280) if not found
 */
export function getColorHex(id) {
  return getColorById(id)?.color || '#6b7280'
}

// Re-export escapeHtml from sanitize module for backward compatibility
export { escapeHtml } from './utils/sanitize.js'
