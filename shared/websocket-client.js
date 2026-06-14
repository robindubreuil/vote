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
    if (this.isConnecting) {
      return
    }

    this.isExplicitlyClosed = false
    this.reconnectAttempts = 0
    this._doConnect()
  }

  _doConnect() {
    this.isConnecting = true
    this.connectionId++

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
        this.isConnecting = false
        this.reconnectAttempts = 0
        this.options.onStatusChange(true)
        this.options.onOpen()
      }

      const currentConnectionId = this.connectionId

      this.ws.onmessage = (event) => {
        try {
          const msg = JSON.parse(event.data)
          this.options.onMessage(msg)
        } catch (e) {
          console.error('Message parse error:', e)
        }
      }

      this.ws.onclose = (event) => {
        this.isConnecting = false

        if (currentConnectionId !== this.connectionId) {
          return
        }

        this.options.onStatusChange(false)

        if (!this.isExplicitlyClosed) {
          if (event.code >= 4000 && event.code < 5000) {
             console.error(`Connection closed permanently (code: ${event.code})`)
          } else {
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
      console.error('WebSocket connection error:', e)
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
      this.isConnecting = false
      this._doConnect()
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
