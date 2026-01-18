import './style.css'
import { icons } from '../../shared/icons.js'
import { COLORS, escapeHtml } from '../../shared/colors.js'
import { VoteClient } from '../../shared/websocket-client.js'
import { renderFooterHTML, renderSessionCodeButton, showError } from '../../shared/ui.js'
import { getWebSocketURL } from '../../shared/config.js'
import { validateName, validateSessionCode } from '../../shared/validation.js'

// Configuration de l'API WebSocket
const WS_URL = getWebSocketURL()

// États de l'application
const AppState = {
  JOINING: 'joining',      // Saisie du code session
  WAITING: 'waiting',      // En attente du prochain vote
  VOTING: 'voting',        // Vote en cours
  VOTED: 'voted',          // Vote enregistré
  CLOSED: 'closed'         // Vote terminé par le formateur
}

// État de l'application
/**
 * @type {Object}
 * @property {string} appState
 * @property {string} sessionCode
 * @property {boolean} connected
 * @property {Array<string>} availableColors
 * @property {Object.<string, string>} colorLabels
 * @property {boolean} multipleChoice
 * @property {Set<string>} selectedColors
 * @property {boolean} hasVoted
 * @property {string|null} stagiaireId
 * @property {string} prenom
 * @property {boolean} prenomEdit
 */
const state = {
  appState: AppState.JOINING,
  sessionCode: '',
  connected: false,
  availableColors: [],
  colorLabels: {}, // Custom labels for colors
  multipleChoice: false,
  selectedColors: new Set(),
  hasVoted: false,
  stagiaireId: null,
  prenom: '',
  prenomEdit: false // Mode édition du nom
}

// Éléments DOM
let app = null
let client = null

// Initialisation
function init() {
  app = document.getElementById('app')

  // Récupérer l'ID du stagiaire (généré par le serveur)
  const savedId = sessionStorage.getItem('vote_stagiaire_id')
  if (savedId) {
    state.stagiaireId = savedId
  }

  // Récupérer le prénom s'il existe
  const savedPrenom = localStorage.getItem('vote_stagiaire_prenom')
  if (savedPrenom) {
    state.prenom = savedPrenom
  }

  // Vérifier si on a déjà un code session enregistré
  let savedCode = sessionStorage.getItem('vote_session_code')
  
  // Check URL params for session code (override saved code if present)
  const urlParams = new URLSearchParams(window.location.search)
  const urlSession = urlParams.get('session')
  if (urlSession && validateSessionCode(urlSession) === null) {
      savedCode = urlSession
  }
  
  if (savedCode) {
    state.sessionCode = savedCode
  }

  // Initialiser la structure de base (Header, Main, Footer)
  renderLayout()

  // Initialiser le client WebSocket
  initClient()

  updateView()

  // Auto-connect if we have both code and name
  if (state.sessionCode && state.prenom && !state.connected) {
    connectToSession(state.sessionCode)
  }
}

// Initialisation du client WebSocket
function initClient() {
  client = new VoteClient(WS_URL, {
    onStatusChange: (connected) => {
      state.connected = connected
      // Re-render to update session code connection status
      if (state.appState !== AppState.JOINING) {
        render()
      }
    },
    onOpen: () => {
      // Si on a un code session et un prénom, on tente de rejoindre
      if (state.sessionCode && state.prenom) {
        client.send({
          type: 'stagiaire_join',
          sessionCode: state.sessionCode,
          name: state.prenom
        })
      }
    },
    onMessage: (msg) => {
      handleMessage(msg)
    }
  })
}

// Connexion à une session
function connectToSession(code) {
  state.sessionCode = code
  // Si le client n'est pas initialisé, on le fait
  if (!client) {
    initClient()
  }

  // On lance la connexion (cela fermera l'ancienne si elle existe)
  client.connect()
}

// Gérer les messages reçus
function handleMessage(msg) {
  switch (msg.type) {
    case 'session_joined':
      // Connexion réussie à la session
      state.sessionCode = msg.sessionCode
      // Store the server-generated stagiaireId
      if (msg.stagiaireId) {
        state.stagiaireId = msg.stagiaireId
        sessionStorage.setItem('vote_stagiaire_id', msg.stagiaireId)
      }
      state.appState = AppState.WAITING
      sessionStorage.setItem('vote_session_code', msg.sessionCode)
      render()
      break

    case 'error':
      showError(msg.message || 'Erreur de connexion')
      break

    case 'vote_started':
      // Nouveau vote lancé
      state.availableColors = msg.colors || []
      state.multipleChoice = msg.multipleChoice || false
      state.colorLabels = msg.labels || {}
      state.selectedColors.clear()

      // Restore existing vote if rejoining
      if (msg.existingVote && Array.isArray(msg.existingVote)) {
        msg.existingVote.forEach(colorId => state.selectedColors.add(colorId))
        state.hasVoted = true
        state.appState = AppState.VOTED
      } else {
        state.hasVoted = false
        state.appState = AppState.VOTING
      }
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

    case 'name_updated':
      // Confirmation de mise à jour du nom
      state.prenomEdit = false
      render()
      break
  }
}

// Structure globale statique
function renderLayout() {
  app.innerHTML = `
    <div class="container" id="main-container"></div>
    ${renderFooterHTML()}
  `
}

// Mise à jour de la vue principale
function updateView() {
  const container = document.getElementById('main-container')
  if (!container) return

  // Sauvegarde du focus
  const activeElementId = document.activeElement?.id
  
  // Rendu du contenu en fonction de l'état
  let contentHTML = ''
  
  if (state.prenomEdit) {
    contentHTML = renderEditNameHTML()
  } else {
    switch (state.appState) {
      case AppState.JOINING:
        contentHTML = renderJoinHTML()
        break
      case AppState.WAITING:
        contentHTML = renderWaitingHTML()
        break
      case AppState.VOTING:
        contentHTML = renderVotingHTML()
        break
      case AppState.VOTED:
        contentHTML = renderVotedHTML()
        break
      case AppState.CLOSED:
        contentHTML = renderClosedHTML()
        break
    }
  }

  // On remplace le contenu
  container.innerHTML = contentHTML

  // Réattacher les écouteurs
  attachEventListeners()

  // Restauration du focus (best effort)
  if (activeElementId) {
    const el = document.getElementById(activeElementId)
    if (el) {
      el.focus()
      // Place cursor at end if it's an input
      if (el.tagName === 'INPUT' && el.type === 'text') {
        const val = el.value
        el.value = ''
        el.value = val
      }
    }
  }
}

// Alias pour compatibilité interne si nécessaire, mais on préfère updateView
function render() {
  updateView()
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
            value="${escapeHtml(state.prenom)}"
            autocomplete="name"
            autocapitalize="words"
            maxlength="16"
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
            value="${escapeHtml(state.sessionCode)}"
            autocomplete="off"
          />
        </div>
        <div class="error-message" role="alert"></div>
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
      <div class="header-right">
        ${renderSessionCodeButton(state.sessionCode, state.connected)}
      </div>
    </header>
    <div class="card">
      <div class="waiting-state">
        <div class="waiting-icon">${icons.hourglass(' class="icon icon-xl"')}</div>
        <div class="waiting-text">En attente du prochain vote...</div>
        <div class="waiting-name">Bonjour, <strong>${escapeHtml(state.prenom)}</strong> !</div>
        <button type="button" class="btn btn-secondary btn-small" id="editNameBtn" aria-label="Modifier mon nom">
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
            value="${escapeHtml(state.prenom)}"
            autocomplete="name"
            autocapitalize="words"
            maxlength="16"
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
      <div class="header-right">
        ${renderSessionCodeButton(state.sessionCode, state.connected)}
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
      ${activeColors.map(color => {
        const label = state.colorLabels[color.id] || color.name
        return `
        <button
          type="button"
          class="vote-button bg-${color.id} ${state.selectedColors.has(color.id) ? 'selected' : ''}"
          data-color="${color.id}"
          aria-pressed="${state.selectedColors.has(color.id)}"
          aria-label="${label}"
        >
          ${escapeHtml(label)}
        </button>
        `
      }).join('')}
    </div>
  `
}

// Rendu pour choix multiple
function renderMultipleChoiceHTML(activeColors) {
  return `
    <div class="vote-grid">
      ${activeColors.map(color => {
        const label = state.colorLabels[color.id] || color.name
        return `
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
          ${escapeHtml(label)}
          <span class="check-indicator"></span>
        </label>
        `
      }).join('')}
    </div>
    <button type="button" class="btn btn-success btn-large" id="submitVote" ${state.selectedColors.size === 0 ? 'disabled' : ''}>
      Valider mon vote
    </button>
  `
}

// Rendu après vote
function renderVotedHTML() {
  const selectedNames = Array.from(state.selectedColors).map(id => {
    return state.colorLabels[id] || COLORS.find(c => c.id === id)?.name || id
  }).join(' + ')

  return `
    <header class="header">
      <h1>${icons.vote(' class="icon icon-md"')} Vote Coloré</h1>
      <div class="header-right">
        ${renderSessionCodeButton(state.sessionCode, state.connected)}
      </div>
    </header>
    <div class="card">
      <div class="voted-state">
        <div class="voted-icon">${icons.check(' class="icon icon-xl"')}</div>
        <div class="voted-title">Vote enregistré !</div>
        <div class="voted-subtitle">${escapeHtml(selectedNames)}</div>
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
      <div class="header-right">
        ${renderSessionCodeButton(state.sessionCode, state.connected)}
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
  // Input Binding (plus explicite pour éviter les pertes d'état)
  const prenomInput = document.getElementById('prenom')
  if (prenomInput) {
    prenomInput.addEventListener('input', (e) => {
      state.prenom = e.target.value
    })
  }

  const editPrenomInput = document.getElementById('editPrenom')
  if (editPrenomInput) {
    editPrenomInput.addEventListener('input', (e) => {
      state.prenom = e.target.value
    })
  }

  const sessionCodeInput = document.getElementById('sessionCode')
  if (sessionCodeInput) {
    sessionCodeInput.addEventListener('input', (e) => {
      state.sessionCode = e.target.value
    })
  }

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
    // Accessibility: Activate on Enter/Space
    btn.addEventListener('keydown', (e) => {
      if (e.key === 'Enter' || e.key === ' ') {
        e.preventDefault()
        handleSingleChoiceVote(e)
      }
    })
  })

  // Checkboxes (choix multiple)
  document.querySelectorAll('.vote-checkbox').forEach(checkbox => {
    checkbox.addEventListener('change', handleCheckboxChange)
    // Accessibility for label
    const label = document.querySelector(`label[for="${checkbox.id}"]`)
    if (label) {
      label.setAttribute('tabindex', '0')
      label.addEventListener('keydown', (e) => {
        if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault()
          checkbox.checked = !checkbox.checked
          // Trigger change event manually
          const event = new Event('change')
          checkbox.dispatchEvent(event)
        }
      })
    }
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

  // Bouton quitter la session
  const leaveSessionBtn = document.getElementById('leaveSessionBtn')
  if (leaveSessionBtn) {
    leaveSessionBtn.addEventListener('click', leaveSession)
  }
}

// Gérer la connexion
function handleJoin(e) {
  e.preventDefault()

  const prenomInput = document.getElementById('prenom')
  const codeInput = document.getElementById('sessionCode')
  const prenom = prenomInput.value.trim()
  const code = codeInput.value.trim()

  // Validation
  const nameError = validateName(prenom)
  if (nameError) {
    prenomInput.classList.add('error')
    showError(nameError)
    return
  }

  const codeError = validateSessionCode(code)
  if (codeError) {
    codeInput.classList.add('error')
    showError(codeError)
    return
  }

  prenomInput.classList.remove('error')
  codeInput.classList.remove('error')

  state.prenom = prenom
  state.sessionCode = code

  // Sauvegarder le prénom
  localStorage.setItem('vote_stagiaire_prenom', prenom)

  connectToSession(code)
}

// Gérer l'édition du nom
function handleEditName(e) {
  e.preventDefault()

  const input = document.getElementById('editPrenom')
  const newPrenom = input.value.trim()

  // Validation du prénom
  const nameError = validateName(newPrenom)
  if (nameError) {
    input.classList.add('error')
    showError(nameError)
    return
  }

  input.classList.remove('error')

  state.prenom = newPrenom
  localStorage.setItem('vote_stagiaire_prenom', newPrenom)

  // Envoyer la mise à jour au serveur
  client.send({
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
    label?.classList.add('selected')
  } else {
    state.selectedColors.delete(colorId)
    label?.classList.remove('selected')
  }

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
  client.send({
    type: 'vote',
    colors: Array.from(state.selectedColors)
  })
}

function leaveSession() {
  if (confirm('Voulez-vous vraiment quitter cette session ?')) {
    sessionStorage.removeItem('vote_session_code')
    state.sessionCode = ''
    state.appState = AppState.JOINING
    state.connected = false
    if (client) {
      client.close() // Close WebSocket connection
      client = null
    }
    render()
  }
}

// Démarrage
init()