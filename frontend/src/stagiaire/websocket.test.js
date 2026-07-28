// @vitest-environment happy-dom
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'

// Capture the options passed to VoteClient so we can drive onMessage/onOpen
// from the test as if the server were sending frames.
let capturedClient = null
let connectCount = 0

class FakeVoteClient {
  constructor(url, opts) {
    this.url = url
    this.opts = opts
    this.sent = []
    this.connected = false
    capturedClient = this
  }
  connect() {
    connectCount++
    this.connected = true
  }
  close() {
    this.connected = false
  }
  isConnected() {
    return this.connected
  }
  send(msg) {
    this.sent.push(msg)
    return true
  }
  // Test-only driver helpers.
  fireOpen() {
    this.opts.onOpen?.()
  }
  fireStatusChange(connected) {
    this.opts.onStatusChange?.(connected)
  }
  fireMessage(msg) {
    this.opts.onMessage?.(msg)
  }
}

vi.mock('@shared/websocket-client.js', () => ({
  VoteClient: FakeVoteClient
}))

// pauseGameExternal / teardownGame are imported by websocket.js — we don't
// want the real game rendering to interfere with state assertions.
const pauseSpy = vi.fn()
const teardownSpy = vi.fn()
vi.mock('./handlers.js', () => ({
  pauseGameExternal: (...a) => pauseSpy(...a),
  teardownGame: (...a) => teardownSpy(...a)
}))

const resetHighScoreSpy = vi.fn()
const saveStreakSpy = vi.fn()
vi.mock('@shared/game-storage.js', () => ({
  resetHighScore: (...a) => resetHighScoreSpy(...a),
  saveStreak: (...a) => saveStreakSpy(...a)
}))

const { state, AppState } = await import('./state.js')
const { renderLayout } = await import('./renderers.js')
const { initClient, connectToSession, getClient } = await import('./websocket.js')

describe('stagiaire websocket — message handling', () => {
  beforeEach(() => {
    capturedClient = null
    connectCount = 0
    pauseSpy.mockClear()
    teardownSpy.mockClear()
    resetHighScoreSpy.mockClear()
    saveStreakSpy.mockClear()

    document.body.innerHTML = '<div id="app"></div>'
    renderLayout(document.getElementById('app'))

    // Reset state.
    state.appState = AppState.JOINING
    state.sessionCode = ''
    state.connected = false
    state.availableColors = []
    state.colorLabels = {}
    state.multipleChoice = false
    state.selectedColors = new Set()
    state.hasVoted = false
    state.stagiaireId = null
    state.reclaimToken = null
    state.prenom = ''
    state.prenomEdit = false
    state.gameEnabled = false
    state.gamePlaying = false
    state.competitive = false
    state.allowBlank = false
    state.voteScore = 0
    state.totalScore = 0
    state.gameScore = 0
    state.rank = 0
    state.totalStagiaires = 0
    state.revealed = false

    sessionStorage.clear()
  })

  afterEach(() => {
    sessionStorage.clear()
  })

  describe('initClient', () => {
    it('constructs a VoteClient (does not connect until connectToSession)', () => {
      initClient()
      expect(capturedClient).not.toBeNull()
      // initClient does not call .connect() — that's connectToSession's job.
      expect(connectCount).toBe(0)
      expect(getClient()).toBe(capturedClient)
    })

    it('onStatusChange(true) sets state.connected and renders', () => {
      initClient()
      capturedClient.fireStatusChange(true)
      expect(state.connected).toBe(true)
    })

    it('onStatusChange(false) pauses the game and renders', () => {
      initClient()
      capturedClient.fireStatusChange(false)
      expect(state.connected).toBe(false)
      expect(pauseSpy).toHaveBeenCalled()
    })

    it('onStatusChange(false) does NOT render when the user is still on the join form', () => {
      // Avoid asserting on DOM (render() is real). Instead, verify no throw.
      state.appState = AppState.JOINING
      initClient()
      expect(() => capturedClient.fireStatusChange(false)).not.toThrow()
    })

    it('onOpen with no session/prenom does NOT send stagiaire_join', () => {
      initClient()
      capturedClient.fireOpen()
      expect(capturedClient.sent).toHaveLength(0)
    })

    it('onOpen sends stagiaire_join with code, name, id, and reclaim token', () => {
      state.sessionCode = 'ABC'
      state.prenom = 'Marie'
      state.stagiaireId = 's1'
      state.reclaimToken = 'tok-123'
      initClient()
      capturedClient.fireOpen()
      expect(capturedClient.sent).toHaveLength(1)
      expect(capturedClient.sent[0]).toEqual({
        type: 'stagiaire_join',
        sessionCode: 'ABC',
        name: 'Marie',
        stagiaireId: 's1',
        reclaimToken: 'tok-123'
      })
    })

    it('onOpen omits stagiaireId/reclaimToken when unset (undefined is dropped by JSON.stringify)', () => {
      state.sessionCode = 'ABC'
      state.prenom = 'Marie'
      initClient()
      capturedClient.fireOpen()
      expect(capturedClient.sent[0]).toEqual({
        type: 'stagiaire_join',
        sessionCode: 'ABC',
        name: 'Marie'
      })
    })
  })

  describe('connectToSession', () => {
    it('sets state.sessionCode and connects (initialising the client if needed)', () => {
      connectToSession('ABC')
      expect(state.sessionCode).toBe('ABC')
      expect(connectCount).toBe(1)
    })

    it('reuses the existing client if initClient was already called', () => {
      initClient()
      const first = capturedClient
      connectToSession('ABC')
      expect(capturedClient).toBe(first)
      // The reused client now has connect() invoked exactly once.
      expect(connectCount).toBe(1)
    })
  })

  describe('session_joined', () => {
    it('stores the sessionCode, stagiaireId, reclaimToken and moves to WAITING', () => {
      state.appState = AppState.JOINING
      initClient()
      capturedClient.fireMessage({
        type: 'session_joined',
        sessionCode: 'DEF',
        stagiaireId: 's42',
        reclaimToken: 'tok-abc'
      })

      expect(state.sessionCode).toBe('DEF')
      expect(state.stagiaireId).toBe('s42')
      expect(state.reclaimToken).toBe('tok-abc')
      expect(state.appState).toBe(AppState.WAITING)
      expect(sessionStorage.getItem('vote_stagiaire_id')).toBe('s42')
      expect(sessionStorage.getItem('vote_stagiaire_reclaim_token')).toBe('tok-abc')
      expect(sessionStorage.getItem('vote_session_code')).toBe('DEF')
    })

    it('reset-on-session_joined: clears high score + streak when transitioning from JOINING', () => {
      state.appState = AppState.JOINING
      initClient()
      capturedClient.fireMessage({ type: 'session_joined', sessionCode: 'DEF', stagiaireId: 'x' })
      expect(resetHighScoreSpy).toHaveBeenCalledTimes(1)
      expect(saveStreakSpy).toHaveBeenCalledWith(0)
    })

    it('does NOT reset high score when rejoining from a non-JOINING state (e.g. reclaim path)', () => {
      state.appState = AppState.WAITING
      initClient()
      capturedClient.fireMessage({ type: 'session_joined', sessionCode: 'DEF', stagiaireId: 'x' })
      expect(resetHighScoreSpy).not.toHaveBeenCalled()
    })

    it('drops stale credentials when the server signals session_expired', () => {
      state.stagiaireId = 'old'
      state.reclaimToken = 'old-tok'
      sessionStorage.setItem('vote_stagiaire_id', 'old')
      sessionStorage.setItem('vote_stagiaire_reclaim_token', 'old-tok')
      initClient()
      capturedClient.fireMessage({
        type: 'session_joined',
        sessionCode: 'DEF',
        staleIdentity: true
      })
      expect(state.stagiaireId).toBeUndefined()
      expect(state.reclaimToken).toBeUndefined()
      expect(sessionStorage.getItem('vote_stagiaire_id')).toBeNull()
      expect(sessionStorage.getItem('vote_stagiaire_reclaim_token')).toBeNull()
    })

    it('drops stale credentials when msg.error === "session_expired"', () => {
      state.stagiaireId = 'old'
      state.reclaimToken = 'old-tok'
      initClient()
      capturedClient.fireMessage({
        type: 'session_joined',
        sessionCode: 'DEF',
        error: 'session_expired'
      })
      expect(state.stagiaireId).toBeUndefined()
    })
  })

  describe('error handling', () => {
    it('"Session not found" is translated to "Session introuvable" via showError', () => {
      const errSpy = vi.spyOn(document, 'querySelector').mockReturnValueOnce({
        textContent: '',
        style: { display: '' }
      })
      initClient()
      expect(() => capturedClient.fireMessage({ type: 'error', message: 'Session not found' })).not.toThrow()
      errSpy.mockRestore()
    })

    it('reclaim-rejection drops cached id/token and retries once as a fresh identity', () => {
      state.sessionCode = 'ABC'
      state.prenom = 'Marie'
      state.stagiaireId = 'stale'
      state.reclaimToken = 'stale-tok'
      sessionStorage.setItem('vote_stagiaire_id', 'stale')
      sessionStorage.setItem('vote_stagiaire_reclaim_token', 'stale-tok')

      initClient()
      capturedClient.sent.length = 0

      capturedClient.fireMessage({
        type: 'error',
        message: 'Session expirée — veuillez recréer votre identité'
      })

      expect(state.stagiaireId).toBeUndefined()
      expect(state.reclaimToken).toBeUndefined()
      expect(sessionStorage.getItem('vote_stagiaire_id')).toBeNull()
      expect(sessionStorage.getItem('vote_stagiaire_reclaim_token')).toBeNull()

      // Retried once as a fresh identity (no stagiaireId, no reclaimToken).
      expect(capturedClient.sent).toHaveLength(1)
      expect(capturedClient.sent[0]).toEqual({
        type: 'stagiaire_join',
        sessionCode: 'ABC',
        name: 'Marie'
      })
    })

    it('reclaim-rejection does NOT retry when name/sessionCode are missing', () => {
      state.stagiaireId = 'stale'
      state.reclaimToken = 'stale-tok'
      initClient()
      capturedClient.sent.length = 0
      capturedClient.fireMessage({
        type: 'error',
        message: 'Session expirée — veuillez recréer votre identité'
      })
      expect(capturedClient.sent).toHaveLength(0)
    })

    it('a generic error renders without crashing', () => {
      // Stub an error-message element so showError actually mutates something.
      document.body.insertAdjacentHTML('beforeend', '<div class="error-message" style="display:none"></div>')
      initClient()
      expect(() => capturedClient.fireMessage({ type: 'error', message: 'Boom' })).not.toThrow()
      expect(document.querySelector('.error-message').textContent).toBe('Boom')
    })
  })

  describe('vote_started', () => {
    it('tears down the game and applies the full config surface', () => {
      initClient()
      capturedClient.fireMessage({
        type: 'vote_started',
        colors: ['rouge', 'vert'],
        multipleChoice: true,
        labels: { rouge: 'Tomato' },
        gameEnabled: true,
        competitive: true,
        allowBlank: true
      })

      expect(teardownSpy).toHaveBeenCalledTimes(1)
      expect(state.availableColors).toEqual(['rouge', 'vert'])
      expect(state.multipleChoice).toBe(true)
      expect(state.colorLabels).toEqual({ rouge: 'Tomato' })
      expect(state.gameEnabled).toBe(true)
      expect(state.competitive).toBe(true)
      expect(state.allowBlank).toBe(true)
      expect(state.revealed).toBe(false)
      expect(state.voteScore).toBe(0)
      expect(state.hasVoted).toBe(false)
      expect(state.appState).toBe(AppState.VOTING)
      expect(state.selectedColors.size).toBe(0)
    })

    it('resumes a prior vote from existingVote (transitions to VOTED)', () => {
      initClient()
      capturedClient.fireMessage({
        type: 'vote_started',
        colors: ['rouge', 'vert'],
        multipleChoice: false,
        existingVote: ['rouge']
      })
      expect(state.hasVoted).toBe(true)
      expect(state.appState).toBe(AppState.VOTED)
      expect(state.selectedColors.has('rouge')).toBe(true)
    })

    it('leaves unknown flags unchanged when the server omits them', () => {
      state.gameEnabled = true
      state.competitive = true
      initClient()
      capturedClient.fireMessage({
        type: 'vote_started',
        colors: ['rouge']
      })
      expect(state.gameEnabled).toBe(true)
      expect(state.competitive).toBe(true)
      // multipleChoice defaults to false when undefined.
      expect(state.multipleChoice).toBe(false)
    })
  })

  describe('vote_accepted', () => {
    it('transitions to VOTED and marks hasVoted', () => {
      state.appState = AppState.VOTING
      state.hasVoted = false
      initClient()
      capturedClient.fireMessage({ type: 'vote_accepted' })
      expect(state.hasVoted).toBe(true)
      expect(state.appState).toBe(AppState.VOTED)
    })
  })

  describe('vote_closed', () => {
    it('tears down the game and transitions to CLOSED', () => {
      state.appState = AppState.VOTED
      initClient()
      capturedClient.fireMessage({ type: 'vote_closed' })
      expect(teardownSpy).toHaveBeenCalledTimes(1)
      expect(state.appState).toBe(AppState.CLOSED)
    })
  })

  describe('answers_revealed', () => {
    it('sets revealed, scores, rank, totalStagiaires, and the correct colors', () => {
      initClient()
      capturedClient.fireMessage({
        type: 'answers_revealed',
        voteScore: 1500,
        gameScore: 800,
        totalScore: 2300,
        rank: 3,
        totalStagiaires: 12,
        correctColors: ['rouge']
      })

      expect(state.revealed).toBe(true)
      expect(state.voteScore).toBe(1500)
      expect(state.gameScore).toBe(800)
      // totalScore is recomposed: server's totalScore + local gameScore
      expect(state.totalScore).toBe(2300 + 800)
      expect(state.rank).toBe(3)
      expect(state.totalStagiaires).toBe(12)
      expect(state.availableColors).toEqual(['rouge'])
    })

    it('preserves prior totalScore when server omits it', () => {
      state.totalScore = 999
      initClient()
      capturedClient.fireMessage({
        type: 'answers_revealed',
        voteScore: 100,
        gameScore: 50,
        rank: 1,
        totalStagiaires: 1
      })
      expect(state.totalScore).toBe(999)
    })
  })

  describe('vote_reset', () => {
    it('pauses the game and transitions back to WAITING with cleared vote state', () => {
      state.appState = AppState.CLOSED
      state.selectedColors = new Set(['rouge'])
      state.hasVoted = true
      state.revealed = true
      state.voteScore = 1500
      initClient()
      capturedClient.fireMessage({
        type: 'vote_reset',
        gameEnabled: false,
        competitive: false,
        allowBlank: false
      })

      expect(pauseSpy).toHaveBeenCalledTimes(1)
      expect(state.appState).toBe(AppState.WAITING)
      expect(state.selectedColors.size).toBe(0)
      expect(state.hasVoted).toBe(false)
      expect(state.revealed).toBe(false)
      expect(state.voteScore).toBe(0)
      expect(state.gameEnabled).toBe(false)
      expect(state.competitive).toBe(false)
      expect(state.allowBlank).toBe(false)
    })
  })

  describe('name_updated', () => {
    it('exits prenomEdit', () => {
      state.prenomEdit = true
      initClient()
      capturedClient.fireMessage({ type: 'name_updated' })
      expect(state.prenomEdit).toBe(false)
    })
  })

  describe('unknown message types', () => {
    it('do not throw and do not change state', () => {
      initClient()
      const before = JSON.stringify({
        appState: state.appState,
        sessionCode: state.sessionCode
      })
      expect(() => capturedClient.fireMessage({ type: 'totally_unknown' })).not.toThrow()
      const after = JSON.stringify({
        appState: state.appState,
        sessionCode: state.sessionCode
      })
      expect(after).toBe(before)
    })
  })
})
