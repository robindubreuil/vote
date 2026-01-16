import './style.css'
import { icons } from '../../shared/icons.js'
import { COLORS, escapeHtml } from '../../shared/colors.js'
import { VoteClient } from '../../shared/websocket-client.js'
import { renderFooterHTML, renderConnectionStatus } from '../../shared/ui.js'

// Configuration de l'API WebSocket
const WS_URL = import.meta.env.VITE_WS_URL || (() => {
  const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:'
  return `${protocol}//${location.host}/ws`
})()

// État de l'application
const state = {
  sessionCode: null,
  connected: false,
  voteState: 'idle', // idle, active, closed
  selectedColors: new Set(COLORS.slice(0, 3).map(c => c.id)), // Par défaut: 3 premières couleurs
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
let client = null
let connectionStatusEl = null

// Initialisation
function init() {
  app = document.getElementById('app')

  // Créer ou récupérer l'ID de session formateur
  let trainerId = localStorage.getItem('vote_trainer_id')
  if (!trainerId) {
    trainerId = 'trainer_' + Math.random().toString(36).substring(2, 11)
    localStorage.setItem('vote_trainer_id', trainerId)
  }

  // Générer un code session
  state.sessionCode = generateSessionCode()

  // Initialiser l'interface de base
  renderFullLayout()
  
  // Connecter le WebSocket
  initClient(trainerId)

  // Listener global pour fermer la popover des stagiaires quand on clique en dehors
  document.addEventListener('click', (e) => {
    const popoverContainer = document.querySelector('.session-info')
    if (popoverContainer && !popoverContainer.contains(e.target)) {
      const trigger = popoverContainer.querySelector('.popover-trigger')
      if (trigger) trigger.classList.remove('active')
    }
  })
}

// Générer un code à 4 chiffres
function generateSessionCode() {
  return Math.floor(1000 + Math.random() * 9000).toString()
}

// Initialisation WebSocket
function initClient(trainerId) {
  client = new VoteClient(WS_URL, {
    onStatusChange: (connected) => {
      state.connected = connected
      updateConnectionStatus(connected)
    },
    onOpen: () => {
      client.send({
        type: 'trainer_join',
        sessionCode: state.sessionCode,
        trainerId: trainerId
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
      state.sessionCode = msg.sessionCode || state.sessionCode
      updateHeader()
      break

    case 'connected_count':
      // Mise à jour des stagiaires connectés avec leurs noms
      state.connectedCount = msg.count || 0
      if (msg.stagiaires) {
        state.stagiaires = msg.stagiaires
        updateStagiaireNames(msg.stagiaires)
      }
      updateHeader()
      break

    case 'stagiaire_names_updated':
      // Mise à jour des noms des stagiaires
      if (msg.stagiaires) {
        state.stagiaires = msg.stagiaires
        updateStagiaireNames(msg.stagiaires)
        // Mettre à jour les votes existants avec les nouveaux noms
        state.votes.forEach(vote => {
          if (state.stagiaireNames[vote.stagiaireId]) {
            vote.stagiaireName = state.stagiaireNames[vote.stagiaireId]
          }
        })
        if (state.voteState !== 'idle') {
            updateVoteResults()
        }
      }
      break

    case 'vote_started':
      state.voteState = 'active'
      state.voteStartTime = Date.now()
      state.votes = []
      startTimer()
      renderMainContent() // Basculer vers l'interface de vote
      break

    case 'vote_received':
      // Nouveau vote d'un stagiaire
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
      
      // Optimisation: mise à jour partielle
      updateVoteResults()
      break

    case 'vote_closed':
      state.voteState = 'closed'
      stopTimer()
      renderMainContent() // Mettre à jour les boutons d'action
      break

    case 'vote_reset':
      state.voteState = 'idle'
      state.votes = []
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

    default:
      console.debug('Type de message inconnu:', msg.type)
  }
}

// Mettre à jour la map des noms des stagiaires
function updateStagiaireNames(stagiaires) {
  state.stagiaireNames = {}
  stagiaires.forEach(s => {
    if (s.name) {
      state.stagiaireNames[s.id] = s.name
    }
  })
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
  connectionStatusEl = renderConnectionStatus(connected, document.body, connectionStatusEl)
}

// Structure globale
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

  header.innerHTML = `
    <h1>${icons.vote(' class="icon icon-md"')} Vote Coloré - Formateur</h1>
    <div class="session-info">
      ${state.sessionCode ? `<span class="session-code">${state.sessionCode}</span>` : ''}
      <div class="connected-count popover-trigger" tabindex="0" role="button" aria-haspopup="true" aria-expanded="false">
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
          <div class="popover-list custom-scrollbar">
            ${renderStagiairesListHTML()}
          </div>
        </div>
      ` : ''}
    </div>
  `
  attachHeaderListeners()
}

function attachHeaderListeners() {
  const popoverTrigger = document.querySelector('.popover-trigger')
  if (popoverTrigger) {
    const toggle = (e) => {
      // Ne pas toggle si on clique sur la popover elle-même
      if (e.target.closest('.stagiaires-popover')) return
      
      popoverTrigger.classList.toggle('active')
      const isExpanded = popoverTrigger.classList.contains('active')
      popoverTrigger.setAttribute('aria-expanded', isExpanded)
    }

    popoverTrigger.addEventListener('click', toggle)
    popoverTrigger.addEventListener('keydown', (e) => {
      if (e.key === 'Enter' || e.key === ' ') {
        e.preventDefault()
        toggle(e)
      }
    })
  }
}

// Générer la liste HTML des stagiaires pour la popover
function renderStagiairesListHTML() {
  const sortedStagiaires = [...state.stagiaires].sort((a, b) => {
    const nameA = (a.name || 'Anonyme').toLowerCase()
    const nameB = (b.name || 'Anonyme').toLowerCase()
    return nameA.localeCompare(nameB)
  })

  return sortedStagiaires.map(s => `
    <div class="stagiaire-popover-item">
      <span class="stagiaire-popover-name">${escapeHtml(s.name || 'Anonyme')}</span>
      <span class="stagiaire-popover-status online"></span>
    </div>
  `).join('')
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
    updateVoteResults() // Hydrate initial stats
  }
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
                <span class="color-swatch" style="background-color: ${color.color}"></span>
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
}

// Rendu du vote en cours
function renderVoteHTML() {
  const activeColors = COLORS.filter(c => state.selectedColors.has(c.id))
  // Empty containers, will be filled by updateVoteResults
  
  return `
    <div class="card">
      <div class="vote-header">
        <div class="vote-timer">${icons.timer(' class="icon icon-sm"')} 00:00</div>
        <div class="vote-stats" aria-live="polite">${icons.chart(' class="icon icon-sm"')} 0 / ${state.connectedCount} votes</div>
      </div>

      <div class="stats-grid stats-grid-3cols">
        <div>
          <div class="stats-header">Par couleur</div>
          <div class="color-bars">
            <!-- Filled by JS -->
          </div>
        </div>

        <div>
          <div class="stats-header">Par combinaison</div>
          <div class="combinations-list custom-scrollbar">
             <!-- Filled by JS -->
          </div>
        </div>

        <div>
          <div class="stats-header">Par stagiaire</div>
          <div class="stagiaires-votes-list custom-scrollbar">
             <!-- Filled by JS -->
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
  const voteStats = document.querySelector('.vote-stats')
  if (voteStats) {
    voteStats.innerHTML = `${icons.chart(' class="icon icon-sm"')} ${state.votes.length} / ${state.connectedCount} votes`
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

  // Premier rendu si vide ou si le nombre de barres a changé
  // (ce qui arrive si on change la config, mais pas pendant le vote normalement)
  if (container.children.length === 0 || container.children.length !== activeColors.length) {
    container.innerHTML = renderColorBarsHTML(activeColors, colorCounts, maxCount)
    return
  }

  // Mise à jour des barres existantes
  activeColors.forEach(color => {
    const count = colorCounts[color.id] || 0
    const percent = (count / maxCount) * 100
    
    // On cherche la ligne correspondant à la couleur
    const row = container.querySelector(`[data-color="${color.id}"]`)
    if (row) {
      const countEl = row.querySelector('.color-bar-count')
      const fillEl = row.querySelector('.color-bar-fill')
      
      if (countEl && countEl.textContent !== count.toString()) {
        countEl.textContent = count
      }
      
      if (fillEl) {
        fillEl.style.width = `${percent}%`
        // Use background-color to respect CSS gradients
        // fillEl.style.backgroundColor = color.color // Already set via inline style, just need to ensure initial render is correct
        if (count === 0) {
            fillEl.classList.add('empty')
        } else {
            fillEl.classList.remove('empty')
        }
      }
    } else {
      // Fallback si on ne trouve pas la ligne (ne devrait pas arriver si la structure est stable)
      container.innerHTML = renderColorBarsHTML(activeColors, colorCounts, maxCount)
    }
  })
}

// Rendu des barres de couleur
function renderColorBarsHTML(activeColors, colorCounts, maxCount) {
  return activeColors.map(color => {
    const count = colorCounts[color.id] || 0
    const percent = (count / maxCount) * 100
    return `
      <div class="color-bar-row" data-color="${color.id}">
        <div class="color-bar-label">
          <span class="color-bar-swatch" style="background-color: ${color.color}"></span>
          <span class="color-bar-name">${color.name}</span>
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

  const maxCount = Math.max(...combinations.map(c => c.count))

  return combinations.map(combo => {
    const percent = maxCount > 0 ? (combo.count / maxCount * 100) : 0
    return `
      <div class="combo-item">
        <div class="combo-colors">
          ${combo.colors.map(colorId => {
            const color = COLORS.find(c => c.id === colorId)
            return `<span class="combo-swatch" style="background-color: ${color?.color || '#666'}"></span>`
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
function renderStagiairesVotesHTML() {
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
      return `<span class="stagiaire-vote-swatch" style="background-color: ${color?.color || '#666'}" title="${color?.name || colorId}"></span>`
    }).join('')

    return `
      <div class="stagiaire-vote-item">
        <span class="stagiaire-vote-name" title="${escapeHtml(displayName)}">${escapeHtml(displayName)}</span>
        <div class="stagiaire-vote-colors">${colorsHTML}</div>
      </div>
    `
  }).join('')
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

// Actions
function startVote() {
  client.send({
    type: 'start_vote',
    sessionCode: state.sessionCode,
    colors: Array.from(state.selectedColors),
    multipleChoice: state.multipleChoice
  })
  // L'état sera mis à jour à la réception du message 'vote_started' par le serveur
}

function closeVote() {
  client.send({
    type: 'close_vote',
    sessionCode: state.sessionCode
  })

  state.voteState = 'closed'
  stopTimer()
  renderMainContent()
}

function resetVote() {
  client.send({
    type: 'reset_vote',
    sessionCode: state.sessionCode,
    colors: Array.from(state.selectedColors),
    multipleChoice: state.multipleChoice
  })

  state.voteState = 'idle'
  state.votes = []
  stopTimer()
  renderMainContent()
}

// Demander confirmation avant de quitter la page
window.addEventListener('beforeunload', (e) => {
  e.preventDefault()
})

// Démarrage
init()
