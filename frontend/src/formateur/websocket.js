import { VoteClient } from '@shared/websocket-client.js'
import { getWebSocketURL } from '@shared/config.js'
import { showError, hideError } from '@shared/ui.js'
import { validateSessionCode } from '@shared/validation.js'
import { createSessionPublisher } from '@shared/session-sync.js'
import { CONSTANTS } from '@shared/config.js'
import { state, resetTrainerState } from './state.js'
import {
  renderFullLayout,
  updateHeader,
  renderMainContent,
  renderLandingPage,
  updateLandingPageLoadingState,
  updateConnectionBanner,
  attachConfigListeners,
  attachHeaderListeners,
  attachVoteListeners,
  cleanupAllListeners,
  attachLandingListeners
} from './renderers.js'
import { startTimer, stopTimer, updateVoteResults } from './utils.js'
import { users } from '@shared/icons.js'
import { showToast } from '@shared/ui.js'
import { t } from '@shared/i18n.js'
import { applyLastConfigIfAvailable } from './handlers.js'
import { safeSessionSet, safeSessionRemove } from '@shared/utils/safe-storage.js'

const WS_URL = getWebSocketURL()

let client = null
let trainerId = null
let publisher = null

export function getClient() {
  return client
}

/**
 * Publish the current session state to any open "Aide à la connexion" tab.
 * No-op when no publisher exists (e.g. before the session is created).
 */
function publishState() {
  if (!publisher) return
  const leaderboard = state.competitive && state.stagiaires.length > 0
    ? state.stagiaires
        .filter((s) => s.connected)
        .map((s) => ({ name: s.name || 'Anonyme', score: (s.score || 0) + (s.gameScore || 0) }))
        .sort((a, b) => b.score - a.score)
        .slice(0, 3)
    : null
  publisher.publish({
    count: state.connectedCount,
    voteState: state.voteState,
    connected: state.connected,
    competitive: state.competitive,
    leaderboard
  })
}

export function initClient() {
  if (client) {
    client.close()
  }

  client = new VoteClient(WS_URL, {
    onStatusChange: (connected) => {
      const wasConnected = state.connected
      state.connected = connected
      if (connected) {
        // Detect reconnects (was previously connected, dropped, now back).
        // Avoid firing on the very first successful connection.
        if (state.everConnected && !wasConnected) {
          showToast(t.formateur.reconnected)
        }
        state.everConnected = true
      }
      updateHeader(client)
      updateConnectionBanner()
      publishState()
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
  if (publisher) {
    publisher.close()
    publisher = null
  }
}

function handleMessage(msg) {
  const app = document.getElementById('app')

  switch (msg.type) {
    case 'session_created':
      state.connecting = false
      if (msg.sessionCode) {
        state.sessionCode = msg.sessionCode
        safeSessionSet('vote_session_code', msg.sessionCode)

        if (!document.getElementById('app-content')) {
          renderFullLayout(app)
        }

        // Start publishing state for any "Aide à la connexion" tab as soon as
        // the session is alive.
        if (!publisher) {
          publisher = createSessionPublisher(msg.sessionCode)
        }
      }
      if (msg.trainerId) {
        trainerId = msg.trainerId
        safeSessionSet('vote_trainer_id', msg.trainerId)
      }
      updateHeader(client)
      attachListeners()
      publishState()

      // Restore the trainer's last-used config (colors, labels, multipleChoice)
      // on the first session of a fresh page lifecycle. No-op on subsequent
      // sessions or if the user has never started a vote.
      if (state.voteState === 'idle') {
        applyLastConfigIfAvailable()
      }
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
          attachListeners()
        }
      } else {
        updateVoteResults()
      }
      publishState()
      break

    case 'vote_started':
      state.voteState = 'active'
      state.voteStartTime = msg.voteElapsed != null
        ? Date.now() - msg.voteElapsed * 1000
        : Date.now()
      if (msg.colors) state.selectedColors = new Set(msg.colors)
      if (msg.multipleChoice !== undefined) state.multipleChoice = msg.multipleChoice
      if (msg.labels) state.colorLabels = msg.labels
      if (msg.competitive !== undefined) state.competitive = msg.competitive
      if (msg.allowBlank !== undefined) state.allowBlank = msg.allowBlank
      if (msg.gameEnabled !== undefined) state.gameEnabled = msg.gameEnabled
      state.revealed = false
      state.correctColors = new Set()

      renderMainContent()
      attachListeners()
      startTimer()
      publishState()
      break

    case 'vote_received':
      break

    case 'vote_closed':
      state.voteState = 'closed'
      stopTimer()
      renderMainContent()
      attachListeners()
      publishState()
      break

    case 'answers_revealed':
      state.revealed = true
      if (msg.correctColors) state.correctColors = new Set(msg.correctColors)
      if (msg.scores) state.scoreboard = msg.scores
      renderMainContent()
      attachListeners()
      publishState()
      break

    case 'vote_reset':
      state.voteState = 'idle'
      stopTimer()
      if (msg.competitive !== undefined) state.competitive = msg.competitive
      if (msg.allowBlank !== undefined) state.allowBlank = msg.allowBlank
      if (msg.gameEnabled !== undefined) state.gameEnabled = msg.gameEnabled
      state.revealed = false
      state.correctColors = new Set()
      renderMainContent()
      attachListeners()
      publishState()
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
      publishState()
      break

    case 'error':
      console.error('Backend error:', msg.message)
      state.connecting = false

      if (msg.message === 'Session introuvable') {
        safeSessionRemove('vote_session_code')
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
    stopTimer()
    safeSessionRemove('vote_session_code')
    safeSessionRemove('vote_trainer_id')
    resetTrainerState()
    cleanupAllListeners()
    renderLandingPage(document.getElementById('app'))
    attachLandingListenersWithHandlers()
  })
}

export function attachLandingListenersWithHandlers() {
  const createSession = () => {
    import('./handlers.js')
      .then(({ joinSession }) => {
        joinSession(null, updateLandingPageLoadingState, initClient)
      })
      .catch(() => showError('Erreur de chargement'))
  }

  const joinSessionFn = (code) => {
    const normalized = CONSTANTS.SESSION_CODE_NORMALIZE(code)
    const error = validateSessionCode(normalized)
    if (error) {
      const joinInput = document.getElementById('joinSessionInput')
      if (joinInput) joinInput.classList.add('error')
      showError(error)
      return
    }
    const joinInput = document.getElementById('joinSessionInput')
    if (joinInput) joinInput.classList.remove('error')
    import('./handlers.js')
      .then(({ joinSession }) => {
        joinSession(normalized, updateLandingPageLoadingState, initClient)
      })
      .catch(() => showError('Erreur de chargement'))
  }

  attachLandingListeners(joinSessionFn, createSession)

  const joinInput = document.getElementById('joinSessionInput')
  if (joinInput) {
    joinInput.addEventListener('input', () => {
      joinInput.classList.remove('error')
      hideError()
    })
  }
}
