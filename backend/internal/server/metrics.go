package server

import (
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"vote-backend/internal/vote"
)

type buildInfo struct {
	Version   string
	BuildTime string
}

func (s *Server) SetBuildInfo(version, buildTime string) {
	s.buildInfo = buildInfo{Version: version, BuildTime: buildTime}
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

	for state, count := range m.VoteStates {
		writeGaugeWithLabels(&b, "vote_sessions_by_state", "Sessions grouped by vote state", float64(count), "state", state)
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

	writeGauge(&b, "go_goroutines", "Number of goroutines", float64(runtime.NumGoroutine()))

	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	writeGauge(&b, "go_mem_alloc_bytes", "Bytes of allocated heap objects", float64(memStats.HeapAlloc))
	writeGauge(&b, "go_mem_sys_bytes", "Total bytes of memory obtained from the OS", float64(memStats.Sys))
	writeGauge(&b, "go_mem_heap_objects", "Number of allocated heap objects", float64(memStats.HeapObjects))
	writeGauge(&b, "go_gc_total", "Total number of GC cycles", float64(memStats.NumGC))

	if s.buildInfo.Version != "" {
		writeInfoMetric(&b, "vote_build_info", s.buildInfo.Version, s.buildInfo.BuildTime)
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

func writeGaugeWithLabels(b *strings.Builder, name, help string, value float64, labelKey, labelValue string) {
	fmt.Fprintf(b, "# HELP %s %s\n", name, help)
	fmt.Fprintf(b, "# TYPE %s gauge\n", name)
	fmt.Fprintf(b, `%s{%s="%s"} %g`+"\n", name, labelKey, labelValue, value)
}

func writeInfoMetric(b *strings.Builder, name, version, buildTime string) {
	fmt.Fprintf(b, "# HELP %s Build information\n", name)
	fmt.Fprintf(b, "# TYPE %s gauge\n", name)
	fmt.Fprintf(b, `%s{version="%s",build_time="%s"} 1`+"\n", name, version, buildTime)
}
