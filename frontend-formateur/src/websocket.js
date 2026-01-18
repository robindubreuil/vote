import { VoteClient } from '../../shared/websocket-client.js'
import { showError } from '../../shared/ui.js'
import { state } from './state.js'
import { renderFullLayout, updateHeader, renderMainContent, renderLandingPage, updateLandingPageLoadingState } from './renderers.js'
import { startTimer, stopTimer, updateVoteResults } from './utils.js'

// WebSocket URL configuration
const WS_URL = import.meta.env.VITE_WS_URL || (location.protocol === 'https:' ? 'wss:' : 'ws:') + '//' + location.host + '/ws'

// Client instance
let client = null
let trainerId = null

/**
 * Get the client instance
 * @returns {VoteClient|null}
 */
export function getClient() {
  return client
}

/**
 * Initialize the WebSocket client
 */
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

/**
 * Handle incoming WebSocket messages
 * @param {Object} msg
 */
function handleMessage(msg) {
  const app = document.getElementById('app')

  switch (msg.type) {
    case 'session_created':
      state.connecting = false
      if (msg.sessionCode) {
        state.sessionCode = msg.sessionCode
        sessionStorage.setItem('vote_session_code', msg.sessionCode)

        // Transition to full layout if we were on landing page
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
          configInfo.innerHTML = `${state.connectedCount} stagiaire${s} connecté${s}`
        } else if (document.getElementById('app-content')) {
          renderMainContent()
          attachListeners()
        }
      } else {
        updateVoteResults()
      }
      break

    case 'vote_started':
      state.voteState = 'active'
      state.voteStartTime = msg.voteStartTime ? msg.voteStartTime * 1000 : Date.now()
      if (msg.colors) state.selectedColors = new Set(msg.colors)
      if (msg.multipleChoice !== undefined) state.multipleChoice = msg.multipleChoice
      if (msg.labels) state.colorLabels = msg.labels

      startTimer()
      renderMainContent()
      attachListeners()
      break

    case 'vote_received':
      // Stats updated via connected_count
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
      if (msg.selectedColors) {
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
        renderLandingPage(app)
        attachLandingListeners()
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

/**
 * Attach event listeners after render
 */
function attachListeners() {
  if (state.voteState === 'idle') {
    // Config page listeners
    import('./renderers.js').then(({ attachConfigListeners }) => {
      attachConfigListeners(client)
    })
  } else {
    // Vote page listeners
    import('./renderers.js').then(({ attachVoteListeners }) => {
      attachVoteListeners(client)
    })
  }

  // Header listeners
  import('./renderers.js').then(({ attachHeaderListeners }) => {
    attachHeaderListeners(client, renderLandingPage)
  })
}

/**
 * Attach landing page listeners
 */
function attachLandingListeners() {
  const createBtn = document.getElementById('createSessionBtn')
  const joinBtn = document.getElementById('joinSessionBtn')
  const joinInput = document.getElementById('joinSessionInput')

  if (createBtn) {
    createBtn.addEventListener('click', () => {
      import('./handlers.js').then(({ joinSession }) => {
        joinSession(null, updateLandingPageLoadingState, initClient)
      })
    })
  }

  if (joinBtn && joinInput) {
    const handleJoin = () => {
      const code = joinInput.value.trim()
      import('../../shared/validation.js').then(({ validateSessionCode }) => {
        const error = validateSessionCode(code)
        if (error) {
          joinInput.classList.add('error')
          showError(error)
          return
        }
        joinInput.classList.remove('error')
        import('./handlers.js').then(({ joinSession }) => {
          joinSession(code, updateLandingPageLoadingState, initClient)
        })
      })
    }

    joinBtn.addEventListener('click', handleJoin)
    joinInput.addEventListener('keydown', (e) => {
      if (e.key === 'Enter') handleJoin()
    })
    joinInput.addEventListener('input', () => {
      joinInput.classList.remove('error')
      import('../../shared/ui.js').then(({ hideError }) => {
        hideError()
      })
    })
  }

  if (state.connecting) {
    updateLandingPageLoadingState(true)
  }
}

// Export handleMessage for external use if needed
export { handleMessage }
