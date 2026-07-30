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

const listPresetsMock = vi.fn(() => [])
const savePresetMock = vi.fn(() => true)
const getLastConfigMock = vi.fn(() => null)
const setLastConfigMock = vi.fn()
vi.mock('@shared/presets.js', () => ({
  listPresets: (...args) => listPresetsMock(...args),
  savePreset: (...args) => savePresetMock(...args),
  deletePreset: () => {},
  getLastConfig: (...args) => getLastConfigMock(...args),
  setLastConfig: (...args) => setLastConfigMock(...args),
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
const {
  startVote,
  closeVote,
  revealAnswers,
  resetVote,
  confirmSavePreset,
  applyPreset,
  applyLastConfigIfAvailable
} = await import('./handlers.js')

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
    listPresetsMock.mockReset()
    listPresetsMock.mockReturnValue([])
    savePresetMock.mockReset()
    savePresetMock.mockReturnValue(true)
    getLastConfigMock.mockReset()
    getLastConfigMock.mockReturnValue(null)
    setLastConfigMock.mockClear()
    state.sessionCode = 'ABC'
    state.selectedColors = new Set(['rouge', 'vert'])
    state.colorLabels = {}
    state.multipleChoice = false
    state.gameEnabled = false
    state.competitive = false
    state.allowBlank = false
    state.correctColors = new Set()
    state.lastConfigApplied = false
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

// F27: the answer key (correctColors) must round-trip through both
// persistence layers — named presets and the last-config autoload. Before
// the fix the v3 schema validated correctColors on read but no site ever
// wrote or restored it, so a saved preset lost its answer key.
describe('correctColors preset round-trip (F27)', () => {
  beforeEach(() => {
    showErrorSpy.mockClear()
    showToastSpy.mockClear()
    mockGetClient.mockReset()
    listPresetsMock.mockReset()
    listPresetsMock.mockReturnValue([])
    savePresetMock.mockReset()
    savePresetMock.mockReturnValue(true)
    getLastConfigMock.mockReset()
    getLastConfigMock.mockReturnValue(null)
    setLastConfigMock.mockClear()
    state.sessionCode = 'ABC'
    state.selectedColors = new Set(['rouge', 'vert'])
    state.colorLabels = {}
    state.multipleChoice = false
    state.gameEnabled = false
    state.competitive = false
    state.allowBlank = false
    state.correctColors = new Set()
    state.lastConfigApplied = false
    state.connected = true
  })

  it('confirmSavePreset carries correctColors into savePreset', () => {
    state.correctColors = new Set(['rouge', 'blank'])
    savePresetMock.mockReturnValue({ id: 'p1', name: 'Sondage' })
    confirmSavePreset('Sondage')
    expect(savePresetMock).toHaveBeenCalledTimes(1)
    const config = savePresetMock.mock.calls[0][1]
    expect(config.correctColors).toEqual(['rouge', 'blank'])
  })

  it('startVote persists correctColors into setLastConfig', () => {
    const client = makeClient(true)
    mockGetClient.mockReturnValue(client)
    state.correctColors = new Set(['vert'])
    startVote(client)
    expect(setLastConfigMock).toHaveBeenCalledTimes(1)
    const config = setLastConfigMock.mock.calls[0][0]
    expect(config.correctColors).toEqual(['vert'])
  })

  it('applyPreset restores state.correctColors from the preset config', () => {
    listPresetsMock.mockReturnValue([
      {
        id: 'p1',
        name: 'Sondage',
        config: {
          selectedColors: ['rouge', 'bleu'],
          colorLabels: {},
          multipleChoice: true,
          gameEnabled: false,
          competitive: true,
          allowBlank: true,
          correctColors: ['rouge', 'blank']
        }
      }
    ])
    applyPreset('p1')
    expect(state.correctColors).toEqual(new Set(['rouge', 'blank']))
    expect(state.selectedColors).toEqual(new Set(['rouge', 'bleu']))
  })

  it('applyPreset tolerates a preset lacking correctColors (empty set)', () => {
    listPresetsMock.mockReturnValue([
      {
        id: 'p1',
        name: 'Old',
        config: { selectedColors: ['rouge'], colorLabels: {}, multipleChoice: false }
      }
    ])
    state.correctColors = new Set(['vert'])
    applyPreset('p1')
    expect(state.correctColors).toEqual(new Set())
  })

  it('applyLastConfigIfAvailable restores state.correctColors', () => {
    getLastConfigMock.mockReturnValue({
      selectedColors: ['rouge'],
      colorLabels: {},
      multipleChoice: false,
      gameEnabled: false,
      competitive: true,
      allowBlank: true,
      correctColors: ['rouge', 'blank']
    })
    applyLastConfigIfAvailable()
    expect(state.correctColors).toEqual(new Set(['rouge', 'blank']))
    // Idempotency guard: a second call is a no-op.
    state.correctColors = new Set()
    applyLastConfigIfAvailable()
    expect(state.correctColors).toEqual(new Set())
  })
})
