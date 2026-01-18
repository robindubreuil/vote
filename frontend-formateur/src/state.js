/**
 * Application state management
 */

import { COLORS } from '../../shared/colors.js'

/**
 * @typedef {Object} Stagiaire
 * @property {string} id
 * @property {string} name
 * @property {boolean} connected
 * @property {string[]} [vote]
 */

/**
 * @type {Object}
 * @property {string|null} sessionCode
 * @property {boolean} connected
 * @property {boolean} connecting
 * @property {'idle'|'active'|'closed'} voteState
 * @property {Set<string>} selectedColors
 * @property {Object.<string, string>} colorLabels
 * @property {boolean} multipleChoice
 * @property {number} connectedCount
 * @property {Stagiaire[]} stagiaires
 * @property {number|null} voteStartTime
 * @property {number|null} timerInterval
 */
export const state = {
  sessionCode: null,
  connected: false,
  connecting: false,
  voteState: 'idle', // idle, active, closed
  selectedColors: new Set(COLORS.slice(0, 3).map(c => c.id)), // Default: first 3 colors (rouge, vert, bleu)
  colorLabels: {}, // Custom labels for colors { colorId: "Custom Label" }
  multipleChoice: false,
  connectedCount: 0,
  stagiaires: [], // [{ id, name, connected, vote: [] }] All stagiaires
  voteStartTime: null,
  timerInterval: null
}
