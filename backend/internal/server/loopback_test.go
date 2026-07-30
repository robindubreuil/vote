package server

import (
	"log/slog"
	"strings"
	"testing"
)

// captureWarnBuffer swaps the default slog handler for one writing to a
// string buffer at WARN so tests can assert a warning was (or was not)
// emitted. Returns the buffer and a restore func.
func captureWarnBuffer(t *testing.T) (*strings.Builder, func()) {
	t.Helper()
	var buf strings.Builder
	prev := slog.Default()
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})
	slog.SetDefault(slog.New(handler))
	return &buf, func() { slog.SetDefault(prev) }
}

// TestLoopbackMonitorWarnsOnLoopbackWithoutTrustedProxies is the S16
// happy path: TrustedProxies empty AND a high loopback share ⇒ warn
// exactly once per armed window.
func TestLoopbackMonitorWarnsOnLoopbackWithoutTrustedProxies(t *testing.T) {
	buf, restore := captureWarnBuffer(t)
	defer restore()

	m := newLoopbackMonitor()
	for i := 0; i < loopbackWarnMinObservations; i++ {
		m.observe("127.0.0.1")
	}
	if !m.maybeWarn(true) {
		t.Fatal("expected a warning when all IPs are loopback and TrustedProxies is empty")
	}
	if !strings.Contains(buf.String(), "TRUSTED_PROXIES is unset") {
		t.Errorf("warning text missing the actionable hint, got:\n%s", buf.String())
	}

	// A second evaluation immediately after must NOT re-warn — the
	// window was reset by evaluateSnapshot, but now it's empty (below
	// the min-observation floor), and warned is armed regardless.
	buf.Reset()
	if m.maybeWarn(true) {
		t.Error("must not spam the warning on back-to-back calls")
	}
}

// TestLoopbackMonitorDoesNotWarnWithTrustedProxies pins the gate: once
// the operator has configured trusted proxies, even 100% loopback IPs
// (e.g. a dev loop) must not trip the warning.
func TestLoopbackMonitorDoesNotWarnWithTrustedProxies(t *testing.T) {
	buf, restore := captureWarnBuffer(t)
	defer restore()

	m := newLoopbackMonitor()
	for i := 0; i < 100; i++ {
		m.observe("127.0.0.1")
	}
	if m.maybeWarn(false) {
		t.Fatal("must not warn when TrustedProxies is configured")
	}
	if buf.Len() != 0 {
		t.Errorf("expected no warning, got:\n%s", buf.String())
	}
}

// TestLoopbackMonitorRespectsMinObservations ensures a handful of
// localhost health checks at boot can't trip a false positive.
func TestLoopbackMonitorRespectsMinObservations(t *testing.T) {
	buf, restore := captureWarnBuffer(t)
	defer restore()

	m := newLoopbackMonitor()
	for i := 0; i < loopbackWarnMinObservations-1; i++ {
		m.observe("127.0.0.1")
	}
	if m.maybeWarn(true) {
		t.Errorf("must not warn below the min-observation floor (%d)", loopbackWarnMinObservations)
	}
	if buf.Len() != 0 {
		t.Errorf("expected no warning below floor, got:\n%s", buf.String())
	}
}

// TestLoopbackMonitorIgnoresNonLoopbackTraffic verifies the fraction
// gate: when real external IPs dominate, the warning stays off even
// with TrustedProxies empty (e.g. a direct-to-internet deploy that
// genuinely has no reverse proxy).
func TestLoopbackMonitorIgnoresNonLoopbackTraffic(t *testing.T) {
	buf, restore := captureWarnBuffer(t)
	defer restore()

	m := newLoopbackMonitor()
	for i := 0; i < 20; i++ {
		m.observe("203.0.113.42")
	}
	m.observe("127.0.0.1") // 1/21 loopback — well below the threshold
	if m.maybeWarn(true) {
		t.Error("must not warn when external IPs dominate")
	}
	if buf.Len() != 0 {
		t.Errorf("expected no warning for external traffic, got:\n%s", buf.String())
	}
}

// TestLoopbackMonitorRearmsAfterConditionClears verifies the warned flag
// resets when the loopback share drops, so a deploy that's fixed then
// regressed warns again instead of going silent after the first hit.
func TestLoopbackMonitorRearmsAfterConditionClears(t *testing.T) {
	buf, restore := captureWarnBuffer(t)
	defer restore()

	m := newLoopbackMonitor()
	// Arm: all loopback.
	for i := 0; i < loopbackWarnMinObservations; i++ {
		m.observe("127.0.0.1")
	}
	if !m.maybeWarn(true) {
		t.Fatal("expected first warning")
	}
	// Clear: all external — warned must reset.
	buf.Reset()
	for i := 0; i < loopbackWarnMinObservations; i++ {
		m.observe("203.0.113.42")
	}
	if m.maybeWarn(true) {
		t.Error("must not warn when condition cleared")
	}
	// Re-arm: loopback again — warns once more.
	for i := 0; i < loopbackWarnMinObservations; i++ {
		m.observe("127.0.0.1")
	}
	if !m.maybeWarn(true) {
		t.Fatal("expected re-arm: warning must fire again after a clear/regress cycle")
	}
	if !strings.Contains(buf.String(), "TRUSTED_PROXIES is unset") {
		t.Errorf("expected re-armed warning, got:\n%s", buf.String())
	}
}

// TestLoopbackMonitorIgnoresUnparsableIP pins the observe robustness: a
// garbage ClientIP (proxy misconfiguration, malformed header) must not
// panic and must not count as loopback.
func TestLoopbackMonitorIgnoresUnparsableIP(t *testing.T) {
	m := newLoopbackMonitor()
	m.observe("not-an-ip")
	m.observe("")
	for i := 0; i < loopbackWarnMinObservations; i++ {
		m.observe("not-an-ip")
	}
	buf, restore := captureWarnBuffer(t)
	defer restore()
	// All observations are non-loopback garbage ⇒ below fraction ⇒ no warn.
	if m.maybeWarn(true) {
		t.Error("unparsable IPs must not count as loopback")
	}
	if buf.Len() != 0 {
		t.Errorf("expected no warning for garbage IPs, got:\n%s", buf.String())
	}
}
