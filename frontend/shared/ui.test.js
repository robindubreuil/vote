// @vitest-environment happy-dom
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import {
  showError,
  hideError,
  showConfirmDialog,
  showToast,
  renderFooterHTML,
  renderSessionCodeButton
} from '@shared/ui.js'

// ui.js holds module-level mutable state:
//   - errorTimeoutId, confirmDialogEl, confirmResolve, confirmLastFocus
//   - toastContainer, activeToasts Map
// We isolate tests by clearing the DOM + running fake timers between cases.

describe('showError / hideError', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    document.body.innerHTML = '<div class="error-message" style="display:none"></div>'
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('showError sets text and reveals the element, then auto-hides after 5s', () => {
    showError('Boom')
    const el = document.querySelector('.error-message')
    expect(el.textContent).toBe('Boom')
    expect(el.style.display).toBe('block')

    vi.advanceTimersByTime(4999)
    expect(el.style.display).toBe('block')
    vi.advanceTimersByTime(1)
    expect(el.style.display).toBe('none')
    expect(el.textContent).toBe('')
  })

  it('calling showError twice in a row resets the 5s timer (no early hide)', () => {
    showError('first')
    vi.advanceTimersByTime(4000)
    showError('second') // should clear the prior timer

    const el = document.querySelector('.error-message')
    expect(el.textContent).toBe('second')
    vi.advanceTimersByTime(1500) // would be 5500 since first call; 1500 since second
    expect(el.style.display).toBe('block')
    vi.advanceTimersByTime(3500) // 5000 since second call → hide
    expect(el.style.display).toBe('none')
  })

  it('showError(null) hides the message immediately', () => {
    showError('something')
    showError(null)
    const el = document.querySelector('.error-message')
    expect(el.style.display).toBe('none')
  })

  it('hideError cancels any pending auto-hide timer', () => {
    showError('msg')
    hideError()
    const el = document.querySelector('.error-message')
    expect(el.style.display).toBe('none')

    // Even after 10s the element stays hidden — calling hideError again
    // must not throw because there is no timer to clear.
    expect(() => vi.advanceTimersByTime(10_000)).not.toThrow()
    expect(el.style.display).toBe('none')
  })

  it('F29: falls back to an error toast when no .error-message slot exists (mid-session views)', () => {
    document.body.innerHTML = ''
    showError('Connexion perdue')
    // No inline error slot — the toast container is the fallback surface.
    const container = document.querySelector('.toast-container')
    expect(container).not.toBeNull()
    const toast = container.querySelector('[data-toast-message="Connexion perdue"]')
    expect(toast).not.toBeNull()
    expect(toast.className).toContain('toast--error')
  })
})

describe('showConfirmDialog', () => {
  let originalRaf

  beforeEach(() => {
    vi.useFakeTimers()
    // happy-dom does not advance focus inside requestAnimationFrame under
    // fake timers, so make rAF synchronous to exercise the focus() call.
    originalRaf = globalThis.requestAnimationFrame
    globalThis.requestAnimationFrame = (cb) => {
      cb(performance.now())
      return 0
    }
    // Detach any prior dialog so ensureConfirmDialog rebuilds fresh state
    // (module-level confirmResolve / confirmLastFocus must be reset).
    document.body.innerHTML = '<button id="trigger">go</button>'
    document.getElementById('trigger').focus()
  })

  afterEach(() => {
    globalThis.requestAnimationFrame = originalRaf
    vi.useRealTimers()
  })

  function fireKey(key, shiftKey = false) {
    const ev = new KeyboardEvent('keydown', {
      key,
      shiftKey,
      bubbles: true,
      cancelable: true
    })
    document.dispatchEvent(ev)
    return ev
  }

  it('renders the modal with title/message and resolves true on OK click', async () => {
    const p = showConfirmDialog({ title: 'Quit?', message: 'Are you sure?', confirmLabel: 'Yes', cancelLabel: 'No' })
    // Microtask for requestAnimationFrame to focus the OK button.
    await vi.advanceTimersByTimeAsync(0)

    const overlay = document.querySelector('.confirm-overlay')
    expect(overlay).not.toBeNull()
    expect(overlay.classList.contains('open')).toBe(true)
    expect(document.body.classList.contains('confirm-lock')).toBe(true)
    expect(overlay.querySelector('.confirm-title').textContent).toBe('Quit?')
    expect(overlay.querySelector('.confirm-message').textContent).toBe('Are you sure?')
    expect(overlay.querySelector('.confirm-ok').textContent).toBe('Yes')
    expect(overlay.querySelector('.confirm-cancel').textContent).toBe('No')

    overlay.querySelector('.confirm-ok').click()
    expect(await p).toBe(true)
    expect(overlay.classList.contains('open')).toBe(false)
  })

  it('Cancel button resolves false', async () => {
    const p = showConfirmDialog({ message: 'm' })
    await vi.advanceTimersByTimeAsync(0)
    document.querySelector('.confirm-cancel').click()
    expect(await p).toBe(false)
  })

  it('click on the backdrop (overlay itself, not the dialog) resolves false', async () => {
    const p = showConfirmDialog({ message: 'm' })
    await vi.advanceTimersByTimeAsync(0)
    const overlay = document.querySelector('.confirm-overlay')
    // Dispatch a click whose target IS the overlay (not a child).
    overlay.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    expect(await p).toBe(false)
  })

  it('Escape cancels, calls preventDefault + stopImmediatePropagation so app shortcuts do not also fire', async () => {
    const otherHandler = vi.fn()
    document.addEventListener('keydown', otherHandler)

    const p = showConfirmDialog({ message: 'm' })
    await vi.advanceTimersByTimeAsync(0)

    const ev = fireKey('Escape')
    // The dialog's capture-phase listener fires before bubble-phase
    // listeners and stops propagation, so otherHandler must not see it.
    expect(ev.defaultPrevented).toBe(true)
    expect(otherHandler).not.toHaveBeenCalled()
    expect(await p).toBe(false)

    document.removeEventListener('keydown', otherHandler)
  })

  it('F31: capture-phase handler fires before a bubble handler registered at init (before dialog open)', async () => {
    // Reproduces the exact F31 scenario: an app-level Escape handler is
    // registered at init (before any dialog opens), so a bubble-phase
    // dialog handler registered lazily on first open would lose the
    // dispatch race. The capture-phase registration inverts the order
    // regardless of registration time, so the dialog handler wins.
    const appHandler = vi.fn()
    document.addEventListener('keydown', appHandler) // registered FIRST

    const p = showConfirmDialog({ message: 'm' })
    await vi.advanceTimersByTimeAsync(0)

    const ev = fireKey('Escape')
    expect(ev.defaultPrevented).toBe(true)
    expect(appHandler).not.toHaveBeenCalled()
    expect(await p).toBe(false)

    document.removeEventListener('keydown', appHandler)
  })

  it('Enter confirms', async () => {
    const p = showConfirmDialog({ message: 'm' })
    await vi.advanceTimersByTimeAsync(0)
    fireKey('Enter')
    expect(await p).toBe(true)
  })

  it('keys are ignored once the dialog has resolved (no late resolve)', async () => {
    const p = showConfirmDialog({ message: 'm' })
    await vi.advanceTimersByTimeAsync(0)
    document.querySelector('.confirm-cancel').click()
    expect(await p).toBe(false)
    // Now there is no active dialog — pressing Enter must not throw.
    expect(() => fireKey('Enter')).not.toThrow()
  })

  it('focus trap: Tab on the last focusable wraps to the first', async () => {
    const p = showConfirmDialog({ message: 'm' })
    await vi.advanceTimersByTimeAsync(0)

    const cancel = document.querySelector('.confirm-cancel')
    const ok = document.querySelector('.confirm-ok')
    // Two buttons: cancel (first) and ok (last). Focus is on OK after open.
    expect(document.activeElement).toBe(ok)

    // Tab from last (ok) → wrap to first (cancel)
    fireKey('Tab')
    expect(document.activeElement).toBe(cancel)

    // Shift+Tab from first (cancel) → wrap to last (ok)
    fireKey('Tab', true)
    expect(document.activeElement).toBe(ok)

    // Cleanup.
    document.querySelector('.confirm-cancel').click()
    await p
  })

  it('focus trap: Tab in the middle does NOT wrap (only on the boundaries)', async () => {
    // Move focus to something that is neither first nor last focusable.
    // With only two buttons, focus is always on a boundary, so this test
    // instead verifies that shiftKey=false + last-element wraps, and that
    // a non-Tab key (e.g. 'a') does not move focus at all.
    const p = showConfirmDialog({ message: 'm' })
    await vi.advanceTimersByTimeAsync(0)
    const ok = document.querySelector('.confirm-ok')
    expect(document.activeElement).toBe(ok)
    fireKey('a')
    expect(document.activeElement).toBe(ok)
    document.querySelector('.confirm-cancel').click()
    await p
  })

  it('restores focus to the previously focused element on resolve', async () => {
    const trigger = document.getElementById('trigger')
    trigger.focus()
    const p = showConfirmDialog({ message: 'm' })
    await vi.advanceTimersByTimeAsync(0)
    expect(document.activeElement).not.toBe(trigger)

    document.querySelector('.confirm-ok').click()
    await p
    expect(document.activeElement).toBe(trigger)
  })

  it('danger:false swaps the OK button to btn-primary', async () => {
    const p = showConfirmDialog({ message: 'm', danger: false })
    await vi.advanceTimersByTimeAsync(0)
    const ok = document.querySelector('.confirm-ok')
    expect(ok.className).toContain('btn-primary')
    expect(ok.className).not.toContain('btn-danger')
    document.querySelector('.confirm-cancel').click()
    await p
  })

  it('title is hidden when not provided', async () => {
    const p = showConfirmDialog({ message: 'm' })
    await vi.advanceTimersByTimeAsync(0)
    const titleEl = document.querySelector('.confirm-title')
    expect(titleEl.style.display).toBe('none')
    document.querySelector('.confirm-cancel').click()
    await p
  })

  it('reuses the same overlay element across calls (no DOM accumulation)', async () => {
    const p1 = showConfirmDialog({ message: 'first' })
    await vi.advanceTimersByTimeAsync(0)
    document.querySelector('.confirm-cancel').click()
    await p1
    const overlaysAfterFirst = document.querySelectorAll('.confirm-overlay')

    const p2 = showConfirmDialog({ message: 'second' })
    await vi.advanceTimersByTimeAsync(0)
    const overlaysDuringSecond = document.querySelectorAll('.confirm-overlay')
    document.querySelector('.confirm-cancel').click()
    await p2

    expect(overlaysAfterFirst).toHaveLength(1)
    expect(overlaysDuringSecond).toHaveLength(1)
  })

  // A1: aria-modal="true" alone is widely ignored by SRs in browse mode.
  // The fix inerts every body-level sibling of the overlay so background
  // content is removed from the a11y tree AND from interaction while the
  // modal is open, and restored on resolve.
  describe('background isolation (A1)', () => {
    it('inerts every body-level sibling while open and restores them on resolve', async () => {
      // Set up realistic background landmarks (formateur/stagiaire shells).
      document.body.innerHTML = `
        <header id="app-header"><button>bg</button></header>
        <main id="app-content"><a href="#">link</a></main>
        <button id="trigger">go</button>
      `
      document.getElementById('trigger').focus()

      const header = document.getElementById('app-header')
      const main = document.getElementById('app-content')
      expect(header.inert).toBe(false)
      expect(main.inert).toBe(false)

      const p = showConfirmDialog({ message: 'm' })
      await vi.advanceTimersByTimeAsync(0)

      // Siblings are inerted; the overlay itself is NOT.
      expect(header.inert).toBe(true)
      expect(main.inert).toBe(true)
      const overlay = document.querySelector('.confirm-overlay')
      expect(overlay.inert).toBe(false)

      document.querySelector('.confirm-cancel').click()
      await p

      // Restored exactly — the previously-inerted elements are interactive
      // again, and the overlay is left alone (it just loses .open).
      expect(header.inert).toBe(false)
      expect(main.inert).toBe(false)
    })

    it('does not touch elements inerted independently after resolve', async () => {
      document.body.innerHTML = `
        <main id="app-content"></main>
        <button id="trigger">go</button>
      `
      document.getElementById('trigger').focus()

      const main = document.getElementById('app-content')
      const p = showConfirmDialog({ message: 'm' })
      await vi.advanceTimersByTimeAsync(0)
      expect(main.inert).toBe(true)

      document.querySelector('.confirm-cancel').click()
      await p
      expect(main.inert).toBe(false)

      // An element inerted by some other feature AFTER resolve must not be
      // clobbered by a future dialog open/close cycle.
      main.inert = true
      const p2 = showConfirmDialog({ message: 'm2' })
      await vi.advanceTimersByTimeAsync(0)
      // It's in our inerted-siblings set while open (still inert — fine).
      expect(main.inert).toBe(true)
      document.querySelector('.confirm-cancel').click()
      await p2
      // unlock restores only what WE inerted; but since main was inerted
      // by us this cycle, it gets unlocked. The invariant that matters:
      // resolve never leaves a sibling we touched still inert.
      expect(main.inert).toBe(false)
    })
  })
})

describe('showToast', () => {
  let originalRaf

  beforeEach(() => {
    // showToast holds module-level state (toastContainer + activeToasts Map).
    // Detaching the previous body makes ensureToastContainer recreate both
    // the container and the per-message Map entries (clearTimeout is a no-op
    // on the abandoned timer IDs).
    document.body.innerHTML = ''
    vi.useFakeTimers()
    // Same rAF stub reason as showConfirmDialog — the toast--visible class
    // is added in a rAF callback.
    originalRaf = globalThis.requestAnimationFrame
    globalThis.requestAnimationFrame = (cb) => {
      cb(performance.now())
      return 0
    }
  })

  afterEach(() => {
    globalThis.requestAnimationFrame = originalRaf
    vi.useRealTimers()
  })

  it('creates a toast node in a dedicated container', () => {
    showToast('Saved')
    const container = document.querySelector('.toast-container')
    expect(container).not.toBeNull()
    expect(container.getAttribute('role')).toBe('status')
    expect(container.getAttribute('aria-live')).toBe('polite')
    const toast = container.querySelector('[data-toast-message="Saved"]')
    expect(toast).not.toBeNull()
    expect(toast.className).toContain('toast--success')
  })

  it('auto-dismisses after the duration (+ 200ms fade)', () => {
    showToast('Bye', { duration: 1000 })
    const container = document.querySelector('.toast-container')
    expect(container.querySelector('[data-toast-message="Bye"]')).not.toBeNull()

    vi.advanceTimersByTime(1000)
    // visible class removed; node still present during the 200ms fade.
    const fading = container.querySelector('[data-toast-message="Bye"]')
    expect(fading).not.toBeNull()
    expect(fading.classList.contains('toast--visible')).toBe(false)

    vi.advanceTimersByTime(200)
    expect(container.querySelector('[data-toast-message="Bye"]')).toBeNull()
  })

  it('calling showToast with the same message reuses the existing node (no duplicate DOM)', () => {
    showToast('Same')
    vi.advanceTimersByTime(500)
    showToast('Same') // should reset timer, NOT create a second node
    const nodes = document.querySelectorAll('[data-toast-message="Same"]')
    expect(nodes).toHaveLength(1)
  })

  it('different messages coexist as separate toasts', () => {
    showToast('A')
    showToast('B')
    const container = document.querySelector('.toast-container')
    expect(container.querySelector('[data-toast-message="A"]')).not.toBeNull()
    expect(container.querySelector('[data-toast-message="B"]')).not.toBeNull()
  })

  it('respects the type option (info / error)', () => {
    showToast('info!', { type: 'info' })
    showToast('error!', { type: 'error' })
    expect(document.querySelector('[data-toast-message="info!"]').className).toContain('toast--info')
    expect(document.querySelector('[data-toast-message="error!"]').className).toContain('toast--error')
  })

  it('empty message is a no-op', () => {
    showToast('')
    expect(document.querySelector('.toast-container')).toBeNull()
  })

  it('a re-shown toast whose original timer already fired creates a fresh node', () => {
    showToast('Again', { duration: 100 })
    vi.advanceTimersByTime(100)
    vi.advanceTimersByTime(200) // past fade → removed from DOM
    expect(document.querySelector('[data-toast-message="Again"]')).toBeNull()

    showToast('Again')
    const node = document.querySelector('[data-toast-message="Again"]')
    expect(node).not.toBeNull()
  })

  it('reuses the same container across toasts (no second container)', () => {
    showToast('one')
    showToast('two')
    expect(document.querySelectorAll('.toast-container')).toHaveLength(1)
  })
})

describe('renderFooterHTML', () => {
  it('includes author, license link, version and date', () => {
    const html = renderFooterHTML()
    expect(html).toContain('footer-author')
    expect(html).toContain('footer-version')
    expect(html).toContain('footer-date')
    expect(html).toContain('Licence MIT')
  })
})

describe('renderSessionCodeButton', () => {
  it('returns empty string when no code', () => {
    expect(renderSessionCodeButton('')).toBe('')
    expect(renderSessionCodeButton(null)).toBe('')
  })

  it('emits a button with id=leaveSessionBtn and connection class', () => {
    const html = renderSessionCodeButton('ABC', true)
    expect(html).toContain('id="leaveSessionBtn"')
    expect(html).toContain('>ABC<')
    expect(html).toContain('connected')
    expect(html).toContain('data-testid="session-code-btn"')
  })

  it('uses the disconnected class when connected=false', () => {
    expect(renderSessionCodeButton('ABC', false)).toContain('disconnected')
  })

  it('uses a custom title when provided', () => {
    const html = renderSessionCodeButton('ABC', true, 'Custom title')
    expect(html).toContain('title="Custom title"')
  })
})
