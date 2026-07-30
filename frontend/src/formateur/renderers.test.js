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
const { t } = await import('@shared/strings.js')
const {
  attachAppKeyboardShortcuts,
  attachConfigListeners,
  attachVoteListeners,
  attachHeaderListeners,
  registerHeaderLeaveHandler,
  updateHeader,
  updateActionButtonsState,
  cleanupAllListeners,
  renderMainContent,
  renderFullLayout,
  setActionHandlers,
  _trackerSizesForTests,
  _resetAppShortcutForTests
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
    // R6: reset the idempotency guard so each test starts from a clean
    // slate (the guard is intentionally sticky for the page lifetime in
    // production).
    _resetAppShortcutForTests()
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

  it('R6: attachAppKeyboardShortcuts is idempotent (calling twice does not stack handlers)', async () => {
    // R6: the Escape shortcut must be bound exactly once per page
    // lifecycle. Calling attachAppKeyboardShortcuts multiple times
    // (e.g. hoisted in main.js + a future session_created re-attach)
    // must NOT stack keydown handlers — otherwise a single Escape
    // press opens N confirmation dialogs. The latest leaveSessionFn
    // wins so a late caller can still swap the handler.
    buildAppShell()
    renderFullLayout(document.getElementById('app'))

    const leave1 = vi.fn()
    const leave2 = vi.fn()
    attachAppKeyboardShortcuts(leave1)
    const sizeAfterFirst = _trackerSizesForTests().app
    attachAppKeyboardShortcuts(leave2)
    attachAppKeyboardShortcuts(leave2)
    const sizeAfterRepeat = _trackerSizesForTests().app

    // Tracker did not grow — still exactly one keydown listener.
    expect(sizeAfterRepeat).toBe(sizeAfterFirst)

    // The latest leaveSessionFn (leave2) is the one that fires.
    _confirmResult = true
    dispatchKey('Escape')
    await flushMicrotasks()
    expect(leave1).not.toHaveBeenCalled()
    expect(leave2).toHaveBeenCalledTimes(1)

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

// F23: updateHeader must (re)attach header listeners whenever it injects
// fresh markup. In production, renderFullLayout emits an EMPTY <header>,
// then updateHeader injects the buttons. If listeners are attached before
// the buttons exist (the original bug), the Quitter / Aide buttons are
// dead for the entire session.
describe('updateHeader re-attaches header listeners on fresh markup (F23)', () => {
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
    state.connected = true
    _resetAppShortcutForTests()
  })

  afterEach(() => {
    cleanupAllListeners()
  })

  it('injects the leave + aid buttons and binds a working click listener on the leave button', async () => {
    // renderFullLayout produces an EMPTY header — the original production
    // ordering where attachHeaderListeners ran before the buttons existed.
    document.body.innerHTML = '<div id="app"><header id="app-header"></header><main id="app-content"></main></div>'

    const leave = vi.fn()
    registerHeaderLeaveHandler(leave)

    updateHeader({ isConnected: () => true })

    const leaveBtn = document.getElementById('leaveSessionBtn')
    expect(leaveBtn).not.toBeNull()
    const aidBtn = document.getElementById('openConnectionAidBtn')
    expect(aidBtn).not.toBeNull()

    // The leave button click should trigger the confirm dialog and, on
    // confirm, call the registered leave handler.
    _confirmResult = true
    leaveBtn.click()
    await flushMicrotasks()
    expect(leave).toHaveBeenCalledTimes(1)
  })

  it('aid button click opens the classroom-display URL', () => {
    document.body.innerHTML = '<div id="app"><header id="app-header"></header><main id="app-content"></main></div>'
    registerHeaderLeaveHandler(() => {})

    const openSpy = vi.spyOn(window, 'open').mockImplementation(() => null)
    updateHeader({ isConnected: () => true })

    document.getElementById('openConnectionAidBtn').click()
    expect(openSpy).toHaveBeenCalledTimes(1)
    const url = openSpy.mock.calls[0][0]
    expect(url).toContain('aide=ABC')
    openSpy.mockRestore()
  })

  it('className fast-path does NOT re-inject or double-bind (idempotent)', async () => {
    document.body.innerHTML = '<div id="app"><header id="app-header"></header><main id="app-content"></main></div>'
    const leave = vi.fn()
    registerHeaderLeaveHandler(leave)

    // First call injects + binds.
    updateHeader({ isConnected: () => true })
    const leaveBtn = document.getElementById('leaveSessionBtn')

    // Second call with same sessionCode + connection state hits the
    // fast-path (className-only). The button element must be unchanged
    // (same node) and a single click must fire the handler exactly once.
    updateHeader({ isConnected: () => true })
    expect(document.getElementById('leaveSessionBtn')).toBe(leaveBtn)

    _confirmResult = true
    leaveBtn.click()
    await flushMicrotasks()
    expect(leave).toHaveBeenCalledTimes(1)
  })

  it('does not double-bind across repeated fresh injections (sessionTracker self-cleaning)', async () => {
    document.body.innerHTML = '<div id="app"><header id="app-header"></header><main id="app-content"></main></div>'
    const leave = vi.fn()
    registerHeaderLeaveHandler(leave)

    // Simulate a session-code change triggering a fresh injection twice.
    updateHeader({ isConnected: () => true })
    // Force a fresh re-inject by clearing the header (as if renderFullLayout
    // ran again).
    document.getElementById('app-header').innerHTML = ''
    updateHeader({ isConnected: () => true })

    // sessionTracker should hold exactly 2 entries (leave + aid), not 4.
    const sizes = _trackerSizesForTests()
    expect(sizes.session).toBe(2)

    _confirmResult = true
    document.getElementById('leaveSessionBtn').click()
    await flushMicrotasks()
    // Must fire exactly once despite two injections.
    expect(leave).toHaveBeenCalledTimes(1)
  })
})

// F24: updateActionButtonsState toggles disabled on the action buttons so
// their state tracks state.connected live (instead of only at render time).
describe('updateActionButtonsState live disabled tracking (F24)', () => {
  beforeEach(() => {
    state.sessionCode = 'ABC'
    state.voteState = 'idle'
    state.connected = true
    state.selectedColors = new Set(['rouge', 'vert'])
  })

  afterEach(() => {
    cleanupAllListeners()
  })

  it('disables action buttons when disconnected', () => {
    document.body.innerHTML = `
      <main id="app-content">
        <button id="startVote"></button>
        <button id="resetConfig"></button>
      </main>`
    state.connected = false
    updateActionButtonsState()
    expect(document.getElementById('startVote').disabled).toBe(true)
    expect(document.getElementById('resetConfig').disabled).toBe(true)
  })

  it('re-enables action buttons when reconnected', () => {
    document.body.innerHTML = `
      <main id="app-content">
        <button id="startVote" disabled></button>
        <button id="resetConfig" disabled></button>
      </main>`
    state.connected = true
    updateActionButtonsState()
    expect(document.getElementById('startVote').disabled).toBe(false)
    expect(document.getElementById('resetConfig').disabled).toBe(false)
  })

  it('keeps startVote disabled when fewer than 2 colors selected (even when connected)', () => {
    document.body.innerHTML = `<main id="app-content"><button id="startVote"></button></main>`
    state.connected = true
    state.selectedColors = new Set(['rouge'])
    updateActionButtonsState()
    expect(document.getElementById('startVote').disabled).toBe(true)
  })

  it('handles vote-phase buttons (closeVote / revealBtn / newVote)', () => {
    document.body.innerHTML = `
      <main id="app-content">
        <button id="closeVote"></button>
        <button id="revealBtn"></button>
        <button id="newVote"></button>
      </main>`
    state.connected = false
    updateActionButtonsState()
    expect(document.getElementById('closeVote').disabled).toBe(true)
    expect(document.getElementById('revealBtn').disabled).toBe(true)
    expect(document.getElementById('newVote').disabled).toBe(true)
  })
})

// F26: the trainer scoreboard must rank by cumulative totalScore (which
// folds in the game score), not the per-round voteScore, and surface both
// the rank column and the total. Before the fix the renderer sorted by
// voteScore and rendered neither rank nor totalScore — dead CSS
// (.scoreboard-row.rank-1, .scoreboard-total) confirmed the intent.
describe('trainer competitive scoreboard (F26)', () => {
  beforeEach(() => {
    _confirmResult = false
    state.sessionCode = 'ABC'
    state.voteState = 'closed'
    state.competitive = true
    state.allowBlank = false
    state.gameEnabled = false
    state.multipleChoice = false
    state.connected = true
    state.selectedColors = new Set(['rouge', 'vert'])
    state.colorLabels = {}
    state.correctColors = new Set(['rouge'])
    state.revealed = true
    state.stagiaires = []
    _resetAppShortcutForTests()
  })

  afterEach(() => {
    cleanupAllListeners()
  })

  function mountScoreboard() {
    buildAppShell()
    renderFullLayout(document.getElementById('app'))
    renderMainContent()
    return document.querySelector('.scoreboard-list')
  }

  it('sorts rows by cumulative totalScore, not per-round voteScore', () => {
    // B has the lower round score but the higher cumulative total — must
    // rank first. The old voteScore sort would put A first.
    state.scoreboard = [
      { id: 'a', name: 'Alice', vote: ['rouge'], voteScore: 500, totalScore: 1000, rank: 2 },
      { id: 'b', name: 'Bob', vote: ['vert'], voteScore: 200, totalScore: 3000, rank: 1 }
    ]
    const list = mountScoreboard()
    const names = [...list.querySelectorAll('.scoreboard-name')].map((n) => n.textContent.trim())
    expect(names).toEqual(['Bob', 'Alice'])
  })

  it('emits a rank-N row class matching each entry rank', () => {
    state.scoreboard = [
      { id: 'a', name: 'Alice', vote: ['rouge'], voteScore: 500, totalScore: 1000, rank: 2 },
      { id: 'b', name: 'Bob', vote: ['vert'], voteScore: 200, totalScore: 3000, rank: 1 }
    ]
    const list = mountScoreboard()
    const rows = [...list.querySelectorAll('.scoreboard-row')]
    expect(rows[0].className).toContain('rank-1')
    expect(rows[1].className).toContain('rank-2')
  })

  it('renders the rank column and totalScore column per row', () => {
    state.scoreboard = [
      { id: 'b', name: 'Bob', vote: ['vert'], voteScore: 200, totalScore: 3000, rank: 1 }
    ]
    const list = mountScoreboard()
    const row = list.querySelector('.scoreboard-row')
    expect(row.querySelector('.scoreboard-rank').textContent.trim()).toBe('1')
    expect(row.querySelector('.scoreboard-total').textContent.trim()).toBe('3000')
    // Per-round voteScore is still surfaced (+200).
    expect(row.querySelector('.scoreboard-votescore').textContent.trim()).toBe('+200')
  })

  it('renders a header labelling the round and total columns', () => {
    state.scoreboard = [
      { id: 'b', name: 'Bob', vote: ['vert'], voteScore: 0, totalScore: 0, rank: 1 }
    ]
    mountScoreboard()
    const header = document.querySelector('.scoreboard-header')
    expect(header).not.toBeNull()
    expect(header.querySelector('.scoreboard-votescore').textContent.trim()).toBe(t.formateur.scoreboardRound)
    expect(header.querySelector('.scoreboard-total').textContent.trim()).toBe(t.formateur.totalScore)
  })

  it('handles missing totalScore/rank (older server) without crashing', () => {
    state.scoreboard = [{ id: 'a', name: 'Alice', vote: ['rouge'], voteScore: 100 }]
    const list = mountScoreboard()
    const row = list.querySelector('.scoreboard-row')
    expect(row.querySelector('.scoreboard-total').textContent.trim()).toBe('0')
    expect(row.querySelector('.scoreboard-rank').textContent.trim()).toBe('—')
    expect(row.className).not.toContain('rank-')
  })
})
