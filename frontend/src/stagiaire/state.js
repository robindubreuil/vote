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
 * @property {string} prenom
 * @property {boolean} prenomEdit
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
  prenom: '',
  prenomEdit: false,
  gameEnabled: false,
  gamePlaying: false,
  competitive: false,
  allowBlank: false,
  voteScore: 0,
  totalScore: 0,
  rank: 0,
  totalStagiaires: 0,
  revealed: false
}
