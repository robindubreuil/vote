import './style.css'
import { icons } from '../../shared/icons.js'
import { VERSION } from '../../shared/version.js'
import { COLORS, escapeHtml } from '../../shared/colors.js'

// Configuration de l'API WebSocket
const WS_URL = import.meta.env.VITE_WS_URL || (() => {
  const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:'
  return `${protocol}//${location.host}/ws`
})()

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
  stagiaireId: null,
  prenom: '',
  prenomEdit: false // Mode édition du nom
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

  // Récupérer le prénom s'il existe
  const savedPrenom = localStorage.getItem('vote_stagiaire_prenom')
  if (savedPrenom) {
    state.prenom = savedPrenom
  }

  // Vérifier si on a déjà un code session enregistré
  const savedCode = localStorage.getItem('vote_session_code')
  if (savedCode) {
    state.sessionCode = savedCode
    // Ne connecter que si on a un prénom
    if (state.prenom) {
      connectWebSocket(savedCode)
    } else {
      render()
    }
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

    // S'identifier comme stagiaire avec le nom
    send({
      type: 'stagiaire_join',
      sessionCode: sessionCode,
      stagiaireId: state.stagiaireId,
      name: state.prenom
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

    case 'name_updated':
      // Confirmation de mise à jour du nom
      state.prenomEdit = false
      render()
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
      ${state.prenomEdit ? renderEditNameHTML() : ''}
      ${state.appState === AppState.WAITING && !state.prenomEdit ? renderWaitingHTML() : ''}
      ${state.appState === AppState.VOTING && !state.prenomEdit ? renderVotingHTML() : ''}
      ${state.appState === AppState.VOTED && !state.prenomEdit ? renderVotedHTML() : ''}
      ${state.appState === AppState.CLOSED && !state.prenomEdit ? renderClosedHTML() : ''}
    </div>
    ${renderFooterHTML()}
  `

  attachEventListeners()
  updateConnectionStatus(state.connected)
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

// Rendu du formulaire de connexion
function renderJoinHTML() {
  return `
    <div class="card">
      <h2 class="card-title">${icons.vote(' class="icon icon-md"')} Vote Coloré</h2>
      <form class="join-form" id="joinForm">
        <div class="input-group">
          <label for="prenom">Votre prénom</label>
          <input
            type="text"
            id="prenom"
            class="session-input"
            placeholder="Ex: Marie"
            value="${state.prenom}"
            autocomplete="name"
            autocapitalize="words"
            required
          />
        </div>
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
      <h1>${icons.vote(' class="icon icon-md"')} Vote Coloré</h1>
      <div class="session-code-display valid">
        ${icons.check(' class="icon icon-sm text-success"')} ${state.sessionCode}
      </div>
    </header>
    <div class="card">
      <div class="waiting-state">
        <div class="waiting-icon">${icons.hourglass(' class="icon icon-xl"')}</div>
        <div class="waiting-text">En attente du prochain vote...</div>
        <div class="waiting-name">Bonjour, <strong>${escapeHtml(state.prenom)}</strong> !</div>
        <button type="button" class="btn btn-secondary btn-small" id="editNameBtn">
          ${icons.pencil(' class="icon icon-sm"')} Modifier mon nom
        </button>
      </div>
    </div>
  `
}

// Rendu du formulaire de modification du nom
function renderEditNameHTML() {
  return `
    <div class="card edit-name-modal">
      <h2 class="card-title">Modifier mon nom</h2>
      <form class="join-form" id="editNameForm">
        <div class="input-group">
          <label for="editPrenom">Ton prénom</label>
          <input
            type="text"
            id="editPrenom"
            class="session-input"
            placeholder="Ex: Marie"
            value="${state.prenom}"
            autocomplete="name"
            autocapitalize="words"
            required
            autofocus
          />
        </div>
        <div class="button-row">
          <button type="button" class="btn btn-secondary" id="cancelEditName">Annuler</button>
          <button type="submit" class="btn btn-primary">Enregistrer</button>
        </div>
      </form>
    </div>
  `
}

// Rendu de l'interface de vote
function renderVotingHTML() {
  const activeColors = COLORS.filter(c => state.availableColors.includes(c.id))

  return `
    <header class="header">
      <h1>${icons.vote(' class="icon icon-md"')} Vote Coloré</h1>
      <div class="session-code-display valid">
        ${icons.check(' class="icon icon-sm text-success"')} ${state.sessionCode}
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
      <h1>${icons.vote(' class="icon icon-md"')} Vote Coloré</h1>
      <div class="session-code-display valid">
        ${icons.check(' class="icon icon-sm text-success"')} ${state.sessionCode}
      </div>
    </header>
    <div class="card">
      <div class="voted-state">
        <div class="voted-icon">${icons.check(' class="icon icon-xl"')}</div>
        <div class="voted-title">Vote enregistré !</div>
        <div class="voted-subtitle">${selectedNames}</div>
        <button type="button" class="btn btn-secondary btn-small" id="changeVoteBtn" style="margin-top: 1rem;">
          ${icons.pencil(' class="icon icon-sm"')} Modifier mon vote
        </button>
      </div>
    </div>
  `
}

// Rendu vote terminé
function renderClosedHTML() {
  return `
    <header class="header">
      <h1>${icons.vote(' class="icon icon-md"')} Vote Coloré</h1>
      <div class="session-code-display valid">
        ${icons.check(' class="icon icon-sm text-success"')} ${state.sessionCode}
      </div>
    </header>
    <div class="card">
      <div class="vote-closed-state">
        <div class="closed-icon">${icons.stop(' class="icon icon-xl"')}</div>
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

  // Formulaire d'édition du nom
  const editNameForm = document.getElementById('editNameForm')
  if (editNameForm) {
    editNameForm.addEventListener('submit', handleEditName)
  }

  // Bouton d'édition du nom
  const editNameBtn = document.getElementById('editNameBtn')
  if (editNameBtn) {
    editNameBtn.addEventListener('click', () => {
      state.prenomEdit = true
      render()
    })
  }

  // Bouton annuler l'édition
  const cancelEditBtn = document.getElementById('cancelEditName')
  if (cancelEditBtn) {
    cancelEditBtn.addEventListener('click', () => {
      state.prenomEdit = false
      render()
    })
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

  // Bouton modifier le vote
  const changeVoteBtn = document.getElementById('changeVoteBtn')
  if (changeVoteBtn) {
    changeVoteBtn.addEventListener('click', () => {
      state.appState = AppState.VOTING
      state.hasVoted = false
      render()
    })
  }
}

// Gérer la connexion
function handleJoin(e) {
  e.preventDefault()

  const prenomInput = document.getElementById('prenom')
  const codeInput = document.getElementById('sessionCode')
  const prenom = prenomInput.value.trim()
  const code = codeInput.value.trim()

  // Validation du prénom
  if (prenom.length < 2) {
    prenomInput.classList.add('error')
    showError('Le prénom doit contenir au moins 2 caractères')
    return
  }

  // Validation du code (4 chiffres)
  if (!/^\d{4}$/.test(code)) {
    codeInput.classList.add('error')
    showError('Le code doit contenir 4 chiffres')
    return
  }

  prenomInput.classList.remove('error')
  codeInput.classList.remove('error')

  state.prenom = prenom
  state.sessionCode = code

  // Sauvegarder le prénom
  localStorage.setItem('vote_stagiaire_prenom', prenom)

  connectWebSocket(code)
}

// Gérer l'édition du nom
function handleEditName(e) {
  e.preventDefault()

  const input = document.getElementById('editPrenom')
  const newPrenom = input.value.trim()

  if (newPrenom.length < 2) {
    input.classList.add('error')
    showError('Le prénom doit contenir au moins 2 caractères')
    return
  }

  input.classList.remove('error')

  state.prenom = newPrenom
  localStorage.setItem('vote_stagiaire_prenom', newPrenom)

  // Envoyer la mise à jour au serveur
  send({
    type: 'update_name',
    name: newPrenom
  })

  // Fermer le modal
  state.prenomEdit = false
  render()
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
    colors: Array.from(state.selectedColors)
  })
}

// Démarrage
init()
