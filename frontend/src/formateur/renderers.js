import { COLORS, escapeHtml, sanitizeColor } from '@shared/colors.js'
import {
  vote,
  timer,
  users,
  chart,
  rocket,
  stop,
  refresh,
  plus,
  loader,
  qrCode,
  bookmark,
  download,
  upload,
  checkPlain
} from '@shared/icons.js'
import { renderFooterHTML, renderSessionCodeButton, showConfirmDialog, isConfirmDialogOpen } from '@shared/ui.js'
import { t } from '@shared/strings.js'
import { createListenerTracker } from '@shared/dom/listeners.js'
import { listPresets } from '@shared/presets.js'
import { formatConnectedCount } from '@shared/utils/format.js'
import { state } from './state.js'
import { getCombinations, sortStagiaires, getColorCounts } from './vote-data.js'

// Three listenership lifetimes:
//   appTracker     — page-lifetime (escape shortcut). Survives session
//                    changes; only torn down on full page reload.
//   sessionTracker — session-lifetime (header buttons). Wiped on session
//                    leave, persists across renders within a session.
//   renderTracker  — per-render (config, vote, landing). Wiped on every
//                    #app-content re-render.
// Splitting these is what lets the Escape-to-leave shortcut survive the
// cleanupAllListeners() call that fires after every server message (C3),
// and what lets attachConfigListeners be called repeatedly without growing
// the listener Set unboundedly (M1).
const appTracker = createListenerTracker()
const sessionTracker = createListenerTracker()
const renderTracker = createListenerTracker()

const trackAppListener = appTracker.track
const trackSessionListener = sessionTracker.track
const trackListener = renderTracker.track

/**
 * Wipe session + render listeners. The app tracker (page-scoped keyboard
 * shortcuts) is intentionally preserved so the Escape shortcut keeps
 * working after the user leaves one session and joins another.
 */
export function cleanupAllListeners() {
  sessionTracker.cleanup()
  renderTracker.cleanup()
}

/**
 * Debug helper for tests: returns the current size of each tracker so a
 * test can assert that repeated attach calls don't grow the Sets
 * unboundedly (M1 regression guard). Not used by production code.
 */
export function _trackerSizesForTests() {
  return {
    app: appTracker.size,
    session: sessionTracker.size,
    render: renderTracker.size
  }
}

const _actionHandlers = {}

export function setActionHandlers(handlers) {
  Object.assign(_actionHandlers, handlers)
}

// F23: the header leave handler is registered once from the composition
// root (main.js) so updateHeader can (re)bind the header buttons whenever
// it injects fresh markup, without needing the leave fn threaded through
// every call site. Mirrors the _actionHandlers registration pattern.
let _headerLeaveFn = null

export function registerHeaderLeaveHandler(fn) {
  _headerLeaveFn = typeof fn === 'function' ? fn : null
}

/**
 * Open the "Aide à la connexion" view in a new browser tab so the formateur
 * can drag it onto the videoprojector screen. The URL is built from the
 * current location so it inherits protocol/host automatically.
 * @param {string} sessionCode
 */
export function openConnectionAid(sessionCode) {
  if (!sessionCode) return
  const url = new URL(window.location.href)
  url.search = `?aide=${encodeURIComponent(sessionCode)}`
  url.hash = ''
  window.open(url.toString(), '_blank', 'noopener')
}

/**
 * Render the landing page
 * @param {HTMLElement} app
 */
export function renderLandingPage(app) {
  app.innerHTML = `
    <div class="container">
      <div class="landing-card card">
        <div class="landing-icon">${vote(' class="icon"')}</div>
        <h1 class="landing-title">${t.common.voteColore}</h1>
        <p class="landing-subtitle">${t.formateur.subtitle}</p>

        <div class="landing-actions">
          <button id="createSessionBtn" class="btn btn-primary btn-large" data-testid="create-session-btn" title="${t.formateur.createSession} — ${t.formateur.shortcutEnter}">
            ${plus(' class="icon icon-md"')} ${t.formateur.createSession}
          </button>

          <div class="landing-divider">${t.formateur.or}</div>

          <div class="input-group">
            <input type="text" id="joinSessionInput" class="input-text" data-testid="join-session-input" placeholder="${t.formateur.sessionPlaceholder}" maxlength="3" pattern="[A-HJ-NP-Ya-hj-np-y]{3}" inputmode="text" autocapitalize="characters" autocomplete="off">
            <button id="joinSessionBtn" class="btn btn-secondary" data-testid="join-session-btn" title="${t.formateur.joinSession} — ${t.formateur.shortcutEnter}">
              ${t.formateur.joinSession}
            </button>
          </div>
          <div class="error-message" role="alert" data-testid="error-message"></div>
          <p class="landing-hint">${t.formateur.shortcutHintLanding}</p>
        </div>
      </div>
    </div>
    ${renderFooterHTML()}
  `
}

/**
 * Update landing page loading state
 * @param {boolean} isLoading
 */
export function updateLandingPageLoadingState(isLoading) {
  const createBtn = document.getElementById('createSessionBtn')
  const joinBtn = document.getElementById('joinSessionBtn')
  const joinInput = document.getElementById('joinSessionInput')

  if (!createBtn || !joinBtn) return

  if (isLoading) {
    createBtn.disabled = true
    joinBtn.disabled = true
    joinInput.disabled = true
    createBtn.innerHTML = `${loader(' class="icon icon-md spin"')} ${t.formateur.connecting}`
  } else {
    createBtn.disabled = false
    joinBtn.disabled = false
    joinInput.disabled = false
    createBtn.innerHTML = `${plus(' class="icon icon-md"')} ${t.formateur.createSession}`
  }
}

/**
 * Render the full app layout
 * @param {HTMLElement} app
 */
export function renderFullLayout(app) {
  app.innerHTML = `
    <div class="container">
      <div id="reconnect-banner" class="reconnect-banner" role="alert" aria-live="assertive" hidden>
        <span class="reconnect-banner-spinner" aria-hidden="true"></span>
        <span class="reconnect-banner-text">${t.formateur.reconnecting}</span>
      </div>
      <header class="header" id="app-header"></header>
      <main id="app-content"></main>
    </div>
    ${renderFooterHTML()}
  `
}

/**
 * Show or hide the reconnection banner. Visible only when the trainer has
 * been connected before (so initial load doesn't trigger a false alarm) and
 * the WS is currently down.
 *
 * F25: when the WS client has permanently given up (state.permanentlyClosed),
 * swap the banner to a recoverable "Connexion perdue — rechargez la page"
 * state with the accent colour — otherwise the trainer stares at a
 * "Reconnexion…" promise that will never resolve (the client stopped trying
 * after ~16h backoff or a 4xxx permanent close).
 */
export function updateConnectionBanner() {
  const banner = document.getElementById('reconnect-banner')
  if (!banner) return
  const shouldShow = !!state.sessionCode && state.everConnected && !state.connected
  if (shouldShow) {
    banner.hidden = false
    banner.setAttribute('aria-hidden', 'false')
    const text = banner.querySelector('.reconnect-banner-text')
    const spinner = banner.querySelector('.reconnect-banner-spinner')
    if (state.permanentlyClosed) {
      banner.classList.add('reconnect-banner-lost')
      if (text) text.textContent = t.formateur.connectionLost
      if (spinner) spinner.hidden = true
    } else {
      banner.classList.remove('reconnect-banner-lost')
      if (text) text.textContent = t.formateur.reconnecting
      if (spinner) spinner.hidden = false
    }
  } else {
    banner.hidden = true
    banner.setAttribute('aria-hidden', 'true')
    banner.classList.remove('reconnect-banner-lost')
  }
}

/**
 * F24: toggle the action buttons' disabled state so it tracks
 * state.connected live, instead of only reflecting the connectivity at
 * the last render. Without this, a mid-session wifi flap leaves the
 * buttons visually enabled; a click silently drops because client.send
 * returns false on a disconnected socket. Cheaper and non-disruptive
 * than a full card re-render (preserves in-progress label edits, preset
 * save form, etc.).
 */
export function updateActionButtonsState() {
  const isConnected = state.connected
  const ids = ['startVote', 'resetConfig', 'closeVote', 'revealBtn', 'newVote']
  for (const id of ids) {
    const btn = document.getElementById(id)
    if (!btn) continue
    let disabled = !isConnected
    if (id === 'startVote') {
      disabled = disabled || state.selectedColors.size < 2
    }
    btn.disabled = disabled
  }
}

/**
 * Update the header
 * @param {Object} client
 */
export function updateHeader(client) {
  const header = document.getElementById('app-header')
  if (!header) return

  const isConnected = client ? client.isConnected() : state.connected
  const existingBtn = document.getElementById('leaveSessionBtn')

  if (existingBtn && existingBtn.textContent === state.sessionCode) {
    existingBtn.className = `session-code ${isConnected ? 'connected' : 'disconnected'}`
    return
  }

  header.innerHTML = `
    <h1>${vote(' class="icon icon-md"')} ${t.common.voteColore} - ${t.common.formateur}</h1>
    <div class="header-right">
      <button
        id="openConnectionAidBtn"
        class="header-action-btn"
        data-testid="open-connection-aid-btn"
        title="${t.formateur.openClassroomDisplayTitle}"
        aria-label="${t.formateur.openClassroomDisplay}"
      >${qrCode(' class="icon icon-md"')}</button>
      ${renderSessionCodeButton(state.sessionCode, isConnected, `${t.formateur.leaveSessionTitle} — ${t.formateur.shortcutEsc}`)}
    </div>
  `

  // F23: fresh markup just replaced the header's children, so the buttons
  // are brand-new elements with no listeners. Re-attach now. The className
  // fast-path above returns early (no innerHTML change), so this only fires
  // on genuine injection — no double-bind. attachHeaderListeners cleans the
  // session tracker first, so repeated injections (e.g. a session-code
  // change) can't accumulate stale references to detached nodes.
  attachHeaderListeners()
}

/**
 * Attach header event listeners.
 *
 * F23: reads the leave handler from the module-level registration
 * (registerHeaderLeaveHandler, set once from main.js) so it can be called
 * with no arguments — updateHeader invokes it automatically after every
 * fresh markup injection. Self-cleaning: the session tracker is wiped
 * first so a repeated attach (session-code change, repeated session_created)
 * can't double-bind or leak references to detached nodes.
 * @param {Object} [client] unused (kept for call-site compatibility)
 * @param {Function} [leaveSessionFn] override; defaults to the registered fn
 */
export function attachHeaderListeners(client, leaveSessionFn) {
  const leave = leaveSessionFn || _headerLeaveFn
  sessionTracker.cleanup()
  const leaveSessionBtn = document.getElementById('leaveSessionBtn')
  if (leaveSessionBtn) {
    trackSessionListener(leaveSessionBtn, 'click', async () => {
      const ok = await showConfirmDialog({
        title: t.formateur.leaveSessionTitle,
        message: t.formateur.leaveSession,
        confirmLabel: t.formateur.leave
      })
      if (ok && leave) leave()
    })
  }

  const aidBtn = document.getElementById('openConnectionAidBtn')
  if (aidBtn && state.sessionCode) {
    trackSessionListener(aidBtn, 'click', () => openConnectionAid(state.sessionCode))
  }
}

/**
 * Render the main content area
 */
export function renderMainContent() {
  const main = document.getElementById('app-content')
  if (!main) return

  if (state.voteState === 'idle') {
    main.innerHTML = renderConfigHTML()
  } else {
    main.innerHTML = renderVoteHTML()
  }
}

/**
 * Attach config page event listeners
 * @param {Object} client
 */
export function attachConfigListeners(client) {
  // Self-cleaning: handlers.js calls this directly after renderMainContent()
  // (preset apply / save / cancel / etc.) without an explicit cleanup, so
  // without this the renderTracker Set would grow unboundedly (M1).
  // Safe to call from attachListeners() too — the tracker is already empty
  // there, so this is a no-op.
  renderTracker.cleanup()
  // Color checkboxes
  document.querySelectorAll('.color-checkbox input[type="checkbox"]').forEach((checkbox) => {
    trackListener(checkbox, 'change', (e) => {
      const colorId = e.target.value
      if (e.target.checked) {
        state.selectedColors.add(colorId)
      } else {
        state.selectedColors.delete(colorId)
      }

      // Update selected class
      const parent = e.target.closest('.color-checkbox')
      parent.classList.toggle('selected', e.target.checked)

      // Update button state
      const startBtn = document.getElementById('startVote')
      if (startBtn) {
        startBtn.disabled = state.selectedColors.size < 2
      }
    })
  })

  // Label inputs for custom color names
  document.querySelectorAll('.color-label-input').forEach((input) => {
    trackListener(input, 'input', (e) => {
      const colorId = e.target.dataset.colorId
      const value = e.target.value.trim()
      if (value) {
        state.colorLabels[colorId] = value
      } else {
        delete state.colorLabels[colorId]
      }
    })
  })

  // Multiple choice toggle
  const toggleMultiple = document.querySelector('.multiple-choice-toggle[data-testid="multiple-choice-toggle"]')
  if (toggleMultiple) {
    trackListener(toggleMultiple, 'click', () => {
      state.multipleChoice = !state.multipleChoice
      const switchEl = toggleMultiple.querySelector('.toggle-switch')
      switchEl.classList.toggle('active', state.multipleChoice)
    })
  }

  // Mini-game toggle
  const toggleGame = document.querySelector('.multiple-choice-toggle[data-testid="game-toggle"]')
  if (toggleGame) {
    trackListener(toggleGame, 'click', () => {
      state.gameEnabled = !state.gameEnabled
      const switchEl = toggleGame.querySelector('.toggle-switch')
      switchEl.classList.toggle('active', state.gameEnabled)
    })
  }

  // Competitive mode toggle
  const toggleCompetitive = document.querySelector('.multiple-choice-toggle[data-testid="competitive-toggle"]')
  if (toggleCompetitive) {
    trackListener(toggleCompetitive, 'click', () => {
      state.competitive = !state.competitive
      if (!state.competitive) state.allowBlank = false
      renderMainContent()
      attachConfigListeners(client)
    })
  }

  // Blank vote toggle
  const toggleBlank = document.querySelector('.multiple-choice-toggle[data-testid="blank-vote-toggle"]')
  if (toggleBlank) {
    trackListener(toggleBlank, 'click', () => {
      state.allowBlank = !state.allowBlank
      const switchEl = toggleBlank.querySelector('.toggle-switch')
      switchEl.classList.toggle('active', state.allowBlank)
    })
  }

  const startBtn = document.getElementById('startVote')
  if (startBtn && _actionHandlers.startVote) {
    trackListener(startBtn, 'click', () => _actionHandlers.startVote(client))
  }

  const resetBtn = document.getElementById('resetConfig')
  if (resetBtn && _actionHandlers.resetConfig) {
    trackListener(resetBtn, 'click', () => _actionHandlers.resetConfig())
  }

  attachPresetListeners()
}

/**
 * Wire up preset chips, the save button, and the inline save form.
 * Delegates chip clicks via data-action attributes so we don't need per-chip
 * listeners (works for arbitrary numbers of presets).
 */
function attachPresetListeners() {
  const saveBtn = document.getElementById('savePresetBtn')
  if (saveBtn && _actionHandlers.beginSavePreset) {
    trackListener(saveBtn, 'click', () => _actionHandlers.beginSavePreset())
  }

  const exportBtn = document.getElementById('presetsExportBtn')
  if (exportBtn && _actionHandlers.exportPresets) {
    trackListener(exportBtn, 'click', () => _actionHandlers.exportPresets())
  }
  const importBtn = document.getElementById('presetsImportBtn')
  if (importBtn && _actionHandlers.importPresets) {
    trackListener(importBtn, 'click', () => _actionHandlers.importPresets())
  }

  const saveConfirm = document.getElementById('presetSaveConfirm')
  const saveCancel = document.getElementById('presetSaveCancel')
  const nameInput = document.getElementById('presetNameInput')
  if (saveConfirm && _actionHandlers.confirmSavePreset) {
    trackListener(saveConfirm, 'click', () => {
      _actionHandlers.confirmSavePreset(nameInput ? nameInput.value : '')
    })
  }
  if (saveCancel && _actionHandlers.cancelSavePreset) {
    trackListener(saveCancel, 'click', () => _actionHandlers.cancelSavePreset())
  }
  if (nameInput) {
    // Auto-focus when the form opens.
    requestAnimationFrame(() => nameInput.focus())
    trackListener(nameInput, 'keydown', (e) => {
      if (e.key === 'Enter') {
        // preventDefault stops the form from submitting; stopImmediatePropagation
        // keeps the keystroke inside this input so it does not bubble to other
        // keydown listeners (e.g. Enter-to-join on the landing page, were this
        // ever reused elsewhere).
        e.preventDefault()
        e.stopImmediatePropagation()
        if (_actionHandlers.confirmSavePreset) _actionHandlers.confirmSavePreset(nameInput.value)
      } else if (e.key === 'Escape') {
        // preventDefault alone does NOT stop propagation. Without
        // stopImmediatePropagation the event bubbles to the document-level
        // app shortcut (attachAppKeyboardShortcuts), which sees
        // `state.sessionCode` truthy and opens the "Quitter la session?"
        // confirm on top of the just-closed preset form.
        e.preventDefault()
        e.stopImmediatePropagation()
        if (_actionHandlers.cancelSavePreset) _actionHandlers.cancelSavePreset()
      }
    })
  }

  // Chip clicks (apply) and × (delete) — single delegated listener each.
  const chipsContainer = document.querySelector('.presets-chips')
  if (chipsContainer) {
    trackListener(chipsContainer, 'click', (e) => {
      const trigger = e.target.closest('[data-preset-action]')
      if (!trigger || !chipsContainer.contains(trigger)) return
      const id = trigger.dataset.presetId
      const action = trigger.dataset.presetAction
      if (action === 'apply' && _actionHandlers.applyPreset) {
        _actionHandlers.applyPreset(id)
      } else if (action === 'delete' && _actionHandlers.deletePreset) {
        _actionHandlers.deletePreset(id)
      }
    })
    // Keyboard: Enter/Space on a chip applies it.
    trackListener(chipsContainer, 'keydown', (e) => {
      if (e.key !== 'Enter' && e.key !== ' ') return
      const trigger = e.target.closest('[data-preset-action="apply"]')
      if (!trigger) return
      e.preventDefault()
      if (_actionHandlers.applyPreset) _actionHandlers.applyPreset(trigger.dataset.presetId)
    })
  }
}

/**
 * Attach vote page event listeners
 * @param {Object} client
 */
export function attachVoteListeners(client) {
  // Self-cleaning — same reason as attachConfigListeners.
  renderTracker.cleanup()
  const closeBtn = document.getElementById('closeVote')
  if (closeBtn && _actionHandlers.closeVote) {
    trackListener(closeBtn, 'click', () => _actionHandlers.closeVote(client))
  }

  const newVoteBtn = document.getElementById('newVote')
  if (newVoteBtn && _actionHandlers.resetVote) {
    trackListener(newVoteBtn, 'click', () => _actionHandlers.resetVote(client))
  }

  const revealBtn = document.getElementById('revealBtn')
  if (revealBtn && _actionHandlers.revealAnswers) {
    trackListener(revealBtn, 'click', () => {
      const correct = new Set(
        Array.from(document.querySelectorAll('input[data-correct-color]:checked')).map((el) => el.dataset.correctColor)
      )
      _actionHandlers.revealAnswers(client, correct)
    })
  }

  document.querySelectorAll('.reveal-color-chip input[data-correct-color]').forEach((checkbox) => {
    trackListener(checkbox, 'change', () => {
      const chip = checkbox.closest('.reveal-color-chip')
      if (chip) chip.classList.toggle('selected', checkbox.checked)
    })
  })
}

/**
 * Render the configuration HTML
 * @returns {string}
 */
export function renderConfigHTML() {
  const isConnected = state.connected
  return `
    <div class="card">
      <h2 class="card-title">${t.formateur.configTitle}</h2>
      <div class="config-info" aria-live="polite" data-testid="connected-count">${users(' class="icon icon-sm"')} ${formatConnectedCount(state.connectedCount)}</div>

      ${renderPresetsSectionHTML()}

      <div class="config-section">
        <div>
          <div class="stats-header">${t.formateur.availableColors}</div>
          <div class="colors-grid">
            ${COLORS.map((color) => {
              const customLabel = state.colorLabels[color.id] || ''
              return `
              <label class="color-checkbox ${state.selectedColors.has(color.id) ? 'selected' : ''}">
                <input
                  type="checkbox"
                  value="${color.id}"
                  ${state.selectedColors.has(color.id) ? 'checked' : ''}
                />
                <span class="color-swatch" style="background-color: ${sanitizeColor(color.color)}"></span>
                <div class="color-label-wrapper">
                  <input
                    type="text"
                    class="color-label-input"
                    data-color-id="${color.id}"
                    value="${escapeHtml(customLabel)}"
                    placeholder="${escapeHtml(color.name)}"
                    maxlength="12"
                  />
                </div>
              </label>
            `
            }).join('')}
          </div>
        </div>

        <label class="multiple-choice-toggle" data-testid="multiple-choice-toggle">
          <span class="toggle-switch ${state.multipleChoice ? 'active' : ''}" data-action="toggle-multiple"></span>
          <span>${t.formateur.multipleChoiceToggle}</span>
        </label>

        <label class="multiple-choice-toggle" data-testid="game-toggle">
          <span class="toggle-switch ${state.gameEnabled ? 'active' : ''}" data-action="toggle-game"></span>
          <span>${t.formateur.gameToggle}
            <span class="toggle-hint">${t.formateur.gameToggleHint}</span>
          </span>
        </label>

        <label class="multiple-choice-toggle" data-testid="competitive-toggle">
          <span class="toggle-switch ${state.competitive ? 'active' : ''}" data-action="toggle-competitive"></span>
          <span>${t.formateur.competitiveToggle}
            <span class="toggle-hint">${t.formateur.competitiveToggleHint}</span>
          </span>
        </label>

        ${
          state.competitive
            ? `
        <label class="multiple-choice-toggle" data-testid="blank-vote-toggle">
          <span class="toggle-switch ${state.allowBlank ? 'active' : ''}" data-action="toggle-blank"></span>
          <span>${t.formateur.blankVoteToggle}</span>
        </label>
        `
            : ''
        }
      </div>

      <div class="button-row">
        <button class="btn btn-secondary" id="resetConfig" ${!isConnected ? 'disabled' : ''}>
          ${refresh(' class="icon icon-md"')} ${t.formateur.reset}
        </button>
        <button class="btn btn-primary btn-large" id="startVote" data-testid="start-vote-btn" ${state.selectedColors.size < 2 || !isConnected ? 'disabled' : ''}>
          ${rocket(' class="icon icon-md"')} ${t.formateur.startVote}
        </button>
      </div>
    </div>
  `
}

/**
 * Render the "Modèles" (saved layouts) section above the color grid.
 * The header + save button are always rendered so first-time users can save
 * their current setup. The chips row only renders when presets exist.
 */
function renderPresetsSectionHTML() {
  const presets = listPresets()
  const saving = state.presetSaving

  const chips = presets
    .map((p) => {
      const swatches = p.config.selectedColors
        .slice(0, 5)
        .map((id) => {
          const c = COLORS.find((x) => x.id === id)
          return c ? `<span class="preset-chip-swatch" style="background-color:${sanitizeColor(c.color)}"></span>` : ''
        })
        .join('')
      const title = t.formateur.presetSwatchTitle(p.name, p.config.selectedColors.length)
      return `
        <div class="preset-chip" data-preset-id="${escapeHtml(p.id)}" data-preset-action="apply" title="${escapeHtml(title)}" tabindex="0">
          <span class="preset-chip-swatches">${swatches}</span>
          <span class="preset-chip-name">${escapeHtml(p.name)}</span>
          <button
            type="button"
            class="preset-chip-delete"
            data-preset-id="${escapeHtml(p.id)}"
            data-preset-action="delete"
            aria-label="Supprimer le modèle ${escapeHtml(p.name)}"
            title="Supprimer"
          >×</button>
        </div>
      `
    })
    .join('')

  const saveForm = saving
    ? `
      <div class="preset-save-row">
        <input
          type="text"
          id="presetNameInput"
          class="input-text preset-name-input"
          placeholder="${escapeHtml(t.formateur.savePresetPlaceholder)}"
          maxlength="40"
          autocomplete="off"
        />
        <button type="button" id="presetSaveConfirm" class="btn btn-primary btn-sm">${t.formateur.savePresetConfirm}</button>
        <button type="button" id="presetSaveCancel" class="btn btn-secondary btn-sm">${t.common.cancel}</button>
      </div>
    `
    : ''

  const saveButton = saving
    ? ''
    : `
      <button type="button" id="savePresetBtn" class="preset-save-btn" title="${escapeHtml(t.formateur.savePreset)}">
        ${bookmark(' class="icon icon-sm"')} ${t.formateur.savePreset}
      </button>
    `

  return `
    <div class="presets-section" data-testid="presets-section">
      <div class="presets-header">
        <span class="stats-header presets-header-label">${t.formateur.presets}</span>
        ${saveButton}
      </div>
      ${chips ? `<div class="presets-chips custom-scrollbar">${chips}</div>` : ''}
      ${presets.length > 0 ? `<p class="presets-hint">${t.formateur.presetsHint}</p>` : ''}
      <div class="presets-io">
        <button type="button" id="presetsImportBtn" class="preset-io-btn" title="${escapeHtml(t.formateur.importPresetsTitle)}">
          ${upload(' class="icon icon-sm"')} ${t.formateur.importPresets}
        </button>
        <button type="button" id="presetsExportBtn" class="preset-io-btn" title="${escapeHtml(t.formateur.exportPresetsTitle)}" ${presets.length === 0 ? 'disabled' : ''}>
          ${download(' class="icon icon-sm"')} ${t.formateur.exportPresets}
        </button>
      </div>
      ${saveForm}
    </div>
  `
}

/**
 * Render the vote HTML
 * @returns {string}
 */
export function renderVoteHTML() {
  const activeColors = COLORS.filter((c) => state.selectedColors.has(c.id))

  // Calculate initial stats
  const voteCount = state.stagiaires.filter((s) => s.vote && s.vote.length > 0).length
  const colorCounts = getColorCounts()
  const maxCount = Math.max(...Object.values(colorCounts), 1)
  const isConnected = state.connected

  return `
    <div class="card">
      <div class="vote-header">
        <div class="vote-timer">${timer(' class="icon icon-sm"')} 00:00</div>
        <div class="vote-stats">
          <span class="vote-count" aria-live="polite" data-testid="vote-count">${chart(' class="icon icon-sm"')} ${voteCount} / ${state.connectedCount} ${t.formateur.votes}</span>
        </div>
      </div>

      <div class="stats-grid stats-grid-3cols">
        <div>
          <div class="stats-header">${t.formateur.byColor}</div>
          <div class="color-bars">
            ${renderColorBarsHTML(activeColors, colorCounts, maxCount)}
          </div>
        </div>

        <div>
          <div class="stats-header">${t.formateur.byCombination}</div>
          <div class="combinations-list custom-scrollbar">
             ${renderCombinationsHTML()}
          </div>
        </div>

        <div>
          <div class="stats-header">${t.formateur.byStagiaire}</div>
          <div class="stagiaires-votes-list custom-scrollbar">
             ${renderStagiairesVotesHTML()}
          </div>
        </div>
      </div>

      ${renderCompetitiveSectionHTML(activeColors)}

      <div class="button-row">
        ${
          state.voteState === 'active'
            ? `
          <button class="btn btn-danger" id="closeVote" data-testid="close-vote-btn" ${!isConnected ? 'disabled' : ''}>${stop(' class="icon icon-md"')} ${t.formateur.closeVote}</button>
        `
            : state.competitive
              ? `
          <button class="btn btn-primary" id="revealBtn" data-testid="reveal-btn" ${!isConnected ? 'disabled' : ''}>${checkPlain(' class="icon icon-md"')} ${t.formateur.revealAnswers}</button>
          <button class="btn btn-success" id="newVote" data-testid="new-vote-btn" ${!isConnected ? 'disabled' : ''}>${refresh(' class="icon icon-md"')} ${t.formateur.newVote}</button>
        `
              : `
          <button class="btn btn-success" id="newVote" data-testid="new-vote-btn" ${!isConnected ? 'disabled' : ''}>${refresh(' class="icon icon-md"')} ${t.formateur.newVote}</button>
        `
        }
      </div>
    </div>
  `
}

function renderRevealSectionHTML(activeColors) {
  return `
    <div class="reveal-section">
      <div class="reveal-title">${t.formateur.markCorrect}</div>
      <div class="reveal-colors">
        ${activeColors
          .map((color) => {
            const checked = state.correctColors.has(color.id) ? 'checked' : ''
            const displayName = state.colorLabels[color.id] || color.name
            return `
          <label class="reveal-color-chip ${checked ? 'selected' : ''}" data-correct-color="${color.id}">
            <input type="checkbox" data-correct-color="${color.id}" ${checked} />
            <span class="color-swatch" style="background-color: ${sanitizeColor(color.color)}"></span>
            <span class="reveal-color-name">${escapeHtml(displayName)}</span>
          </label>`
          })
          .join('')}
        ${
          state.allowBlank
            ? `
        <label class="reveal-color-chip ${state.correctColors.has('blank') ? 'selected' : ''}" data-correct-color="blank">
          <input type="checkbox" data-correct-color="blank" ${state.correctColors.has('blank') ? 'checked' : ''} />
          <span class="reveal-color-name">${t.formateur.blankVote}</span>
        </label>
        `
            : ''
        }
      </div>
    </div>
  `
}

function renderScoreboardHTML() {
  // F26: rank by cumulative totalScore (which already folds in the game
  // score), not the per-round voteScore that resets every round. The
  // backend assigns competition ranks (R4) over this same ordering, so
  // mirroring it here keeps the trainer's table consistent with the
  // classroom aid leaderboard and the rank value each entry carries.
  const ranked = [...state.scoreboard].sort((a, b) => {
    const ta = a.totalScore ?? 0
    const tb = b.totalScore ?? 0
    if (tb !== ta) return tb - ta
    return String(a.name || '').localeCompare(String(b.name || ''))
  })
  const rows = ranked
    .map((entry) => {
      const voteColors = (entry.vote || []).filter((c) => c !== 'blank')
      const isBlank = (entry.vote || []).includes('blank')
      const colorsHTML = voteColors
        .map((colorId) => {
          const color = COLORS.find((c) => c.id === colorId)
          const isCorrect = state.correctColors.has(colorId)
          return `<span class="scoreboard-swatch ${isCorrect ? 'correct' : 'wrong'}" style="background-color: ${sanitizeColor(color?.color)}" title="${escapeHtml(color?.name || colorId)}"></span>`
        })
        .join('')
      const voteDisplay = isBlank ? '<span class="scoreboard-blank">blanc</span>' : colorsHTML || '—'
      const voteScore = entry.voteScore ?? 0
      const voteScoreClass = voteScore >= 0 ? 'positive' : 'negative'
      const voteScoreText = voteScore >= 0 ? `+${voteScore}` : String(voteScore)
      const totalScore = entry.totalScore ?? 0
      const rank = entry.rank || 0
      return `
      <li class="scoreboard-row${rank ? ` rank-${rank}` : ''}">
        <span class="scoreboard-rank">${rank || '—'}</span>
        <span class="scoreboard-name">${escapeHtml(entry.name || t.common.anonymous)}</span>
        <span class="scoreboard-vote">${voteDisplay}</span>
        <span class="scoreboard-votescore ${voteScoreClass}" title="${t.formateur.score}">${voteScoreText}</span>
        <span class="scoreboard-total" title="${t.formateur.totalScore}">${totalScore}</span>
      </li>
    `
    })
    .join('')

  return `
    <div class="scoreboard-section">
      <div class="scoreboard-title">${t.formateur.scoreboard}</div>
      <div class="scoreboard-header">
        <span class="scoreboard-rank" aria-hidden="true">#</span>
        <span class="scoreboard-name">${t.formateur.scoreboardName}</span>
        <span class="scoreboard-vote" aria-hidden="true"></span>
        <span class="scoreboard-votescore">${t.formateur.scoreboardRound}</span>
        <span class="scoreboard-total">${t.formateur.totalScore}</span>
      </div>
      <ol class="scoreboard-list">${rows}</ol>
    </div>
  `
}

function renderCompetitiveSectionHTML(activeColors) {
  if (!state.competitive || state.voteState !== 'closed') return ''

  // R14: the reveal section stays visible after reveal so the trainer can
  // correct the answer key and re-reveal. The backend's RevealAnswers is
  // idempotent (BL2/BL3): it reverses the previously applied scores and
  // applies the new ones. The current correctColors are pre-checked, so a
  // re-reveal after a tweak is a single click on "Révéler".
  return renderRevealSectionHTML(activeColors) + (state.revealed ? renderScoreboardHTML() : '')
}

/**
 * Render color bars HTML
 * @param {Array} activeColors
 * @param {Object} colorCounts
 * @param {number} maxCount
 * @returns {string}
 */
export function renderColorBarsHTML(activeColors, colorCounts, maxCount) {
  // Sort colors by vote count desc
  const sortedColors = [...activeColors].sort((a, b) => (colorCounts[b.id] || 0) - (colorCounts[a.id] || 0))

  return sortedColors
    .map((color) => {
      const count = colorCounts[color.id] || 0
      const percent = (count / maxCount) * 100
      const displayName = state.colorLabels[color.id] || color.name
      return `
      <div class="color-bar-row" data-color="${color.id}">
        <div class="color-bar-label">
          <span class="color-bar-swatch" style="background-color: ${sanitizeColor(color.color)}"></span>
          <span class="color-bar-name">${escapeHtml(displayName)}</span>
        </div>
        <div class="color-bar-track">
          <div class="color-bar-fill ${count === 0 ? 'empty' : ''}" style="width: ${percent}%; background-color: ${sanitizeColor(color.color)}"></div>
        </div>
        <span class="color-bar-count">${count}</span>
      </div>
    `
    })
    .join('')
}

/**
 * Render combinations HTML.
 * Each combination is shown as a horizontal bar whose width is proportional to
 * its vote count (relative to the most popular combination) and which is
 * internally divided into equal-width colored segments — one per color of the
 * combination. This scales gracefully from 1 to N colors: segments shrink as N
 * grows but always remain visible inside the proportional bar.
 * @returns {string}
 */
export function renderCombinationsHTML() {
  const combinations = getCombinations()

  if (combinations.length === 0) {
    return `
      <div class="empty-state">
        <div class="empty-icon">${chart(' class="icon icon-xl"')}</div>
        <div>${t.formateur.noVotes}</div>
      </div>
    `
  }

  const maxCount = combinations.reduce((max, c) => Math.max(max, c.count), 1)

  return combinations
    .map((combo) => {
      const percent = (combo.count / maxCount) * 100
      const segments = combo.colors
        .map((colorId) => {
          const color = COLORS.find((c) => c.id === colorId)
          return `<span class="combo-segment" style="background-color: ${sanitizeColor(color?.color)}" title="${escapeHtml(color?.name || colorId)}"></span>`
        })
        .join('')

      return `
      <div class="combo-item" data-count="${combo.count}" data-max="${maxCount}">
        <div class="combo-bar-track" title="${combo.colors.length} couleur${combo.colors.length > 1 ? 's' : ''} • ${combo.count} vote${combo.count > 1 ? 's' : ''}">
          <div class="combo-bar-fill" style="width: ${percent}%">${segments}</div>
        </div>
        <div class="combo-count">${combo.count}</div>
      </div>
    `
    })
    .join('')
}

/**
 * Render stagiaires votes HTML
 * @returns {string}
 */
export function renderStagiairesVotesHTML() {
  if (state.stagiaires.length === 0) {
    return `
      <div class="empty-state">
        <div class="empty-icon">${users(' class="icon icon-xl"')}</div>
        <div>${t.formateur.noStagiaires}</div>
      </div>
    `
  }

  const sorted = sortStagiaires(state.stagiaires)

  return sorted
    .map((s) => {
      const displayName = s.name || t.common.anonymous
      const hasVoted = s.vote && s.vote.length > 0
      const isBlank = s.vote?.includes('blank')
      const isConnected = s.connected

      const onlineDot = `<span class="online-dot ${isConnected ? 'connected' : 'disconnected'}" title="${isConnected ? t.formateur.online : t.formateur.offline}"></span>`

      if (!hasVoted) {
        // Non-voter: "waiting" label
        return `
        <div class="stagiaire-vote-item waiting">
          <span class="stagiaire-vote-name">${onlineDot}<span class="name-text" title="${escapeHtml(displayName)}">${escapeHtml(displayName)}</span></span>
          <span class="stagiaire-vote-waiting">${t.formateur.waiting}</span>
        </div>
      `
      }

      if (isBlank) {
        return `
        <div class="stagiaire-vote-item">
          <span class="stagiaire-vote-name">${onlineDot}<span class="name-text" title="${escapeHtml(displayName)}">${escapeHtml(displayName)}</span></span>
          <div class="stagiaire-vote-colors"><span class="stagiaire-vote-blank">${t.formateur.blankVote}</span></div>
        </div>
      `
      }

      const colorsHTML = s.vote
        .filter((colorId) => colorId !== 'blank')
        .map((colorId) => {
          const color = COLORS.find((c) => c.id === colorId)
          return `<span class="stagiaire-vote-swatch" style="background-color: ${sanitizeColor(color?.color)}" title="${escapeHtml(color?.name || colorId)}"></span>`
        })
        .join('')

      return `
      <div class="stagiaire-vote-item">
        <span class="stagiaire-vote-name">${onlineDot}<span class="name-text" title="${escapeHtml(displayName)}">${escapeHtml(displayName)}</span></span>
        <div class="stagiaire-vote-colors">${colorsHTML}</div>
      </div>
    `
    })
    .join('')
}

/**
 * Attach landing page event listeners including keyboard shortcuts
 * @param {Function} joinSessionFn
 * @param {Function} createSessionFn
 */
export function attachLandingListeners(joinSessionFn, createSessionFn) {
  const createBtn = document.getElementById('createSessionBtn')
  const joinBtn = document.getElementById('joinSessionBtn')
  const joinInput = document.getElementById('joinSessionInput')

  if (createBtn) {
    trackListener(createBtn, 'click', createSessionFn)
  }

  if (joinBtn) {
    const joinHandler = () => {
      const code = joinInput?.value.trim()
      joinSessionFn(code)
    }
    trackListener(joinBtn, 'click', joinHandler)
  }

  // Keyboard shortcuts on landing page
  const keyHandler = (e) => {
    if (e.key === 'Enter' && !(document.activeElement instanceof HTMLButtonElement)) {
      if (document.activeElement === joinInput && joinInput.value.trim()) {
        joinSessionFn(joinInput.value.trim())
      } else if (!joinInput || !joinInput.value.trim()) {
        createSessionFn()
      }
    }
  }

  trackListener(document, 'keydown', keyHandler)
}

/**
 * Attach keyboard shortcuts for the full app.
 *
 * Registered on the app tracker (not the render tracker) so the cleanup
 * that runs after every server message does NOT kill the Escape-to-leave
 * shortcut mid-session. The handler no-ops when `state.sessionCode` is
 * falsy, so it's safe to keep attached on the landing page too.
 *
 * R6: idempotent — a module-level guard ensures the document-level
 * keydown listener is bound exactly once per page lifecycle, even if
 * the call site is reached twice (e.g. reload-with-session + a future
 * session_created re-attach). The latest `leaveSessionFn` wins, so a
 * late call can still swap the leave handler if needed.
 * @param {Function} leaveSessionFn
 */
let _appShortcutAttached = false
let _appShortcutLeaveFn = null
export function attachAppKeyboardShortcuts(leaveSessionFn) {
  // Always refresh the leave handler — a late caller may swap it.
  _appShortcutLeaveFn = leaveSessionFn
  if (_appShortcutAttached) return
  _appShortcutAttached = true

  const keyHandler = async (e) => {
    // Escape key - leave session with confirmation.
    // F31: skip when a confirm dialog is already open. Without this guard
    // (paired with the capture-phase keydown in ui.js), pressing Escape
    // while a confirm is visible re-enters showConfirmDialog synchronously,
    // overwrites confirmResolve, and leaks the original caller's promise.
    if (e.key === 'Escape' && state.sessionCode && !isConfirmDialogOpen()) {
      e.preventDefault()
      const ok = await showConfirmDialog({
        title: t.formateur.leaveSessionTitle,
        message: t.formateur.leaveSession,
        confirmLabel: t.formateur.leave
      })
      if (ok) _appShortcutLeaveFn()
    }
  }

  trackAppListener(document, 'keydown', keyHandler)
}

/**
 * Test-only reset for the app-shortcut idempotency guard. Production
 * code never calls this — the guard is intentionally sticky for the
 * page lifetime. Tests need it to verify "first attach binds, second
 * attach is a no-op" from a clean slate, AND to prevent the appTracker
 * from accumulating keydown listeners across test cases (the tracker
 * itself is module-level and not touched by cleanupAllListeners).
 */
export function _resetAppShortcutForTests() {
  appTracker.cleanup()
  _appShortcutAttached = false
  _appShortcutLeaveFn = null
}
