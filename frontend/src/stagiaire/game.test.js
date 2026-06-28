import { describe, it, expect, beforeEach, vi } from 'vitest'
import { Mastermind, computePegs, _test } from './game.js'

// localStorage stub — Mastermind persists high scores on win.
const store = new Map()
vi.stubGlobal('localStorage', {
  getItem: (k) => (store.has(k) ? store.get(k) : null),
  setItem: (k, v) => void store.set(k, String(v)),
  removeItem: (k) => void store.delete(k),
  clear: () => store.clear()
})

describe('computePegs', () => {
  it('returns all black when guess matches secret exactly', () => {
    expect(computePegs(['a', 'b', 'c', 'd'], ['a', 'b', 'c', 'd'])).toEqual({ black: 4, white: 0 })
  })

  it('returns all white when guess has all right colors in wrong positions', () => {
    expect(computePegs(['a', 'b', 'c', 'd'], ['d', 'c', 'b', 'a'])).toEqual({ black: 0, white: 4 })
  })

  it('returns zero pegs when no shared colors', () => {
    expect(computePegs(['a', 'b'], ['c', 'd'])).toEqual({ black: 0, white: 0 })
  })

  it('handles duplicates correctly (black takes priority)', () => {
    // guess: a a b, secret: a b c
    // black: a==a (slot 0). white: guess's 2nd a finds nothing (secret's a is consumed),
    // guess's b finds secret's b (slot 1). → 1 black, 1 white
    expect(computePegs(['a', 'a', 'b'], ['a', 'b', 'c'])).toEqual({ black: 1, white: 1 })
  })

  it('does not double-count duplicate colors', () => {
    // guess: a a a, secret: a b c → only 1 black, 0 white (other a's have no match)
    expect(computePegs(['a', 'a', 'a'], ['a', 'b', 'c'])).toEqual({ black: 1, white: 0 })
  })

  it('handles duplicates where guess has more of a color than secret', () => {
    // guess: a a, secret: a b → 1 black, 0 white (only one a in secret)
    expect(computePegs(['a', 'a'], ['a', 'b'])).toEqual({ black: 1, white: 0 })
  })

  it('handles duplicates where secret has more of a color than guess', () => {
    // guess: a b, secret: a a → 1 black, 0 white
    expect(computePegs(['a', 'b'], ['a', 'a'])).toEqual({ black: 1, white: 0 })
  })

  it('mixed case: 2 black, 1 white', () => {
    // guess: a b c, secret: a c b → black: a. white: b↔b, c↔c
    expect(computePegs(['a', 'b', 'c'], ['a', 'c', 'b'])).toEqual({ black: 1, white: 2 })
  })
})

describe('Mastermind game flow', () => {
  let game

  beforeEach(() => {
    store.clear()
    // Use a fixed palette so secret-generation tests are deterministic-ish
    game = new Mastermind({
      colors: [
        { id: 'rouge', color: '#ef4444', name: 'Rouge' },
        { id: 'vert', color: '#22c55e', name: 'Vert' },
        { id: 'bleu', color: '#3b82f6', name: 'Bleu' },
        { id: 'jaune', color: '#eab308', name: 'Jaune' }
      ]
    })
  })

  it('starts with a 4-color secret and 8 attempts', () => {
    const s = game.getBoardState()
    expect(s.codeLength).toBe(4)
    expect(s.maxAttempts).toBe(8)
    expect(s.attemptsLeft).toBe(8)
    expect(s.status).toBe('playing')
    expect(s.secret).toBeNull()
    expect(game.secret).toHaveLength(4)
  })

  it('place() fills slots left-to-right', () => {
    expect(game.place('rouge')).toBe(true)
    expect(game.currentRow[0]).toBe('rouge')
    expect(game.place('vert')).toBe(true)
    expect(game.currentRow[1]).toBe('vert')
  })

  it('place() rejects when row is full', () => {
    game.place('rouge')
    game.place('vert')
    game.place('bleu')
    game.place('jaune')
    expect(game.place('rouge')).toBe(false)
  })

  it('place() rejects unknown colors', () => {
    expect(game.place('orange')).toBe(false)
  })

  it('clear(slotIndex) removes a specific slot', () => {
    game.place('rouge')
    game.place('vert')
    expect(game.clear(0)).toBe(true)
    expect(game.currentRow[0]).toBeNull()
    expect(game.currentRow[1]).toBe('vert')
  })

  it('clear() with no index removes the last filled slot', () => {
    game.place('rouge')
    game.place('vert')
    expect(game.clear()).toBe(true)
    expect(game.currentRow[1]).toBeNull()
    expect(game.currentRow[0]).toBe('rouge')
  })

  it('submit() rejects incomplete rows', () => {
    game.place('rouge')
    expect(game.submit()).toBe(false)
    expect(game.guesses).toHaveLength(0)
  })

  it('submit() accepts a complete row and records the guess', () => {
    game.place('rouge')
    game.place('vert')
    game.place('bleu')
    game.place('jaune')
    expect(game.submit()).toBe(true)
    expect(game.guesses).toHaveLength(1)
    expect(game.pegs).toHaveLength(1)
    expect(game.currentRow.every((c) => c === null)).toBe(true)
  })

  it('detects a win on exact match', () => {
    // Force the secret by direct assignment (test-only)
    game.secret = ['rouge', 'vert', 'bleu', 'jaune']
    game.place('rouge')
    game.place('vert')
    game.place('bleu')
    game.place('jaune')
    game.submit()
    expect(game.status).toBe('won')
    expect(game.score).toBeGreaterThan(0)
  })

  it('detects a loss after max attempts without solving', () => {
    game.secret = ['rouge', 'vert', 'bleu', 'jaune']
    // Submit wrong guess 8 times
    for (let i = 0; i < game.maxAttempts; i++) {
      game.place('rouge')
      game.place('rouge')
      game.place('rouge')
      game.place('rouge')
      game.submit()
    }
    expect(game.status).toBe('lost')
    expect(game.score).toBe(0)
  })

  it('reveals the secret in board state only after win or loss', () => {
    game.secret = ['rouge', 'vert', 'bleu', 'jaune']
    expect(game.getBoardState().secret).toBeNull()
    game.place('rouge')
    game.place('vert')
    game.place('bleu')
    game.place('jaune')
    game.submit()
    expect(game.getBoardState().secret).toEqual(['rouge', 'vert', 'bleu', 'jaune'])
  })

  it('newGame() resets state with a new secret', () => {
    game.secret = ['rouge', 'vert', 'bleu', 'jaune']
    game.place('rouge')
    game.place('vert')
    game.place('bleu')
    game.place('jaune')
    game.submit()
    expect(game.status).toBe('won')
    game.newGame()
    expect(game.status).toBe('playing')
    expect(game.guesses).toHaveLength(0)
    expect(game.currentRow.every((c) => c === null)).toBe(true)
  })

  it('scoring rewards fewer attempts', () => {
    // Solve in 1 attempt → highest score
    game.secret = ['rouge', 'vert', 'bleu', 'jaune']
    game.place('rouge')
    game.place('vert')
    game.place('bleu')
    game.place('jaune')
    game.submit()
    const fastScore = game.score

    game.newGame()
    game.secret = ['rouge', 'vert', 'bleu', 'jaune']
    // Solve in 3 attempts
    for (let i = 0; i < 2; i++) {
      game.place('rouge')
      game.place('rouge')
      game.place('rouge')
      game.place('rouge')
      game.submit()
    }
    game.place('rouge')
    game.place('vert')
    game.place('bleu')
    game.place('jaune')
    game.submit()
    const slowScore = game.score

    expect(fastScore).toBeGreaterThan(slowScore)
  })
})

describe('sanitizePalette', () => {
  it('returns default palette when input is empty', () => {
    const p = _test.sanitizePalette([])
    expect(p.length).toBeGreaterThanOrEqual(6)
  })

  it('returns default palette when input has fewer than 2 colors', () => {
    const p = _test.sanitizePalette([{ id: 'rouge', color: '#ef4444', name: 'Rouge' }])
    expect(p.length).toBeGreaterThanOrEqual(6)
  })

  it('filters unknown color ids', () => {
    const p = _test.sanitizePalette([
      { id: 'rouge', color: '#ef4444', name: 'Rouge' },
      { id: 'turquoise', color: '#fff', name: 'Nope' },
      { id: 'vert', color: '#22c55e', name: 'Vert' }
    ])
    expect(p.map((c) => c.id)).toEqual(['rouge', 'vert'])
  })
})
