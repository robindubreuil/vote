export function createListenerTracker() {
  const listeners = new Set()

  function track(target, event, handler) {
    const element = typeof target === 'string' ? document.querySelector(target) : target
    if (!element) return
    element.addEventListener(event, handler)
    listeners.add({ element, event, handler })
  }

  function trackAll(selector, event, handler) {
    document.querySelectorAll(selector).forEach(element => {
      element.addEventListener(event, handler)
      listeners.add({ element, event, handler })
    })
  }

  function cleanup() {
    for (const { element, event, handler } of listeners) {
      element.removeEventListener(event, handler)
    }
    listeners.clear()
  }

  return { track, trackAll, cleanup }
}
