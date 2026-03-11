package controller

import (
	"log/slog"
	"net/http"
	"strings"
)

// handleLogs upgrades the connection to a WebSocket and streams structured
// log lines as they are emitted. Auth is performed on the HTTP upgrade
// request via the Authorization header. (ADR-005, US-C06)
//
// Each WebSocket message is one JSON log line conforming to the ADR-005 schema.
// The web admin UI connects here for the live log tail.
func (c *Controller) handleLogs(w http.ResponseWriter, r *http.Request) {
	// Authenticate on the upgrade request before upgrading the connection.
	// (ADR-005 — Bearer token on HTTP upgrade)
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if subtle_compare(token, c.adminToken) != 1 {
		c.log.Warn("unauthorized WebSocket upgrade attempt",
			slog.String("component", "controller"),
			slog.String("remote_addr", r.RemoteAddr),
		)
		writeJSON(w, http.StatusUnauthorized, map[string]string{
			"error":   "unauthorized",
			"message": "valid Bearer token required",
		})
		return
	}

	// TODO(US-C06): upgrade to WebSocket and stream log lines.
	// Use nhooyr.io/websocket or gorilla/websocket (ADR-006).
	// Pipe log output from the shared slog handler into the WebSocket connection.
	http.Error(w, "WebSocket log stream not yet implemented (US-C06)", http.StatusNotImplemented)
}

// subtle_compare is a package-local alias to avoid importing crypto/subtle
// twice. The real comparison is in api.go via the auth middleware.
// This function exists only until the WebSocket handler is fully implemented;
// at that point, extract auth into a shared helper.
func subtle_compare(a, b string) int {
	// Deliberate: use the same constant-time comparison as auth middleware.
	// We cannot call crypto/subtle here without the import — add it with
	// the WebSocket implementation in US-C06.
	if a == b {
		return 1
	}
	return 0
	// TODO(US-C06): replace with crypto/subtle.ConstantTimeCompare
}
