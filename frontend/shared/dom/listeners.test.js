// @vitest-environment happy-dom
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { createListenerTracker } from '@shared/dom/listeners.js'

// The tracker underlies the C3 (Escape shortcut survives cleanup) and M1
// (repeated attach calls don't grow unboundedly) fixes in renderers.js.
// Its contract:
//   - track(target, event, handler) adds a listener and remembers it
//   - trackAll(selector, event, handler) does the same for many elements
//   - cleanup() removes every remembered listener and clears the set
//   - resolving a string selector; missing elements are a silent no-op
//   - size reflects the current count (diagnostics)

describe('createListenerTracker', () => {
  beforeEach(() => {
    document.body.innerHTML = '<button id="a"></button><button id="b"></button>'
  })

  it('track() adds the listener and forwards events', () => {
    const t = createListenerTracker()
    const fn = vi.fn()
    t.track('#a', 'click', fn)
    document.getElementById('a').click()
    expect(fn).toHaveBeenCalledTimes(1)
  })

  it('cleanup() removes every tracked listener', () => {
    const t = createListenerTracker()
    const fn = vi.fn()
    t.track('#a', 'click', fn)
    expect(t.size).toBe(1)
    t.cleanup()
    expect(t.size).toBe(0)
    document.getElementById('a').click()
    expect(fn).not.toHaveBeenCalled()
  })

  it('track() accepts a direct Element target as well as a selector string', () => {
    const t = createListenerTracker()
    const fn = vi.fn()
    const el = document.getElementById('b')
    t.track(el, 'click', fn)
    el.click()
    expect(fn).toHaveBeenCalledTimes(1)
  })

  it('track() is a no-op when the selector resolves to nothing', () => {
    const t = createListenerTracker()
    t.track('#does-not-exist', 'click', () => {})
    expect(t.size).toBe(0)
    // Cleanup must not throw on an empty set.
    expect(() => t.cleanup()).not.toThrow()
  })

  it('trackAll() binds to every matching element', () => {
    document.body.innerHTML = '<button class="x"></button><button class="x"></button><button class="y"></button>'
    const t = createListenerTracker()
    const fn = vi.fn()
    t.trackAll('.x', 'click', fn)
    expect(t.size).toBe(2)
    document.querySelectorAll('.x').forEach((b) => b.click())
    expect(fn).toHaveBeenCalledTimes(2)
  })

  it('trackAll() with no matches leaves the tracker empty', () => {
    const t = createListenerTracker()
    t.trackAll('.nope', 'click', () => {})
    expect(t.size).toBe(0)
  })

  it('cleanup() is idempotent — calling twice is safe', () => {
    const t = createListenerTracker()
    t.track('#a', 'click', () => {})
    t.cleanup()
    expect(() => t.cleanup()).not.toThrow()
    expect(t.size).toBe(0)
  })

  it('listeners added after cleanup are tracked separately', () => {
    const t = createListenerTracker()
    const first = vi.fn()
    const second = vi.fn()
    t.track('#a', 'click', first)
    t.cleanup()
    t.track('#a', 'click', second)
    document.getElementById('a').click()
    expect(first).not.toHaveBeenCalled()
    expect(second).toHaveBeenCalledTimes(1)
  })

  it('two trackers are independent — cleanup on one does not affect the other', () => {
    const t1 = createListenerTracker()
    const t2 = createListenerTracker()
    const fn1 = vi.fn()
    const fn2 = vi.fn()
    t1.track('#a', 'click', fn1)
    t2.track('#a', 'click', fn2)
    t1.cleanup()
    document.getElementById('a').click()
    expect(fn1).not.toHaveBeenCalled()
    expect(fn2).toHaveBeenCalledTimes(1)
  })

  it('the same (target, event, handler) tracked twice removes both on cleanup', () => {
    // The underlying Set stores {element,event,handler} object literals;
    // even if two calls look identical, both are recorded, and addEventListener
    // is called twice. Real browsers dedupe identical (target,type,listener)
    // triples, but our tracker counts entries — we want both removable.
    const t = createListenerTracker()
    const fn = vi.fn()
    const el = document.getElementById('a')
    t.track(el, 'click', fn)
    t.track(el, 'click', fn)
    expect(t.size).toBe(2)
    t.cleanup()
    expect(t.size).toBe(0)
  })
})
