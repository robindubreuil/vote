package config

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// captureSlog captures slog output at INFO into a buffer for the duration
// of the test. Returns the buffer; the test reads it after invoking the
// code under test.
func captureSlog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	slog.SetDefault(logger)
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

// TestGetEnvDurationWarnsOnParseError is the B5 regression test: a typo
// in a duration env var (e.g. "1h3" or "10 minutes" where the parser wants
// "10m") previously fell back to the default silently. The fix surfaces
// the bad value via slog.Warn so an operator sees their override rejected
// rather than debugging a server that mysteriously ignored the env var.
func TestGetEnvDurationWarnsOnParseError(t *testing.T) {
	buf := captureSlog(t)

	t.Setenv("VOTE_TEST_DURATION", "1h3")
	got := getEnvDuration("VOTE_TEST_DURATION", 42*time.Second)
	if got != 42*time.Second {
		t.Errorf("expected default fallback 42s, got %v", got)
	}

	out := buf.String()
	for _, want := range []string{
		"invalid duration env",
		"key=VOTE_TEST_DURATION",
		"value=1h3",
		"default=42s",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("log output missing %q\noutput:\n%s", want, out)
		}
	}
}

// TestGetEnvDurationNoWarnOnValid covers the happy path: a valid override
// produces no warning and is returned verbatim.
func TestGetEnvDurationNoWarnOnValid(t *testing.T) {
	buf := captureSlog(t)

	t.Setenv("VOTE_TEST_DURATION", "2h45m")
	got := getEnvDuration("VOTE_TEST_DURATION", 42*time.Second)
	if got != 2*time.Hour+45*time.Minute {
		t.Errorf("expected 2h45m, got %v", got)
	}
	if strings.Contains(buf.String(), "invalid duration env") {
		t.Errorf("valid override should not warn\noutput:\n%s", buf.String())
	}
}

// TestGetEnvDurationNoWarnOnMissing covers the unset case: no env, no warn.
func TestGetEnvDurationNoWarnOnMissing(t *testing.T) {
	buf := captureSlog(t)
	t.Setenv("VOTE_TEST_DURATION", "")
	got := getEnvDuration("VOTE_TEST_DURATION", 7*time.Second)
	if got != 7*time.Second {
		t.Errorf("expected default, got %v", got)
	}
	if strings.Contains(buf.String(), "invalid duration env") {
		t.Errorf("missing env should not warn\noutput:\n%s", buf.String())
	}
}

// TestGetEnvIntWarnsOnParseError mirrors the duration test for int envs.
func TestGetEnvIntWarnsOnParseError(t *testing.T) {
	buf := captureSlog(t)

	t.Setenv("VOTE_TEST_INT", "abc")
	got := getEnvInt("VOTE_TEST_INT", 99)
	if got != 99 {
		t.Errorf("expected default fallback 99, got %d", got)
	}

	out := buf.String()
	for _, want := range []string{
		"invalid int env",
		"key=VOTE_TEST_INT",
		"value=abc",
		"default=99",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("log output missing %q\noutput:\n%s", want, out)
		}
	}
}

// TestGetEnvIntNoWarnOnValid covers the happy path.
func TestGetEnvIntNoWarnOnValid(t *testing.T) {
	buf := captureSlog(t)
	t.Setenv("VOTE_TEST_INT", "123")
	got := getEnvInt("VOTE_TEST_INT", 99)
	if got != 123 {
		t.Errorf("expected 123, got %d", got)
	}
	if strings.Contains(buf.String(), "invalid int env") {
		t.Errorf("valid override should not warn\noutput:\n%s", buf.String())
	}
}

// TestSplitListFiltersEmptyElements is the B5 second half: list envs
// (ALLOWED_ORIGINS, TRUSTED_PROXIES, VALID_COLORS) previously kept empty
// elements from typos like "a,,b" or a trailing comma, producing phantom
// "" list members. splitList drops them.
func TestSplitListFiltersEmptyElements(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"a", []string{"a"}},
		{"a,b,c", []string{"a", "b", "c"}},
		{"a,,b", []string{"a", "b"}},                        // typo
		{"a,b,", []string{"a", "b"}},                        // trailing comma
		{",,a,,", []string{"a"}},                            // only one valid
		{"  spaced  ,  more  ", []string{"spaced", "more"}}, // trimmed
		{",,,", nil},                                        // all empty
	}
	for _, tc := range cases {
		got := splitList(tc.in)
		if !slicesEqual(got, tc.want) {
			t.Errorf("splitList(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestLoadConfigFiltersEmptyListElements is an end-to-end regression for
// the B5 list-filtering fix: a malformed ALLOWED_ORIGINS with a trailing
// comma or double-comma must not yield a phantom "" origin in the loaded
// Config, and the same applies to TRUSTED_PROXIES and VALID_COLORS.
func TestLoadConfigFiltersEmptyListElements(t *testing.T) {
	t.Setenv("ALLOWED_ORIGINS", "http://a.com,,http://b.com,")
	t.Setenv("TRUSTED_PROXIES", "10.0.0.1,,10.0.0.2,")
	t.Setenv("VALID_COLORS", "rouge,vert,,")

	cfg := LoadConfig()

	if !slicesEqual(cfg.AllowedOrigins, []string{"http://a.com", "http://b.com"}) {
		t.Errorf("AllowedOrigins has phantom elements: %v", cfg.AllowedOrigins)
	}
	if !slicesEqual(cfg.TrustedProxies, []string{"10.0.0.1", "10.0.0.2"}) {
		t.Errorf("TrustedProxies has phantom elements: %v", cfg.TrustedProxies)
	}
	if !slicesEqual(cfg.ValidColors, []string{"rouge", "vert"}) {
		t.Errorf("ValidColors has phantom elements: %v", cfg.ValidColors)
	}
}

// TestLoadConfigWarnsOnBadDuration verifies the warning surfaces when the
// bad value flows through LoadConfig, not just the direct helper.
func TestLoadConfigWarnsOnBadDuration(t *testing.T) {
	buf := captureSlog(t)
	t.Setenv("SESSION_TIMEOUT", "not-a-duration")
	_ = LoadConfig()
	if !strings.Contains(buf.String(), "invalid duration env") {
		t.Errorf("LoadConfig should warn on bad SESSION_TIMEOUT\noutput:\n%s", buf.String())
	}
}
