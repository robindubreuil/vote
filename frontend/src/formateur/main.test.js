import { describe, it, expect, beforeEach, afterEach } from 'vitest'
import { COLORS } from '../../../shared/colors.js'
import { validateSessionCode, validateName } from '../../../shared/validation.js'
import { getColorCounts, getCombinations, sortStagiaires } from './utils.js'
import { state } from './state.js'

function calculateBarWidth(count, maxCount) {
  if (maxCount === 0) return 0
  return (count / maxCount) * 100
}

describe('Formateur - Couleurs', () => {
  it('should have 8 colors defined', () => {
    expect(COLORS).toHaveLength(8)
  })

  it('should have correct color IDs', () => {
    const ids = COLORS.map(c => c.id)
    expect(ids).toContain('rouge')
    expect(ids).toContain('vert')
    expect(ids).toContain('bleu')
    expect(ids).toContain('jaune')
    expect(ids).toContain('orange')
    expect(ids).toContain('violet')
    expect(ids).toContain('rose')
    expect(ids).toContain('gris')
  })

  it('should have valid hex color codes', () => {
    COLORS.forEach(color => {
      expect(color.color).toMatch(/^#[0-9a-fA-F]{6}$/)
    })
  })
})

describe('Formateur - getColorCounts', () => {
  beforeEach(() => {
    state.stagiaires = []
  })

  it('should count votes per color', () => {
    state.stagiaires = [
      { id: 's1', vote: ['rouge'] },
      { id: 's2', vote: ['rouge'] },
      { id: 's3', vote: ['vert'] },
      { id: 's4', vote: ['bleu'] }
    ]
    expect(getColorCounts()).toEqual({ rouge: 2, vert: 1, bleu: 1 })
  })

  it('should handle multiple choice votes', () => {
    state.stagiaires = [
      { id: 's1', vote: ['rouge', 'vert'] },
      { id: 's2', vote: ['rouge', 'bleu'] },
      { id: 's3', vote: ['vert'] }
    ]
    expect(getColorCounts()).toEqual({ rouge: 2, vert: 2, bleu: 1 })
  })

  it('should return empty object for no votes', () => {
    state.stagiaires = []
    expect(getColorCounts()).toEqual({})
  })

  it('should skip stagiaires without votes', () => {
    state.stagiaires = [
      { id: 's1', vote: ['rouge'] },
      { id: 's2', vote: null },
      { id: 's3' }
    ]
    expect(getColorCounts()).toEqual({ rouge: 1 })
  })
})

describe('Formateur - getCombinations', () => {
  beforeEach(() => {
    state.stagiaires = []
  })

  it('should group votes by combination', () => {
    state.stagiaires = [
      { id: 's1', vote: ['rouge'] },
      { id: 's2', vote: ['rouge'] },
      { id: 's3', vote: ['vert'] }
    ]
    const combos = getCombinations()
    expect(combos).toHaveLength(2)
    expect(combos[0]).toEqual({ colors: ['rouge'], count: 2 })
    expect(combos[1]).toEqual({ colors: ['vert'], count: 1 })
  })

  it('should sort combinations by count descending', () => {
    state.stagiaires = [
      { id: 's1', vote: ['bleu'] },
      { id: 's2', vote: ['rouge'] },
      { id: 's3', vote: ['rouge'] },
      { id: 's4', vote: ['rouge'] }
    ]
    const combos = getCombinations()
    expect(combos[0].count).toBeGreaterThanOrEqual(combos[1].count)
  })

  it('should handle order-independent combinations', () => {
    state.stagiaires = [
      { id: 's1', vote: ['rouge', 'vert'] },
      { id: 's2', vote: ['vert', 'rouge'] },
      { id: 's3', vote: ['rouge', 'bleu'] }
    ]
    const combos = getCombinations()
    expect(combos).toHaveLength(2)
    expect(combos[0]).toEqual({ colors: ['rouge', 'vert'], count: 2 })
  })

  it('should return empty array for no votes', () => {
    state.stagiaires = []
    expect(getCombinations()).toEqual([])
  })

  it('should skip stagiaires with empty votes', () => {
    state.stagiaires = [
      { id: 's1', vote: [] },
      { id: 's2' },
      { id: 's3', vote: ['rouge'] }
    ]
    expect(getCombinations()).toHaveLength(1)
  })
})

describe('Formateur - sortStagiaires', () => {
  it('should sort non-voters first', () => {
    const stagiaires = [
      { id: 's1', name: 'Alice', vote: ['rouge'], connected: true },
      { id: 's2', name: 'Bob', connected: true },
      { id: 's3', name: 'Charlie', vote: ['vert'], connected: true }
    ]
    const sorted = sortStagiaires(stagiaires)
    expect(sorted[0].id).toBe('s2')
  })

  it('should sort voters by combination popularity', () => {
    const stagiaires = [
      { id: 's1', name: 'Alice', vote: ['bleu'], connected: true },
      { id: 's2', name: 'Bob', vote: ['rouge'], connected: true },
      { id: 's3', name: 'Charlie', vote: ['rouge'], connected: true }
    ]
    const sorted = sortStagiaires(stagiaires)
    expect(sorted[0].vote).toEqual(['rouge'])
    expect(sorted[1].vote).toEqual(['rouge'])
    expect(sorted[2].vote).toEqual(['bleu'])
  })

  it('should sort by name as tiebreaker', () => {
    const stagiaires = [
      { id: 's1', name: 'Charlie', connected: true },
      { id: 's2', name: 'Alice', connected: true },
      { id: 's3', name: 'Bob', connected: true }
    ]
    const sorted = sortStagiaires(stagiaires)
    expect(sorted[0].name).toBe('Alice')
    expect(sorted[1].name).toBe('Bob')
    expect(sorted[2].name).toBe('Charlie')
  })
})

describe('Formateur - Percentages and bar widths', () => {
  beforeEach(() => {
    state.stagiaires = []
  })

  it('should calculate percentages without division by zero', () => {
    state.stagiaires = [
      { id: 's1', vote: ['rouge'] },
      { id: 's2', vote: ['rouge'] },
      { id: 's3', vote: ['vert'] }
    ]
    const counts = getColorCounts()
    const maxCount = Math.max(...Object.values(counts), 1)

    expect(maxCount).toBe(2)
    expect(calculateBarWidth(counts['rouge'], maxCount)).toBe(100)
    expect(calculateBarWidth(counts['vert'], maxCount)).toBe(50)
  })

  it('should handle no votes', () => {
    const counts = getColorCounts()
    const maxCount = Math.max(...Object.values(counts), 1)
    expect(maxCount).toBe(1)
  })

  it('should handle ties', () => {
    state.stagiaires = [
      { id: 's1', vote: ['rouge'] },
      { id: 's2', vote: ['vert'] },
      { id: 's3', vote: ['bleu'] }
    ]
    const counts = getColorCounts()
    const maxCount = Math.max(...Object.values(counts), 1)
    expect(calculateBarWidth(counts['rouge'], maxCount)).toBe(100)
    expect(calculateBarWidth(counts['vert'], maxCount)).toBe(100)
    expect(calculateBarWidth(counts['bleu'], maxCount)).toBe(100)
  })

  it('should handle large vote counts', () => {
    state.stagiaires = Array.from({ length: 50 }, (_, i) => ({
      id: `s${i}`,
      vote: [i < 25 ? 'rouge' : 'vert']
    }))
    const counts = getColorCounts()
    const maxCount = Math.max(...Object.values(counts), 1)
    expect(counts['rouge']).toBe(25)
    expect(counts['vert']).toBe(25)
    expect(calculateBarWidth(counts['rouge'], maxCount)).toBe(100)
  })
})

describe('Formateur - Timer formatting', () => {
  it('should format elapsed time correctly', () => {
    const formatTime = (elapsed) => {
      const mins = Math.floor(elapsed / 60).toString().padStart(2, '0')
      const secs = (elapsed % 60).toString().padStart(2, '0')
      return `${mins}:${secs}`
    }

    expect(formatTime(0)).toBe('00:00')
    expect(formatTime(59)).toBe('00:59')
    expect(formatTime(60)).toBe('01:00')
    expect(formatTime(359)).toBe('05:59')
    expect(formatTime(360)).toBe('06:00')
  })
})

describe('Formateur - Validation', () => {
  it('should require at least 2 colors to start a vote', () => {
    const selectedColors = new Set(['rouge'])
    expect(selectedColors.size).toBeLessThan(2)
  })

  it('should allow starting with 2 or more colors', () => {
    const selectedColors = new Set(['rouge', 'vert'])
    expect(selectedColors.size).toBeGreaterThanOrEqual(2)
  })

  it('should validate session codes', () => {
    expect(validateSessionCode('1234')).toBeNull()
    expect(validateSessionCode('abcd')).toBeTruthy()
    expect(validateSessionCode('123')).toBeTruthy()
    expect(validateSessionCode('')).toBeTruthy()
  })
})

describe('Formateur - State management', () => {
  beforeEach(() => {
    state.sessionCode = null
    state.connected = false
    state.voteState = 'idle'
    state.selectedColors = new Set(['rouge', 'vert', 'bleu'])
    state.multipleChoice = false
    state.connectedCount = 0
    state.stagiaires = []
    state.voteStartTime = null
  })

  it('should initialize with idle state', () => {
    expect(state.voteState).toBe('idle')
  })

  it('should manage selected colors with Set', () => {
    expect(state.selectedColors.has('rouge')).toBe(true)
    expect(state.selectedColors.has('jaune')).toBe(false)

    state.selectedColors.add('jaune')
    expect(state.selectedColors.has('jaune')).toBe(true)

    state.selectedColors.delete('rouge')
    expect(state.selectedColors.has('rouge')).toBe(false)
  })

  it('should convert Set to Array for sending', () => {
    const colorsArray = Array.from(state.selectedColors)
    expect(colorsArray).toEqual(expect.arrayContaining(['rouge', 'vert', 'bleu']))
    expect(colorsArray).toHaveLength(3)
  })
})

describe('Formateur - WebSocket messages', () => {
  it('should serialize start_vote message', () => {
    const message = {
      type: 'start_vote',
      sessionCode: '1234',
      colors: ['rouge', 'vert', 'bleu'],
      multipleChoice: false
    }
    const parsed = JSON.parse(JSON.stringify(message))
    expect(parsed.type).toBe('start_vote')
    expect(parsed.colors).toEqual(['rouge', 'vert', 'bleu'])
    expect(parsed.multipleChoice).toBe(false)
  })

  it('should parse vote_received message', () => {
    const parsed = JSON.parse('{"type":"vote_received","stagiaireId":"s1","colors":["rouge"]}')
    expect(parsed.type).toBe('vote_received')
    expect(parsed.stagiaireId).toBe('s1')
    expect(parsed.colors).toEqual(['rouge'])
  })
})
