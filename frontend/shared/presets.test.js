import { beforeEach, describe, expect, it, vi } from 'vitest'

const store = new Map()

vi.stubGlobal('localStorage', {
  getItem: (k) => (store.has(k) ? store.get(k) : null),
  setItem: (k, v) => void store.set(k, String(v)),
  removeItem: (k) => void store.delete(k),
  clear: () => store.clear()
})

vi.stubGlobal('crypto', {
  randomUUID: () => 'uuid-' + Math.random().toString(36).slice(2, 10)
})

const {
  listPresets,
  savePreset,
  deletePreset,
  renamePreset,
  getLastConfig,
  setLastConfig,
  serializePresets,
  deserializePresets,
  _resetForTests,
  _constants
} = await import('./presets.js')

beforeEach(() => {
  store.clear()
  _resetForTests()
})

describe('getLastConfig / setLastConfig', () => {
  it('returns null when nothing stored', () => {
    expect(getLastConfig()).toBeNull()
  })

  it('round-trips a valid config', () => {
    setLastConfig({
      selectedColors: ['rouge', 'vert'],
      colorLabels: { rouge: 'Pour' },
      multipleChoice: true,
      gameEnabled: true
    })
    expect(getLastConfig()).toEqual({
      selectedColors: ['rouge', 'vert'],
      colorLabels: { rouge: 'Pour' },
      multipleChoice: true,
      gameEnabled: true
    })
  })

  it('defaults gameEnabled to false when missing from the input', () => {
    setLastConfig({
      selectedColors: ['rouge'],
      colorLabels: {},
      multipleChoice: false
    })
    expect(getLastConfig()).toEqual({
      selectedColors: ['rouge'],
      colorLabels: {},
      multipleChoice: false,
      gameEnabled: false
    })
  })

  it('drops unknown color ids on read', () => {
    store.set(
      _constants.LAST_CONFIG_KEY,
      JSON.stringify({
        selectedColors: ['rouge', 'turquoise'],
        colorLabels: { rouge: 'Pour', turquoise: 'Nope' },
        multipleChoice: false
      })
    )
    expect(getLastConfig()).toEqual({
      selectedColors: ['rouge'],
      colorLabels: { rouge: 'Pour' },
      multipleChoice: false,
      gameEnabled: false
    })
  })

  it('clamps labels to 12 chars', () => {
    setLastConfig({
      selectedColors: ['rouge'],
      colorLabels: { rouge: 'abcdefghijklmnopqrstuvwxyz' },
      multipleChoice: false
    })
    expect(getLastConfig().colorLabels.rouge).toHaveLength(12)
  })

  it('survives corrupted JSON', () => {
    store.set(_constants.LAST_CONFIG_KEY, '{not json')
    expect(getLastConfig()).toBeNull()
  })

  it('survives quota errors', () => {
    const original = localStorage.setItem
    localStorage.setItem = () => {
      throw new DOMException('quota', 'QuotaExceededError')
    }
    expect(() => setLastConfig({ selectedColors: ['rouge'], colorLabels: {}, multipleChoice: false })).not.toThrow()
    localStorage.setItem = original
  })
})

describe('savePreset / listPresets', () => {
  it('rejects empty names', () => {
    expect(savePreset('   ', { selectedColors: ['rouge'], colorLabels: {}, multipleChoice: false })).toBeNull()
    expect(listPresets()).toHaveLength(0)
  })

  it('rejects empty color sets', () => {
    expect(savePreset('X', { selectedColors: [], colorLabels: {}, multipleChoice: false })).toBeNull()
  })

  it('saves and lists', () => {
    const p = savePreset('Sondage', { selectedColors: ['rouge', 'vert'], colorLabels: {}, multipleChoice: false })
    expect(p).not.toBeNull()
    expect(p.name).toBe('Sondage')
    const all = listPresets()
    expect(all).toHaveLength(1)
    expect(all[0].id).toBe(p.id)
  })

  it('returns presets sorted by createdAt desc', async () => {
    const a = savePreset('A', { selectedColors: ['rouge'], colorLabels: {}, multipleChoice: false })
    await new Promise((r) => setTimeout(r, 5))
    const b = savePreset('B', { selectedColors: ['vert'], colorLabels: {}, multipleChoice: false })
    const all = listPresets()
    expect(all[0].id).toBe(b.id)
    expect(all[1].id).toBe(a.id)
  })

  it('disambiguates duplicate names', () => {
    savePreset('Top', { selectedColors: ['rouge'], colorLabels: {}, multipleChoice: false })
    const second = savePreset('Top', { selectedColors: ['vert'], colorLabels: {}, multipleChoice: false })
    expect(second.name).toBe('Top (2)')
    const third = savePreset('Top', { selectedColors: ['bleu'], colorLabels: {}, multipleChoice: false })
    expect(third.name).toBe('Top (3)')
  })

  it('disambiguates case-insensitively', () => {
    savePreset('Top', { selectedColors: ['rouge'], colorLabels: {}, multipleChoice: false })
    const second = savePreset('TOP', { selectedColors: ['vert'], colorLabels: {}, multipleChoice: false })
    expect(second.name).toBe('TOP (2)')
  })

  it('caps at MAX_PRESETS (oldest evicted)', () => {
    for (let i = 0; i < _constants.MAX_PRESETS + 5; i++) {
      savePreset(`P${i}`, { selectedColors: ['rouge'], colorLabels: {}, multipleChoice: false })
    }
    expect(listPresets()).toHaveLength(_constants.MAX_PRESETS)
    // The most recent (highest index) must still be present, the oldest gone.
    const names = new Set(listPresets().map((p) => p.name))
    expect(names.has(`P${_constants.MAX_PRESETS + 4}`)).toBe(true)
    expect(names.has('P0')).toBe(false)
  })
})

describe('deletePreset', () => {
  it('removes by id', () => {
    const p = savePreset('X', { selectedColors: ['rouge'], colorLabels: {}, multipleChoice: false })
    expect(deletePreset(p.id)).toBe(true)
    expect(listPresets()).toHaveLength(0)
  })

  it('returns true even if id not found (idempotent)', () => {
    expect(deletePreset('does-not-exist')).toBe(true)
  })

  it('ignores falsy ids', () => {
    expect(deletePreset(null)).toBe(false)
    expect(deletePreset('')).toBe(false)
  })
})

describe('renamePreset', () => {
  it('renames an existing preset', () => {
    const p = savePreset('Old', { selectedColors: ['rouge'], colorLabels: {}, multipleChoice: false })
    expect(renamePreset(p.id, 'New')).toBe(true)
    expect(listPresets()[0].name).toBe('New')
  })

  it('rejects empty new name', () => {
    const p = savePreset('Old', { selectedColors: ['rouge'], colorLabels: {}, multipleChoice: false })
    expect(renamePreset(p.id, '   ')).toBe(false)
  })

  it('rejects unknown id', () => {
    expect(renamePreset('nope', 'X')).toBe(false)
  })

  it('clamps long names', () => {
    const p = savePreset('A', { selectedColors: ['rouge'], colorLabels: {}, multipleChoice: false })
    renamePreset(p.id, 'x'.repeat(100))
    expect(listPresets()[0].name).toHaveLength(40)
  })
})

describe('listPresets — corruption recovery', () => {
  it('treats non-array JSON as empty', () => {
    store.set(_constants.PRESETS_KEY, '{"not":"an array"}')
    expect(listPresets()).toEqual([])
  })

  it('drops entries missing required fields', () => {
    store.set(
      _constants.PRESETS_KEY,
      JSON.stringify([
        {
          id: 'a',
          name: 'A',
          config: { selectedColors: ['rouge'], colorLabels: {}, multipleChoice: false },
          createdAt: 1,
          _v: 1
        },
        {
          id: 'b',
          name: '',
          config: { selectedColors: ['rouge'], colorLabels: {}, multipleChoice: false },
          createdAt: 2,
          _v: 1
        },
        {
          id: 'c',
          name: 'C',
          config: { selectedColors: [], colorLabels: {}, multipleChoice: false },
          createdAt: 3,
          _v: 1
        }
      ])
    )
    const all = listPresets()
    expect(all).toHaveLength(1)
    expect(all[0].id).toBe('a')
  })
})

describe('serializePresets / deserializePresets', () => {
  it('exports empty library as valid skeleton', () => {
    const json = serializePresets()
    const parsed = JSON.parse(json)
    expect(parsed.$schema).toBe('vote-presets')
    expect(parsed._v).toBe(_constants.SCHEMA_VERSION)
    expect(parsed.presets).toEqual([])
    expect(typeof parsed.exportedAt).toBe('string')
  })

  it('round-trips presets without ids', () => {
    savePreset('A', { selectedColors: ['rouge'], colorLabels: { rouge: 'Pour' }, multipleChoice: false })
    savePreset('B', { selectedColors: ['vert', 'bleu'], colorLabels: {}, multipleChoice: true })

    const json = serializePresets()
    store.clear() // simulate fresh browser
    const result = deserializePresets(json)

    expect(result.ok).toBe(true)
    expect(result.imported).toBe(2)
    expect(result.skipped).toBe(0)

    const all = listPresets()
    expect(all).toHaveLength(2)
    expect(all.map((p) => p.name).sort()).toEqual(['A', 'B'])
    // Each preset got a fresh local id
    expect(all.every((p) => typeof p.id === 'string' && p.id.length > 0)).toBe(true)
  })

  it('dedupes names when importing into non-empty library', () => {
    savePreset('A', { selectedColors: ['rouge'], colorLabels: {}, multipleChoice: false })
    const json = serializePresets()

    const result = deserializePresets(json)
    expect(result.imported).toBe(1)
    const names = listPresets().map((p) => p.name)
    expect(names).toContain('A')
    expect(names).toContain('A (2)')
  })

  it('rejects malformed JSON', () => {
    const result = deserializePresets('{not json')
    expect(result).toEqual({ ok: false, error: 'invalid-json' })
  })

  it('rejects valid JSON missing presets array', () => {
    const result = deserializePresets(JSON.stringify({ foo: 'bar' }))
    expect(result).toEqual({ ok: false, error: 'invalid-format' })
  })

  it('rejects null', () => {
    const result = deserializePresets('null')
    expect(result).toEqual({ ok: false, error: 'invalid-format' })
  })

  it('skips entries with invalid shape but imports the rest', () => {
    const payload = JSON.stringify({
      $schema: 'vote-presets',
      _v: 1,
      presets: [
        null,
        'not an object',
        { name: 'Good', config: { selectedColors: ['rouge'], colorLabels: {}, multipleChoice: false } },
        { name: 'No config' },
        { name: '', config: { selectedColors: ['rouge'], colorLabels: {}, multipleChoice: false } }
      ]
    })
    const result = deserializePresets(payload)
    expect(result.ok).toBe(true)
    expect(result.imported).toBe(1)
    expect(result.skipped).toBe(4)
  })

  it('sanitises imported colors against current palette', () => {
    const payload = JSON.stringify({
      $schema: 'vote-presets',
      _v: 1,
      presets: [
        {
          name: 'Foreign',
          config: {
            selectedColors: ['rouge', 'turquoise', 'cerise'],
            colorLabels: { rouge: 'Pour', turquoise: 'Nope' },
            multipleChoice: false
          }
        }
      ]
    })
    const result = deserializePresets(payload)
    expect(result.imported).toBe(1)
    const p = listPresets()[0]
    expect(p.config.selectedColors).toEqual(['rouge'])
    expect(p.config.colorLabels).toEqual({ rouge: 'Pour' })
  })

  it('respects MAX_PRESETS cap on import', () => {
    const payload = {
      $schema: 'vote-presets',
      _v: 1,
      presets: Array.from({ length: _constants.MAX_PRESETS + 10 }, (_, i) => ({
        name: `P${i}`,
        config: { selectedColors: ['rouge'], colorLabels: {}, multipleChoice: false }
      }))
    }
    const result = deserializePresets(JSON.stringify(payload))
    expect(result.imported).toBeLessThanOrEqual(_constants.MAX_PRESETS)
    expect(listPresets()).toHaveLength(_constants.MAX_PRESETS)
  })
})
