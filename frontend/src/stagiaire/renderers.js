import { vote, hourglass, pencil, check, stop, gamepad } from '@shared/icons.js'
import { COLORS, escapeHtml } from '@shared/colors.js'
import { renderFooterHTML, renderSessionCodeButton } from '@shared/ui.js'
import { t } from '@shared/i18n.js'
import { createListenerTracker } from '@shared/dom/listeners.js'
import { state, AppState } from './state.js'

let handleKeyPress = null

const { track: trackListener, cleanup: cleanupEventListeners } = createListenerTracker()

/**
 * Render the initial layout structure (Header, Main, Footer)
 * @param {HTMLElement} app - The app element
 */
export function renderLayout(app) {
  app.innerHTML = `
    <div class="container" id="main-container"></div>
    <div id="game-overlay" class="game-overlay" hidden>
      <div class="game-frame">
        <div class="game-hud">
          <div class="game-hud-stat">
            <span class="game-hud-label">${t.stagiaire?.gameBest || 'Record'}</span>
            <span class="game-hud-value" id="gameBest">0</span>
          </div>
          <div class="game-hud-stat game-hud-stat-level">
            <span class="game-hud-label">${t.stagiaire?.gameLevel || 'Niveau'}</span>
            <span class="game-hud-value" id="gameLevel">1</span>
            <div class="game-hud-progress"><div class="game-hud-progress-bar" id="gameLevelProgress"></div></div>
          </div>
          <div class="game-hud-stat">
            <span class="game-hud-label">${t.stagiaire?.gameStreak || 'Série'}</span>
            <span class="game-hud-value">
              <span id="gameStreak">0</span><span class="game-mult-badge" id="gameMultBadge" hidden></span>
            </span>
          </div>
          <div class="game-hud-stat">
            <span class="game-hud-label">${t.stagiaire?.gameAttempts || 'Coups restants'}</span>
            <span class="game-hud-value" id="gameAttempts">8</span>
          </div>
          <button type="button" class="btn btn-secondary btn-small game-rules-btn" id="gameRulesBtn" aria-label="${t.stagiaire?.gameHowToPlay || 'Comment jouer'}">
            ?
          </button>
          <button type="button" class="btn btn-secondary btn-small game-quit-btn" id="gameQuitBtn" aria-label="${t.stagiaire?.quitGame || 'Quitter'}">
            ${t.stagiaire?.quitGame || 'Quitter'}
          </button>
        </div>
        <div class="game-board-wrap">
          <ol class="game-board" id="gameBoard" aria-label="Plateau de jeu"></ol>
          <div class="game-palette" id="gamePalette" aria-label="Palette de couleurs"></div>
          <div class="game-actions">
            <button type="button" class="btn btn-secondary" id="gameClearBtn">${t.stagiaire?.gameClear || 'Effacer'}</button>
            <button type="button" class="btn btn-primary btn-large" id="gameSubmitBtn">${t.stagiaire?.gameValidate || 'Valider'}</button>
          </div>
        </div>
        <div class="game-overlay-screen" id="gamePauseScreen" hidden>
          <div class="game-screen-title">${t.stagiaire?.gamePaused || 'Pause'}</div>
          <button type="button" class="btn btn-primary" id="gameResumeBtn">${t.stagiaire?.gameResume || 'Reprendre'}</button>
          <button type="button" class="btn btn-secondary btn-small" id="gameQuitFromPauseBtn">${t.stagiaire?.quitGame || 'Quitter'}</button>
        </div>
        <div class="game-overlay-screen" id="gameOverScreen" hidden>
          <div class="game-screen-levelup" id="gameLevelUp" hidden></div>
          <div class="game-screen-title" id="gameOverTitle"></div>
          <div class="game-screen-multiplier" id="gameOverMultiplier" hidden></div>
          <div class="game-screen-score" id="gameOverScore"></div>
          <div class="game-screen-best" id="gameOverBest" hidden></div>
          <div class="game-screen-secret" id="gameOverSecret"></div>
          <button type="button" class="btn btn-primary btn-large" id="gameRestartBtn">${t.stagiaire?.gameNewGame || 'Nouvelle partie'}</button>
          <button type="button" class="btn btn-secondary btn-small" id="gameQuitFromOverBtn">${t.stagiaire?.quitGame || 'Quitter'}</button>
        </div>
        <div class="game-overlay-screen" id="gameRulesScreen" hidden>
          <div class="game-screen-title">${t.stagiaire?.gameRulesTitle || 'Comment jouer'}</div>
          <ul class="game-rules-list">
            ${(t.stagiaire?.gameRules || []).map((r) => `<li>${r}</li>`).join('')}
          </ul>
          <div class="game-peg-example">
            <span class="game-peg game-peg-black" aria-label="Pion doré"></span>
            <span class="game-peg-label">bonne couleur, bonne position</span>
          </div>
          <div class="game-peg-example">
            <span class="game-peg game-peg-white" aria-label="Pion blanc"></span>
            <span class="game-peg-label">bonne couleur, mauvaise position</span>
          </div>
          <button type="button" class="btn btn-primary" id="gameRulesCloseBtn">OK</button>
        </div>
      </div>
    </div>
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
      <form class="join-form" id="joinForm" data-testid="join-form">
        <div class="input-group">
          <label for="prenom">${t.stagiaire.yourName}</label>
          <input
            type="text"
            id="prenom"
            data-testid="name-input"
            class="session-input"
            placeholder="${t.stagiaire.exMarie}"
            value="${escapeHtml(state.prenom)}"
            autocomplete="name"
            autocapitalize="words"
            maxlength="16"
          />
        </div>
        <div class="input-group">
          <label for="sessionCode">${t.common.sessionCode}</label>
          <input
            type="text"
            id="sessionCode"
            data-testid="session-code-input"
            class="session-input"
            placeholder="ABC"
            maxlength="3"
            inputmode="text"
            autocapitalize="characters"
            pattern="[A-HJ-NP-Ya-hj-np-y]{3}"
            value="${escapeHtml(state.sessionCode)}"
            autocomplete="off"
          />
        </div>
        <div class="error-message" role="alert" data-testid="error-message"></div>
        <button type="submit" class="btn btn-primary btn-large" data-testid="join-btn">
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
        <div class="waiting-text" aria-live="polite" data-testid="waiting-text">${t.stagiaire.waiting}</div>
        <div class="waiting-name" data-testid="waiting-name">${t.stagiaire.hello}, <strong>${escapeHtml(state.prenom)}</strong> !</div>
        <button type="button" class="btn btn-secondary btn-small" id="editNameBtn" data-testid="edit-name-btn" aria-label="${t.stagiaire.modifyName}">
          ${pencil(' class="icon icon-sm"')} ${t.stagiaire.modifyName}
        </button>
        ${renderPlayGameButtonHTML()}
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
      <form class="join-form" id="editNameForm" data-testid="edit-name-form">
        <div class="input-group">
          <label for="editPrenom">${t.stagiaire.yourNameInformal}</label>
          <input
            type="text"
            id="editPrenom"
            data-testid="edit-name-input"
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
  const activeColors = COLORS.filter((c) => state.availableColors.includes(c.id))

  return `
    <header class="header">
      <h1>${vote(' class="icon icon-md"')} ${t.common.voteColore}</h1>
      <div class="header-right">
        ${renderSessionCodeButton(state.sessionCode, state.connected)}
      </div>
    </header>
    <div class="card">
      <h2 class="card-title">${t.stagiaire.voteNow}</h2>
      <p class="vote-instruction ${state.multipleChoice ? 'multiple-choice' : 'single-choice'}" data-testid="vote-instruction" aria-live="polite">
        ${state.multipleChoice ? t.stagiaire.multipleChoice : t.stagiaire.singleChoice}
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
    <div class="vote-grid" data-count="${activeColors.length}">
      ${activeColors
        .map((color) => {
          const label = state.colorLabels[color.id] || color.name
          return `
        <button
          type="button"
          class="vote-button bg-${color.id} ${state.selectedColors.has(color.id) ? 'selected' : ''}"
          data-color="${color.id}"
          data-testid="vote-btn-${color.id}"
          aria-pressed="${state.selectedColors.has(color.id)}"
          aria-label="${label}"
          ${!isConnected ? 'disabled' : ''}
        >
          ${escapeHtml(label)}
        </button>
        `
        })
        .join('')}
    </div>
  `
}

/**
 * Render multiple choice voting checkboxes
 */
function renderMultipleChoiceHTML(activeColors) {
  const isConnected = state.connected
  return `
    <div class="vote-grid" data-count="${activeColors.length}">
      ${activeColors
        .map((color) => {
          const label = state.colorLabels[color.id] || color.name
          return `
        <input
          type="checkbox"
          id="color-${color.id}"
          class="vote-checkbox"
          data-testid="vote-checkbox-${color.id}"
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
        })
        .join('')}
    </div>
    <button type="button" class="btn btn-success btn-large" id="submitVote" data-testid="submit-vote-btn" ${state.selectedColors.size === 0 || !isConnected ? 'disabled' : ''}>
      ${t.stagiaire.validateVote}
    </button>
    ${state.allowBlank ? `
    <button type="button" class="btn btn-secondary" id="blankVoteBtn" data-testid="blank-vote-btn" ${!isConnected ? 'disabled' : ''}>
      ${t.formateur.blankVote}
    </button>
    ` : ''}
  `
}

/**
 * Render the voted state
 */
function renderVotedHTML() {
  const selectedNames = Array.from(state.selectedColors)
    .map((id) => {
      if (id === 'blank') return t.formateur.blankVote
      return state.colorLabels[id] || COLORS.find((c) => c.id === id)?.name || id
    })
    .join(' + ')

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
        <div class="voted-title" aria-live="polite" aria-atomic="true" data-testid="voted-title">${t.stagiaire.voteRecorded}</div>
        <div class="voted-subtitle" data-testid="voted-subtitle">${escapeHtml(selectedNames)}</div>
        <button type="button" class="btn btn-secondary btn-small" id="changeVoteBtn" data-testid="change-vote-btn" style="margin-top: 1rem;">
          ${pencil(' class="icon icon-sm"')} ${t.stagiaire.modifyVote}
        </button>
        ${renderPlayGameButtonHTML()}
      </div>
    </div>
  `
}

/**
 * Render the vote closed state
 */
function renderClosedHTML() {
  const competitiveHTML = state.competitive && state.revealed
    ? (() => {
        const positive = state.voteScore >= 0
        const scoreText = positive ? `+${state.voteScore}` : String(state.voteScore)
        const rankSuffix = state.rank === 1 ? 'er' : 'e'
        return `
        <div class="score-feedback ${positive ? 'positive' : 'negative'}">
          <div class="score-feedback-vote">${scoreText} pts</div>
          <div class="score-feedback-total">${t.stagiaire.totalScoreLabel || 'Total'} : ${state.totalScore} pts</div>
          <div class="score-feedback-rank">${state.rank}${rankSuffix} / ${state.totalStagiaires}</div>
        </div>
      `
      })()
    : ''

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
        <div class="waiting-text" data-testid="vote-closed-text">${t.stagiaire.voteClosed}</div>
        ${competitiveHTML}
      </div>
    </div>
  `
}

/**
 * The "play the mini-game" CTA. Only rendered when the trainer enabled
 * the feature for this vote session.
 */
function renderPlayGameButtonHTML() {
  if (!state.gameEnabled) return ''
  return `
    <button type="button" class="btn btn-game btn-small" id="playGameBtn" data-testid="play-game-btn">
      ${gamepad(' class="icon icon-sm"')} ${t.stagiaire.playGame}
    </button>
  `
}

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
  document.querySelectorAll('.vote-button').forEach((btn) => {
    trackListener(btn, 'click', handleSingleChoiceVote)
  })

  // Checkboxes (choix multiple)
  document.querySelectorAll('.vote-checkbox').forEach((checkbox) => {
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

  // Bouton vote blanc
  const blankBtn = document.getElementById('blankVoteBtn')
  if (blankBtn) {
    trackListener(blankBtn, 'click', () => {
      if (handleBlankVote) handleBlankVote()
    })
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

  // Mini-jeu — bouton "Jouer" dans les écrans d'attente
  const playGameBtn = document.getElementById('playGameBtn')
  if (playGameBtn) {
    trackListener(playGameBtn, 'click', () => handlePlayGame && handlePlayGame())
  }

  // Keyboard shortcuts
  if (handleKeyPress) {
    trackListener(document, 'keydown', handleKeyPress)
  }
}

export { cleanupEventListeners }

let handleJoin,
  handleEditName,
  handleSingleChoiceVote,
  handleCheckboxChange,
  handleSubmitVote,
  handleBlankVote,
  leaveSession,
  handlePlayGame

export function setHandlers(handlers) {
  ;({
    handleJoin,
    handleEditName,
    handleSingleChoiceVote,
    handleCheckboxChange,
    handleSubmitVote,
    handleBlankVote,
    leaveSession,
    handleKeyPress,
    handlePlayGame
  } = handlers)
}
