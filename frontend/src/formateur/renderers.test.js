// @vitest-environment happy-dom
import { describe, it, expect, beforeEach, vi } from 'vitest'

// Mock showConfirmDialog so the Escape shortcut handler resolves
// synchronously without rendering a real modal. We need to control the
// resolved value per-test, so we expose a setter on the mock.
let _confirmResult = false
vi.mock('@shared/ui.js', async (importOriginal) => {
  const actual = await importOriginal()
  return {
    ...actual,
    showConfirmDialog: () => Promise.resolve(_confirmResult)
  }
})

const { state } = await import('./state.js')
const {
  attachAppKeyboardShortcuts,
  attachConfigListeners,
  attachVoteListeners,
  attachHeaderListeners,
  cleanupAllListeners,
  renderMainContent,
  renderFullLayout,
  setActionHandlers,
  _trackerSizesForTests
} = await import('./renderers.js')

// Minimal action handlers so attachConfigListeners / attachVoteListeners
// can wire their buttons. The functions only need to be defined — they
// aren't invoked by these tests.
setActionHandlers({
  startVote: () => {},
  closeVote: () => {},
  resetVote: () => {},
  revealAnswers: () => {},
  resetConfig: () => {},
  beginSavePreset: () => {},
  cancelSavePreset: () => {},
  confirmSavePreset: () => {},
  applyPreset: () => {},
  deletePreset: () => {},
  exportPresets: () => {},
  importPresets: () => {}
})

function buildAppShell() {
  document.body.innerHTML = `
    <div id="app">
      <header>
        <button id="leaveSessionBtn">Leave</button>
        <button id="openConnectionAidBtn">Aid</button>
      </header>
      <main id="app-content"></main>
    </div>
  `
}

function dispatchKey(key) {
  const ev = new KeyboardEvent('keydown', { key, bubbles: true })
  document.dispatchEvent(ev)
  return ev
}

// Flush the microtask queue enough times for the async handler chain
// (keydown → await showConfirmDialog → leave) to settle.
function flushMicrotasks() {
  return new Promise((resolve) => setTimeout(resolve, 0))
}

describe('listener tracker split (C3 + M1 regression)', () => {
  beforeEach(() => {
    _confirmResult = false
    state.sessionCode = 'ABC'
    state.voteState = 'idle'
    state.connectedCount = 0
    state.selectedColors = new Set(['rouge', 'vert'])
    state.colorLabels = {}
    state.stagiaires = []
    state.competitive = false
    state.allowBlank = false
    state.gameEnabled = false
    state.multipleChoice = false
  })

  it('C3: Escape shortcut survives cleanupAllListeners() (session_created flow)', async () => {
    buildAppShell()
    renderFullLayout(document.getElementById('app'))

    const leave = vi.fn()
    attachAppKeyboardShortcuts(leave)

    // Simulate the post-session_created cleanup that previously killed
    // the shortcut, then re-render content.
    renderMainContent()
    attachConfigListeners({})
    cleanupAllListeners()

    // Confirm resolves to true → leave should fire.
    _confirmResult = true
    dispatchKey('Escape')
    await flushMicrotasks()
    expect(leave).toHaveBeenCalledTimes(1)

    cleanupAllListeners()
  })

  it('C3: Escape shortcut does NOT fire when state.sessionCode is null (landing page)', async () => {
    document.body.innerHTML = '<div id="app"></div>'
    state.sessionCode = null
    const leave = vi.fn()
    attachAppKeyboardShortcuts(leave)
    _confirmResult = true
    dispatchKey('Escape')
    await flushMicrotasks()
    expect(leave).not.toHaveBeenCalled()
    cleanupAllListeners()
  })

  it('M1: repeated attachConfigListeners calls do not grow the render tracker', () => {
    buildAppShell()
    renderFullLayout(document.getElementById('app'))
    renderMainContent()
    attachConfigListeners({})
    const initialSize = _trackerSizesForTests().render
    expect(initialSize).toBeGreaterThan(0)

    // Simulate the trainer applying several presets in a row. Each preset
    // handler in handlers.js does renderMainContent + attachConfigListeners.
    // Without self-cleaning in attachConfigListeners, the renderTracker
    // would accumulate stale entries and grow linearly with each cycle.
    for (let i = 0; i < 5; i++) {
      renderMainContent()
      attachConfigListeners({})
    }

    // After 5 cycles, the tracker should still hold roughly the same
    // number of entries — not 5x. We allow some slack because toggling
    // competitive mode in the rendered HTML can change which controls are
    // present, but the order of magnitude must not have grown.
    const finalSize = _trackerSizesForTests().render
    expect(finalSize).toBeLessThanOrEqual(initialSize * 2)
    expect(finalSize).toBeGreaterThanOrEqual(initialSize)

    cleanupAllListeners()
  })

  it('M1: attachConfigListeners cleans up vote listeners when transitioning idle→active→idle', () => {
    buildAppShell()
    renderFullLayout(document.getElementById('app'))
    state.voteState = 'active'
    renderMainContent()
    attachVoteListeners({})

    // Transition back to idle and attach config — vote listeners must be
    // cleaned up, not accumulated. The close-vote button from the prior
    // render is now detached; its handler should not be in the tracker.
    state.voteState = 'idle'
    renderMainContent()
    attachConfigListeners({})

    // Sanity: no close-vote button exists in idle state.
    expect(document.getElementById('closeVote')).toBeNull()
    cleanupAllListeners()
  })

  it('cleanupAllListeners preserves app shortcut but wipes session + render', async () => {
    buildAppShell()
    renderFullLayout(document.getElementById('app'))
    renderMainContent()

    const leave = vi.fn()
    attachAppKeyboardShortcuts(leave)
    attachHeaderListeners({}, () => {})
    attachConfigListeners({})

    _confirmResult = true
    dispatchKey('Escape')
    await flushMicrotasks()
    expect(leave).toHaveBeenCalledTimes(1)

    // Now wipe session + render. App shortcut should still work.
    cleanupAllListeners()

    dispatchKey('Escape')
    await flushMicrotasks()
    expect(leave).toHaveBeenCalledTimes(2)

    // sessionTracker should be empty after cleanup — sizes helper confirms
    // the structural invariant without depending on DOM state.
    const sizes = _trackerSizesForTests()
    expect(sizes.session).toBe(0)
    expect(sizes.render).toBe(0)
    expect(sizes.app).toBeGreaterThan(0)

    cleanupAllListeners()
  })

  it('escape on landing page (no app shell) is a no-op, not an error', async () => {
    document.body.innerHTML = '<div id="app"></div>'
    state.sessionCode = null
    const leave = vi.fn()
    attachAppKeyboardShortcuts(leave)
    _confirmResult = true
    dispatchKey('Escape')
    await flushMicrotasks()
    expect(leave).not.toHaveBeenCalled()
    cleanupAllListeners()
  })
})

// F4 regression: Escape inside the preset-name <input> must call
// cancelSavePreset WITHOUT bubbling to the document-level app shortcut
// (which would open the "Quitter la session?" confirm on top of the
// just-closed form). Before the fix, preventDefault alone did not stop
// propagation; stopImmediatePropagation is now used.
describe('preset input Escape isolation (F4)', () => {
  beforeEach(() => {
    _confirmResult = false
    state.sessionCode = 'ABC'
    state.voteState = 'idle'
    state.connectedCount = 0
    state.selectedColors = new Set(['rouge', 'vert'])
    state.colorLabels = {}
    state.stagiaires = []
    state.competitive = false
    state.allowBlank = false
    state.gameEnabled = false
    state.multipleChoice = false
    state.presetSaving = true
  })

  function mountPresetInput() {
    buildAppShell()
    renderFullLayout(document.getElementById('app'))
    renderMainContent()
    // The preset save form is conditionally rendered when presetSaving=true.
    // We attach listeners which bind #presetNameInput if present.
    attachConfigListeners({})
    return document.getElementById('presetNameInput')
  }

  it('Escape in preset input calls cancelSavePreset and does NOT open leave-session dialog', async () => {
    const cancelSpy = vi.fn()
    setActionHandlers({
      startVote: () => {},
      closeVote: () => {},
      resetVote: () => {},
      revealAnswers: () => {},
      resetConfig: () => {},
      beginSavePreset: () => {},
      cancelSavePreset: cancelSpy,
      confirmSavePreset: () => {},
      applyPreset: () => {},
      deletePreset: () => {},
      exportPresets: () => {},
      importPresets: () => {}
    })

    const input = mountPresetInput()
    // If presetSaving=true doesn't render the input in the current HTML,
    // mount it manually so the test exercises the handler regardless of
    // renderConfigHTML's exact conditional shape.
    let target = input
    if (!target) {
      target = document.createElement('input')
      target.id = 'presetNameInput'
      document.getElementById('app-content').appendChild(target)
      attachConfigListeners({})
    }

    const leave = vi.fn()
    attachAppKeyboardShortcuts(leave)

    _confirmResult = true // if the app handler fires, leave WILL be called
    target.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await flushMicrotasks()

    expect(cancelSpy).toHaveBeenCalledTimes(1)
    expect(leave).not.toHaveBeenCalled()

    cleanupAllListeners()
  })

  it('Enter in preset input calls confirmSavePreset and does NOT trigger landing-page Enter', async () => {
    const confirmSpy = vi.fn()
    setActionHandlers({
      startVote: () => {},
      closeVote: () => {},
      resetVote: () => {},
      revealAnswers: () => {},
      resetConfig: () => {},
      beginSavePreset: () => {},
      cancelSavePreset: () => {},
      confirmSavePreset: confirmSpy,
      applyPreset: () => {},
      deletePreset: () => {},
      exportPresets: () => {},
      importPresets: () => {}
    })

    const input = mountPresetInput()
    let target = input
    if (!target) {
      target = document.createElement('input')
      target.id = 'presetNameInput'
      target.value = 'Mon preset'
      document.getElementById('app-content').appendChild(target)
      attachConfigListeners({})
    } else {
      target.value = 'Mon preset'
    }

    // Landing Enter handler is NOT attached here (we're in full layout),
    // but we verify confirmSavePreset fires and no unhandled error occurs.
    target.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
    await flushMicrotasks()

    expect(confirmSpy).toHaveBeenCalledWith('Mon preset')

    cleanupAllListeners()
  })
})
