package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"vote-backend/internal/config"
	"vote-backend/internal/hub"
	"vote-backend/internal/server"
)

var version = "dev"
var buildTime = "unknown"

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "--help", "-h":
			fmt.Printf("vote-server %s (built %s)\n\nUsage: vote-server\n\nEnvironment variables:\n  PORT                       Listen port (default: 8080)\n  ALLOWED_ORIGINS            Comma-separated CORS origins (default: localhost origins)\n  TRUSTED_PROXIES            Comma-separated trusted proxy IPs\n  VALID_COLORS               Comma-separated allowed vote colors\n  VOTE_DASHBOARD_SECRET      Enables /dashboard when set (unset = disabled)\n  VOTE_DASHBOARD_MAX_AGE     Dashboard cookie lifetime (default: 168h)\n  VOTE_DATA_DIR              Persistent stats dir (default: ./data, Docker: /var/lib/vote)\n  VOTE_STATS_INTERVAL        Stats disk-flush interval (default: 5m)\n  VOTE_MAX_SESSIONS_PER_HOUR Per-IP session creation cap (default: 20)\n", version, buildTime)
			return
		case "--version", "-v":
			fmt.Printf("vote-server %s (built %s)\n", version, buildTime)
			return
		}
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	cfg := config.LoadConfig()

	h := hub.NewHub(cfg)
	go h.Run()

	srv := server.NewServer(cfg, h)
	srv.SetBuildInfo(version, buildTime)

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

	slog.Info("Server started", "port", cfg.Port, "version", version)

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
