import { describe, it, expect, beforeEach, vi } from 'vitest'

const store = new Map()

vi.stubGlobal('localStorage', {
  getItem: (k) => (store.has(k) ? store.get(k) : null),
  setItem: (k, v) => void store.set(k, String(v)),
  removeItem: (k) => void store.delete(k),
  clear: () => store.clear()
})

import { loadHighScore, saveHighScore, resetHighScore, loadStreak, hasSeenRules, _test } from './game-storage.js'

const { resetForTests } = _test

describe('game-storage', () => {
  beforeEach(() => {
    resetForTests()
  })

  describe('loadHighScore', () => {
    it('returns 0 when nothing is stored', () => {
      expect(loadHighScore()).toBe(0)
    })

    it('returns the stored integer', () => {
      localStorage.setItem('vote:game:highscore', '420')
      expect(loadHighScore()).toBe(420)
    })

    it('returns 0 for non-numeric junk', () => {
      localStorage.setItem('vote:game:highscore', 'oops')
      expect(loadHighScore()).toBe(0)
    })

    it('returns 0 for negative numbers', () => {
      localStorage.setItem('vote:game:highscore', '-10')
      expect(loadHighScore()).toBe(0)
    })

    it('parses leading digits and ignores trailing garbage', () => {
      localStorage.setItem('vote:game:highscore', '123abc')
      expect(loadHighScore()).toBe(123)
    })
  })

  describe('saveHighScore', () => {
    it('saves and reports a new record on empty store', () => {
      expect(saveHighScore(100)).toBe(true)
      expect(loadHighScore()).toBe(100)
    })

    it('does not save a score that does not beat the best', () => {
      saveHighScore(500)
      expect(saveHighScore(499)).toBe(false)
      expect(loadHighScore()).toBe(500)
    })

    it('saves a new record that beats the previous', () => {
      saveHighScore(100)
      expect(saveHighScore(150)).toBe(true)
      expect(loadHighScore()).toBe(150)
    })

    it('rejects zero and negative scores', () => {
      expect(saveHighScore(0)).toBe(false)
      expect(saveHighScore(-50)).toBe(false)
      expect(loadHighScore()).toBe(0)
    })

    it('rejects NaN and non-numeric input', () => {
      expect(saveHighScore(NaN)).toBe(false)
      expect(saveHighScore('abc')).toBe(false)
      expect(saveHighScore(null)).toBe(false)
      expect(loadHighScore()).toBe(0)
    })

    it('floors fractional scores', () => {
      saveHighScore(99.9)
      expect(loadHighScore()).toBe(99)
    })
  })

  describe('resetHighScore', () => {
    it('clears the stored score', () => {
      saveHighScore(750)
      resetHighScore()
      expect(loadHighScore()).toBe(0)
    })

    it('is a no-op when nothing is stored', () => {
      expect(() => resetHighScore()).not.toThrow()
    })
  })

  // F1 regression: reads must not crash when localStorage.getItem throws.
  // Firefox "never remember history" mode and some embedded WebViews throw
  // SecurityError on getItem (not just setItem). The module documents that
  // it "fails silently"; before the fix, only writes were wrapped.
  describe('read-throws resilience (F1)', () => {
    function withThrowingGetItem(fn) {
      const original = localStorage.getItem
      localStorage.getItem = () => {
        throw new DOMException('The operation is insecure.', 'SecurityError')
      }
      try {
        fn()
      } finally {
        localStorage.getItem = original
      }
    }

    it('loadHighScore returns 0 when getItem throws', () => {
      saveHighScore(500) // sanity: write path works
      withThrowingGetItem(() => {
        expect(loadHighScore()).toBe(0)
      })
    })

    it('hasSeenRules returns false when getItem throws', () => {
      localStorage.setItem('vote:game:seenRules', '1')
      withThrowingGetItem(() => {
        expect(hasSeenRules()).toBe(false)
      })
    })

    it('loadStreak returns 0 when getItem throws', () => {
      localStorage.setItem('vote:game:streak', '3')
      withThrowingGetItem(() => {
        expect(loadStreak()).toBe(0)
      })
    })

    it('resetForTests does not throw when removeItem throws', () => {
      const original = localStorage.removeItem
      localStorage.removeItem = () => {
        throw new DOMException('The operation is insecure.', 'SecurityError')
      }
      try {
        expect(() => resetForTests()).not.toThrow()
      } finally {
        localStorage.removeItem = original
      }
    })
  })
})
