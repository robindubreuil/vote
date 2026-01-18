/**
 * Shared configuration constants and helpers
 */

/**
 * Generates the WebSocket URL for the current environment.
 * Uses VITE_WS_URL environment variable if defined, otherwise constructs
 * the URL from the current location (ws:// or wss:// based on protocol).
 * @returns {string} The WebSocket URL (e.g., 'ws://localhost:8080/ws')
 */
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
