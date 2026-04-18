package server

import (
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
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
	uptime := time.Since(s.startTime).Seconds()

	var b strings.Builder

	writeGauge(&b, "vote_uptime_seconds", "Process uptime in seconds", uptime)
	writeGauge(&b, "vote_sessions_active", "Number of active sessions", float64(m.ActiveSessions))
	writeGauge(&b, "vote_trainers_connected", "Number of connected trainers", float64(m.ConnectedTrainers))
	writeGauge(&b, "vote_stagiaires_connected", "Number of connected stagiaires", float64(m.ConnectedStagiaires))

	for state, count := range m.VoteStates {
		writeGaugeWithLabels(&b, "vote_sessions_by_state", "Sessions grouped by vote state", float64(count), "state", state)
	}

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
