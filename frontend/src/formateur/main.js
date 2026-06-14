import './style.css'
import {
  renderLandingPage,
  renderFullLayout,
  attachAppKeyboardShortcuts,
  cleanupAllListeners,
  setActionHandlers
} from './renderers.js'
import { initClient, closeClient, attachLandingListenersWithHandlers } from './websocket.js'
import * as handlers from './handlers.js'
import { state } from './state.js'
import { validateSessionCode } from '../../../shared/validation.js'

setActionHandlers({
  startVote: handlers.startVote,
  closeVote: handlers.closeVote,
  resetVote: handlers.resetVote,
  resetConfig: handlers.resetConfig
})

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

function init() {
  const app = document.getElementById('app')

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
