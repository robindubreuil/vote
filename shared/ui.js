import { VERSION } from './version.js'
import { escapeHtml } from './colors.js'

/**
 * Generates the common footer HTML
 */
export function renderFooterHTML() {
  return `
    <footer class="footer">
      <span class="footer-author">${VERSION.author}</span>
      <span class="footer-separator">•</span>
      <a href="https://opensource.org/licenses/MIT" target="_blank" class="footer-link">Licence MIT</a>
      <span class="footer-separator">•</span>
      <span class="footer-version" title="${escapeHtml(VERSION.fullHash)}">${escapeHtml(VERSION.commitHash)}</span>
      <span class="footer-date">${escapeHtml(VERSION.commitDate)}</span>
    </footer>
  `
}

/**
 * Creates or updates the connection status indicator
 * @param {boolean} connected 
 * @param {HTMLElement} parentElement (optional) if null, appends to body
 * @param {HTMLElement} existingElement (optional) to update existing
 * @returns {HTMLElement} The status element
 */
export function renderConnectionStatus(connected, parentElement = document.body, existingElement = null) {
  let statusEl = existingElement
  
  if (!statusEl) {
    statusEl = document.createElement('div')
    // Check if parentElement is document.body to apply fixed positioning class
    // If it's inside a container, the CSS might need adjustment or we keep using the fixed one
    statusEl.className = 'connection-status'
    statusEl.setAttribute('role', 'status')
    statusEl.setAttribute('aria-live', 'polite')
    parentElement.appendChild(statusEl)
  }

  statusEl.className = `connection-status ${connected ? 'connected' : 'disconnected'}`
  
  // Accessibility text
  const statusText = connected ? 'Connecté' : 'Reconnexion...'
  
  statusEl.innerHTML = `
    <span class="dot" aria-hidden="true"></span>
    <span>${statusText}</span>
  `
  
  return statusEl
}
