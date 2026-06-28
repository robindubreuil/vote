import { showError, showToast } from '@shared/ui.js'
import { COLORS } from '@shared/colors.js'
import {
  listPresets,
  savePreset,
  deletePreset,
  getLastConfig,
  setLastConfig,
  serializePresets,
  deserializePresets
} from '@shared/presets.js'
import { t } from '@shared/i18n.js'
import { state } from './state.js'
import { renderMainContent, attachConfigListeners } from './renderers.js'
import { getClient } from './websocket.js'
import { safeSessionSet } from '@shared/utils/safe-storage.js'

export async function resetConfig() {
  state.selectedColors = new Set(COLORS.slice(0, 3).map((c) => c.id))
  state.colorLabels = {}
  state.multipleChoice = false
  state.gameEnabled = false
  state.presetSaving = false
  renderMainContent()
  const client = getClient()
  if (client) {
    attachConfigListeners(client)
  }
}

export function beginSavePreset() {
  state.presetSaving = true
  renderMainContent()
  const client = getClient()
  if (client) attachConfigListeners(client)
}

export function cancelSavePreset() {
  state.presetSaving = false
  renderMainContent()
  const client = getClient()
  if (client) attachConfigListeners(client)
}

export function confirmSavePreset(rawName) {
  const name = String(rawName || '').trim()
  if (!name) {
    showToast(t.formateur.presetNameRequired, { type: 'error' })
    return
  }
  const saved = savePreset(name, {
    selectedColors: Array.from(state.selectedColors),
    colorLabels: state.colorLabels,
    multipleChoice: state.multipleChoice,
    gameEnabled: state.gameEnabled
  })
  if (!saved) {
    showToast(t.formateur.presetSaveFailed, { type: 'error' })
    return
  }
  state.presetSaving = false
  renderMainContent()
  const client = getClient()
  if (client) attachConfigListeners(client)
  showToast(t.formateur.presetSaved)
}

export function applyPreset(id) {
  const preset = listPresets().find((p) => p.id === id)
  if (!preset) return
  state.selectedColors = new Set(preset.config.selectedColors)
  state.colorLabels = { ...preset.config.colorLabels }
  state.multipleChoice = preset.config.multipleChoice
  state.gameEnabled = Boolean(preset.config.gameEnabled)
  renderMainContent()
  const client = getClient()
  if (client) attachConfigListeners(client)
  showToast(`${t.formateur.presetApplied} : ${preset.name}`)
}

export function deletePresetHandler(id) {
  const preset = listPresets().find((p) => p.id === id)
  if (!preset) return
  deletePreset(id)
  renderMainContent()
  const client = getClient()
  if (client) attachConfigListeners(client)
  showToast(`${t.formateur.presetDeleted} : ${preset.name}`)
}

/**
 * Export every preset to a JSON file the trainer can share or restore later.
 * No-op (with info toast) if the library is empty — avoids creating a hollow
 * skeleton file the user might mistake for a real backup.
 */
export function exportPresetsHandler() {
  const presets = listPresets()
  if (presets.length === 0) {
    showToast(t.formateur.noPresetsToExport, { type: 'info' })
    return
  }
  const json = serializePresets()
  const blob = new Blob([json], { type: 'application/json' })
  const url = URL.createObjectURL(blob)
  const today = new Date().toISOString().slice(0, 10)
  const a = document.createElement('a')
  a.href = url
  a.download = `vote-presets-${today}.json`
  document.body.appendChild(a)
  a.click()
  document.body.removeChild(a)
  // Give the browser a tick to start the download before revoking.
  setTimeout(() => URL.revokeObjectURL(url), 0)
  showToast(t.formateur.presetsExported(presets.length))
}

/**
 * Open a file picker and merge the selected backup into the local library.
 * Each preset is sanitised and name-deduplicated against the existing set.
 */
export function importPresetsHandler() {
  const input = document.createElement('input')
  input.type = 'file'
  input.accept = '.json,application/json'
  input.style.display = 'none'
  input.addEventListener('change', async () => {
    document.body.removeChild(input)
    const file = input.files && input.files[0]
    if (!file) return
    // Hard cap at 1 MB — a legitimate backup is ~10 KB, anything bigger is
    // either malicious or someone imported the wrong file.
    if (file.size > 1024 * 1024) {
      showToast(t.formateur.invalidJsonFile, { type: 'error' })
      return
    }
    let text
    try {
      text = await file.text()
    } catch {
      showToast(t.formateur.invalidJsonFile, { type: 'error' })
      return
    }
    const result = deserializePresets(text)
    if (!result.ok) {
      showToast(result.error === 'invalid-json' ? t.formateur.invalidJsonFile : t.formateur.invalidPresetsFile, {
        type: 'error'
      })
      return
    }
    renderMainContent()
    const client = getClient()
    if (client) attachConfigListeners(client)
    showToast(t.formateur.presetsImported(result.imported), {
      type: result.imported > 0 ? 'success' : 'info'
    })
  })
  document.body.appendChild(input)
  input.click()
}

/**
 * Restore the most recently used config (autoload layer).
 * Called once per page lifecycle on session_created. No-op if the user has
 * never started a vote before, or if we've already autoloaded this session.
 */
export function applyLastConfigIfAvailable() {
  if (state.lastConfigApplied) return
  state.lastConfigApplied = true
  const last = getLastConfig()
  if (!last || last.selectedColors.length === 0) return
  state.selectedColors = new Set(last.selectedColors)
  state.colorLabels = { ...last.colorLabels }
  state.multipleChoice = last.multipleChoice
  state.gameEnabled = Boolean(last.gameEnabled)
  renderMainContent()
  const client = getClient()
  if (client) attachConfigListeners(client)
}

export function startVote(client) {
  if (!client) {
    showError('Erreur de connexion')
    return
  }

  // Persist the committed config so the next session auto-restores it.
  setLastConfig({
    selectedColors: Array.from(state.selectedColors),
    colorLabels: state.colorLabels,
    multipleChoice: state.multipleChoice,
    gameEnabled: state.gameEnabled
  })

  client.send({
    type: 'start_vote',
    sessionCode: state.sessionCode,
    colors: Array.from(state.selectedColors),
    multipleChoice: state.multipleChoice,
    labels: state.colorLabels,
    gameEnabled: state.gameEnabled
  })
}

export function closeVote(client) {
  if (!client) {
    showError('Erreur de connexion')
    return
  }

  client.send({
    type: 'close_vote',
    sessionCode: state.sessionCode
  })
}

export function resetVote(client) {
  if (!client) {
    showError('Erreur de connexion')
    return
  }

  client.send({
    type: 'reset_vote',
    sessionCode: state.sessionCode,
    colors: Array.from(state.selectedColors),
    multipleChoice: state.multipleChoice,
    gameEnabled: state.gameEnabled
  })
}

export function joinSession(code, setLoadingFn, initClientFn) {
  state.sessionCode = code || ''
  state.connecting = true

  if (code) {
    safeSessionSet('vote_session_code', code)
  }

  if (typeof setLoadingFn === 'function') {
    setLoadingFn(true)
  }

  if (typeof initClientFn === 'function') {
    initClientFn()
  }
}
