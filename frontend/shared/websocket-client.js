export class VoteClient {
  constructor(url, options = {}) {
    this.url = url
    this.options = {
      onOpen: () => {},
      onMessage: () => {},
      onClose: () => {},
      onError: () => {},
      onStatusChange: () => {}, // (connected: boolean) => void
      // F25: fired exactly once when the client gives up for good —
      // either after maxReconnectAttempts (≈16h backoff at default
      // cadence) or an application-defined 4xxx permanent close. The
      // app uses it to swap the "Reconnexion…" banner to a recoverable
      // "rechargez la page" state, since the client has stopped trying
      // and the last onStatusChange(false) is all the app would
      // otherwise see.
      onPermanentClose: () => {},
      initialReconnectDelay: 2000,
      maxReconnectDelay: 30000,
      // Ceiling on reconnect attempts before the client gives up. Default
      // ~50 attempts ≈ 16h at the default 2s→30s exponential backoff, which
      // is well beyond any plausible transient outage and bounds memory/CPU
      // use if the server is gone for good.
      maxReconnectAttempts: 50,
      ...options
    }

    this.ws = null
    this.reconnectTimeoutId = null
    this.reconnectAttempts = 0
    this.isExplicitlyClosed = false
    this.isPermanentlyClosed = false
    this.isConnecting = false
    this.connectionId = 0 // Track connection attempts to prevent race conditions

    // navigator.onLine is `undefined` in non-browser environments (SSR,
    // test runs without a DOM). Treat that as "always online" so we don't
    // pause reconnects indefinitely in environments that never fire the
    // online event.
    this.online = typeof navigator !== 'undefined' && typeof navigator.onLine === 'boolean' ? navigator.onLine : true

    this._bindOnlineEvents()
  }

  /**
   * Listen for browser online/offline events so reconnects pause while the
   * OS reports no network (no point burning attempts against a dead NIC)
   * and resume immediately when connectivity returns.
   *
   * R3: idempotent — unbinds any previously-bound handlers first so a
   * `connect()` following a `close()` re-arms the listeners. Without this,
   * a stagiaire who leaves one session and joins another without a reload
   * (close → connect on the same instance) loses offline/online awareness:
   * `_unbindOnlineEvents()` ran in `close()`, and `connect()` didn't
   * re-bind, so a classroom wifi drop burned reconnect attempts against a
   * dead NIC with no fast-retry on `online` — exactly the regression
   * FH1/FH2 were built to prevent.
   */
  _bindOnlineEvents() {
    if (typeof window === 'undefined' || typeof window.addEventListener !== 'function') {
      return
    }
    // Drop any prior bindings so repeat calls don't stack handlers.
    this._unbindOnlineEvents()
    this._onOnline = () => {
      this.online = true
      // Fast-retry on connectivity return. Covers two cases:
      //   1. A reconnect was pending — cancel it and reconnect now.
      //   2. scheduleReconnect skipped because we were offline (this.online
      //      was false) so no timer was ever set — reconnect now.
      if (this.isExplicitlyClosed || this.isPermanentlyClosed) return
      if (this.isConnected() || this.isConnecting) return
      if (this.reconnectTimeoutId) {
        clearTimeout(this.reconnectTimeoutId)
        this.reconnectTimeoutId = null
      }
      this.isConnecting = false
      this._doConnect()
    }
    this._onOffline = () => {
      this.online = false
      // Pause any pending reconnect — it'll either fire on the online
      // event or be rescheduled when _doConnect runs again.
      if (this.reconnectTimeoutId) {
        clearTimeout(this.reconnectTimeoutId)
        this.reconnectTimeoutId = null
      }
    }
    window.addEventListener('online', this._onOnline)
    window.addEventListener('offline', this._onOffline)
  }

  _unbindOnlineEvents() {
    if (typeof window === 'undefined' || typeof window.removeEventListener !== 'function') {
      return
    }
    if (this._onOnline) window.removeEventListener('online', this._onOnline)
    if (this._onOffline) window.removeEventListener('offline', this._onOffline)
    // Drop references so a stale handler can't be re-removed (and so
    // _bindOnlineEvents's "already bound?" implicit check via these
    // fields behaves predictably).
    this._onOnline = null
    this._onOffline = null
  }

  connect() {
    if (this.isConnecting) {
      return
    }

    this.isExplicitlyClosed = false
    this.isPermanentlyClosed = false
    this.reconnectAttempts = 0
    // R3: re-arm online/offline awareness. close() unbinds these; a
    // connect() on the same instance (leave → rejoin without a reload)
    // must re-bind so a wifi drop still pauses reconnects.
    this._bindOnlineEvents()
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
        // Late close from a previous connection attempt: ignore it so we
        // don't double-fire status changes or schedule a reconnect against
        // the fresh socket.
        if (currentConnectionId !== this.connectionId) {
          return
        }

        this.isConnecting = false
        this.options.onStatusChange(false)

        if (!this.isExplicitlyClosed) {
          if (event.code >= 4000 && event.code < 5000) {
            // Application-defined permanent close — stop trying.
            this.isPermanentlyClosed = true
            console.error(`Connection closed permanently (code: ${event.code})`)
            this.options.onPermanentClose(event.code)
          } else {
            this.scheduleReconnect()
          }
        }
        this.options.onClose(event)
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

  /**
   * Schedule the next reconnect with exponential backoff + jitter.
   *
   * Backoff: initialReconnectDelay * 2^attempts, capped at maxReconnectDelay.
   * Jitter: 50–100% of the base delay. Without jitter, every client behind
   * a shared-NAT classroom retries in lockstep after a server restart and
   // they all hammer the per-IP rate limiter at the same instant.
   *
   * Offline awareness: if `navigator.onLine === false`, the timer is NOT
   * scheduled — the browser `online` event triggers an immediate retry.
   */
  scheduleReconnect() {
    if (this.isPermanentlyClosed || this.isExplicitlyClosed) {
      return
    }

    if (this.reconnectAttempts >= this.options.maxReconnectAttempts) {
      this.isPermanentlyClosed = true
      console.error(`Max reconnect attempts (${this.options.maxReconnectAttempts}) reached; giving up`)
      // F25: notify the app so the "Reconnexion…" banner can swap to a
      // recoverable "rechargez la page" state instead of promising a
      // reconnect that will never come.
      this.options.onPermanentClose()
      return
    }

    const base = Math.min(
      this.options.initialReconnectDelay * Math.pow(2, this.reconnectAttempts),
      this.options.maxReconnectDelay
    )
    // Jitter: spread 30+ clients across [50%, 100%] of the base interval.
    const jittered = base * (0.5 + Math.random() * 0.5)
    const delay = Math.min(jittered, this.options.maxReconnectDelay)

    this.reconnectAttempts++

    // If the browser is offline, don't schedule — the `online` event will
    // trigger an immediate retry via _onOnline.
    if (!this.online) {
      return
    }

    this.reconnectTimeoutId = setTimeout(() => {
      this.reconnectTimeoutId = null
      this.isConnecting = false
      this._doConnect()
    }, delay)
  }

  send(data) {
    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify(data))
      return true
    }
    console.warn("Tentative d'envoi sur WebSocket déconnecté")
    return false
  }

  close() {
    this.isExplicitlyClosed = true
    this.isConnecting = false
    if (this.reconnectTimeoutId) {
      clearTimeout(this.reconnectTimeoutId)
      this.reconnectTimeoutId = null
    }
    this._unbindOnlineEvents()
    if (this.ws) {
      this.ws.close()
    }
  }

  isConnected() {
    return this.ws && this.ws.readyState === WebSocket.OPEN
  }
}
