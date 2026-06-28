import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { createSessionPublisher, createSessionSubscriber } from '@shared/session-sync.js'

// Minimal sessionStorage stub for the node test environment.
function installSessionStorage() {
  const store = new Map()
  const stub = {
    getItem: (key) => (store.has(key) ? store.get(key) : null),
    setItem: (key, value) => {
      store.set(key, String(value))
    },
    removeItem: (key) => store.delete(key),
    clear: () => store.clear()
  }
  vi.stubGlobal('sessionStorage', stub)
  return store
}

describe('session-sync — publisher/subscriber over BroadcastChannel', () => {
  let originalBC
  let channelsByName
  const created = []

  beforeEach(() => {
    channelsByName = new Map()
    originalBC = globalThis.BroadcastChannel
    created.length = 0
    installSessionStorage()
    // In-memory BroadcastChannel polyfill so publisher and subscriber within
    // the same test process can actually exchange messages.
    globalThis.BroadcastChannel = class FakeBC {
      constructor(name) {
        this.name = name
        this.closed = false
        this.onmessage = null
        const list = channelsByName.get(name) || []
        list.push(this)
        channelsByName.set(name, list)
      }
      postMessage(data) {
        if (this.closed) return
        const list = channelsByName.get(this.name) || []
        list.forEach((c) => {
          if (c !== this && !c.closed && typeof c.onmessage === 'function') {
            c.onmessage({ data })
          }
        })
      }
      close() {
        this.closed = true
        const list = channelsByName.get(this.name) || []
        const next = list.filter((c) => c !== this)
        if (next.length) channelsByName.set(this.name, next)
        else channelsByName.delete(this.name)
      }
    }
  })

  afterEach(() => {
    // Tear down every publisher/subscriber created during the test so the
    // publisher heartbeat interval does not leak across tests.
    created.forEach((handle) => handle.close())
    created.length = 0
    globalThis.BroadcastChannel = originalBC
    vi.unstubAllGlobals()
    vi.useRealTimers()
  })

  const track = (handle) => {
    created.push(handle)
    return handle
  }

  it('publishes state to a late subscriber via hello/state handshake', () => {
    const publisher = track(createSessionPublisher('ABC'))
    publisher.publish({ count: 5, voteState: 'idle', connected: true })

    const states = []
    const subscriber = track(createSessionSubscriber('ABC', (s) => states.push(s)))
    subscriber.start()

    expect(states).toHaveLength(1)
    expect(states[0]).toEqual({ count: 5, voteState: 'idle', connected: true, competitive: false, leaderboard: null })
  })

  it('delivers subsequent state pushes live', () => {
    const states = []
    const subscriber = track(createSessionSubscriber('ABC', (s) => states.push(s)))
    subscriber.start()

    const publisher = track(createSessionPublisher('ABC'))
    publisher.publish({ count: 1, voteState: 'active', connected: true })
    publisher.publish({ count: 2, voteState: 'active', connected: true })

    expect(states).toEqual([
      { count: 1, voteState: 'active', connected: true, competitive: false, leaderboard: null },
      { count: 2, voteState: 'active', connected: true, competitive: false, leaderboard: null }
    ])
  })

  it('falls back to defaults when BroadcastChannel is unavailable', () => {
    globalThis.BroadcastChannel = undefined

    const publisher = track(createSessionPublisher('ABC'))
    expect(() => publisher.publish({ count: 9 })).not.toThrow()
    expect(() => publisher.close()).not.toThrow()

    const subscriber = track(createSessionSubscriber('ABC', () => {}))
    expect(() => subscriber.start()).not.toThrow()
    expect(() => subscriber.close()).not.toThrow()
  })

  it('isolates channels by session code', () => {
    const aStates = []
    const bStates = []
    const subA = track(createSessionSubscriber('1111', (s) => aStates.push(s)))
    const subB = track(createSessionSubscriber('2222', (s) => bStates.push(s)))
    subA.start()
    subB.start()

    const pubA = track(createSessionPublisher('1111'))
    pubA.publish({ count: 7, voteState: 'idle', connected: true })

    expect(aStates).toHaveLength(1)
    expect(bStates).toHaveLength(0)
  })

  it('snapshots the latest state in sessionStorage so a freshly opened aid view gets it via hello', () => {
    const pub = track(createSessionPublisher('KQR'))
    pub.publish({ count: 3, voteState: 'closed', connected: false })

    const raw = sessionStorage.getItem('vote_session_state_snapshot')
    expect(raw).not.toBeNull()
    expect(JSON.parse(raw)).toEqual({ count: 3, voteState: 'closed', connected: false, competitive: false, leaderboard: null })
  })

  it('emits a periodic heartbeat so subscribers can detect a missing publisher', async () => {
    vi.useFakeTimers()

    const states = []
    const subscriber = track(createSessionSubscriber('ABC', (s) => states.push(s)))
    subscriber.start()

    const publisher = track(createSessionPublisher('ABC'))
    publisher.publish({ count: 4, voteState: 'idle', connected: true })
    states.length = 0 // ignore the initial publish

    await vi.advanceTimersByTimeAsync(4001)
    await vi.advanceTimersByTimeAsync(4001)

    expect(states.length).toBeGreaterThanOrEqual(2)
    expect(states[0]).toEqual({ count: 4, voteState: 'idle', connected: true, competitive: false, leaderboard: null })
  })
})

describe('session-sync — graceful when sessionStorage throws', () => {
  beforeEach(() => {
    vi.stubGlobal('sessionStorage', {
      getItem: vi.fn(() => {
        throw new Error('denied')
      }),
      setItem: vi.fn(() => {
        throw new Error('denied')
      }),
      removeItem: vi.fn(),
      clear: vi.fn()
    })
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('does not throw when sessionStorage is unavailable', () => {
    const pub = createSessionPublisher('DEF')
    expect(() => pub.publish({ count: 1, voteState: 'idle', connected: true })).not.toThrow()
  })
})
