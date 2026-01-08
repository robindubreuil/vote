import './style.css'

// Configuration des couleurs (même que le formateur)
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

// Configuration de l'API WebSocket (à adapter selon le backend)
const WS_URL = import.meta.env.VITE_WS_URL || 'ws://localhost:8080/ws'

// États de l'application
const AppState = {
  JOINING: 'joining',      // Saisie du code session
  WAITING: 'waiting',      // En attente du prochain vote
  VOTING: 'voting',        // Vote en cours
  VOTED: 'voted',          // Vote enregistré
  CLOSED: 'closed'         // Vote terminé par le formateur
}

// État de l'application
const state = {
  appState: AppState.JOINING,
  sessionCode: '',
  sessionId: null,
  connected: false,
  availableColors: [],
  multipleChoice: false,
  selectedColors: new Set(),
  hasVoted: false,
  stagiaireId: null
}

// Éléments DOM
let app = null
let ws = null

// Initialisation
function init() {
  app = document.getElementById('app')

  // Récupérer ou créer l'ID du stagiaire
  let savedId = localStorage.getItem('vote_stagiaire_id')
  if (!savedId) {
    savedId = 'stagiaire_' + Math.random().toString(36).substr(2, 9)
    localStorage.setItem('vote_stagiaire_id', savedId)
  }
  state.stagiaireId = savedId

  // Vérifier si on a déjà un code session enregistré
  const savedCode = localStorage.getItem('vote_session_code')
  if (savedCode) {
    state.sessionCode = savedCode
    connectWebSocket(savedCode)
  } else {
    render()
  }
}

// Connexion WebSocket
function connectWebSocket(sessionCode) {
  updateConnectionStatus(false)

  ws = new WebSocket(WS_URL)

  ws.onopen = () => {
    console.log('WebSocket connecté')
    updateConnectionStatus(true)

    // S'identifier comme stagiaire
    send({
      type: 'stagiaire_join',
      sessionCode: sessionCode,
      stagiaireId: state.stagiaireId
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
    setTimeout(() => {
      if (state.sessionCode) {
        connectWebSocket(state.sessionCode)
      }
    }, 2000)
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
    case 'session_joined':
      // Connexion réussie à la session
      state.sessionId = msg.sessionId
      state.sessionCode = msg.sessionCode
      state.appState = AppState.WAITING
      localStorage.setItem('vote_session_code', msg.sessionCode)
      render()
      break

    case 'join_error':
      // Erreur de connexion (mauvais code)
      showError('Code de session invalide ou session non trouvée')
      state.appState = AppState.JOINING
      state.sessionCode = ''
      localStorage.removeItem('vote_session_code')
      render()
      break

    case 'vote_started':
      // Nouveau vote lancé
      state.availableColors = msg.colors || []
      state.multipleChoice = msg.multipleChoice || false
      state.selectedColors.clear()
      state.hasVoted = false
      state.appState = AppState.VOTING
      render()
      break

    case 'vote_accepted':
      // Vote accepté
      state.hasVoted = true
      state.appState = AppState.VOTED
      render()
      break

    case 'vote_closed':
      // Vote terminé par le formateur
      state.appState = AppState.CLOSED
      render()
      break

    case 'vote_reset':
      // Réinitialisation pour un nouveau vote
      state.appState = AppState.WAITING
      state.selectedColors.clear()
      state.hasVoted = false
      render()
      break

    case 'config_updated':
      // La configuration a été mise à jour
      if (msg.colors) {
        state.availableColors = msg.colors
      }
      if (msg.multipleChoice !== undefined) {
        state.multipleChoice = msg.multipleChoice
      }
      if (state.appState === AppState.VOTING) {
        state.selectedColors.clear()
        render()
      }
      break
  }
}

// Mettre à jour le statut de connexion
function updateConnectionStatus(connected) {
  state.connected = connected

  const existing = document.querySelector('.connection-status')
  if (existing) {
    existing.remove()
  }

  // Ne pas afficher le statut si on est en phase de connexion
  if (state.appState === AppState.JOINING) {
    return
  }

  const status = document.createElement('div')
  status.className = `connection-status ${connected ? 'connected' : 'disconnected'}`
  status.innerHTML = `
    <span class="dot"></span>
    ${connected ? '' : 'Reconnexion...'}
  `
  document.body.appendChild(status)
}

// Afficher une erreur
function showError(message) {
  const errorEl = document.querySelector('.error-message')
  if (errorEl) {
    errorEl.textContent = message
    setTimeout(() => {
      errorEl.textContent = ''
    }, 3000)
  }
}

// Rendu principal
function render() {
  app.innerHTML = `
    <div class="container">
      ${state.appState === AppState.JOINING ? renderJoinHTML() : ''}
      ${state.appState === AppState.WAITING ? renderWaitingHTML() : ''}
      ${state.appState === AppState.VOTING ? renderVotingHTML() : ''}
      ${state.appState === AppState.VOTED ? renderVotedHTML() : ''}
      ${state.appState === AppState.CLOSED ? renderClosedHTML() : ''}
    </div>
  `

  attachEventListeners()
  updateConnectionStatus(state.connected)
}

// Rendu du formulaire de connexion
function renderJoinHTML() {
  return `
    <div class="card">
      <h2 class="card-title">🗳️ Vote Coloré</h2>
      <form class="join-form" id="joinForm">
        <div class="input-group">
          <label for="sessionCode">Code de session</label>
          <input
            type="text"
            id="sessionCode"
            class="session-input"
            placeholder="0000"
            maxlength="4"
            pattern="[0-9]{4}"
            inputmode="numeric"
            value="${state.sessionCode}"
            autocomplete="off"
          />
        </div>
        <div class="error-message"></div>
        <button type="submit" class="btn btn-primary btn-large">
          Rejoindre
        </button>
      </form>
    </div>
  `
}

// Rendu de l'état d'attente
function renderWaitingHTML() {
  return `
    <header class="header">
      <h1>🗳️ Vote Coloré</h1>
      <div class="session-code-display valid">
        ✓ ${state.sessionCode}
      </div>
    </header>
    <div class="card">
      <div class="waiting-state">
        <div class="waiting-icon">⏳</div>
        <div class="waiting-text">En attente du prochain vote...</div>
      </div>
    </div>
  `
}

// Rendu de l'interface de vote
function renderVotingHTML() {
  const activeColors = COLORS.filter(c => state.availableColors.includes(c.id))

  return `
    <header class="header">
      <h1>🗳️ Vote Coloré</h1>
      <div class="session-code-display valid">
        ✓ ${state.sessionCode}
      </div>
    </header>
    <div class="card">
      <h2 class="card-title">Votez maintenant !</h2>
      <p class="vote-instruction ${state.multipleChoice ? 'multiple-choice' : 'single-choice'}">
        ${state.multipleChoice
          ? 'Vous pouvez choisir plusieurs couleurs'
          : 'Choisissez une seule couleur'
        }
      </p>

      ${state.multipleChoice ? renderMultipleChoiceHTML(activeColors) : renderSingleChoiceHTML(activeColors)}
    </div>
  `
}

// Rendu pour choix unique
function renderSingleChoiceHTML(activeColors) {
  return `
    <div class="vote-grid">
      ${activeColors.map(color => `
        <button
          type="button"
          class="vote-button bg-${color.id} ${state.selectedColors.has(color.id) ? 'selected' : ''}"
          data-color="${color.id}"
          aria-pressed="${state.selectedColors.has(color.id)}"
        >
          ${color.name}
        </button>
      `).join('')}
    </div>
  `
}

// Rendu pour choix multiple
function renderMultipleChoiceHTML(activeColors) {
  return `
    <div class="vote-grid">
      ${activeColors.map(color => `
        <input
          type="checkbox"
          id="color-${color.id}"
          class="vote-checkbox"
          value="${color.id}"
          ${state.selectedColors.has(color.id) ? 'checked' : ''}
        />
        <label
          for="color-${color.id}"
          class="vote-checkbox-label bg-${color.id} ${state.selectedColors.has(color.id) ? 'selected' : ''}"
        >
          ${color.name}
          <span class="check-indicator"></span>
        </label>
      `).join('')}
    </div>
    <button type="button" class="btn btn-success btn-large" id="submitVote" ${state.selectedColors.size === 0 ? 'disabled' : ''}>
      Valider mon vote
    </button>
  `
}

// Rendu après vote
function renderVotedHTML() {
  const selectedNames = Array.from(state.selectedColors).map(id => {
    const color = COLORS.find(c => c.id === id)
    return color?.name || id
  }).join(' + ')

  return `
    <header class="header">
      <h1>🗳️ Vote Coloré</h1>
      <div class="session-code-display valid">
        ✓ ${state.sessionCode}
      </div>
    </header>
    <div class="card">
      <div class="voted-state">
        <div class="voted-icon">✓</div>
        <div class="voted-title">Vote enregistré !</div>
        <div class="voted-subtitle">${selectedNames}</div>
      </div>
    </div>
  `
}

// Rendu vote terminé
function renderClosedHTML() {
  return `
    <header class="header">
      <h1>🗳️ Vote Coloré</h1>
      <div class="session-code-display valid">
        ✓ ${state.sessionCode}
      </div>
    </header>
    <div class="card">
      <div class="vote-closed-state">
        <div class="closed-icon">⏹</div>
        <div class="waiting-text">Le vote est terminé</div>
      </div>
    </div>
  `
}

// Attacher les écouteurs d'événements
function attachEventListeners() {
  // Formulaire de connexion
  const joinForm = document.getElementById('joinForm')
  if (joinForm) {
    joinForm.addEventListener('submit', handleJoin)
  }

  // Boutons de vote (choix unique)
  document.querySelectorAll('.vote-button').forEach(btn => {
    btn.addEventListener('click', handleSingleChoiceVote)
  })

  // Checkboxes (choix multiple)
  document.querySelectorAll('.vote-checkbox').forEach(checkbox => {
    checkbox.addEventListener('change', handleCheckboxChange)
  })

  // Bouton valider (choix multiple)
  const submitBtn = document.getElementById('submitVote')
  if (submitBtn) {
    submitBtn.addEventListener('click', handleSubmitVote)
  }
}

// Gérer la connexion
function handleJoin(e) {
  e.preventDefault()

  const input = document.getElementById('sessionCode')
  const code = input.value.trim()

  // Validation du code (4 chiffres)
  if (!/^\d{4}$/.test(code)) {
    input.classList.add('error')
    showError('Le code doit contenir 4 chiffres')
    return
  }

  input.classList.remove('error')
  state.sessionCode = code
  connectWebSocket(code)
}

// Gérer le vote à choix unique
function handleSingleChoiceVote(e) {
  const colorId = e.target.dataset.color

  // Désélectionner les autres
  state.selectedColors.clear()
  state.selectedColors.add(colorId)

  // Envoyer le vote immédiatement
  submitVote()
}

// Gérer le changement de checkbox
function handleCheckboxChange(e) {
  const colorId = e.target.value
  const label = document.querySelector(`label[for="color-${colorId}"]`)

  if (e.target.checked) {
    state.selectedColors.add(colorId)
  } else {
    state.selectedColors.delete(colorId)
  }

  // Mettre à jour l'UI
  const allLabels = document.querySelectorAll('.vote-checkbox-label')
  allLabels.forEach(lbl => {
    const checkbox = document.getElementById(lbl.htmlFor)
    if (checkbox && checkbox.checked) {
      lbl.classList.add('selected')
    } else {
      lbl.classList.remove('selected')
    }
  })

  // Mettre à jour le bouton
  const submitBtn = document.getElementById('submitVote')
  if (submitBtn) {
    submitBtn.disabled = state.selectedColors.size === 0
  }
}

// Gérer la soumission du vote
function handleSubmitVote() {
  if (state.selectedColors.size > 0) {
    submitVote()
  }
}

// Soumettre le vote
function submitVote() {
  send({
    type: 'vote',
    sessionId: state.sessionId,
    stagiaireId: state.stagiaireId,
    couleurs: Array.from(state.selectedColors)
  })
}

// Démarrage
init()
