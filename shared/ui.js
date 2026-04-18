import { VERSION } from './version.js'
import { escapeHtml } from './colors.js'
import { icons } from './icons.js'

/**
 * Generates the common footer HTML containing author info, license, and version.
 * @returns {string} HTML string for the footer element
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
 * Generates the session code button HTML with connection status
 * @param {string} sessionCode - The session code to display
 * @param {boolean} connected - WebSocket connection status
 * @param {string} title - Tooltip text (default: "Quitter cette session" or "Changer de session")
 * @returns {string} HTML string
 */
export function renderSessionCodeButton(sessionCode, connected = false, title = null) {
  if (!sessionCode) return ''

  const connectionClass = connected ? 'connected' : 'disconnected'
  const defaultTitle = title || 'Quitter cette session'

  return `<button class="session-code ${connectionClass}" id="leaveSessionBtn" data-testid="session-code-btn" title="${defaultTitle}">${escapeHtml(sessionCode)}</button>`
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

let errorTimeoutId = null

/**
 * Displays an error message in the error-message element.
 * The error will auto-hide after 5 seconds.
 * @param {string} message - The error message to display
 */
export function showError(message) {
  const errorEl = document.querySelector('.error-message')
  if (errorEl) {
    if (errorTimeoutId) {
      clearTimeout(errorTimeoutId)
      errorTimeoutId = null
    }
    
    errorEl.textContent = message
    errorEl.style.display = 'block'
    
    errorTimeoutId = setTimeout(() => {
      hideError()
    }, 5000)
  }
}

/**
 * Hides the error message element.
 */
export function hideError() {
  const errorEl = document.querySelector('.error-message')
  if (errorEl) {
    if (errorTimeoutId) {
      clearTimeout(errorTimeoutId)
      errorTimeoutId = null
    }
    errorEl.textContent = ''
    errorEl.style.display = 'none'
  }
}
