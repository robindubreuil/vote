// @vitest-environment happy-dom
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'

// Mock the toast + i18n so we can assert the boundary calls showToast with
// the right key, without depending on real DOM toast plumbing.
const showToastSpy = vi.fn()
vi.mock('./ui.js', () => ({
  showToast: (...args) => showToastSpy(...args)
}))
vi.mock('./i18n.js', () => ({
  t: { common: { unexpectedError: 'Une erreur inattendue est survenue' } }
}))

// We isolate each test under its own module graph so the module-level
// cooldown (`lastToastAt`) doesn't bleed across cases. Without this, an
// earlier test that surfaces a toast would suppress the next test's
// expected toast for the cooldown duration.
async function freshBoundary() {
  vi.resetModules()
  const mod = await import('./error-boundary.js')
  return mod
}

describe('installGlobalErrorHandlers (F11)', () => {
  let teardown

  beforeEach(() => {
    vi.useFakeTimers()
    showToastSpy.mockClear()
    // Reset the global install flag so each test starts from a clean
    // boundary state. Without this, the previous test's installed
    // handlers (or the no-op short-circuit) would leak across cases.
    delete window.__voteErrorBoundaryInstalled
    teardown = null
  })

  afterEach(() => {
    if (teardown) teardown()
    vi.useRealTimers()
  })

  it('surfaces a toast on a window "error" event', async () => {
    const { installGlobalErrorHandlers } = await freshBoundary()
    teardown = installGlobalErrorHandlers()
    window.dispatchEvent(
      new ErrorEvent('error', {
        message: 'boom',
        filename: 'app.js',
        lineno: 42,
        error: new Error('boom')
      })
    )
    expect(showToastSpy).toHaveBeenCalledTimes(1)
    expect(showToastSpy).toHaveBeenCalledWith(
      'Une erreur inattendue est survenue',
      expect.objectContaining({ type: 'error' })
    )
  })

  it('surfaces a toast on a window "unhandledrejection" event', async () => {
    const { installGlobalErrorHandlers } = await freshBoundary()
    teardown = installGlobalErrorHandlers()
    // happy-dom does not implement PromiseRejectionEvent; synthesize a
    // plain Event and attach the `reason` payload the handler reads.
    const reason = new Error('rejected')
    const ev = new Event('unhandledrejection')
    Object.defineProperty(ev, 'reason', { value: reason, writable: false })
    window.dispatchEvent(ev)
    expect(showToastSpy).toHaveBeenCalledTimes(1)
  })

  it('ignores opaque cross-origin "Script error." reports', async () => {
    const { installGlobalErrorHandlers } = await freshBoundary()
    teardown = installGlobalErrorHandlers()
    window.dispatchEvent(
      new ErrorEvent('error', {
        message: 'Script error.',
        filename: '',
        lineno: 0
      })
    )
    expect(showToastSpy).not.toHaveBeenCalled()
  })

  it('ignores AbortError rejections (used by reconnect backoff)', async () => {
    const { installGlobalErrorHandlers } = await freshBoundary()
    teardown = installGlobalErrorHandlers()
    const abort = new Error('aborted')
    abort.name = 'AbortError'
    const ev = new Event('unhandledrejection')
    Object.defineProperty(ev, 'reason', { value: abort, writable: false })
    window.dispatchEvent(ev)
    expect(showToastSpy).not.toHaveBeenCalled()
  })

  it('is idempotent — installing twice does not double-bind', async () => {
    const { installGlobalErrorHandlers } = await freshBoundary()
    teardown = installGlobalErrorHandlers()
    const t2 = installGlobalErrorHandlers() // second call: no-op short-circuit
    const t3 = installGlobalErrorHandlers() // third call: same no-op
    // Subsequent installs return the SAME stable no-op (so callers can
    // safely call .teardown() on what they got back without breaking
    // anything), but it's NOT the real teardown the first call returned.
    expect(t2).toBe(t3)
    // One dispatch → exactly one toast, proving the second install did
    // not bind a second `error` listener.
    window.dispatchEvent(new ErrorEvent('error', { message: 'a', filename: 'x', lineno: 1 }))
    expect(showToastSpy).toHaveBeenCalledTimes(1)
  })

  it('rate-limits toasts so a runaway loop does not flood the UI', async () => {
    const { installGlobalErrorHandlers } = await freshBoundary()
    teardown = installGlobalErrorHandlers()
    for (let i = 0; i < 5; i++) {
      window.dispatchEvent(new ErrorEvent('error', { message: `e${i}`, filename: 'x', lineno: 1 }))
    }
    expect(showToastSpy).toHaveBeenCalledTimes(1)

    vi.advanceTimersByTime(5000)
    window.dispatchEvent(new ErrorEvent('error', { message: 'after-cooldown', filename: 'x', lineno: 1 }))
    expect(showToastSpy).toHaveBeenCalledTimes(2)
  })

  it('teardown removes the listeners', async () => {
    const { installGlobalErrorHandlers } = await freshBoundary()
    teardown = installGlobalErrorHandlers()
    teardown()
    teardown = null
    window.dispatchEvent(new ErrorEvent('error', { message: 'after teardown', filename: 'x', lineno: 1 }))
    expect(showToastSpy).not.toHaveBeenCalled()
  })

  it('can be re-installed after teardown', async () => {
    const { installGlobalErrorHandlers } = await freshBoundary()
    const t1 = installGlobalErrorHandlers()
    t1()
    teardown = installGlobalErrorHandlers()
    window.dispatchEvent(new ErrorEvent('error', { message: 're-installed', filename: 'x', lineno: 1 }))
    expect(showToastSpy).toHaveBeenCalledTimes(1)
  })
})

describe('guardDynamicImport (F11)', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    showToastSpy.mockClear()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('returns the module namespace on success', async () => {
    const { guardDynamicImport } = await freshBoundary()
    const mod = { foo: 'bar' }
    const result = await guardDynamicImport(Promise.resolve(mod), 'm')
    expect(result).toBe(mod)
    expect(showToastSpy).not.toHaveBeenCalled()
  })

  it('surfaces a toast and re-throws on failure', async () => {
    const { guardDynamicImport } = await freshBoundary()
    const err = new Error('chunk missing')
    await expect(guardDynamicImport(Promise.reject(err), 'm')).rejects.toBe(err)
    expect(showToastSpy).toHaveBeenCalledTimes(1)
  })

  it('uses the context label in the console.error message', async () => {
    const { guardDynamicImport } = await freshBoundary()
    const consoleSpy = vi.spyOn(console, 'error').mockImplementation(() => {})
    const err = new Error('boom')
    await expect(guardDynamicImport(Promise.reject(err), 'formateur renderers')).rejects.toBe(err)
    expect(consoleSpy).toHaveBeenCalledWith(expect.stringContaining('formateur renderers'), err)
    consoleSpy.mockRestore()
  })
})
