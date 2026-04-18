import './style.css'
import {
  renderLandingPage, renderFullLayout, updateHeader, renderMainContent,
  attachLandingListeners, attachAppKeyboardShortcuts, cleanupAllListeners
} from './renderers.js'
import { initClient, closeClient } from './websocket.js'
import { state } from './state.js'
import { validateSessionCode } from '../../../shared/validation.js'
import { showError, hideError } from '../../../shared/ui.js'

function leaveSession() {
  sessionStorage.removeItem('vote_session_code')
  sessionStorage.removeItem('vote_trainer_id')
  closeClient()
  state.sessionCode = null
  state.connectedCount = 0
  state.voteState = 'idle'
  state.connecting = false
  cleanupAllListeners()
  renderLandingPage(document.getElementById('app'))
  attachLandingListenersWithHandlers()
}

function attachLandingListenersWithHandlers() {
  const createSession = () => {
    import('./handlers.js').then(({ joinSession }) => {
      joinSession(null, undefined, initClient)
    }).catch(() => showError("Erreur de chargement"))
  }

  const joinSessionFn = (code) => {
    const error = validateSessionCode(code)
    if (error) {
      const joinInput = document.getElementById('joinSessionInput')
      if (joinInput) joinInput.classList.add('error')
      showError(error)
      return
    }
    const joinInput = document.getElementById('joinSessionInput')
    if (joinInput) joinInput.classList.remove('error')
    import('./handlers.js').then(({ joinSession }) => {
      joinSession(code, undefined, initClient)
    }).catch(() => showError("Erreur de chargement"))
  }

  attachLandingListeners(joinSessionFn, createSession)

  const joinInput = document.getElementById('joinSessionInput')
  if (joinInput) {
    joinInput.addEventListener('input', () => {
      joinInput.classList.remove('error')
      hideError()
    })
  }
}

function init() {
  const app = document.getElementById('app')

  let trainerId = sessionStorage.getItem('vote_trainer_id')
  let savedSessionCode = sessionStorage.getItem('vote_session_code')

  const urlParams = new URLSearchParams(window.location.search)
  const urlSession = urlParams.get('session')
  if (urlSession && validateSessionCode(urlSession) === null) {
    savedSessionCode = urlSession
  }

  if (savedSessionCode) {
    state.sessionCode = savedSessionCode
    renderFullLayout(app)
    initClient()
    attachAppKeyboardShortcuts(leaveSession)
  } else {
    renderLandingPage(app)
    attachLandingListenersWithHandlers()
  }
}

init()
