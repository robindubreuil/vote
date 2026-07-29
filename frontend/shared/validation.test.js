import { describe, it, expect } from 'vitest'
import { validateName, validateSessionCode } from './validation.js'

// F18: validateName measured length on the raw input before trimming.
// "  aaaaaaaaaaaaaaaa  " (16 letters padded to 20) was rejected even
// though it trims to a valid 16-char name. The fix mirrors the
// backend's IsValidName (BL4). These tests pin the trim-first rule.
describe('validateName — trim-first (F18)', () => {
  it('rejects an empty name', () => {
    expect(validateName('')).toBe('Le prénom est requis')
    expect(validateName(null)).toBe('Le prénom est requis')
    expect(validateName(undefined)).toBe('Le prénom est requis')
  })

  it('rejects whitespace-only input after trim', () => {
    expect(validateName('   ')).toBe('Le prénom est requis')
    expect(validateName('\t\t')).toBe('Le prénom est requis')
  })

  it('accepts a name that trims to <= MAX_NAME_LENGTH', () => {
    // 16 letters padded to 20 — fails the length check pre-trim,
    // passes post-trim.
    expect(validateName('  aaaaaaaaaaaaaaaa  ')).toBeNull()
  })

  it('rejects a name that trims to > MAX_NAME_LENGTH', () => {
    // 17 letters padded to 21 — still too long after trim.
    expect(validateName('  aaaaaaaaaaaaaaaaa  ')).toBe(
      'Le prénom est trop long (maximum 16 caractères)'
    )
  })

  it('measures length by UTF-16 code units after trim', () => {
    // 16 chars — boundary
    expect(validateName('a'.repeat(16))).toBeNull()
    // 17 chars — one over
    expect(validateName('a'.repeat(17))).toBe('Le prénom est trop long (maximum 16 caractères)')
  })

  it('rejects forbidden punctuation (period, comma, exclamation)', () => {
    // Document restriction (F18): only letters, digits, spaces,
    // hyphens, and apostrophes. Periods, commas, and other
    // punctuation are rejected to keep classroom first-names
    // predictable for the trainer reading them aloud.
    expect(validateName('St. Pierre')).toMatch(/Caractère non autorisé/)
    expect(validateName('Martin, Jr')).toMatch(/Caractère non autorisé/)
    expect(validateName('Test!')).toMatch(/Caractère non autorisé/)
  })

  it('accepts letters, digits, spaces, hyphens, and apostrophes', () => {
    expect(validateName('Marie')).toBeNull()
    expect(validateName('Jean-Pierre')).toBeNull()
    expect(validateName("O'Brien")).toBeNull()
    expect(validateName('Anne Marie')).toBeNull()
    expect(validateName('émilie')).toBeNull() // Unicode letters
    expect(validateName('User42')).toBeNull()
  })

  it('does not mutate the caller\'s input', () => {
    const input = '  Marie  '
    validateName(input)
    expect(input).toBe('  Marie  ')
  })
})

describe('validateSessionCode', () => {
  it('accepts a valid 3-letter uppercase code', () => {
    expect(validateSessionCode('ABC')).toBeNull()
  })

  it('accepts lowercase (frontend regex is case-insensitive; caller normalises)', () => {
    expect(validateSessionCode('abc')).toBeNull()
  })

  it('rejects codes containing disambiguation-risky letters (I, O, Z)', () => {
    expect(validateSessionCode('IOA')).toMatch(/Le code doit contenir 3 lettres/)
    expect(validateSessionCode('XYZ')).toMatch(/Le code doit contenir 3 lettres/)
  })

  it('rejects too-short / too-long codes', () => {
    expect(validateSessionCode('AB')).toMatch(/Le code doit contenir 3 lettres/)
    expect(validateSessionCode('ABCD')).toMatch(/Le code doit contenir 3 lettres/)
  })

  it('rejects empty input', () => {
    expect(validateSessionCode('')).toBe('Le code session est requis')
    expect(validateSessionCode(null)).toBe('Le code session est requis')
  })
})
