/**
 * Gets session code from URL query parameter
 * @returns {string|null} Session code from URL or null
 */
export function getSessionCodeFromURL() {
  const params = new URLSearchParams(window.location.search)
  const session = params.get('session')
  return session || null
}

/**
 * Sets session code in URL query parameter
 * @param {string} code - Session code to set
 */
export function setSessionCodeInURL(code) {
  const url = new URL(window.location)
  if (code) {
    url.searchParams.set('session', code)
  } else {
    url.searchParams.delete('session')
  }
  window.history.replaceState({}, '', url)
}
