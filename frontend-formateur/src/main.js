import './style.css'
import { icons } from '../../shared/icons.js'
import { COLORS, escapeHtml } from '../../shared/colors.js'
import { VoteClient } from '../../shared/websocket-client.js'
import { renderFooterHTML, renderSessionCodeButton, showError, hideError } from '../../shared/ui.js'
import { getWebSocketURL } from '../../shared/config.js'
import { validateSessionCode } from '../../shared/validation.js'

// Configuration de l'API WebSocket
const WS_URL = getWebSocketURL()

/**
 * @typedef {Object} Stagiaire
 * @property {string} id
 * @property {string} name
 * @property {boolean} connected
 * @property {string[]} [vote]
 */

// État de l'application
/**
 * @type {Object}
 * @property {string|null} sessionCode
 * @property {boolean} connected
 * @property {'idle'|'active'|'closed'} voteState
 * @property {Set<string>} selectedColors
 * @property {Object.<string, string>} colorLabels
 * @property {boolean} multipleChoice
 * @property {number} connectedCount
 * @property {Stagiaire[]} stagiaires
 * @property {number|null} voteStartTime
 * @property {number|null} timerInterval
 */
const state = {
  sessionCode: null,
  connected: false,
  connecting: false,
  voteState: 'idle', // idle, active, closed
  selectedColors: new Set(COLORS.slice(0, 3).map(c => c.id)), // Par défaut: 3 premières couleurs
  colorLabels: {}, // Custom labels for colors { colorId: "Custom Label" }
  multipleChoice: false,
  connectedCount: 0,
  stagiaires: [], // [{ id, name, connected, vote: [] }] Tous les stagiaires
  voteStartTime: null,
  timerInterval: null
}

// Éléments DOM
let app = null
let client = null
let trainerId = null

// Initialisation
function init() {
  app = document.getElementById('app')

  // Récupérer l'ID de session formateur (généré par le serveur)
  trainerId = sessionStorage.getItem('vote_trainer_id')

  // Créer ou récupérer le code session
  let savedSessionCode = sessionStorage.getItem('vote_session_code')
  
  // Check URL params for session code (override saved code if present)
  const urlParams = new URLSearchParams(window.location.search)
  const urlSession = urlParams.get('session')
  if (urlSession && validateSessionCode(urlSession) === null) {
      savedSessionCode = urlSession
  }

  if (savedSessionCode) {
    state.sessionCode = savedSessionCode
    // Initialiser l'interface de base
    renderFullLayout()
    // Connecter le WebSocket
    initClient()
  } else {
    renderLandingPage()
  }
}

// Initialisation WebSocket
function initClient() {
  if (client) {
    client.close()
  }

  client = new VoteClient(WS_URL, {
    onStatusChange: (connected) => {
      state.connected = connected
      updateHeader() // Update session code connection status
      // Re-render main content to update button states (disabled/enabled)
      renderMainContent()
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

// Gérer les messages reçus
function handleMessage(msg) {
  switch (msg.type) {
    case 'session_created':
      state.connecting = false
      // Use the server-generated session code
      if (msg.sessionCode) {
        state.sessionCode = msg.sessionCode
        sessionStorage.setItem('vote_session_code', msg.sessionCode)
        
        // Transition to full layout if we were on landing page
        if (!document.getElementById('app-content')) {
            renderFullLayout()
        }
      }
      // Store the server-generated trainerId
      if (msg.trainerId) {
        trainerId = msg.trainerId
        sessionStorage.setItem('vote_trainer_id', msg.trainerId)
      }
      updateHeader()
      break

    case 'connected_count':
      // Mise à jour de la liste des stagiaires
      state.connectedCount = msg.count || 0
      if (msg.stagiaires) {
        state.stagiaires = msg.stagiaires
      }
      updateHeader()
      
      if (state.voteState === 'idle') {
        // Mise à jour directe du compteur dans la vue config
        const configInfo = document.querySelector('.config-info')
        if (configInfo) {
           const s = state.connectedCount > 1 ? 's' : ''
           configInfo.innerHTML = `${icons.users(' class="icon icon-sm"')} ${state.connectedCount} stagiaire${s} connecté${s}`
        } else if (document.getElementById('app-content')) {
            // Si pas d'élément trouvé mais qu'on est dans l'app, re-render
            renderMainContent()
        }
      } else {
        updateVoteResults()
      }
      break

    case 'vote_started':
      state.voteState = 'active'
      // Use the server's vote start time if available (for reconnection), otherwise use now
      state.voteStartTime = msg.voteStartTime ? msg.voteStartTime * 1000 : Date.now()
      // Mise à jour de la config si on a rejoint une session existante
      if (msg.colors) state.selectedColors = new Set(msg.colors)
      if (msg.multipleChoice !== undefined) state.multipleChoice = msg.multipleChoice
      if (msg.labels) state.colorLabels = msg.labels

      startTimer()
      renderMainContent() // Basculer vers l'interface de vote
      break

    case 'vote_received':
      // Nouveau vote d'un stagiaire - les stats sont mises à jour via connected_count
      // Pas d'action requise car connected_count contient l'état complet
      break

    case 'vote_closed':
      state.voteState = 'closed'
      stopTimer()
      renderMainContent() // Mettre à jour les boutons d'action
      break

    case 'vote_reset':
      state.voteState = 'idle'
      stopTimer()
      renderMainContent() // Revenir à la configuration
      break

    case 'config_updated':
      // La configuration a été mise à jour (par le formateur ou par le backend)
      if (msg.selectedColors) {
        state.selectedColors = new Set(msg.selectedColors)
      }
      if (msg.multipleChoice !== undefined) {
        state.multipleChoice = msg.multipleChoice
      }
      if (state.voteState === 'idle') {
          renderMainContent()
      }
      break
    
    case 'error':
        // Si erreur lors de la connexion
        console.error("Erreur backend:", msg.message)
        state.connecting = false
        
        if (msg.message === "Session introuvable") {
            sessionStorage.removeItem('vote_session_code')
            state.sessionCode = null
            renderLandingPage()
            // Petit délai pour que le DOM soit prêt avant d'afficher l'erreur
            setTimeout(() => {
                showError(msg.message)
                document.getElementById('joinSessionInput')?.focus()
            }, 50)
        } else {
            showError(msg.message)
            // Reset landing page buttons if still there
            updateLandingPageLoadingState(false)
            // Restore focus to input if it exists
            document.getElementById('joinSessionInput')?.focus()
        }
        break

    default:
      console.debug('Type de message inconnu:', msg.type)
  }
}

// Démarrer le timer
function startTimer() {
  stopTimer()
  state.timerInterval = setInterval(() => {
    const timerEl = document.querySelector('.vote-timer')
    if (timerEl && state.voteStartTime) {
      const elapsed = Math.floor((Date.now() - state.voteStartTime) / 1000)
      const mins = Math.floor(elapsed / 60).toString().padStart(2, '0')
      const secs = (elapsed % 60).toString().padStart(2, '0')
      timerEl.innerHTML = `${icons.timer(' class="icon icon-sm"')} ${mins}:${secs}`
    }
  }, 1000)
}

// Arrêter le timer
function stopTimer() {
  if (state.timerInterval) {
    clearInterval(state.timerInterval)
    state.timerInterval = null
  }
}

// Page d'accueil (Landing Page)
function renderLandingPage() {
  app.innerHTML = `
    <div class="container">
      <div class="landing-card card">
        <div class="landing-icon">${icons.vote(' class="icon"')}</div>
        <h1 class="landing-title">Vote Coloré</h1>
        <p class="landing-subtitle">Interface Formateur</p>
        
        <div class="landing-actions">
          <button id="createSessionBtn" class="btn btn-primary btn-large">
            ${icons.plus(' class="icon icon-md"')} Créer une nouvelle session
          </button>
          
          <div class="landing-divider">OU</div>
          
          <div class="input-group">
            <input type="text" id="joinSessionInput" class="input-text" placeholder="Code session (ex: 1234)" maxlength="4" pattern="[0-9]{4}" inputmode="numeric">
            <button id="joinSessionBtn" class="btn btn-secondary">
              Rejoindre
            </button>
          </div>
          <div class="error-message" role="alert"></div>
        </div>
      </div>
    </div>
    ${renderFooterHTML()}
  `
  
  document.getElementById('createSessionBtn').addEventListener('click', () => {
    // Let server generate the session code
    joinSession(null)
  })

  const joinBtn = document.getElementById('joinSessionBtn')
  const joinInput = document.getElementById('joinSessionInput')
  
  const handleJoin = () => {
    const code = joinInput.value.trim()
    
    const error = validateSessionCode(code)
    if (error) {
      joinInput.classList.add('error')
      showError(error)
      return
    }

    joinInput.classList.remove('error')
    joinSession(code)
  }

  joinBtn.addEventListener('click', handleJoin)
  joinInput.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') handleJoin()
  })
  
  joinInput.addEventListener('input', () => {
     joinInput.classList.remove('error')
     hideError()
  })

  if (state.connecting) {
      updateLandingPageLoadingState(true)
  }
}

function updateLandingPageLoadingState(isLoading) {
    const createBtn = document.getElementById('createSessionBtn')
    const joinBtn = document.getElementById('joinSessionBtn')
    const joinInput = document.getElementById('joinSessionInput')
    
    if (!createBtn || !joinBtn) return

    if (isLoading) {
        createBtn.disabled = true
        joinBtn.disabled = true
        joinInput.disabled = true
        createBtn.innerHTML = `${icons.loader(' class="icon icon-md spin"')} Connexion...`
    } else {
        createBtn.disabled = false
        joinBtn.disabled = false
        joinInput.disabled = false
        createBtn.innerHTML = `${icons.plus(' class="icon icon-md"')} Créer une nouvelle session`
    }
}

function joinSession(code) {
  // If creating new session (code is null), let server generate
  // If joining existing session, use the provided code
  state.sessionCode = code || ""  // Empty string signals server to generate
  state.connecting = true

  if (code) {
    sessionStorage.setItem('vote_session_code', code)
  }
  
  updateLandingPageLoadingState(true)
  initClient()
}

// Structure globale App
function renderFullLayout() {
  app.innerHTML = `
    <div class="container">
      <header class="header" id="app-header"></header>
      <main id="app-content"></main>
    </div>
    ${renderFooterHTML()}
  `
  updateHeader()
  renderMainContent()
}

// Rendu du Header uniquement
function updateHeader() {
  const header = document.getElementById('app-header')
  if (!header) return

  const isConnected = client ? client.isConnected() : state.connected

  header.innerHTML = `
    <h1>${icons.vote(' class="icon icon-md"')} Vote Coloré - Formateur</h1>
    <div class="header-right">
      ${renderSessionCodeButton(state.sessionCode, isConnected)}
    </div>
  `
  attachHeaderListeners()
}

function attachHeaderListeners() {
  const leaveSessionBtn = document.getElementById('leaveSessionBtn')
  if (leaveSessionBtn) {
    leaveSessionBtn.addEventListener('click', () => {
      if (confirm('Voulez-vous quitter cette session ?')) {
        sessionStorage.removeItem('vote_session_code')
        if (client) {
            client.close()
            client = null
        }
        // Retour à la landing page
        state.sessionCode = null
        state.connectedCount = 0
        state.voteState = 'idle'
        renderLandingPage()
      }
    })
  }
}

// Rendu du contenu principal (Config ou Vote)
function renderMainContent() {
  const main = document.getElementById('app-content')
  if (!main) return

  if (state.voteState === 'idle') {
    main.innerHTML = renderConfigHTML()
    attachConfigListeners()
  } else {
    main.innerHTML = renderVoteHTML()
    attachVoteListeners()
    // No need to call updateVoteResults separately, renderVoteHTML now includes the data
  }
}

// Rendu de la configuration
function renderConfigHTML() {
  const isConnected = state.connected
  return `
    <div class="card">
      <h2 class="card-title">Configuration du prochain vote</h2>
      <div class="config-info">${icons.users(' class="icon icon-sm"')} ${state.connectedCount} stagiaire${state.connectedCount > 1 ? 's' : ''} connecté${state.connectedCount > 1 ? 's' : ''}</div>

      <div class="config-section">
        <div>
          <div class="stats-header">Couleurs disponibles</div>
          <div class="colors-grid">
            ${COLORS.map(color => {
              const customLabel = state.colorLabels[color.id] || ''
              return `
              <label class="color-checkbox ${state.selectedColors.has(color.id) ? 'selected' : ''}">
                <input
                  type="checkbox"
                  value="${color.id}"
                  ${state.selectedColors.has(color.id) ? 'checked' : ''}
                />
                <span class="color-swatch" style="background-color: ${color.color}"></span>
                <div class="color-label-wrapper">
                  <input
                    type="text"
                    class="color-label-input"
                    data-color-id="${color.id}"
                    value="${escapeHtml(customLabel)}"
                    placeholder="${escapeHtml(color.name)}"
                    maxlength="6"
                  />
                </div>
              </label>
            `}).join('')}
          </div>
        </div>

        <label class="multiple-choice-toggle">
          <span class="toggle-switch ${state.multipleChoice ? 'active' : ''}" data-action="toggle-multiple"></span>
          <span>Choix multiple (autoriser plusieurs couleurs)</span>
        </label>
      </div>

      <div class="button-row">
        <button class="btn btn-secondary" id="resetConfig" ${!isConnected ? 'disabled' : ''}>
          ${icons.refresh(' class="icon icon-md"')} Réinitialiser
        </button>
        <button class="btn btn-primary btn-large" id="startVote" ${state.selectedColors.size < 2 || !isConnected ? 'disabled' : ''}>
          ${icons.rocket(' class="icon icon-md"')} Lancer le vote
        </button>
      </div>
    </div>
  `
}

function attachConfigListeners() {
  // Checkboxes des couleurs
  document.querySelectorAll('.color-checkbox input[type="checkbox"]').forEach(checkbox => {
    checkbox.addEventListener('change', (e) => {
      const colorId = e.target.value
      if (e.target.checked) {
        state.selectedColors.add(colorId)
      } else {
        state.selectedColors.delete(colorId)
      }

      // Mettre à jour la classe selected
      const parent = e.target.closest('.color-checkbox')
      parent.classList.toggle('selected', e.target.checked)

      // Mettre à jour l'état du bouton
      const startBtn = document.getElementById('startVote')
      if (startBtn) {
        startBtn.disabled = state.selectedColors.size < 2
      }
    })
  })

  // Label inputs for custom color names
  document.querySelectorAll('.color-label-input').forEach(input => {
    input.addEventListener('input', (e) => {
      const colorId = e.target.dataset.colorId
      const value = e.target.value.trim()
      if (value) {
        state.colorLabels[colorId] = value
      } else {
        delete state.colorLabels[colorId]
      }
    })
  })

  // Toggle choix multiple
  const toggleMultiple = document.querySelector('.multiple-choice-toggle')
  if (toggleMultiple) {
    toggleMultiple.addEventListener('click', (e) => {
      // Avoid double trigger if clicking the inner switch
      if (e.target.dataset.action === 'toggle-multiple') return
      
      state.multipleChoice = !state.multipleChoice
      const switchEl = toggleMultiple.querySelector('.toggle-switch')
      switchEl.classList.toggle('active', state.multipleChoice)
    })
  }

  // Bouton lancer le vote
  const startBtn = document.getElementById('startVote')
  if (startBtn) {
    startBtn.addEventListener('click', startVote)
  }

  // Bouton réinitialiser la config
  const resetBtn = document.getElementById('resetConfig')
  if (resetBtn) {
    resetBtn.addEventListener('click', resetConfig)
  }
}

// Rendu du vote en cours avec données pré-remplies
function renderVoteHTML() {
  const activeColors = COLORS.filter(c => state.selectedColors.has(c.id))
  
  // Calculate initial stats
  const voteCount = state.stagiaires.filter(s => s.vote && s.vote.length > 0).length
  const colorCounts = getColorCounts()
  const maxCount = Math.max(...Object.values(colorCounts), 1)
  const isConnected = state.connected

  return `
    <div class="card">
      <div class="vote-header">
        <div class="vote-timer">${icons.timer(' class="icon icon-sm"')} 00:00</div>
        <div class="vote-stats">
          <span class="vote-count" aria-live="polite">${icons.chart(' class="icon icon-sm"')} ${voteCount} / ${state.connectedCount} votes</span>
        </div>
      </div>

      <div class="stats-grid stats-grid-3cols">
        <div>
          <div class="stats-header">Par couleur</div>
          <div class="color-bars">
            ${renderColorBarsHTML(activeColors, colorCounts, maxCount)}
          </div>
        </div>

        <div>
          <div class="stats-header">Par combinaison</div>
          <div class="combinations-list custom-scrollbar">
             ${renderCombinationsHTML()}
          </div>
        </div>

        <div>
          <div class="stats-header">Par stagiaire</div>
          <div class="stagiaires-votes-list custom-scrollbar">
             ${renderStagiairesVotesHTML()}
          </div>
        </div>
      </div>

      <div class="button-row">
        ${state.voteState === 'active' ? `
          <button class="btn btn-danger" id="closeVote" ${!isConnected ? 'disabled' : ''}>${icons.stop(' class="icon icon-md"')} Fermer le vote</button>
        ` : `
          <button class="btn btn-success" id="newVote" ${!isConnected ? 'disabled' : ''}>${icons.refresh(' class="icon icon-md"')} Nouveau vote</button>
        `}
      </div>
    </div>
  `
}

function attachVoteListeners() {
   // Bouton fermer le vote
  const closeBtn = document.getElementById('closeVote')
  if (closeBtn) {
    closeBtn.addEventListener('click', closeVote)
  }

  // Bouton nouveau vote
  const newVoteBtn = document.getElementById('newVote')
  if (newVoteBtn) {
    newVoteBtn.addEventListener('click', resetVote)
  }
}

// Mettre à jour uniquement les résultats du vote
function updateVoteResults() {
  // Count votes from stagiaires
  const voteCount = state.stagiaires.filter(s => s.vote && s.vote.length > 0).length

  // Update vote count
  const voteCountEl = document.querySelector('.vote-count')
  if (voteCountEl) {
    voteCountEl.innerHTML = `${icons.chart(' class="icon icon-sm"')} ${voteCount} / ${state.connectedCount} votes`
  }

  const activeColors = COLORS.filter(c => state.selectedColors.has(c.id))
  const colorCounts = getColorCounts()
  const maxCount = Math.max(...Object.values(colorCounts), 1)

  // Optimisation: Mise à jour granulaire des barres
  updateColorBars(activeColors, colorCounts, maxCount)

  // Mise à jour des combinaisons
  // Note: Pour les combinaisons et la liste des votes, on fait un re-render partiel
  // car la structure change souvent (ordre, nombre d'items)
  const combinationsList = document.querySelector('.combinations-list')
  if (combinationsList) {
    combinationsList.innerHTML = renderCombinationsHTML()
  }

  // Mise à jour de la liste des votes par stagiaire
  // Ici on pourrait aussi optimiser mais le tri changeant rend le diff complexe
  const stagiairesVotesList = document.querySelector('.stagiaires-votes-list')
  if (stagiairesVotesList) {
    stagiairesVotesList.innerHTML = renderStagiairesVotesHTML()
  }
}

// Mise à jour optimisée des barres de couleur
function updateColorBars(activeColors, colorCounts, maxCount) {
  const container = document.querySelector('.color-bars')
  if (!container) return

  // Trier les couleurs par popularité
  const sortedColors = [...activeColors].sort((a, b) => (colorCounts[b.id] || 0) - (colorCounts[a.id] || 0))

  // Créer un Map des éléments existants pour lookup rapide
  const existingRows = new Map()
  Array.from(container.children).forEach(row => {
    const colorId = row.getAttribute('data-color')
    if (colorId) existingRows.set(colorId, row)
  })

  // Reconstruire le HTML avec le bon ordre
  const fragment = document.createDocumentFragment()
  sortedColors.forEach(color => {
    const count = colorCounts[color.id] || 0
    const percent = (count / maxCount) * 100

    let row = existingRows.get(color.id)

    if (row) {
      // Mise à jour de la ligne existante
      const countEl = row.querySelector('.color-bar-count')
      const fillEl = row.querySelector('.color-bar-fill')

      if (countEl && countEl.textContent !== count.toString()) {
        countEl.textContent = count
      }

      if (fillEl) {
        fillEl.style.width = `${percent}%`
        if (count === 0) {
          fillEl.classList.add('empty')
        } else {
          fillEl.classList.remove('empty')
        }
      }
      
      // Re-append to ensure correct order
      fragment.appendChild(row)
    } else {
      // Créer une nouvelle ligne
      row = document.createElement('div')
      row.className = 'color-bar-row'
      row.setAttribute('data-color', color.id)
      row.innerHTML = `
        <div class="color-bar-label">
          <span class="color-bar-swatch" style="background-color: ${color.color}"></span>
          <span class="color-bar-name">${escapeHtml(state.colorLabels[color.id] || color.name)}</span>
        </div>
        <div class="color-bar-track">
          <span class="color-bar-count">${count}</span>
          <div class="color-bar-fill ${count === 0 ? 'empty' : ''}" style="width: ${percent}%; background-color: ${color.color}"></div>
        </div>
      `
      fragment.appendChild(row)
    }
  })

  // Only update if order changed or new elements added, but for simplicity here we just replace
  // because we are re-appending existing nodes, they are moved, not destroyed
  container.innerHTML = ''
  container.appendChild(fragment)
}

// Rendu des barres de couleur
function renderColorBarsHTML(activeColors, colorCounts, maxCount) {
  // Trier les couleurs par nombre de votes décroissant
  const sortedColors = [...activeColors].sort((a, b) => (colorCounts[b.id] || 0) - (colorCounts[a.id] || 0))

  return sortedColors.map(color => {
    const count = colorCounts[color.id] || 0
    const percent = (count / maxCount) * 100
    const displayName = state.colorLabels[color.id] || color.name
    return `
      <div class="color-bar-row" data-color="${color.id}">
        <div class="color-bar-label">
          <span class="color-bar-swatch" style="background-color: ${color.color}"></span>
          <span class="color-bar-name">${escapeHtml(displayName)}</span>
        </div>
        <div class="color-bar-track">
          <span class="color-bar-count">${count}</span>
          <div class="color-bar-fill ${count === 0 ? 'empty' : ''}" style="width: ${percent}%; background-color: ${color.color}"></div>
        </div>
      </div>
    `
  }).join('')
}

// Rendu des combinaisons
function renderCombinationsHTML() {
  const combinations = getCombinations()

  if (combinations.length === 0) {
    return `
      <div class="empty-state">
        <div class="empty-icon">${icons.chart(' class="icon icon-xl"')}</div>
        <div>Aucun vote pour le moment</div>
      </div>
    `
  }

  return combinations.map(combo => {
    return `
      <div class="combo-item">
        <div class="combo-colors">
          ${combo.colors.map(colorId => {
            const color = COLORS.find(c => c.id === colorId)
            return `<span class="combo-swatch" style="background-color: ${color?.color || '#666'}"></span>`
          }).join('')}
        </div>
        <div class="combo-count">${combo.count}</div>
      </div>
    `
  }).join('')
}

// Helper pour le tri des stagiaires
function sortStagiaires(stagiaires) {
  // Calculer la popularité de chaque combinaison (parmi ceux qui ont voté)
  const comboPopularity = new Map()
  stagiaires.forEach(s => {
    if (s.vote && s.vote.length > 0) {
      const key = s.vote.slice().sort().join('+')
      comboPopularity.set(key, (comboPopularity.get(key) || 0) + 1)
    }
  })

  return [...stagiaires].sort((a, b) => {
    const aHasVoted = a.vote && a.vote.length > 0
    const bHasVoted = b.vote && b.vote.length > 0

    // Non-votants en premier
    if (aHasVoted !== bHasVoted) {
      return aHasVoted ? 1 : -1
    }

    // Si les deux ont voté, trier par popularité de la combinaison
    if (aHasVoted && bHasVoted) {
      const keyA = a.vote.slice().sort().join('+')
      const keyB = b.vote.slice().sort().join('+')
      const popularityA = comboPopularity.get(keyA) || 0
      const popularityB = comboPopularity.get(keyB) || 0

      if (popularityB !== popularityA) {
        return popularityB - popularityA
      }
    }

    // Même statut : trier par nom
    const nameA = (a.name || 'Anonyme').toLowerCase()
    const nameB = (b.name || 'Anonyme').toLowerCase()
    return nameA.localeCompare(nameB)
  })
}

// Rendu des votes par stagiaire
function renderStagiairesVotesHTML() {
  if (state.stagiaires.length === 0) {
    return `
      <div class="empty-state">
        <div class="empty-icon">${icons.users(' class="icon icon-xl"')}</div>
        <div>Aucun stagiaire connecté</div>
      </div>
    `
  }

  const sorted = sortStagiaires(state.stagiaires)

  return sorted.map(s => {
    const displayName = s.name || 'Anonyme'
    const hasVoted = s.vote && s.vote.length > 0
    const isConnected = s.connected

    // Online indicator dot (always visible, green if connected, gray if disconnected)
    const onlineDot = `<span class="online-dot ${isConnected ? 'connected' : 'disconnected'}" title="${isConnected ? 'En ligne' : 'Hors ligne'}"></span>`

    if (!hasVoted) {
      // Non-votant : label "En attente"
      return `
        <div class="stagiaire-vote-item waiting">
          <span class="stagiaire-vote-name">${onlineDot}<span class="name-text" title="${escapeHtml(displayName)}">${escapeHtml(displayName)}</span></span>
          <span class="stagiaire-vote-waiting">En attente</span>
        </div>
      `
    }

    // votant : afficher les couleurs
    const colorsHTML = s.vote.map(colorId => {
      const color = COLORS.find(c => c.id === colorId)
      return `<span class="stagiaire-vote-swatch" style="background-color: ${color?.color || '#666'}" title="${color?.name || colorId}"></span>`
    }).join('')

    return `
      <div class="stagiaire-vote-item">
        <span class="stagiaire-vote-name">${onlineDot}<span class="name-text" title="${escapeHtml(displayName)}">${escapeHtml(displayName)}</span></span>
        <div class="stagiaire-vote-colors">${colorsHTML}</div>
      </div>
    `
  }).join('')
}

// Calculer les combinaisons de votes
function getCombinations() {
  const comboMap = new Map()

  state.stagiaires.forEach(s => {
    if (s.vote && s.vote.length > 0) {
      const key = s.vote.slice().sort().join('+')
      comboMap.set(key, (comboMap.get(key) || 0) + 1)
    }
  })

  return Array.from(comboMap.entries())
    .map(([key, count]) => ({
      colors: key ? key.split('+') : [],
      count
    }))
    .sort((a, b) => b.count - a.count)
}

// Calculer les votes par couleur
function getColorCounts() {
  const counts = {}
  state.stagiaires.forEach(s => {
    if (s.vote) {
      s.vote.forEach(colorId => {
        counts[colorId] = (counts[colorId] || 0) + 1
      })
    }
  })
  return counts
}

// Actions
function resetConfig() {
  // Reset to defaults: 3 first colors, no labels, single choice
  state.selectedColors = new Set(COLORS.slice(0, 3).map(c => c.id))
  state.colorLabels = {}
  state.multipleChoice = false
  renderMainContent() // Re-render the config UI
}

function startVote() {
  const btn = document.getElementById('startVote')
  if (btn) {
    btn.disabled = true
    btn.innerHTML = `${icons.loader(' class="icon icon-md spin"')} Lancement...`
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

function closeVote() {
  const btn = document.getElementById('closeVote')
  if (btn) {
    btn.disabled = true
    btn.innerHTML = `${icons.loader(' class="icon icon-md spin"')} Fermeture...`
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

function resetVote() {
  const btn = document.getElementById('newVote')
  if (btn) {
    btn.disabled = true
    btn.innerHTML = `${icons.loader(' class="icon icon-md spin"')} Réinitialisation...`
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

// Démarrage
init()