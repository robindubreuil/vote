// localStorage-backed high-score store for the stagiaire mini-game.
// Keyed per device (not per session) so a trainee's personal best
// follows them across sessions. Designed to fail silently — private
// mode or quota errors never crash the game. Reads go through
// safeLocalGet because Firefox "never remember history" mode and some
// embedded WebViews throw SecurityError on getItem (not just setItem).

import { safeLocalGet, safeLocalRemove } from './utils/safe-storage.js'

const HIGH_SCORE_KEY = 'vote:game:highscore'
const SEEN_RULES_KEY = 'vote:game:seenRules'
const STREAK_KEY = 'vote:game:streak'

export function loadHighScore() {
  const raw = safeLocalGet(HIGH_SCORE_KEY)
  if (!raw) return 0
  const parsed = Number.parseInt(raw, 10)
  return Number.isFinite(parsed) && parsed > 0 ? parsed : 0
}

export function hasSeenRules() {
  return safeLocalGet(SEEN_RULES_KEY) === '1'
}

export function markRulesSeen() {
  try {
    localStorage.setItem(SEEN_RULES_KEY, '1')
  } catch {
    // ignore
  }
}

/**
 * Persist the new score only if it beats the previous best.
 * @returns {boolean} true if the score was a new record
 */
export function saveHighScore(score) {
  const n = Math.floor(Number(score) || 0)
  if (n <= 0) return false
  const prev = loadHighScore()
  if (n <= prev) return false
  try {
    localStorage.setItem(HIGH_SCORE_KEY, String(n))
  } catch {
    return false
  }
  return true
}

export function resetHighScore() {
  try {
    localStorage.removeItem(HIGH_SCORE_KEY)
  } catch {
    // ignore
  }
}

export function loadStreak() {
  const raw = safeLocalGet(STREAK_KEY)
  const parsed = Number.parseInt(raw, 10)
  return Number.isFinite(parsed) && parsed > 0 ? parsed : 0
}

export function saveStreak(n) {
  try {
    localStorage.setItem(STREAK_KEY, String(Math.max(0, Math.floor(n))))
  } catch {
    // ignore
  }
}

export function _resetForTests() {
  safeLocalRemove(HIGH_SCORE_KEY)
  safeLocalRemove(SEEN_RULES_KEY)
  safeLocalRemove(STREAK_KEY)
}

export const _constants = { HIGH_SCORE_KEY, SEEN_RULES_KEY, STREAK_KEY }
