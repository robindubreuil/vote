import { COLORS } from '@shared/colors.js'

export const state = {
  sessionCode: null,
  connected: false,
  connecting: false,
  everConnected: false,
  // F25: true once the WS client has permanently given up
  // (maxReconnectAttempts reached or a 4xxx permanent close). Drives the
  // banner's "rechargez la page" state instead of "Reconnexion…".
  permanentlyClosed: false,
  voteState: 'idle',
  selectedColors: new Set(COLORS.slice(0, 3).map((c) => c.id)),
  colorLabels: {},
  multipleChoice: false,
  gameEnabled: false,
  competitive: false,
  allowBlank: false,
  correctColors: new Set(),
  revealed: false,
  scoreboard: [],
  connectedCount: 0,
  stagiaires: [],
  voteStartTime: null,
  timerInterval: null,
  presetSaving: false,
  lastConfigApplied: false
}

export function resetTrainerState() {
  state.sessionCode = null
  state.connected = false
  state.everConnected = false
  state.permanentlyClosed = false
  state.connecting = false
  state.connectedCount = 0
  state.stagiaires = []
  state.voteState = 'idle'
  state.voteStartTime = null
  state.selectedColors = new Set(COLORS.slice(0, 3).map((c) => c.id))
  state.colorLabels = {}
  state.multipleChoice = false
  state.gameEnabled = false
  state.competitive = false
  state.allowBlank = false
  state.correctColors = new Set()
  state.revealed = false
  state.scoreboard = []
  state.presetSaving = false
  state.lastConfigApplied = false
}
