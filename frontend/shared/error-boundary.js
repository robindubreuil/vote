import { showToast } from './ui.js'
import { t } from './i18n.js'

// Module-level guard so a burst of failures doesn't stack a wall of
// toasts. The first failure surfaces immediately; subsequent ones within
// the cooldown are still logged but suppressed in the UI.
let lastToastAt = 0
const TOAST_COOLDOWN_MS = 4000

// Stable no-op teardown returned by repeat `installGlobalErrorHandlers`
// calls. Keeping a single reference lets callers compare teardowns for
// equality (and lets our tests assert the idempotency contract).
const noopTeardown = () => {}

function shouldSurface() {
  const now = Date.now()
  if (now - lastToastAt < TOAST_COOLDOWN_MS) return false
  lastToastAt = now
  return true
}

// Cross-origin classic scripts (loaded without crossorigin) report only
// `"Script error."` with empty filename/lineno — no actionable detail.
// Surfaceing a toast for these just trains users to dismiss everything.
function isOpaqueScriptError(event) {
  return (
    event &&
    event.message === 'Script error.' &&
    (!event.filename || event.filename === '') &&
    (!event.lineno || event.lineno === 0)
  )
}

function surface(message) {
  console.error(message)
  if (shouldSurface()) {
    showToast(t.common.unexpectedError, { type: 'error', duration: 5000 })
  }
}

/**
 * Install window-level error + unhandledrejection handlers.
 *
 * Both handlers log to the console (so dev tools still get the full
 * stack) and surface a single transient toast to the user. The toast is
 * rate-limited so a runaway loop (e.g. a render error firing on every
 * frame) doesn't bury the screen.
 *
 * Idempotent: calling it twice is safe — the second call returns the
 * same no-op teardown. Returns a real teardown function so callers
 * (e.g. tests) can remove the listeners and re-install.
 * @returns {() => void}
 */
export function installGlobalErrorHandlers() {
  if (window.__voteErrorBoundaryInstalled) return noopTeardown
  window.__voteErrorBoundaryInstalled = true

  const onError = (event) => {
    if (isOpaqueScriptError(event)) return
    const detail = event.error || event.message || 'unknown error'
    surface(`[vote] Uncaught error: ${detail}`)
  }
  const onRejection = (event) => {
    const reason = event && event.reason
    // Our websocket-client uses `throw new Error(...)` inside promise
    // chains for reconnect backoff; the WS client itself surfaces those
    // via status callbacks, so swallow AbortError-shaped reasons to
    // avoid double-reporting.
    if (reason && reason.name === 'AbortError') return
    surface(`[vote] Unhandled promise rejection: ${reason || 'unknown'}`)
  }

  window.addEventListener('error', onError)
  window.addEventListener('unhandledrejection', onRejection)

  return () => {
    window.removeEventListener('error', onError)
    window.removeEventListener('unhandledrejection', onRejection)
    delete window.__voteErrorBoundaryInstalled
  }
}

/**
 * Wrap a dynamic `import()` call so a chunk-load failure (network drop,
 // service worker cache miss) surfaces to the user instead of stalling the
 * panel silently. Returns the module namespace on success and rejects with
 * the original error after surfacing a toast.
 * @param {Promise<unknown>} importPromise
 * @param {string} [context]
 * @returns {Promise<unknown>}
 */
export async function guardDynamicImport(importPromise, context = 'dynamic import') {
  try {
    return await importPromise
  } catch (err) {
    console.error(`[vote] Failed to load ${context}:`, err)
    if (shouldSurface()) {
      showToast(t.common.sessionError, { type: 'error', duration: 5000 })
    }
    throw err
  }
}
