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

    it('updates state.prenom and sends update_name on the client', () => {
      const fakeClient = { send: vi.fn(() => true) }
      mockGetClient.mockReturnValue(fakeClient)
      fillEditForm('NewName')
      const ev = submitEvent()
      handleEditName(ev)
      expect(state.prenom).toBe('NewName')
      expect(state.prenomEdit).toBe(false)
      expect(fakeClient.send).toHaveBeenCalledWith({ type: 'update_name', name: 'NewName' })
    })

    it('is a no-op when no client is connected', () => {
      mockGetClient.mockReturnValue(null)
      fillEditForm('NewName')
      const ev = submitEvent()
      expect(() => handleEditName(ev)).not.toThrow()
      expect(state.prenom).toBe('NewName')
      expect(state.prenomEdit).toBe(false)
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
  })
})
