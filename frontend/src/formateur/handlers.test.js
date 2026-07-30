// @vitest-environment happy-dom
import { describe, it, expect, beforeEach, vi } from 'vitest'

// =====================================================================
// Shared mocks — handlers.js imports getClient from ./websocket.js and
// showError from @shared/ui.js. We control getClient's return (and its
// send() return value) per-test to exercise the F24 send-failure path.
// =====================================================================

const showErrorSpy = vi.fn()
const showToastSpy = vi.fn()
vi.mock('@shared/ui.js', () => ({
  showError: showErrorSpy,
  showToast: showToastSpy,
  renderFooterHTML: () => '',
  renderSessionCodeButton: (code) => `<button id="leaveSessionBtn">${code || ''}</button>`,
  showConfirmDialog: vi.fn()
}))

vi.mock('@shared/presets.js', () => ({
  listPresets: () => [],
  savePreset: () => true,
  deletePreset: () => {},
  getLastConfig: () => null,
  setLastConfig: vi.fn(),
  serializePresets: () => '{}',
  deserializePresets: () => ({ ok: false })
}))

vi.mock('./renderers.js', () => ({
  renderMainContent: vi.fn(),
  attachConfigListeners: vi.fn()
}))

// getClient mock — reassigned per-test.
const mockGetClient = vi.fn()
vi.mock('./websocket.js', () => ({
  getClient: (...args) => mockGetClient(...args)
}))

const { state } = await import('./state.js')
const { startVote, closeVote, revealAnswers, resetVote } = await import('./handlers.js')

function makeClient(sendReturns = true) {
  return {
    send: vi.fn(() => sendReturns),
    isConnected: () => sendReturns
  }
}

describe('formateur action handlers — F24 send-failure surfacing', () => {
  beforeEach(() => {
    showErrorSpy.mockClear()
    showToastSpy.mockClear()
    mockGetClient.mockReset()
    state.sessionCode = 'ABC'
    state.selectedColors = new Set(['rouge', 'vert'])
    state.colorLabels = {}
    state.multipleChoice = false
    state.gameEnabled = false
    state.competitive = false
    state.allowBlank = false
    state.connected = true
  })

  describe('startVote', () => {
    it('sends start_vote and does NOT show an error when send succeeds', () => {
      const client = makeClient(true)
      mockGetClient.mockReturnValue(client)
      startVote(client)
      expect(client.send).toHaveBeenCalledTimes(1)
      expect(client.send.mock.calls[0][0]).toMatchObject({ type: 'start_vote', sessionCode: 'ABC' })
      expect(showErrorSpy).not.toHaveBeenCalled()
    })

    it('shows an error when client.send returns false (socket down)', () => {
      const client = makeClient(false)
      startVote(client)
      expect(client.send).toHaveBeenCalledTimes(1)
      expect(showErrorSpy).toHaveBeenCalledTimes(1)
      expect(showErrorSpy).toHaveBeenCalledWith('Erreur de connexion')
    })

    it('shows an error when no client is provided', () => {
      startVote(null)
      expect(showErrorSpy).toHaveBeenCalledWith('Erreur de connexion')
    })
  })

  describe('closeVote', () => {
    it('sends close_vote on success', () => {
      const client = makeClient(true)
      closeVote(client)
      expect(client.send).toHaveBeenCalledTimes(1)
      expect(client.send.mock.calls[0][0]).toMatchObject({ type: 'close_vote', sessionCode: 'ABC' })
      expect(showErrorSpy).not.toHaveBeenCalled()
    })

    it('surfaces send failure', () => {
      const client = makeClient(false)
      closeVote(client)
      expect(showErrorSpy).toHaveBeenCalledWith('Erreur de connexion')
    })

    it('surfaces missing client', () => {
      closeVote(null)
      expect(showErrorSpy).toHaveBeenCalledWith('Erreur de connexion')
    })
  })

  describe('revealAnswers', () => {
    it('sends reveal_answers with correctColors on success', () => {
      const client = makeClient(true)
      revealAnswers(client, new Set(['rouge']))
      expect(client.send).toHaveBeenCalledTimes(1)
      expect(client.send.mock.calls[0][0]).toMatchObject({
        type: 'reveal_answers',
        sessionCode: 'ABC',
        correctColors: ['rouge']
      })
      expect(showErrorSpy).not.toHaveBeenCalled()
    })

    it('surfaces send failure', () => {
      const client = makeClient(false)
      revealAnswers(client, new Set(['rouge']))
      expect(showErrorSpy).toHaveBeenCalledWith('Erreur de connexion')
    })

    it('surfaces missing client', () => {
      revealAnswers(null, new Set())
      expect(showErrorSpy).toHaveBeenCalledWith('Erreur de connexion')
    })
  })

  describe('resetVote', () => {
    it('sends reset_vote on success', () => {
      const client = makeClient(true)
      resetVote(client)
      expect(client.send).toHaveBeenCalledTimes(1)
      expect(client.send.mock.calls[0][0]).toMatchObject({ type: 'reset_vote', sessionCode: 'ABC' })
      expect(showErrorSpy).not.toHaveBeenCalled()
    })

    it('surfaces send failure', () => {
      const client = makeClient(false)
      resetVote(client)
      expect(showErrorSpy).toHaveBeenCalledWith('Erreur de connexion')
    })

    it('surfaces missing client', () => {
      resetVote(null)
      expect(showErrorSpy).toHaveBeenCalledWith('Erreur de connexion')
    })
  })
})
