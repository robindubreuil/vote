import { CONSTANTS } from './config.js'

/**
 * Validates a user name.
 * @param {string} name - The name to validate
 * @returns {string|null} - Error message or null if valid
 */
export function validateName(name) {
  if (!name || name.length === 0) {
    return 'Le prénom est requis'
  }

  if (name.length > CONSTANTS.MAX_NAME_LENGTH) {
    return `Le prénom est trop long (maximum ${CONSTANTS.MAX_NAME_LENGTH} caractères)`
  }

  // Check for invalid characters (allow letters, numbers, spaces, hyphens, apostrophes)
  // Using negative approach for better Unicode support
  for (const char of name) {
    const isLetter = /\p{L}/u.test(char)
    const isDigit = /\p{N}/u.test(char)
    const isSpace = char === ' '
    const isHyphen = char === '-'
    const isApostrophe = char === '\'' || char === '`'

    if (!isLetter && !isDigit && !isSpace && !isHyphen && !isApostrophe) {
      return `Caractère non autorisé: "${char}" (lettres, chiffres, espaces, tirets et apostrophes uniquement)`
    }
  }

  return null // No error
}

/**
 * Validates a session code.
 * @param {string} code - The session code to validate
 * @returns {string|null} - Error message or null if valid
 */
export function validateSessionCode(code) {
  if (!code) {
    return 'Le code session est requis'
  }
  
  if (!CONSTANTS.SESSION_CODE_REGEX.test(code)) {
    return `Le code doit contenir ${CONSTANTS.SESSION_CODE_LENGTH} chiffres`
  }
  
  return null
}
