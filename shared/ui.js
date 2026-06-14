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

let errorTimeoutId = null

/**
 * Displays an error message in the error-message element.
 * The error will auto-hide after 5 seconds.
 * @param {string} message - The error message to display
 */
export function showError(message) {
  if (message == null) {
    hideError()
    return
  }
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
