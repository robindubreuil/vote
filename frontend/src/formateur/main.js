import './style.css'
import {
  renderLandingPage,
  renderFullLayout,
  attachAppKeyboardShortcuts,
  registerHeaderLeaveHandler,
  cleanupAllListeners,
  setActionHandlers
} from './renderers.js'
import { initClient, closeClient, attachLandingListenersWithHandlers } from './websocket.js'
import * as handlers from './handlers.js'
import { state, resetTrainerState } from './state.js'
import { validateSessionCode } from '@shared/validation.js'
import { CONSTANTS } from '@shared/config.js'
import { initPWA } from '@shared/pwa.js'
import { safeSessionGet, safeSessionRemove } from '@shared/utils/safe-storage.js'
import { stopTimer } from './utils.js'
import { installGlobalErrorHandlers } from '@shared/error-boundary.js'

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
  safeSessionRemove('vote_trainer_token')
  closeClient()
  resetTrainerState()
  cleanupAllListeners()
  renderLandingPage(document.getElementById('app'))
  attachLandingListenersWithHandlers()
}

async function init() {
  const app = document.getElementById('app')

  const urlParams = new URLSearchParams(window.location.search)

  // "Aide à la connexion" standalone view — render it and bail out.
  // This view lives in its own tab so the formateur can move it to the
  // videoprojector. It does NOT connect to the WebSocket (the backend only
  // allows one trainer per session); instead it subscribes to state updates
  // from the main formateur tab via BroadcastChannel.
  //
  // Dynamic import: connection-aid pulls in `qrcode` (~116 KB), which only
  // this code path uses. Keeping it out of the entry chunk saves every
  // regular formateur page load from paying for it (and keeps it off the
  // service-worker precache list).
  const rawAidCode = urlParams.get('aide')
  if (rawAidCode) {
    const aidCode = CONSTANTS.SESSION_CODE_NORMALIZE(rawAidCode)
    if (validateSessionCode(aidCode) === null) {
      const { initConnectionAid } = await import('./connection-aid.js')
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
  } else {
    renderLandingPage(app)
    attachLandingListenersWithHandlers()
  }

  // R6: attach the app-level Escape shortcut once per page lifecycle,
  // regardless of entry path. Previously this only ran in the
  // savedSessionCode branch, so the most common flow (land → Créer)
  // never wired Escape-to-leave — the session_created handler in
  // websocket.js doesn't call it either. The handler self-guards on
  // state.sessionCode (no-op on the landing page) and attachAppKeyboardShortcuts
  // is itself idempotent, so this is safe to call unconditionally.
  attachAppKeyboardShortcuts(leaveSession)
  // F23: register the same leave handler for the header buttons so
  // updateHeader can (re)bind them whenever it injects fresh markup,
  // without needing the fn passed per-call.
  registerHeaderLeaveHandler(leaveSession)
}

init().catch((err) => console.error('Formateur init failed:', err))
installGlobalErrorHandlers()
initPWA()
