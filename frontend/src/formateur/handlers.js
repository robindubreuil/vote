import { showError } from '../../../shared/ui.js'
import { COLORS } from '../../../shared/colors.js'
import { state } from './state.js'
import { renderMainContent, attachConfigListeners } from './renderers.js'

export async function resetConfig() {
  state.selectedColors = new Set(COLORS.slice(0, 3).map(c => c.id))
  state.colorLabels = {}
  state.multipleChoice = false
  renderMainContent()

  const { getClient } = await import('./websocket.js')
  const client = getClient()
  if (client) {
    attachConfigListeners(client)
  }
}

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

export function joinSession(code, setLoadingFn, initClientFn) {
  state.sessionCode = code || ""
  state.connecting = true

  if (code) {
    sessionStorage.setItem('vote_session_code', code)
  }

  if (typeof setLoadingFn === 'function') {
    setLoadingFn(true)
  }

  if (typeof initClientFn === 'function') {
    initClientFn()
  }
}
