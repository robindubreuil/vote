import { describe, it, expect } from 'vitest'
import { COLORS, getColorById, sanitizeColor } from './colors.js'

describe('COLORS constant', () => {
  it('exposes eight well-known colors', () => {
    expect(COLORS).toHaveLength(8)
    expect(COLORS.map((c) => c.id)).toEqual(['rouge', 'vert', 'bleu', 'jaune', 'orange', 'violet', 'rose', 'gris'])
  })

  it('every palette colour is a 6-digit hex token (sanitizeColor contract)', () => {
    for (const c of COLORS) {
      expect(c.color).toMatch(/^#[0-9a-fA-F]{6}$/)
    }
  })
})

describe('getColorById', () => {
  it('returns the requested colour', () => {
    expect(getColorById('vert').name).toBe('Vert')
  })

  it('falls back to the first colour for unknown IDs', () => {
    expect(getColorById('nope').id).toBe('rouge')
  })
})

// F10: sanitizeColor is the defense-in-depth tripwire that lets the
// style="background-color: ..." interpolations stay safe the day a
// custom-palette feature lets operator-supplied values reach the DOM.
describe('sanitizeColor (F10)', () => {
  it('accepts 3-digit hex tokens', () => {
    expect(sanitizeColor('#fff')).toBe('#fff')
    expect(sanitizeColor('#ABC')).toBe('#ABC')
  })

  it('accepts 6-digit hex tokens', () => {
    expect(sanitizeColor('#ef4444')).toBe('#ef4444')
    expect(sanitizeColor('#000000')).toBe('#000000')
  })

  it('accepts 4- and 8-digit hex tokens with alpha', () => {
    expect(sanitizeColor('#abcd')).toBe('#abcd')
    expect(sanitizeColor('#01234567')).toBe('#01234567')
  })

  it('rejects named colours', () => {
    expect(sanitizeColor('red')).toBe('#666666')
    expect(sanitizeColor('transparent')).toBe('#666666')
    expect(sanitizeColor('currentColor')).toBe('#666666')
  })

  it('rejects CSS colour functions (rgb/hsl/url)', () => {
    expect(sanitizeColor('rgb(255,0,0)')).toBe('#666666')
    expect(sanitizeColor('hsl(0 100% 50%)')).toBe('#666666')
    expect(sanitizeColor('url(javascript:alert(1))')).toBe('#666666')
  })

  it('rejects broken hex tokens (wrong length, stray chars)', () => {
    expect(sanitizeColor('#12')).toBe('#666666')
    expect(sanitizeColor('#12345')).toBe('#666666')
    expect(sanitizeColor('#1234567')).toBe('#666666')
    expect(sanitizeColor('#gggggg')).toBe('#666666')
    expect(sanitizeColor('#ff ffff')).toBe('#666666')
  })

  it('rejects payloads that try to break out of the style attribute', () => {
    expect(sanitizeColor('#fff"; pointer-events:all')).toBe('#666666')
    expect(sanitizeColor('#fff"><script>')).toBe('#666666')
    expect(sanitizeColor('"></span><script>alert(1)</script>')).toBe('#666666')
  })

  it('rejects non-strings (numbers, objects, undefined)', () => {
    expect(sanitizeColor(undefined)).toBe('#666666')
    expect(sanitizeColor(null)).toBe('#666666')
    expect(sanitizeColor(0xff0000)).toBe('#666666')
    expect(sanitizeColor({ color: '#fff' })).toBe('#666666')
  })

  it('honours a caller-supplied fallback when it is itself a hex token', () => {
    expect(sanitizeColor('garbage', '#abc123')).toBe('#abc123')
    expect(sanitizeColor('#fff', '#abc123')).toBe('#fff')
  })

  it('rejects a caller-supplied fallback that is itself not a hex token', () => {
    expect(sanitizeColor('garbage', 'red')).toBe('#666666')
    expect(sanitizeColor('garbage', 'url(javascript:1)')).toBe('#666666')
  })
})
