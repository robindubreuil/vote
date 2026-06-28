import './style.css'
import QRCode from 'qrcode'
import { t } from '@shared/i18n.js'
import { vote } from '@shared/icons.js'
import { renderFooterHTML } from '@shared/ui.js'
import { createSessionSubscriber } from '@shared/session-sync.js'
import { buildJoinURL } from './connection-aid-url.js'

const QR_DARK = '#0f172a'
const QR_LIGHT = '#ffffff'

/**
 * Copy text to clipboard with a graceful fallback for non-secure contexts
 * (where navigator.clipboard may be unavailable).
 * @param {string} text
 */
async function copyToClipboard(text) {
  try {
    if (navigator.clipboard && window.isSecureContext) {
      await navigator.clipboard.writeText(text)
      return true
    }
  } catch {
    /* fall through to legacy path */
  }

  const textarea = document.createElement('textarea')
  textarea.value = text
  textarea.style.position = 'fixed'
  textarea.style.opacity = '0'
  document.body.appendChild(textarea)
  textarea.select()
  let ok
  try {
    ok = Boolean(document.execCommand('copy'))
  } catch {
    ok = false
  }
  document.body.removeChild(textarea)
  return ok
}

/**
 * Render the static HTML skeleton of the aid view.
 * @param {string} sessionCode
 * @param {string} joinURL
 * @returns {string}
 */
function renderSkeleton(sessionCode, joinURL) {
  return `
    <div class="aid-page">
      <header class="aid-header">
        <h1 class="aid-title">${vote(' class="icon icon-lg aid-title-icon"')} ${t.formateur.connectionAid}</h1>
        <button type="button" class="aid-fullscreen-btn" id="aidFullscreenBtn" title="${t.formateur.fullscreen}" aria-label="${t.formateur.fullscreen}"></button>
      </header>

      <main class="aid-main">
        <section class="aid-qr-column">
          <p class="aid-headline">${t.formateur.scanToJoin}</p>
          <div class="aid-qr-wrap">
            <div class="aid-qr" id="aidQr" role="img" aria-label="${t.formateur.qrAriaLabel}">
              <div class="aid-qr-spinner"></div>
            </div>
          </div>
        </section>

        <section class="aid-info-column">
          <p class="aid-code-label">${t.formateur.sessionCodeLabel}</p>
          <div class="aid-code" id="aidCode" aria-label="${t.common.sessionCode}">${sessionCode}</div>

          <div class="aid-divider" role="separator"></div>

          <p class="aid-url-label">${t.formateur.orManualUrl}</p>
          <div class="aid-url-row">
            <a href="${joinURL}" class="aid-url" id="aidUrl" target="_blank" rel="noopener">${joinURL}</a>
            <button type="button" class="aid-copy-btn" id="aidCopyBtn" title="${t.formateur.copyUrl}" aria-label="${t.formateur.copyUrl}"></button>
          </div>

          <div class="aid-count-row" aria-live="polite">
            <span class="aid-count-dot" id="aidCountDot" aria-hidden="true"></span>
            <span class="aid-count-text" id="aidCountText">${t.formateur.connectedUnknown}</span>
          </div>
        </section>
      </main>

      ${renderFooterHTML()}
    </div>
  `
}

/**
 * Generate the QR code SVG into the #aidQr element.
 * @param {string} url
 */
async function renderQR(url) {
  const target = document.getElementById('aidQr')
  if (!target) return

  try {
    const svg = await QRCode.toString(url, {
      type: 'svg',
      errorCorrectionLevel: 'H',
      margin: 2,
      color: { dark: QR_DARK, light: QR_LIGHT }
    })
    target.innerHTML = svg
    const svgEl = target.querySelector('svg')
    if (svgEl) {
      svgEl.setAttribute('width', '100%')
      svgEl.setAttribute('height', '100%')
      svgEl.setAttribute('preserveAspectRatio', 'xMidYMid meet')
    }
  } catch (err) {
    console.error('QR generation failed:', err)
    target.innerHTML = `<div class="aid-qr-error">${t.formateur.qrError}</div>`
  }
}

/**
 * Update the connected-count display.
 * @param {{ count: number, voteState: string, connected: boolean } | null} state
 */
function updateCountDisplay(state) {
  const text = document.getElementById('aidCountText')
  const dot = document.getElementById('aidCountDot')
  if (!text || !dot) return

  if (!state) {
    text.textContent = t.formateur.connectedUnknown
    dot.className = 'aid-count-dot unknown'
    return
  }

  const s = state.count > 1 ? 's' : ''
  text.textContent = `${state.count} stagiaire${s} connecté${s}`
  dot.className = 'aid-count-dot ' + (state.connected ? 'live' : 'stale')
}

/**
 * Update the copy button label after a copy action.
 * @param {HTMLButtonElement} btn
 * @param {boolean} ok
 */
function flashCopyButton(btn, ok) {
  if (!btn) return
  const next = ok ? t.formateur.copied : t.formateur.copyFailed
  const original = btn.dataset.label || btn.title || ''
  btn.dataset.label = original
  btn.classList.add(ok ? 'success' : 'error')
  btn.title = next
  btn.setAttribute('aria-label', next)
  setTimeout(() => {
    btn.classList.remove('success', 'error')
    btn.title = original
    btn.setAttribute('aria-label', original)
  }, 1800)
}

/**
 * Toggle fullscreen on the document root.
 */
function toggleFullscreen() {
  try {
    if (!document.fullscreenElement) {
      document.documentElement.requestFullscreen?.()
    } else {
      document.exitFullscreen?.()
    }
  } catch {
    /* ignore */
  }
}

/**
 * Attach all event listeners for the aid view.
 * @param {string} joinURL
 */
function attachListeners(joinURL) {
  const copyBtn = document.getElementById('aidCopyBtn')
  if (copyBtn) {
    copyBtn.addEventListener('click', async () => {
      const ok = await copyToClipboard(joinURL)
      flashCopyButton(copyBtn, ok)
    })
  }

  const fsBtn = document.getElementById('aidFullscreenBtn')
  if (fsBtn) {
    fsBtn.addEventListener('click', toggleFullscreen)
  }

  // Keyboard: F for fullscreen, C to copy URL
  document.addEventListener('keydown', (e) => {
    if (e.target instanceof HTMLInputElement || e.target instanceof HTMLTextAreaElement) return
    if (e.key === 'f' || e.key === 'F') {
      toggleFullscreen()
    } else if (e.key === 'c' || e.key === 'C') {
      copyBtn?.click()
    }
  })
}

/**
 * Initialize the "Aide à la connexion" view.
 * @param {string} sessionCode
 */
export function initConnectionAid(sessionCode) {
  const app = document.getElementById('app')
  if (!app) return

  const joinURL = buildJoinURL(sessionCode)
  app.innerHTML = renderSkeleton(sessionCode, joinURL)

  void renderQR(joinURL)
  attachListeners(joinURL)

  // Track freshness so we can dim the indicator when the main tab goes quiet
  // (closed, crashed, or backgrounded and not pushing updates).
  let lastUpdate = 0
  const onUpdate = (state) => {
    lastUpdate = Date.now()
    updateCountDisplay(state)
  }

  const subscriber = createSessionSubscriber(sessionCode, onUpdate)
  subscriber.start()

  window.setInterval(() => {
    if (!lastUpdate) return
    if (Date.now() - lastUpdate > 8000) {
      const dot = document.getElementById('aidCountDot')
      if (dot && !dot.classList.contains('unknown')) {
        dot.classList.remove('live')
        dot.classList.add('stale')
      }
    }
  }, 2000)

  window.addEventListener('beforeunload', () => subscriber.close())
}
