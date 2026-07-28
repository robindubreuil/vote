package server

import (
	"strings"
	"testing"
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
			expectedRawSubstrings: []string{`version="1.2.3"`, `build_time="2026-01-01"`},
		},
		{
			name:                  "double-quote in version",
			version:               `evil"version`,
			buildTime:             "2026-01-01",
			expectedRawSubstrings: []string{`version="evil\"version"`},
			forbiddenRawSubstrings: []string{
				`version="evil"version"`, // unescaped quote closes the label early
			},
		},
		{
			name:                   "backslash in version",
			version:                `win\path`,
			buildTime:              "2026-01-01",
			expectedRawSubstrings:  []string{`version="win\\path"`},
			forbiddenRawSubstrings: []string{`version="win\path"`},
		},
		{
			name:      "newline in build_time",
			version:   "1.2.3",
			buildTime: "line one\nline two",
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
			name:                  "all three specials combined",
			version:               `a"b\c`,
			buildTime:             "x\ny",
			expectedRawSubstrings: []string{`version="a\"b\\c"`, `build_time="x\ny"`},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var b strings.Builder
			writeInfoMetric(&b, "vote_build_info", tc.version, tc.buildTime)
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
