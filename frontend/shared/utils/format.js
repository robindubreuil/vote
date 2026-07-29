// Small formatting helpers. Kept here so pluralisation rules live in a
// single place — French agreement ("stagiaire(s) connecté(s)") was
// duplicated between the formateur config card and the connection-aid
// view, and the two copies had drifted in the past (F16).

/**
 * Format the "N stagiaire(s) connecté(s)" line shown to the trainer.
 *
 * French agreement rule: the noun ("stagiaire") and the past
 * participle ("connecté") both take a trailing "s" only when N is
 * strictly greater than 1. N === 0 ("zéro stagiaire connecté") and
 * N === 1 ("un stagiaire connecté") are both singular, which matches
 * standard French grammar and the behaviour the snapshot tests have
 * always pinned.
 * @param {number} count
 * @returns {string}
 */
export function formatConnectedCount(count) {
  const n = Math.max(0, Math.floor(Number(count) || 0))
  const s = n > 1 ? 's' : ''
  return `${n} stagiaire${s} connecté${s}`
}
