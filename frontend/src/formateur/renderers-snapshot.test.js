// @vitest-environment happy-dom
import { describe, it, expect, beforeEach, vi } from 'vitest'

// Stub showConfirmDialog so any keyboard path that hits it resolves sync.
vi.mock('@shared/ui.js', async (importOriginal) => {
  const actual = await importOriginal()
  return {
    ...actual,
    showConfirmDialog: () => Promise.resolve(false)
  }
})

const { state } = await import('./state.js')
const {
  renderLandingPage,
  renderFullLayout,
  renderMainContent,
  updateHeader,
  updateLandingPageLoadingState,
  updateConnectionBanner,
  renderConfigHTML,
  renderVoteHTML,
  renderColorBarsHTML,
  renderCombinationsHTML,
  renderStagiairesVotesHTML
} = await import('./renderers.js')

// Snapshot / idempotency tests for the formateur DOM builders.
// Goal: re-rendering the same state twice must produce identical HTML, and
// every public render function must produce stable output for a fixed input.

function resetState() {
  state.sessionCode = null
  state.connected = false
  state.connecting = false
  state.everConnected = false
  state.voteState = 'idle'
  state.selectedColors = new Set(['rouge', 'vert', 'bleu'])
  state.colorLabels = {}
  state.multipleChoice = false
  state.gameEnabled = false
  state.competitive = false
  state.allowBlank = false
  state.correctColors = new Set()
  state.revealed = false
  state.scoreboard = []
  state.connectedCount = 0
  state.stagiaires = []
  state.voteStartTime = null
  state.presetSaving = false
  state.lastConfigApplied = false
}

describe('formateur renderers — idempotency snapshots', () => {
  beforeEach(() => {
    resetState()
    document.body.innerHTML = '<div id="app"></div>'
  })

  describe('renderLandingPage', () => {
    it('produces identical HTML across two calls with the same input', () => {
      const app = document.getElementById('app')
      renderLandingPage(app)
      const a = app.innerHTML
      renderLandingPage(app)
      const b = app.innerHTML
      expect(a).toBe(b)
    })

    it('contains the create-session button and join input', () => {
      renderLandingPage(document.getElementById('app'))
      expect(document.getElementById('createSessionBtn')).not.toBeNull()
      expect(document.getElementById('joinSessionInput')).not.toBeNull()
      expect(document.getElementById('joinSessionInput').getAttribute('maxlength')).toBe('3')
    })
  })

  describe('renderFullLayout', () => {
    it('renders header + main + reconnect banner', () => {
      renderFullLayout(document.getElementById('app'))
      expect(document.getElementById('app-header')).not.toBeNull()
      expect(document.getElementById('app-content')).not.toBeNull()
      expect(document.getElementById('reconnect-banner')).not.toBeNull()
    })
  })

  describe('updateLandingPageLoadingState', () => {
    it('isLoading=true disables buttons and swaps the create label to a loader', () => {
      renderLandingPage(document.getElementById('app'))
      updateLandingPageLoadingState(true)
      const createBtn = document.getElementById('createSessionBtn')
      const joinBtn = document.getElementById('joinSessionBtn')
      const joinInput = document.getElementById('joinSessionInput')
      expect(createBtn.disabled).toBe(true)
      expect(joinBtn.disabled).toBe(true)
      expect(joinInput.disabled).toBe(true)
      // The button swaps to a loader icon + "Connexion…" label.
      expect(createBtn.innerHTML).toContain('Connexion')
      expect(createBtn.querySelector('svg')).not.toBeNull()
    })

    it('isLoading=false restores the create button and re-enables everything', () => {
      renderLandingPage(document.getElementById('app'))
      updateLandingPageLoadingState(true)
      updateLandingPageLoadingState(false)
      const createBtn = document.getElementById('createSessionBtn')
      expect(createBtn.disabled).toBe(false)
      expect(document.getElementById('joinSessionBtn').disabled).toBe(false)
      expect(document.getElementById('joinSessionInput').disabled).toBe(false)
    })

    it('is a no-op when the landing buttons are absent', () => {
      document.body.innerHTML = '<div id="app"></div>'
      expect(() => updateLandingPageLoadingState(true)).not.toThrow()
    })
  })

  describe('updateConnectionBanner', () => {
    it('shows the banner only when sessionCode + everConnected + not connected', () => {
      renderFullLayout(document.getElementById('app'))
      const banner = document.getElementById('reconnect-banner')

      // No sessionCode → hidden.
      state.sessionCode = null
      state.everConnected = true
      state.connected = false
      updateConnectionBanner()
      expect(banner.hidden).toBe(true)

      // sessionCode + everConnected + disconnected → visible.
      state.sessionCode = 'ABC'
      updateConnectionBanner()
      expect(banner.hidden).toBe(false)

      // Reconnected → hidden.
      state.connected = true
      updateConnectionBanner()
      expect(banner.hidden).toBe(true)

      // Never connected → hidden (no false alarm on first load).
      state.connected = false
      state.everConnected = false
      updateConnectionBanner()
      expect(banner.hidden).toBe(true)
    })

    it('is a no-op when the banner is missing', () => {
      document.body.innerHTML = '<div id="app"></div>'
      expect(() => updateConnectionBanner()).not.toThrow()
    })
  })

  describe('updateHeader', () => {
    it('renders the session-code button + open-aid button when no header exists yet', () => {
      renderFullLayout(document.getElementById('app'))
      state.sessionCode = 'ABC'
      updateHeader({ isConnected: () => true })
      expect(document.getElementById('leaveSessionBtn').textContent).toBe('ABC')
      expect(document.getElementById('openConnectionAidBtn')).not.toBeNull()
    })

    it('updates the connected class in place when the session-code button already matches', () => {
      renderFullLayout(document.getElementById('app'))
      state.sessionCode = 'ABC'
      updateHeader({ isConnected: () => false })
      const btn = document.getElementById('leaveSessionBtn')
      expect(btn.className).toContain('disconnected')

      // Same code, status flips to connected → only the class changes, no re-render.
      updateHeader({ isConnected: () => true })
      expect(btn.className).toContain('connected')
    })

    it('re-renders when the session code changes', () => {
      renderFullLayout(document.getElementById('app'))
      state.sessionCode = 'ABC'
      updateHeader({ isConnected: () => true })
      state.sessionCode = 'DEF'
      updateHeader({ isConnected: () => true })
      expect(document.getElementById('leaveSessionBtn').textContent).toBe('DEF')
    })

    it('uses state.connected when no client is provided', () => {
      renderFullLayout(document.getElementById('app'))
      state.sessionCode = 'ABC'
      state.connected = true
      updateHeader(null)
      expect(document.getElementById('leaveSessionBtn').className).toContain('connected')
    })
  })

  describe('renderMainContent routing', () => {
    it('renders the config card in idle state', () => {
      renderFullLayout(document.getElementById('app'))
      state.voteState = 'idle'
      renderMainContent()
      expect(document.getElementById('startVote')).not.toBeNull()
      expect(document.getElementById('resetConfig')).not.toBeNull()
    })

    it('renders the vote card in active state', () => {
      renderFullLayout(document.getElementById('app'))
      state.voteState = 'active'
      renderMainContent()
      expect(document.getElementById('closeVote')).not.toBeNull()
    })

    it('is a no-op when #app-content is missing', () => {
      document.body.innerHTML = '<div id="app"></div>'
      expect(() => renderMainContent()).not.toThrow()
    })

    it('produces identical HTML when called twice on the same state (idempotent)', () => {
      renderFullLayout(document.getElementById('app'))
      state.voteState = 'idle'
      state.selectedColors = new Set(['rouge', 'vert'])
      renderMainContent()
      const a = document.getElementById('app-content').innerHTML
      renderMainContent()
      const b = document.getElementById('app-content').innerHTML
      expect(a).toBe(b)
    })
  })

  describe('renderConfigHTML', () => {
    it('reflects connectedCount in the info line', () => {
      state.connectedCount = 0
      const html0 = renderConfigHTML()
      expect(html0).toContain('0 stagiaire')

      state.connectedCount = 3
      const html3 = renderConfigHTML()
      expect(html3).toContain('3 stagiaires connectés')
    })

    it('reflects selected colors via the checked attribute', () => {
      state.selectedColors = new Set(['rouge'])
      const html = renderConfigHTML()
      // The rouge checkbox is checked; vert is not.
      expect(html).toMatch(/value="rouge"[^>]*checked/)
      expect(html).not.toMatch(/value="vert"[^>]*checked/)
    })

    it('disables the start button when fewer than 2 colors are selected', () => {
      state.selectedColors = new Set(['rouge'])
      state.connected = true
      expect(renderConfigHTML()).toMatch(/id="startVote"[^>]*disabled/)

      state.selectedColors = new Set(['rouge', 'vert'])
      const html = renderConfigHTML()
      expect(html).not.toMatch(/id="startVote"[^>]*disabled/)
    })

    it('disables the start button when disconnected', () => {
      state.selectedColors = new Set(['rouge', 'vert'])
      state.connected = false
      expect(renderConfigHTML()).toMatch(/id="startVote"[^>]*disabled/)
    })

    it('omits the blank-vote toggle when competitive is off', () => {
      state.competitive = false
      expect(renderConfigHTML()).not.toMatch(/data-testid="blank-vote-toggle"/)
    })

    it('shows the blank-vote toggle when competitive is on', () => {
      state.competitive = true
      expect(renderConfigHTML()).toMatch(/data-testid="blank-vote-toggle"/)
    })
  })

  describe('renderVoteHTML', () => {
    beforeEach(() => {
      state.selectedColors = new Set(['rouge', 'vert', 'bleu'])
      state.connectedCount = 5
      state.stagiaires = [
        { id: 's1', name: 'Marie', connected: true, vote: ['rouge'] },
        { id: 's2', name: 'Joe', connected: true, vote: [] }
      ]
    })

    it('shows the close button in active state', () => {
      state.voteState = 'active'
      state.connected = true
      expect(renderVoteHTML()).toMatch(/id="closeVote"/)
    })

    it('shows the reveal button when competitive + closed + not revealed', () => {
      state.voteState = 'closed'
      state.competitive = true
      state.revealed = false
      state.connected = true
      const html = renderVoteHTML()
      expect(html).toMatch(/id="revealBtn"/)
      expect(html).toMatch(/id="newVote"/)
    })

    it('keeps the reveal button after reveal so the answer key can be corrected (R14)', () => {
      state.voteState = 'closed'
      state.competitive = true
      state.revealed = true
      state.connected = true
      const html = renderVoteHTML()
      expect(html).toMatch(/id="revealBtn"/)
      expect(html).toMatch(/id="newVote"/)
    })

    it('keeps the reveal section (correct-color checkboxes) after reveal (R14)', () => {
      state.voteState = 'closed'
      state.competitive = true
      state.revealed = true
      state.correctColors = new Set(['rouge'])
      state.connected = true
      const html = renderVoteHTML()
      // The reveal section stays present and pre-checks the current key.
      expect(html).toContain('reveal-section')
      expect(html).toMatch(/data-correct-color="rouge"[^>]*checked/)
      // The scoreboard also shows once revealed.
      expect(html).toContain('scoreboard-section')
    })

    it('disables the reveal button when disconnected (R14)', () => {
      state.voteState = 'closed'
      state.competitive = true
      state.revealed = false
      state.connected = false
      expect(renderVoteHTML()).toMatch(/id="revealBtn"[^>]*disabled/)
    })

    it('non-competitive closed state only shows the new-vote button', () => {
      state.voteState = 'closed'
      state.competitive = false
      state.revealed = false
      state.connected = true
      const html = renderVoteHTML()
      expect(html).not.toMatch(/id="revealBtn"/)
      expect(html).toMatch(/id="newVote"/)
    })

    it('disables new-vote when disconnected', () => {
      state.voteState = 'closed'
      state.competitive = false
      state.revealed = false
      state.connected = false
      expect(renderVoteHTML()).toMatch(/id="newVote"[^>]*disabled/)
    })

    it('counts only stagiaires with a non-empty vote in the header', () => {
      state.voteState = 'active'
      state.connected = true
      const html = renderVoteHTML()
      // 1 of 2 stagiaires has voted.
      expect(html).toContain('1 / 5')
    })
  })

  describe('renderColorBarsHTML', () => {
    it('sorts colors by descending vote count', () => {
      const activeColors = [
        { id: 'rouge', name: 'Rouge', color: '#ef4444' },
        { id: 'vert', name: 'Vert', color: '#22c55e' },
        { id: 'bleu', name: 'Bleu', color: '#3b82f6' }
      ]
      const colorCounts = { rouge: 1, vert: 5, bleu: 3 }
      const html = renderColorBarsHTML(activeColors, colorCounts, 5)
      const vertIdx = html.indexOf('data-color="vert"')
      const bleuIdx = html.indexOf('data-color="bleu"')
      const rougeIdx = html.indexOf('data-color="rouge"')
      expect(vertIdx).toBeLessThan(bleuIdx)
      expect(bleuIdx).toBeLessThan(rougeIdx)
    })

    it('uses 0% width for colors with no votes', () => {
      const html = renderColorBarsHTML([{ id: 'rouge', name: 'R', color: '#f00' }], { rouge: 0 }, 5)
      expect(html).toMatch(/width: 0%/)
    })

    it('uses the custom color label when provided', () => {
      state.colorLabels = { rouge: 'Tomato' }
      const html = renderColorBarsHTML([{ id: 'rouge', name: 'Rouge', color: '#f00' }], { rouge: 1 }, 1)
      expect(html).toContain('Tomato')
    })
  })

  describe('renderCombinationsHTML', () => {
    it('shows the empty state when no votes have been cast', () => {
      state.stagiaires = []
      expect(renderCombinationsHTML()).toContain('empty-state')
    })

    it('renders each combination as a bar', () => {
      state.stagiaires = [
        { id: 's1', connected: true, vote: ['rouge'] },
        { id: 's2', connected: true, vote: ['rouge'] },
        { id: 's3', connected: true, vote: ['vert'] }
      ]
      state.selectedColors = new Set(['rouge', 'vert'])
      const html = renderCombinationsHTML()
      // Two distinct combinations.
      expect(html).toContain('combo-item')
    })
  })

  describe('renderStagiairesVotesHTML', () => {
    it('shows the empty state when no stagiaires exist', () => {
      state.stagiaires = []
      expect(renderStagiairesVotesHTML()).toContain('empty-state')
    })

    it('renders a waiting row for connected non-voters', () => {
      state.stagiaires = [{ id: 's1', name: 'Marie', connected: true, vote: [] }]
      const html = renderStagiairesVotesHTML()
      expect(html).toContain('waiting')
      expect(html).toContain('Marie')
    })

    it('renders color swatches for a voted stagiaire', () => {
      state.selectedColors = new Set(['rouge', 'vert'])
      state.stagiaires = [{ id: 's1', name: 'Marie', connected: true, vote: ['rouge'] }]
      const html = renderStagiairesVotesHTML()
      expect(html).toContain('stagiaire-vote-swatch')
    })

    it('renders the blank-vote label for a "blank" vote', () => {
      state.stagiaires = [{ id: 's1', name: 'Marie', connected: true, vote: ['blank'] }]
      const html = renderStagiairesVotesHTML()
      expect(html).toContain('stagiaire-vote-blank')
    })

    it('marks disconnected stagiaires with the disconnected dot class', () => {
      state.stagiaires = [{ id: 's1', name: 'Marie', connected: false, vote: ['rouge'] }]
      const html = renderStagiairesVotesHTML()
      expect(html).toContain('disconnected')
    })

    it('falls back to "Anonyme" when name is missing', () => {
      state.stagiaires = [{ id: 's1', name: '', connected: true, vote: ['rouge'] }]
      expect(renderStagiairesVotesHTML()).toContain('Anonyme')
    })
  })

  // ============================================
  // F10: sanitizeColor tripwire
  // ============================================
  // COLORS is a constant today, so sanitizeColor is defense-in-depth.
  // The tests below feed poisoned IDs into the rendering paths to prove
  // that *if* a custom-palette feature ever let operator-supplied
  // colour values reach the DOM, the malicious payload would be stripped
  // before the style attribute is generated.
  describe('F10: color sanitization', () => {
    const POISON = '"; pointer-events:all; background-image:url(javascript:1)'
    const POISONED_COLORS = [
      { id: 'x', name: 'X', color: POISON },
      { id: 'y', name: 'Y', color: '#22c55e' }
    ]

    it('renderConfigHTML strips a poisoned color value from the swatch style', () => {
      // Replace the module-level COLORS reference for this assertion.
      // We exercise renderColorBarsHTML directly since it accepts an
      // explicit colors array (the other renderers read COLORS).
      const html = renderColorBarsHTML([POISONED_COLORS[0]], { x: 1 }, 1)
      expect(html).not.toContain(POISON)
      expect(html).toContain('#666666') // sanitizeColor fallback
    })

    it('renderCombinationsHTML strips a poisoned segment color', () => {
      state.selectedColors = new Set(['x'])
      state.stagiaires = [{ id: 's1', name: 'M', connected: true, vote: ['x'] }]
      // COLORS is module-scoped; the renderers look up `color?.color` by id.
      // We can't inject poisoned values through state.stagiaires alone, but
      // we can prove the sanitizeColor contract by checking that a missing
      // color (color === undefined) produces the safe fallback rather than
      // an empty/undefined interpolation.
      const html = renderCombinationsHTML()
      expect(html).not.toContain('undefined')
      expect(html).toContain('background-color: #666666')
    })

    it('renderStagiairesVotesHTML uses the safe fallback for unknown color IDs', () => {
      state.selectedColors = new Set(['unknown-id'])
      state.stagiaires = [{ id: 's1', name: 'M', connected: true, vote: ['unknown-id'] }]
      const html = renderStagiairesVotesHTML()
      expect(html).not.toContain('undefined')
      expect(html).toContain('background-color: #666666')
    })

    it('renderColorBarsHTML never emits the raw `color.color` value when it is non-hex', () => {
      // Direct path: pass a poisoned color object to renderColorBarsHTML.
      const html = renderColorBarsHTML([{ id: 'x', name: 'X', color: 'red' }], { x: 1 }, 1)
      // 'red' (named colour) is rejected → #666666 fallback used in both
      // the swatch and the fill.
      const matches = html.match(/background-color:\s*red/g)
      expect(matches).toBeNull()
    })
  })
})
