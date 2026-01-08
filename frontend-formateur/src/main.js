import './style.css'
import { icons } from '../../shared/icons.js'
import { VERSION } from '../../shared/version.js'

// Configuration des couleurs disponibles
const COLORS = [
  { id: 'rouge', name: 'Rouge', color: '#ef4444' },
  { id: 'vert', name: 'Vert', color: '#22c55e' },
  { id: 'bleu', name: 'Bleu', color: '#3b82f6' },
  { id: 'jaune', name: 'Jaune', color: '#eab308' },
  { id: 'orange', name: 'Orange', color: '#f97316' },
  { id: 'violet', name: 'Violet', color: '#a855f7' },
  { id: 'rose', name: 'Rose', color: '#ec4899' },
  { id: 'gris', name: 'Gris', color: '#6b7280' }
]

// Configuration de l'API WebSocket
const WS_URL = import.meta.env.VITE_WS_URL || (() => {
  const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:'
  return `${protocol}//${location.host}/ws`
})()

// État de l'application
const state = {
  sessionCode: null,
  sessionId: null,
  connected: false,
  voteState: 'idle', // idle, active, closed
  selectedColors: new Set(['rouge', 'vert', 'bleu']), // Par défaut
  multipleChoice: false,
  connectedCount: 0,
  votes: [], // { stagiaireId, stagiaireName, couleurs: [] }
  stagiaires: [], // [{ id, name }] Liste des stagiaires connectés
  stagiaireNames: {}, // { id: name } Map des noms pour lookup rapide
  voteStartTime: null,
  timerInterval: null
}

// Éléments DOM
let app = null
let ws = null

// Initialisation
function init() {
  app = document.getElementById('app')

  // Créer ou récupérer l'ID de session formateur
  let trainerId = localStorage.getItem('vote_trainer_id')
  if (!trainerId) {
    trainerId = 'trainer_' + Math.random().toString(36).substr(2, 9)
    localStorage.setItem('vote_trainer_id', trainerId)
  }

  // Générer un code session
  state.sessionCode = generateSessionCode()

  render()
  connectWebSocket()
}

// Générer un code à 4 chiffres
function generateSessionCode() {
  return Math.floor(1000 + Math.random() * 9000).toString()
}

// Connexion WebSocket
function connectWebSocket() {
  updateConnectionStatus(false)

  ws = new WebSocket(WS_URL)

  ws.onopen = () => {
    console.log('WebSocket connecté')
    updateConnectionStatus(true)

    // S'identifier comme formateur et créer/rejoindre la session
    send({
      type: 'trainer_join',
      sessionCode: state.sessionCode,
      trainerId: localStorage.getItem('vote_trainer_id')
    })
  }

  ws.onmessage = (event) => {
    try {
      const msg = JSON.parse(event.data)
      handleMessage(msg)
    } catch (e) {
      console.error('Erreur parsing message:', e)
    }
  }

  ws.onclose = () => {
    console.log('WebSocket déconnecté')
    updateConnectionStatus(false)
    // Tentative de reconnexion après 2 secondes
    setTimeout(connectWebSocket, 2000)
  }

  ws.onerror = (error) => {
    console.error('WebSocket error:', error)
  }
}

// Envoyer un message
function send(data) {
  if (ws && ws.readyState === WebSocket.OPEN) {
    ws.send(JSON.stringify(data))
  }
}

// Gérer les messages reçus
function handleMessage(msg) {
  switch (msg.type) {
    case 'session_created':
      state.sessionId = msg.sessionId
      state.sessionCode = msg.sessionCode || state.sessionCode
      render()
      break

    case 'connected_count':
      // Mise à jour des stagiaires connectés avec leurs noms
      state.connectedCount = msg.count || 0
      if (msg.stagiaires) {
        state.stagiaires = msg.stagiaires
        // Mettre à jour la map des noms
        state.stagiaireNames = {}
        msg.stagiaires.forEach(s => {
          if (s.name) {
            state.stagiaireNames[s.id] = s.name
          }
        })
      }
      renderHeader()
      break

    case 'stagiaire_names_updated':
      // Mise à jour des noms des stagiaires
      if (msg.stagiaires) {
        state.stagiaires = msg.stagiaires
        state.stagiaireNames = {}
        msg.stagiaires.forEach(s => {
          if (s.name) {
            state.stagiaireNames[s.id] = s.name
          }
        })
        // Mettre à jour les votes existants avec les nouveaux noms
        state.votes.forEach(vote => {
          if (state.stagiaireNames[vote.stagiaireId]) {
            vote.stagiaireName = state.stagiaireNames[vote.stagiaireId]
          }
        })
        renderVoteResults()
      }
      break

    case 'stagiaire_left':
      state.connectedCount = msg.connectedCount || 0
      renderHeader()
      break

    case 'vote_started':
      state.voteState = 'active'
      state.voteStartTime = Date.now()
      state.votes = []
      startTimer()
      render()
      break

    case 'vote_received':
      // Nouveau vote d'un stagiaire avec son nom
      const existingIndex = state.votes.findIndex(v => v.stagiaireId === msg.stagiaireId)
      const voteData = {
        couleurs: msg.colors,
        stagiaireId: msg.stagiaireId,
        stagiaireName: msg.stagiaireName || state.stagiaireNames[msg.stagiaireId] || ''
      }
      // Mettre à jour la map des noms
      if (msg.stagiaireName) {
        state.stagiaireNames[msg.stagiaireId] = msg.stagiaireName
      }
      if (existingIndex >= 0) {
        state.votes[existingIndex] = voteData
      } else {
        state.votes.push(voteData)
      }
      renderVoteResults()
      break

    case 'vote_closed':
      state.voteState = 'closed'
      stopTimer()
      render()
      break

    case 'vote_reset':
      state.voteState = 'idle'
      state.votes = []
      stopTimer()
      render()
      break

    case 'config_updated':
      // La configuration a été mise à jour (par le formateur ou par le backend)
      if (msg.selectedColors) {
        state.selectedColors = new Set(msg.selectedColors)
      }
      if (msg.multipleChoice !== undefined) {
        state.multipleChoice = msg.multipleChoice
      }
      render()
      break

    case 'connected_count':
      state.connectedCount = msg.count
      renderHeader()
      break
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

// Mettre à jour le statut de connexion
function updateConnectionStatus(connected) {
  state.connected = connected

  const existing = document.querySelector('.connection-status')
  if (existing) {
    existing.remove()
  }

  const status = document.createElement('div')
  status.className = `connection-status ${connected ? 'connected' : 'disconnected'}`
  status.innerHTML = `
    <span class="dot"></span>
    ${connected ? 'Connecté' : 'Déconnecté...'}
  `
  document.body.appendChild(status)
}

// Rendu principal
function render() {
  app.innerHTML = `
    <div class="container">
      ${renderHeaderHTML()}
      ${state.voteState === 'idle' ? renderConfigHTML() : renderVoteHTML()}
    </div>
    ${renderFooterHTML()}
  `

  attachEventListeners()
}

// Rendu du footer
function renderFooterHTML() {
  return `
    <footer class="footer">
      <span class="footer-author">${VERSION.author}</span>
      <span class="footer-separator">•</span>
      <a href="https://opensource.org/licenses/MIT" target="_blank" class="footer-link">Licence MIT</a>
      <span class="footer-separator">•</span>
      <span class="footer-version" title="${VERSION.fullHash}">${VERSION.commitHash}</span>
      <span class="footer-date">${VERSION.commitDate}</span>
    </footer>
  `
}

function renderHeader() {
  const header = document.querySelector('.header')
  if (header) {
    header.innerHTML = renderHeaderContent()
  }
}

function renderHeaderHTML() {
  // Construire la liste des stagiaires pour la popover
  const sortedStagiaires = [...state.stagiaires].sort((a, b) => {
    const nameA = (a.name || 'Anonyme').toLowerCase()
    const nameB = (b.name || 'Anonyme').toLowerCase()
    return nameA.localeCompare(nameB)
  })

  const stagiairesListHTML = sortedStagiaires.map(s => `
    <div class="stagiaire-popover-item">
      <span class="stagiaire-popover-name">${escapeHtml(s.name || 'Anonyme')}</span>
      <span class="stagiaire-popover-status online"></span>
    </div>
  `).join('')

  return `
    <header class="header">
      <h1>${icons.vote(' class="icon icon-md"')} Vote Coloré - Formateur</h1>
      <div class="session-info">
        ${state.sessionCode ? `<span class="session-code">${state.sessionCode}</span>` : ''}
        <div class="connected-count popover-trigger">
          <span class="dot"></span>
          <span class="count-text">${state.connectedCount} connecté${state.connectedCount > 1 ? 's' : ''}</span>
          ${state.connectedCount > 0 ? `<span class="popover-arrow">${icons.chevronDown(' class="icon icon-xs"')}</span>` : ''}
        </div>
        ${state.connectedCount > 0 ? `
          <div class="stagiaires-popover">
            <div class="popover-header">
              <span class="popover-title">Stagiaires connectés</span>
              <span class="popover-count">${state.connectedCount}</span>
            </div>
            <div class="popover-list">
              ${stagiairesListHTML}
            </div>
          </div>
        ` : ''}
      </div>
    </header>
  `
}

function renderHeaderContent() {
  // Construire la liste des stagiaires pour la popover
  const sortedStagiaires = [...state.stagiaires].sort((a, b) => {
    const nameA = (a.name || 'Anonyme').toLowerCase()
    const nameB = (b.name || 'Anonyme').toLowerCase()
    return nameA.localeCompare(nameB)
  })

  const stagiairesListHTML = sortedStagiaires.map(s => `
    <div class="stagiaire-popover-item">
      <span class="stagiaire-popover-name">${escapeHtml(s.name || 'Anonyme')}</span>
      <span class="stagiaire-popover-status online"></span>
    </div>
  `).join('')

  return `
    <h1>${icons.vote(' class="icon icon-md"')} Vote Coloré - Formateur</h1>
    <div class="session-info">
      ${state.sessionCode ? `<span class="session-code">${state.sessionCode}</span>` : ''}
      <div class="connected-count popover-trigger">
        <span class="dot"></span>
        <span class="count-text">${state.connectedCount} connecté${state.connectedCount > 1 ? 's' : ''}</span>
        ${state.connectedCount > 0 ? `<span class="popover-arrow">${icons.chevronDown(' class="icon icon-xs"')}</span>` : ''}
      </div>
      ${state.connectedCount > 0 ? `
        <div class="stagiaires-popover">
          <div class="popover-header">
            <span class="popover-title">Stagiaires connectés</span>
            <span class="popover-count">${state.connectedCount}</span>
          </div>
          <div class="popover-list">
            ${stagiairesListHTML}
          </div>
        </div>
      ` : ''}
    </div>
  `
}

// Rendu de la configuration
function renderConfigHTML() {
  return `
    <div class="card">
      <h2 class="card-title">Configuration du prochain vote</h2>

      <div class="config-section">
        <div>
          <div class="stats-header">Couleurs disponibles</div>
          <div class="colors-grid">
            ${COLORS.map(color => `
              <label class="color-checkbox ${state.selectedColors.has(color.id) ? 'selected' : ''}">
                <input
                  type="checkbox"
                  value="${color.id}"
                  ${state.selectedColors.has(color.id) ? 'checked' : ''}
                />
                <span class="color-swatch" style="background: ${color.color}"></span>
                <span class="color-name">${color.name}</span>
              </label>
            `).join('')}
          </div>
        </div>

        <label class="multiple-choice-toggle">
          <span class="toggle-switch ${state.multipleChoice ? 'active' : ''}" data-action="toggle-multiple"></span>
          <span>Choix multiple (autoriser plusieurs couleurs)</span>
        </label>
      </div>

      <div class="button-row">
        <button class="btn btn-primary btn-large" id="startVote" ${state.selectedColors.size < 2 ? 'disabled' : ''}>
          ${icons.rocket(' class="icon icon-md"')} Lancer le vote
        </button>
      </div>
    </div>
  `
}

// Rendu du vote en cours
function renderVoteHTML() {
  const activeColors = COLORS.filter(c => state.selectedColors.has(c.id))

  return `
    <div class="card">
      <div class="vote-header">
        <div class="vote-timer">${icons.timer(' class="icon icon-sm"')} 00:00</div>
        <div class="vote-stats">${icons.chart(' class="icon icon-sm"')} ${state.votes.length} / ${state.connectedCount} votes</div>
      </div>

      <div class="stats-grid stats-grid-3cols">
        <div>
          <div class="stats-header">Par couleur</div>
          <div class="color-bars">
            ${activeColors.map(color => {
              const count = getColorCount(color.id)
              const colorCounts = getColorCounts()
              const maxCount = Math.max(...Object.values(colorCounts), 1)
              const percent = (count / maxCount) * 100
              return `
                <div class="color-bar-row">
                  <div class="color-bar-label">
                    <span class="color-bar-swatch" style="background: ${color.color}"></span>
                    <span class="color-bar-name">${color.name}</span>
                  </div>
                  <div class="color-bar-track">
                    <span class="color-bar-count">${count}</span>
                    <div class="color-bar-fill ${count === 0 ? 'empty' : ''}" style="width: ${percent}%; background: ${color.color}"></div>
                  </div>
                </div>
              `
            }).join('')}
          </div>
        </div>

        <div>
          <div class="stats-header">Par combinaison</div>
          <div class="combinations-list">
            ${renderCombinationsHTML(activeColors)}
          </div>
        </div>

        <div>
          <div class="stats-header">Par stagiaire</div>
          <div class="stagiaires-votes-list">
            ${renderStagiairesVotesHTML(activeColors)}
          </div>
        </div>
      </div>

      <div class="button-row">
        ${state.voteState === 'active' ? `
          <button class="btn btn-danger" id="closeVote">${icons.stop(' class="icon icon-md"')} Fermer le vote</button>
        ` : `
          <button class="btn btn-success" id="newVote">${icons.refresh(' class="icon icon-md"')} Nouveau vote</button>
        `}
      </div>
    </div>
  `
}

// Rendu des combinaisons
function renderCombinationsHTML(activeColors) {
  const combinations = getCombinations()

  if (combinations.length === 0) {
    return `
      <div class="empty-state">
        <div class="empty-icon">${icons.chart(' class="icon icon-xl"')}</div>
        <div>Aucun vote pour le moment</div>
      </div>
    `
  }

  const maxCount = Math.max(...combinations.map(c => c.count))

  return combinations.map(combo => {
    const percent = maxCount > 0 ? (combo.count / maxCount * 100) : 0
    return `
      <div class="combo-item">
        <div class="combo-colors">
          ${combo.colors.map(colorId => {
            const color = COLORS.find(c => c.id === colorId)
            return `<span class="combo-swatch" style="background: ${color?.color || '#666'}"></span>`
          }).join('')}
        </div>
        <div class="combo-bar">
          <div class="combo-bar-fill" style="width: ${percent}%"></div>
        </div>
        <div class="combo-count">${combo.count}</div>
      </div>
    `
  }).join('')
}

// Rendu des votes par stagiaire
function renderStagiairesVotesHTML(activeColors) {
  if (state.votes.length === 0) {
    return `
      <div class="empty-state">
        <div class="empty-icon">${icons.users(' class="icon icon-xl"')}</div>
        <div>Aucun vote pour le moment</div>
      </div>
    `
  }

  // Trier les votes par nom de stagiaire
  const sortedVotes = [...state.votes].sort((a, b) => {
    const nameA = (a.stagiaireName || 'Anonyme').toLowerCase()
    const nameB = (b.stagiaireName || 'Anonyme').toLowerCase()
    return nameA.localeCompare(nameB)
  })

  return sortedVotes.map(vote => {
    const displayName = vote.stagiaireName || 'Anonyme'
    const colorsHTML = vote.couleurs.map(colorId => {
      const color = COLORS.find(c => c.id === colorId)
      return `<span class="stagiaire-vote-swatch" style="background: ${color?.color || '#666'}" title="${color?.name || colorId}"></span>`
    }).join('')

    return `
      <div class="stagiaire-vote-item">
        <span class="stagiaire-vote-name" title="${escapeHtml(displayName)}">${escapeHtml(displayName)}</span>
        <div class="stagiaire-vote-colors">${colorsHTML}</div>
      </div>
    `
  }).join('')
}

// Échapper le HTML pour éviter les XSS
function escapeHtml(text) {
  const div = document.createElement('div')
  div.textContent = text
  return div.innerHTML
}

// Calculer les combinaisons de votes
function getCombinations() {
  const comboMap = new Map()

  state.votes.forEach(vote => {
    const key = vote.couleurs.slice().sort().join('+')
    comboMap.set(key, (comboMap.get(key) || 0) + 1)
  })

  return Array.from(comboMap.entries())
    .map(([key, count]) => ({
      colors: key ? key.split('+') : [],
      count
    }))
    .sort((a, b) => b.count - a.count)
}

// Calculer le nombre de votes pour une couleur
function getColorCount(colorId) {
  return getColorCounts()[colorId] || 0
}

// Calculer les votes par couleur
function getColorCounts() {
  const counts = {}
  state.votes.forEach(vote => {
    vote.couleurs.forEach(colorId => {
      counts[colorId] = (counts[colorId] || 0) + 1
    })
  })
  return counts
}

// Mettre à jour uniquement les résultats du vote (sans re-render complet)
function renderVoteResults() {
  const voteStats = document.querySelector('.vote-stats')
  if (voteStats) {
    voteStats.innerHTML = `${icons.chart(' class="icon icon-sm"')} ${state.votes.length} / ${state.connectedCount} votes`
  }

  // Mettre à jour les barres de couleur
  const colorBarsContainer = document.querySelector('.color-bars')
  if (colorBarsContainer) {
    const activeColors = COLORS.filter(c => state.selectedColors.has(c.id))
    const maxCount = Math.max(...Object.values(getColorCounts()), 1)

    colorBarsContainer.innerHTML = activeColors.map(color => {
      const count = getColorCount(color.id)
      const percent = (count / maxCount) * 100
      return `
        <div class="color-bar-row">
          <div class="color-bar-label">
            <span class="color-bar-swatch" style="background: ${color.color}"></span>
            <span class="color-bar-name">${color.name}</span>
          </div>
          <div class="color-bar-track">
            <span class="color-bar-count">${count}</span>
            <div class="color-bar-fill ${count === 0 ? 'empty' : ''}" style="width: ${percent}%; background: ${color.color}"></div>
          </div>
        </div>
      `
    }).join('')
  }

  // Mettre à jour les combinaisons
  const combinationsList = document.querySelector('.combinations-list')
  if (combinationsList) {
    const activeColors = COLORS.filter(c => state.selectedColors.has(c.id))
    combinationsList.innerHTML = renderCombinationsHTML(activeColors)
  }

  // Mettre à jour la liste des votes par stagiaire
  const stagiairesVotesList = document.querySelector('.stagiaires-votes-list')
  if (stagiairesVotesList) {
    const activeColors = COLORS.filter(c => state.selectedColors.has(c.id))
    stagiairesVotesList.innerHTML = renderStagiairesVotesHTML(activeColors)
  }
}

// Attacher les écouteurs d'événements
function attachEventListeners() {
  // Popover des stagiaires (toggle au clic pour mobile)
  const popoverTrigger = document.querySelector('.popover-trigger')
  if (popoverTrigger) {
    popoverTrigger.addEventListener('click', (e) => {
      // Ne pas toggle si on clique sur la popover elle-même
      if (e.target.closest('.stagiaires-popover')) return
      popoverTrigger.classList.toggle('active')
    })

    // Fermer la popover si on clique ailleurs
    document.addEventListener('click', (e) => {
      if (!e.target.closest('.session-info')) {
        popoverTrigger.classList.remove('active')
      }
    })
  }

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

  // Toggle choix multiple
  const toggleMultiple = document.querySelector('[data-action="toggle-multiple"]')
  if (toggleMultiple) {
    toggleMultiple.addEventListener('click', () => {
      state.multipleChoice = !state.multipleChoice
      toggleMultiple.classList.toggle('active', state.multipleChoice)
    })
  }

  // Bouton lancer le vote
  const startBtn = document.getElementById('startVote')
  if (startBtn) {
    startBtn.addEventListener('click', startVote)
  }

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

// Actions
function startVote() {
  const activeColors = Array.from(state.selectedColors)

  send({
    type: 'start_vote',
    sessionId: state.sessionId,
    colors: activeColors,
    multipleChoice: state.multipleChoice
  })

  // Optimistic update
  state.voteState = 'active'
  state.voteStartTime = Date.now()
  state.votes = []
  startTimer()
  render()
}

function closeVote() {
  send({
    type: 'close_vote',
    sessionId: state.sessionId
  })

  state.voteState = 'closed'
  stopTimer()
  render()
}

function resetVote() {
  send({
    type: 'reset_vote',
    sessionId: state.sessionId,
    colors: Array.from(state.selectedColors),
    multipleChoice: state.multipleChoice
  })

  state.voteState = 'idle'
  state.votes = []
  stopTimer()
  render()
}

// Démarrage
init()
