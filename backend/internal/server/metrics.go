package server

import (
	"fmt"
	"runtime/metrics"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"vote-backend/internal/models"
	"vote-backend/internal/vote"
)

type buildInfo struct {
	Version   string
	BuildTime string
	GitCommit string
}

// SetBuildInfo stores the build metadata injected via -ldflags so the
// /version endpoint and the vote_build_info metric can report it. All
// three values are operator-set at build time; missing values surface as
// the literal "unknown" rather than empty strings, which keeps the JSON
// output stable for monitors that parse it.
func (s *Server) SetBuildInfo(version, buildTime, gitCommit string) {
	s.buildInfo = buildInfo{
		Version:   version,
		BuildTime: buildTime,
		GitCommit: gitCommit,
	}
}

func (b buildInfo) versionOrDefault() string {
	if b.Version != "" {
		return b.Version
	}
	return "unknown"
}

func (b buildInfo) buildTimeOrDefault() string {
	if b.BuildTime != "" {
		return b.BuildTime
	}
	return "unknown"
}

func (b buildInfo) gitCommitOrDefault() string {
	if b.GitCommit != "" {
		return b.GitCommit
	}
	return "unknown"
}

func (s *Server) handleMetrics(c *gin.Context) {
	c.Header("Content-Type", "text/plain; version=0.0.4; charset=utf-8")

	m := s.hub.GetMetrics()
	p := s.hub.ProductStats()
	uptime := time.Since(s.startTime).Seconds()

	var b strings.Builder

	writeGauge(&b, "vote_uptime_seconds", "Process uptime in seconds", uptime)
	writeGauge(&b, "vote_sessions_active", "Number of active sessions", float64(m.ActiveSessions))
	writeGauge(&b, "vote_trainers_connected", "Number of connected trainers", float64(m.ConnectedTrainers))
	writeGauge(&b, "vote_stagiaires_connected", "Number of connected stagiaires", float64(m.ConnectedStagiaires))

	// Prometheus expects exactly one HELP line and one TYPE line per metric
	// name. Iterate the fixed state set so the header is emitted once and the
	// output order is deterministic (the dashboard parser tolerates any
	// order, but scrapers and tests benefit from stability).
	const stateMetric = "vote_sessions_by_state"
	fmt.Fprintf(&b, "# HELP %s Sessions grouped by vote state\n", stateMetric)
	fmt.Fprintf(&b, "# TYPE %s gauge\n", stateMetric)
	for _, state := range []string{models.VoteStateIdle, models.VoteStateActive, models.VoteStateClosed} {
		fmt.Fprintf(&b, `%s{state="%s"} %g`+"\n", stateMetric, state, float64(m.VoteStates[state]))
	}

	writeCounter(&b, "vote_sessions_created_total", "Total sessions created since process start", float64(p.SessionsCreated))
	writeCounter(&b, "vote_votes_started_total", "Total votes opened by trainers", float64(p.VotesStarted))
	writeCounter(&b, "vote_votes_cast_total", "Total individual votes submitted by trainees", float64(p.VotesCast))
	writeCounter(&b, "vote_trainees_joined_total", "Total trainee join events", float64(p.TraineesJoined))
	writeCounter(&b, "vote_game_enabled_votes_total", "Votes that had the waiting mini-game enabled", float64(p.GameEnabledVotes))
	writeCounter(&b, "vote_multiple_choice_votes_total", "Votes configured as multiple-choice", float64(p.MultipleChoiceVotes))

	writeHistogram(&b, "vote_session_duration_seconds", "Wall-clock duration of ended sessions", p.SessionDuration)
	writeHistogram(&b, "vote_votes_per_session", "Number of submitted votes per ended session", p.VotesPerSession)
	writeHistogram(&b, "vote_trainees_per_session", "Number of trainees who joined per ended session", p.TraineesPerSession)

	// R12: Go runtime gauges are read in a single non-STW pass via
	// runtime/metrics. The previous runtime.ReadMemStats call forced a
	// stop-the-world on every /metrics scrape and every 30s dashboard
	// poll, periodically stalling the real-time WebSocket path the
	// endpoint exists to monitor — and the stall itself was invisible
	// to /metrics. runtime/metrics.Read never stops the world.
	rt := readRuntimeGauges()
	writeGauge(&b, "go_goroutines", "Number of goroutines", float64(rt.goroutines))
	writeGauge(&b, "go_mem_alloc_bytes", "Bytes of heap memory occupied by live and unmarked objects", float64(rt.heapObjectBytes))
	writeGauge(&b, "go_mem_sys_bytes", "Bytes of memory mapped by the runtime (all classes)", float64(rt.totalBytes))
	writeGauge(&b, "go_mem_heap_objects", "Number of live or unswept heap objects", float64(rt.heapObjects))
	writeGauge(&b, "go_gc_total", "Total number of completed GC cycles", float64(rt.gcCycles))

	if s.buildInfo.Version != "" || s.buildInfo.GitCommit != "" {
		writeInfoMetric(&b, "vote_build_info",
			s.buildInfo.versionOrDefault(),
			s.buildInfo.buildTimeOrDefault(),
			s.buildInfo.gitCommitOrDefault())
	}

	c.String(200, b.String())
}

func writeGauge(b *strings.Builder, name, help string, value float64) {
	fmt.Fprintf(b, "# HELP %s %s\n", name, help)
	fmt.Fprintf(b, "# TYPE %s gauge\n", name)
	fmt.Fprintf(b, "%s %g\n", name, value)
}

func writeCounter(b *strings.Builder, name, help string, value float64) {
	fmt.Fprintf(b, "# HELP %s %s\n", name, help)
	fmt.Fprintf(b, "# TYPE %s counter\n", name)
	fmt.Fprintf(b, "%s %g\n", name, value)
}

// writeHistogram renders a histogram in Prometheus cumulative-bucket format:
// one `_bucket{le="x"}` line per bound (cumulative), plus `+Inf` total,
// followed by `_sum` and `_count`.
func writeHistogram(b *strings.Builder, name, help string, h vote.HistogramSnapshot) {
	fmt.Fprintf(b, "# HELP %s %s\n", name, help)
	fmt.Fprintf(b, "# TYPE %s histogram\n", name)
	for _, bucket := range h.Buckets {
		fmt.Fprintf(b, `%s_bucket{le="%s"} %d`+"\n", name, formatLE(bucket.LE), bucket.Count)
	}
	fmt.Fprintf(b, `%s_bucket{le="+Inf"} %d`+"\n", name, h.Count)
	fmt.Fprintf(b, "%s_sum %g\n", name, h.Sum)
	fmt.Fprintf(b, "%s_count %d\n", name, h.Count)
}

// formatLE renders a bucket upper bound for a Prometheus label. Integer
// bounds print without a trailing decimal (e.g. "5" not "5.0"); +Inf is
// rendered separately by the caller.
func formatLE(v float64) string {
	if v == float64(int64(v)) {
		return strconv.FormatInt(int64(v), 10)
	}
	return strconv.FormatFloat(v, 'g', -1, 64)
}

// escapeLabelValue escapes a string for safe interpolation into a Prometheus
// label value. The exposition format only allows `\`, `"`, and `\n` to be
// backslash-escaped; any other byte is passed through verbatim. A bare `"`,
// `\`, or newline in the value (which ldflags-derived build metadata can
// carry if an operator passes a multi-line or quoted string) would otherwise
// break the label syntax and cause Prometheus to reject the entire /metrics
// scrape (B6). Inputs here are operator-set via -ldflags; they are trusted
// not to be hostile, but they must still be well-formed.
func escapeLabelValue(v string) string {
	if !strings.ContainsAny(v, `\`+"\n"+`"`) {
		return v
	}
	var b strings.Builder
	b.Grow(len(v))
	for _, r := range v {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func writeInfoMetric(b *strings.Builder, name, version, buildTime, gitCommit string) {
	fmt.Fprintf(b, "# HELP %s Build information\n", name)
	fmt.Fprintf(b, "# TYPE %s gauge\n", name)
	fmt.Fprintf(b, `%s{version="%s",build_time="%s",git_commit="%s"} 1`+"\n",
		name, escapeLabelValue(version), escapeLabelValue(buildTime), escapeLabelValue(gitCommit))
}

// runtimeGauges holds the Go runtime values exposed under /metrics. Each
// field maps 1:1 to a runtime/metrics sample name (see readRuntimeGauges).
type runtimeGauges struct {
	heapObjectBytes uint64 // /memory/classes/heap/objects:bytes
	totalBytes      uint64 // /memory/classes/total:bytes
	heapObjects     uint64 // /gc/heap/objects:objects
	gcCycles        uint64 // /gc/cycles/total:gc-cycles
	goroutines      uint64 // /sched/goroutines:goroutines
}

// readRuntimeGauges samples the Go runtime gauges in a single non-STW pass.
//
// R12: this replaces runtime.ReadMemStats, which forces a stop-the-world.
// Every metric here is KindUint64 and its name is guaranteed stable across
// Go versions (a kind change would introduce a new name, never mutate an
// existing one), so KindBad only surfaces on a future Go that dropped a
// name — in which case we fall back to 0 rather than panic so /metrics
// keeps serving. The five sample names mirror the prior MemStats fields:
//
//   - heapObjectBytes ≈ HeapAlloc (live + dead-not-yet-marked heap)
//   - totalBytes      ≈ Sys       (all runtime-mapped RW memory)
//   - heapObjects     ≈ HeapObjects
//   - gcCycles        = NumGC
//   - goroutines      = NumGoroutine
//
// A fresh sample slice per call keeps concurrent /metrics scrapes free of
// the aliasing caveat in metrics.Read's docs (Values must not be read while
// a Read with that value is outstanding).
func readRuntimeGauges() runtimeGauges {
	samples := []metrics.Sample{
		{Name: "/memory/classes/heap/objects:bytes"},
		{Name: "/memory/classes/total:bytes"},
		{Name: "/gc/heap/objects:objects"},
		{Name: "/gc/cycles/total:gc-cycles"},
		{Name: "/sched/goroutines:goroutines"},
	}
	metrics.Read(samples)
	get := func(i int) uint64 {
		if samples[i].Value.Kind() == metrics.KindUint64 {
			return samples[i].Value.Uint64()
		}
		return 0
	}
	return runtimeGauges{
		heapObjectBytes: get(0),
		totalBytes:      get(1),
		heapObjects:     get(2),
		gcCycles:        get(3),
		goroutines:      get(4),
	}
}
