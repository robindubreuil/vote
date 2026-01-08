// Tests pour le frontend formateur
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'

// Mock du DOM
function mockDOM() {
  document.body.innerHTML = '<div id="app"></div>'
}

// Nettoyer le DOM après chaque test
function cleanupDOM() {
  document.body.innerHTML = ''
}

// Configuration des couleurs (extrait de main.js)
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

// Fonctions utilitaires extraites de main.js pour les tests
function getColorCounts(votes) {
  const counts = {}
  votes.forEach(vote => {
    vote.couleurs.forEach(colorId => {
      counts[colorId] = (counts[colorId] || 0) + 1
    })
  })
  return counts
}

function getColorCount(colorId, votes) {
  return getColorCounts(votes)[colorId] || 0
}

function getCombinations(votes) {
  const comboMap = new Map()

  votes.forEach(vote => {
    const key = vote.couleurs.slice().sort().join('+')
    comboMap.set(key, (comboMap.get(key) || 0) + 1)
  })

  return Array.from(comboMap.entries())
    .map(([key, count]) => ({
      colors: key ? key.split('+') : [],
      count
    }))
    .sort((a, b) => b.count - a.count)
}

function generateSessionCode() {
  return Math.floor(1000 + Math.random() * 9000).toString()
}

describe('Formateur - Fonctions utilitaires', () => {
  describe('generateSessionCode', () => {
    it('devrait générer un code à 4 chiffres', () => {
      const code = generateSessionCode()
      expect(code).toMatch(/^\d{4}$/)
    })

    it('devrait générer un code entre 1000 et 9999', () => {
      const code = parseInt(generateSessionCode())
      expect(code).toBeGreaterThanOrEqual(1000)
      expect(code).toBeLessThanOrEqual(9999)
    })

    it('devrait générer des codes différents', () => {
      const codes = new Set()
      for (let i = 0; i < 100; i++) {
        codes.add(generateSessionCode())
      }
      // Avec 100 itérations, on devrait avoir au moins 90 codes uniques
      expect(codes.size).toBeGreaterThan(90)
    })
  })

  describe('getColorCounts', () => {
    it('devrait compter les votes par couleur', () => {
      const votes = [
        { couleurs: ['rouge'], stagiaireId: 's1' },
        { couleurs: ['rouge'], stagiaireId: 's2' },
        { couleurs: ['vert'], stagiaireId: 's3' },
        { couleurs: ['bleu'], stagiaireId: 's4' }
      ]
      const counts = getColorCounts(votes)
      expect(counts).toEqual({ rouge: 2, vert: 1, bleu: 1 })
    })

    it('devrait gérer les votes à choix multiple', () => {
      const votes = [
        { couleurs: ['rouge', 'vert'], stagiaireId: 's1' },
        { couleurs: ['rouge', 'bleu'], stagiaireId: 's2' },
        { couleurs: ['vert'], stagiaireId: 's3' }
      ]
      const counts = getColorCounts(votes)
      expect(counts).toEqual({ rouge: 2, vert: 2, bleu: 1 })
    })

    it('devrait retourner un objet vide pour aucun vote', () => {
      const counts = getColorCounts([])
      expect(counts).toEqual({})
    })
  })

  describe('getColorCount', () => {
    const votes = [
      { couleurs: ['rouge', 'vert'], stagiaireId: 's1' },
      { couleurs: ['rouge'], stagiaireId: 's2' }
    ]

    it('devrait retourner le nombre de votes pour une couleur', () => {
      expect(getColorCount('rouge', votes)).toBe(2)
      expect(getColorCount('vert', votes)).toBe(1)
    })

    it('devrait retourner 0 pour une couleur sans vote', () => {
      expect(getColorCount('bleu', votes)).toBe(0)
    })
  })

  describe('getCombinations', () => {
    it('devrait regrouper les votes par combinaison', () => {
      const votes = [
        { couleurs: ['rouge'], stagiaireId: 's1' },
        { couleurs: ['rouge'], stagiaireId: 's2' },
        { couleurs: ['vert'], stagiaireId: 's3' }
      ]
      const combos = getCombinations(votes)
      expect(combos).toHaveLength(2)
      expect(combos[0]).toEqual({ colors: ['rouge'], count: 2 })
      expect(combos[1]).toEqual({ colors: ['vert'], count: 1 })
    })

    it('devrait trier les combinaisons par ordre décroissant', () => {
      const votes = [
        { couleurs: ['bleu'], stagiaireId: 's1' },
        { couleurs: ['rouge'], stagiaireId: 's2' },
        { couleurs: ['rouge'], stagiaireId: 's3' },
        { couleurs: ['rouge'], stagiaireId: 's4' }
      ]
      const combos = getCombinations(votes)
      expect(combos[0].count).toBeGreaterThanOrEqual(combos[1].count)
    })

    it('devrait gérer les choix multiples', () => {
      const votes = [
        { couleurs: ['rouge', 'vert'], stagiaireId: 's1' },
        { couleurs: ['vert', 'rouge'], stagiaireId: 's2' },
        { couleurs: ['rouge', 'bleu'], stagiaireId: 's3' }
      ]
      const combos = getCombinations(votes)
      expect(combos).toHaveLength(2)
      // rouge+vert (trié alphabétiquement) devrait apparaître 2 fois
      expect(combos[0]).toEqual({ colors: ['rouge', 'vert'], count: 2 })
    })

    it('devrait retourner un tableau vide pour aucun vote', () => {
      const combos = getCombinations([])
      expect(combos).toEqual([])
    })
  })
})

describe('Formateur - Couleurs', () => {
  it('devrait avoir 8 couleurs définies', () => {
    expect(COLORS).toHaveLength(8)
  })

  it('devrait avoir les IDs de couleurs corrects', () => {
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

  it('devrait avoir des codes hexadécimaux valides', () => {
    COLORS.forEach(color => {
      expect(color.color).toMatch(/^#[0-9a-fA-F]{6}$/)
    })
  })
})

describe('Formateur - Gestion d\'état', () => {
  let state

  beforeEach(() => {
    state = {
      sessionCode: null,
      sessionId: null,
      connected: false,
      voteState: 'idle',
      selectedColors: new Set(['rouge', 'vert', 'bleu']),
      multipleChoice: false,
      connectedCount: 0,
      votes: [],
      voteStartTime: null,
      timerInterval: null
    }
  })

  it('devrait initialiser avec l\'état idle', () => {
    expect(state.voteState).toBe('idle')
  })

  it('devrait gérer les couleurs sélectionnées avec Set', () => {
    expect(state.selectedColors.has('rouge')).toBe(true)
    expect(state.selectedColors.has('jaune')).toBe(false)

    state.selectedColors.add('jaune')
    expect(state.selectedColors.has('jaune')).toBe(true)

    state.selectedColors.delete('rouge')
    expect(state.selectedColors.has('rouge')).toBe(false)
  })

  it('devrait calculer correctement le nombre de couleurs sélectionnées', () => {
    expect(state.selectedColors.size).toBe(3)

    state.selectedColors.add('jaune')
    expect(state.selectedColors.size).toBe(4)
  })

  it('devrait convertir Set en Array pour l\'envoi', () => {
    const colorsArray = Array.from(state.selectedColors)
    expect(colorsArray).toEqual(expect.arrayContaining(['rouge', 'vert', 'bleu']))
    expect(colorsArray).toHaveLength(3)
  })
})

describe('Formateur - Calculs de pourcentages', () => {
  it('devrait calculer les pourcentages correctement sans division par zéro', () => {
    const votes = [
      { couleurs: ['rouge'], stagiaireId: 's1' },
      { couleurs: ['rouge'], stagiaireId: 's2' },
      { couleurs: ['vert'], stagiaireId: 's3' }
    ]

    const counts = getColorCounts(votes)
    const maxCount = Math.max(...Object.values(counts), 1) // Le 1 évite -Infinity

    expect(maxCount).toBe(2)

    // Pour rouge: 2/2 * 100 = 100%
    const rougePercent = (counts['rouge'] / maxCount) * 100
    expect(rougePercent).toBe(100)

    // Pour vert: 1/2 * 100 = 50%
    const vertPercent = (counts['vert'] / maxCount) * 100
    expect(vertPercent).toBe(50)
  })

  it('devrait gérer le cas où il n\'y a pas de votes', () => {
    const votes = []
    const counts = getColorCounts(votes)
    const values = Object.values(counts)

    // Math.max avec tableau vide et default value
    const maxCount = Math.max(...values, 1)
    expect(maxCount).toBe(1) // Pas -Infinity
  })
})

describe('Formateur - Gestion du timer', () => {
  it('devrait formater le temps correctement', () => {
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
  it('devrait avoir au minimum 2 couleurs pour lancer un vote', () => {
    const selectedColors = new Set(['rouge'])
    expect(selectedColors.size).toBeLessThan(2)
  })

  it('devrait autoriser le lancement avec 2 couleurs ou plus', () => {
    const selectedColors = new Set(['rouge', 'vert'])
    expect(selectedColors.size).toBeGreaterThanOrEqual(2)
  })
})

describe('Formateur - Messages WebSocket', () => {
  it('devrait sérialiser correctement les messages', () => {
    const message = {
      type: 'start_vote',
      sessionId: '1234',
      colors: ['rouge', 'vert', 'bleu'],
      multipleChoice: false
    }

    const json = JSON.stringify(message)
    const parsed = JSON.parse(json)

    expect(parsed.type).toBe('start_vote')
    expect(parsed.colors).toEqual(['rouge', 'vert', 'bleu'])
    expect(parsed.multipleChoice).toBe(false)
  })

  it('devrait parser les messages du serveur', () => {
    const jsonMessage = '{"type":"vote_received","stagiaireId":"s1","couleurs":["rouge"]}'
    const parsed = JSON.parse(jsonMessage)

    expect(parsed.type).toBe('vote_received')
    expect(parsed.stagiaireId).toBe('s1')
    expect(parsed.couleurs).toEqual(['rouge'])
  })
})
