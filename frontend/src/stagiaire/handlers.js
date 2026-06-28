import { validateName, validateSessionCode } from '@shared/validation.js'
import { showError, showConfirmDialog } from '@shared/ui.js'
import { loader } from '@shared/icons.js'
import { CONSTANTS } from '@shared/config.js'
import { t } from '@shared/i18n.js'
import { state, AppState } from './state.js'
import { render } from './renderers.js'
import { getClient } from './websocket.js'
import { Mastermind, getDifficulty, getLevelProgress, streakMultiplier } from './game.js'
import { loadHighScore, hasSeenRules, markRulesSeen } from '@shared/game-storage.js'
import { COLORS, escapeHtml } from '@shared/colors.js'
import { safeLocalSet, safeSessionRemove } from '@shared/utils/safe-storage.js'

// connectToSession function - will be set by main.js
let connectToSessionFn = null

// Active Mastermind instance. Module-local so the websocket layer can
// pause the game on incoming vote events without going through render().
let game = null
let pendingPegAnimation = false
let scoreAnimId = null

export function setConnectToSession(fn) {
  connectToSessionFn = fn
}

function getOverlay() {
  return document.getElementById('game-overlay')
}

function showOverlayScreen(id) {
  for (const sid of ['gamePauseScreen', 'gameOverScreen', 'gameRulesScreen']) {
    const el = document.getElementById(sid)
    if (el) el.hidden = sid !== id
  }
}

function hideAllOverlayScreens() {
  for (const sid of ['gamePauseScreen', 'gameOverScreen', 'gameRulesScreen']) {
    const el = document.getElementById(sid)
    if (el) el.hidden = true
  }
}

function createGame() {
  const diff = getDifficulty(loadHighScore())
  return new Mastermind({
    colors: COLORS.slice(0, diff.paletteSize).map((c) => ({ id: c.id, color: c.color, name: c.name })),
    codeLength: 4,
    maxAttempts: 8,
    level: diff.level
  })
}

/**
 * Render the current Mastermind state into the overlay board + palette.
 * Called after every place/clear/submit.
 */
function renderBoard() {
  if (!game) return
  const boardState = game.getBoardState()

  const bestEl = document.getElementById('gameBest')
  if (bestEl) bestEl.textContent = String(boardState.best)
  const levelEl = document.getElementById('gameLevel')
  if (levelEl) levelEl.textContent = String(boardState.level)
  const progEl = document.getElementById('gameLevelProgress')
  if (progEl) {
    const prog = getLevelProgress(boardState.best)
    progEl.style.width = `${prog.pct}%`
    progEl.title = prog.toNext > 0 ? `Plus que ${prog.toNext} pts → Niveau ${boardState.level + 1}` : 'Niveau maximum'
  }
  const streakEl = document.getElementById('gameStreak')
  if (streakEl) streakEl.textContent = String(boardState.streak)
  const multBadge = document.getElementById('gameMultBadge')
  if (multBadge) {
    const mult = streakMultiplier(boardState.streak)
    if (mult > 1) {
      const label = mult >= 3 ? '×3' : `×${mult.toString().replace(/\.?0+$/, '')}`
      multBadge.textContent = label
      multBadge.hidden = false
    } else {
      multBadge.hidden = true
    }
  }
  const attemptsEl = document.getElementById('gameAttempts')
  if (attemptsEl) attemptsEl.textContent = String(boardState.attemptsLeft)

  const board = document.getElementById('gameBoard')
  if (!board) return
  const rows = []
  for (let i = boardState.guesses.length - 1; i >= 0; i--) {
    const animate = pendingPegAnimation && i === boardState.guesses.length - 1
    rows.push(renderPastRow(boardState.guesses[i], boardState.pegs[i], boardState.codeLength, i, animate))
  }
  pendingPegAnimation = false
  if (boardState.status === 'playing') {
    rows.push(renderCurrentRow(boardState.currentRow, boardState.codeLength, boardState.guesses.length))
    const remaining = boardState.attemptsLeft - 1
    for (let i = 0; i < remaining; i++) {
      rows.push(renderEmptyRow(boardState.codeLength))
    }
  }
  board.innerHTML = rows.join('')

  // Palette: tappable colors
  const palette = document.getElementById('gamePalette')
  if (palette) {
    palette.innerHTML = boardState.palette
      .map(
        (c) => `
        <button type="button"
          class="game-color-chip bg-${c.id}"
          data-color="${c.id}"
          data-testid="game-color-${c.id}"
          aria-label="${escapeHtml(c.name)}"
          ${boardState.status !== 'playing' ? 'disabled' : ''}
        ></button>`
      )
      .join('')
    palette.querySelectorAll('.game-color-chip').forEach((btn) => {
      btn.onclick = () => handleColorPlace(btn.dataset.color)
    })
  }

  // Submit + clear buttons
  const submitBtn = document.getElementById('gameSubmitBtn')
  const clearBtn = document.getElementById('gameClearBtn')
  const rowFull = boardState.currentRow.every((c) => c !== null)
  if (submitBtn) submitBtn.disabled = boardState.status !== 'playing' || !rowFull
  if (clearBtn) clearBtn.disabled = boardState.status !== 'playing' || boardState.currentRow.every((c) => c === null)

  // Slot taps: clear individual slot
  board.querySelectorAll('.game-slot[data-slot]').forEach((slot) => {
    slot.onclick = () => {
      if (game && game.status === 'playing') {
        game.clear(Number(slot.dataset.slot))
        renderBoard()
      }
    }
  })

  // End-of-round screen
  if (boardState.status !== 'playing') {
    showEndScreen(boardState)
  }
}

function renderPastRow(guess, peg, codeLength, rowIndex, animate = false) {
  const slots = guess
    .map(
      (id) => `
      <span class="game-slot game-slot-filled bg-${id}" aria-label="Couleur placée"></span>`
    )
    .join('')
  const pegs = []
  let di = 0
  for (let i = 0; i < peg.black; i++) {
    const cls = animate ? 'game-peg game-peg-black game-peg-pop' : 'game-peg game-peg-black'
    const style = animate ? ` style="animation-delay:${di * 80}ms"` : ''
    pegs.push(`<span class="${cls}"${style} aria-label="Pion doré"></span>`)
    di++
  }
  for (let i = 0; i < peg.white; i++) {
    const cls = animate ? 'game-peg game-peg-white game-peg-pop' : 'game-peg game-peg-white'
    const style = animate ? ` style="animation-delay:${di * 80}ms"` : ''
    pegs.push(`<span class="${cls}"${style} aria-label="Pion blanc"></span>`)
    di++
  }
  for (let i = 0; i < Math.max(0, codeLength - peg.black - peg.white); i++) {
    const cls = animate ? 'game-peg game-peg-empty game-peg-pop' : 'game-peg game-peg-empty'
    const style = animate ? ` style="animation-delay:${di * 80}ms"` : ''
    pegs.push(`<span class="${cls}"${style}></span>`)
    di++
  }
  return `
    <li class="game-row game-row-past" data-row="${rowIndex}">
      <span class="game-row-pegs">${pegs.join('')}</span>
      <span class="game-row-slots">${slots}</span>
    </li>
  `
}

function renderCurrentRow(currentRow, codeLength, rowIndex) {
  const slots = currentRow
    .map((id, i) =>
      id === null
        ? `<span class="game-slot game-slot-empty" data-slot="${i}" role="button" tabindex="0" aria-label="Emplacement vide"></span>`
        : `<span class="game-slot game-slot-filled bg-${id}" data-slot="${i}" role="button" tabindex="0" aria-label="Couleur placée"></span>`
    )
    .join('')
  return `
    <li class="game-row game-row-current" data-row="${rowIndex}">
      <span class="game-row-pegs">${'<span class="game-peg game-peg-empty"></span>'.repeat(codeLength)}</span>
      <span class="game-row-slots">${slots}</span>
    </li>
  `
}

function renderEmptyRow(codeLength) {
  const slots = '<span class="game-slot game-slot-empty game-slot-locked"></span>'.repeat(codeLength)
  return `
    <li class="game-row game-row-empty" aria-hidden="true">
      <span class="game-row-pegs">${'<span class="game-peg game-peg-empty"></span>'.repeat(codeLength)}</span>
      <span class="game-row-slots">${slots}</span>
    </li>
  `
}

function showEndScreen(boardState) {
  const overScreen = document.getElementById('gameOverScreen')
  if (!overScreen) return
  const titleEl = document.getElementById('gameOverTitle')
  if (titleEl) {
    titleEl.textContent = boardState.status === 'won' ? t.stagiaire.gameWon : t.stagiaire.gameLost
  }
  const multEl = document.getElementById('gameOverMultiplier')
  if (multEl) {
    if (boardState.status === 'won' && boardState.multiplier > 1 && boardState.baseScore != null) {
      multEl.hidden = false
      const multLabel = boardState.multiplier >= 3 ? '3' : boardState.multiplier.toString().replace(/\.?0+$/, '')
      multEl.innerHTML = `<span class="game-mult-base">${boardState.baseScore}</span> <span class="game-mult-x">×</span> <span class="game-mult-val">${multLabel}</span>`
    } else {
      multEl.hidden = true
    }
  }
  const scoreEl = document.getElementById('gameOverScore')
  if (scoreEl) {
    if (boardState.status === 'won' && boardState.score > 0) {
      animateScoreCountUp(scoreEl, boardState.score)
    } else {
      scoreEl.textContent = t.stagiaire.gameFinalScore(0)
    }
  }
  const levelUpEl = document.getElementById('gameLevelUp')
  if (levelUpEl) {
    if (boardState.leveledUp) {
      const newLevel = getDifficulty(boardState.best).level
      levelUpEl.textContent = `Niveau ${newLevel} débloqué !`
      levelUpEl.hidden = false
    } else {
      levelUpEl.hidden = true
    }
  }
  const bestEl = document.getElementById('gameOverBest')
  if (bestEl) {
    bestEl.hidden = !boardState.isRecord
    if (boardState.isRecord) bestEl.textContent = t.stagiaire.gameNewBest
  }
  const secretEl = document.getElementById('gameOverSecret')
  if (secretEl) {
    const chips = (boardState.secret || [])
      .map((id) => `<span class="game-slot game-slot-filled bg-${id}"></span>`)
      .join('')
    secretEl.innerHTML = `<span class="game-screen-secret-label">Le code était</span><span class="game-screen-secret-chips">${chips}</span>`
  }
  showOverlayScreen('gameOverScreen')
}

function animateScoreCountUp(el, target, duration = 800) {
  if (scoreAnimId) cancelAnimationFrame(scoreAnimId)
  const start = performance.now()
  function tick(now) {
    const elapsed = now - start
    const progress = Math.min(1, elapsed / duration)
    const eased = 1 - Math.pow(1 - progress, 3)
    el.textContent = t.stagiaire.gameFinalScore(Math.round(target * eased))
    if (progress < 1) {
      scoreAnimId = requestAnimationFrame(tick)
    } else {
      scoreAnimId = null
    }
  }
  scoreAnimId = requestAnimationFrame(tick)
}

function handleColorPlace(colorId) {
  if (!game || game.status !== 'playing') return
  game.place(colorId)
  renderBoard()
}

function handleSubmit() {
  if (!game) return
  const ok = game.submit()
  if (ok) {
    pendingPegAnimation = true
    renderBoard()
    if (game.status === 'won' || game.status === 'lost') {
      const client = getClient()
      if (client) {
        client.send({ type: 'report_game_score', gameScore: loadHighScore() })
      }
    }
  }
}

function handleClear() {
  if (!game) return
  game.clear()
  renderBoard()
}

function handleNewGame() {
  game = createGame()
  hideAllOverlayScreens()
  renderBoard()
}

/**
 * Open the game overlay. Creates a fresh Mastermind instance and renders
 * the board. Vote events from the websocket layer will pause the game
 * via pauseGameExternal().
 */
export function handlePlayGame() {
  const overlay = getOverlay()
  if (!overlay) return
  state.gamePlaying = true
  overlay.hidden = false

  if (game && game.status === 'playing') {
    hideAllOverlayScreens()
    renderBoard()
    return
  }

  game = createGame()
  hideAllOverlayScreens()
  renderBoard()
  bindOverlayButtons()

  if (!hasSeenRules()) {
    showOverlayScreen('gameRulesScreen')
    markRulesSeen()
  }
}

function bindOverlayButtons() {
  const submitBtn = document.getElementById('gameSubmitBtn')
  if (submitBtn) submitBtn.onclick = handleSubmit
  const clearBtn = document.getElementById('gameClearBtn')
  if (clearBtn) clearBtn.onclick = handleClear
  const restartBtn = document.getElementById('gameRestartBtn')
  if (restartBtn) restartBtn.onclick = handleNewGame
  const resumeBtn = document.getElementById('gameResumeBtn')
  if (resumeBtn)
    resumeBtn.onclick = () => {
      hideAllOverlayScreens()
    }
  const rulesBtn = document.getElementById('gameRulesBtn')
  if (rulesBtn)
    rulesBtn.onclick = () => {
      showOverlayScreen('gameRulesScreen')
    }
  const rulesCloseBtn = document.getElementById('gameRulesCloseBtn')
  if (rulesCloseBtn)
    rulesCloseBtn.onclick = () => {
      hideAllOverlayScreens()
    }
  for (const id of ['gameQuitBtn', 'gameQuitFromPauseBtn', 'gameQuitFromOverBtn']) {
    const el = document.getElementById(id)
    if (el) el.onclick = handleQuitGame
  }
}

export function handleQuitGame() {
  game = null
  const overlay = getOverlay()
  if (overlay) overlay.hidden = true
  state.gamePlaying = false
}

/**
 * Pause the game when the trainer starts/closes a vote or when the WS
 * drops. No-op if the round already ended — the end screen is more
 * useful than a pause banner in that case.
 */
export function pauseGameExternal() {
  if (!game || !state.gamePlaying) return
  if (game.status !== 'playing') return
  showOverlayScreen('gamePauseScreen')
}

/**
 * Soft teardown: hide the overlay but preserve the game instance so the
 * trainee can resume the same puzzle after voting or after the formateur
 * closes the vote.
 */
export function teardownGame() {
  const overlay = getOverlay()
  if (overlay) overlay.hidden = true
  state.gamePlaying = false
}

/**
 * Handle the join session form submission
 */
export function handleJoin(e) {
  e.preventDefault()

  const prenomInput = document.getElementById('prenom')
  const codeInput = document.getElementById('sessionCode')
  const prenom = prenomInput.value.trim()
  const code = CONSTANTS.SESSION_CODE_NORMALIZE(codeInput.value.trim())

  // Validation
  const nameError = validateName(prenom)
  if (nameError) {
    prenomInput.classList.add('error')
    showError(nameError)
    return
  }

  const codeError = validateSessionCode(code)
  if (codeError) {
    codeInput.classList.add('error')
    showError(codeError)
    return
  }

  prenomInput.classList.remove('error')
  codeInput.classList.remove('error')

  state.prenom = prenom
  state.sessionCode = code
  if (codeInput) codeInput.value = code // reflect normalized uppercase back to UI

  // Sauvegarder le prénom
  safeLocalSet('vote_stagiaire_prenom', prenom)

  connectToSessionFn(code)
}

/**
 * Handle the edit name form submission
 */
export function handleEditName(e) {
  e.preventDefault()

  const input = document.getElementById('editPrenom')
  const newPrenom = input.value.trim()

  // Validation du prénom
  const nameError = validateName(newPrenom)
  if (nameError) {
    input.classList.add('error')
    showError(nameError)
    return
  }

  input.classList.remove('error')

  state.prenom = newPrenom
  safeLocalSet('vote_stagiaire_prenom', newPrenom)

  const client = getClient()
  if (client) {
    client.send({
      type: 'update_name',
      name: newPrenom
    })
  }

  state.prenomEdit = false
  render()
}

/**
 * Handle single choice vote button click
 */
export function handleSingleChoiceVote(e) {
  const colorId = e.target.dataset.color

  state.selectedColors.clear()
  state.selectedColors.add(colorId)

  submitVote(e.target)
}

export function handleBlankVote() {
  state.selectedColors.clear()
  state.selectedColors.add('blank')
  submitVote()
}

/**
 * Handle checkbox change for multiple choice voting
 */
export function handleCheckboxChange(e) {
  const colorId = e.target.value
  const label = document.querySelector(`label[for="color-${colorId}"]`)

  if (e.target.checked) {
    state.selectedColors.add(colorId)
    label?.classList.add('selected')
  } else {
    state.selectedColors.delete(colorId)
    label?.classList.remove('selected')
  }

  // Mettre à jour le bouton
  const submitBtn = document.getElementById('submitVote')
  if (submitBtn) {
    submitBtn.disabled = state.selectedColors.size === 0
  }
}

/**
 * Handle the submit vote button click
 */
export function handleSubmitVote() {
  if (state.selectedColors.size > 0) {
    submitVote()
  }
}

/**
 * Submit the vote to the server
 */
export function submitVote(triggerButton = null) {
  const client = getClient()
  if (!client) {
    showError('Erreur de connexion')
    return
  }

  const btn = triggerButton || document.getElementById('submitVote')
  const originalContent = btn ? btn.innerHTML : ''

  if (btn) {
    btn.disabled = true
    if (btn.id === 'submitVote') {
      btn.innerHTML = `${loader(' class="icon icon-md spin"')} Envoi...`
    } else {
      btn.style.opacity = '0.7'
      btn.style.cursor = 'wait'
    }
  }

  const success = client.send({
    type: 'vote',
    colors: Array.from(state.selectedColors),
    stagiaireId: state.stagiaireId || undefined
  })

  if (!success) {
    if (btn) {
      btn.disabled = false
      btn.innerHTML = originalContent
      btn.style.opacity = ''
      btn.style.cursor = ''
    }
    showError('Erreur de connexion')
  }
}

/**
 * Leave the current session
 */
export async function leaveSession() {
  const ok = await showConfirmDialog({
    title: t.stagiaire.leaveSessionTitle,
    message: t.stagiaire.leaveSession,
    confirmLabel: t.stagiaire.leave
  })
  if (!ok) return

  teardownGame()
  game = null
  safeSessionRemove('vote_session_code')
  safeSessionRemove('vote_stagiaire_id')
  state.sessionCode = ''
  state.appState = AppState.JOINING
  state.connected = false
  state.hasVoted = false
  state.selectedColors.clear()
  state.availableColors = []
  state.colorLabels = {}
  state.multipleChoice = false
  state.gameEnabled = false
  state.gamePlaying = false
  state.stagiaireId = null
  const client = getClient()
  if (client) {
    client.close()
  }
  render()
}

/**
 * Handle keyboard shortcuts
 * - Escape: Cancel current action (e.g., exit edit mode)
 * - Enter: Submit forms (works natively for forms)
 */
export function handleKeyPress(event) {
  // Escape key - cancel edit mode
  if (event.key === 'Escape') {
    if (state.prenomEdit) {
      state.prenomEdit = false
      render()
      event.preventDefault()
    }
  }
}
