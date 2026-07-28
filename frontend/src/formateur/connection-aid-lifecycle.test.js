// @vitest-environment happy-dom
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'

// Mock the heavy dependencies so the test exercises only the lifecycle
// logic of initConnectionAid (interval clear, keydown removal, subscriber
// close) without pulling in `qrcode` (~116 KB) or a real BroadcastChannel.
vi.mock('qrcode', () => ({
  default: { toString: vi.fn(async () => '<svg></svg>') }
}))

// Each test gets a fresh subscriber via `lastSubscriber`. Sharing a
// single instance across tests meant `close` accumulated invocations
// from earlier tests even after `vi.clearAllMocks()` (which clears call
// history but not stale `beforeunload` listeners left attached to window).
let lastSubscriber = null
vi.mock('@shared/session-sync.js', () => ({
  createSessionSubscriber: () => {
    lastSubscriber = { start: vi.fn(), close: vi.fn() }
    return lastSubscriber
  }
}))

vi.mock('./connection-aid-url.js', () => ({
  buildJoinURL: (code) => `https://example.test/#${code}`
}))

const { initConnectionAid } = await import('./connection-aid.js')

describe('connection-aid — F12 cleanup', () => {
  let clearIntervalSpy

  beforeEach(() => {
    document.body.innerHTML = '<div id="app"></div>'
    lastSubscriber = null
    clearIntervalSpy = vi.spyOn(globalThis, 'clearInterval')
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('registers a keydown listener that can be removed (named handler)', () => {
    const addSpy = vi.spyOn(document, 'addEventListener')
    initConnectionAid('ABC')
    const keydownCall = addSpy.mock.calls.find(([evt]) => evt === 'keydown')
    expect(keydownCall).toBeTruthy()
    expect(typeof keydownCall[1]).toBe('function') // named, not anonymous
  })

  it('starts a stale-check interval whose id is tracked for cleanup', () => {
    const setIntervalSpy = vi.spyOn(globalThis, 'setInterval')
    initConnectionAid('ABC')
    expect(setIntervalSpy).toHaveBeenCalled()
    // The captured id will be passed to clearInterval on teardown. We
    // don't assert its type — happy-dom returns a Timer handle while
    // browsers return a number; both are opaque tokens passed back to
    // clearInterval.
    expect(setIntervalSpy.mock.results[0].value).toBeDefined()
  })

  it('teardown on beforeunload removes the keydown listener, clears the interval, closes the subscriber', () => {
    const documentRemoveSpy = vi.spyOn(document, 'removeEventListener')
    initConnectionAid('ABC')
    expect(lastSubscriber).not.toBeNull()
    // Dispatching beforeunload triggers the { once: true } teardown.
    window.dispatchEvent(new Event('beforeunload'))

    // The keydown listener was registered on document (not window), so
    // document.removeEventListener must have been invoked with 'keydown'.
    const keydownRemoves = documentRemoveSpy.mock.calls.filter(([e]) => e === 'keydown')
    expect(keydownRemoves.length).toBeGreaterThanOrEqual(1)
    expect(clearIntervalSpy).toHaveBeenCalled()
    expect(lastSubscriber.close).toHaveBeenCalledTimes(1)
  })

  it('teardown is idempotent (beforeunload + pagehide do not double-close)', () => {
    initConnectionAid('ABC')
    window.dispatchEvent(new Event('beforeunload'))
    window.dispatchEvent(new Event('pagehide'))
    expect(lastSubscriber.close).toHaveBeenCalledTimes(1)
  })

  it('pagehide alone also tears down (covers mobile Safari)', () => {
    initConnectionAid('ABC')
    window.dispatchEvent(new Event('pagehide'))
    expect(lastSubscriber.close).toHaveBeenCalledTimes(1)
    expect(clearIntervalSpy).toHaveBeenCalled()
  })
})
