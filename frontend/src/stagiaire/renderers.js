import { vote, hourglass, pencil, check, stop, gamepad } from '@shared/icons.js'
import { COLORS, escapeHtml } from '@shared/colors.js'
import { renderFooterHTML, renderSessionCodeButton } from '@shared/ui.js'
import { t } from '@shared/strings.js'
import { createListenerTracker } from '@shared/dom/listeners.js'
import { state, AppState } from './state.js'

let handleKeyPress = null

const { track: trackListener, cleanup: cleanupEventListeners } = createListenerTracker()

/**
 * Render the initial layout structure (Header, Main, Footer).
 *
 * Accessibility (F6): the layout owns a single stable `<header>` and a
 * `<main>` landmark. Per-state renderers only return the inner main
 * content; the header is populated by `updateView` via
 * `renderStagiaireHeaderHTML`, so every AppState always has exactly one
 * `<header>` and one `<main>` — the previous design emitted a new
 * `<header>` per state and omitted it entirely from the JOINING and
 * edit-name views, leaving screen-reader users without a banner landmark.
 * @param {HTMLElement} app - The app element
 */
export function renderLayout(app) {
  app.innerHTML = `
    <header class="header" id="app-header" hidden></header>
    <main class="container" id="main-container"></main>
    <div id="game-overlay" class="game-overlay" hidden>
      <div class="game-frame">
        <div class="game-hud">
          <div class="game-hud-stat">
            <span class="game-hud-label">${t.stagiaire.gameBest}</span>
            <span class="game-hud-value" id="gameBest">0</span>
          </div>
          <div class="game-hud-stat game-hud-stat-level">
            <span class="game-hud-label">${t.stagiaire.gameLevel}</span>
            <span class="game-hud-value" id="gameLevel">1</span>
            <div class="game-hud-progress"><div class="game-hud-progress-bar" id="gameLevelProgress"></div></div>
          </div>
          <div class="game-hud-stat">
            <span class="game-hud-label">${t.stagiaire.gameStreak}</span>
            <span class="game-hud-value">
              <span id="gameStreak">0</span><span class="game-mult-badge" id="gameMultBadge" hidden></span>
            </span>
          </div>
          <div class="game-hud-stat">
            <span class="game-hud-label">${t.stagiaire.gameAttempts}</span>
            <span class="game-hud-value" id="gameAttempts">8</span>
          </div>
          <button type="button" class="btn btn-secondary btn-small game-rules-btn" id="gameRulesBtn" aria-label="${t.stagiaire.gameHowToPlay}">
            ?
          </button>
          <button type="button" class="btn btn-secondary btn-small game-quit-btn" id="gameQuitBtn" aria-label="${t.stagiaire.quitGame}">
            ${t.stagiaire.quitGame}
          </button>
        </div>
        <div class="game-board-wrap">
          <ol class="game-board" id="gameBoard" aria-label="${t.stagiaire.gameBoardAriaLabel}"></ol>
          <div class="game-palette" id="gamePalette" aria-label="${t.stagiaire.gamePaletteAriaLabel}"></div>
          <div class="game-actions">
            <button type="button" class="btn btn-secondary" id="gameClearBtn">${t.stagiaire.gameClear}</button>
            <button type="button" class="btn btn-primary btn-large" id="gameSubmitBtn">${t.stagiaire.gameValidate}</button>
          </div>
        </div>
        <div class="game-overlay-screen" id="gamePauseScreen" hidden>
          <div class="game-screen-title">${t.stagiaire.gamePaused}</div>
          <button type="button" class="btn btn-primary" id="gameResumeBtn">${t.stagiaire.gameResume}</button>
          <button type="button" class="btn btn-secondary btn-small" id="gameQuitFromPauseBtn">${t.stagiaire.quitGame}</button>
        </div>
        <div class="game-overlay-screen" id="gameOverScreen" hidden>
          <div class="game-screen-levelup" id="gameLevelUp" hidden></div>
          <div class="game-screen-title" id="gameOverTitle"></div>
          <div class="game-screen-multiplier" id="gameOverMultiplier" hidden></div>
          <div class="game-screen-score" id="gameOverScore"></div>
          <div class="game-screen-best" id="gameOverBest" hidden></div>
          <div class="game-screen-secret" id="gameOverSecret"></div>
          <button type="button" class="btn btn-primary btn-large" id="gameRestartBtn">${t.stagiaire.gameNewGame}</button>
          <button type="button" class="btn btn-secondary btn-small" id="gameQuitFromOverBtn">${t.stagiaire.quitGame}</button>
        </div>
        <div class="game-overlay-screen" id="gameRulesScreen" hidden>
          <div class="game-screen-title">${t.stagiaire.gameRulesTitle}</div>
          <ul class="game-rules-list">
            ${t.stagiaire.gameRules.map((r) => `<li>${r}</li>`).join('')}
          </ul>
          <div class="game-peg-example">
            <span class="game-peg game-peg-black" aria-label="${t.stagiaire.gamePegBlack}"></span>
            <span class="game-peg-label">${t.stagiaire.gamePegBlackDesc}</span>
          </div>
          <div class="game-peg-example">
            <span class="game-peg game-peg-white" aria-label="${t.stagiaire.gamePegWhite}"></span>
            <span class="game-peg-label">${t.stagiaire.gamePegWhiteDesc}</span>
          </div>
          <button type="button" class="btn btn-primary" id="gameRulesCloseBtn">${t.stagiaire.gameRulesOk}</button>
        </div>
      </div>
    </div>
    ${renderFooterHTML()}
  `
}

/**
 * Build the banner header markup based on the current state.
 * Returns '' for states that should not show a header (JOINING, and the
 * edit-name modal which replaces the whole main area). `updateView`
 * hides the header element when this returns empty.
 * @returns {string}
 */
function renderStagiaireHeaderHTML() {
  // The header needs a session code (otherwise there's nothing to show
  // but the title, which the join card already provides).
  if (!state.sessionCode) return ''
  if (state.prenomEdit) return ''
  switch (state.appState) {
    case AppState.WAITING:
    case AppState.VOTING:
    case AppState.VOTED:
    case AppState.CLOSED:
      return `
        <h1>${vote(' class="icon icon-md"')} ${t.common.voteColore}</h1>
        <div class="header-right">
          ${renderSessionCodeButton(state.sessionCode, state.connected)}
        </div>
      `
    default:
      return ''
  }
}

/**
 * Update the main view based on current state
 */
export function updateView() {
  const header = document.getElementById('app-header')
  const container = document.getElementById('main-container')
  if (!container) return

  // Sync the stable header. Hidden when there's no content (JOINING,
  // edit-name modal) so the landmark stays present in the a11y tree
  // without showing an empty banner.
  if (header) {
    const headerHTML = renderStagiaireHeaderHTML()
    header.innerHTML = headerHTML
    header.hidden = !headerHTML
  }

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
 *
 * Accessibility (F8): both inputs reference the error-message element
 * via `aria-describedby` so AT users hear the validation message when
 * they focus the field. `aria-invalid` is toggled by the handlers
 * (`handleJoin`) on validation failure / success.
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
            aria-describedby="join-error"
            aria-invalid="false"
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
            aria-describedby="join-error"
            aria-invalid="false"
          />
        </div>
        <div class="error-message" id="join-error" role="alert" data-testid="error-message"></div>
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
 *
 * Accessibility (F8): the input references an inline error element via
 * `aria-describedby`. The handler (`handleEditName`) toggles
 * `aria-invalid` on validation failure and clears it on success.
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
            aria-describedby="edit-name-error"
            aria-invalid="false"
          />
        </div>
        <div class="error-message" id="edit-name-error" role="alert" data-testid="edit-name-error"></div>
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
 * Render single choice voting buttons.
 *
 * Accessibility (F7): the wrapper exposes `role="radiogroup"` + an
 * `aria-label` so screen-reader users hear the group context before the
 * options. Each button uses `role="radio"` + `aria-checked` (rather than
 * the toggle pattern `aria-pressed`) so the semantics match the
 * single-select behaviour: picking one option clears the others.
 *
 * Accessibility (A3): the group uses the WAI-ARIA APG roving-tabindex
 * pattern — exactly one radio is in the tab order (`tabindex="0"`, the
 * checked one or the first), the rest carry `tabindex="-1"`. Arrow /
 * Home / End keys (handled in `handleSingleChoiceKeydown`) move between
 * radios; Tab leaves the group. This honours the implicit contract of
 * `role="radiogroup"` and gives keyboard users the same nav model as
 * native radio inputs.
 */
function renderSingleChoiceHTML(activeColors) {
  const isConnected = state.connected
  // Single-choice → at most one selected. The tab stop is the checked
  // radio; when nothing is checked yet it falls back to the first so the
  // group always has exactly one focusable entry.
  const tabStopId = [...state.selectedColors][0] || activeColors[0]?.id
  return `
    <div class="vote-grid" role="radiogroup" aria-label="${t.stagiaire.singleChoiceGroupLabel}" data-count="${activeColors.length}">
      ${activeColors
        .map((color) => {
          const label = state.colorLabels[color.id] || color.name
          const selected = state.selectedColors.has(color.id)
          const tabIndex = color.id === tabStopId ? 0 : -1
          return `
        <button
          type="button"
          class="vote-button bg-${color.id} ${selected ? 'selected' : ''}"
          data-color="${color.id}"
          data-testid="vote-btn-${color.id}"
          role="radio"
          aria-checked="${selected}"
          aria-label="${label}"
          tabindex="${tabIndex}"
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
 * Render multiple choice voting checkboxes.
 *
 * Accessibility (F7): the options live inside a `<fieldset>` whose
 * `<legend>` carries the instruction. Native checkboxes already expose
 * the multi-select semantics, so the fieldset/legend pair only adds the
 * programmatic group label that was previously missing.
 */
function renderMultipleChoiceHTML(activeColors) {
  const isConnected = state.connected
  return `
    <fieldset class="vote-grid vote-fieldset" data-count="${activeColors.length}">
      <legend class="vote-fieldset-legend">${t.stagiaire.multipleChoiceGroupLabel}</legend>
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
    </fieldset>
    <button type="button" class="btn btn-success btn-large" id="submitVote" data-testid="submit-vote-btn" ${state.selectedColors.size === 0 || !isConnected ? 'disabled' : ''}>
      ${t.stagiaire.validateVote}
    </button>
    ${
      state.allowBlank
        ? `
    <button type="button" class="btn btn-secondary" id="blankVoteBtn" data-testid="blank-vote-btn" ${!isConnected ? 'disabled' : ''}>
      ${t.formateur.blankVote}
    </button>
    `
        : ''
    }
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
  const competitiveHTML =
    state.competitive && state.revealed
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

  // A3: single-choice radiogroup — arrow / Home / End key navigation
  // (roving tabindex). The handler lives on the radiogroup container
  // so it catches keys regardless of which radio currently holds focus.
  document.querySelectorAll('.vote-grid[role="radiogroup"]').forEach((rg) => {
    trackListener(rg, 'keydown', handleSingleChoiceKeydown)
  })

  // Checkboxes (choix multiple)
  // A2: native <input type="checkbox"> is the sole tab target. The
  // sibling <label for=...> used to carry tabindex="0" + a manual
  // Space/Enter keydown handler, giving keyboard users two tab stops
  // per color (16 stops for an 8-color palette) and duplicating the
  // checkbox's own Space/Enter behaviour. Dropping both leaves the
  // native checkbox as the single focusable control; the label stays
  // clickable (mouse + touch) via its for= association.
  document.querySelectorAll('.vote-checkbox').forEach((checkbox) => {
    trackListener(checkbox, 'change', handleCheckboxChange)
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
  handleSingleChoiceKeydown,
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
    handleSingleChoiceKeydown,
    handleCheckboxChange,
    handleSubmitVote,
    handleBlankVote,
    leaveSession,
    handleKeyPress,
    handlePlayGame
  } = handlers)
}
