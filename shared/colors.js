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

// Helper pour récupérer une couleur par ID
export function getColorById(id) {
  return COLORS.find(c => c.id === id) || COLORS[0]
}

// Helper pour récupérer la couleur hex par ID
export function getColorHex(id) {
  return getColorById(id)?.color || '#6b7280'
}

/**
 * Escape HTML to prevent XSS attacks
 * Use this before inserting user-provided text into innerHTML
 */
export function escapeHtml(text) {
  const div = document.createElement('div')
  div.textContent = text
  return div.innerHTML
}
