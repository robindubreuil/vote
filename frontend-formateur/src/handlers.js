import { icons } from '../../shared/icons.js'
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
  const btn = document.getElementById('startVote')
  if (btn) {
    btn.disabled = true
    btn.innerHTML = `${icons.loader(' class="icon icon-md spin"')} Lancement...`
  }

  if (!client) {
    if (btn) {
      btn.disabled = false
      btn.innerHTML = `${icons.rocket(' class="icon icon-md"')} Lancer le vote`
    }
    showError("Erreur de connexion")
    return
  }

  const success = client.send({
    type: 'start_vote',
    sessionCode: state.sessionCode,
    colors: Array.from(state.selectedColors),
    multipleChoice: state.multipleChoice,
    labels: state.colorLabels
  })

  if (!success && btn) {
    btn.disabled = false
    btn.innerHTML = `${icons.rocket(' class="icon icon-md"')} Lancer le vote`
    showError("Erreur de connexion")
  }
}

/**
 * Close the vote
 * @param {Object} client - WebSocket client instance
 */
export function closeVote(client) {
  const btn = document.getElementById('closeVote')
  if (btn) {
    btn.disabled = true
    btn.innerHTML = `${icons.loader(' class="icon icon-md spin"')} Fermeture...`
  }

  if (!client) {
    if (btn) {
      btn.disabled = false
      btn.innerHTML = `${icons.stop(' class="icon icon-md"')} Fermer le vote`
    }
    showError("Erreur de connexion")
    return
  }

  const success = client.send({
    type: 'close_vote',
    sessionCode: state.sessionCode
  })

  if (!success && btn) {
    btn.disabled = false
    btn.innerHTML = `${icons.stop(' class="icon icon-md"')} Fermer le vote`
    showError("Erreur de connexion")
  }
}

/**
 * Reset the vote for a new round
 * @param {Object} client - WebSocket client instance
 */
export function resetVote(client) {
  const btn = document.getElementById('newVote')
  if (btn) {
    btn.disabled = true
    btn.innerHTML = `${icons.loader(' class="icon icon-md spin"')} Réinitialisation...`
  }

  if (!client) {
    if (btn) {
      btn.disabled = false
      btn.innerHTML = `${icons.refresh(' class="icon icon-md"')} Nouveau vote`
    }
    showError("Erreur de connexion")
    return
  }

  const success = client.send({
    type: 'reset_vote',
    sessionCode: state.sessionCode,
    colors: Array.from(state.selectedColors),
    multipleChoice: state.multipleChoice
  })

  if (!success && btn) {
    btn.disabled = false
    btn.innerHTML = `${icons.refresh(' class="icon icon-md"')} Nouveau vote`
    showError("Erreur de connexion")
  }
}

/**
 * Join a session (create or join existing)
 * @param {string|null} code - Session code, or null to create new
 * @param {Function} updateLandingPageLoadingState
 * @param {Function} initClient
 */
export function joinSession(code, updateLandingPageLoadingState, initClient) {
  state.sessionCode = code || ""
  state.connecting = true

  if (code) {
    sessionStorage.setItem('vote_session_code', code)
  }

  updateLandingPageLoadingState(true)
  initClient()
}
