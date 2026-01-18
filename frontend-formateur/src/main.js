import './style.css'
import { renderLandingPage, renderFullLayout, updateHeader, renderMainContent } from './renderers.js'
import { initClient } from './websocket.js'
import { state } from './state.js'
import { validateSessionCode } from '../../shared/validation.js'

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
  } else {
    // Show landing page
    renderLandingPage(app)
    // Attach landing page listeners
    attachLandingListeners()
  }
}

/**
 * Attach landing page event listeners
 */
function attachLandingListeners() {
  const createBtn = document.getElementById('createSessionBtn')
  const joinBtn = document.getElementById('joinSessionBtn')
  const joinInput = document.getElementById('joinSessionInput')

  if (createBtn) {
    createBtn.addEventListener('click', () => {
      import('./handlers.js').then(({ joinSession }) => {
        import('./renderers.js').then(({ updateLandingPageLoadingState }) => {
          joinSession(null, updateLandingPageLoadingState, initClient)
        })
      })
    })
  }

  if (joinBtn && joinInput) {
    const handleJoin = () => {
      const code = joinInput.value.trim()
      const error = validateSessionCode(code)
      if (error) {
        joinInput.classList.add('error')
        import('../../shared/ui.js').then(({ showError }) => {
          showError(error)
        })
        return
      }
      joinInput.classList.remove('error')
      import('./handlers.js').then(({ joinSession }) => {
        import('./renderers.js').then(({ updateLandingPageLoadingState }) => {
          joinSession(code, updateLandingPageLoadingState, initClient)
        })
      })
    }

    joinBtn.addEventListener('click', handleJoin)
    joinInput.addEventListener('keydown', (e) => {
      if (e.key === 'Enter') handleJoin()
    })
    joinInput.addEventListener('input', () => {
      joinInput.classList.remove('error')
      import('../../shared/ui.js').then(({ hideError }) => {
        hideError()
      })
    })
  }

  if (state.connecting) {
    import('./renderers.js').then(({ updateLandingPageLoadingState }) => {
      updateLandingPageLoadingState(true)
    })
  }
}

// Start the application
init()
