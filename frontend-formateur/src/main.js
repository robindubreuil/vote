import './style.css'
import {
  renderLandingPage, renderFullLayout, updateHeader, renderMainContent,
  attachLandingListeners, attachAppKeyboardShortcuts, cleanupAllListeners
} from './renderers.js'
import { initClient } from './websocket.js'
import { state } from './state.js'
import { validateSessionCode } from '../../shared/validation.js'
import { showError, hideError } from '../../shared/ui.js'

/**
 * Leave the current session
 */
function leaveSession() {
  sessionStorage.removeItem('vote_session_code')
  if (state.client) {
    state.client.close()
  }
  state.sessionCode = null
  state.connectedCount = 0
  state.voteState = 'idle'
  state.client = null

  // Re-render landing page
  renderLandingPage(document.getElementById('app'))

  // Re-attach landing listeners
  attachLandingListenersWithHandlers()
}

/**
 * Attach landing page event listeners with keyboard shortcuts
 */
function attachLandingListenersWithHandlers() {
  const createSession = () => {
    import('./handlers.js').then(({ joinSession }) => {
      joinSession(null, initClient)
    })
  }

  const joinSession = (code) => {
    const error = validateSessionCode(code)
    if (error) {
      const joinInput = document.getElementById('joinSessionInput')
      if (joinInput) joinInput.classList.add('error')
      showError(error)
      return
    }
    const joinInput = document.getElementById('joinSessionInput')
    if (joinInput) joinInput.classList.remove('error')
    import('./handlers.js').then(({ joinSession: joinSessionFn }) => {
      joinSessionFn(code, initClient)
    })
  }

  attachLandingListeners(joinSession, createSession)

  // Also attach input event for error clearing
  const joinInput = document.getElementById('joinSessionInput')
  if (joinInput) {
    joinInput.addEventListener('input', () => {
      joinInput.classList.remove('error')
      hideError()
    })
  }
}

/**
 * Initialize the application
 */
function init() {
  const app = document.getElementById('app')

  // Get or generate trainer ID
  let trainerId = sessionStorage.getItem('vote_trainer_id')

  // Get or create session code
  let savedSessionCode = sessionStorage.getItem('vote_session_code')

  // Check URL params for session code (override saved code if present)
  const urlParams = new URLSearchParams(window.location.search)
  const urlSession = urlParams.get('session')
  if (urlSession && validateSessionCode(urlSession) === null) {
    savedSessionCode = urlSession
  }

  if (savedSessionCode) {
    state.sessionCode = savedSessionCode
    // Render full layout
    renderFullLayout(app)
    // Initialize WebSocket
    initClient()
    // Attach keyboard shortcuts for app
    attachAppKeyboardShortcuts(leaveSession)
  } else {
    // Show landing page
    renderLandingPage(app)
    // Attach landing page listeners
    attachLandingListenersWithHandlers()
  }
}

// Start the application
init()
