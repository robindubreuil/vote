import { COLORS } from '../../../shared/colors.js'

export const state = {
  sessionCode: null,
  connected: false,
  connecting: false,
  voteState: 'idle',
  selectedColors: new Set(COLORS.slice(0, 3).map(c => c.id)),
  colorLabels: {},
  multipleChoice: false,
  connectedCount: 0,
  stagiaires: [],
  voteStartTime: null,
  timerInterval: null
}
