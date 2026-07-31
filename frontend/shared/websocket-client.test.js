// @vitest-environment happy-dom
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { VoteClient } from '@shared/websocket-client.js'

// WebSocket.readyState constants — kept on the mock so production code that
// references `WebSocket.OPEN` keeps working.
const CONNECTING = 0
const OPEN = 1
const CLOSING = 2
const CLOSED = 3

class MockWebSocket {
  static instances = []
  static OPEN = OPEN
  static CONNECTING = CONNECTING
  static CLOSING = CLOSING
  static CLOSED = CLOSED

  constructor(url) {
    this.url = url
    this.readyState = CONNECTING
    this.onopen = null
    this.onmessage = null
    this.onclose = null
    this.onerror = null
    this.sent = []
    this.closeCallCount = 0
    MockWebSocket.instances.push(this)
  }

  send(data) {
    this.sent.push(data)
  }

  close() {
    this.closeCallCount++
    // Real WebSocket close() is a no-op if already CLOSING/CLOSED.
    if (this.readyState >= CLOSING) return
    this.readyState = CLOSED
  }

  // Test-only helpers to drive the lifecycle from outside.
  fireOpen() {
    this.readyState = OPEN
    if (this.onopen) this.onopen()
  }
  fireMessage(data) {
    if (this.onmessage) {
      this.onmessage({ data: typeof data === 'string' ? data : JSON.stringify(data) })
    }
  }
  fireError(err) {
    if (this.onerror) this.onerror(err || new Error('test error'))
  }
  fireClose(code = 1000, reason = '') {
    this.readyState = CLOSED
    if (this.onclose) this.onclose({ code, reason, wasClean: true })
  }
}

describe('VoteClient', () => {
  let originalWebSocket
  let originalRandom

  beforeEach(() => {
    vi.useFakeTimers()
    originalWebSocket = globalThis.WebSocket
    originalRandom = Math.random
    globalThis.WebSocket = MockWebSocket
    MockWebSocket.instances.length = 0
    // Pin Math.random so jitter is deterministic. The client multiplies the
    // base delay by (0.5 + random*0.5); pinning random to 0 makes the
    // effective delay exactly half the base, which is easy to assert against.
    Math.random = () => 0
    // happy-dom ships navigator.onLine defaulting to true.
  })

  afterEach(() => {
    vi.useRealTimers()
    globalThis.WebSocket = originalWebSocket
    Math.random = originalRandom
    MockWebSocket.instances.length = 0
  })

  it('connects and reports status changes', () => {
    const status = vi.fn()
    const client = new VoteClient('ws://x', { onStatusChange: status })
    client.connect()

    expect(MockWebSocket.instances).toHaveLength(1)
    expect(status).toHaveBeenCalledWith(false)
    expect(status).toHaveBeenCalledTimes(1)

    MockWebSocket.instances[0].fireOpen()
    expect(status).toHaveBeenCalledWith(true)
    expect(client.isConnected()).toBe(true)
    client.close()
  })

  it('parses JSON messages and forwards them to onMessage', () => {
    const messages = vi.fn()
    const client = new VoteClient('ws://x', { onMessage: messages })
    client.connect()

    const ws = MockWebSocket.instances[0]
    ws.fireOpen()
    ws.fireMessage({ type: 'hello' })

    expect(messages).toHaveBeenCalledWith({ type: 'hello' })
    client.close()
  })

  it('swallows malformed JSON without crashing', () => {
    const messages = vi.fn()
    const errSpy = vi.spyOn(console, 'error').mockImplementation(() => {})
    const client = new VoteClient('ws://x', { onMessage: messages })
    client.connect()

    const ws = MockWebSocket.instances[0]
    ws.fireOpen()
    ws.fireMessage('{not json')

    expect(messages).not.toHaveBeenCalled()
    errSpy.mockRestore()
    client.close()
  })

  it('send() returns false when not OPEN and true when OPEN', () => {
    const client = new VoteClient('ws://x')
    expect(client.send({ type: 'x' })).toBe(false)

    client.connect()
    const ws = MockWebSocket.instances[0]
    expect(client.send({ type: 'x' })).toBe(false) // still CONNECTING

    ws.fireOpen()
    expect(client.send({ type: 'x' })).toBe(true)
    expect(ws.sent).toHaveLength(1)
    expect(JSON.parse(ws.sent[0])).toEqual({ type: 'x' })
    client.close()
  })

  // P1: both JSON.stringify (circular ref) and ws.send (InvalidStateError
  // after a readyState transition) can throw AFTER the OPEN check passes.
  // All call sites treat send() as a true/false predicate, so a thrown
  // error would escape to the click handler and surface via the global
  // error boundary as a misleading "unexpected error". The try/catch
  // reports the failure as a normal send=false instead.
  describe('send() failure isolation (P1)', () => {
    it('returns false and does not throw when JSON.stringify rejects a circular reference', () => {
      const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {})
      const client = new VoteClient('ws://x')
      client.connect()
      const ws = MockWebSocket.instances[0]
      ws.fireOpen()

      const circular = { a: 1 }
      circular.self = circular
      expect(client.send(circular)).toBe(false)
      // Nothing was handed to the socket.
      expect(ws.sent).toHaveLength(0)
      expect(warnSpy).toHaveBeenCalled()
      warnSpy.mockRestore()
      client.close()
    })

    it('returns false and does not throw when ws.send throws after the readyState check (post-check state transition)', () => {
      const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {})
      const client = new VoteClient('ws://x')
      client.connect()
      const ws = MockWebSocket.instances[0]
      ws.fireOpen()

      // Simulate the socket closing between the OPEN check and ws.send:
      // readyState is still OPEN for the guard, but the underlying send
      // throws InvalidStateError (mirrors a real browser race).
      ws.send = () => {
        throw new DOMException('readyState != OPEN', 'InvalidStateError')
      }
      expect(client.send({ type: 'x' })).toBe(false)
      expect(warnSpy).toHaveBeenCalled()
      warnSpy.mockRestore()
      client.close()
    })
  })

  it('close() prevents any further reconnect attempts', () => {
    const client = new VoteClient('ws://x')
    client.connect()
    const ws = MockWebSocket.instances[0]
    ws.fireOpen()
    ws.fireClose(1006)

    // A reconnect is scheduled — wait for it not to fire after close().
    client.close()
    vi.advanceTimersByTime(60_000)
    expect(MockWebSocket.instances).toHaveLength(1)
  })

  describe('reconnect backoff', () => {
    it('uses exponential backoff capped at maxReconnectDelay', () => {
      // random=0 ⇒ effective delay = base * 0.5
      const client = new VoteClient('ws://x', {
        initialReconnectDelay: 1000,
        maxReconnectDelay: 10_000
      })
      client.connect()
      const ws = MockWebSocket.instances[0]
      ws.fireOpen()

      // Trigger a sequence of close → reconnect → close cycles and verify
      // the delay grows by 2x each attempt up to the cap. Critically, we
      // do NOT fire `open` between attempts — a successful open resets the
      // attempt counter, which we test separately.
      // expectedDelays[i] = base/2 with random=0; base = 1000 * 2^i, capped at 10000.
      //   i=0: base=1000,  delay=500
      //   i=1: base=2000,  delay=1000
      //   i=2: base=4000,  delay=2000
      //   i=3: base=8000,  delay=4000
      //   i=4: base=16000→cap 10000, delay=5000
      const expectedDelays = [500, 1000, 2000, 4000, 5000]
      let current = MockWebSocket.instances[0]
      current.fireOpen()
      for (let i = 0; i < expectedDelays.length; i++) {
        current.fireClose(1006)
        const delay = expectedDelays[i]
        // No new socket before the timer fires.
        expect(MockWebSocket.instances).toHaveLength(i + 1)
        vi.advanceTimersByTime(delay - 1)
        expect(MockWebSocket.instances).toHaveLength(i + 1)
        vi.advanceTimersByTime(1)
        expect(MockWebSocket.instances).toHaveLength(i + 2)
        current = MockWebSocket.instances[i + 1]
      }
      client.close()
    })

    it('spreads reconnect attempts with jitter', () => {
      // With random=0.5, effective delay = base * (0.5 + 0.5*0.5) = base * 0.75
      Math.random = () => 0.5
      const client = new VoteClient('ws://x', {
        initialReconnectDelay: 1000,
        maxReconnectDelay: 10_000
      })
      client.connect()
      const ws = MockWebSocket.instances[0]
      ws.fireOpen()
      ws.fireClose(1006)

      // base = 1000 * 2^0 = 1000, jittered = 1000 * 0.75 = 750
      vi.advanceTimersByTime(749)
      expect(MockWebSocket.instances).toHaveLength(1)
      vi.advanceTimersByTime(1)
      expect(MockWebSocket.instances).toHaveLength(2)
      client.close()
    })

    it('caps delay at maxReconnectDelay even after jitter', () => {
      // random=1 ⇒ effective delay = base * 1.0, but base is already capped.
      Math.random = () => 1
      const client = new VoteClient('ws://x', {
        initialReconnectDelay: 1000,
        maxReconnectDelay: 5_000,
        maxReconnectAttempts: 100
      })
      client.connect()
      let ws = MockWebSocket.instances[0]
      ws.fireOpen()

      // 13 close cycles — base grows 1000, 2000, 4000, 8000→cap 5000, 5000, ...
      // After cap, delay should never exceed 5000.
      for (let i = 0; i < 13; i++) {
        ws.fireClose(1006)
        // Should reconnect within maxReconnectDelay.
        vi.advanceTimersByTime(5_000)
        expect(MockWebSocket.instances).toHaveLength(i + 2)
        ws = MockWebSocket.instances[i + 1]
        ws.fireOpen()
      }
      client.close()
    })

    it('gives up after maxReconnectAttempts and marks client permanently closed', () => {
      const errSpy = vi.spyOn(console, 'error').mockImplementation(() => {})
      const client = new VoteClient('ws://x', {
        initialReconnectDelay: 1,
        maxReconnectDelay: 1,
        maxReconnectAttempts: 3
      })
      client.connect()
      // Burn through all attempts.
      for (let i = 0; i < 3; i++) {
        MockWebSocket.instances[MockWebSocket.instances.length - 1].fireClose(1006)
        vi.advanceTimersByTime(10)
      }
      expect(MockWebSocket.instances).toHaveLength(4) // initial + 3 retries

      // The 4th close should NOT trigger another reconnect.
      MockWebSocket.instances[MockWebSocket.instances.length - 1].fireClose(1006)
      vi.advanceTimersByTime(10_000)
      expect(MockWebSocket.instances).toHaveLength(4)
      expect(client.isPermanentlyClosed).toBe(true)
      errSpy.mockRestore()
    })

    // F25: the app must learn the client gave up so it can swap its
    // "Reconnexion…" banner to a recoverable "rechargez la page" state.
    // Before the fix the flag was set silently — the last
    // onStatusChange(false) was all the app saw, leaving the banner up
    // forever.
    it('F25: fires onPermanentClose exactly once when max attempts are reached', () => {
      const errSpy = vi.spyOn(console, 'error').mockImplementation(() => {})
      const onPermanentClose = vi.fn()
      const client = new VoteClient('ws://x', {
        initialReconnectDelay: 1,
        maxReconnectDelay: 1,
        maxReconnectAttempts: 3,
        onPermanentClose
      })
      client.connect()
      // Burn through all 3 attempts (each close schedules a reconnect
      // because attempts < max). After this loop we have 4 sockets and
      // reconnectAttempts == 3 — the next close is the give-up trigger.
      for (let i = 0; i < 3; i++) {
        MockWebSocket.instances[MockWebSocket.instances.length - 1].fireClose(1006)
        vi.advanceTimersByTime(10)
      }
      expect(onPermanentClose).toHaveBeenCalledTimes(0)

      // The give-up close: scheduleReconnect sees attempts >= max,
      // flips isPermanentlyClosed, and fires onPermanentClose.
      MockWebSocket.instances[MockWebSocket.instances.length - 1].fireClose(1006)
      expect(onPermanentClose).toHaveBeenCalledTimes(1)
      expect(client.isPermanentlyClosed).toBe(true)

      // A further close must NOT re-fire: the client already gave up
      // and scheduleReconnect early-returns on isPermanentlyClosed.
      MockWebSocket.instances[MockWebSocket.instances.length - 1].fireClose(1006)
      vi.advanceTimersByTime(10_000)
      expect(onPermanentClose).toHaveBeenCalledTimes(1)
      errSpy.mockRestore()
    })

    it('resets attempt counter after a successful open', () => {
      const errSpy = vi.spyOn(console, 'error').mockImplementation(() => {})
      const client = new VoteClient('ws://x', {
        initialReconnectDelay: 1000,
        maxReconnectDelay: 1000,
        maxReconnectAttempts: 5
      })
      client.connect()

      // Burn 4 attempts (one short of giving up), then succeed.
      for (let i = 0; i < 4; i++) {
        MockWebSocket.instances[MockWebSocket.instances.length - 1].fireClose(1006)
        vi.advanceTimersByTime(1000)
      }
      expect(MockWebSocket.instances).toHaveLength(5)
      MockWebSocket.instances[4].fireOpen()

      // Counter reset — we should be able to fail 5 more times before giving up.
      for (let i = 0; i < 5; i++) {
        MockWebSocket.instances[MockWebSocket.instances.length - 1].fireClose(1006)
        vi.advanceTimersByTime(1000)
      }
      expect(MockWebSocket.instances).toHaveLength(10)
      // 6th failure → no new socket.
      MockWebSocket.instances[MockWebSocket.instances.length - 1].fireClose(1006)
      vi.advanceTimersByTime(10_000)
      expect(MockWebSocket.instances).toHaveLength(10)
      expect(client.isPermanentlyClosed).toBe(true)
      errSpy.mockRestore()
      client.close()
    })
  })

  describe('close-code branching', () => {
    it('treats 4xxx codes as permanent and stops reconnecting', () => {
      const errSpy = vi.spyOn(console, 'error').mockImplementation(() => {})
      const client = new VoteClient('ws://x')
      client.connect()
      MockWebSocket.instances[0].fireClose(4001)

      vi.advanceTimersByTime(60_000)
      expect(MockWebSocket.instances).toHaveLength(1)
      expect(client.isPermanentlyClosed).toBe(true)
      errSpy.mockRestore()
    })

    // F25: the 4xxx permanent-close branch must also surface to the app.
    it('F25: fires onPermanentClose with the code on a 4xxx permanent close', () => {
      const errSpy = vi.spyOn(console, 'error').mockImplementation(() => {})
      const onPermanentClose = vi.fn()
      const client = new VoteClient('ws://x', { onPermanentClose })
      client.connect()
      MockWebSocket.instances[0].fireClose(4001)

      expect(onPermanentClose).toHaveBeenCalledTimes(1)
      // The close code is forwarded so the app can branch on it if needed.
      expect(onPermanentClose).toHaveBeenCalledWith(4001)
      expect(client.isPermanentlyClosed).toBe(true)
      errSpy.mockRestore()
    })

    it('reconnects on normal close codes (1006, 1011, etc.)', () => {
      const client = new VoteClient('ws://x', {
        initialReconnectDelay: 1000,
        maxReconnectDelay: 1000
      })
      client.connect()
      MockWebSocket.instances[0].fireClose(1006)
      vi.advanceTimersByTime(500) // jittered with random=0 ⇒ base*0.5
      expect(MockWebSocket.instances).toHaveLength(2)
      client.close()
    })
  })

  describe('connectionId race', () => {
    it('ignores close events from a superseded connection', () => {
      const status = vi.fn()
      const client = new VoteClient('ws://x', { onStatusChange: status })
      client.connect()
      const firstWs = MockWebSocket.instances[0]

      // Supersede: a fresh _doConnect bumps connectionId and creates a new ws.
      client._doConnect()
      const secondWs = MockWebSocket.instances[1]
      expect(secondWs).not.toBe(firstWs)

      // Late close from the first connection — must NOT trigger reconnect
      // or status change.
      status.mockClear()
      firstWs.fireClose(1006)

      expect(status).not.toHaveBeenCalled()
      // No third socket should have been created by the stale close handler.
      expect(MockWebSocket.instances).toHaveLength(2)
      client.close()
    })

    it('still reconnects when the current connection closes', () => {
      const client = new VoteClient('ws://x', {
        initialReconnectDelay: 1000,
        maxReconnectDelay: 1000
      })
      client.connect()
      client._doConnect() // supersede
      const secondWs = MockWebSocket.instances[1]

      secondWs.fireClose(1006)
      vi.advanceTimersByTime(500)
      expect(MockWebSocket.instances).toHaveLength(3)
      client.close()
    })
  })

  describe('offline / online awareness', () => {
    it('pauses reconnect while navigator is offline', () => {
      const client = new VoteClient('ws://x', {
        initialReconnectDelay: 1000,
        maxReconnectDelay: 1000
      })
      client.connect()
      // Simulate the OS dropping the network.
      Object.defineProperty(navigator, 'onLine', {
        value: false,
        configurable: true
      })
      window.dispatchEvent(new Event('offline'))

      MockWebSocket.instances[0].fireClose(1006)
      // A reconnect was scheduled by fireClose, but the offline handler
      // should have cancelled it.
      vi.advanceTimersByTime(60_000)
      expect(MockWebSocket.instances).toHaveLength(1)

      Object.defineProperty(navigator, 'onLine', {
        value: true,
        configurable: true
      })
      client.close()
    })

    it('fast-retries on the online event when a reconnect was pending', () => {
      const client = new VoteClient('ws://x', {
        initialReconnectDelay: 10_000,
        maxReconnectDelay: 10_000
      })
      client.connect()
      Object.defineProperty(navigator, 'onLine', {
        value: false,
        configurable: true
      })
      window.dispatchEvent(new Event('offline'))

      MockWebSocket.instances[0].fireClose(1006)
      // Without offline, this would have waited 5000ms (jittered). With
      // offline active, no timer is scheduled.
      vi.advanceTimersByTime(60_000)
      expect(MockWebSocket.instances).toHaveLength(1)

      // Online event should trigger an immediate reconnect.
      Object.defineProperty(navigator, 'onLine', {
        value: true,
        configurable: true
      })
      window.dispatchEvent(new Event('online'))
      expect(MockWebSocket.instances).toHaveLength(2)
      client.close()
    })

    it('does not fast-retry on online if the client was explicitly closed', () => {
      const client = new VoteClient('ws://x', {
        initialReconnectDelay: 10_000,
        maxReconnectDelay: 10_000
      })
      client.connect()
      Object.defineProperty(navigator, 'onLine', {
        value: false,
        configurable: true
      })
      window.dispatchEvent(new Event('offline'))
      MockWebSocket.instances[0].fireClose(1006)

      client.close()
      window.dispatchEvent(new Event('online'))
      expect(MockWebSocket.instances).toHaveLength(1)
    })
  })

  describe('event listener cleanup', () => {
    it('close() removes online/offline listeners (no leak across instances)', () => {
      const addSpy = vi.spyOn(window, 'addEventListener')
      const removeSpy = vi.spyOn(window, 'removeEventListener')

      const client = new VoteClient('ws://x')
      client.connect()
      client.close()

      // R3: connect() re-binds (idempotent unbind + rebind), so the raw
      // call counts include the constructor bind + the connect rebind.
      //What matters for leak prevention is that every add is matched by
      // a remove after close(), so no handler outlives the instance.
      const onlineAdds = addSpy.mock.calls.filter(([e]) => e === 'online' || e === 'offline')
      const onlineRemoves = removeSpy.mock.calls.filter(([e]) => e === 'online' || e === 'offline')
      expect(onlineAdds.length).toBeGreaterThan(0)
      expect(onlineRemoves).toHaveLength(onlineAdds.length)

      addSpy.mockRestore()
      removeSpy.mockRestore()
    })

    it('R3: connect() re-binds online/offline listeners after close() (leave → rejoin)', () => {
      // Regression: close() unbinds the online/offline listeners.
      // connect() on the same instance (stagiaire leaveSession →
      // connectToSession without a page reload) must re-bind them, or a
      // classroom wifi drop burns reconnect attempts against a dead NIC
      // with no fast-retry on `online` — exactly what FH1/FH2 fixed.
      const client = new VoteClient('ws://x', {
        initialReconnectDelay: 10_000,
        maxReconnectDelay: 10_000
      })
      client.connect()
      const firstSocket = MockWebSocket.instances[0]
      firstSocket.fireOpen()
      client.close()

      // After close, online/offline listeners are unbound — verify by
      // checking that dispatching 'online' does NOT trigger a reconnect.
      Object.defineProperty(navigator, 'onLine', { value: false, configurable: true })
      window.dispatchEvent(new Event('offline'))
      Object.defineProperty(navigator, 'onLine', { value: true, configurable: true })
      window.dispatchEvent(new Event('online'))
      expect(MockWebSocket.instances).toHaveLength(1)

      // Now reconnect on the same instance (the leave → rejoin path).
      client.connect()
      const secondSocket = MockWebSocket.instances[1]
      secondSocket.fireOpen()

      // Simulate the wifi drop: offline pauses, online fast-retries.
      Object.defineProperty(navigator, 'onLine', { value: false, configurable: true })
      window.dispatchEvent(new Event('offline'))
      secondSocket.fireClose(1006)
      vi.advanceTimersByTime(60_000)
      expect(MockWebSocket.instances).toHaveLength(2) // no reconnect while offline

      Object.defineProperty(navigator, 'onLine', { value: true, configurable: true })
      window.dispatchEvent(new Event('online'))
      // R3 fix: online event fires → immediate reconnect. Without the
      // rebind in connect(), this would still be 2 (listeners unbound
      // by the prior close()).
      expect(MockWebSocket.instances).toHaveLength(3)

      client.close()
    })

    it('R3: _bindOnlineEvents is idempotent (no duplicate handlers on repeated connect)', () => {
      const addSpy = vi.spyOn(window, 'addEventListener')
      const removeSpy = vi.spyOn(window, 'removeEventListener')

      const client = new VoteClient('ws://x')
      // Constructor binds once. Multiple connect() calls must not stack
      // handlers — each connect() unbinds then rebinds, so the net
      // listener count on window stays at exactly 2 (one online, one
      // offline), not 2×N.
      client.connect()
      client.connect()
      client.connect()

      // Count active 'online' listeners by tallying adds minus removes.
      const onlineAdds = addSpy.mock.calls.filter(([e]) => e === 'online').length
      const onlineRemoves = removeSpy.mock.calls.filter(([e]) => e === 'online').length
      const offlineAdds = addSpy.mock.calls.filter(([e]) => e === 'offline').length
      const offlineRemoves = removeSpy.mock.calls.filter(([e]) => e === 'offline').length

      expect(onlineAdds - onlineRemoves).toBe(1)
      expect(offlineAdds - offlineRemoves).toBe(1)

      client.close()
      addSpy.mockRestore()
      removeSpy.mockRestore()
    })
  })
})
