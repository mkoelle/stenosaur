// Package main is the binary entrypoint for stenosaur.
//
// Responsibilities (US-D08):
//   - Validate all configuration from environment variables before any Zoom
//     connection is attempted
//   - Wire pkg/media, pkg/renderer, pkg/bot, and pkg/controller together
//   - Start the HTTP server and block until SIGTERM/SIGINT
//
// If any startup validation fails, the process exits with a non-zero code and
// a specific, actionable error message. No partial startup. (ADR-003)
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

	"github.com/mkoelle/stenosaur/pkg/bot"
	"github.com/mkoelle/stenosaur/pkg/controller"
	"github.com/mkoelle/stenosaur/pkg/media"
	"github.com/mkoelle/stenosaur/pkg/renderer"
)

func main() {
	// Structured logger shared across all packages (ADR-005, ADR-006).
	// Component tag for cmd-level messages is "controller" (closest fit).
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel(),
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			// Rename "time" → "ts" and "msg" → "msg" per ADR-005 schema.
			if a.Key == slog.TimeKey {
				a.Key = "ts"
			}
			return a
		},
	}))

	cfg, err := loadConfig()
	if err != nil {
		// Validation errors go to stderr as plain text for operator readability.
		fmt.Fprintf(os.Stderr, "startup error: %v\n", err)
		os.Exit(1)
	}

	// Buffer configuration (ADR-002).
	bufCfg := media.BufferConfig{
		AudioFrames: cfg.AudioBufferFrames,
		VideoFrames: cfg.VideoBufferFrames,
	}

	audioBuffer := media.NewAudioBuffer(bufCfg.AudioFrames, log)
	videoBuffer := media.NewVideoBuffer(bufCfg.VideoFrames, log)

	rend := renderer.New(log)
	_ = rend // wired to buffers in a future story

	zoomClient, err := bot.NewPWAClient(log)
	if err != nil {
		fmt.Fprintf(os.Stderr, "startup error: failed to initialize Playwright: %v\n", err)
		os.Exit(1)
	}

	ctrl := controller.New(cfg.AdminToken, cfg.AdminPort, zoomClient, audioBuffer, videoBuffer, log)

	// Graceful shutdown on SIGTERM or SIGINT.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()

	log.Info("stenosaur starting",
		slog.String("component", "controller"),
		slog.String("admin_port", cfg.AdminPort),
	)

	if err := ctrl.Start(ctx); err != nil && err != http.ErrServerClosed {
		log.Error("server error",
			slog.String("component", "controller"),
			slog.String("err", err.Error()),
		)
		os.Exit(1)
	}

	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := ctrl.Shutdown(shutdownCtx); err != nil {
		log.Error("graceful shutdown failed",
			slog.String("component", "controller"),
			slog.String("err", err.Error()),
		)
	}

	log.Info("stenosaur stopped", slog.String("component", "controller"))
}

// logLevel reads LOG_LEVEL from the environment and returns the corresponding
// slog.Level. Defaults to Info. (ADR-003)
func logLevel() slog.Level {
	switch os.Getenv("LOG_LEVEL") {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
