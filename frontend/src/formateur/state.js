import { COLORS } from '@shared/colors.js'

export const state = {
  sessionCode: null,
  connected: false,
  connecting: false,
  // True once the WS has opened at least once. Lets the reconnection banner
  // distinguish "never connected" (initial load, no banner) from "dropped
  // mid-session" (banner).
  everConnected: false,
  voteState: 'idle',
  selectedColors: new Set(COLORS.slice(0, 3).map((c) => c.id)),
  colorLabels: {},
  multipleChoice: false,
  gameEnabled: false,
  connectedCount: 0,
  stagiaires: [],
  voteStartTime: null,
  timerInterval: null,
  // Preset UI state — not persisted. Saving form visibility is local to the
  // current config card; if the user navigates away we want it gone.
  presetSaving: false,
  // Ensures we only autoload last-config once per page lifecycle, so an
  // explicit reset doesn't get silently overridden by a re-render.
  lastConfigApplied: false
}
