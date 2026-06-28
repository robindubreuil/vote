import { VoteClient } from '@shared/websocket-client.js'
import { getWebSocketURL } from '@shared/config.js'
import { showError } from '@shared/ui.js'
import { state, AppState } from './state.js'
import { render } from './renderers.js'
import { pauseGameExternal } from './handlers.js'
import { safeSessionSet } from '@shared/utils/safe-storage.js'

// Configuration de l'API WebSocket
const WS_URL = getWebSocketURL()

// WebSocket client instance
let client = null

/**
 * Initialize the WebSocket client
 * @returns {VoteClient} The initialized client
 */
export function initClient() {
  client = new VoteClient(WS_URL, {
    onStatusChange: (connected) => {
      state.connected = connected
      // Auto-pause the game on disconnect — the trainee shouldn't be
      // mid-catch when the round could start without them knowing.
      if (!connected) {
        pauseGameExternal()
      }
      // Re-render to update session code connection status
      if (state.appState !== AppState.JOINING) {
        render()
      }
    },
    onOpen: () => {
      // Si on a un code session et un prénom, on tente de rejoindre
      if (state.sessionCode && state.prenom) {
        client.send({
          type: 'stagiaire_join',
          sessionCode: state.sessionCode,
          name: state.prenom,
          stagiaireId: state.stagiaireId || undefined
        })
      }
    },
    onMessage: (msg) => {
      handleMessage(msg)
    }
  })

  return client
}

/**
 * Get the current WebSocket client
 */
export function getClient() {
  return client
}

/**
 * Connect to a session
 * @param {string} code - The session code to connect to
 */
export function connectToSession(code) {
  state.sessionCode = code
  // Si le client n'est pas initialisé, on le fait
  if (!client) {
    initClient()
  }

  // On lance la connexion (cela fermera l'ancienne si elle existe)
  client.connect()
}

/**
 * Handle incoming WebSocket messages
 * @param {Object} msg - The message received from the server
 */
function handleMessage(msg) {
  switch (msg.type) {
    case 'session_joined':
      // Connexion réussie à la session
      state.sessionCode = msg.sessionCode
      // Store the server-generated stagiaireId
      if (msg.stagiaireId) {
        state.stagiaireId = msg.stagiaireId
        safeSessionSet('vote_stagiaire_id', msg.stagiaireId)
      }
      state.appState = AppState.WAITING
      safeSessionSet('vote_session_code', msg.sessionCode)
      render()
      break

    case 'error': {
      let errorMessage = msg.message || 'Erreur de connexion'
      if (errorMessage === 'Session not found') {
        errorMessage = 'Session introuvable'
      }
      showError(errorMessage)
      break
    }

    case 'vote_started':
      // Nouveau vote lancé — pause the mini-game if it was open and
      // surface the vote UI. The trainee must vote before resuming.
      pauseGameExternal()
      state.availableColors = msg.colors || []
      state.multipleChoice = msg.multipleChoice || false
      state.colorLabels = msg.labels || {}
      if (typeof msg.gameEnabled === 'boolean') {
        state.gameEnabled = msg.gameEnabled
      }
      state.selectedColors.clear()

      // Restore existing vote if rejoining
      if (msg.existingVote && Array.isArray(msg.existingVote)) {
        msg.existingVote.forEach((colorId) => state.selectedColors.add(colorId))
        state.hasVoted = true
        state.appState = AppState.VOTED
      } else {
        state.hasVoted = false
        state.appState = AppState.VOTING
      }
      render()
      break

    case 'vote_accepted':
      // Vote accepté
      state.hasVoted = true
      state.appState = AppState.VOTED
      render()
      break

    case 'vote_closed':
      // Vote terminé par le formateur — pause the game too; the trainee
      // shouldn't be mid-catch when the round result is shown.
      pauseGameExternal()
      state.appState = AppState.CLOSED
      render()
      break

    case 'vote_reset':
      // Réinitialisation pour un nouveau vote
      if (typeof msg.gameEnabled === 'boolean') {
        state.gameEnabled = msg.gameEnabled
      }
      pauseGameExternal()
      state.appState = AppState.WAITING
      state.selectedColors.clear()
      state.hasVoted = false
      render()
      break

    case 'name_updated':
      // Confirmation de mise à jour du nom
      state.prenomEdit = false
      render()
      break
  }
}
