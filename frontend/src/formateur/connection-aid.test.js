import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { buildJoinURL } from './connection-aid-url.js'

describe('connection-aid — buildJoinURL', () => {
  it('uses the short root hash form: <origin>/#<code>', () => {
    expect(buildJoinURL('ABC', { origin: 'http://localhost:5173', pathname: '/formateur/' })).toBe(
      'http://localhost:5173/#ABC'
    )
  })

  it('handles formateur/index.html form', () => {
    expect(buildJoinURL('DEF', { origin: 'http://vote.example', pathname: '/formateur/index.html' })).toBe(
      'http://vote.example/#DEF'
    )
  })

  it('works under a subpath deployment', () => {
    expect(buildJoinURL('KQR', { origin: 'https://host', pathname: '/app/formateur/' })).toBe(
      'https://host/app/#KQR'
    )
  })

  it('url-encodes the session code (defensive)', () => {
    expect(buildJoinURL('A C', { origin: 'http://h', pathname: '/formateur/' })).toBe('http://h/#A%20C')
  })

  it('falls back to root when no /formateur/ segment is present', () => {
    expect(buildJoinURL('ABC', { origin: 'http://h', pathname: '/' })).toBe('http://h/#ABC')
    expect(buildJoinURL('ABC', { origin: 'http://h', pathname: '/some-other-path' })).toBe('http://h/#ABC')
  })

  it('always ends in /#<code>', () => {
    const url = buildJoinURL('ABC', { origin: 'http://h', pathname: '/formateur/' })
    expect(url).toMatch(/\/#ABC$/)
  })
})

describe('connection-aid — buildJoinURL with VITE_STAGIAIRE_BASE_URL override', () => {
  beforeEach(() => {
    vi.stubEnv('VITE_STAGIAIRE_BASE_URL', 'https://vote.example.com/')
  })

  afterEach(() => {
    vi.unstubAllEnvs()
  })

  it('uses the env override verbatim when set', () => {
    expect(buildJoinURL('ABC', { origin: 'http://other', pathname: '/formateur/' })).toBe(
      'https://vote.example.com/#ABC'
    )
  })

  it('appends a trailing slash to the override if missing', () => {
    vi.stubEnv('VITE_STAGIAIRE_BASE_URL', 'https://vote.example.com')
    expect(buildJoinURL('ABC')).toBe('https://vote.example.com/#ABC')
  })

  it('preserves a sub-path supplied in the override', () => {
    vi.stubEnv('VITE_STAGIAIRE_BASE_URL', 'https://example.com/stagiaire/')
    expect(buildJoinURL('DEF')).toBe('https://example.com/stagiaire/#DEF')
  })
})
