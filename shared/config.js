/**
 * Shared configuration constants and helpers
 */

// WebSocket URL generation
export const getWebSocketURL = () => {
  // If defined in environment variables (Vite), use it
  if (import.meta.env.VITE_WS_URL) {
    return import.meta.env.VITE_WS_URL
  }
  
  // Otherwise construct from current location
  const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:'
  return `${protocol}//${location.host}/ws`
}

// Application Constants
export const CONSTANTS = {
  MAX_NAME_LENGTH: 16,
  SESSION_CODE_LENGTH: 4,
  SESSION_CODE_REGEX: /^\d{4}$/
}
