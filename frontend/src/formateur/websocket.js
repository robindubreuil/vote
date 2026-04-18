import { VoteClient } from '../../../shared/websocket-client.js'
import { getWebSocketURL } from '../../../shared/config.js'
import { showError } from '../../../shared/ui.js'
import { state } from './state.js'
import { renderFullLayout, updateHeader, renderMainContent, renderLandingPage, updateLandingPageLoadingState, attachConfigListeners, attachHeaderListeners, attachVoteListeners, cleanupAllListeners, attachLandingListeners } from './renderers.js'
import { startTimer, stopTimer, updateVoteResults } from './utils.js'
import { users } from '../../../shared/icons.js'

const WS_URL = getWebSocketURL()

let client = null
let trainerId = null

export function getClient() {
  return client
}

export function initClient() {
  if (client) {
    client.close()
  }

  client = new VoteClient(WS_URL, {
    onStatusChange: (connected) => {
      state.connected = connected
      updateHeader(client)
      renderMainContent()
      attachListeners()
    },
    onOpen: () => {
      client.send({
        type: 'trainer_join',
        sessionCode: state.sessionCode,
        trainerId: trainerId || undefined
      })
    },
    onMessage: (msg) => {
      handleMessage(msg)
    }
  })

  client.connect()
}

export function closeClient() {
  if (client) {
    client.close()
    client = null
  }
}

function handleMessage(msg) {
  const app = document.getElementById('app')

  switch (msg.type) {
    case 'session_created':
      state.connecting = false
      if (msg.sessionCode) {
        state.sessionCode = msg.sessionCode
        sessionStorage.setItem('vote_session_code', msg.sessionCode)

        if (!document.getElementById('app-content')) {
          renderFullLayout(app)
        }
      }
      if (msg.trainerId) {
        trainerId = msg.trainerId
        sessionStorage.setItem('vote_trainer_id', msg.trainerId)
      }
      updateHeader(client)
      attachListeners()
      break

    case 'connected_count':
      state.connectedCount = msg.count || 0
      if (msg.stagiaires) {
        state.stagiaires = msg.stagiaires
      }
      updateHeader(client)

      if (state.voteState === 'idle') {
        const configInfo = document.querySelector('.config-info')
        if (configInfo) {
          const s = state.connectedCount > 1 ? 's' : ''
          configInfo.innerHTML = `${users(' class="icon icon-sm"')} ${state.connectedCount} stagiaire${s} connecté${s}`
        } else if (document.getElementById('app-content')) {
          renderMainContent()
        }
      } else {
        updateVoteResults()
      }
      attachListeners()
      break

    case 'vote_started':
      state.voteState = 'active'
      state.voteStartTime = msg.voteStartTime ? msg.voteStartTime * 1000 : Date.now()
      if (msg.colors) state.selectedColors = new Set(msg.colors)
      if (msg.multipleChoice !== undefined) state.multipleChoice = msg.multipleChoice
      if (msg.labels) state.colorLabels = msg.labels

      renderMainContent()
      attachListeners()
      startTimer()
      break

    case 'vote_received':
      break

    case 'vote_closed':
      state.voteState = 'closed'
      stopTimer()
      renderMainContent()
      attachListeners()
      break

    case 'vote_reset':
      state.voteState = 'idle'
      stopTimer()
      renderMainContent()
      attachListeners()
      break

    case 'config_updated':
      if (msg.selectedColors && msg.selectedColors.length > 0) {
        state.selectedColors = new Set(msg.selectedColors)
      }
      if (msg.multipleChoice !== undefined) {
        state.multipleChoice = msg.multipleChoice
      }
      if (state.voteState === 'idle') {
        renderMainContent()
        attachListeners()
      }
      break

    case 'error':
      console.error("Backend error:", msg.message)
      state.connecting = false

      if (msg.message === "Session introuvable") {
        sessionStorage.removeItem('vote_session_code')
        state.sessionCode = null
        cleanupAllListeners()
        renderLandingPage(app)
        attachLandingListenersWithHandlers()
        setTimeout(() => {
          showError(msg.message)
          document.getElementById('joinSessionInput')?.focus()
        }, 50)
      } else {
        showError(msg.message)
        updateLandingPageLoadingState(false)
        document.getElementById('joinSessionInput')?.focus()
      }
      break

    default:
      console.debug('Unknown message type:', msg.type)
  }
}

function attachListeners() {
  if (!document.getElementById('app-content')) {
    return
  }

  cleanupAllListeners()

  if (state.voteState === 'idle') {
    attachConfigListeners(client)
  } else {
    attachVoteListeners(client)
  }

  attachHeaderListeners(client, () => {
    closeClient()
    sessionStorage.removeItem('vote_session_code')
    sessionStorage.removeItem('vote_trainer_id')
    state.sessionCode = null
    state.connectedCount = 0
    state.voteState = 'idle'
    state.connecting = false
    cleanupAllListeners()
    renderLandingPage(document.getElementById('app'))
    attachLandingListenersWithHandlers()
  })
}

function attachLandingListenersWithHandlers() {
  const createSession = () => {
    import('./handlers.js').then(({ joinSession }) => {
      joinSession(null, updateLandingPageLoadingState, initClient)
    }).catch(() => showError("Erreur de chargement"))
  }

  const joinSessionFn = (code) => {
    import('../../../shared/validation.js').then(({ validateSessionCode }) => {
      const error = validateSessionCode(code)
      if (error) {
        const joinInput = document.getElementById('joinSessionInput')
        if (joinInput) joinInput.classList.add('error')
        showError(error)
        return
      }
      const joinInput = document.getElementById('joinSessionInput')
      if (joinInput) joinInput.classList.remove('error')
      import('./handlers.js').then(({ joinSession }) => {
        joinSession(code, updateLandingPageLoadingState, initClient)
      }).catch(() => showError("Erreur de chargement"))
    }).catch(() => showError("Erreur de chargement"))
  }

  attachLandingListeners(joinSessionFn, createSession)

  const joinInput = document.getElementById('joinSessionInput')
  if (joinInput) {
    joinInput.addEventListener('input', () => {
      joinInput.classList.remove('error')
      showError(null)
    })
  }
}

export { handleMessage }
