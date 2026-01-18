import { COLORS } from '../../shared/colors.js'
import { icons } from '../../shared/icons.js'
import { state } from './state.js'

/**
 * Calculate vote counts per color
 * @returns {Object.<string, number>} Counts by color ID
 */
export function getColorCounts() {
  const counts = {}
  state.stagiaires.forEach(s => {
    if (s.vote) {
      s.vote.forEach(colorId => {
        counts[colorId] = (counts[colorId] || 0) + 1
      })
    }
  })
  return counts
}

/**
 * Calculate vote combinations
 * @returns {Array<{colors: string[], count: number}>} Sorted combinations by count desc
 */
export function getCombinations() {
  const comboMap = new Map()

  state.stagiaires.forEach(s => {
    if (s.vote && s.vote.length > 0) {
      const key = s.vote.slice().sort().join('+')
      comboMap.set(key, (comboMap.get(key) || 0) + 1)
    }
  })

  return Array.from(comboMap.entries())
    .map(([key, count]) => ({
      colors: key ? key.split('+') : [],
      count
    }))
    .sort((a, b) => b.count - a.count)
}

/**
 * Sort stagiaires by vote status and name
 * Non-voters first, then by combination popularity, then by name
 * @param {Array} stagiaires
 * @returns {Array} Sorted array
 */
export function sortStagiaires(stagiaires) {
  // Calculate popularity of each combination (among voters)
  const comboPopularity = new Map()
  stagiaires.forEach(s => {
    if (s.vote && s.vote.length > 0) {
      const key = s.vote.slice().sort().join('+')
      comboPopularity.set(key, (comboPopularity.get(key) || 0) + 1)
    }
  })

  return [...stagiaires].sort((a, b) => {
    const aHasVoted = a.vote && a.vote.length > 0
    const bHasVoted = b.vote && b.vote.length > 0

    // Non-voters first
    if (aHasVoted !== bHasVoted) {
      return aHasVoted ? 1 : -1
    }

    // If both voted, sort by combination popularity
    if (aHasVoted && bHasVoted) {
      const keyA = a.vote.slice().sort().join('+')
      const keyB = b.vote.slice().sort().join('+')
      const popularityA = comboPopularity.get(keyA) || 0
      const popularityB = comboPopularity.get(keyB) || 0

      if (popularityB !== popularityA) {
        return popularityB - popularityA
      }
    }

    // Same status: sort by name
    const nameA = (a.name || 'Anonyme').toLowerCase()
    const nameB = (b.name || 'Anonyme').toLowerCase()
    return nameA.localeCompare(nameB)
  })
}

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
  Array.from(container.children).forEach(row => {
    const colorId = row.getAttribute('data-color')
    if (colorId) existingRows.set(colorId, row)
  })

  // Rebuild HTML with correct order
  const fragment = document.createDocumentFragment()
  sortedColors.forEach(color => {
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
          <span class="color-bar-swatch" style="background-color: ${color.color}"></span>
          <span class="color-bar-name">${state.colorLabels[color.id] || color.name}</span>
        </div>
        <div class="color-bar-track">
          <span class="color-bar-count">${count}</span>
          <div class="color-bar-fill ${count === 0 ? 'empty' : ''}" style="width: ${percent}%; background-color: ${color.color}"></div>
        </div>
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
      const elapsed = Math.floor((Date.now() - state.voteStartTime) / 1000)
      const mins = Math.floor(elapsed / 60).toString().padStart(2, '0')
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
 */
export function updateVoteResults() {
  // Count votes from stagiaires
  const voteCount = state.stagiaires.filter(s => s.vote && s.vote.length > 0).length

  // Update vote count
  const voteCountEl = document.querySelector('.vote-count')
  if (voteCountEl) {
    voteCountEl.innerHTML = `${icons.chart(' class="icon icon-sm"')} ${voteCount} / ${state.connectedCount} votes`
  }

  const activeColors = COLORS.filter(c => state.selectedColors.has(c.id))
  const colorCounts = getColorCounts()
  const maxCount = Math.max(...Object.values(colorCounts), 1)

  // Optimized update of color bars
  updateColorBars(activeColors, colorCounts, maxCount)

  // Update combinations and stagiaires lists via dynamic import
  import('./renderers.js').then(({ renderCombinationsHTML, renderStagiairesVotesHTML }) => {
    const combinationsList = document.querySelector('.combinations-list')
    if (combinationsList) {
      combinationsList.innerHTML = renderCombinationsHTML()
    }

    const stagiairesVotesList = document.querySelector('.stagiaires-votes-list')
    if (stagiairesVotesList) {
      stagiairesVotesList.innerHTML = renderStagiairesVotesHTML()
    }
  })
}
