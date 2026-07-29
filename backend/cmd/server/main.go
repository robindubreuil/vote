package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"vote-backend/internal/config"
	"vote-backend/internal/hub"
	"vote-backend/internal/server"
)

var version = "dev"
var buildTime = "unknown"
var gitCommit = "unknown"

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "--help", "-h":
			fmt.Printf("vote-server %s (built %s, commit %s)\n\nUsage: vote-server [--health]\n\nFlags:\n  --health     Probe the local /livez endpoint and exit (0=ok, 1=down)\n  --version    Print version and exit\n\nEnvironment variables:\n  PORT                       Listen port (default: 8080)\n  ALLOWED_ORIGINS            Comma-separated CORS origins (default: localhost origins)\n  TRUSTED_PROXIES            Comma-separated trusted proxy IPs\n  VALID_COLORS               Comma-separated allowed vote colors\n  VOTE_DASHBOARD_SECRET      Enables /dashboard when set (unset = disabled)\n  VOTE_DASHBOARD_MAX_AGE     Dashboard cookie lifetime (default: 168h)\n  VOTE_DATA_DIR              Persistent stats dir (default: ./data, Docker: /var/lib/vote)\n  VOTE_STATS_INTERVAL        Stats disk-flush interval (default: 5m)\n  VOTE_MAX_SESSIONS_PER_HOUR Per-IP session creation cap (default: 20)\n", version, buildTime, gitCommit)
			return
		case "--version", "-v":
			fmt.Printf("vote-server %s (built %s, commit %s)\n", version, buildTime, gitCommit)
			return
		case "--health":
			os.Exit(runHealthCheck())
		}
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	cfg := config.LoadConfig()

	h := hub.NewHub(cfg)
	h.Run()

	srv := server.NewServer(cfg, h)
	srv.SetBuildInfo(version, buildTime, gitCommit)

	// Open the persistent stats store (FHS data dir). Failures are non-fatal:
	// the server runs without persistence and counters reset on restart, as
	// before this feature existed.
	if err := srv.EnablePersistence(); err != nil {
		slog.Warn("Stats persistence disabled (server continues without it)", "error", err, "data_dir", cfg.DataDir)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		if err := srv.Run(); err != nil {
			errCh <- err
		}
	}()

	slog.Info("Server started", "port", cfg.Port, "version", version, "git_commit", gitCommit)

	select {
	case <-ctx.Done():
		slog.Info("Shutting down...")
	case err := <-errCh:
		slog.Error("Server error, shutting down", "error", err)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	h.Shutdown()

	// Flush the final counter checkpoint so the next boot restores to exactly
	// here, not the last periodic sample.
	srv.FlushStats()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("Server shutdown error", "error", err)
	}
	srv.CloseStore()

	slog.Info("Server stopped")
}

// runHealthCheck performs an in-process liveness probe against the local
// /livez endpoint and returns the process exit code (0 = ok, 1 = down).
//
// It exists so the distroless runtime image (D10) — which ships no shell,
// wget, or curl — can still run a Docker HEALTHCHECK by invoking the
// server binary itself (`vote-server --health`). The probe always targets
// loopback so it works regardless of HOST binding, honours the same PORT
// env var the server listens on, and uses a tight timeout because the
// orchestrator's own retry cadence handles transient blips.
func runHealthCheck() int {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	const timeout = 3 * time.Second
	client := &http.Client{Timeout: timeout}
	resp, err := client.Get("http://127.0.0.1:" + port + "/livez")
	if err != nil {
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}
