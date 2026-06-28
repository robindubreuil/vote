import { COLORS, escapeHtml } from '@shared/colors.js'
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
  upload
} from '@shared/icons.js'
import { renderFooterHTML, renderSessionCodeButton, showConfirmDialog } from '@shared/ui.js'
import { t } from '@shared/i18n.js'
import { createListenerTracker } from '@shared/dom/listeners.js'
import { listPresets } from '@shared/presets.js'
import { state } from './state.js'
import { getCombinations, sortStagiaires, getColorCounts } from './utils.js'
const { track: trackListener, cleanup: cleanupAllListeners } = createListenerTracker()

const _actionHandlers = {}

export function setActionHandlers(handlers) {
  Object.assign(_actionHandlers, handlers)
}

export { cleanupAllListeners }

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
 */
export function updateConnectionBanner() {
  const banner = document.getElementById('reconnect-banner')
  if (!banner) return
  const shouldShow = !!state.sessionCode && state.everConnected && !state.connected
  if (shouldShow) {
    banner.hidden = false
    banner.setAttribute('aria-hidden', 'false')
  } else {
    banner.hidden = true
    banner.setAttribute('aria-hidden', 'true')
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
        title="${t.formateur.openConnectionAidTitle}"
        aria-label="${t.formateur.openConnectionAid}"
      >${qrCode(' class="icon icon-md"')}</button>
      ${renderSessionCodeButton(state.sessionCode, isConnected, `${t.formateur.leaveSessionTitle} — ${t.formateur.shortcutEsc}`)}
    </div>
  `
}

/**
 * Attach header event listeners
 * @param {Object} client
 * @param {Function} renderLandingPageFn
 */
export function attachHeaderListeners(client, leaveSessionFn) {
  const leaveSessionBtn = document.getElementById('leaveSessionBtn')
  if (leaveSessionBtn) {
    trackListener(leaveSessionBtn, 'click', async () => {
      const ok = await showConfirmDialog({
        title: t.formateur.leaveSessionTitle,
        message: t.formateur.leaveSession,
        confirmLabel: t.formateur.leave
      })
      if (ok) leaveSessionFn()
    })
  }

  const aidBtn = document.getElementById('openConnectionAidBtn')
  if (aidBtn && state.sessionCode) {
    trackListener(aidBtn, 'click', () => openConnectionAid(state.sessionCode))
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
        e.preventDefault()
        if (_actionHandlers.confirmSavePreset) _actionHandlers.confirmSavePreset(nameInput.value)
      } else if (e.key === 'Escape') {
        e.preventDefault()
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
  const closeBtn = document.getElementById('closeVote')
  if (closeBtn && _actionHandlers.closeVote) {
    trackListener(closeBtn, 'click', () => _actionHandlers.closeVote(client))
  }

  const newVoteBtn = document.getElementById('newVote')
  if (newVoteBtn && _actionHandlers.resetVote) {
    trackListener(newVoteBtn, 'click', () => _actionHandlers.resetVote(client))
  }
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
      <div class="config-info" aria-live="polite" data-testid="connected-count">${users(' class="icon icon-sm"')} ${state.connectedCount} stagiaire${state.connectedCount > 1 ? 's' : ''} connecté${state.connectedCount > 1 ? 's' : ''}</div>

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
                <span class="color-swatch" style="background-color: ${color.color}"></span>
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
          return c ? `<span class="preset-chip-swatch" style="background-color:${c.color}"></span>` : ''
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

      <div class="button-row">
        ${
          state.voteState === 'active'
            ? `
          <button class="btn btn-danger" id="closeVote" data-testid="close-vote-btn" ${!isConnected ? 'disabled' : ''}>${stop(' class="icon icon-md"')} ${t.formateur.closeVote}</button>
        `
            : `
          <button class="btn btn-success" id="newVote" data-testid="new-vote-btn" ${!isConnected ? 'disabled' : ''}>${refresh(' class="icon icon-md"')} ${t.formateur.newVote}</button>
        `
        }
      </div>
    </div>
  `
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
          <span class="color-bar-swatch" style="background-color: ${color.color}"></span>
          <span class="color-bar-name">${escapeHtml(displayName)}</span>
        </div>
        <div class="color-bar-track">
          <div class="color-bar-fill ${count === 0 ? 'empty' : ''}" style="width: ${percent}%; background-color: ${color.color}"></div>
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
          return `<span class="combo-segment" style="background-color: ${color?.color || '#666'}" title="${color?.name || colorId}"></span>`
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
      const displayName = s.name || t.formateur.anonymous
      const hasVoted = s.vote && s.vote.length > 0
      const isConnected = s.connected

      // Online indicator dot
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

      // Voter: show colors
      const colorsHTML = s.vote
        .map((colorId) => {
          const color = COLORS.find((c) => c.id === colorId)
          return `<span class="stagiaire-vote-swatch" style="background-color: ${color?.color || '#666'}" title="${color?.name || colorId}"></span>`
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
 * Attach keyboard shortcuts for the full app
 * @param {Function} leaveSessionFn
 */
export function attachAppKeyboardShortcuts(leaveSessionFn) {
  const keyHandler = async (e) => {
    // Escape key - leave session with confirmation
    if (e.key === 'Escape' && state.sessionCode) {
      e.preventDefault()
      const ok = await showConfirmDialog({
        title: t.formateur.leaveSessionTitle,
        message: t.formateur.leaveSession,
        confirmLabel: t.formateur.leave
      })
      if (ok) leaveSessionFn()
    }
  }

  trackListener(document, 'keydown', keyHandler)
}
