import { VoteClient } from '@shared/websocket-client.js'
import { getWebSocketURL } from '@shared/config.js'
import { showError } from '@shared/ui.js'
import { t } from '@shared/i18n.js'
import { state, AppState } from './state.js'
import { render } from './renderers.js'
import { pauseGameExternal, teardownGame } from './handlers.js'
import { safeSessionSet, safeSessionRemove } from '@shared/utils/safe-storage.js'
import { resetHighScore, saveStreak } from '@shared/game-storage.js'

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
          stagiaireId: state.stagiaireId || undefined,
          // S6/S12: the reclaim token proves ownership of the
          // stagiaireId. Without it, a stale ID alone is rejected
          // (anyone who reads sessionStorage on a shared device could
          // otherwise inherit the prior stagiaire's scores).
          reclaimToken: state.reclaimToken || undefined
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
      state.sessionCode = msg.sessionCode
      if (msg.stagiaireId) {
        state.stagiaireId = msg.stagiaireId
        safeSessionSet('vote_stagiaire_id', msg.stagiaireId)
      }
      // S6/S12: persist the reclaim token alongside the ID so a
      // reconnect can prove ownership. sessionStorage (not localStorage)
      // scopes the token to the tab — a shared device cannot leak it
      // across users.
      if (msg.reclaimToken) {
        state.reclaimToken = msg.reclaimToken
        safeSessionSet('vote_stagiaire_reclaim_token', msg.reclaimToken)
      }
      // If the server signals that the prior ID is no longer
      // reclaimable, drop the stale credentials so the next attempt
      // starts fresh (and any loop stops resending the bad ID).
      if (msg.error === 'session_expired' || msg.staleIdentity) {
        delete state.stagiaireId
        delete state.reclaimToken
        safeSessionRemove('vote_stagiaire_id')
        safeSessionRemove('vote_stagiaire_reclaim_token')
      }
      if (state.appState === AppState.JOINING) {
        resetHighScore()
        saveStreak(0)
      }
      state.appState = AppState.WAITING
      safeSessionSet('vote_session_code', msg.sessionCode)
      render()
      break

    case 'error': {
      let errorMessage = msg.message || t.stagiaire.connectionError
      if (errorMessage === 'Session not found') {
        errorMessage = t.stagiaire.sessionNotFound
      }
      // S6/S12: the server rejected the reclaim token (or the ID is
      // stale post-restart). Drop the cached credentials and retry
      // once as a fresh identity. Without this, the auto-reconnect
      // loop would keep resending the same bad ID forever.
      //
      // F9: compare against the i18n sentinel (kept in sync with the
      // server's UserFacingError mapping in backend/internal/vote/
      // errors.go) instead of an inline literal — a wording change
      // is now a two-file edit instead of a silent break.
      if (errorMessage === t.stagiaire.sessionExpired) {
        delete state.stagiaireId
        delete state.reclaimToken
        safeSessionRemove('vote_stagiaire_id')
        safeSessionRemove('vote_stagiaire_reclaim_token')
        if (state.sessionCode && state.prenom && client) {
          client.send({
            type: 'stagiaire_join',
            sessionCode: state.sessionCode,
            name: state.prenom
          })
        }
        // Suppress the toast — the retry is transparent to the user.
        break
      }
      showError(errorMessage)
      break
    }

    case 'vote_started':
      teardownGame()
      state.availableColors = msg.colors || []
      state.multipleChoice = msg.multipleChoice || false
      state.colorLabels = msg.labels || {}
      if (typeof msg.gameEnabled === 'boolean') {
        state.gameEnabled = msg.gameEnabled
      }
      if (typeof msg.competitive === 'boolean') {
        state.competitive = msg.competitive
      }
      if (typeof msg.allowBlank === 'boolean') {
        state.allowBlank = msg.allowBlank
      }
      state.selectedColors.clear()
      state.revealed = false
      state.voteScore = 0

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
      teardownGame()
      state.appState = AppState.CLOSED
      render()
      break

    case 'answers_revealed':
      state.revealed = true
      if (typeof msg.voteScore === 'number') state.voteScore = msg.voteScore
      if (typeof msg.gameScore === 'number') {
        state.gameScore = msg.gameScore
      }
      if (typeof msg.totalScore === 'number') {
        state.totalScore = msg.totalScore + (state.gameScore || 0)
      }
      if (typeof msg.rank === 'number') state.rank = msg.rank
      if (typeof msg.totalStagiaires === 'number') state.totalStagiaires = msg.totalStagiaires
      if (msg.correctColors) state.availableColors = msg.correctColors
      render()
      break

    case 'vote_reset':
      if (typeof msg.gameEnabled === 'boolean') {
        state.gameEnabled = msg.gameEnabled
      }
      if (typeof msg.competitive === 'boolean') {
        state.competitive = msg.competitive
      }
      if (typeof msg.allowBlank === 'boolean') {
        state.allowBlank = msg.allowBlank
      }
      pauseGameExternal()
      state.appState = AppState.WAITING
      state.selectedColors.clear()
      state.hasVoted = false
      state.revealed = false
      state.voteScore = 0
      render()
      break

    case 'name_updated':
      // Confirmation de mise à jour du nom
      state.prenomEdit = false
      render()
      break
  }
}
