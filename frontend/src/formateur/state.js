import { COLORS } from '@shared/colors.js'

export const state = {
  sessionCode: null,
  connected: false,
  connecting: false,
  everConnected: false,
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
