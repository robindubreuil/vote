package server

import (
	"log/slog"
	"net"
	"sync/atomic"
	"time"
)

// S16: per-IP protections collapse behind a reverse proxy when
// VOTE_TRUSTED_PROXIES is unset. Gin's ClientIP() returns RemoteAddr
// when TrustedProxies is empty, and behind the documented Caddy deploy
// (reverse_proxy … localhost:8080) RemoteAddr is 127.0.0.1 for every
// client. The per-IP connection cap becomes a global ceiling, the
// session-creation cap is shared across all trainers, and one
// attacker's brute-force trips failed-join backoff that locks out every
// legitimate student (amplification DoS).
//
// loopbackMonitor watches the resolved ClientIP of inbound requests and
// warns once per "armed" window when the operator is in that state: no
// trusted proxies configured AND a high fraction of resolved IPs are
// loopback. The warning is advisory — it never refuses traffic — and it
// is gated behind a minimum observation count so a few localhost probes
// at startup can't trip it.

const (
	// loopbackWarnMinObservations is the minimum requests observed in a
	// window before the warning can fire. Stops a handful of localhost
	// health checks at boot from a false positive.
	loopbackWarnMinObservations = 10
	// loopbackWarnFraction is the ClientIP-loopback share above which
	// (with TrustedProxies empty) we warn. Behind a proxy with
	// TrustedProxies unset, ~100% of IPs collapse to the proxy's
	// loopback address, so 0.5 is a wide margin.
	loopbackWarnFraction = 0.5
	// loopbackWatchInterval is how often the background evaluator runs.
	// Long enough to accumulate a meaningful sample, short enough that
	// a misconfigured deploy surfaces a warning within a minute of real
	// traffic rather than going unnoticed.
	loopbackWatchInterval = time.Minute
)

type loopbackMonitor struct {
	loopback atomic.Int64
	total    atomic.Int64
	// warned suppresses log spam: set when a warning fires, cleared
	// when the condition clears so a future re-occurrence warns again.
	warned atomic.Bool
}

func newLoopbackMonitor() *loopbackMonitor {
	return &loopbackMonitor{}
}

// observe records one inbound request's resolved ClientIP. Cheap on the
// hot HTTP path: one IsLoopback check and two atomic adds.
func (m *loopbackMonitor) observe(clientIP string) {
	m.total.Add(1)
	if ip := net.ParseIP(clientIP); ip != nil && ip.IsLoopback() {
		m.loopback.Add(1)
	}
}

// evaluateSnapshot reads + resets the window and returns whether the
// S16 warning condition currently holds. Resetting after read makes
// each evaluation a fresh sliding window rather than a cumulative
// average, so a deploy that's fixed is noticed within one interval.
func (m *loopbackMonitor) evaluateSnapshot() (loopback, total int64) {
	loopback = m.loopback.Swap(0)
	total = m.total.Swap(0)
	return loopback, total
}

// maybeWarn emits the S16 warning if the configured conditions are met.
// trustedProxiesEmpty gates the whole check — the warning is meaningless
// once the operator has configured trusted proxies. Returns true iff it
// emitted a warning this call.
func (m *loopbackMonitor) maybeWarn(trustedProxiesEmpty bool) bool {
	if !trustedProxiesEmpty {
		m.warned.Store(false)
		return false
	}
	loopback, total := m.evaluateSnapshot()
	if total < loopbackWarnMinObservations {
		return false
	}
	if float64(loopback)/float64(total) < loopbackWarnFraction {
		m.warned.Store(false)
		return false
	}
	if m.warned.Swap(true) {
		return false
	}
	slog.Warn("TRUSTED_PROXIES is unset and most client IPs resolve to loopback — per-IP protections (connection cap, session-creation cap, failed-join backoff) are collapsing into a single shared bucket; set VOTE_TRUSTED_PROXIES to your reverse proxy's address",
		"loopback_ips", loopback, "total_ips", total)
	return true
}

// startLoopbackWatch launches the periodic evaluator. It returns
// immediately; the goroutine exits when done is closed. The monitor is
// safe to use without this goroutine (observe still records); the
// watcher only turns observations into warnings.
func (s *Server) startLoopbackWatch(done <-chan struct{}) {
	if s.loopback == nil {
		return
	}
	trustedProxiesEmpty := len(s.config.TrustedProxies) == 0
	s.watcherWG.Add(1)
	go func() {
		defer s.watcherWG.Done()
		ticker := time.NewTicker(loopbackWatchInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.loopback.maybeWarn(trustedProxiesEmpty)
			case <-done:
				return
			}
		}
	}()
}
