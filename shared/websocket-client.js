export class VoteClient {
  constructor(url, options = {}) {
    this.url = url
    this.options = {
      onOpen: () => {},
      onMessage: () => {},
      onClose: () => {},
      onError: () => {},
      onStatusChange: () => {}, // (connected: boolean) => void
      initialReconnectDelay: 2000,
      maxReconnectDelay: 30000,
      ...options
    }

    this.ws = null
    this.reconnectTimeoutId = null
    this.reconnectAttempts = 0
    this.isExplicitlyClosed = false
    this.isConnecting = false
    this.connectionId = 0 // Track connection attempts to prevent race conditions
  }

  connect() {
    // Prevent multiple simultaneous connection attempts
    if (this.isConnecting) {
      return
    }

    this.isExplicitlyClosed = false
    this.isConnecting = true
    this.connectionId++

    // Cleanup existing connection
    if (this.ws) {
      this.ws.close()
      this.ws = null
    }

    if (this.reconnectTimeoutId) {
      clearTimeout(this.reconnectTimeoutId)
      this.reconnectTimeoutId = null
    }

    this.options.onStatusChange(false)

    try {
      this.ws = new WebSocket(this.url)

      this.ws.onopen = () => {
        console.log('WebSocket connecté')
        this.isConnecting = false
        this.reconnectAttempts = 0
        this.options.onStatusChange(true)
        this.options.onOpen()
      }

      // Store current connectionId to prevent race conditions in onclose
      const currentConnectionId = this.connectionId

      this.ws.onmessage = (event) => {
        try {
          const msg = JSON.parse(event.data)
          this.options.onMessage(msg)
        } catch (e) {
          console.error('Erreur parsing message:', e)
        }
      }

      this.ws.onclose = (event) => {
        // Always update status to disconnected
        this.isConnecting = false

        // Only handle close event if this is still the current connection
        if (currentConnectionId !== this.connectionId) {
          return
        }

        this.options.onStatusChange(false)

        if (!this.isExplicitlyClosed) {
          // Check for fatal error codes (4000-4999) which indicate we shouldn't retry
          if (event.code >= 4000 && event.code < 5000) {
             console.error(`Connexion fermée définitivement (Code: ${event.code})`)
             // Do NOT schedule reconnect
          } else {
             console.log('WebSocket déconnecté, tentative de reconnexion...')
             this.scheduleReconnect()
          }
        }
        this.options.onClose()
      }

      this.ws.onerror = (error) => {
        console.error('WebSocket error:', error)
        this.options.onError(error)
      }

    } catch (e) {
      console.error('Erreur connexion WebSocket:', e)
      this.isConnecting = false
      this.scheduleReconnect()
    }
  }

  scheduleReconnect() {
    const delay = Math.min(
      this.options.initialReconnectDelay * Math.pow(2, this.reconnectAttempts),
      this.options.maxReconnectDelay
    )

    this.reconnectAttempts++

    this.reconnectTimeoutId = setTimeout(() => {
      this.isConnecting = false // Reset before attempting new connection
      this.connect()
    }, delay)
  }

  send(data) {
    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify(data))
      return true
    }
    console.warn('Tentative d\'envoi sur WebSocket déconnecté')
    return false
  }

  close() {
    this.isExplicitlyClosed = true
    this.isConnecting = false
    if (this.reconnectTimeoutId) {
      clearTimeout(this.reconnectTimeoutId)
      this.reconnectTimeoutId = null
    }
    if (this.ws) {
      this.ws.close()
    }
  }

  isConnected() {
    return this.ws && this.ws.readyState === WebSocket.OPEN
  }
}
