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
			fmt.Printf("vote-server %s (built %s)\n\nUsage: vote-server\n\nEnvironment variables:\n  PORT             Listen port (default: 8080)\n  ALLOWED_ORIGINS  Comma-separated CORS origins (default: localhost origins)\n", version, buildTime)
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

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		if err := srv.Run(); err != nil {
			slog.Error("Server start error", "error", err)
			os.Exit(1)
		}
	}()

	slog.Info("Server started", "port", cfg.Port, "version", version)

	<-ctx.Done()
	slog.Info("Shutting down...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	h.Shutdown()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("Server shutdown error", "error", err)
	}

	slog.Info("Server stopped")
}
