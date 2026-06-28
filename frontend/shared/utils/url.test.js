import { describe, it, expect, afterEach, beforeEach } from 'vitest'
import { getSessionCodeFromURL } from './url.js'

// The helper reads window.location.hash and window.location.search, which we
// cannot mutate directly in JSDOM-free tests. We stub the global with the
// shape the implementation actually touches.
function setLocation({ hash = '', search = '' } = {}) {
  const loc = { hash, search }
  Object.defineProperty(globalThis, 'window', {
    configurable: true,
    writable: true,
    value: { location: loc }
  })
}

afterEach(() => {
  // Restore a minimal window so other tests in the suite don't break.
  Object.defineProperty(globalThis, 'window', {
    configurable: true,
    writable: true,
    value: globalThis.window || { location: { hash: '', search: '' } }
  })
})

describe('getSessionCodeFromURL', () => {
  beforeEach(() => {
    // ensure each test starts from a clean global window
    setLocation({ hash: '', search: '' })
  })

  it('reads the code from the hash (#ABC)', () => {
    setLocation({ hash: '#ABC' })
    expect(getSessionCodeFromURL()).toBe('ABC')
  })

  it('falls back to ?session= when no hash is set (legacy URLs)', () => {
    setLocation({ search: '?session=DEF' })
    expect(getSessionCodeFromURL()).toBe('DEF')
  })

  it('prefers the hash over the query string when both are present', () => {
    setLocation({ hash: '#KQR', search: '?session=XYZ' })
    expect(getSessionCodeFromURL()).toBe('KQR')
  })

  it('returns null when neither hash nor ?session= is set', () => {
    setLocation({})
    expect(getSessionCodeFromURL()).toBeNull()
  })

  it('returns null for an empty hash', () => {
    setLocation({ hash: '' })
    expect(getSessionCodeFromURL()).toBeNull()
  })

  it('trims whitespace around the hash value', () => {
    setLocation({ hash: '#  ABC  ' })
    expect(getSessionCodeFromURL()).toBe('ABC')
  })
})
