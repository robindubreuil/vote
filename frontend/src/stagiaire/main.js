import './style.css'
import { renderLayout, render, setHandlers } from './renderers.js'
import { initClient, connectToSession } from './websocket.js'
import * as handlers from './handlers.js'
import { state } from './state.js'
import { validateSessionCode } from '@shared/validation.js'
import { CONSTANTS } from '@shared/config.js'
import { getSessionCodeFromURL } from '@shared/utils/url.js'
import { safeLocalGet, safeSessionGet } from '@shared/utils/safe-storage.js'

// Élément DOM principal
const app = document.getElementById('app')

/**
 * Initialize the application
 */
function init() {
  // Set connectToSession function in handlers
  handlers.setConnectToSession(connectToSession)

  // Set handlers in renderers for event listener attachment
  setHandlers({
    handleJoin: handlers.handleJoin,
    handleEditName: handlers.handleEditName,
    handleSingleChoiceVote: handlers.handleSingleChoiceVote,
    handleCheckboxChange: handlers.handleCheckboxChange,
    handleSubmitVote: handlers.handleSubmitVote,
    handleBlankVote: handlers.handleBlankVote,
    leaveSession: handlers.leaveSession,
    handleKeyPress: handlers.handleKeyPress,
    handlePlayGame: handlers.handlePlayGame
  })

  // Initialize the WebSocket client BEFORE rendering
  // so handlers can access it via getClient()
  initClient()

  // Récupérer l'ID du stagiaire (généré par le serveur)
  const savedId = safeSessionGet('vote_stagiaire_id')
  if (savedId) {
    state.stagiaireId = savedId
  }

  const savedPrenom = safeLocalGet('vote_stagiaire_prenom')
  if (savedPrenom) {
    state.prenom = savedPrenom
  }

  let savedCode = safeSessionGet('vote_session_code')

  // Check URL params for session code (override saved code if present)
  const urlSession = getSessionCodeFromURL()
  if (urlSession && validateSessionCode(urlSession) === null) {
    savedCode = CONSTANTS.SESSION_CODE_NORMALIZE(urlSession)
  }

  if (savedCode) {
    state.sessionCode = savedCode
  }

  // Initialiser la structure de base (Header, Main, Footer)
  renderLayout(app)

  render()

  // Auto-connect if we have both code and name
  if (state.sessionCode && state.prenom && !state.connected) {
    connectToSession(state.sessionCode)
  }
}

// Démarrage
init()
