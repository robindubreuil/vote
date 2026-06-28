// Service worker registration with an update-available toast.
// Production only — Vite HMR and a caching SW fight each other in dev.

const UPDATE_TOAST_ID = 'pwa-update-toast'

let updateAvailable = false

export function initPWA() {
  if (!('serviceWorker' in navigator)) return
  // import.meta.env.PROD is statically replaced by Vite; in dev this branch
  // is dead-code-eliminated so the SW file is never even fetched.
  if (!import.meta.env.PROD) return

  const register = () => {
    navigator.serviceWorker
      .register('/sw.js', { scope: '/' })
      .then((reg) => {
        // New SW took over after skipWaiting — reload once so the page picks
        // up the new HTML/JS.
        let refreshing = false
        navigator.serviceWorker.addEventListener('controllerchange', () => {
          if (refreshing) return
          refreshing = true
          window.location.reload()
        })

        // Poll for updates every 60 minutes. Browsers also auto-check on
        // navigation but this catches long-running classroom sessions where
        // the trainer never closes the tab.
        setInterval(() => reg.update().catch(() => {}), 60 * 60 * 1000)

        // The initial registration may already have a waiting worker (e.g.
        // we just deployed and the page reloaded). Show the toast right away.
        if (reg.waiting) showUpdateToast(reg)
        reg.addEventListener('updatefound', () => {
          const next = reg.installing
          if (!next) return
          next.addEventListener('statechange', () => {
            if (next.state === 'installed' && navigator.serviceWorker.controller) {
              showUpdateToast(reg)
            }
          })
        })
      })
      .catch((err) => console.warn('SW registration failed:', err))
  }

  if (document.readyState === 'complete') register()
  else window.addEventListener('load', register)

  // Offline indicator: brief toast when the network drops mid-session.
  window.addEventListener('offline', () => {
    updateAvailable = true
    showOfflineToast()
  })
  window.addEventListener('online', () => hideOfflineToast())
}

function showUpdateToast(registration) {
  if (document.getElementById(UPDATE_TOAST_ID)) return
  const toast = document.createElement('div')
  toast.id = UPDATE_TOAST_ID
  toast.className = 'pwa-toast pwa-toast--update'
  toast.setAttribute('role', 'status')
  toast.innerHTML = `
    <span class="pwa-toast-text">Une nouvelle version est disponible.</span>
    <button type="button" class="pwa-toast-action">Recharger</button>
    <button type="button" class="pwa-toast-close" aria-label="Fermer">×</button>
  `
  document.body.appendChild(toast)

  toast.querySelector('.pwa-toast-action').addEventListener('click', () => {
    const waiting = registration.waiting
    if (waiting) {
      waiting.postMessage('SKIP_WAITING')
    } else {
      window.location.reload()
    }
  })
  toast.querySelector('.pwa-toast-close').addEventListener('click', () => toast.remove())
}

const OFFLINE_TOAST_ID = 'pwa-offline-toast'

function showOfflineToast() {
  if (document.getElementById(OFFLINE_TOAST_ID)) return
  const toast = document.createElement('div')
  toast.id = OFFLINE_TOAST_ID
  toast.className = 'pwa-toast pwa-toast--offline'
  toast.setAttribute('role', 'alert')
  toast.innerHTML = `<span class="pwa-toast-text">Hors ligne — les votes ne sont pas reçus.</span>`
  document.body.appendChild(toast)
}

function hideOfflineToast() {
  document.getElementById(OFFLINE_TOAST_ID)?.remove()
}

export const _test = { updateAvailable }
