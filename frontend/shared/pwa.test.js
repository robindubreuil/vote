// @vitest-environment happy-dom
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'

// pwa.js is prod-only (Vite dead-code-eliminates the SW fetch in dev).
// We stub import.meta.env.PROD plus a fake ServiceWorkerContainer + Registration.

class FakeSW {
  constructor() {
    this.state = 'installed'
    this.listeners = {}
    this.postMessage = vi.fn()
  }
  addEventListener(ev, fn) {
    ;(this.listeners[ev] ||= []).push(fn)
  }
  _fire(ev) {
    ;(this.listeners[ev] || []).forEach((fn) => fn({ target: this }))
  }
}

class FakeReg {
  constructor() {
    this.waiting = null
    this.installing = null
    this.active = null
    this.updateListeners = []
    this.updateCalls = 0
  }
  addEventListener(ev, fn) {
    if (ev === 'updatefound') this.updateListeners.push(fn)
  }
  update() {
    this.updateCalls++
    return Promise.resolve()
  }
  _fireUpdateFound() {
    this.updateListeners.forEach((fn) => fn())
  }
}

function installServiceWorkerMock({ waiting = null } = {}) {
  const listeners = {}
  const container = {
    controller: new FakeSW(),
    registrations: [],
    lastRegisterArgs: null,
    addEventListener(ev, fn) {
      ;(listeners[ev] ||= []).push(fn)
    },
    register(path, opts) {
      this.lastRegisterArgs = { path, opts }
      const reg = new FakeReg()
      reg.waiting = waiting
      this.registrations.push(reg)
      this._lastReg = reg
      return Promise.resolve(reg)
    },
    _fireControllerChange() {
      ;(listeners.controllerchange || []).forEach((fn) => fn())
    }
  }
  Object.defineProperty(navigator, 'serviceWorker', {
    value: container,
    configurable: true
  })
  return { container }
}

describe('initPWA', () => {
  let originalSW
  let originalRaf
  let reloadSpy

  beforeEach(() => {
    vi.resetModules()
    document.body.innerHTML = ''
    vi.stubEnv('PROD', true)
    vi.stubEnv('DEV', false)

    originalSW = navigator.serviceWorker
    originalRaf = globalThis.requestAnimationFrame
    globalThis.requestAnimationFrame = (cb) => {
      cb(performance.now())
      return 0
    }
    reloadSpy = vi.spyOn(window.location, 'reload').mockImplementation(() => {})
  })

  afterEach(() => {
    vi.unstubAllEnvs()
    vi.useRealTimers()
    if (originalSW === undefined) delete navigator.serviceWorker
    else
      Object.defineProperty(navigator, 'serviceWorker', {
        value: originalSW,
        configurable: true
      })
    globalThis.requestAnimationFrame = originalRaf
    reloadSpy.mockRestore()
  })

  async function importPWA() {
    return await import('@shared/pwa.js')
  }

  it('is a no-op when serviceWorker is unsupported', async () => {
    delete navigator.serviceWorker
    const { initPWA } = await importPWA()
    expect(() => initPWA()).not.toThrow()
  })

  it('is a no-op in dev (import.meta.env.PROD = false)', async () => {
    vi.stubEnv('PROD', false)
    vi.stubEnv('DEV', true)
    installServiceWorkerMock()
    const { initPWA } = await importPWA()
    initPWA()
    expect(navigator.serviceWorker.registrations).toHaveLength(0)
  })

  it('registers /sw.js with scope / on load when prod + SW available', async () => {
    const { container } = installServiceWorkerMock()
    const { initPWA } = await importPWA()
    initPWA()
    // Microtask flush so the .register().then chain runs.
    await new Promise((r) => setTimeout(r, 0))
    expect(container.lastRegisterArgs).toEqual({ path: '/sw.js', opts: { scope: '/' } })
    expect(container.registrations).toHaveLength(1)
  })

  it('shows the update toast when the initial registration has a waiting worker', async () => {
    installServiceWorkerMock({ waiting: new FakeSW() })
    const { initPWA } = await importPWA()
    initPWA()
    await new Promise((r) => setTimeout(r, 0))
    expect(document.getElementById('pwa-update-toast')).not.toBeNull()
  })

  it('shows the update toast on updatefound → installed state', async () => {
    installServiceWorkerMock()
    const { initPWA } = await importPWA()
    initPWA()
    await new Promise((r) => setTimeout(r, 0))
    const reg = navigator.serviceWorker._lastReg

    const next = new FakeSW()
    next.state = 'installing'
    reg.installing = next
    reg._fireUpdateFound()
    next._fire('statechange')
    expect(document.getElementById('pwa-update-toast')).toBeNull()

    next.state = 'installed'
    next._fire('statechange')
    expect(document.getElementById('pwa-update-toast')).not.toBeNull()
  })

  it('does NOT show the update toast when there is no controlling SW (first install)', async () => {
    installServiceWorkerMock()
    navigator.serviceWorker.controller = null
    const { initPWA } = await importPWA()
    initPWA()
    await new Promise((r) => setTimeout(r, 0))
    const reg = navigator.serviceWorker._lastReg
    const next = new FakeSW()
    next.state = 'installed'
    reg.installing = next
    reg._fireUpdateFound()
    next._fire('statechange')
    expect(document.getElementById('pwa-update-toast')).toBeNull()
  })

  it('clicking Recharger posts SKIP_WAITING to the waiting worker', async () => {
    const waitingWorker = new FakeSW()
    installServiceWorkerMock({ waiting: waitingWorker })
    const { initPWA } = await importPWA()
    initPWA()
    await new Promise((r) => setTimeout(r, 0))
    expect(document.getElementById('pwa-update-toast')).not.toBeNull()
    document.querySelector('.pwa-toast-action').click()
    expect(waitingWorker.postMessage).toHaveBeenCalledWith('SKIP_WAITING')
  })

  it('clicking Recharger hard-reloads when no waiting worker exists', async () => {
    installServiceWorkerMock()
    const { initPWA } = await importPWA()
    initPWA()
    await new Promise((r) => setTimeout(r, 0))
    const reg = navigator.serviceWorker._lastReg
    // Force the toast into existence via updatefound flow, then drop the
    // waiting reference so the action takes the reload branch.
    const next = new FakeSW()
    next.state = 'installed'
    reg.installing = next
    reg._fireUpdateFound()
    next._fire('statechange')
    reg.waiting = null

    document.querySelector('.pwa-toast-action').click()
    expect(reloadSpy).toHaveBeenCalled()
  })

  it('clicking the close button removes the toast', async () => {
    installServiceWorkerMock({ waiting: new FakeSW() })
    const { initPWA } = await importPWA()
    initPWA()
    await new Promise((r) => setTimeout(r, 0))
    expect(document.getElementById('pwa-update-toast')).not.toBeNull()
    document.querySelector('.pwa-toast-close').click()
    expect(document.getElementById('pwa-update-toast')).toBeNull()
  })

  it('does not duplicate the update toast if it is already visible', async () => {
    installServiceWorkerMock({ waiting: new FakeSW() })
    const { initPWA } = await importPWA()
    initPWA()
    await new Promise((r) => setTimeout(r, 0))
    expect(document.querySelectorAll('#pwa-update-toast')).toHaveLength(1)

    // Re-trigger via the updatefound path: should NOT add a second toast.
    const reg = navigator.serviceWorker._lastReg
    const next = new FakeSW()
    next.state = 'installed'
    reg.installing = next
    reg._fireUpdateFound()
    next._fire('statechange')
    expect(document.querySelectorAll('#pwa-update-toast')).toHaveLength(1)
  })

  it('controllerchange triggers exactly one reload (loop guard)', async () => {
    installServiceWorkerMock()
    const { initPWA } = await importPWA()
    initPWA()
    await new Promise((r) => setTimeout(r, 0))

    navigator.serviceWorker._fireControllerChange()
    expect(reloadSpy).toHaveBeenCalledTimes(1)

    // A second controllerchange (e.g. from a race in the browser firing it
    // twice) must NOT trigger another reload — the page is already reloading.
    navigator.serviceWorker._fireControllerChange()
    expect(reloadSpy).toHaveBeenCalledTimes(1)
  })

  it('offline event shows the offline toast, online event hides it', async () => {
    installServiceWorkerMock()
    const { initPWA } = await importPWA()
    initPWA()

    window.dispatchEvent(new Event('offline'))
    expect(document.getElementById('pwa-offline-toast')).not.toBeNull()

    window.dispatchEvent(new Event('online'))
    expect(document.getElementById('pwa-offline-toast')).toBeNull()
  })

  it('offline toast is idempotent — repeated offline events add only one', async () => {
    installServiceWorkerMock()
    const { initPWA } = await importPWA()
    initPWA()

    window.dispatchEvent(new Event('offline'))
    window.dispatchEvent(new Event('offline'))
    window.dispatchEvent(new Event('offline'))
    expect(document.querySelectorAll('#pwa-offline-toast')).toHaveLength(1)
  })

  it('polls reg.update() on a 60-minute interval', async () => {
    vi.useFakeTimers()
    installServiceWorkerMock()
    const { initPWA } = await importPWA()
    initPWA()
    await vi.advanceTimersByTimeAsync(0)
    const reg = navigator.serviceWorker._lastReg
    expect(reg.updateCalls).toBe(0)
    await vi.advanceTimersByTimeAsync(60 * 60 * 1000)
    expect(reg.updateCalls).toBe(1)
    await vi.advanceTimersByTimeAsync(60 * 60 * 1000)
    expect(reg.updateCalls).toBe(2)
  })

  it('update polling swallows reg.update() rejections', async () => {
    vi.useFakeTimers()
    installServiceWorkerMock()
    const { initPWA } = await importPWA()
    initPWA()
    await vi.advanceTimersByTimeAsync(0)
    const reg = navigator.serviceWorker._lastReg
    reg.update = () => Promise.reject(new Error('network'))
    await vi.advanceTimersByTimeAsync(60 * 60 * 1000)
    // Allow any unhandled promise rejection to settle without throwing.
    await vi.advanceTimersByTimeAsync(0)
  })
})
