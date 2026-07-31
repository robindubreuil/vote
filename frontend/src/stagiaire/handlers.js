import { validateName, validateSessionCode } from '@shared/validation.js'
import { showError, showConfirmDialog } from '@shared/ui.js'
import { loader } from '@shared/icons.js'
import { CONSTANTS } from '@shared/config.js'
import { t } from '@shared/strings.js'
import { state, resetStagiaireState } from './state.js'
import { render } from './renderers.js'
import { getClient } from './websocket.js'
import { Mastermind, getDifficulty, getLevelProgress, streakMultiplier } from './game.js'
import { loadHighScore, hasSeenRules, markRulesSeen } from '@shared/game-storage.js'
import { COLORS, escapeHtml } from '@shared/colors.js'
import { safeSessionSet, safeSessionRemove } from '@shared/utils/safe-storage.js'

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

/**
 * Toggle field-level accessibility state on a form input.
 *
 * Sets `aria-invalid` and the legacy `.error` class together so screen
 * readers (aria-invalid) and sighted users (CSS-styled .error) both get
 * the validation signal. `aria-invalid="false"` (rather than removing
 * the attribute) is deliberate: a freshly rendered input defaults to
 * `aria-invalid="false"` so this keeps the DOM state symmetric and
 * avoids ever surfacing the implicit "unset" state to AT.
 *
 * R8: exported so the websocket error handler can mark the edit-name
 * input invalid when a server-side rename rejection arrives (name
 * collision under the session lock). The function is pure DOM, so it
 * has no dependency on handlers.js state — safe to call from either
 * module without ordering concerns.
 *
 * @param {HTMLInputElement} input
 * @param {boolean} invalid
 */
export function setFieldInvalid(input, invalid) {
  if (!input) return
  if (invalid) {
    input.classList.add('error')
    input.setAttribute('aria-invalid', 'true')
  } else {
    input.classList.remove('error')
    input.setAttribute('aria-invalid', 'false')
  }
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
    progEl.title =
      prog.toNext > 0 ? t.stagiaire.gameLevelProgressTitle(prog.toNext, boardState.level + 1) : t.stagiaire.gameLevelMax
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
      <span class="game-slot game-slot-filled bg-${id}" aria-label="${t.stagiaire.gameSlotFilled}"></span>`
    )
    .join('')
  const pegs = []
  let di = 0
  for (let i = 0; i < peg.black; i++) {
    const cls = animate ? 'game-peg game-peg-black game-peg-pop' : 'game-peg game-peg-black'
    const style = animate ? ` style="animation-delay:${di * 80}ms"` : ''
    pegs.push(`<span class="${cls}"${style} aria-label="${t.stagiaire.gamePegBlack}"></span>`)
    di++
  }
  for (let i = 0; i < peg.white; i++) {
    const cls = animate ? 'game-peg game-peg-white game-peg-pop' : 'game-peg game-peg-white'
    const style = animate ? ` style="animation-delay:${di * 80}ms"` : ''
    pegs.push(`<span class="${cls}"${style} aria-label="${t.stagiaire.gamePegWhite}"></span>`)
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
        ? `<span class="game-slot game-slot-empty" data-slot="${i}" role="button" tabindex="0" aria-label="${t.stagiaire.gameSlotEmpty}"></span>`
        : `<span class="game-slot game-slot-filled bg-${id}" data-slot="${i}" role="button" tabindex="0" aria-label="${t.stagiaire.gameSlotFilled}"></span>`
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
        client.send({ type: 'report_game_score', gameScore: game.score })
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
  cancelScoreAnimation()
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
  cancelScoreAnimation()
  const overlay = getOverlay()
  if (overlay) overlay.hidden = true
  state.gamePlaying = false
}

function cancelScoreAnimation() {
  if (scoreAnimId) {
    cancelAnimationFrame(scoreAnimId)
    scoreAnimId = null
  }
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
    setFieldInvalid(prenomInput, true)
    setFieldInvalid(codeInput, false)
    showError(nameError)
    return
  }

  const codeError = validateSessionCode(code)
  if (codeError) {
    setFieldInvalid(codeInput, true)
    setFieldInvalid(prenomInput, false)
    showError(codeError)
    return
  }

  setFieldInvalid(prenomInput, false)
  setFieldInvalid(codeInput, false)

  state.prenom = prenom
  state.sessionCode = code
  if (codeInput) codeInput.value = code // reflect normalized uppercase back to UI

  // F15: prenom is persisted in sessionStorage (not localStorage) so it
  // is scoped to the tab — same boundary as the reclaim token and
  // stagiaire ID. Closing the tab clears it; a different student on a
  // shared tablet starts fresh instead of auto-joining under the
  // previous user's name.
  safeSessionSet('vote_stagiaire_prenom', prenom)

  connectToSessionFn(code)
}

/**
 * Handle the edit name form submission.
 *
 * R8: don't commit `state.prenom` optimistically. The server performs an
 * authoritative normalised-name collision check under the session lock
 * (`vote/manager.go:UpdateStagiaireName`) and can reject with
 * `ErrNameInUse` in the same TOCTOU window CC2 identified for joins. If
 * we commit + close the modal before the response arrives, a rejection
 * leaves the trainee's UI showing the rejected name while the trainer
 * still sees the old one, with the modal already closed and only a 5s
 * toast as a signal.
 *
 * Instead: send the request and keep the modal open. The `name_updated`
 * handler in websocket.js commits `msg.name` (the canonical server-
 * normalised name) and closes the modal on success. On `error`, the
 * modal stays open with the user's input preserved for correction. We
 * fall back to the optimistic path only when there is no client (fully
 * offline) so the local UI doesn't feel stuck — the next reconnect
 * re-syncs via `stagiaire_join`.
 */
export function handleEditName(e) {
  e.preventDefault()

  // R8: clear any stale pendingRename from a prior attempt so the
  // websocket error handler doesn't route a rejection into a modal
  // that's opening fresh.
  state.pendingRename = null

  const input = document.getElementById('editPrenom')
  const newPrenom = input.value.trim()

  // Validation du prénom
  const nameError = validateName(newPrenom)
  if (nameError) {
    setFieldInvalid(input, true)
    // The edit-name modal has its own inline error element; surface the
    // message there so AT users don't have to navigate back to the join
    // form's #join-error slot (which doesn't exist in this state).
    const inlineError = document.getElementById('edit-name-error')
    if (inlineError) {
      inlineError.textContent = nameError
      inlineError.style.display = 'block'
    } else {
      showError(nameError)
    }
    return
  }

  setFieldInvalid(input, false)
  const inlineError = document.getElementById('edit-name-error')
  if (inlineError) {
    inlineError.textContent = ''
    inlineError.style.display = 'none'
  }

  const client = getClient()
  if (!client) {
    // Offline fallback: commit locally so the UI isn't stuck. The next
    // reconnect re-syncs via stagiaire_join.
    state.prenom = newPrenom
    safeSessionSet('vote_stagiaire_prenom', newPrenom)
    state.prenomEdit = false
    render()
    return
  }

  // R8: mark a rename as in-flight so the websocket error handler can
  // route a rejection into the modal's inline error slot (instead of a
  // 5s toast that disappears before the user can correct the input).
  // Cleared by name_updated on success.
  state.pendingRename = newPrenom
  // F28: client.send returns false when the socket dropped between the
  // click and the send (mirrors F24's formateur guard). Without this
  // pendingRename would stay set forever — no name_updated ever arrives
  // to clear it, the inline error slot stays empty, and the modal gives
  // no signal that the rename failed. On false: clear the in-flight
  // flag, surface a connection message in the inline slot, mark the
  // field invalid, and keep the modal open with the user's input
  // preserved (same shape as the R8 server-rejection path).
  const ok = client.send({
    type: 'update_name',
    name: newPrenom
  })
  if (!ok) {
    state.pendingRename = null
    const sendError = document.getElementById('edit-name-error')
    if (sendError) {
      sendError.textContent = t.stagiaire.connectionError
      sendError.style.display = 'block'
    }
    setFieldInvalid(input, true)
  }
  // Intentionally do NOT set state.prenom / close the modal here — wait
  // for the server's authoritative response.
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

/**
 * A3: arrow-key navigation for the single-choice radiogroup.
 *
 * `role="radiogroup"` carries an implicit WAI-ARIA Authoring Practices
 * contract that Arrow keys move between radios (Tab leaves the group).
 * Tab-only navigation left the keyboard interaction at odds with the
 * declared role.
 *
 * Implements the APG roving-tabindex pattern: exactly one radio holds
 * `tabindex="0"` (the checked one, else the first), the rest are `-1`.
 * On each arrow / Home / End move we update selection, ARIA state, and
 * tabindex in place — no re-render, since focus must land on the
 * newly-active radio — then commit + submit exactly like a click so the
 * vote flow is identical whether driven by mouse, Tab+Space, or arrows.
 * @param {KeyboardEvent} e
 */
export function handleSingleChoiceKeydown(e) {
  const radiogroup = e.currentTarget
  if (!radiogroup) return
  const buttons = Array.from(radiogroup.querySelectorAll('.vote-button'))
  if (buttons.length === 0) return

  const key = e.key
  let forward = null
  let jumpTo = null
  if (key === 'ArrowRight' || key === 'ArrowDown') {
    forward = true
  } else if (key === 'ArrowLeft' || key === 'ArrowUp') {
    forward = false
  } else if (key === 'Home') {
    jumpTo = 'home'
  } else if (key === 'End') {
    jumpTo = 'end'
  } else {
    return
  }

  e.preventDefault()

  const currentIndex = Math.max(
    0,
    buttons.findIndex((b) => b === document.activeElement)
  )
  let nextIndex
  if (jumpTo === 'home') nextIndex = 0
  else if (jumpTo === 'end') nextIndex = buttons.length - 1
  else if (forward) nextIndex = (currentIndex + 1) % buttons.length
  else nextIndex = (currentIndex - 1 + buttons.length) % buttons.length

  const nextBtn = buttons[nextIndex]
  if (!nextBtn) return

  // Roving tabindex + ARIA + visual state, updated in place so focus
  // stays on the active radio without a re-render.
  buttons.forEach((b) => {
    const selected = b === nextBtn
    b.classList.toggle('selected', selected)
    b.setAttribute('aria-checked', String(selected))
    b.tabIndex = selected ? 0 : -1
  })
  nextBtn.focus()

  state.selectedColors.clear()
  state.selectedColors.add(nextBtn.dataset.color)
  submitVote(nextBtn)
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
  safeSessionRemove('vote_stagiaire_reclaim_token')
  resetStagiaireState()
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
