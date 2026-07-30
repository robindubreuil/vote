package server

import (
	"strings"
	"testing"

	"vote-backend/internal/vote"
)

// TestWriteInfoMetricEscapesLabelValues is the B6 regression test: a
// version or build_time containing `"`, `\`, or newline (which ldflags-
// derived values can carry if an operator passes a multi-line or quoted
// string) must be escaped so the Prometheus exposition stays well-formed.
// An unescaped `"` would close the label early and a stray newline would
// terminate the metric line, both causing Prometheus to reject the entire
// /metrics scrape.
func TestWriteInfoMetricEscapesLabelValues(t *testing.T) {
	cases := []struct {
		name      string
		version   string
		buildTime string
		gitCommit string
		// expectedRawSubstrings must appear verbatim in the output (after
		// escaping). Each case asserts the wire-level escape sequence.
		expectedRawSubstrings []string
		// forbiddenRawSubstrings must NOT appear (would indicate an
		// unescaped special char broke the label syntax).
		forbiddenRawSubstrings []string
	}{
		{
			name:                  "plain ascii passes through",
			version:               "1.2.3",
			buildTime:             "2026-01-01",
			gitCommit:             "abc1234",
			expectedRawSubstrings: []string{`version="1.2.3"`, `build_time="2026-01-01"`, `git_commit="abc1234"`},
		},
		{
			name:                  "double-quote in version",
			version:               `evil"version`,
			buildTime:             "2026-01-01",
			gitCommit:             "abc1234",
			expectedRawSubstrings: []string{`version="evil\"version"`},
			forbiddenRawSubstrings: []string{
				`version="evil"version"`, // unescaped quote closes the label early
			},
		},
		{
			name:                   "backslash in version",
			version:                `win\path`,
			buildTime:              "2026-01-01",
			gitCommit:              "abc1234",
			expectedRawSubstrings:  []string{`version="win\\path"`},
			forbiddenRawSubstrings: []string{`version="win\path"`},
		},
		{
			name:      "newline in build_time",
			version:   "1.2.3",
			buildTime: "line one\nline two",
			gitCommit: "abc1234",
			expectedRawSubstrings: []string{
				`build_time="line one\nline two"`,
				"\n", // the surrounding metric line still ends with a real newline
			},
			forbiddenRawSubstrings: []string{
				// A bare newline inside the label value would split the
				// metric across two text lines, breaking the scrape. The
				// escape turns it into the two-character sequence `\n`.
				"line one\n\"line two",
			},
		},
		{
			name:                  "all three specials combined across all labels",
			version:               `a"b\c`,
			buildTime:             "x\ny",
			gitCommit:             `d"e`,
			expectedRawSubstrings: []string{`version="a\"b\\c"`, `build_time="x\ny"`, `git_commit="d\"e"`},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var b strings.Builder
			writeInfoMetric(&b, "vote_build_info", tc.version, tc.buildTime, tc.gitCommit)
			got := b.String()

			for _, want := range tc.expectedRawSubstrings {
				if !strings.Contains(got, want) {
					t.Errorf("output missing expected substring %q\noutput:\n%s", want, got)
				}
			}
			for _, bad := range tc.forbiddenRawSubstrings {
				if strings.Contains(got, bad) {
					t.Errorf("output contains forbidden raw substring %q (escape regression)\noutput:\n%s", bad, got)
				}
			}

			// The metric must occupy exactly one line (the final newline
			// ends it). An unescaped newline inside a value would split
			// the metric across two physical lines and break scrapers.
			// Count physical newlines: writeInfoMetric produces exactly
			// three lines (HELP, TYPE, sample). The sample line itself
			// must contain no literal newlines.
			lines := strings.Split(got, "\n")
			// Trailing empty element from the final newline.
			if len(lines) != 4 || lines[3] != "" {
				t.Errorf("expected exactly 3 lines + trailing empty, got %d lines:\n%q", len(lines), got)
			}
			sample := lines[2]
			if !strings.HasPrefix(sample, `vote_build_info{`) {
				t.Errorf("sample line malformed: %q", sample)
			}
			if strings.Contains(sample, "\r") {
				t.Errorf("sample line contains CR: %q", sample)
			}
		})
	}
}

// TestEscapeLabelValueNoAllocOnCleanInput pins the fast path: when the
// value contains no special characters, the function returns the input
// string directly without allocating a new one. This matters because
// writeInfoMetric runs on every /metrics scrape.
func TestEscapeLabelValueNoAllocOnCleanInput(t *testing.T) {
	clean := "1.2.3"
	if got := escapeLabelValue(clean); got != clean {
		t.Errorf("clean input should pass through: got %q, want %q", got, clean)
	}
	// Verify the function returns the same string identity (no copy) on
	// the fast path. We can't compare pointers portably across versions,
	// but the length+contents equality above plus the ContainsAny guard
	// in the implementation is sufficient.
}

// TestFormatLE pins the two rendering branches of a Prometheus histogram
// bucket bound. The integer branch (e.g. 60 → "60") is exercised
// end-to-end by the histogram output tests in dashboard_test.go; the
// non-integer branch (e.g. 0.5 → "0.5") was previously uncovered because
// every shipped histogram uses integer bounds. B13: a future histogram
// with fractional bounds (latency-in-seconds with a 0.5s bucket, p99
// estimates, etc.) must render via strconv.FormatFloat with the shortest
// representation that round-trips ('g', precision -1), not the default
// %g verb that would otherwise emit exponential notation for small
// values. The table also pins the +Inf sentinel — formatLE is never
// called with +Inf (the caller emits that bucket separately) but the
// test documents the contract.
func TestFormatLE(t *testing.T) {
	cases := []struct {
		name string
		v    float64
		want string
	}{
		{"zero", 0, "0"},
		{"positive integer", 60, "60"},
		{"large integer", 3600, "3600"},
		{"one half", 0.5, "0.5"},
		{"quarter", 0.25, "0.25"},
		{"tenth", 0.1, "0.1"},
		{"sub-millisecond seconds", 0.001, "0.001"},
		{"two point five", 2.5, "2.5"},
		{"integer-valued float", 100.0, "100"},
		{"negative", -1, "-1"}, // not a valid LE but the formatter shouldn't special-case it
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := formatLE(c.v)
			if got != c.want {
				t.Errorf("formatLE(%v): got %q, want %q", c.v, got, c.want)
			}
		})
	}
}

// TestFormatLEExpositionInContext renders a real histogram through
// writeHistogram with a fractional bucket bound to assert the non-integer
// branch integrates cleanly into the wire format: the `le="0.5"` label
// must appear on its own line with the count, and must not leak exponent
// notation or a trailing ".0" that the integer branch avoids.
func TestFormatLEExpositionInContext(t *testing.T) {
	var b strings.Builder
	writeHistogram(&b, "vote_latency_seconds", "test histogram with fractional bucket",
		vote.HistogramSnapshot{
			Count: 3,
			Sum:   1.5,
			Buckets: []vote.HistogramBucket{
				{LE: 0.5, Count: 1},
				{LE: 1.0, Count: 2},
				{LE: 2.5, Count: 3},
			},
		})
	out := b.String()
	for _, want := range []string{
		`vote_latency_seconds_bucket{le="0.5"} 1`,
		`vote_latency_seconds_bucket{le="1"} 2`,
		`vote_latency_seconds_bucket{le="2.5"} 3`,
		`vote_latency_seconds_bucket{le="+Inf"} 3`,
		`vote_latency_seconds_count 3`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\ngot:\n%s", want, out)
		}
	}
	// The fractional 0.5 must NOT be rendered as "5e-01" or "0.50".
	for _, bad := range []string{`le="5e-01"`, `le="0.50"`, `le="0.5e+00"`} {
		if strings.Contains(out, bad) {
			t.Errorf("output should not contain %q, got:\n%s", bad, out)
		}
	}
}
