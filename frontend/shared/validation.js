import { CONSTANTS } from './config.js'

/**
 * Validates a user name.
 *
 * The trim happens BEFORE the length check so a name like "  Marie  "
 * (which the user might paste or autocomplete) is measured by its
 * effective length, not its raw length. Without this, "  aaaaaaaaaaaaaaaa  "
 * (16 letters padded to 20) would be rejected even though it trims to a
 * valid 16-char name. Mirrors the backend's `IsValidName` (BL4).
 *
 * The character set is intentionally restrictive: letters, digits,
 * spaces, hyphens, and apostrophes only. No periods, commas, or other
 * punctuation — the goal is to keep classroom first-names simple and
 * predictable for the trainer reading them aloud.
 * @param {string} name - The name to validate
 * @returns {string|null} - Error message or null if valid
 */
export function validateName(name) {
  const trimmed = typeof name === 'string' ? name.trim() : ''

  if (trimmed.length === 0) {
    return 'Le prénom est requis'
  }

  if (trimmed.length > CONSTANTS.MAX_NAME_LENGTH) {
    return `Le prénom est trop long (maximum ${CONSTANTS.MAX_NAME_LENGTH} caractères)`
  }

  // Check for invalid characters (allow letters, numbers, spaces, hyphens, apostrophes)
  // Using negative approach for better Unicode support
  for (const char of trimmed) {
    const isLetter = /\p{L}/u.test(char)
    const isDigit = /\p{N}/u.test(char)
    const isSpace = char === ' '
    const isHyphen = char === '-'
    const isApostrophe = char === "'" || char === '`'

    if (!isLetter && !isDigit && !isSpace && !isHyphen && !isApostrophe) {
      return `Caractère non autorisé: "${char}" (lettres, chiffres, espaces, tirets et apostrophes uniquement)`
    }
  }

  return null // No error
}

/**
 * Validates a session code.
 * Accepts lowercase input (normalized to uppercase before matching).
 * @param {string} code - The session code to validate
 * @returns {string|null} - Error message or null if valid
 */
export function validateSessionCode(code) {
  if (!code) {
    return 'Le code session est requis'
  }

  const normalized = CONSTANTS.SESSION_CODE_NORMALIZE(code)
  if (!CONSTANTS.SESSION_CODE_REGEX.test(normalized)) {
    return `Le code doit contenir ${CONSTANTS.SESSION_CODE_LENGTH} lettres`
  }

  return null
}
