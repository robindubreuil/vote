import { describe, it, expect, beforeEach } from 'vitest'
import { getColorCounts, getCombinations, sortStagiaires } from './vote-data.js'
import { state } from './state.js'

// F14: the pure data helpers were extracted from utils.js into
// vote-data.js to break the utils.js ↔ renderers.js import cycle.
// These tests pin the contract: helpers only read `state`, no DOM.

function resetState() {
  state.stagiaires = []
  state.selectedColors = new Set()
  state.colorLabels = {}
  state.connectedCount = 0
}

beforeEach(resetState)

describe('getColorCounts', () => {
  it('returns {} when no stagiaire has voted', () => {
    state.stagiaires = [
      { id: 'a', name: 'A', connected: true, vote: null },
      { id: 'b', name: 'B', connected: true, vote: [] }
    ]
    expect(getColorCounts()).toEqual({})
  })

  it('counts votes per color id', () => {
    state.stagiaires = [
      { id: 'a', name: 'A', connected: true, vote: ['rouge', 'vert'] },
      { id: 'b', name: 'B', connected: true, vote: ['rouge'] },
      { id: 'c', name: 'C', connected: true, vote: ['bleu'] }
    ]
    expect(getColorCounts()).toEqual({ rouge: 2, vert: 1, bleu: 1 })
  })

  it('counts repeated colors in a multi-choice vote', () => {
    state.stagiaires = [
      { id: 'a', name: 'A', connected: true, vote: ['rouge', 'rouge', 'vert'] }
    ]
    expect(getColorCounts()).toEqual({ rouge: 2, vert: 1 })
  })
})

describe('getCombinations', () => {
  it('returns [] when no votes', () => {
    state.stagiaires = [{ id: 'a', name: 'A', connected: true, vote: null }]
    expect(getCombinations()).toEqual([])
  })

  it('groups identical combinations and sorts by count desc', () => {
    state.stagiaires = [
      { id: 'a', name: 'A', connected: true, vote: ['rouge', 'vert'] },
      { id: 'b', name: 'B', connected: true, vote: ['vert', 'rouge'] }, // same combo, order-insensitive
      { id: 'c', name: 'C', connected: true, vote: ['bleu'] }
    ]
    const combos = getCombinations()
    expect(combos).toHaveLength(2)
    expect(combos[0].count).toBe(2)
    expect(combos[0].colors.sort()).toEqual(['rouge', 'vert'])
    expect(combos[1].count).toBe(1)
  })

  it('does not include blank votes as a combination of zero colors', () => {
    state.stagiaires = [
      { id: 'a', name: 'A', connected: true, vote: [] },
      { id: 'b', name: 'B', connected: true, vote: ['rouge'] }
    ]
    const combos = getCombinations()
    expect(combos).toEqual([{ colors: ['rouge'], count: 1 }])
  })
})

describe('sortStagiaires', () => {
  it('places non-voters before voters', () => {
    state.stagiaires = [
      { id: 'a', name: 'Voter', connected: true, vote: ['rouge'] },
      { id: 'b', name: 'Watcher', connected: true, vote: null }
    ]
    const sorted = sortStagiaires(state.stagiaires)
    expect(sorted.map((s) => s.id)).toEqual(['b', 'a'])
  })

  it('sorts voters by combination popularity, then by name', () => {
    state.stagiaires = [
      { id: 'a', name: 'Zoe', connected: true, vote: ['rouge'] }, // rare combo
      { id: 'b', name: 'Amy', connected: true, vote: ['vert'] },
      { id: 'c', name: 'Bob', connected: true, vote: ['vert'] } // popular combo
    ]
    const sorted = sortStagiaires(state.stagiaires)
    // 'vert' has count 2 (Bob and Amy, alphabetical), 'rouge' has 1 (Zoe)
    expect(sorted.map((s) => s.id)).toEqual(['b', 'c', 'a'])
  })

  it('falls back to "Anonyme" for sorting when name is missing', () => {
    // Same combo, no names — both collapse to "anonyme" and stay in
    // their original order.
    state.stagiaires = [
      { id: 'a', name: '', connected: true, vote: ['rouge'] },
      { id: 'b', name: null, connected: true, vote: ['rouge'] }
    ]
    const sorted = sortStagiaires(state.stagiaires)
    expect(sorted.map((s) => s.id)).toEqual(['a', 'b'])
  })
})
