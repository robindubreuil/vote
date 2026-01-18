import './style.css'
import { renderLayout, render, setHandlers } from './renderers.js'
import { initClient, connectToSession } from './websocket.js'
import * as handlers from './handlers.js'
import { state } from './state.js'
import { validateSessionCode } from '../../shared/validation.js'
import { getSessionCodeFromURL } from '../../shared/utils/url.js'

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
    leaveSession: handlers.leaveSession,
    handleKeyPress: handlers.handleKeyPress
  })

  // Initialize the WebSocket client BEFORE rendering
  // so handlers can access it via getClient()
  initClient()

  // Récupérer l'ID du stagiaire (généré par le serveur)
  const savedId = sessionStorage.getItem('vote_stagiaire_id')
  if (savedId) {
    state.stagiaireId = savedId
  }

  // Récupérer le prénom s'il existe
  const savedPrenom = localStorage.getItem('vote_stagiaire_prenom')
  if (savedPrenom) {
    state.prenom = savedPrenom
  }

  // Vérifier si on a déjà un code session enregistré
  let savedCode = sessionStorage.getItem('vote_session_code')

  // Check URL params for session code (override saved code if present)
  const urlSession = getSessionCodeFromURL()
  if (urlSession && validateSessionCode(urlSession) === null) {
      savedCode = urlSession
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
