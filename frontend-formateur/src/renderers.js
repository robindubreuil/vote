import { COLORS, escapeHtml } from '../../shared/colors.js'
import { icons } from '../../shared/icons.js'
import { renderFooterHTML, renderSessionCodeButton } from '../../shared/ui.js'
import { state } from './state.js'
import { getCombinations, sortStagiaires, getColorCounts } from './utils.js'

/**
 * Render the landing page
 * @param {HTMLElement} app
 */
export function renderLandingPage(app) {
  app.innerHTML = `
    <div class="container">
      <div class="landing-card card">
        <div class="landing-icon">${icons.vote(' class="icon"')}</div>
        <h1 class="landing-title">Vote Coloré</h1>
        <p class="landing-subtitle">Interface Formateur</p>

        <div class="landing-actions">
          <button id="createSessionBtn" class="btn btn-primary btn-large">
            ${icons.plus(' class="icon icon-md"')} Créer une nouvelle session
          </button>

          <div class="landing-divider">OU</div>

          <div class="input-group">
            <input type="text" id="joinSessionInput" class="input-text" placeholder="Code session (ex: 1234)" maxlength="4" pattern="[0-9]{4}" inputmode="numeric">
            <button id="joinSessionBtn" class="btn btn-secondary">
              Rejoindre
            </button>
          </div>
          <div class="error-message" role="alert"></div>
        </div>
      </div>
    </div>
    ${renderFooterHTML()}
  `
}

/**
 * Update landing page loading state
 * @param {boolean} isLoading
 */
export function updateLandingPageLoadingState(isLoading) {
  const createBtn = document.getElementById('createSessionBtn')
  const joinBtn = document.getElementById('joinSessionBtn')
  const joinInput = document.getElementById('joinSessionInput')

  if (!createBtn || !joinBtn) return

  if (isLoading) {
    createBtn.disabled = true
    joinBtn.disabled = true
    joinInput.disabled = true
    createBtn.innerHTML = `${icons.loader(' class="icon icon-md spin"')} Connexion...`
  } else {
    createBtn.disabled = false
    joinBtn.disabled = false
    joinInput.disabled = false
    createBtn.innerHTML = `${icons.plus(' class="icon icon-md"')} Créer une nouvelle session`
  }
}

/**
 * Render the full app layout
 * @param {HTMLElement} app
 */
export function renderFullLayout(app) {
  app.innerHTML = `
    <div class="container">
      <header class="header" id="app-header"></header>
      <main id="app-content"></main>
    </div>
    ${renderFooterHTML()}
  `
}

/**
 * Update the header
 * @param {Object} client
 */
export function updateHeader(client) {
  const header = document.getElementById('app-header')
  if (!header) return

  const isConnected = client ? client.isConnected() : state.connected

  header.innerHTML = `
    <h1>${icons.vote(' class="icon icon-md"')} Vote Coloré - Formateur</h1>
    <div class="header-right">
      ${renderSessionCodeButton(state.sessionCode, isConnected)}
    </div>
  `
}

/**
 * Attach header event listeners
 * @param {Object} client
 * @param {Function} renderLandingPageFn
 */
export function attachHeaderListeners(client, renderLandingPageFn) {
  const leaveSessionBtn = document.getElementById('leaveSessionBtn')
  if (leaveSessionBtn) {
    leaveSessionBtn.addEventListener('click', () => {
      if (confirm('Voulez-vous quitter cette session ?')) {
        sessionStorage.removeItem('vote_session_code')
        if (client) {
          client.close()
        }
        // Return to landing page
        state.sessionCode = null
        state.connectedCount = 0
        state.voteState = 'idle'
        renderLandingPageFn(document.getElementById('app'))
      }
    })
  }
}

/**
 * Render the main content area
 */
export function renderMainContent() {
  const main = document.getElementById('app-content')
  if (!main) return

  if (state.voteState === 'idle') {
    main.innerHTML = renderConfigHTML()
  } else {
    main.innerHTML = renderVoteHTML()
  }
}

/**
 * Attach config page event listeners
 * @param {Object} client
 */
export function attachConfigListeners(client) {
  // Color checkboxes
  document.querySelectorAll('.color-checkbox input[type="checkbox"]').forEach(checkbox => {
    checkbox.addEventListener('change', (e) => {
      const colorId = e.target.value
      if (e.target.checked) {
        state.selectedColors.add(colorId)
      } else {
        state.selectedColors.delete(colorId)
      }

      // Update selected class
      const parent = e.target.closest('.color-checkbox')
      parent.classList.toggle('selected', e.target.checked)

      // Update button state
      const startBtn = document.getElementById('startVote')
      if (startBtn) {
        startBtn.disabled = state.selectedColors.size < 2
      }
    })
  })

  // Label inputs for custom color names
  document.querySelectorAll('.color-label-input').forEach(input => {
    input.addEventListener('input', (e) => {
      const colorId = e.target.dataset.colorId
      const value = e.target.value.trim()
      if (value) {
        state.colorLabels[colorId] = value
      } else {
        delete state.colorLabels[colorId]
      }
    })
  })

  // Multiple choice toggle
  const toggleMultiple = document.querySelector('.multiple-choice-toggle')
  if (toggleMultiple) {
    toggleMultiple.addEventListener('click', (e) => {
      // Avoid double trigger if clicking the inner switch
      if (e.target.dataset.action === 'toggle-multiple') return

      state.multipleChoice = !state.multipleChoice
      const switchEl = toggleMultiple.querySelector('.toggle-switch')
      switchEl.classList.toggle('active', state.multipleChoice)
    })
  }

  // Start vote button
  const startBtn = document.getElementById('startVote')
  if (startBtn) {
    // Import handler dynamically to avoid circular dependency
    import('./handlers.js').then(({ startVote }) => {
      startBtn.addEventListener('click', () => startVote(client))
    })
  }

  // Reset config button
  const resetBtn = document.getElementById('resetConfig')
  if (resetBtn) {
    import('./handlers.js').then(({ resetConfig }) => {
      resetBtn.addEventListener('click', resetConfig)
    })
  }
}

/**
 * Attach vote page event listeners
 * @param {Object} client
 */
export function attachVoteListeners(client) {
  // Close vote button
  const closeBtn = document.getElementById('closeVote')
  if (closeBtn) {
    import('./handlers.js').then(({ closeVote }) => {
      closeBtn.addEventListener('click', () => closeVote(client))
    })
  }

  // New vote button
  const newVoteBtn = document.getElementById('newVote')
  if (newVoteBtn) {
    import('./handlers.js').then(({ resetVote }) => {
      newVoteBtn.addEventListener('click', () => resetVote(client))
    })
  }
}

/**
 * Render the configuration HTML
 * @returns {string}
 */
export function renderConfigHTML() {
  const isConnected = state.connected
  return `
    <div class="card">
      <h2 class="card-title">Configuration du prochain vote</h2>
      <div class="config-info">${icons.users(' class="icon icon-sm"')} ${state.connectedCount} stagiaire${state.connectedCount > 1 ? 's' : ''} connecté${state.connectedCount > 1 ? 's' : ''}</div>

      <div class="config-section">
        <div>
          <div class="stats-header">Couleurs disponibles</div>
          <div class="colors-grid">
            ${COLORS.map(color => {
              const customLabel = state.colorLabels[color.id] || ''
              return `
              <label class="color-checkbox ${state.selectedColors.has(color.id) ? 'selected' : ''}">
                <input
                  type="checkbox"
                  value="${color.id}"
                  ${state.selectedColors.has(color.id) ? 'checked' : ''}
                />
                <span class="color-swatch" style="background-color: ${color.color}"></span>
                <div class="color-label-wrapper">
                  <input
                    type="text"
                    class="color-label-input"
                    data-color-id="${color.id}"
                    value="${escapeHtml(customLabel)}"
                    placeholder="${escapeHtml(color.name)}"
                    maxlength="6"
                  />
                </div>
              </label>
            `}).join('')}
          </div>
        </div>

        <label class="multiple-choice-toggle">
          <span class="toggle-switch ${state.multipleChoice ? 'active' : ''}" data-action="toggle-multiple"></span>
          <span>Choix multiple (autoriser plusieurs couleurs)</span>
        </label>
      </div>

      <div class="button-row">
        <button class="btn btn-secondary" id="resetConfig" ${!isConnected ? 'disabled' : ''}>
          ${icons.refresh(' class="icon icon-md"')} Réinitialiser
        </button>
        <button class="btn btn-primary btn-large" id="startVote" ${state.selectedColors.size < 2 || !isConnected ? 'disabled' : ''}>
          ${icons.rocket(' class="icon icon-md"')} Lancer le vote
        </button>
      </div>
    </div>
  `
}

/**
 * Render the vote HTML
 * @returns {string}
 */
export function renderVoteHTML() {
  const activeColors = COLORS.filter(c => state.selectedColors.has(c.id))

  // Calculate initial stats
  const voteCount = state.stagiaires.filter(s => s.vote && s.vote.length > 0).length
  const colorCounts = getColorCounts()
  const maxCount = Math.max(...Object.values(colorCounts), 1)
  const isConnected = state.connected

  return `
    <div class="card">
      <div class="vote-header">
        <div class="vote-timer">${icons.timer(' class="icon icon-sm"')} 00:00</div>
        <div class="vote-stats">
          <span class="vote-count" aria-live="polite">${icons.chart(' class="icon icon-sm"')} ${voteCount} / ${state.connectedCount} votes</span>
        </div>
      </div>

      <div class="stats-grid stats-grid-3cols">
        <div>
          <div class="stats-header">Par couleur</div>
          <div class="color-bars">
            ${renderColorBarsHTML(activeColors, colorCounts, maxCount)}
          </div>
        </div>

        <div>
          <div class="stats-header">Par combinaison</div>
          <div class="combinations-list custom-scrollbar">
             ${renderCombinationsHTML()}
          </div>
        </div>

        <div>
          <div class="stats-header">Par stagiaire</div>
          <div class="stagiaires-votes-list custom-scrollbar">
             ${renderStagiairesVotesHTML()}
          </div>
        </div>
      </div>

      <div class="button-row">
        ${state.voteState === 'active' ? `
          <button class="btn btn-danger" id="closeVote" ${!isConnected ? 'disabled' : ''}>${icons.stop(' class="icon icon-md"')} Fermer le vote</button>
        ` : `
          <button class="btn btn-success" id="newVote" ${!isConnected ? 'disabled' : ''}>${icons.refresh(' class="icon icon-md"')} Nouveau vote</button>
        `}
      </div>
    </div>
  `
}

/**
 * Render color bars HTML
 * @param {Array} activeColors
 * @param {Object} colorCounts
 * @param {number} maxCount
 * @returns {string}
 */
export function renderColorBarsHTML(activeColors, colorCounts, maxCount) {
  // Sort colors by vote count desc
  const sortedColors = [...activeColors].sort((a, b) => (colorCounts[b.id] || 0) - (colorCounts[a.id] || 0))

  return sortedColors.map(color => {
    const count = colorCounts[color.id] || 0
    const percent = (count / maxCount) * 100
    const displayName = state.colorLabels[color.id] || color.name
    return `
      <div class="color-bar-row" data-color="${color.id}">
        <div class="color-bar-label">
          <span class="color-bar-swatch" style="background-color: ${color.color}"></span>
          <span class="color-bar-name">${escapeHtml(displayName)}</span>
        </div>
        <div class="color-bar-track">
          <span class="color-bar-count">${count}</span>
          <div class="color-bar-fill ${count === 0 ? 'empty' : ''}" style="width: ${percent}%; background-color: ${color.color}"></div>
        </div>
      </div>
    `
  }).join('')
}

/**
 * Render combinations HTML
 * @returns {string}
 */
export function renderCombinationsHTML() {
  const combinations = getCombinations()

  if (combinations.length === 0) {
    return `
      <div class="empty-state">
        <div class="empty-icon">${icons.chart(' class="icon icon-xl"')}</div>
        <div>Aucun vote pour le moment</div>
      </div>
    `
  }

  return combinations.map(combo => {
    return `
      <div class="combo-item">
        <div class="combo-colors">
          ${combo.colors.map(colorId => {
            const color = COLORS.find(c => c.id === colorId)
            return `<span class="combo-swatch" style="background-color: ${color?.color || '#666'}"></span>`
          }).join('')}
        </div>
        <div class="combo-count">${combo.count}</div>
      </div>
    `
  }).join('')
}

/**
 * Render stagiaires votes HTML
 * @returns {string}
 */
export function renderStagiairesVotesHTML() {
  if (state.stagiaires.length === 0) {
    return `
      <div class="empty-state">
        <div class="empty-icon">${icons.users(' class="icon icon-xl"')}</div>
        <div>Aucun stagiaire connecté</div>
      </div>
    `
  }

  const sorted = sortStagiaires(state.stagiaires)

  return sorted.map(s => {
    const displayName = s.name || 'Anonyme'
    const hasVoted = s.vote && s.vote.length > 0
    const isConnected = s.connected

    // Online indicator dot
    const onlineDot = `<span class="online-dot ${isConnected ? 'connected' : 'disconnected'}" title="${isConnected ? 'En ligne' : 'Hors ligne'}"></span>`

    if (!hasVoted) {
      // Non-voter: "waiting" label
      return `
        <div class="stagiaire-vote-item waiting">
          <span class="stagiaire-vote-name">${onlineDot}<span class="name-text" title="${escapeHtml(displayName)}">${escapeHtml(displayName)}</span></span>
          <span class="stagiaire-vote-waiting">En attente</span>
        </div>
      `
    }

    // Voter: show colors
    const colorsHTML = s.vote.map(colorId => {
      const color = COLORS.find(c => c.id === colorId)
      return `<span class="stagiaire-vote-swatch" style="background-color: ${color?.color || '#666'}" title="${color?.name || colorId}"></span>`
    }).join('')

    return `
      <div class="stagiaire-vote-item">
        <span class="stagiaire-vote-name">${onlineDot}<span class="name-text" title="${escapeHtml(displayName)}">${escapeHtml(displayName)}</span></span>
        <div class="stagiaire-vote-colors">${colorsHTML}</div>
      </div>
    `
  }).join('')
}
