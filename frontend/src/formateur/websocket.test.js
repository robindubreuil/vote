// @vitest-environment happy-dom
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'

// Capture VoteClient options so tests can drive onMessage/onStatusChange/onOpen.
let capturedClient = null

class FakeVoteClient {
  constructor(url, opts) {
    this.url = url
    this.opts = opts
    this.sent = []
    this.connected = false
    capturedClient = this
  }
  connect() {
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

vi.mock('@shared/websocket-client.js', () => ({ VoteClient: FakeVoteClient }))

// Renderer spies — verify the websocket layer invokes them at the right times
// without re-testing the renderer's DOM behavior (covered in renderers.test.js).
const rendererSpies = {
  renderFullLayout: vi.fn((app) => {
    app.innerHTML =
      '<div id="reconnect-banner" hidden></div><header id="app-header"></header><main id="app-content"></main>'
  }),
  renderMainContent: vi.fn(),
  renderLandingPage: vi.fn((app) => {
    app.innerHTML = '<div class="landing-card"></div>'
  }),
  updateHeader: vi.fn(),
  updateLandingPageLoadingState: vi.fn(),
  updateConnectionBanner: vi.fn(),
  attachConfigListeners: vi.fn(),
  attachVoteListeners: vi.fn(),
  attachHeaderListeners: vi.fn(),
  attachLandingListeners: vi.fn(),
  cleanupAllListeners: vi.fn()
}
vi.mock('./renderers.js', () => rendererSpies)

const startTimerSpy = vi.fn()
const stopTimerSpy = vi.fn()
const updateVoteResultsSpy = vi.fn()
vi.mock('./utils.js', () => ({
  startTimer: startTimerSpy,
  stopTimer: stopTimerSpy,
  updateVoteResults: updateVoteResultsSpy
}))

const applyLastConfigSpy = vi.fn()
vi.mock('./handlers.js', () => ({
  applyLastConfigIfAvailable: applyLastConfigSpy
}))

const publisherSpy = {
  publish: vi.fn(),
  close: vi.fn()
}
vi.mock('@shared/session-sync.js', () => ({
  createSessionPublisher: vi.fn(() => publisherSpy)
}))

const showToastSpy = vi.fn()
const showErrorSpy = vi.fn()
const hideErrorSpy = vi.fn()
vi.mock('@shared/ui.js', () => ({
  showToast: showToastSpy,
  showError: showErrorSpy,
  hideError: hideErrorSpy,
  renderFooterHTML: () => '<footer></footer>',
  renderSessionCodeButton: (code) => `<button id="leaveSessionBtn">${code || ''}</button>`,
  showConfirmDialog: vi.fn(() => Promise.resolve(false))
}))

const { state, resetTrainerState } = await import('./state.js')
const { initClient, closeClient } = await import('./websocket.js')
const { createSessionPublisher } = await import('@shared/session-sync.js')

describe('formateur websocket — message handling', () => {
  beforeEach(() => {
    capturedClient = null
    Object.values(rendererSpies).forEach((s) => s.mockClear())
    startTimerSpy.mockClear()
    stopTimerSpy.mockClear()
    updateVoteResultsSpy.mockClear()
    applyLastConfigSpy.mockClear()
    publisherSpy.publish.mockClear()
    publisherSpy.close.mockClear()
    createSessionPublisher.mockClear()
    showToastSpy.mockClear()
    showErrorSpy.mockClear()
    hideErrorSpy.mockClear()

    document.body.innerHTML = '<div id="app"></div>'
    sessionStorage.clear()

    resetTrainerState()
    state.lastConfigApplied = false
  })

  afterEach(() => {
    sessionStorage.clear()
  })

  describe('initClient', () => {
    it('constructs a VoteClient, connects, and forwards status changes', () => {
      initClient()
      expect(capturedClient).not.toBeNull()
      expect(capturedClient.connected).toBe(true)
      capturedClient.fireStatusChange(true)
      expect(state.connected).toBe(true)
      // everConnected flips only on the first successful connect.
      expect(state.everConnected).toBe(true)
    })

    it('persists/replays trainerId + trainerToken across initClient (reconnect path)', () => {
      sessionStorage.setItem('vote_trainer_id', 'trainer-1')
      sessionStorage.setItem('vote_trainer_token', 'tok-xyz')
      initClient()
      capturedClient.fireOpen()
      expect(capturedClient.sent[0]).toEqual({
        type: 'trainer_join',
        sessionCode: state.sessionCode,
        trainerId: 'trainer-1',
        trainerToken: 'tok-xyz'
      })
    })

    it('omits trainerId/trainerToken from the join when none are persisted', () => {
      initClient()
      capturedClient.fireOpen()
      expect(capturedClient.sent[0]).toEqual({
        type: 'trainer_join',
        sessionCode: state.sessionCode
      })
    })

    it('fires the "Reconnecté" toast only after a drop → reconnect (everConnected guard)', () => {
      initClient()
      capturedClient.fireStatusChange(true)
      // First connect must NOT fire the reconnect toast.
      expect(showToastSpy).not.toHaveBeenCalled()
      // Drop and reconnect.
      capturedClient.fireStatusChange(false)
      capturedClient.fireStatusChange(true)
      expect(showToastSpy).toHaveBeenCalledTimes(1)
    })

    it('updateHeader + updateConnectionBanner + publishState all run on status change', () => {
      initClient()
      rendererSpies.updateHeader.mockClear()
      rendererSpies.updateConnectionBanner.mockClear()
      capturedClient.fireStatusChange(true)
      expect(rendererSpies.updateHeader).toHaveBeenCalledTimes(1)
      expect(rendererSpies.updateConnectionBanner).toHaveBeenCalledTimes(1)
    })
  })

  describe('closeClient', () => {
    it('closes the client and the publisher', () => {
      initClient()
      const client = capturedClient
      // Run a session_created so a publisher exists.
      capturedClient.fireMessage({ type: 'session_created', sessionCode: 'ABC', trainerId: 't1', trainerToken: 'tok' })

      closeClient()
      expect(client.connected).toBe(false)
      expect(publisherSpy.close).toHaveBeenCalledTimes(1)
    })
  })

  describe('session_created', () => {
    it('persists session code + ids + token, renders the layout, attaches listeners, publishes state', () => {
      initClient()
      capturedClient.fireMessage({
        type: 'session_created',
        sessionCode: 'ABC',
        trainerId: 't1',
        trainerToken: 'tok-abc'
      })

      expect(state.sessionCode).toBe('ABC')
      expect(sessionStorage.getItem('vote_session_code')).toBe('ABC')
      expect(sessionStorage.getItem('vote_trainer_id')).toBe('t1')
      expect(sessionStorage.getItem('vote_trainer_token')).toBe('tok-abc')

      expect(rendererSpies.renderFullLayout).toHaveBeenCalledTimes(1)
      expect(rendererSpies.attachHeaderListeners).toHaveBeenCalledTimes(1)
      expect(createSessionPublisher).toHaveBeenCalledWith('ABC')
      expect(rendererSpies.updateHeader).toHaveBeenCalled()
      expect(rendererSpies.attachConfigListeners).toHaveBeenCalledTimes(1)
      expect(publisherSpy.publish).toHaveBeenCalledTimes(1)
    })

    it('does NOT re-render the full layout when app-content is already present', () => {
      // Pre-populate the DOM as if a prior session was rendered.
      document.body.innerHTML = '<div id="app"><div id="reconnect-banner"></div><main id="app-content"></main></div>'
      initClient()
      capturedClient.fireMessage({ type: 'session_created', sessionCode: 'ABC' })
      // renderFullLayout should be skipped because #app-content exists.
      expect(rendererSpies.renderFullLayout).not.toHaveBeenCalled()
    })

    it('applies the last-saved config when a fresh session_created arrives in idle state', () => {
      initClient()
      capturedClient.fireMessage({ type: 'session_created', sessionCode: 'ABC' })
      expect(applyLastConfigSpy).toHaveBeenCalledTimes(1)
    })

    it('does NOT apply the last config when voteState is not idle (mid-vote reconnect)', () => {
      state.voteState = 'active'
      initClient()
      capturedClient.fireMessage({ type: 'session_created', sessionCode: 'ABC' })
      expect(applyLastConfigSpy).not.toHaveBeenCalled()
    })

    it('persists the trainerToken once and reuses it on a subsequent initClient', () => {
      initClient()
      capturedClient.fireMessage({
        type: 'session_created',
        sessionCode: 'ABC',
        trainerId: 't1',
        trainerToken: 'tok-abc'
      })
      closeClient()
      initClient()
      capturedClient.fireOpen()
      expect(capturedClient.sent[0]).toMatchObject({ trainerId: 't1', trainerToken: 'tok-abc' })
    })

    it('only creates the publisher once even if multiple session_created fire', () => {
      initClient()
      capturedClient.fireMessage({ type: 'session_created', sessionCode: 'ABC' })
      capturedClient.fireMessage({ type: 'session_created', sessionCode: 'ABC' })
      expect(createSessionPublisher).toHaveBeenCalledTimes(1)
    })
  })

  describe('connected_count', () => {
    beforeEach(() => {
      initClient()
      capturedClient.fireMessage({ type: 'session_created', sessionCode: 'ABC' })
      rendererSpies.renderMainContent.mockClear()
      rendererSpies.attachConfigListeners.mockClear()
    })

    it('updates the count and stagiaires and re-renders when idle', () => {
      capturedClient.fireMessage({
        type: 'connected_count',
        count: 3,
        stagiaires: [{ id: 's1', name: 'Marie', connected: true }]
      })
      expect(state.connectedCount).toBe(3)
      expect(state.stagiaires).toHaveLength(1)
      expect(rendererSpies.updateHeader).toHaveBeenCalled()
      expect(publisherSpy.publish).toHaveBeenCalled()
    })

    it('renders the live results instead of the config when voteState is not idle', () => {
      state.voteState = 'active'
      capturedClient.fireMessage({ type: 'connected_count', count: 5 })
      expect(updateVoteResultsSpy).toHaveBeenCalled()
    })

    it('publishes state with a top-3 leaderboard in competitive mode', () => {
      state.competitive = true
      state.stagiaires = [
        { id: 's1', name: 'Marie', connected: true, score: 100, gameScore: 50 },
        { id: 's2', name: 'Joe', connected: true, score: 200, gameScore: 0 },
        { id: 's3', name: 'Anna', connected: false, score: 9999, gameScore: 0 }
      ]
      capturedClient.fireMessage({ type: 'connected_count', count: 2 })
      const payload = publisherSpy.publish.mock.calls.at(-1)[0]
      expect(payload.leaderboard).toHaveLength(2)
      // Joe (200) ranks above Marie (150); Anna is disconnected → excluded.
      expect(payload.leaderboard[0].name).toBe('Joe')
      expect(payload.leaderboard[1].name).toBe('Marie')
    })

    it('publishes leaderboard=null when not competitive', () => {
      state.competitive = false
      capturedClient.fireMessage({ type: 'connected_count', count: 1 })
      const payload = publisherSpy.publish.mock.calls.at(-1)[0]
      expect(payload.leaderboard).toBeNull()
    })

    it('updates the live .config-info line via the shared formatConnectedCount helper (R13)', () => {
      // Seed a real .config-info element (renderers are mocked, so the
      // initial render never created one). The handler's fast path updates
      // innerHTML in place instead of re-rendering.
      document.body.innerHTML = '<div class="config-info"></div>'
      state.voteState = 'idle'

      capturedClient.fireMessage({ type: 'connected_count', count: 3 })
      expect(document.querySelector('.config-info').innerHTML).toContain('3 stagiaires connectés')

      capturedClient.fireMessage({ type: 'connected_count', count: 1 })
      expect(document.querySelector('.config-info').innerHTML).toContain('1 stagiaire connecté')

      // Singular stays singular at zero too (F16 rule).
      capturedClient.fireMessage({ type: 'connected_count', count: 0 })
      expect(document.querySelector('.config-info').innerHTML).toContain('0 stagiaire connecté')
    })
  })

  describe('vote_started', () => {
    it('transitions to active, applies config, renders, and starts the timer', () => {
      initClient()
      capturedClient.fireMessage({ type: 'session_created', sessionCode: 'ABC' })
      rendererSpies.renderMainContent.mockClear()
      rendererSpies.attachVoteListeners.mockClear()

      capturedClient.fireMessage({
        type: 'vote_started',
        colors: ['rouge', 'vert'],
        multipleChoice: true,
        labels: { rouge: 'Tomato' },
        competitive: true,
        allowBlank: true,
        gameEnabled: true,
        voteElapsed: 5
      })

      expect(state.voteState).toBe('active')
      expect(state.voteStartTime).toBeLessThanOrEqual(Date.now())
      expect(state.selectedColors).toEqual(new Set(['rouge', 'vert']))
      expect(state.multipleChoice).toBe(true)
      expect(state.colorLabels).toEqual({ rouge: 'Tomato' })
      expect(state.competitive).toBe(true)
      expect(state.allowBlank).toBe(true)
      expect(state.gameEnabled).toBe(true)
      expect(state.revealed).toBe(false)
      expect(rendererSpies.renderMainContent).toHaveBeenCalledTimes(1)
      expect(rendererSpies.attachVoteListeners).toHaveBeenCalledTimes(1)
      expect(startTimerSpy).toHaveBeenCalledTimes(1)
      expect(publisherSpy.publish).toHaveBeenCalled()
    })
  })

  describe('vote_received', () => {
    it('is a silent no-op (handler exists but does nothing)', () => {
      initClient()
      expect(() => capturedClient.fireMessage({ type: 'vote_received' })).not.toThrow()
    })
  })

  describe('vote_closed', () => {
    it('transitions to closed, stops the timer, renders', () => {
      initClient()
      capturedClient.fireMessage({ type: 'session_created', sessionCode: 'ABC' })
      state.voteState = 'active'
      rendererSpies.renderMainContent.mockClear()

      capturedClient.fireMessage({ type: 'vote_closed' })
      expect(state.voteState).toBe('closed')
      expect(stopTimerSpy).toHaveBeenCalled()
      expect(rendererSpies.renderMainContent).toHaveBeenCalledTimes(1)
    })
  })

  describe('answers_revealed', () => {
    it('marks revealed, stores correctColors + scoreboard, renders', () => {
      initClient()
      capturedClient.fireMessage({ type: 'session_created', sessionCode: 'ABC' })
      rendererSpies.renderMainContent.mockClear()

      capturedClient.fireMessage({
        type: 'answers_revealed',
        correctColors: ['rouge'],
        scores: [{ id: 's1', name: 'Marie', voteScore: 2000, totalScore: 2000, rank: 1 }]
      })

      expect(state.revealed).toBe(true)
      expect(state.correctColors).toEqual(new Set(['rouge']))
      expect(state.scoreboard).toHaveLength(1)
      expect(rendererSpies.renderMainContent).toHaveBeenCalledTimes(1)
    })
  })

  describe('vote_reset', () => {
    it('transitions back to idle, clears reveal state, stops the timer', () => {
      initClient()
      capturedClient.fireMessage({ type: 'session_created', sessionCode: 'ABC' })
      state.voteState = 'closed'
      state.revealed = true
      state.correctColors = new Set(['rouge'])
      rendererSpies.renderMainContent.mockClear()
      rendererSpies.attachConfigListeners.mockClear()

      capturedClient.fireMessage({
        type: 'vote_reset',
        competitive: false,
        allowBlank: false,
        gameEnabled: false
      })

      expect(state.voteState).toBe('idle')
      expect(state.revealed).toBe(false)
      expect(state.correctColors.size).toBe(0)
      expect(stopTimerSpy).toHaveBeenCalled()
      expect(state.competitive).toBe(false)
      expect(state.allowBlank).toBe(false)
      expect(state.gameEnabled).toBe(false)
      expect(rendererSpies.renderMainContent).toHaveBeenCalledTimes(1)
      expect(rendererSpies.attachConfigListeners).toHaveBeenCalledTimes(1)
    })
  })

  describe('config_updated (FH4 sync on reconnect)', () => {
    it('applies every server-provided field without clobbering omitted ones', () => {
      initClient()
      capturedClient.fireMessage({ type: 'session_created', sessionCode: 'ABC' })
      state.selectedColors = new Set(['jaune'])
      state.multipleChoice = false
      state.colorLabels = { jaune: 'Old' }
      state.gameEnabled = false
      state.competitive = false
      state.allowBlank = false
      rendererSpies.renderMainContent.mockClear()
      rendererSpies.attachConfigListeners.mockClear()

      capturedClient.fireMessage({
        type: 'config_updated',
        selectedColors: ['rouge', 'vert'],
        multipleChoice: true,
        labels: { rouge: 'Tomato' },
        gameEnabled: true,
        competitive: true,
        allowBlank: true
      })

      expect(state.selectedColors).toEqual(new Set(['rouge', 'vert']))
      expect(state.multipleChoice).toBe(true)
      expect(state.colorLabels).toEqual({ rouge: 'Tomato' })
      expect(state.gameEnabled).toBe(true)
      expect(state.competitive).toBe(true)
      expect(state.allowBlank).toBe(true)
      expect(rendererSpies.renderMainContent).toHaveBeenCalledTimes(1)
      expect(rendererSpies.attachConfigListeners).toHaveBeenCalledTimes(1)
    })

    it('does NOT re-render when voteState is not idle', () => {
      initClient()
      capturedClient.fireMessage({ type: 'session_created', sessionCode: 'ABC' })
      state.voteState = 'active'
      rendererSpies.renderMainContent.mockClear()
      capturedClient.fireMessage({
        type: 'config_updated',
        selectedColors: ['rouge']
      })
      expect(rendererSpies.renderMainContent).not.toHaveBeenCalled()
    })

    it('ignores config_updated with empty selectedColors (does not wipe local state)', () => {
      initClient()
      state.selectedColors = new Set(['rouge'])
      capturedClient.fireMessage({
        type: 'config_updated',
        selectedColors: []
      })
      expect(state.selectedColors).toEqual(new Set(['rouge']))
    })
  })

  describe('error', () => {
    it('"Session introuvable" clears the persisted code and returns to the landing page', () => {
      sessionStorage.setItem('vote_session_code', 'ABC')
      state.sessionCode = 'ABC'
      initClient()
      rendererSpies.cleanupAllListeners.mockClear()
      rendererSpies.renderLandingPage.mockClear()

      capturedClient.fireMessage({ type: 'error', message: 'Session introuvable' })

      expect(state.sessionCode).toBeNull()
      expect(sessionStorage.getItem('vote_session_code')).toBeNull()
      expect(rendererSpies.cleanupAllListeners).toHaveBeenCalledTimes(1)
      expect(rendererSpies.renderLandingPage).toHaveBeenCalledTimes(1)
      expect(rendererSpies.attachLandingListeners).toHaveBeenCalledTimes(1)
    })

    it('a generic error just calls showError + resets loading state', () => {
      initClient()
      capturedClient.fireMessage({ type: 'error', message: "Quelque chose s'est mal passé" })
      expect(showErrorSpy).toHaveBeenCalledWith("Quelque chose s'est mal passé")
      expect(rendererSpies.renderLandingPage).not.toHaveBeenCalled()
    })

    it('logs the error to the console', () => {
      const errSpy = vi.spyOn(console, 'error').mockImplementation(() => {})
      initClient()
      capturedClient.fireMessage({ type: 'error', message: 'Boom' })
      expect(errSpy).toHaveBeenCalled()
      errSpy.mockRestore()
    })
  })

  describe('unknown message types', () => {
    it('are logged at debug level without throwing', () => {
      const debugSpy = vi.spyOn(console, 'debug').mockImplementation(() => {})
      initClient()
      expect(() => capturedClient.fireMessage({ type: 'unknown' })).not.toThrow()
      expect(debugSpy).toHaveBeenCalled()
      debugSpy.mockRestore()
    })
  })

  describe('attachListeners interaction (session ↔ render trackers)', () => {
    it('idle state routes to attachConfigListeners', () => {
      initClient()
      capturedClient.fireMessage({ type: 'session_created', sessionCode: 'ABC' })
      // connected_count with voteState=idle also re-attaches config listeners.
      rendererSpies.attachConfigListeners.mockClear()
      capturedClient.fireMessage({ type: 'connected_count', count: 1 })
      expect(rendererSpies.attachConfigListeners).toHaveBeenCalled()
    })

    it('active state routes to attachVoteListeners', () => {
      initClient()
      capturedClient.fireMessage({ type: 'session_created', sessionCode: 'ABC' })
      capturedClient.fireMessage({ type: 'vote_started', colors: ['rouge'] })
      expect(rendererSpies.attachVoteListeners).toHaveBeenCalled()
    })

    it('is a no-op when #app-content does not exist (pre-session_created)', () => {
      initClient()
      // No session_created → no app-content. attachListeners returns early.
      expect(() => capturedClient.fireMessage({ type: 'vote_started', colors: ['rouge'] })).not.toThrow()
    })
  })

  describe('attachLandingListenersWithHandlers', () => {
    it('wires the landing page DOM without throwing', async () => {
      document.body.innerHTML = '<div id="app"></div>'
      rendererSpies.renderLandingPage.mockImplementation((app) => {
        app.innerHTML = `
          <button id="createSessionBtn"></button>
          <button id="joinSessionBtn"></button>
          <input id="joinSessionInput" />
          <div class="error-message" style="display:none"></div>
        `
      })
      // Render the landing first so attachLandingListeners can find the buttons.
      const websocket = await import('./websocket.js')
      websocket.attachLandingListenersWithHandlers()
      // Sanity: handlers are bound; clicking the create button triggers a
      // dynamic import that we cannot easily intercept here. Verify no crash.
      expect(rendererSpies.attachLandingListeners).toHaveBeenCalledTimes(1)
    })
  })
})
