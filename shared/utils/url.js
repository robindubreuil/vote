export function getSessionCodeFromURL() {
  const params = new URLSearchParams(window.location.search)
  const session = params.get('session')
  return session || null
}
