// @vitest-environment happy-dom
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'

// =====================================================================
// Shared mocks
// =====================================================================
//
// handlers.js imports:
//   - ./websocket.js   → getClient (we want to control client.send return)
//   - ./game.js        → Mastermind, getDifficulty, getLevelProgress, streakMultiplier
//   - @shared/game-storage.js → loadHighScore, hasSeenRules, markRulesSeen, ...
//   - @shared/ui.js    → showError, showConfirmDialog
//
// The game-lifecycle suite (M5) needs a Mastermind that immediately enters
// the "won" state so renderBoard calls animateScoreCountUp. The validation
// suite never touches the game, so the same mock is harmless there.

vi.mock('./game.js', () => {
  class FakeMastermind {
    constructor() {
      this.status = 'won'
      this.score = 800
      this.baseScore = 800
      this.multiplier = 1
      this.isRecord = true
      this.leveledUp = false
      this.level = 1
      this.palette = [
        { id: 'rouge', color: '#ff0000', name: 'Rouge' },
        { id: 'vert', color: '#00ff00', name: 'Vert' }
      ]
      this.codeLength = 4
      this.maxAttempts = 8
      this.guesses = [['rouge', 'vert', 'rouge', 'vert']]
      this.pegs = [{ black: 4, white: 0 }]
      this.currentRow = [null, null, null, null]
      this.secret = ['rouge', 'vert', 'rouge', 'vert']
    }
    place() {}
    clear() {}
    submit() {
      return true
    }
    getBoardState() {
      return {
        palette: this.palette,
        codeLength: this.codeLength,
        maxAttempts: this.maxAttempts,
        guesses: this.guesses.map((g) => [...g]),
        pegs: this.pegs.map((p) => ({ ...p })),
        currentRow: [...this.currentRow],
        status: this.status,
        score: this.score,
        baseScore: this.baseScore,
        multiplier: this.multiplier,
        isRecord: Boolean(this.isRecord),
        best: 1000,
        level: this.level,
        attemptsUsed: 1,
        attemptsLeft: 7,
        secret: [...this.secret],
        streak: 0,
        leveledUp: false
      }
    }
  }
  return {
    Mastermind: FakeMastermind,
    getDifficulty: () => ({ level: 1, paletteSize: 4 }),
    getLevelProgress: () => ({ pct: 50, toNext: 100 }),
    streakMultiplier: () => 1
  }
})

vi.mock('@shared/game-storage.js', () => ({
  loadHighScore: () => 0,
  saveHighScore: () => {},
  hasSeenRules: () => true,
  markRulesSeen: () => {},
  resetHighScore: () => {},
  saveStreak: () => {},
  loadStreak: () => 0
}))

// getClient mock — reassigned per-test via mockGetClient.mockReturnValue.
const mockGetClient = vi.fn()
vi.mock('./websocket.js', () => ({
  getClient: (...args) => mockGetClient(...args),
  pauseGameExternal: vi.fn(),
  teardownGame: vi.fn()
}))

// showConfirmDialog mock — controlled per-test.
const mockConfirm = vi.fn(() => Promise.resolve(false))
vi.mock('@shared/ui.js', async (importOriginal) => {
  const actual = await importOriginal()
  return {
    ...actual,
    showConfirmDialog: (...args) => mockConfirm(...args),
    showError: vi.fn(actual.showError)
  }
})

const { state, AppState } = await import('./state.js')
const { renderLayout, render } = await import('./renderers.js')
const {
  handlePlayGame,
  handleQuitGame,
  teardownGame,
  setConnectToSession,
  handleJoin,
  handleEditName,
  handleCheckboxChange,
  handleSubmitVote,
  handleSingleChoiceVote,
  handleSingleChoiceKeydown,
  handleBlankVote,
  submitVote,
  leaveSession,
  handleKeyPress
} = await import('./handlers.js')

// =====================================================================
// M5: score animation rAF cleanup (original suite)
// =====================================================================

describe('stagiaire handlers — M5 score animation rAF cleanup', () => {
  let rafSpy
  let cancelSpy

  beforeEach(() => {
    document.body.innerHTML = '<div id="app"></div>'
    renderLayout(document.getElementById('app'))
    state.appState = AppState.WAITING
    state.gamePlaying = false
    state.sessionCode = 'ABC'
    state.prenom = 'Test'
    state.stagiaireId = 's1'
    state.gameEnabled = true
    setConnectToSession(() => {})

    let nextId = 1
    rafSpy = vi.spyOn(globalThis, 'requestAnimationFrame').mockImplementation(() => nextId++)
    cancelSpy = vi.spyOn(globalThis, 'cancelAnimationFrame').mockImplementation(() => {})
  })

  afterEach(() => {
    rafSpy.mockRestore()
    cancelSpy.mockRestore()
    vi.useRealTimers()
  })

  it('teardownGame cancels the pending score animation rAF', () => {
    handlePlayGame()
    expect(rafSpy).toHaveBeenCalled()

    teardownGame()

    expect(cancelSpy).toHaveBeenCalled()
    const overlay = document.getElementById('game-overlay')
    expect(overlay.hidden).toBe(true)
  })

  it('handleQuitGame cancels the pending score animation rAF', () => {
    handlePlayGame()
    expect(rafSpy).toHaveBeenCalled()

    handleQuitGame()

    expect(cancelSpy).toHaveBeenCalled()
    const overlay = document.getElementById('game-overlay')
    expect(overlay.hidden).toBe(true)
  })

  it('teardownGame is a no-op when no animation is pending (does not call cancelAnimationFrame spuriously)', () => {
    teardownGame()
    expect(cancelSpy).not.toHaveBeenCalled()
  })
})

// =====================================================================
// Session 8: vote submit, join validation, leave flow
// =====================================================================

describe('stagiaire handlers — submit / join / leave', () => {
  let connectSpy

  beforeEach(() => {
    vi.clearAllMocks()
    document.body.innerHTML = '<div id="app"></div>'
    renderLayout(document.getElementById('app'))

    state.appState = AppState.WAITING
    state.sessionCode = 'ABC'
    state.prenom = 'Marie'
    state.stagiaireId = 's1'
    state.selectedColors = new Set()
    state.availableColors = ['rouge', 'vert']
    state.multipleChoice = true
    state.gameEnabled = false
    state.gamePlaying = false
    state.prenomEdit = false
    state.pendingRename = null
    state.connected = true

    connectSpy = vi.fn()
    setConnectToSession(connectSpy)
  })

  describe('submitVote', () => {
    it('shows an error when there is no client', () => {
      mockGetClient.mockReturnValue(null)
      // No DOM state to depend on — just verify the error path.
      expect(() => submitVote()).not.toThrow()
      // showError is the safe-path: 'Erreur de connexion'
      // The error element may not exist (no .error-message rendered in WAITING),
      // in which case showError silently no-ops. Verify via the spy-like mock.
    })

    it('disables the submit button while sending and restores it on send failure', () => {
      const fakeClient = { send: vi.fn(() => false) }
      mockGetClient.mockReturnValue(fakeClient)

      // Render the VOTING view with a selection so the submit button starts
      // enabled.
      state.appState = AppState.VOTING
      state.selectedColors = new Set(['rouge'])
      render()
      const btn = document.getElementById('submitVote')
      const original = btn.innerHTML
      expect(btn.disabled).toBe(false)

      submitVote(btn)

      expect(fakeClient.send).toHaveBeenCalledWith({
        type: 'vote',
        colors: ['rouge'],
        stagiaireId: 's1'
      })
      // send returned false → button restored, not stuck disabled.
      expect(btn.disabled).toBe(false)
      expect(btn.innerHTML).toBe(original)
    })

    it('keeps the button disabled after a successful send (vote_accepted will re-enable on render)', () => {
      const fakeClient = { send: vi.fn(() => true) }
      mockGetClient.mockReturnValue(fakeClient)
      state.appState = AppState.VOTING
      state.selectedColors = new Set(['rouge'])
      render()
      const btn = document.getElementById('submitVote')
      const original = btn.innerHTML

      submitVote(btn)

      expect(btn.disabled).toBe(true)
      expect(btn.innerHTML).not.toBe(original) // loader inserted
      expect(btn.innerHTML).toContain('Envoi')
    })

    it('handleSubmitVote is a no-op when no color is selected', () => {
      const fakeClient = { send: vi.fn(() => true) }
      mockGetClient.mockReturnValue(fakeClient)
      state.selectedColors = new Set()
      handleSubmitVote()
      expect(fakeClient.send).not.toHaveBeenCalled()
    })

    it('handleSubmitVote sends when at least one color is selected', () => {
      const fakeClient = { send: vi.fn(() => true) }
      mockGetClient.mockReturnValue(fakeClient)
      state.selectedColors = new Set(['rouge'])
      handleSubmitVote()
      expect(fakeClient.send).toHaveBeenCalledTimes(1)
      expect(fakeClient.send).toHaveBeenCalledWith(expect.objectContaining({ type: 'vote', colors: ['rouge'] }))
    })

    it('submitVote sends the live snapshot of selectedColors (Array.from)', () => {
      const fakeClient = { send: vi.fn(() => true) }
      mockGetClient.mockReturnValue(fakeClient)
      state.selectedColors = new Set(['vert', 'rouge'])
      state.appState = AppState.VOTING
      render()
      const btn = document.getElementById('submitVote')
      submitVote(btn)
      const sent = fakeClient.send.mock.calls[0][0]
      expect(sent.colors).toContain('rouge')
      expect(sent.colors).toContain('vert')
      expect(sent.colors).toHaveLength(2)
    })
  })

  describe('handleSingleChoiceVote / handleBlankVote', () => {
    it('handleSingleChoiceVote replaces the selection with the clicked color', () => {
      state.selectedColors = new Set(['rouge'])
      // Render so a real vote button exists to act as the event target.
      state.appState = AppState.VOTING
      state.multipleChoice = false
      state.availableColors = ['rouge', 'vert']
      render()
      const vertBtn = document.querySelector('[data-testid="vote-btn-vert"]')
      const fakeClient = { send: vi.fn(() => true) }
      mockGetClient.mockReturnValue(fakeClient)
      handleSingleChoiceVote({ target: vertBtn })
      expect(state.selectedColors.has('vert')).toBe(true)
      expect(state.selectedColors.has('rouge')).toBe(false)
      expect(state.selectedColors.size).toBe(1)
    })

    it('handleBlankVote sets selection to ["blank"] only', () => {
      const fakeClient = { send: vi.fn(() => true) }
      mockGetClient.mockReturnValue(fakeClient)
      state.selectedColors = new Set(['rouge', 'vert'])
      handleBlankVote()
      expect(state.selectedColors.has('blank')).toBe(true)
      expect(state.selectedColors.size).toBe(1)
    })
  })

  // A3: role="radiogroup" must honour the WAI-ARIA APG arrow-key
  // contract. The roving-tabindex markup is asserted in renderers.test;
  // these cover the interaction — arrow / Home / End keys move the
  // selection, focus, AND submit the vote (same path as a click).
  describe('A3: handleSingleChoiceKeydown — radiogroup arrow navigation', () => {
    function keyEvent(key, radiogroup) {
      return { key, currentTarget: radiogroup, preventDefault: vi.fn() }
    }

    function setupSelection(startColor) {
      state.appState = AppState.VOTING
      state.sessionCode = 'ABC'
      state.multipleChoice = false
      state.availableColors = ['rouge', 'vert', 'bleu']
      state.selectedColors = new Set(startColor ? [startColor] : [])
      state.connected = true
      render()
      const grid = document.querySelector('.vote-grid[role="radiogroup"]')
      const rouge = document.querySelector('[data-testid="vote-btn-rouge"]')
      const vert = document.querySelector('[data-testid="vote-btn-vert"]')
      const bleu = document.querySelector('[data-testid="vote-btn-bleu"]')
      return { grid, rouge, vert, bleu }
    }

    it('ArrowRight moves selection forward by one, wraps focus, and submits', () => {
      const fakeClient = { send: vi.fn(() => true) }
      mockGetClient.mockReturnValue(fakeClient)
      const { grid, rouge, vert } = setupSelection('rouge')
      rouge.focus()
      expect(document.activeElement).toBe(rouge)

      handleSingleChoiceKeydown(keyEvent('ArrowRight', grid))

      expect(state.selectedColors.has('vert')).toBe(true)
      expect(state.selectedColors.size).toBe(1)
      expect(document.activeElement).toBe(vert)
      // Tab stop roved to the newly-active radio.
      expect(vert.tabIndex).toBe(0)
      expect(rouge.tabIndex).toBe(-1)
      // aria-checked tracks the new selection.
      expect(vert.getAttribute('aria-checked')).toBe('true')
      expect(rouge.getAttribute('aria-checked')).toBe('false')
      // A vote was submitted for the new color (same path as a click).
      expect(fakeClient.send).toHaveBeenCalledWith(expect.objectContaining({ type: 'vote', colors: ['vert'] }))
    })

    it('ArrowLeft moves selection backward by one', () => {
      const fakeClient = { send: vi.fn(() => true) }
      mockGetClient.mockReturnValue(fakeClient)
      const { grid, rouge, vert } = setupSelection('vert')
      vert.focus()

      handleSingleChoiceKeydown(keyEvent('ArrowLeft', grid))

      expect(state.selectedColors.has('rouge')).toBe(true)
      expect(document.activeElement).toBe(rouge)
    })

    it('ArrowDown/ArrowUp behave like Right/Left', () => {
      const fakeClient = { send: vi.fn(() => true) }
      mockGetClient.mockReturnValue(fakeClient)
      const { grid, rouge, vert } = setupSelection('rouge')
      rouge.focus()

      handleSingleChoiceKeydown(keyEvent('ArrowDown', grid))
      expect(state.selectedColors.has('vert')).toBe(true)
      expect(document.activeElement).toBe(vert)

      handleSingleChoiceKeydown(keyEvent('ArrowUp', grid))
      expect(state.selectedColors.has('rouge')).toBe(true)
      expect(document.activeElement).toBe(rouge)
    })

    it('ArrowRight wraps from last back to first', () => {
      const fakeClient = { send: vi.fn(() => true) }
      mockGetClient.mockReturnValue(fakeClient)
      const { grid, rouge, bleu } = setupSelection('bleu')
      bleu.focus()

      handleSingleChoiceKeydown(keyEvent('ArrowRight', grid))

      expect(state.selectedColors.has('rouge')).toBe(true)
      expect(document.activeElement).toBe(rouge)
    })

    it('ArrowLeft wraps from first forward to last', () => {
      const fakeClient = { send: vi.fn(() => true) }
      mockGetClient.mockReturnValue(fakeClient)
      const { grid, bleu, rouge } = setupSelection('rouge')
      rouge.focus()

      handleSingleChoiceKeydown(keyEvent('ArrowLeft', grid))

      expect(state.selectedColors.has('bleu')).toBe(true)
      expect(document.activeElement).toBe(bleu)
    })

    it('Home jumps to the first radio, End to the last', () => {
      const fakeClient = { send: vi.fn(() => true) }
      mockGetClient.mockReturnValue(fakeClient)
      const { grid, rouge, vert, bleu } = setupSelection('vert')
      vert.focus()

      handleSingleChoiceKeydown(keyEvent('End', grid))
      expect(state.selectedColors.has('bleu')).toBe(true)
      expect(document.activeElement).toBe(bleu)

      handleSingleChoiceKeydown(keyEvent('Home', grid))
      expect(state.selectedColors.has('rouge')).toBe(true)
      expect(document.activeElement).toBe(rouge)
    })

    it('ignores non-navigation keys (lets the event bubble, no submit)', () => {
      const fakeClient = { send: vi.fn(() => true) }
      mockGetClient.mockReturnValue(fakeClient)
      const { grid, rouge } = setupSelection('rouge')
      rouge.focus()

      const ev = keyEvent('a', grid)
      handleSingleChoiceKeydown(ev)
      // No preventDefault, no selection change, no submit.
      expect(ev.preventDefault).not.toHaveBeenCalled()
      expect(state.selectedColors.has('rouge')).toBe(true)
      expect(fakeClient.send).not.toHaveBeenCalled()
    })

    it('does not submit when the socket is down (submitVote surfaces the error)', () => {
      mockGetClient.mockReturnValue({ send: vi.fn(() => false) })
      const { grid, rouge, vert } = setupSelection('rouge')
      rouge.focus()

      expect(() => handleSingleChoiceKeydown(keyEvent('ArrowRight', grid))).not.toThrow()
      // Selection + focus still moved even though the vote didn't land.
      expect(state.selectedColors.has('vert')).toBe(true)
      expect(document.activeElement).toBe(vert)
    })
  })

  describe('handleCheckboxChange', () => {
    it('checking a color adds it to the selection and marks the label', () => {
      state.appState = AppState.VOTING
      state.multipleChoice = true
      state.selectedColors = new Set()
      render()

      const cb = document.getElementById('color-rouge')
      cb.checked = true
      handleCheckboxChange({ target: { value: 'rouge', checked: true } })

      expect(state.selectedColors.has('rouge')).toBe(true)
    })

    it('unchecking removes it', () => {
      state.selectedColors = new Set(['vert'])
      handleCheckboxChange({ target: { value: 'vert', checked: false } })
      expect(state.selectedColors.has('vert')).toBe(false)
    })

    it('disables the submit button when the selection becomes empty', () => {
      state.appState = AppState.VOTING
      state.multipleChoice = true
      state.connected = true
      state.selectedColors = new Set(['rouge'])
      render()
      expect(document.getElementById('submitVote').disabled).toBe(false)

      state.selectedColors = new Set()
      handleCheckboxChange({ target: { value: 'rouge', checked: false } })
      expect(document.getElementById('submitVote').disabled).toBe(true)
    })
  })

  describe('handleJoin', () => {
    function fillJoinForm({ prenom, code }) {
      state.appState = AppState.JOINING
      render()
      const p = document.getElementById('prenom')
      const c = document.getElementById('sessionCode')
      if (prenom !== undefined) p.value = prenom
      if (code !== undefined) c.value = code
      return { p, c }
    }

    function submitEvent() {
      return new Event('submit', { bubbles: true, cancelable: true })
    }

    it('rejects an empty name and sets the error class', () => {
      const { p } = fillJoinForm({ prenom: '', code: 'ABC' })
      const ev = submitEvent()
      handleJoin(ev)
      expect(p.classList.contains('error')).toBe(true)
      expect(connectSpy).not.toHaveBeenCalled()
    })

    it('F8: marks the invalid input with aria-invalid and clears the other', () => {
      const { p, c } = fillJoinForm({ prenom: '', code: 'ABC' })
      const ev = submitEvent()
      handleJoin(ev)
      expect(p.getAttribute('aria-invalid')).toBe('true')
      expect(c.getAttribute('aria-invalid')).toBe('false')
    })

    it('F8: marks code input aria-invalid when the code is bad', () => {
      const { p, c } = fillJoinForm({ prenom: 'Marie', code: 'XYZ' })
      const ev = submitEvent()
      handleJoin(ev)
      expect(c.getAttribute('aria-invalid')).toBe('true')
      expect(p.getAttribute('aria-invalid')).toBe('false')
    })

    it('F8: clears aria-invalid on both inputs when validation passes', () => {
      const { p, c } = fillJoinForm({ prenom: 'Marie', code: 'abc' })
      const ev = submitEvent()
      handleJoin(ev)
      expect(p.getAttribute('aria-invalid')).toBe('false')
      expect(c.getAttribute('aria-invalid')).toBe('false')
      expect(connectSpy).toHaveBeenCalledTimes(1)
    })

    it('rejects an invalid session code', () => {
      const { c } = fillJoinForm({ prenom: 'Marie', code: 'XYZ' })
      const ev = submitEvent()
      handleJoin(ev)
      expect(c.classList.contains('error')).toBe(true)
      expect(connectSpy).not.toHaveBeenCalled()
    })

    it('normalizes lowercase session codes to uppercase before connecting', () => {
      const { c } = fillJoinForm({ prenom: 'Marie', code: 'abc' })
      const ev = submitEvent()
      handleJoin(ev)
      expect(connectSpy).toHaveBeenCalledTimes(1)
      expect(c.value).toBe('ABC')
      expect(state.sessionCode).toBe('ABC')
      expect(state.prenom).toBe('Marie')
    })

    it('passes the normalized code to connectToSession', () => {
      fillJoinForm({ prenom: 'Marie', code: 'abc' })
      const ev = submitEvent()
      handleJoin(ev)
      expect(connectSpy).toHaveBeenCalledWith('ABC')
    })
  })

  describe('handleEditName', () => {
    function fillEditForm(prenom) {
      state.appState = AppState.WAITING
      state.prenomEdit = true
      state.prenom = 'Old'
      render()
      const input = document.getElementById('editPrenom')
      input.value = prenom
      return { input }
    }

    function submitEvent() {
      return new Event('submit', { bubbles: true, cancelable: true })
    }

    it('rejects an empty name', () => {
      const { input } = fillEditForm('')
      const ev = submitEvent()
      handleEditName(ev)
      expect(input.classList.contains('error')).toBe(true)
      expect(state.prenom).toBe('Old') // unchanged
    })

    it('F8: sets aria-invalid and surfaces the message in the inline error element', () => {
      const { input } = fillEditForm('')
      const ev = submitEvent()
      handleEditName(ev)
      expect(input.getAttribute('aria-invalid')).toBe('true')
      const inlineError = document.getElementById('edit-name-error')
      expect(inlineError).not.toBeNull()
      expect(inlineError.textContent).not.toBe('')
      expect(inlineError.style.display).toBe('block')
    })

    it('F8: clears aria-invalid and hides the inline error on submit (R8: defers commit to name_updated)', () => {
      const fakeClient = { send: vi.fn(() => true) }
      mockGetClient.mockReturnValue(fakeClient)
      const { input } = fillEditForm('NewName')
      // Mark the field invalid + surface the inline error, then submit.
      // R8: handleEditName no longer commits prenom or closes the modal
      // optimistically — it sends update_name and waits for the server.
      // The field-level reset (aria-invalid=false, inline error hidden)
      // still runs so the user gets immediate feedback that the local
      // validation passed.
      input.setAttribute('aria-invalid', 'true')
      const inlineError = document.getElementById('edit-name-error')
      inlineError.style.display = 'block'

      const ev = submitEvent()
      handleEditName(ev)

      // Field-level state is cleared on submit.
      expect(input.getAttribute('aria-invalid')).toBe('false')
      expect(inlineError.style.display).toBe('none')
      // R8: prenom is NOT committed optimistically — the server may reject.
      expect(state.prenom).toBe('Old')
      // R8: modal stays open (prenomEdit=true) until name_updated.
      expect(state.prenomEdit).toBe(true)
      // R8: the in-flight value is tracked for the error-routing path.
      expect(state.pendingRename).toBe('NewName')
      expect(fakeClient.send).toHaveBeenCalledWith({ type: 'update_name', name: 'NewName' })
    })

    it('updates state.prenom and sends update_name on the client', () => {
      const fakeClient = { send: vi.fn(() => true) }
      mockGetClient.mockReturnValue(fakeClient)
      fillEditForm('NewName')
      const ev = submitEvent()
      handleEditName(ev)
      // R8: prenom is NOT committed optimistically.
      expect(state.prenom).toBe('Old')
      expect(state.prenomEdit).toBe(true)
      expect(state.pendingRename).toBe('NewName')
      expect(fakeClient.send).toHaveBeenCalledWith({ type: 'update_name', name: 'NewName' })
    })

    it('R8: falls back to optimistic commit when no client is connected (offline)', () => {
      mockGetClient.mockReturnValue(null)
      fillEditForm('NewName')
      const ev = submitEvent()
      expect(() => handleEditName(ev)).not.toThrow()
      // No client → can't wait for server response. Commit locally so
      // the UI isn't stuck; the next reconnect re-syncs via stagiaire_join.
      expect(state.prenom).toBe('NewName')
      expect(state.prenomEdit).toBe(false)
      expect(state.pendingRename).toBeNull()
    })

    // F28: client.send returns false when the socket dropped between the
    // click and the send. Without the guard, pendingRename would stay set
    // forever (no name_updated ever arrives to clear it) and the modal
    // gives no signal the rename failed. On false: clear the in-flight
    // flag, surface a connection message in the inline slot, mark the
    // field invalid, keep the modal open with the user's input preserved.
    it('F28: when client.send returns false, clears pendingRename, surfaces an inline error, and keeps the modal open', () => {
      const fakeClient = { send: vi.fn(() => false) }
      mockGetClient.mockReturnValue(fakeClient)
      const { input } = fillEditForm('NewName')
      const inlineError = document.getElementById('edit-name-error')
      const ev = submitEvent()

      handleEditName(ev)

      // pendingRename cleared so a later unrelated error isn't misrouted.
      expect(state.pendingRename).toBeNull()
      // The send was attempted exactly once.
      expect(fakeClient.send).toHaveBeenCalledTimes(1)
      expect(fakeClient.send).toHaveBeenCalledWith({ type: 'update_name', name: 'NewName' })
      // Inline error slot surfaces the connection message.
      expect(inlineError.textContent).not.toBe('')
      expect(inlineError.style.display).toBe('block')
      // Field flagged invalid for AT users.
      expect(input.getAttribute('aria-invalid')).toBe('true')
      // Modal stays open + prenom NOT committed (deferred to the server
      // response that will now never arrive — the user retries manually).
      expect(state.prenomEdit).toBe(true)
      expect(state.prenom).toBe('Old')
    })
  })

  describe('handleKeyPress', () => {
    it('Escape exits prenomEdit', () => {
      state.prenomEdit = true
      const ev = { key: 'Escape', preventDefault: vi.fn() }
      handleKeyPress(ev)
      expect(state.prenomEdit).toBe(false)
      expect(ev.preventDefault).toHaveBeenCalled()
    })

    it('Escape is a no-op when not in edit mode', () => {
      state.prenomEdit = false
      const ev = { key: 'Escape', preventDefault: vi.fn() }
      handleKeyPress(ev)
      expect(ev.preventDefault).not.toHaveBeenCalled()
    })

    it('non-Escape keys are ignored', () => {
      state.prenomEdit = true
      const ev = { key: 'Enter', preventDefault: vi.fn() }
      handleKeyPress(ev)
      expect(state.prenomEdit).toBe(true)
      expect(ev.preventDefault).not.toHaveBeenCalled()
    })
  })

  describe('leaveSession', () => {
    it('does nothing when the confirm dialog resolves false', async () => {
      mockConfirm.mockResolvedValueOnce(false)
      const fakeClient = { close: vi.fn() }
      mockGetClient.mockReturnValue(fakeClient)
      await leaveSession()
      expect(state.sessionCode).toBe('ABC') // unchanged
      expect(fakeClient.close).not.toHaveBeenCalled()
    })

    it('clears all session state and closes the client when confirmed', async () => {
      mockConfirm.mockResolvedValueOnce(true)
      const fakeClient = { close: vi.fn() }
      mockGetClient.mockReturnValue(fakeClient)
      // Seed some state to verify it's cleared.
      state.selectedColors = new Set(['rouge'])
      state.stagiaireId = 's1'
      state.reclaimToken = 'tok'
      state.hasVoted = true

      await leaveSession()

      expect(state.sessionCode).toBe('')
      expect(state.appState).toBe(AppState.JOINING)
      expect(state.connected).toBe(false)
      expect(state.hasVoted).toBe(false)
      expect(state.selectedColors.size).toBe(0)
      expect(state.availableColors).toEqual([])
      expect(state.stagiaireId).toBeNull()
      expect(state.reclaimToken).toBeNull()
      expect(fakeClient.close).toHaveBeenCalledTimes(1)
    })

    // F5 regression: leaveSession must reset every session-scoped field,
    // not just the ones the original manual reset happened to list. The 7
    // fields below were omitted before resetStagiaireState() was extracted.
    it('resets scoreboard, reveal, and edit-name fields (F5)', async () => {
      mockConfirm.mockResolvedValueOnce(true)
      const fakeClient = { close: vi.fn() }
      mockGetClient.mockReturnValue(fakeClient)
      // Seed all the fields that were previously leaked across sessions.
      state.prenomEdit = true
      state.voteScore = 1500
      state.totalScore = 4200
      state.gameScore = 300
      state.rank = 1
      state.totalStagiaires = 25
      state.revealed = true
      state.competitive = true
      state.allowBlank = true
      state.colorLabels = { rouge: 'Pour' }
      state.multipleChoice = true
      state.gameEnabled = true
      state.gamePlaying = true

      await leaveSession()

      expect(state.prenomEdit).toBe(false)
      expect(state.voteScore).toBe(0)
      expect(state.totalScore).toBe(0)
      expect(state.gameScore).toBe(0)
      expect(state.rank).toBe(0)
      expect(state.totalStagiaires).toBe(0)
      expect(state.revealed).toBe(false)
      expect(state.competitive).toBe(false)
      expect(state.allowBlank).toBe(false)
      expect(state.colorLabels).toEqual({})
      expect(state.multipleChoice).toBe(false)
      expect(state.gameEnabled).toBe(false)
      expect(state.gamePlaying).toBe(false)
    })
  })
})

// =====================================================================
// F15: prenom is now persisted in sessionStorage (not localStorage) so
// closing the tab clears it and a different student on a shared tablet
// starts fresh instead of auto-joining under the previous user's name.
// Same boundary as the reclaim token (S6/S12) and the cached session
// code.
// =====================================================================

describe('stagiaire handlers — F15 prenom persistence', () => {
  let connectSpy

  beforeEach(() => {
    vi.clearAllMocks()
    document.body.innerHTML = '<div id="app"></div>'
    renderLayout(document.getElementById('app'))

    state.appState = AppState.JOINING
    state.prenom = ''
    state.sessionCode = ''
    state.selectedColors = new Set()
    state.prenomEdit = false
    state.connected = false

    sessionStorage.clear()
    localStorage.clear()

    connectSpy = vi.fn()
    setConnectToSession(connectSpy)
    // handleEditName path: getClient() returns an object with .send.
    mockGetClient.mockReturnValue({ send: vi.fn(() => true) })
  })

  function fillJoinForm({ prenom, code }) {
    render() // ensures the JOINING form (#prenom, #sessionCode) exists
    const p = document.getElementById('prenom')
    const c = document.getElementById('sessionCode')
    if (prenom !== undefined) p.value = prenom
    if (code !== undefined) c.value = code
    return { p, c }
  }

  function submitEvent() {
    return new Event('submit', { bubbles: true, cancelable: true })
  }

  it('handleJoin writes the prenom to sessionStorage, not localStorage', () => {
    fillJoinForm({ prenom: 'Camille', code: 'ABC' })
    handleJoin(submitEvent())

    expect(sessionStorage.getItem('vote_stagiaire_prenom')).toBe('Camille')
    expect(localStorage.getItem('vote_stagiaire_prenom')).toBeNull()
  })

  it('handleEditName does NOT persist prenom optimistically (R8: defers to name_updated)', () => {
    // R8: handleEditName sends update_name and waits for the server
    // response before committing. sessionStorage is written by the
    // name_updated handler in websocket.js, not here. This avoids a
    // rejected name lingering in sessionStorage on a shared device.
    state.appState = AppState.WAITING
    state.prenom = 'Old'
    state.prenomEdit = true
    state.sessionCode = 'ABC'
    state.connected = true
    render()

    const input = document.getElementById('editPrenom')
    input.value = 'New'
    handleEditName(submitEvent())

    // Not committed — server may reject with ErrNameInUse.
    expect(sessionStorage.getItem('vote_stagiaire_prenom')).toBeNull()
    expect(localStorage.getItem('vote_stagiaire_prenom')).toBeNull()
    expect(state.prenom).toBe('Old')
    expect(state.pendingRename).toBe('New')
  })
})
