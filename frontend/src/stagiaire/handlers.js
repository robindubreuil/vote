import { validateName, validateSessionCode } from '../../../shared/validation.js'
import { showError } from '../../../shared/ui.js'
import { loader } from '../../../shared/icons.js'
import { state, AppState } from './state.js'
import { render } from './renderers.js'
import { getClient } from './websocket.js'

// connectToSession function - will be set by main.js
let connectToSessionFn = null

/**
 * Set the connectToSession function (called from main.js)
 */
export function setConnectToSession(fn) {
  connectToSessionFn = fn
}

/**
 * Handle the join session form submission
 */
export function handleJoin(e) {
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

  connectToSessionFn(code)
}

/**
 * Handle the edit name form submission
 */
export function handleEditName(e) {
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

  const client = getClient()
  if (client) {
    client.send({
      type: 'update_name',
      name: newPrenom
    })
  }

  state.prenomEdit = false
  render()
}

/**
 * Handle single choice vote button click
 */
export function handleSingleChoiceVote(e) {
  const colorId = e.target.dataset.color

  // Désélectionner les autres
  state.selectedColors.clear()
  state.selectedColors.add(colorId)

  // Envoyer le vote immédiatement
  submitVote(e.target)
}

/**
 * Handle checkbox change for multiple choice voting
 */
export function handleCheckboxChange(e) {
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

/**
 * Handle the submit vote button click
 */
export function handleSubmitVote() {
  if (state.selectedColors.size > 0) {
    submitVote()
  }
}

/**
 * Submit the vote to the server
 */
export function submitVote(triggerButton = null) {
  const client = getClient()
  if (!client) {
    showError("Erreur de connexion")
    return
  }

  const btn = triggerButton || document.getElementById('submitVote')
  const originalContent = btn ? btn.innerHTML : ''

  if (btn) {
    btn.disabled = true
    if (btn.id === 'submitVote') {
       btn.innerHTML = `${loader(' class="icon icon-md spin"')} Envoi...`
    } else {
       btn.style.opacity = '0.7'
       btn.style.cursor = 'wait'
    }
  }

  const success = client.send({
    type: 'vote',
    colors: Array.from(state.selectedColors),
    stagiaireId: state.stagiaireId || undefined
  })

  if (!success) {
    if (btn) {
      btn.disabled = false
      btn.innerHTML = originalContent
      btn.style.opacity = ''
      btn.style.cursor = ''
    }
    showError("Erreur de connexion")
  }
}

/**
 * Leave the current session
 */
export function leaveSession() {
  if (confirm('Voulez-vous vraiment quitter cette session ?')) {
    sessionStorage.removeItem('vote_session_code')
    sessionStorage.removeItem('vote_stagiaire_id')
    state.sessionCode = ''
    state.appState = AppState.JOINING
    state.connected = false
    state.hasVoted = false
    state.selectedColors.clear()
    state.availableColors = []
    state.colorLabels = {}
    state.multipleChoice = false
    state.stagiaireId = null
    const client = getClient()
    if (client) {
      client.close()
    }
    render()
  }
}

/**
 * Handle keyboard shortcuts
 * - Escape: Cancel current action (e.g., exit edit mode)
 * - Enter: Submit forms (works natively for forms)
 */
export function handleKeyPress(event) {
  // Escape key - cancel edit mode
  if (event.key === 'Escape') {
    if (state.prenomEdit) {
      state.prenomEdit = false
      render()
      event.preventDefault()
    }
  }
}
