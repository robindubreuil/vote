/**
 * Cross-tab sync between the formateur main app and the "Aide à la connexion"
 * view (which lives in a separate tab so the formateur can move it to the
 * videoprojector screen).
 *
 * The backend only allows one trainer per session (a second trainer_join kicks
 * the first — see backend/internal/hub/hub.go), so the aid view cannot connect
 * as a trainer. Instead, the main formateur tab publishes session state over a
 * BroadcastChannel named after the session code; the aid tab subscribes.
 *
 * Protocol:
 *   - main -> aid:  { type: 'state', count, voteState, connected }
 *     Emitted every time the trainer receives a connected_count / vote_*
 *     message from the server, plus once immediately on 'hello' (so the aid
 *     tab does not have to wait for the next server event when opened late).
 *   - aid  -> main: { type: 'hello' }
 *     Sent once when the aid view starts, so the main tab can reply with the
 *     current state without waiting for the next server push.
 *
 * Graceful degradation: BroadcastChannel is unavailable in very old browsers
 * (or when the page is opened from a file:// or opaque context). Both helper
 * factories return no-op publishers / inert subscribers in that case, so the
 * aid view simply falls back to "—" instead of crashing.
 */

const channelName = (sessionCode) => `vote-formateur-${sessionCode}`

const HEARTBEAT_INTERVAL_MS = 4000

/**
 * Build a publisher used by the main formateur tab.
 *
 * The publisher emits a state message every time `publish()` is called (which
 * the formateur WS handler does on each server event) AND on a periodic
 * heartbeat. The heartbeat lets the aid view detect when the main tab has
 * been closed or navigated away — without it, a closed main tab would leave
 * the aid view showing the last known count forever, with no signal that
 * updates have stopped.
 *
 * @param {string} sessionCode
 * @param {() => { count: number, voteState: string, connected: boolean }} [readState]
 *        optional callback returning the current state, used to drive the
 *        heartbeat. When omitted, the heartbeat replays the last published
 *        snapshot from sessionStorage.
 * @returns {{ publish(state: object): void, close(): void }}
 */
export function createSessionPublisher(sessionCode, readState) {
  if (typeof BroadcastChannel === 'undefined') {
    return { publish() {}, close() {} }
  }

  let channel
  try {
    channel = new BroadcastChannel(channelName(sessionCode))
  } catch {
    return { publish() {}, close() {} }
  }

  let lastSnapshot = null

  const persist = (snapshot) => {
    lastSnapshot = snapshot
    try {
      sessionStorage.setItem('vote_session_state_snapshot', JSON.stringify(snapshot))
    } catch {
      /* ignore quota / disabled storage */
    }
  }

  const readSnapshot = () => {
    if (lastSnapshot) return lastSnapshot
    try {
      const raw = sessionStorage.getItem('vote_session_state_snapshot')
      return raw ? JSON.parse(raw) : null
    } catch {
      return null
    }
  }

  channel.onmessage = (event) => {
    if (event.data && event.data.type === 'hello') {
      const snapshot = (typeof readState === 'function' ? readState() : null) || readSnapshot()
      if (snapshot) {
        channel.postMessage({ type: 'state', ...snapshot })
      }
    }
  }

  // Heartbeat: re-emit the latest state at a fixed cadence so subscribers can
  // detect when the main tab has gone away (no message for N seconds).
  const heartbeatId = setInterval(() => {
    const snapshot = (typeof readState === 'function' ? readState() : null) || readSnapshot()
    if (snapshot) {
      channel.postMessage({ type: 'state', ...snapshot })
    }
  }, HEARTBEAT_INTERVAL_MS)

  return {
    publish(partial) {
      const snapshot = { count: 0, voteState: 'idle', connected: false, ...partial }
      persist(snapshot)
      try {
        channel.postMessage({ type: 'state', ...snapshot })
      } catch {
        /* ignore */
      }
    },
    close() {
      clearInterval(heartbeatId)
      try {
        channel.close()
      } catch {
        /* ignore */
      }
    }
  }
}

/**
 * Build a subscriber used by the aid view.
 * @param {string} sessionCode
 * @param {(state: { count: number, voteState: string, connected: boolean }) => void} onUpdate
 * @returns {{ start(): void, close(): void }}
 */
export function createSessionSubscriber(sessionCode, onUpdate) {
  if (typeof BroadcastChannel === 'undefined') {
    return { start() {}, close() {} }
  }

  let channel
  try {
    channel = new BroadcastChannel(channelName(sessionCode))
  } catch {
    return { start() {}, close() {} }
  }

  channel.onmessage = (event) => {
    if (event.data && event.data.type === 'state') {
      onUpdate({
        count: event.data.count ?? 0,
        voteState: event.data.voteState ?? 'idle',
        connected: Boolean(event.data.connected)
      })
    }
  }

  return {
    start() {
      try {
        channel.postMessage({ type: 'hello' })
      } catch {
        /* ignore */
      }
    },
    close() {
      try {
        channel.close()
      } catch {
        /* ignore */
      }
    }
  }
}
