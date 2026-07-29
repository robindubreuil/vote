import { COLORS, escapeHtml, sanitizeColor } from '@shared/colors.js'
import { icons } from '@shared/icons.js'
import { state } from './state.js'
import { getColorCounts } from './vote-data.js'
import { renderCombinationsHTML, renderStagiairesVotesHTML } from './renderers.js'

// Re-export the data helpers from their new home in `vote-data.js` so
// existing call sites (formateur/main.test.js, etc.) keep working
// without touching their imports. New code should import directly from
// `./vote-data.js`.
export { getColorCounts, getCombinations, sortStagiaires } from './vote-data.js'

/**
 * Update color bars with optimized DOM manipulation
 * @param {Array} activeColors
 * @param {Object.<string, number>} colorCounts
 * @param {number} maxCount
 */
export function updateColorBars(activeColors, colorCounts, maxCount) {
  const container = document.querySelector('.color-bars')
  if (!container) return

  // Sort colors by popularity
  const sortedColors = [...activeColors].sort((a, b) => (colorCounts[b.id] || 0) - (colorCounts[a.id] || 0))

  // Create a Map of existing elements for fast lookup
  const existingRows = new Map()
  Array.from(container.children).forEach((row) => {
    const colorId = row.getAttribute('data-color')
    if (colorId) existingRows.set(colorId, row)
  })

  // Rebuild HTML with correct order
  const fragment = document.createDocumentFragment()
  sortedColors.forEach((color) => {
    const count = colorCounts[color.id] || 0
    const percent = (count / maxCount) * 100

    let row = existingRows.get(color.id)

    if (row) {
      // Update existing row
      const countEl = row.querySelector('.color-bar-count')
      const fillEl = row.querySelector('.color-bar-fill')

      if (countEl && countEl.textContent !== count.toString()) {
        countEl.textContent = count
      }

      if (fillEl) {
        fillEl.style.width = `${percent}%`
        if (count === 0) {
          fillEl.classList.add('empty')
        } else {
          fillEl.classList.remove('empty')
        }
      }

      // Re-append to ensure correct order
      fragment.appendChild(row)
    } else {
      // Create new row
      row = document.createElement('div')
      row.className = 'color-bar-row'
      row.setAttribute('data-color', color.id)
      row.innerHTML = `
        <div class="color-bar-label">
          <span class="color-bar-swatch" style="background-color: ${sanitizeColor(color.color)}"></span>
          <span class="color-bar-name">${escapeHtml(state.colorLabels[color.id] || color.name)}</span>
        </div>
        <div class="color-bar-track">
          <div class="color-bar-fill ${count === 0 ? 'empty' : ''}" style="width: ${percent}%; background-color: ${sanitizeColor(color.color)}"></div>
        </div>
        <span class="color-bar-count">${count}</span>
      `
      fragment.appendChild(row)
    }
  })

  container.innerHTML = ''
  container.appendChild(fragment)
}

/**
 * Start the vote timer
 */
export function startTimer() {
  stopTimer()
  state.timerInterval = setInterval(() => {
    const timerEl = document.querySelector('.vote-timer')
    if (timerEl && state.voteStartTime) {
      const elapsed = Math.max(0, Math.floor((Date.now() - state.voteStartTime) / 1000))
      const mins = Math.floor(elapsed / 60)
        .toString()
        .padStart(2, '0')
      const secs = (elapsed % 60).toString().padStart(2, '0')
      timerEl.innerHTML = `${icons.timer(' class="icon icon-sm"')} ${mins}:${secs}`
    }
  }, 1000)
}

/**
 * Stop the vote timer
 */
export function stopTimer() {
  if (state.timerInterval) {
    clearInterval(state.timerInterval)
    state.timerInterval = null
  }
}

/**
 * Update vote results display
 *
 * F14: previously this reached into `renderers.js` via a dynamic
 * import on every call. `renderers.js` statically imports the data
 * helpers from `vote-data.js` (extracted from this module), so the
 * static import below no longer forms a cycle and a failed chunk load
 * can no longer silently stall the vote panel.
 */
export function updateVoteResults() {
  // Count votes from stagiaires
  const voteCount = state.stagiaires.filter((s) => s.vote && s.vote.length > 0).length

  // Update vote count
  const voteCountEl = document.querySelector('.vote-count')
  if (voteCountEl) {
    voteCountEl.innerHTML = `${icons.chart(' class="icon icon-sm"')} ${voteCount} / ${state.connectedCount} votes`
  }

  const activeColors = COLORS.filter((c) => state.selectedColors.has(c.id))
  const colorCounts = getColorCounts()
  const maxCount = Math.max(...Object.values(colorCounts), 1)

  // Optimized update of color bars
  updateColorBars(activeColors, colorCounts, maxCount)

  const combinationsList = document.querySelector('.combinations-list')
  if (combinationsList) {
    combinationsList.innerHTML = renderCombinationsHTML()
  }

  const stagiairesVotesList = document.querySelector('.stagiaires-votes-list')
  if (stagiairesVotesList) {
    stagiairesVotesList.innerHTML = renderStagiairesVotesHTML()
  }
}
