/**
 * Build the absolute stagiaire URL that the QR code encodes and that the
 * formateur can show as a manual fallback.
 *
 * Format: `<base>#<sessionCode>` — the hash form keeps the URL short and
 * avoids the `?=` noise, which both improves readability (when transcribed
 * manually) and shrinks the QR code (easier to scan from across the room).
 *
 * Resolution order for the base URL:
 *   1. `VITE_STAGIAIRE_BASE_URL` env var (full URL including path), for
 *      asymmetric deployments — e.g. formateur at /formateur but stagiaire
 *      at the root of a sub-domain.
 *   2. Derivation from the current formateur pathname: any `/formateur/`
 *      segment is swapped for `/stagiaire/`, preserving any deployment
 *      sub-path (e.g. "/app/formateur/" -> "/app/stagiaire/").
 *   3. Defensive fallback: `/stagiaire/` at the current origin.
 *
 * @param {string} sessionCode
 * @param {{ origin: string, pathname: string }} [loc] location-like object;
 *        defaults to window.location. The override makes this function pure
 *        and unit-testable.
 * @returns {string}
 */
export function buildJoinURL(sessionCode, loc) {
  const envOverride = import.meta.env && import.meta.env.VITE_STAGIAIRE_BASE_URL
  if (envOverride) {
    const normalized = envOverride.endsWith('/') ? envOverride : envOverride + '/'
    return `${normalized}#${encodeURIComponent(sessionCode)}`
  }

  const location = loc || window.location
  const formateurIdx = location.pathname.indexOf('/formateur/')
  let basePath
  if (formateurIdx !== -1) {
    basePath = location.pathname.slice(0, formateurIdx) + '/'
  } else {
    basePath = '/'
  }
  return `${location.origin}${basePath}#${encodeURIComponent(sessionCode)}`
}
