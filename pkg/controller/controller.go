// Package controller implements the HTTP server, REST API, WebSocket log stream,
// and web admin UI for stenosaur.
//
// It holds no business logic — it reads state from pkg/bot and pkg/media and
// dispatches commands. All HTML rendering is handled by pkg/controller/ui via
// Templ templates. (ADR-005, ADR-007)
//
// Allowed imports: stdlib, pkg/media, pkg/bot, third-party HTTP/WebSocket.
// Must not import: pkg/renderer.
package controller

import (
	"context"
	"log/slog"
	"net"
	"net/http"

	"github.com/mkoelle/stenosaur/pkg/bot"
	"github.com/mkoelle/stenosaur/pkg/media"
)

// Controller wires the HTTP server and all registered handlers.
type Controller struct {
	adminToken  string
	adminPort   string
	zoomClient  bot.ZoomClient
	audioBuffer *media.AudioBuffer
	videoBuffer *media.VideoBuffer
	log         *slog.Logger
	server      *http.Server
}

// New constructs a Controller. The HTTP server is not started until Start() is
// called.
func New(
	adminToken string,
	adminPort string,
	zoom bot.ZoomClient,
	audio *media.AudioBuffer,
	video *media.VideoBuffer,
	log *slog.Logger,
) *Controller {
	c := &Controller{
		adminToken:  adminToken,
		adminPort:   adminPort,
		zoomClient:  zoom,
		audioBuffer: audio,
		videoBuffer: video,
		log:         log,
	}

	mux := http.NewServeMux()
	c.registerRoutes(mux)

	c.server = &http.Server{
		Addr:    net.JoinHostPort("", adminPort),
		Handler: mux,
	}

	return c
}

// registerRoutes mounts all endpoints defined in ADR-005 onto mux.
func (c *Controller) registerRoutes(mux *http.ServeMux) {
	// Unauthenticated health endpoints (ADR-005).
	mux.HandleFunc("GET /healthz", c.handleHealthz)
	mux.HandleFunc("GET /readyz", c.handleReadyz)

	// Authenticated REST endpoints (ADR-005).
	mux.HandleFunc("GET /status", c.auth(c.handleStatus))
	mux.HandleFunc("POST /start", c.auth(c.handleStart))
	mux.HandleFunc("POST /stop", c.auth(c.handleStop))
	mux.HandleFunc("PATCH /config", c.auth(c.handleConfig))
	mux.HandleFunc("POST /media", c.auth(c.handleMedia))

	// WebSocket log stream — auth handled on upgrade (ADR-005).
	mux.HandleFunc("GET /logs", c.handleLogs)

	// Static web admin UI assets (ADR-007).
	// TODO(US-C01): mount embedded static files and Templ-rendered UI routes.
}

// Start begins listening for HTTP connections. Blocks until the context is
// cancelled or a fatal server error occurs.
func (c *Controller) Start(ctx context.Context) error {
	c.log.Info("admin HTTP server starting",
		slog.String("component", "controller"),
		slog.String("addr", c.server.Addr),
	)
	go func() {
		<-ctx.Done()
		_ = c.server.Shutdown(context.Background())
	}()
	return c.server.ListenAndServe()
}

// Shutdown gracefully drains in-flight requests.
func (c *Controller) Shutdown(ctx context.Context) error {
	return c.server.Shutdown(ctx)
}
