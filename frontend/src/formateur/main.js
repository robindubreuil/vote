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
import { state, resetTrainerState } from './state.js'
import { validateSessionCode } from '@shared/validation.js'
import { CONSTANTS } from '@shared/config.js'
import { initConnectionAid } from './connection-aid.js'
import { initPWA } from '@shared/pwa.js'
import { safeSessionGet, safeSessionRemove } from '@shared/utils/safe-storage.js'
import { stopTimer } from './utils.js'

setActionHandlers({
  startVote: handlers.startVote,
  closeVote: handlers.closeVote,
  resetVote: handlers.resetVote,
  revealAnswers: handlers.revealAnswers,
  resetConfig: handlers.resetConfig,
  beginSavePreset: handlers.beginSavePreset,
  cancelSavePreset: handlers.cancelSavePreset,
  confirmSavePreset: handlers.confirmSavePreset,
  applyPreset: handlers.applyPreset,
  deletePreset: handlers.deletePresetHandler,
  exportPresets: handlers.exportPresetsHandler,
  importPresets: handlers.importPresetsHandler
})

function leaveSession() {
  stopTimer()
  safeSessionRemove('vote_session_code')
  safeSessionRemove('vote_trainer_id')
  closeClient()
  resetTrainerState()
  cleanupAllListeners()
  renderLandingPage(document.getElementById('app'))
  attachLandingListenersWithHandlers()
}

function init() {
  const app = document.getElementById('app')

  const urlParams = new URLSearchParams(window.location.search)

  // "Aide à la connexion" standalone view — render it and bail out.
  // This view lives in its own tab so the formateur can move it to the
  // videoprojector. It does NOT connect to the WebSocket (the backend only
  // allows one trainer per session); instead it subscribes to state updates
  // from the main formateur tab via BroadcastChannel.
  const rawAidCode = urlParams.get('aide')
  if (rawAidCode) {
    const aidCode = CONSTANTS.SESSION_CODE_NORMALIZE(rawAidCode)
    if (validateSessionCode(aidCode) === null) {
      initConnectionAid(aidCode)
      return
    }
  }

  let savedSessionCode = safeSessionGet('vote_session_code')

  const urlSession = urlParams.get('session')
  if (urlSession && validateSessionCode(urlSession) === null) {
    savedSessionCode = CONSTANTS.SESSION_CODE_NORMALIZE(urlSession)
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
initPWA()
