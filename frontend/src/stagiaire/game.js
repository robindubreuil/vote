// Le Code Couleur — the stagiaire mini-game shown between votes.
//
// Mastermind reimagined with vote colors: the trainer's selected palette
// is the alphabet of a secret N-color code. The trainee has M attempts
// to crack it. Each guess gets peg feedback:
//
//   • black peg — right color, right slot
//   • white peg — right color, wrong slot
//
// The class is pure state — no rAF, no DOM. The owning handlers module
// renders from getBoardState() and dispatches user actions via the
// place / clear / submit / newGame methods.

import { COLORS } from '@shared/colors.js'
import { loadHighScore, saveHighScore } from '@shared/game-storage.js'

const DEFAULT_CODE_LENGTH = 4
const DEFAULT_MAX_ATTEMPTS = 8
const POINTS_PER_REMAINING_ATTEMPT = 100
const TIME_BONUS_UNDER_30S = 80
const TIME_BONUS_UNDER_60S = 40

function defaultPalette() {
  return COLORS.slice(0, 6).map((c) => ({ id: c.id, color: c.color, name: c.name }))
}

function sanitizePalette(input) {
  if (!Array.isArray(input) || input.length < 2) return defaultPalette()
  const known = new Set(COLORS.map((c) => c.id))
  const filtered = input
    .filter((c) => c && typeof c.id === 'string' && known.has(c.id))
    .map((c) => {
      const found = COLORS.find((x) => x.id === c.id)
      return { id: c.id, color: found.color, name: found.name }
    })
  // Need at least 2 distinct colors for a meaningful puzzle. If the vote
  // used fewer, pad from the full palette so the code is still solvable.
  if (filtered.length < 2) return defaultPalette()
  return filtered
}

function makeSecret(palette, length) {
  const secret = []
  const allowed = palette.length
  for (let i = 0; i < length; i++) {
    secret.push(palette[Math.floor(Math.random() * allowed)].id)
  }
  return secret
}

/**
 * Compute Mastermind peg feedback for a guess against a secret.
 *
 * Algorithm: two passes.
 *   1. Black pass — count exact slot matches; mark those slots consumed.
 *   2. White pass — for each remaining guess color, find an unmatched
 *      secret slot with the same color.
 *
 * Returns { black: number, white: number }.
 */
export function computePegs(guess, secret) {
  const n = Math.min(guess.length, secret.length)
  const guessConsumed = new Array(n).fill(false)
  const secretConsumed = new Array(n).fill(false)
  let black = 0
  let white = 0

  // Black pass
  for (let i = 0; i < n; i++) {
    if (guess[i] === secret[i]) {
      black += 1
      guessConsumed[i] = true
      secretConsumed[i] = true
    }
  }

  // White pass
  for (let i = 0; i < n; i++) {
    if (guessConsumed[i]) continue
    for (let j = 0; j < n; j++) {
      if (secretConsumed[j]) continue
      if (guess[i] === secret[j]) {
        white += 1
        secretConsumed[j] = true
        break
      }
    }
  }

  return { black, white }
}

export class Mastermind {
  /**
   * @param {Object} opts
   * @param {Array<{id:string,color?:string,name?:string}>} [opts.colors]
   * @param {number} [opts.codeLength] default 4
   * @param {number} [opts.maxAttempts] default 8
   */
  constructor(opts = {}) {
    this.palette = sanitizePalette(opts.colors)
    this.codeLength = Math.max(2, Math.min(6, Number(opts.codeLength) || DEFAULT_CODE_LENGTH))
    this.maxAttempts = Math.max(4, Math.min(12, Number(opts.maxAttempts) || DEFAULT_MAX_ATTEMPTS))
    this._newRound()
  }

  _newRound() {
    this.secret = makeSecret(this.palette, this.codeLength)
    this.guesses = []
    this.pegs = []
    this.currentRow = new Array(this.codeLength).fill(null)
    this.status = 'playing' // 'playing' | 'won' | 'lost'
    this.startedAt = Date.now()
    this.solvedAt = null
    this.score = null
  }

  /** Place a color in the first empty slot of the current row. No-op if row is full. */
  place(colorId) {
    if (this.status !== 'playing') return false
    if (!this.palette.some((c) => c.id === colorId)) return false
    const idx = this.currentRow.indexOf(null)
    if (idx === -1) return false
    this.currentRow[idx] = colorId
    return true
  }

  /** Remove the color at slotIndex, or the last filled slot if no index. */
  clear(slotIndex) {
    if (this.status !== 'playing') return false
    if (typeof slotIndex === 'number') {
      if (slotIndex < 0 || slotIndex >= this.codeLength) return false
      if (this.currentRow[slotIndex] === null) return false
      this.currentRow[slotIndex] = null
      return true
    }
    // No index → clear the last filled slot
    for (let i = this.codeLength - 1; i >= 0; i--) {
      if (this.currentRow[i] !== null) {
        this.currentRow[i] = null
        return true
      }
    }
    return false
  }

  /** Submit the current row. Returns true if a row was actually submitted. */
  submit() {
    if (this.status !== 'playing') return false
    if (this.currentRow.some((c) => c === null)) return false

    const guess = [...this.currentRow]
    const peg = computePegs(guess, this.secret)
    this.guesses.push(guess)
    this.pegs.push(peg)
    this.currentRow = new Array(this.codeLength).fill(null)

    if (peg.black === this.codeLength) {
      this.status = 'won'
      this.solvedAt = Date.now()
      this.score = this._computeScore()
      // Persist high score — only on win, only if it beats the previous best.
      const isRecord = saveHighScore(this.score)
      this.isRecord = isRecord
    } else if (this.guesses.length >= this.maxAttempts) {
      this.status = 'lost'
      this.score = 0
      this.isRecord = false
    }
    return true
  }

  _computeScore() {
    const remaining = this.maxAttempts - this.guesses.length
    let s = (remaining + 1) * POINTS_PER_REMAINING_ATTEMPT
    const elapsedSec = Math.max(0, (this.solvedAt - this.startedAt) / 1000)
    if (elapsedSec < 30) s += TIME_BONUS_UNDER_30S
    else if (elapsedSec < 60) s += TIME_BONUS_UNDER_60S
    return s
  }

  /** Reset for a new round with a new secret. */
  newGame() {
    this._newRound()
  }

  /** Read-only snapshot for the renderer. */
  getBoardState() {
    return {
      palette: this.palette,
      codeLength: this.codeLength,
      maxAttempts: this.maxAttempts,
      guesses: this.guesses.map((g) => [...g]),
      pegs: this.pegs.map((p) => ({ ...p })),
      currentRow: [...this.currentRow],
      status: this.status,
      score: this.score,
      isRecord: Boolean(this.isRecord),
      best: loadHighScore(),
      attemptsUsed: this.guesses.length,
      attemptsLeft: this.maxAttempts - this.guesses.length,
      secret: this.status === 'lost' || this.status === 'won' ? [...this.secret] : null
    }
  }
}

export const _test = { computePegs, sanitizePalette, defaultPalette, DEFAULT_CODE_LENGTH }
