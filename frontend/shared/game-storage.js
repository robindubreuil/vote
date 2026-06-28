// localStorage-backed high-score store for the stagiaire mini-game.
// Keyed per device (not per session) so a trainee's personal best
// follows them across sessions. Designed to fail silently — private
// mode or quota errors never crash the game.

const HIGH_SCORE_KEY = 'vote:game:highscore'

export function loadHighScore() {
  const raw = localStorage.getItem(HIGH_SCORE_KEY)
  if (!raw) return 0
  const parsed = Number.parseInt(raw, 10)
  return Number.isFinite(parsed) && parsed > 0 ? parsed : 0
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

export function _resetForTests() {
  try {
    localStorage.removeItem(HIGH_SCORE_KEY)
  } catch {
    // ignore
  }
}

export const _constants = { HIGH_SCORE_KEY }
