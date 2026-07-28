// @vitest-environment happy-dom
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'

// Drive the score animation by mocking the Mastermind class so the game
// starts already in the "won" state. renderBoard will then call
// showEndScreen → animateScoreCountUp → requestAnimationFrame.
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

const { state, AppState } = await import('./state.js')
const { renderLayout } = await import('./renderers.js')
const { handlePlayGame, handleQuitGame, teardownGame, setConnectToSession } = await import('./handlers.js')

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

    // Fake rAF: return a monotonically increasing id and remember it so the
    // test can assert cancelAnimationFrame was called.
    let nextId = 1
    rafSpy = vi.spyOn(globalThis, 'requestAnimationFrame').mockImplementation((_cb) => {
      const id = nextId++
      return id
    })
    cancelSpy = vi.spyOn(globalThis, 'cancelAnimationFrame').mockImplementation(() => {})
  })

  afterEach(() => {
    rafSpy.mockRestore()
    cancelSpy.mockRestore()
    vi.useRealTimers()
  })

  it('teardownGame cancels the pending score animation rAF', async () => {
    handlePlayGame()
    // handlePlayGame → createGame() → renderBoard() → showEndScreen (status='won')
    // → animateScoreCountUp → requestAnimationFrame.
    expect(rafSpy).toHaveBeenCalled()

    // Before the fix, teardownGame did NOT cancel — the rAF kept ticking
    // against the (now-hidden) overlay element.
    teardownGame()

    expect(cancelSpy).toHaveBeenCalled()
    // Overlay is hidden after teardown.
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
    // No handlePlayGame → no animation scheduled.
    teardownGame()
    expect(cancelSpy).not.toHaveBeenCalled()
  })
})
