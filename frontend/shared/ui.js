import { VERSION } from './version.js'
import { escapeHtml } from './colors.js'
import { t } from './i18n.js'

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

/* ============================================
   In-app confirm dialog
   Replaces the jarring native window.confirm() with a themed modal that
   matches the rest of the UI. Returns a Promise<boolean>.
   ============================================ */
let confirmDialogEl = null
let confirmResolve = null
let confirmLastFocus = null

function ensureConfirmDialog() {
  if (confirmDialogEl && document.body.contains(confirmDialogEl)) return confirmDialogEl

  confirmDialogEl = document.createElement('div')
  confirmDialogEl.className = 'confirm-overlay'
  confirmDialogEl.setAttribute('role', 'dialog')
  confirmDialogEl.setAttribute('aria-modal', 'true')
  confirmDialogEl.setAttribute('aria-labelledby', 'confirmDialogTitle')
  confirmDialogEl.innerHTML = `
    <div class="confirm-dialog">
      <h2 class="confirm-title" id="confirmDialogTitle"></h2>
      <p class="confirm-message" id="confirmDialogMessage"></p>
      <div class="confirm-actions">
        <button type="button" class="btn btn-secondary confirm-cancel" data-confirm="cancel"></button>
        <button type="button" class="btn btn-danger confirm-ok" data-confirm="ok"></button>
      </div>
    </div>
  `
  document.body.appendChild(confirmDialogEl)

  confirmDialogEl.addEventListener('click', (e) => {
    const action = e.target?.dataset?.confirm
    if (action) {
      resolveConfirm(action === 'ok')
    } else if (e.target === confirmDialogEl) {
      // Click on backdrop = cancel
      resolveConfirm(false)
    }
  })

  // Key handler lives on document so Escape/Enter work regardless of where
  // focus currently sits (the trigger button may still be focused before
  // requestAnimationFrame moves focus into the dialog). stopImmediatePropagation
  // prevents other app-level Escape handlers (e.g. formateur "Esc to leave")
  // from firing while the dialog is open.
  document.addEventListener('keydown', (e) => {
    if (!confirmResolve) return
    if (e.key === 'Escape') {
      e.preventDefault()
      e.stopImmediatePropagation()
      resolveConfirm(false)
    } else if (e.key === 'Enter') {
      e.preventDefault()
      e.stopImmediatePropagation()
      resolveConfirm(true)
    } else if (e.key === 'Tab') {
      const focusable = confirmDialogEl.querySelectorAll('button')
      if (focusable.length === 0) return
      const first = focusable[0]
      const last = focusable[focusable.length - 1]
      if (e.shiftKey && document.activeElement === first) {
        e.preventDefault()
        last.focus()
      } else if (!e.shiftKey && document.activeElement === last) {
        e.preventDefault()
        first.focus()
      }
    }
  })

  return confirmDialogEl
}

function resolveConfirm(value) {
  if (!confirmResolve) return
  const resolve = confirmResolve
  confirmResolve = null
  const dialog = confirmDialogEl
  if (dialog) {
    dialog.classList.remove('open')
    document.body.classList.remove('confirm-lock')
  }
  if (confirmLastFocus && typeof confirmLastFocus.focus === 'function') {
    confirmLastFocus.focus()
  }
  confirmLastFocus = null
  resolve(value)
}

/**
 * Show a themed confirmation dialog and resolve with the user's choice.
 *
 * @param {{ title?: string, message: string, confirmLabel?: string, cancelLabel?: string, danger?: boolean }} opts
 * @returns {Promise<boolean>} true if the user confirmed, false otherwise
 */
export function showConfirmDialog(opts) {
  const { title, message, confirmLabel, cancelLabel, danger = true } = opts || {}

  const dialog = ensureConfirmDialog()
  dialog.querySelector('.confirm-title').textContent = title || ''
  dialog.querySelector('.confirm-title').style.display = title ? '' : 'none'
  dialog.querySelector('.confirm-message').textContent = message

  const okBtn = dialog.querySelector('.confirm-ok')
  const cancelBtn = dialog.querySelector('.confirm-cancel')
  okBtn.textContent = confirmLabel || t.common.confirm || 'Confirmer'
  cancelBtn.textContent = cancelLabel || t.common.cancel
  okBtn.className = `btn ${danger ? 'btn-danger' : 'btn-primary'} confirm-ok`

  confirmLastFocus = document.activeElement

  return new Promise((resolve) => {
    confirmResolve = resolve
    dialog.classList.add('open')
    document.body.classList.add('confirm-lock')
    // Focus the confirm button so Enter works immediately; Escape still cancels.
    requestAnimationFrame(() => okBtn.focus())
  })
}

/* ============================================
   Transient toast (success / info)
   ============================================ */
let toastContainer = null
const activeToasts = new Map() // message -> timeout id

function ensureToastContainer() {
  if (toastContainer && document.body.contains(toastContainer)) return toastContainer
  toastContainer = document.createElement('div')
  toastContainer.className = 'toast-container'
  toastContainer.setAttribute('role', 'status')
  toastContainer.setAttribute('aria-live', 'polite')
  document.body.appendChild(toastContainer)
  return toastContainer
}

/**
 * Show a transient toast message. Auto-dismisses after `duration` ms.
 * Reuses an existing toast with the same message if present (no duplicates).
 * @param {string} message
 * @param {{ duration?: number, type?: 'success' | 'info' | 'error' }} opts
 */
export function showToast(message, opts = {}) {
  if (!message) return
  const { duration = 2500, type = 'success' } = opts
  const container = ensureToastContainer()

  // Reuse existing toast with the same message: reset its timer and update
  // the type class. Avoids leaving a stale DOM node when the same message
  // fires twice in quick succession.
  const existing = container.querySelector(`[data-toast-message="${cssEscape(message)}"]`)
  if (existing) {
    if (activeToasts.has(message)) clearTimeout(activeToasts.get(message))
    existing.className = `toast toast--${type} toast--visible`
  } else {
    const toast = document.createElement('div')
    toast.className = `toast toast--${type}`
    toast.dataset.toastMessage = message
    toast.textContent = message
    container.appendChild(toast)
    requestAnimationFrame(() => toast.classList.add('toast--visible'))
  }

  const timeoutId = setTimeout(() => {
    const node = container.querySelector(`[data-toast-message="${cssEscape(message)}"]`)
    if (node) {
      node.classList.remove('toast--visible')
      setTimeout(() => {
        node.remove()
        activeToasts.delete(message)
      }, 200)
    } else {
      activeToasts.delete(message)
    }
  }, duration)
  activeToasts.set(message, timeoutId)
}

// CSS.escape with a fallback for non-attribute-safe characters.
function cssEscape(s) {
  if (typeof CSS !== 'undefined' && typeof CSS.escape === 'function') return CSS.escape(s)
  return String(s).replace(/["\\\n\r\t]/g, '\\$&')
}
