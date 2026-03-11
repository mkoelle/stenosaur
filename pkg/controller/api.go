package controller

import (
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
)

// ── Auth middleware ───────────────────────────────────────────────────────────

// auth wraps a handler with Bearer token authentication. (ADR-005, US-C05)
// Tokens are compared using constant-time comparison to prevent timing attacks.
func (c *Controller) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		// subtle.ConstantTimeCompare requires equal-length inputs.
		// We compare hashed lengths or pad — here we use XOR-based compare
		// which handles unequal lengths safely. (ADR-005)
		if subtle.ConstantTimeCompare([]byte(token), []byte(c.adminToken)) != 1 {
			c.log.Warn("unauthorized request",
				slog.String("component", "controller"),
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
			)
			writeJSON(w, http.StatusUnauthorized, map[string]string{
				"error":   "unauthorized",
				"message": "valid Bearer token required",
			})
			return
		}
		next(w, r)
	}
}

// ── Health endpoints (no auth) ────────────────────────────────────────────────

// handleHealthz returns 200 whenever the HTTP server is responding. (ADR-005, US-C02)
func (c *Controller) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleReadyz checks all subsystems and returns 200 if all are running,
// 503 if any are not. (ADR-005, US-C03)
func (c *Controller) handleReadyz(w http.ResponseWriter, _ *http.Request) {
	checks := map[string]string{
		"zoom_session":   subsystemStatus(c.zoomClient.IsConnected()),
		"audio_pipeline": "running", // TODO(US-C03): check real pipeline state
		"video_pipeline": "running", // TODO(US-C03): check real pipeline state
		"chromium":       "running", // TODO(US-C03): check Chromium process liveness
	}

	allReady := true
	for _, v := range checks {
		if v != "running" && v != "connected" {
			allReady = false
			break
		}
	}

	status := "ready"
	code := http.StatusOK
	if !allReady {
		status = "degraded"
		code = http.StatusServiceUnavailable
	}

	writeJSON(w, code, map[string]any{
		"status": status,
		"checks": checks,
	})
}

// ── Authenticated endpoints ───────────────────────────────────────────────────

// handleStatus returns current session state and pipeline buffer metrics. (ADR-005, US-C04)
func (c *Controller) handleStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"session": map[string]any{
			"state": sessionState(c.zoomClient.IsConnected()),
		},
		"pipeline": map[string]any{
			"audio_buffer_depth":  c.audioBuffer.Depth(),
			"audio_underrun_total": c.audioBuffer.UnderrunTotal.Load(),
			"audio_overrun_total":  c.audioBuffer.OverrunTotal.Load(),
			"audio_drift_ms":      0, // TODO(US-M05): wire drift detection
			"video_buffer_depth":  c.videoBuffer.Depth(),
			"video_underrun_total": c.videoBuffer.UnderrunTotal.Load(),
			"video_overrun_total":  c.videoBuffer.OverrunTotal.Load(),
		},
	})
}

// handleStart joins the configured meeting and begins media injection. (US-C05)
// TODO(US-C05): read meeting URL and display name from config; call ZoomClient.Join.
func (c *Controller) handleStart(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "starting"})
}

// handleStop leaves the meeting. (US-C05)
func (c *Controller) handleStop(w http.ResponseWriter, r *http.Request) {
	if err := c.zoomClient.Leave(r.Context()); err != nil {
		c.log.Error("leave failed",
			slog.String("component", "controller"),
			slog.String("err", err.Error()),
		)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "stopped"})
}

// handleConfig updates meeting URL, display name, or media source. (US-C05)
// TODO(US-C05): parse and validate request body; apply to running session.
func (c *Controller) handleConfig(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "not yet implemented"})
}

// handleMedia accepts a media file upload. (US-C05)
// TODO(US-C05): stream upload to MEDIA_DIR; validate format.
func (c *Controller) handleMedia(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "not yet implemented"})
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func subsystemStatus(ok bool) string {
	if ok {
		return "connected"
	}
	return "stopped"
}

func sessionState(connected bool) string {
	if connected {
		return "streaming"
	}
	return "idle"
}
