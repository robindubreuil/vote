// Persistent storage for vote layouts (a "vote layout" = the configurable
// part of a vote: which colors are selected, their custom labels, and
// whether multiple choice is enabled).
//
// Two layers, both backed by localStorage:
//   1. Last-config autoload — transparent. The config from the most recent
//      start_vote is restored automatically when the formateur creates a
//      new session.
//   2. Named presets — explicit. The trainer saves a setup with a name
//      ("Sondage d'opinion", "Évaluation à 5 niveaux"...) and re-applies
//      it later in a single click.
//
// Schema is versioned so future migrations can run on read.

import { COLORS } from './colors.js'

const PRESETS_KEY = 'vote:presets'
const LAST_CONFIG_KEY = 'vote:lastConfig'
// v1: initial schema (selectedColors, colorLabels, multipleChoice).
// v2: added gameEnabled. Reading v1 just defaults gameEnabled=false.
// v3: added competitive + correctColors. Same pattern — sanitizeConfig
// coerces missing fields to defaults.
const SCHEMA_VERSION = 3

const MAX_PRESETS = 30

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

function safeParse(raw, fallback) {
  if (!raw) return fallback
  try {
    const parsed = JSON.parse(raw)
    return parsed == null ? fallback : parsed
  } catch {
    return fallback
  }
}

// Strip unknown color IDs and clamp label length so a preset saved against
// an older palette still loads cleanly if COLORS changes shape.
function sanitizeConfig(config) {
  if (!config || typeof config !== 'object') return null
  const knownIds = new Set(COLORS.map((c) => c.id))

  const rawColors = Array.isArray(config.selectedColors) ? config.selectedColors : []
  const selectedColors = rawColors.filter((id) => typeof id === 'string' && knownIds.has(id))

  const rawLabels = config.colorLabels && typeof config.colorLabels === 'object' ? config.colorLabels : {}
  const colorLabels = {}
  for (const [id, label] of Object.entries(rawLabels)) {
    if (knownIds.has(id) && typeof label === 'string' && label.trim()) {
      colorLabels[id] = label.slice(0, 12)
    }
  }

  return {
    selectedColors,
    colorLabels,
    multipleChoice: Boolean(config.multipleChoice),
    gameEnabled: Boolean(config.gameEnabled),
    competitive: Boolean(config.competitive),
    allowBlank: Boolean(config.allowBlank),
    correctColors: Array.isArray(config.correctColors)
      ? config.correctColors.filter((id) => typeof id === 'string' && knownIds.has(id))
      : []
  }
}

function withMeta(preset) {
  return {
    id: String(preset.id),
    name: String(preset.name || '')
      .slice(0, 40)
      .trim(),
    createdAt: Number(preset.createdAt) || Date.now(),
    _v: SCHEMA_VERSION,
    config: sanitizeConfig(preset.config)
  }
}

// ---------------------------------------------------------------------------
// Last-config autoload
// ---------------------------------------------------------------------------

export function getLastConfig() {
  const raw = safeParse(localStorage.getItem(LAST_CONFIG_KEY), null)
  return sanitizeConfig(raw)
}

export function setLastConfig(config) {
  const clean = sanitizeConfig(config)
  if (!clean) return
  try {
    localStorage.setItem(LAST_CONFIG_KEY, JSON.stringify({ ...clean, _v: SCHEMA_VERSION }))
  } catch {
    // Quota exceeded or storage disabled (private mode) — silently ignore.
  }
}

// ---------------------------------------------------------------------------
// Named presets
// ---------------------------------------------------------------------------

export function listPresets() {
  const arr = safeParse(localStorage.getItem(PRESETS_KEY), [])
  if (!Array.isArray(arr)) return []
  return arr
    .map(withMeta)
    .filter((p) => p.name && p.config && p.config.selectedColors.length > 0)
    .sort((a, b) => b.createdAt - a.createdAt)
}

export function getPreset(id) {
  return listPresets().find((p) => p.id === id) || null
}

// Returns the saved preset (with assigned id) or null if validation failed.
export function savePreset(name, config) {
  const clean = sanitizeConfig(config)
  if (!clean || clean.selectedColors.length === 0) return null
  const trimmed = String(name || '').trim()
  if (!trimmed) return null

  const presets = listPresets()
  // Unique name: append " (2)", " (3)" if needed.
  let finalName = trimmed
  let suffix = 2
  const existingNames = new Set(presets.map((p) => p.name.toLowerCase()))
  while (existingNames.has(finalName.toLowerCase())) {
    finalName = `${trimmed} (${suffix++})`
  }

  const preset = {
    id:
      typeof crypto !== 'undefined' && crypto.randomUUID
        ? crypto.randomUUID()
        : `p-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`,
    name: finalName,
    createdAt: Date.now(),
    _v: SCHEMA_VERSION,
    config: clean
  }

  const next = [preset, ...presets].slice(0, MAX_PRESETS)
  try {
    localStorage.setItem(PRESETS_KEY, JSON.stringify(next))
  } catch {
    return null
  }
  return preset
}

export function deletePreset(id) {
  if (!id) return false
  const target = String(id)
  const next = listPresets().filter((p) => p.id !== target)
  try {
    localStorage.setItem(PRESETS_KEY, JSON.stringify(next))
  } catch {
    return false
  }
  return true
}

export function renamePreset(id, newName) {
  const trimmed = String(newName || '').trim()
  if (!trimmed) return false
  const presets = listPresets()
  const idx = presets.findIndex((p) => p.id === String(id))
  if (idx === -1) return false
  presets[idx] = { ...presets[idx], name: trimmed.slice(0, 40) }
  try {
    localStorage.setItem(PRESETS_KEY, JSON.stringify(presets))
  } catch {
    return false
  }
  return true
}

// ---------------------------------------------------------------------------
// Backup / restore (export / import)
// ---------------------------------------------------------------------------

const BACKUP_MARKER = 'vote-presets'

/**
 * Serialize all presets to a portable JSON string. Format:
 *   { "$schema": "vote-presets", "_v": 1, "exportedAt": ISO, "presets": [...] }
 * Preset IDs and the autoload last-config are excluded — IDs are per-browser
 * and would only cause phantom collisions on import.
 * @returns {string} pretty-printed JSON
 */
export function serializePresets() {
  const presets = listPresets().map((p) => ({
    name: p.name,
    createdAt: p.createdAt,
    config: p.config
  }))
  return JSON.stringify(
    {
      $schema: BACKUP_MARKER,
      _v: SCHEMA_VERSION,
      exportedAt: new Date().toISOString(),
      presets
    },
    null,
    2
  )
}

/**
 * Parse a backup file and import each preset via savePreset (which sanitises
 * the config and de-duplicates names against the existing library).
 * @param {string} jsonText
 * @returns {{ ok: true, imported: number, skipped: number } | { ok: false, error: 'invalid-json' | 'invalid-format' }}
 */
export function deserializePresets(jsonText) {
  let data
  try {
    data = JSON.parse(jsonText)
  } catch {
    return { ok: false, error: 'invalid-json' }
  }
  if (!data || typeof data !== 'object' || !Array.isArray(data.presets)) {
    return { ok: false, error: 'invalid-format' }
  }

  // Count by measuring actual storage growth — savePreset silently evicts
  // the oldest entry when the library hits MAX_PRESETS, so a "successful"
  // return value doesn't always mean a new row was added.
  let imported = 0
  let skipped = 0
  for (const p of data.presets) {
    const before = listPresets().length
    if (before >= MAX_PRESETS) {
      // Library full: count every remaining entry as skipped.
      skipped += data.presets.length - (imported + skipped)
      break
    }
    const grew = (() => {
      if (!p || typeof p !== 'object') return false
      const result = savePreset(p.name, p.config)
      if (!result) return false
      return listPresets().length > before
    })()
    if (grew) imported++
    else skipped++
  }
  return { ok: true, imported, skipped }
}

// Test-only: clear everything. Used by unit tests; not exported through
// the public surface used by the UI.
export function _resetForTests() {
  localStorage.removeItem(PRESETS_KEY)
  localStorage.removeItem(LAST_CONFIG_KEY)
}

export const _constants = { PRESETS_KEY, LAST_CONFIG_KEY, SCHEMA_VERSION, MAX_PRESETS }
