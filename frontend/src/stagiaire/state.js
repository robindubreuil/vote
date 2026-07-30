/**
 * Application state management
 */

// États de l'application
export const AppState = {
  JOINING: 'joining', // Saisie du code session
  WAITING: 'waiting', // En attente du prochain vote
  VOTING: 'voting', // Vote en cours
  VOTED: 'voted', // Vote enregistré
  CLOSED: 'closed' // Vote terminé par le formateur
}

/**
 * État de l'application
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
 * @property {string|null} reclaimToken Per-stagiaire secret proving
 *   ownership of stagiaireId on reconnect (S6/S12).
 * @property {string} prenom
 * @property {boolean} prenomEdit
 * @property {string|null} pendingRename R8: the in-flight rename value.
 *   Set by handleEditName when sending `update_name`, cleared by
 *   `name_updated` on success or by the `error` handler on rejection.
 *   Used to route a server-side name-collision rejection into the
 *   edit-name modal's inline error slot instead of a generic toast.
 */
export const state = {
  appState: AppState.JOINING,
  sessionCode: '',
  connected: false,
  availableColors: [],
  colorLabels: {},
  multipleChoice: false,
  selectedColors: new Set(),
  hasVoted: false,
  stagiaireId: null,
  reclaimToken: null,
  prenom: '',
  prenomEdit: false,
  pendingRename: null,
  gameEnabled: false,
  gamePlaying: false,
  competitive: false,
  allowBlank: false,
  voteScore: 0,
  totalScore: 0,
  gameScore: 0,
  rank: 0,
  totalStagiaires: 0,
  revealed: false
}

/**
 * Reset every session-scoped field to its initial value. Symmetric with
 * `resetTrainerState()` on the formateur side. Used when the stagiaire
 * leaves a session so that joining another session starts from a clean
 * slate — no leaked scoreboard, no leftover edit-name modal, no stale
 * reveal state from the prior session.
 *
 * `prenom` is intentionally preserved: it is the user's preferred display
 * name and is reused on the join form for the next session.
 */
export function resetStagiaireState() {
  state.sessionCode = ''
  state.appState = AppState.JOINING
  state.connected = false
  state.hasVoted = false
  state.selectedColors.clear()
  state.availableColors = []
  state.colorLabels = {}
  state.multipleChoice = false
  state.gameEnabled = false
  state.gamePlaying = false
  state.stagiaireId = null
  state.reclaimToken = null
  state.prenomEdit = false
  state.pendingRename = null
  state.competitive = false
  state.allowBlank = false
  state.voteScore = 0
  state.totalScore = 0
  state.gameScore = 0
  state.rank = 0
  state.totalStagiaires = 0
  state.revealed = false
}
