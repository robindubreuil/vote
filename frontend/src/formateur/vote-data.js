// Pure vote-data helpers, extracted from `utils.js` so the render
// layer can depend on them without forming an import cycle.
//
// Background (F14): `utils.js` updates the live vote panel and used to
// reach into `renderers.js` via a dynamic import to refresh the
// combinations and per-stagiaire lists. `renderers.js` in turn imported
// the data helpers from `utils.js`. Vite warned that the dynamic import
// was "ineffective" — `renderers.js` is statically imported elsewhere,
// so it never split into its own chunk, and a failed load would silently
// stall the panel. Moving the pure data helpers here lets `utils.js`
// statically import the render layer and break the cycle cleanly.
//
// These helpers only read `state` — no DOM, no rendering, no side
// effects — which keeps them trivially unit-testable.

import { COLORS } from '@shared/colors.js'
import { t } from '@shared/strings.js'
import { state } from './state.js'

/**
 * Calculate vote counts per color
 * @returns {Object.<string, number>} Counts by color ID
 */
export function getColorCounts() {
  const counts = {}
  state.stagiaires.forEach((s) => {
    if (s.vote) {
      s.vote.forEach((colorId) => {
        counts[colorId] = (counts[colorId] || 0) + 1
      })
    }
  })
  return counts
}

/**
 * Calculate vote combinations
 * @returns {Array<{colors: string[], count: number}>} Sorted combinations by count desc
 */
export function getCombinations() {
  const comboMap = new Map()

  state.stagiaires.forEach((s) => {
    if (s.vote && s.vote.length > 0) {
      const key = s.vote.slice().sort().join('+')
      comboMap.set(key, (comboMap.get(key) || 0) + 1)
    }
  })

  return Array.from(comboMap.entries())
    .map(([key, count]) => ({
      colors: key ? key.split('+') : [],
      count
    }))
    .sort((a, b) => b.count - a.count)
}

/**
 * Sort stagiaires by vote status and name
 * Non-voters first, then by combination popularity, then by name
 * @param {Array} stagiaires
 * @returns {Array} Sorted array
 */
export function sortStagiaires(stagiaires) {
  // Calculate popularity of each combination (among voters)
  const comboPopularity = new Map()
  stagiaires.forEach((s) => {
    if (s.vote && s.vote.length > 0) {
      const key = s.vote.slice().sort().join('+')
      comboPopularity.set(key, (comboPopularity.get(key) || 0) + 1)
    }
  })

  return [...stagiaires].sort((a, b) => {
    const aHasVoted = a.vote && a.vote.length > 0
    const bHasVoted = b.vote && b.vote.length > 0

    // Non-voters first
    if (aHasVoted !== bHasVoted) {
      return aHasVoted ? 1 : -1
    }

    // If both voted, sort by combination popularity
    if (aHasVoted && bHasVoted) {
      const keyA = a.vote.slice().sort().join('+')
      const keyB = b.vote.slice().sort().join('+')
      const popularityA = comboPopularity.get(keyA) || 0
      const popularityB = comboPopularity.get(keyB) || 0

      if (popularityB !== popularityA) {
        return popularityB - popularityA
      }
    }

    // Same status: sort by name
    const nameA = (a.name || t.common.anonymous).toLowerCase()
    const nameB = (b.name || t.common.anonymous).toLowerCase()
    return nameA.localeCompare(nameB)
  })
}

// Re-export COLORS so existing callers that destructured it from
// `utils.js` keep working without touching their import sites.
export { COLORS }
