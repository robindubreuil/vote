import { showError } from '../../shared/ui.js'
import { COLORS } from '../../shared/colors.js'
import { state } from './state.js'
import { renderMainContent } from './renderers.js'

/**
 * Reset config to default values
 */
export function resetConfig() {
  state.selectedColors = new Set(COLORS.slice(0, 3).map(c => c.id))
  state.colorLabels = {}
  state.multipleChoice = false
  renderMainContent()
}

/**
 * Start the vote
 * @param {Object} client - WebSocket client instance
 */
export function startVote(client) {
  if (!client) {
    showError("Erreur de connexion")
    return
  }

  client.send({
    type: 'start_vote',
    sessionCode: state.sessionCode,
    colors: Array.from(state.selectedColors),
    multipleChoice: state.multipleChoice,
    labels: state.colorLabels
  })
}

/**
 * Close the vote
 * @param {Object} client - WebSocket client instance
 */
export function closeVote(client) {
  if (!client) {
    showError("Erreur de connexion")
    return
  }

  client.send({
    type: 'close_vote',
    sessionCode: state.sessionCode
  })
}

/**
 * Reset the vote for a new round
 * @param {Object} client - WebSocket client instance
 */
export function resetVote(client) {
  if (!client) {
    showError("Erreur de connexion")
    return
  }

  client.send({
    type: 'reset_vote',
    sessionCode: state.sessionCode,
    colors: Array.from(state.selectedColors),
    multipleChoice: state.multipleChoice
  })
}

/**
 * Join a session (create or join existing)
 * @param {string|null} code - Session code, or null to create new
 * @param {Function} initClient
 */
export function joinSession(code, initClient) {
  state.sessionCode = code || ""
  state.connecting = true

  if (code) {
    sessionStorage.setItem('vote_session_code', code)
  }

  initClient()
}
