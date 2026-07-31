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
 *
 * F32: distinguishes "not a record" from "record but storage failed" so
 * the caller can still celebrate the record (badge) while surfacing that
 * the save didn't land (toast). Previously a quota throw returned false
 * indistinguishably from a non-record, so a genuine best score was
 * silently framed as "not your best" and the HUD kept showing the old
 * value.
 *
 * @returns {{ isRecord: boolean, persisted: boolean }} isRecord is true
 *   when the score beats the previous best; persisted is false when the
 *   write was rejected (quota / private mode) even though it was a record.
 */
export function saveHighScore(score) {
  const n = Math.floor(Number(score) || 0)
  if (n <= 0) return { isRecord: false, persisted: false }
  const prev = loadHighScore()
  if (n <= prev) return { isRecord: false, persisted: false }
  try {
    localStorage.setItem(HIGH_SCORE_KEY, String(n))
  } catch {
    return { isRecord: true, persisted: false }
  }
  return { isRecord: true, persisted: true }
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

// F17: test-only surface is gated behind `import.meta.env.DEV`. In
// production builds Vite statically replaces the flag with `false` and
// Rollup's dead-code elimination collapses this to `export const _test =
// null`, so the helper (a) is not in the shipped bundle and (b) cannot
// be invoked from the console to wipe a user's high score. Vitest runs
// in dev mode, so tests still see the live object.
export const _test = import.meta.env.DEV
  ? {
      resetForTests() {
        safeLocalRemove(HIGH_SCORE_KEY)
        safeLocalRemove(SEEN_RULES_KEY)
        safeLocalRemove(STREAK_KEY)
      },
      constants: { HIGH_SCORE_KEY, SEEN_RULES_KEY, STREAK_KEY }
    }
  : null
