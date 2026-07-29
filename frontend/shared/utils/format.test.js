import { describe, it, expect } from 'vitest'
import { formatConnectedCount } from './format.js'

// F16: pluralisation was duplicated between the formateur config card
// and the connection-aid view. The two copies had drifted in the past
// (one used strict > 1, the other also implicitly handled 0). The
// helper is the single source of truth now — these tests pin the
// agreement rule so a future "quick fix" can't reintroduce the drift.
describe('formatConnectedCount', () => {
  it('uses the singular form for exactly one stagiaire', () => {
    expect(formatConnectedCount(1)).toBe('1 stagiaire connecté')
  })

  it('uses the singular form for zero (French agreement after "zéro")', () => {
    // Standard French grammar: "zéro stagiaire connecté" is singular.
    // The snapshot tests pin this behaviour; the helper preserves it.
    expect(formatConnectedCount(0)).toBe('0 stagiaire connecté')
  })

  it('uses the plural form for two or more', () => {
    expect(formatConnectedCount(2)).toBe('2 stagiaires connectés')
    expect(formatConnectedCount(30)).toBe('30 stagiaires connectés')
  })

  it('floors fractional input', () => {
    expect(formatConnectedCount(1.9)).toBe('1 stagiaire connecté')
    expect(formatConnectedCount(2.1)).toBe('2 stagiaires connectés')
  })

  it('coerces numeric strings', () => {
    expect(formatConnectedCount('3')).toBe('3 stagiaires connectés')
  })

  it('treats non-numeric input as zero', () => {
    expect(formatConnectedCount(NaN)).toBe('0 stagiaire connecté')
    expect(formatConnectedCount(null)).toBe('0 stagiaire connecté')
    expect(formatConnectedCount(undefined)).toBe('0 stagiaire connecté')
    expect(formatConnectedCount('oops')).toBe('0 stagiaire connecté')
  })

  it('clamps negative input to zero', () => {
    expect(formatConnectedCount(-5)).toBe('0 stagiaire connecté')
  })
})
