// @vitest-environment happy-dom
import { describe, it, expect, beforeEach } from 'vitest'
import { state, AppState } from './state.js'
import { renderLayout, render } from './renderers.js'

// The stagiaire renderer is the trainee's whole UI: it renders one of five
// AppState shells (JOINING, WAITING, VOTING, VOTED, CLOSED) plus a prenomEdit
// modal, attaches listeners via a tracker, and wipes them on every re-render.
// We assert:
//   - the right view shows up for each appState
//   - renderPlayGameButtonHTML is gated on state.gameEnabled
//   - render is idempotent (re-rendering the same state produces the same DOM)
//   - the listener tracker is cleaned up between renders (no leak)
//   - focus is preserved across re-renders

describe('stagiaire renderers', () => {
  beforeEach(() => {
    document.body.innerHTML = '<div id="app"></div>'
    // Reset every relevant state field so tests don't depend on each other.
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

    renderLayout(document.getElementById('app'))
  })

  describe('renderLayout', () => {
    it('renders the main container and game overlay skeleton', () => {
      expect(document.getElementById('main-container')).not.toBeNull()
      expect(document.getElementById('game-overlay')).not.toBeNull()
      expect(document.getElementById('game-overlay').hidden).toBe(true)
      // Game HUD elements exist (so handlers can populate them later).
      expect(document.getElementById('gameBoard')).not.toBeNull()
      expect(document.getElementById('gamePalette')).not.toBeNull()
      expect(document.getElementById('gameSubmitBtn')).not.toBeNull()
    })

    it('renders the footer', () => {
      expect(document.querySelector('.footer')).not.toBeNull()
    })

    it('renders all overlay screens initially hidden', () => {
      expect(document.getElementById('gamePauseScreen').hidden).toBe(true)
      expect(document.getElementById('gameOverScreen').hidden).toBe(true)
      expect(document.getElementById('gameRulesScreen').hidden).toBe(true)
    })
  })

  describe('AppState.JOINING', () => {
    it('renders the join form with both required inputs', () => {
      state.appState = AppState.JOINING
      render()
      expect(document.getElementById('joinForm')).not.toBeNull()
      expect(document.getElementById('prenom')).not.toBeNull()
      expect(document.getElementById('sessionCode')).not.toBeNull()
      // No session-code button on the join form (no session yet).
      expect(document.getElementById('leaveSessionBtn')).toBeNull()
    })

    it('reflects the saved prenom in the input value', () => {
      state.prenom = 'Marie'
      state.appState = AppState.JOINING
      render()
      expect(document.getElementById('prenom').value).toBe('Marie')
    })
  })

  describe('AppState.WAITING', () => {
    beforeEach(() => {
      state.appState = AppState.WAITING
      state.sessionCode = 'ABC'
      state.prenom = 'Marie'
    })

    it('renders the waiting text and the session-code button', () => {
      render()
      expect(document.querySelector('[data-testid="waiting-text"]')).not.toBeNull()
      expect(document.getElementById('leaveSessionBtn')).not.toBeNull()
      expect(document.getElementById('leaveSessionBtn').textContent).toBe('ABC')
      expect(document.getElementById('editNameBtn')).not.toBeNull()
    })

    it('marks the session-code button connected when state.connected=true', () => {
      state.connected = true
      render()
      const btn = document.getElementById('leaveSessionBtn')
      expect(btn.className).toContain('connected')
    })

    it('omits the play-game button when state.gameEnabled is false', () => {
      state.gameEnabled = false
      render()
      expect(document.getElementById('playGameBtn')).toBeNull()
    })

    it('shows the play-game button when state.gameEnabled is true', () => {
      state.gameEnabled = true
      render()
      expect(document.getElementById('playGameBtn')).not.toBeNull()
    })
  })

  describe('AppState.VOTING — single choice', () => {
    beforeEach(() => {
      state.appState = AppState.VOTING
      state.sessionCode = 'ABC'
      state.availableColors = ['rouge', 'vert', 'bleu']
      state.multipleChoice = false
    })

    it('renders one vote button per available color', () => {
      render()
      expect(document.querySelector('[data-testid="vote-btn-rouge"]')).not.toBeNull()
      expect(document.querySelector('[data-testid="vote-btn-vert"]')).not.toBeNull()
      expect(document.querySelector('[data-testid="vote-btn-bleu"]')).not.toBeNull()
      // Single-choice: no submit button.
      expect(document.getElementById('submitVote')).toBeNull()
    })

    it('marks the selected vote button with the selected class', () => {
      state.selectedColors = new Set(['vert'])
      render()
      const vert = document.querySelector('[data-testid="vote-btn-vert"]')
      expect(vert.className).toContain('selected')
      expect(vert.getAttribute('aria-checked')).toBe('true')
    })

    it('disables buttons when disconnected', () => {
      state.connected = false
      render()
      expect(document.querySelector('[data-testid="vote-btn-rouge"]').disabled).toBe(true)
    })
  })

  describe('AppState.VOTING — multiple choice', () => {
    beforeEach(() => {
      state.appState = AppState.VOTING
      state.sessionCode = 'ABC'
      state.availableColors = ['rouge', 'vert']
      state.multipleChoice = true
    })

    it('renders checkboxes + a submit button', () => {
      render()
      expect(document.getElementById('color-rouge')).not.toBeNull()
      expect(document.getElementById('color-vert')).not.toBeNull()
      expect(document.getElementById('submitVote')).not.toBeNull()
    })

    it('disables submit when no color is selected', () => {
      state.selectedColors = new Set()
      render()
      expect(document.getElementById('submitVote').disabled).toBe(true)
    })

    it('enables submit when at least one color is selected', () => {
      state.connected = true
      state.selectedColors = new Set(['rouge'])
      render()
      expect(document.getElementById('submitVote').disabled).toBe(false)
    })

    it('omits the blank-vote button when allowBlank is false', () => {
      state.allowBlank = false
      render()
      expect(document.getElementById('blankVoteBtn')).toBeNull()
    })

    it('shows the blank-vote button when allowBlank is true', () => {
      state.allowBlank = true
      render()
      expect(document.getElementById('blankVoteBtn')).not.toBeNull()
    })

    it('uses colorLabels when available, falling back to the default name', () => {
      state.colorLabels = { rouge: 'Tomato' }
      render()
      expect(document.querySelector('label[for="color-rouge"]').textContent).toContain('Tomato')
      expect(document.querySelector('label[for="color-vert"]').textContent).toContain('Vert')
    })
  })

  describe('AppState.VOTED', () => {
    beforeEach(() => {
      state.appState = AppState.VOTED
      state.sessionCode = 'ABC'
      state.prenom = 'Marie'
      state.selectedColors = new Set(['rouge', 'vert'])
    })

    it('renders the voted title and the chosen colors', () => {
      render()
      expect(document.querySelector('[data-testid="voted-title"]')).not.toBeNull()
      const subtitle = document.querySelector('[data-testid="voted-subtitle"]')
      expect(subtitle.textContent).toContain('Rouge')
      expect(subtitle.textContent).toContain('Vert')
    })

    it('renders the change-vote button', () => {
      render()
      expect(document.getElementById('changeVoteBtn')).not.toBeNull()
    })

    it('shows the play-game button only when gameEnabled is true', () => {
      state.gameEnabled = false
      render()
      expect(document.getElementById('playGameBtn')).toBeNull()
      state.gameEnabled = true
      render()
      expect(document.getElementById('playGameBtn')).not.toBeNull()
    })

    it('renders "blanc" label when the vote contains "blank"', () => {
      state.selectedColors = new Set(['blank'])
      render()
      const subtitle = document.querySelector('[data-testid="voted-subtitle"]')
      expect(subtitle.textContent).toContain('blanc')
    })
  })

  describe('AppState.CLOSED', () => {
    beforeEach(() => {
      state.appState = AppState.CLOSED
      state.sessionCode = 'ABC'
    })

    it('renders the vote-closed text', () => {
      render()
      expect(document.querySelector('[data-testid="vote-closed-text"]')).not.toBeNull()
    })

    it('omits the score feedback when not competitive', () => {
      state.competitive = false
      render()
      expect(document.querySelector('.score-feedback')).toBeNull()
    })

    it('omits the score feedback when competitive but not revealed', () => {
      state.competitive = true
      state.revealed = false
      render()
      expect(document.querySelector('.score-feedback')).toBeNull()
    })

    it('shows the score feedback when competitive + revealed', () => {
      state.competitive = true
      state.revealed = true
      state.voteScore = 1500
      state.totalScore = 3000
      state.rank = 2
      state.totalStagiaires = 12
      render()
      const feedback = document.querySelector('.score-feedback')
      expect(feedback).not.toBeNull()
      expect(feedback.textContent).toContain('+1500')
      expect(feedback.textContent).toContain('3000')
      // 2 → "2e" rank suffix
      expect(feedback.textContent).toContain('2e')
      expect(feedback.textContent).toContain('12')
    })

    it('uses "1er" rank suffix when rank is 1', () => {
      state.competitive = true
      state.revealed = true
      state.rank = 1
      state.totalStagiaires = 5
      render()
      expect(document.querySelector('.score-feedback').textContent).toContain('1er')
    })

    it('shows the negative voteScore without a plus sign', () => {
      state.competitive = true
      state.revealed = true
      state.voteScore = -500
      state.totalScore = 0
      state.rank = 5
      state.totalStagiaires = 5
      render()
      expect(document.querySelector('.score-feedback').textContent).toContain('-500')
    })
  })

  describe('prenomEdit', () => {
    beforeEach(() => {
      state.appState = AppState.WAITING
      state.prenomEdit = true
      state.prenom = 'Old'
    })

    it('renders the edit-name modal instead of the waiting view', () => {
      render()
      expect(document.getElementById('editNameForm')).not.toBeNull()
      expect(document.getElementById('editPrenom').value).toBe('Old')
    })

    it('does not render the join form when in edit mode', () => {
      render()
      expect(document.getElementById('joinForm')).toBeNull()
    })
  })

  describe('idempotency', () => {
    it('render() twice on the same state produces identical innerHTML', () => {
      state.appState = AppState.VOTING
      state.sessionCode = 'ABC'
      state.availableColors = ['rouge', 'vert']
      state.multipleChoice = true
      state.selectedColors = new Set(['rouge'])
      render()
      const a = document.getElementById('main-container').innerHTML
      render()
      const b = document.getElementById('main-container').innerHTML
      expect(a).toBe(b)
    })

    it('render() with VOTING state then VOTED state actually changes the DOM', () => {
      state.appState = AppState.VOTING
      state.sessionCode = 'ABC'
      state.availableColors = ['rouge']
      state.multipleChoice = false
      render()
      const votingHTML = document.getElementById('main-container').innerHTML

      state.appState = AppState.VOTED
      state.selectedColors = new Set(['rouge'])
      render()
      const votedHTML = document.getElementById('main-container').innerHTML

      expect(votingHTML).not.toBe(votedHTML)
    })
  })

  describe('listener lifecycle', () => {
    // The renderer wires inline handlers (changeVoteBtn, editNameBtn,
    // cancelEditName) directly. The tracker's contract is verified in
    // shared/dom/listeners.test.js; here we confirm the inline handlers
    // actually mutate state and trigger a re-render.

    it('the changeVoteBtn click transitions back to VOTING', () => {
      state.appState = AppState.VOTED
      state.sessionCode = 'ABC'
      state.selectedColors = new Set(['rouge'])
      render()
      expect(document.getElementById('changeVoteBtn')).not.toBeNull()

      document.getElementById('changeVoteBtn').click()
      expect(state.appState).toBe(AppState.VOTING)
      expect(state.hasVoted).toBe(false)
    })

    it('the editNameBtn click flips state.prenomEdit and re-renders', () => {
      state.appState = AppState.WAITING
      state.sessionCode = 'ABC'
      state.prenom = 'Marie'
      render()
      document.getElementById('editNameBtn').click()
      expect(state.prenomEdit).toBe(true)
      // The edit modal is now visible.
      expect(document.getElementById('editNameForm')).not.toBeNull()
    })

    it('the cancelEditName click exits prenomEdit', () => {
      state.appState = AppState.WAITING
      state.sessionCode = 'ABC'
      state.prenomEdit = true
      render()
      document.getElementById('cancelEditName').click()
      expect(state.prenomEdit).toBe(false)
    })
  })

  describe('focus preservation across re-renders', () => {
    it('restores focus to the same element ID after a re-render', () => {
      state.appState = AppState.JOINING
      render()
      const input = document.getElementById('prenom')
      input.focus()
      expect(document.activeElement).toBe(input)

      // Re-render: focus should be restored by id.
      render()
      expect(document.activeElement?.id).toBe('prenom')
    })
  })

  // ============================================
  // Session 12 — accessibility (F6, F7, F8)
  // ============================================
  describe('F6: landmark structure', () => {
    it('renderLayout emits exactly one <header> and one <main>', () => {
      // renderLayout was called in beforeEach. Inspect the resulting DOM.
      expect(document.querySelectorAll('header#app-header')).toHaveLength(1)
      expect(document.querySelectorAll('main#main-container')).toHaveLength(1)
    })

    it('header is hidden during JOINING (no session code to show)', () => {
      state.appState = AppState.JOINING
      render()
      const header = document.getElementById('app-header')
      expect(header.hidden).toBe(true)
      expect(header.innerHTML).toBe('')
    })

    it('header is populated and visible in WAITING', () => {
      state.appState = AppState.WAITING
      state.sessionCode = 'ABC'
      render()
      const header = document.getElementById('app-header')
      expect(header.hidden).toBe(false)
      expect(header.querySelector('h1')).not.toBeNull()
      expect(header.querySelector('#leaveSessionBtn')).not.toBeNull()
    })

    it('header is populated and visible in VOTING, VOTED, CLOSED', () => {
      for (const s of [AppState.VOTING, AppState.VOTED, AppState.CLOSED]) {
        state.appState = s
        state.sessionCode = 'ABC'
        render()
        const header = document.getElementById('app-header')
        expect(header.hidden, `header visible for ${s}`).toBe(false)
        expect(header.querySelector('#leaveSessionBtn'), `header content for ${s}`).not.toBeNull()
      }
    })

    it('header is hidden while the edit-name modal is open', () => {
      state.appState = AppState.WAITING
      state.prenomEdit = true
      state.sessionCode = 'ABC'
      render()
      expect(document.getElementById('app-header').hidden).toBe(true)
    })

    it('each per-state render contains no inline <header> (header is stable, not per-render)', () => {
      for (const s of [AppState.WAITING, AppState.VOTING, AppState.VOTED, AppState.CLOSED]) {
        state.appState = s
        state.sessionCode = 'ABC'
        render()
        const nested = document.getElementById('main-container').querySelectorAll('header')
        expect(nested.length, `no nested header in main-container for ${s}`).toBe(0)
      }
    })
  })

  describe('F7: vote grid grouping semantics', () => {
    it('single-choice grid exposes role=radiogroup + aria-label', () => {
      state.appState = AppState.VOTING
      state.sessionCode = 'ABC'
      state.availableColors = ['rouge', 'vert']
      state.multipleChoice = false
      render()
      const grid = document.querySelector('.vote-grid')
      expect(grid.getAttribute('role')).toBe('radiogroup')
      expect(grid.getAttribute('aria-label')).toBeTruthy()
    })

    it('single-choice buttons expose role=radio + aria-checked', () => {
      state.appState = AppState.VOTING
      state.sessionCode = 'ABC'
      state.availableColors = ['rouge', 'vert']
      state.multipleChoice = false
      state.selectedColors = new Set(['rouge'])
      render()
      const rouge = document.querySelector('[data-testid="vote-btn-rouge"]')
      const vert = document.querySelector('[data-testid="vote-btn-vert"]')
      expect(rouge.getAttribute('role')).toBe('radio')
      expect(rouge.getAttribute('aria-checked')).toBe('true')
      expect(vert.getAttribute('aria-checked')).toBe('false')
    })

    it('multiple-choice options are wrapped in a <fieldset>', () => {
      state.appState = AppState.VOTING
      state.sessionCode = 'ABC'
      state.availableColors = ['rouge', 'vert']
      state.multipleChoice = true
      render()
      const fieldset = document.querySelector('fieldset.vote-grid')
      expect(fieldset).not.toBeNull()
    })

    it('multiple-choice fieldset carries a non-empty <legend>', () => {
      state.appState = AppState.VOTING
      state.sessionCode = 'ABC'
      state.availableColors = ['rouge', 'vert']
      state.multipleChoice = true
      render()
      const legend = document.querySelector('fieldset.vote-grid legend')
      expect(legend).not.toBeNull()
      expect(legend.textContent.trim()).not.toBe('')
    })
  })

  describe('F8: form-validation aria wiring', () => {
    it('join form inputs reference the error element via aria-describedby', () => {
      state.appState = AppState.JOINING
      render()
      const error = document.getElementById('join-error')
      expect(error).not.toBeNull()
      expect(error.getAttribute('role')).toBe('alert')
      expect(document.getElementById('prenom').getAttribute('aria-describedby')).toBe('join-error')
      expect(document.getElementById('sessionCode').getAttribute('aria-describedby')).toBe('join-error')
    })

    it('join form inputs start with aria-invalid=false', () => {
      state.appState = AppState.JOINING
      render()
      expect(document.getElementById('prenom').getAttribute('aria-invalid')).toBe('false')
      expect(document.getElementById('sessionCode').getAttribute('aria-invalid')).toBe('false')
    })

    it('edit-name input references its own inline error element', () => {
      state.appState = AppState.WAITING
      state.prenomEdit = true
      state.prenom = 'Marie'
      state.sessionCode = 'ABC'
      render()
      const error = document.getElementById('edit-name-error')
      expect(error).not.toBeNull()
      expect(error.getAttribute('role')).toBe('alert')
      const input = document.getElementById('editPrenom')
      expect(input.getAttribute('aria-describedby')).toBe('edit-name-error')
      expect(input.getAttribute('aria-invalid')).toBe('false')
    })
  })
})
