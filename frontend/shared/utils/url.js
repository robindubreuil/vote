/**
 * Extract the session code from the current URL.
 *
 * Accepts both formats:
 *   - Hash form (preferred, short):  /stagiaire/#1234
 *   - Query form (legacy):           /stagiaire/?session=1234
 *
 * The hash form is preferred because it produces a much shorter URL (and
 * therefore a simpler, easier-to-scan QR code), and avoids the `?=` noise.
 * The query form is kept as a fallback so existing printed QR codes or
 * bookmarks keep working.
 *
 * @returns {string|null} the 4-digit code, or null if none found
 */
export function getSessionCodeFromURL() {
  // Hash form: location.hash looks like "#1234" (or empty).
  // Strip the leading '#' and any accidental whitespace.
  if (window.location.hash) {
    const fromHash = window.location.hash.replace(/^#/, '').trim()
    if (fromHash) return fromHash
  }

  // Legacy query form: ?session=1234
  const params = new URLSearchParams(window.location.search)
  const fromQuery = params.get('session')
  return fromQuery || null
}
