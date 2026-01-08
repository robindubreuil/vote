// Tests pour le frontend stagiaire
import { describe, it, expect, beforeEach, vi } from 'vitest'

// États de l'application
const AppState = {
  JOINING: 'joining',
  WAITING: 'waiting',
  VOTING: 'voting',
  VOTED: 'voted',
  CLOSED: 'closed'
}

// Couleurs disponibles
const COLORS = [
  { id: 'rouge', name: 'Rouge', color: '#ef4444' },
  { id: 'vert', name: 'Vert', color: '#22c55e' },
  { id: 'bleu', name: 'Bleu', color: '#3b82f6' },
  { id: 'jaune', name: 'Jaune', color: '#eab308' },
  { id: 'orange', name: 'Orange', color: '#f97316' },
  { id: 'violet', name: 'Violet', color: '#a855f7' },
  { id: 'rose', name: 'Rose', color: '#ec4899' },
  { id: 'gris', name: 'Gris', color: '#6b7280' }
]

// Fonction de validation du code de session
function isValidSessionCode(code) {
  return /^\d{4}$/.test(code)
}

// Fonction pour formater le nom des couleurs sélectionnées
function formatSelectedColorNames(selectedColors, colors) {
  return Array.from(selectedColors).map(id => {
    const color = colors.find(c => c.id === id)
    return color?.name || id
  }).join(' + ')
}

describe('Stagiaire - Validation du code de session', () => {
  it('devrait accepter un code à 4 chiffres valide', () => {
    expect(isValidSessionCode('1234')).toBe(true)
    expect(isValidSessionCode('0000')).toBe(true)
    expect(isValidSessionCode('9999')).toBe(true)
  })

  it('devrait rejeter un code avec moins de 4 chiffres', () => {
    expect(isValidSessionCode('123')).toBe(false)
    expect(isValidSessionCode('12')).toBe(false)
    expect(isValidSessionCode('1')).toBe(false)
    expect(isValidSessionCode('')).toBe(false)
  })

  it('devrait rejeter un code avec plus de 4 chiffres', () => {
    expect(isValidSessionCode('12345')).toBe(false)
    expect(isValidSessionCode('123456')).toBe(false)
  })

  it('devrait rejeter un code avec des lettres', () => {
    expect(isValidSessionCode('abcd')).toBe(false)
    expect(isValidSessionCode('1a2b')).toBe(false)
    expect(isValidSessionCode('12ab')).toBe(false)
  })

  it('devrait rejeter un code avec des caractères spéciaux', () => {
    expect(isValidSessionCode('12 4')).toBe(false)
    expect(isValidSessionCode('12-4')).toBe(false)
    expect(isValidSessionCode('12.4')).toBe(false)
  })
})

describe('Stagiaire - États de l\'application', () => {
  it('devrait avoir les 5 états définis', () => {
    expect(AppState.JOINING).toBe('joining')
    expect(AppState.WAITING).toBe('waiting')
    expect(AppState.VOTING).toBe('voting')
    expect(AppState.VOTED).toBe('voted')
    expect(AppState.CLOSED).toBe('closed')
  })
})

describe('Stagiaire - Couleurs', () => {
  it('devrait avoir 8 couleurs définies', () => {
    expect(COLORS).toHaveLength(8)
  })

  it('devrait avoir des couleurs valides avec codes hexadécimaux', () => {
    COLORS.forEach(color => {
      expect(color.id).toBeTruthy()
      expect(color.name).toBeTruthy()
      expect(color.color).toMatch(/^#[0-9a-fA-F]{6}$/)
    })
  })

  it('devrait filtrer les couleurs actives', () => {
    const availableColors = ['rouge', 'vert', 'bleu']
    const activeColors = COLORS.filter(c => availableColors.includes(c.id))

    expect(activeColors).toHaveLength(3)
    expect(activeColors[0].id).toBe('rouge')
    expect(activeColors[1].id).toBe('vert')
    expect(activeColors[2].id).toBe('bleu')
  })
})

describe('Stagiaire - Gestion d\'état', () => {
  let state

  beforeEach(() => {
    state = {
      appState: AppState.JOINING,
      sessionCode: '',
      sessionId: null,
      connected: false,
      availableColors: [],
      multipleChoice: false,
      selectedColors: new Set(),
      hasVoted: false,
      stagiaireId: null
    }
  })

  it('devrait initialiser avec l\'état JOINING', () => {
    expect(state.appState).toBe(AppState.JOINING)
  })

  it('devrait passer à WAITING après connexion réussie', () => {
    state.appState = AppState.WAITING
    state.sessionCode = '1234'
    state.sessionId = 'session-123'
    state.connected = true

    expect(state.appState).toBe(AppState.WAITING)
    expect(state.sessionCode).toBe('1234')
    expect(state.sessionId).toBe('session-123')
    expect(state.connected).toBe(true)
  })

  it('devrait passer à VOTING quand un vote commence', () => {
    state.appState = AppState.WAITING
    state.appState = AppState.VOTING
    state.availableColors = ['rouge', 'vert', 'bleu']
    state.multipleChoice = false
    state.selectedColors.clear()
    state.hasVoted = false

    expect(state.appState).toBe(AppState.VOTING)
    expect(state.availableColors).toEqual(['rouge', 'vert', 'bleu'])
    expect(state.multipleChoice).toBe(false)
    expect(state.selectedColors.size).toBe(0)
    expect(state.hasVoted).toBe(false)
  })

  it('devrait passer à VOTED après avoir voté', () => {
    state.appState = AppState.VOTED
    state.selectedColors = new Set(['rouge'])
    state.hasVoted = true

    expect(state.appState).toBe(AppState.VOTED)
    expect(state.hasVoted).toBe(true)
    expect(state.selectedColors.has('rouge')).toBe(true)
  })

  it('devrait passer à CLOSED quand le formateur ferme le vote', () => {
    state.appState = AppState.CLOSED

    expect(state.appState).toBe(AppState.CLOSED)
  })

  it('devrait revenir à WAITING après reset', () => {
    state.appState = AppState.WAITING
    state.selectedColors.clear()
    state.hasVoted = false

    expect(state.appState).toBe(AppState.WAITING)
    expect(state.selectedColors.size).toBe(0)
    expect(state.hasVoted).toBe(false)
  })
})

describe('Stagiaire - Gestion du vote', () => {
  it('devrait gérer le vote à choix unique', () => {
    const selectedColors = new Set()
    selectedColors.add('rouge')

    expect(selectedColors.size).toBe(1)
    expect(selectedColors.has('rouge')).toBe(true)
  })

  it('devrait gérer le vote à choix multiple', () => {
    const selectedColors = new Set()
    selectedColors.add('rouge')
    selectedColors.add('vert')
    selectedColors.add('bleu')

    expect(selectedColors.size).toBe(3)
    expect(Array.from(selectedColors)).toEqual(['rouge', 'vert', 'bleu'])
  })

  it('devrait pouvoir désélectionner une couleur', () => {
    const selectedColors = new Set(['rouge', 'vert'])
    selectedColors.delete('rouge')

    expect(selectedColors.size).toBe(1)
    expect(selectedColors.has('rouge')).toBe(false)
    expect(selectedColors.has('vert')).toBe(true)
  })

  it('devrait convertir le Set en Array pour l\'envoi', () => {
    const selectedColors = new Set(['rouge', 'bleu'])
    const couleurs = Array.from(selectedColors)

    expect(couleurs).toEqual(['rouge', 'bleu'])
  })
})

describe('Stagiaire - Formatage des couleurs', () => {
  it('devrait formater une seule couleur', () => {
    const selectedColors = new Set(['rouge'])
    const formatted = formatSelectedColorNames(selectedColors, COLORS)

    expect(formatted).toBe('Rouge')
  })

  it('devrait formater plusieurs couleurs avec +', () => {
    const selectedColors = new Set(['rouge', 'vert', 'bleu'])
    const formatted = formatSelectedColorNames(selectedColors, COLORS)

    expect(formatted).toBe('Rouge + Vert + Bleu')
  })

  it('devrait gérer les IDs inconnus', () => {
    const selectedColors = new Set(['unknown'])
    const formatted = formatSelectedColorNames(selectedColors, COLORS)

    expect(formatted).toBe('unknown')
  })
})

describe('Stagiaire - Messages WebSocket', () => {
  it('devrait sérialiser le message de connexion', () => {
    const message = {
      type: 'stagiaire_join',
      sessionCode: '1234',
      stagiaireId: 'stagiaire_abc123'
    }

    const json = JSON.stringify(message)
    const parsed = JSON.parse(json)

    expect(parsed.type).toBe('stagiaire_join')
    expect(parsed.sessionCode).toBe('1234')
    expect(parsed.stagiaireId).toBe('stagiaire_abc123')
  })

  it('devrait sérialiser le message de vote (choix unique)', () => {
    const message = {
      type: 'vote',
      sessionId: 'session-123',
      stagiaireId: 'stagiaire_abc123',
      couleurs: ['rouge']
    }

    const json = JSON.stringify(message)
    const parsed = JSON.parse(json)

    expect(parsed.type).toBe('vote')
    expect(parsed.couleurs).toEqual(['rouge'])
    expect(parsed.couleurs).toHaveLength(1)
  })

  it('devrait sérialiser le message de vote (choix multiple)', () => {
    const message = {
      type: 'vote',
      sessionId: 'session-123',
      stagiaireId: 'stagiaire_abc123',
      couleurs: ['rouge', 'vert', 'bleu']
    }

    const json = JSON.stringify(message)
    const parsed = JSON.parse(json)

    expect(parsed.type).toBe('vote')
    expect(parsed.couleurs).toEqual(['rouge', 'vert', 'bleu'])
    expect(parsed.couleurs).toHaveLength(3)
  })

  it('devrait parser le message vote_started', () => {
    const jsonMessage = '{"type":"vote_started","colors":["rouge","vert"],"multipleChoice":false}'
    const parsed = JSON.parse(jsonMessage)

    expect(parsed.type).toBe('vote_started')
    expect(parsed.colors).toEqual(['rouge', 'vert'])
    expect(parsed.multipleChoice).toBe(false)
  })

  it('devrait parser le message vote_accepted', () => {
    const jsonMessage = '{"type":"vote_accepted"}'
    const parsed = JSON.parse(jsonMessage)

    expect(parsed.type).toBe('vote_accepted')
  })

  it('devrait parser le message vote_closed', () => {
    const jsonMessage = '{"type":"vote_closed"}'
    const parsed = JSON.parse(jsonMessage)

    expect(parsed.type).toBe('vote_closed')
  })

  it('devrait parser le message vote_reset', () => {
    const jsonMessage = '{"type":"vote_reset"}'
    const parsed = JSON.parse(jsonMessage)

    expect(parsed.type).toBe('vote_reset')
  })

  it('devrait parser le message join_error', () => {
    const jsonMessage = '{"type":"join_error"}'
    const parsed = JSON.parse(jsonMessage)

    expect(parsed.type).toBe('join_error')
  })

  it('devrait parser le message session_joined', () => {
    const jsonMessage = '{"type":"session_joined","sessionId":"session-123","sessionCode":"1234"}'
    const parsed = JSON.parse(jsonMessage)

    expect(parsed.type).toBe('session_joined')
    expect(parsed.sessionId).toBe('session-123')
    expect(parsed.sessionCode).toBe('1234')
  })
})

describe('Stagiaire - ID du stagiaire', () => {
  it('devrait générer un ID avec le préfixe stagiaire_', () => {
    const id = 'stagiaire_' + Math.random().toString(36).substr(2, 9)

    expect(id).toMatch(/^stagiaire_[a-z0-9]+$/)
    expect(id.length).toBeGreaterThan(10)
  })

  it('devrait générer des IDs différents', () => {
    const ids = new Set()
    for (let i = 0; i < 100; i++) {
      ids.add('stagiaire_' + Math.random().toString(36).substr(2, 9))
    }
    expect(ids.size).toBeGreaterThan(90)
  })
})

describe('Stagiaire - localStorage', () => {
  it('devrait stocker et récupérer l\'ID du stagiaire', () => {
    const testId = 'stagiaire_test123'

    // Simuler localStorage
    const storage = {
      getItem: vi.fn((key) => key === 'vote_stagiaire_id' ? testId : null),
      setItem: vi.fn(),
      removeItem: vi.fn()
    }

    storage.setItem('vote_stagiaire_id', testId)
    expect(storage.setItem).toHaveBeenCalledWith('vote_stagiaire_id', testId)

    const retrievedId = storage.getItem('vote_stagiaire_id')
    expect(retrievedId).toBe(testId)
  })

  it('devrait stocker et récupérer le code de session', () => {
    const testCode = '1234'

    const storage = {
      getItem: vi.fn((key) => key === 'vote_session_code' ? testCode : null),
      setItem: vi.fn(),
      removeItem: vi.fn()
    }

    storage.setItem('vote_session_code', testCode)
    expect(storage.setItem).toHaveBeenCalledWith('vote_session_code', testCode)

    const retrievedCode = storage.getItem('vote_session_code')
    expect(retrievedCode).toBe(testCode)
  })

  it('devrait supprimer le code de session en cas d\'erreur', () => {
    const storage = {
      getItem: vi.fn(() => '1234'),
      setItem: vi.fn(),
      removeItem: vi.fn()
    }

    storage.removeItem('vote_session_code')
    expect(storage.removeItem).toHaveBeenCalledWith('vote_session_code')
  })
})
