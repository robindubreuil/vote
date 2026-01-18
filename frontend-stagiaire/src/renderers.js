import { vote, hourglass, pencil, check, stop } from '../../shared/icons.js'
import { COLORS, escapeHtml } from '../../shared/colors.js'
import { renderFooterHTML, renderSessionCodeButton } from '../../shared/ui.js'
import { t } from '../../shared/i18n.js'
import { state, AppState } from './state.js'

// Keyboard shortcuts handler reference
let handleKeyPress = null

// Track all event listeners for cleanup
const currentListeners = new Set()

/**
 * Render the initial layout structure (Header, Main, Footer)
 * @param {HTMLElement} app - The app element
 */
export function renderLayout(app) {
  app.innerHTML = `
    <div class="container" id="main-container"></div>
    ${renderFooterHTML()}
  `
}

/**
 * Update the main view based on current state
 */
export function updateView() {
  const container = document.getElementById('main-container')
  if (!container) return

  // Sauvegarde du focus
  const activeElementId = document.activeElement?.id

  // Rendu du contenu en fonction de l'état
  let contentHTML = ''

  if (state.prenomEdit) {
    contentHTML = renderEditNameHTML()
  } else {
    switch (state.appState) {
      case AppState.JOINING:
        contentHTML = renderJoinHTML()
        break
      case AppState.WAITING:
        contentHTML = renderWaitingHTML()
        break
      case AppState.VOTING:
        contentHTML = renderVotingHTML()
        break
      case AppState.VOTED:
        contentHTML = renderVotedHTML()
        break
      case AppState.CLOSED:
        contentHTML = renderClosedHTML()
        break
    }
  }

  // On remplace le contenu
  container.innerHTML = contentHTML

  // Réattacher les écouteurs
  // Clean up old listeners before attaching new ones
  cleanupEventListeners()

  attachEventListeners()

  // Restauration du focus (best effort)
  if (activeElementId) {
    const el = document.getElementById(activeElementId)
    if (el) {
      el.focus()
      // Place cursor at end if it's an input
      if (el.tagName === 'INPUT' && el.type === 'text') {
        const val = el.value
        el.value = ''
        el.value = val
      }
    }
  }
}

/**
 * Alias for updateView for compatibility
 */
export function render() {
  updateView()
}

/**
 * Render the join session form
 */
function renderJoinHTML() {
  return `
    <div class="card">
      <h2 class="card-title">${vote(' class="icon icon-md"')} ${t.common.voteColore}</h2>
      <form class="join-form" id="joinForm">
        <div class="input-group">
          <label for="prenom">${t.stagiaire.yourName}</label>
          <input
            type="text"
            id="prenom"
            class="session-input"
            placeholder="${t.stagiaire.exMarie}"
            value="${escapeHtml(state.prenom)}"
            autocomplete="name"
            autocapitalize="words"
            maxlength="16"
            required
          />
        </div>
        <div class="input-group">
          <label for="sessionCode">${t.common.sessionCode}</label>
          <input
            type="text"
            id="sessionCode"
            class="session-input"
            placeholder="0000"
            maxlength="4"
            pattern="[0-9]{4}"
            inputmode="numeric"
            value="${escapeHtml(state.sessionCode)}"
            autocomplete="off"
          />
        </div>
        <div class="error-message" role="alert"></div>
        <button type="submit" class="btn btn-primary btn-large">
          ${t.stagiaire.join}
        </button>
      </form>
    </div>
  `
}

/**
 * Render the waiting state
 */
function renderWaitingHTML() {
  return `
    <header class="header">
      <h1>${vote(' class="icon icon-md"')} ${t.common.voteColore}</h1>
      <div class="header-right">
        ${renderSessionCodeButton(state.sessionCode, state.connected)}
      </div>
    </header>
    <div class="card">
      <div class="waiting-state">
        <div class="waiting-icon">${hourglass(' class="icon icon-xl"')}</div>
        <div class="waiting-text" aria-live="polite">${t.stagiaire.waiting}</div>
        <div class="waiting-name">${t.stagiaire.hello}, <strong>${escapeHtml(state.prenom)}</strong> !</div>
        <button type="button" class="btn btn-secondary btn-small" id="editNameBtn" aria-label="${t.stagiaire.modifyName}">
          ${pencil(' class="icon icon-sm"')} ${t.stagiaire.modifyName}
        </button>
      </div>
    </div>
  `
}

/**
 * Render the edit name form
 */
function renderEditNameHTML() {
  return `
    <div class="card edit-name-modal">
      <h2 class="card-title">${t.stagiaire.modifyName}</h2>
      <form class="join-form" id="editNameForm">
        <div class="input-group">
          <label for="editPrenom">${t.stagiaire.yourNameInformal}</label>
          <input
            type="text"
            id="editPrenom"
            class="session-input"
            placeholder="${t.stagiaire.exMarie}"
            value="${escapeHtml(state.prenom)}"
            autocomplete="name"
            autocapitalize="words"
            maxlength="16"
            required
            autofocus
          />
        </div>
        <div class="button-row">
          <button type="button" class="btn btn-secondary" id="cancelEditName">${t.common.cancel}</button>
          <button type="submit" class="btn btn-primary">${t.common.save}</button>
        </div>
      </form>
    </div>
  `
}

/**
 * Render the voting interface
 */
function renderVotingHTML() {
  const activeColors = COLORS.filter(c => state.availableColors.includes(c.id))

  return `
    <header class="header">
      <h1>${vote(' class="icon icon-md"')} ${t.common.voteColore}</h1>
      <div class="header-right">
        ${renderSessionCodeButton(state.sessionCode, state.connected)}
      </div>
    </header>
    <div class="card">
      <h2 class="card-title">${t.stagiaire.voteNow}</h2>
      <p class="vote-instruction ${state.multipleChoice ? 'multiple-choice' : 'single-choice'}" aria-live="polite">
        ${state.multipleChoice
          ? t.stagiaire.multipleChoice
          : t.stagiaire.singleChoice
        }
      </p>

      ${state.multipleChoice ? renderMultipleChoiceHTML(activeColors) : renderSingleChoiceHTML(activeColors)}
    </div>
  `
}

/**
 * Render single choice voting buttons
 */
function renderSingleChoiceHTML(activeColors) {
  const isConnected = state.connected
  return `
    <div class="vote-grid">
      ${activeColors.map(color => {
        const label = state.colorLabels[color.id] || color.name
        return `
        <button
          type="button"
          class="vote-button bg-${color.id} ${state.selectedColors.has(color.id) ? 'selected' : ''}"
          data-color="${color.id}"
          aria-pressed="${state.selectedColors.has(color.id)}"
          aria-label="${label}"
          ${!isConnected ? 'disabled' : ''}
        >
          ${escapeHtml(label)}
        </button>
        `
      }).join('')}
    </div>
  `
}

/**
 * Render multiple choice voting checkboxes
 */
function renderMultipleChoiceHTML(activeColors) {
  const isConnected = state.connected
  return `
    <div class="vote-grid">
      ${activeColors.map(color => {
        const label = state.colorLabels[color.id] || color.name
        return `
        <input
          type="checkbox"
          id="color-${color.id}"
          class="vote-checkbox"
          value="${color.id}"
          ${state.selectedColors.has(color.id) ? 'checked' : ''}
          ${!isConnected ? 'disabled' : ''}
        />
        <label
          for="color-${color.id}"
          class="vote-checkbox-label bg-${color.id} ${state.selectedColors.has(color.id) ? 'selected' : ''} ${!isConnected ? 'disabled' : ''}"
        >
          ${escapeHtml(label)}
          <span class="check-indicator"></span>
        </label>
        `
      }).join('')}
    </div>
    <button type="button" class="btn btn-success btn-large" id="submitVote" ${state.selectedColors.size === 0 || !isConnected ? 'disabled' : ''}>
      ${t.stagiaire.validateVote}
    </button>
  `
}

/**
 * Render the voted state
 */
function renderVotedHTML() {
  const selectedNames = Array.from(state.selectedColors).map(id => {
    return state.colorLabels[id] || COLORS.find(c => c.id === id)?.name || id
  }).join(' + ')

  return `
    <header class="header">
      <h1>${vote(' class="icon icon-md"')} ${t.common.voteColore}</h1>
      <div class="header-right">
        ${renderSessionCodeButton(state.sessionCode, state.connected)}
      </div>
    </header>
    <div class="card">
      <div class="voted-state">
        <div class="voted-icon">${check(' class="icon icon-xl"')}</div>
        <div class="voted-title" aria-live="polite" aria-atomic="true">${t.stagiaire.voteRecorded}</div>
        <div class="voted-subtitle">${escapeHtml(selectedNames)}</div>
        <button type="button" class="btn btn-secondary btn-small" id="changeVoteBtn" style="margin-top: 1rem;">
          ${pencil(' class="icon icon-sm"')} ${t.stagiaire.modifyVote}
        </button>
      </div>
    </div>
  `
}

/**
 * Render the vote closed state
 */
function renderClosedHTML() {
  return `
    <header class="header">
      <h1>${vote(' class="icon icon-md"')} ${t.common.voteColore}</h1>
      <div class="header-right">
        ${renderSessionCodeButton(state.sessionCode, state.connected)}
      </div>
    </header>
    <div class="card">
      <div class="vote-closed-state">
        <div class="closed-icon">${stop(' class="icon icon-xl"')}</div>
        <div class="waiting-text">${t.stagiaire.voteClosed}</div>
      </div>
    </div>
  `
}

/**
 * Helper function to add and track event listeners
 * @param {Element|string} target - Element or selector
 * @param {string} event - Event name
 * @param {Function} handler - Event handler
 * @param {boolean} useSelector - If true, target is a selector string
 */
function trackListener(target, event, handler, useSelector = false) {
  const element = useSelector ? document.querySelector(target) : target
  if (!element) return

  element.addEventListener(event, handler)
  currentListeners.add({ element, event, handler })
}

/**
 * Helper function to add and track event listeners to multiple elements
 * @param {string} selector - CSS selector
 * @param {string} event - Event name
 * @param {Function} handler - Event handler
 */
function trackListenersForAll(selector, event, handler) {
  document.querySelectorAll(selector).forEach(element => {
    element.addEventListener(event, handler)
    currentListeners.add({ element, event, handler })
  })
}

/**
 * Attach all event listeners to DOM elements
 */
export function attachEventListeners() {
  // Input Binding (plus explicite pour éviter les pertes d'état)
  const prenomInput = document.getElementById('prenom')
  if (prenomInput) {
    trackListener(prenomInput, 'input', (e) => {
      state.prenom = e.target.value
    })
  }

  const editPrenomInput = document.getElementById('editPrenom')
  if (editPrenomInput) {
    trackListener(editPrenomInput, 'input', (e) => {
      state.prenom = e.target.value
    })
  }

  const sessionCodeInput = document.getElementById('sessionCode')
  if (sessionCodeInput) {
    trackListener(sessionCodeInput, 'input', (e) => {
      state.sessionCode = e.target.value
    })
  }

  // Formulaire de connexion
  const joinForm = document.getElementById('joinForm')
  if (joinForm) {
    trackListener(joinForm, 'submit', handleJoin)
  }

  // Formulaire d'édition du nom
  const editNameForm = document.getElementById('editNameForm')
  if (editNameForm) {
    trackListener(editNameForm, 'submit', handleEditName)
  }

  // Bouton d'édition du nom
  const editNameBtn = document.getElementById('editNameBtn')
  if (editNameBtn) {
    trackListener(editNameBtn, 'click', () => {
      state.prenomEdit = true
      render()
    })
  }

  // Bouton annuler l'édition
  const cancelEditBtn = document.getElementById('cancelEditName')
  if (cancelEditBtn) {
    trackListener(cancelEditBtn, 'click', () => {
      state.prenomEdit = false
      render()
    })
  }

  // Boutons de vote (choix unique)
  document.querySelectorAll('.vote-button').forEach(btn => {
    trackListener(btn, 'click', handleSingleChoiceVote)
    // Accessibility: Activate on Enter/Space
    trackListener(btn, 'keydown', (e) => {
      if (e.key === 'Enter' || e.key === ' ') {
        e.preventDefault()
        handleSingleChoiceVote(e)
      }
    })
  })

  // Checkboxes (choix multiple)
  document.querySelectorAll('.vote-checkbox').forEach(checkbox => {
    trackListener(checkbox, 'change', handleCheckboxChange)
    // Accessibility for label
    const label = document.querySelector(`label[for="${checkbox.id}"]`)
    if (label) {
      label.setAttribute('tabindex', '0')
      trackListener(label, 'keydown', (e) => {
        if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault()
          checkbox.checked = !checkbox.checked
          // Trigger change event manually
          const event = new Event('change')
          checkbox.dispatchEvent(event)
        }
      })
    }
  })

  // Bouton valider (choix multiple)
  const submitBtn = document.getElementById('submitVote')
  if (submitBtn) {
    trackListener(submitBtn, 'click', handleSubmitVote)
  }

  // Bouton modifier le vote
  const changeVoteBtn = document.getElementById('changeVoteBtn')
  if (changeVoteBtn) {
    trackListener(changeVoteBtn, 'click', () => {
      state.appState = AppState.VOTING
      state.hasVoted = false
      render()
    })
  }

  // Bouton quitter la session
  const leaveSessionBtn = document.getElementById('leaveSessionBtn')
  if (leaveSessionBtn) {
    trackListener(leaveSessionBtn, 'click', leaveSession)
  }

  // Keyboard shortcuts
  if (handleKeyPress) {
    trackListener(document, 'keydown', handleKeyPress)
  }
}

/**
 * Remove all tracked event listeners
 */
export function cleanupEventListeners() {
  for (const { element, event, handler } of currentListeners) {
    element.removeEventListener(event, handler)
  }
  currentListeners.clear()
}

// Import handlers to avoid circular dependency
// These will be passed in from handlers.js
let handleJoin, handleEditName, handleSingleChoiceVote, handleCheckboxChange, handleSubmitVote, leaveSession

/**
 * Set the handler functions (called from handlers.js)
 */
export function setHandlers(handlers) {
  ({ handleJoin, handleEditName, handleSingleChoiceVote, handleCheckboxChange, handleSubmitVote, leaveSession, handleKeyPress } = handlers)
}
