import { describe, it, expect, beforeEach } from 'vitest'
import { COLORS } from '../../../shared/colors.js'
import { validateSessionCode, validateName } from '../../../shared/validation.js'
import { AppState } from './state.js'
import { state } from './state.js'

function formatSelectedColorNames(selectedColors, colors) {
  return Array.from(selectedColors).map(id => {
    const color = colors.find(c => c.id === id)
    return color?.name || id
  }).join(' + ')
}

describe('Stagiaire - Validation du code de session', () => {
  it('should accept valid 4-digit codes', () => {
    expect(validateSessionCode('1234')).toBeNull()
    expect(validateSessionCode('0000')).toBeNull()
    expect(validateSessionCode('9999')).toBeNull()
  })

  it('should reject codes with fewer than 4 digits', () => {
    expect(validateSessionCode('123')).toBeTruthy()
    expect(validateSessionCode('12')).toBeTruthy()
    expect(validateSessionCode('1')).toBeTruthy()
    expect(validateSessionCode('')).toBeTruthy()
  })

  it('should reject codes with more than 4 digits', () => {
    expect(validateSessionCode('12345')).toBeTruthy()
    expect(validateSessionCode('123456')).toBeTruthy()
  })

  it('should reject codes with letters', () => {
    expect(validateSessionCode('abcd')).toBeTruthy()
    expect(validateSessionCode('1a2b')).toBeTruthy()
  })

  it('should reject codes with special characters', () => {
    expect(validateSessionCode('12 4')).toBeTruthy()
    expect(validateSessionCode('12-4')).toBeTruthy()
  })
})

describe('Stagiaire - Name validation', () => {
  it('should accept valid names', () => {
    expect(validateName('Marie')).toBeNull()
    expect(validateName('Jean-Pierre')).toBeNull()
    expect(validateName("O'Brien")).toBeNull()
  })

  it('should reject empty names', () => {
    expect(validateName('')).toBeTruthy()
    expect(validateName(null)).toBeTruthy()
  })

  it('should reject names that are too long', () => {
    expect(validateName('a'.repeat(17))).toBeTruthy()
  })

  it('should accept names at max length', () => {
    expect(validateName('a'.repeat(16))).toBeNull()
  })

  it('should reject names with invalid characters', () => {
    expect(validateName('Marie!')).toBeTruthy()
    expect(validateName('Test@')).toBeTruthy()
  })
})

describe('Stagiaire - Application states', () => {
  it('should have all 5 states defined', () => {
    expect(AppState.JOINING).toBe('joining')
    expect(AppState.WAITING).toBe('waiting')
    expect(AppState.VOTING).toBe('voting')
    expect(AppState.VOTED).toBe('voted')
    expect(AppState.CLOSED).toBe('closed')
  })
})

describe('Stagiaire - Colors', () => {
  it('should have 8 colors defined', () => {
    expect(COLORS).toHaveLength(8)
  })

  it('should have valid hex color codes', () => {
    COLORS.forEach(color => {
      expect(color.id).toBeTruthy()
      expect(color.name).toBeTruthy()
      expect(color.color).toMatch(/^#[0-9a-fA-F]{6}$/)
    })
  })

  it('should filter active colors', () => {
    const availableColors = ['rouge', 'vert', 'bleu']
    const activeColors = COLORS.filter(c => availableColors.includes(c.id))

    expect(activeColors).toHaveLength(3)
    expect(activeColors[0].id).toBe('rouge')
    expect(activeColors[1].id).toBe('vert')
    expect(activeColors[2].id).toBe('bleu')
  })
})

describe('Stagiaire - State transitions', () => {
  beforeEach(() => {
    state.appState = AppState.JOINING
    state.sessionCode = ''
    state.connected = false
    state.availableColors = []
    state.multipleChoice = false
    state.selectedColors = new Set()
    state.hasVoted = false
    state.stagiaireId = null
  })

  it('should initialize in JOINING state', () => {
    expect(state.appState).toBe(AppState.JOINING)
  })

  it('should transition to WAITING after successful join', () => {
    state.appState = AppState.WAITING
    state.sessionCode = '1234'
    state.connected = true

    expect(state.appState).toBe(AppState.WAITING)
    expect(state.sessionCode).toBe('1234')
    expect(state.connected).toBe(true)
  })

  it('should transition to VOTING when vote starts', () => {
    state.appState = AppState.VOTING
    state.availableColors = ['rouge', 'vert', 'bleu']
    state.selectedColors.clear()

    expect(state.appState).toBe(AppState.VOTING)
    expect(state.selectedColors.size).toBe(0)
  })

  it('should transition to VOTED after voting', () => {
    state.appState = AppState.VOTED
    state.selectedColors = new Set(['rouge'])
    state.hasVoted = true

    expect(state.appState).toBe(AppState.VOTED)
    expect(state.hasVoted).toBe(true)
  })

  it('should transition to CLOSED when trainer closes', () => {
    state.appState = AppState.CLOSED
    expect(state.appState).toBe(AppState.CLOSED)
  })

  it('should reset to WAITING after vote reset', () => {
    state.appState = AppState.WAITING
    state.selectedColors.clear()
    state.hasVoted = false

    expect(state.appState).toBe(AppState.WAITING)
    expect(state.selectedColors.size).toBe(0)
    expect(state.hasVoted).toBe(false)
  })
})

describe('Stagiaire - Vote handling', () => {
  it('should handle single choice vote', () => {
    const selectedColors = new Set()
    selectedColors.add('rouge')

    expect(selectedColors.size).toBe(1)
    expect(selectedColors.has('rouge')).toBe(true)
  })

  it('should handle multiple choice vote', () => {
    const selectedColors = new Set()
    selectedColors.add('rouge')
    selectedColors.add('vert')
    selectedColors.add('bleu')

    expect(selectedColors.size).toBe(3)
  })

  it('should deselect a color', () => {
    const selectedColors = new Set(['rouge', 'vert'])
    selectedColors.delete('rouge')

    expect(selectedColors.size).toBe(1)
    expect(selectedColors.has('rouge')).toBe(false)
    expect(selectedColors.has('vert')).toBe(true)
  })

  it('should convert Set to Array for sending', () => {
    const selectedColors = new Set(['rouge', 'bleu'])
    expect(Array.from(selectedColors)).toEqual(['rouge', 'bleu'])
  })
})

describe('Stagiaire - Color name formatting', () => {
  it('should format a single color', () => {
    expect(formatSelectedColorNames(new Set(['rouge']), COLORS)).toBe('Rouge')
  })

  it('should format multiple colors with +', () => {
    expect(formatSelectedColorNames(new Set(['rouge', 'vert', 'bleu']), COLORS)).toBe('Rouge + Vert + Bleu')
  })

  it('should handle unknown IDs', () => {
    expect(formatSelectedColorNames(new Set(['unknown']), COLORS)).toBe('unknown')
  })
})

describe('Stagiaire - WebSocket messages', () => {
  it('should serialize join message', () => {
    const message = {
      type: 'stagiaire_join',
      sessionCode: '1234',
      name: 'Marie',
      stagiaireId: 'abc123def456'
    }
    const parsed = JSON.parse(JSON.stringify(message))
    expect(parsed.type).toBe('stagiaire_join')
    expect(parsed.sessionCode).toBe('1234')
    expect(parsed.stagiaireId).toBe('abc123def456')
  })

  it('should serialize vote message with colors', () => {
    const message = {
      type: 'vote',
      colors: ['rouge'],
      stagiaireId: 'abc123def456'
    }
    const parsed = JSON.parse(JSON.stringify(message))
    expect(parsed.type).toBe('vote')
    expect(parsed.colors).toEqual(['rouge'])
    expect(parsed.stagiaireId).toBe('abc123def456')
  })

  it('should parse vote_started message', () => {
    const parsed = JSON.parse('{"type":"vote_started","colors":["rouge","vert"],"multipleChoice":false}')
    expect(parsed.type).toBe('vote_started')
    expect(parsed.colors).toEqual(['rouge', 'vert'])
    expect(parsed.multipleChoice).toBe(false)
  })

  it('should parse vote_accepted message', () => {
    expect(JSON.parse('{"type":"vote_accepted"}').type).toBe('vote_accepted')
  })

  it('should parse vote_closed message', () => {
    expect(JSON.parse('{"type":"vote_closed"}').type).toBe('vote_closed')
  })

  it('should parse session_joined with stagiaireId', () => {
    const parsed = JSON.parse('{"type":"session_joined","sessionCode":"1234","stagiaireId":"abc123def456"}')
    expect(parsed.type).toBe('session_joined')
    expect(parsed.sessionCode).toBe('1234')
    expect(parsed.stagiaireId).toBe('abc123def456')
  })
})
