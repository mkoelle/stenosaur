// Package renderer produces audio and video frames from configurable sources
// and writes them into the shared buffers in pkg/media.
//
// All output is normalized to the format contracts defined in ADR-002:
//   - Audio: 48 kHz, mono, 16-bit PCM, 480 samples/frame
//   - Video: 1280×720, YUV 4:2:0 planar (.y4m, tag C420), 30 FPS
//
// Source types:
//   - WebRenderer  — live webpage rendered in a Chromium browser context
//   - FileDecoder  — audio/video file decoded via FFmpeg
//   - AudioSynth   — programmatic audio (440 Hz test tone; SMPTE bars for video)
//
// pkg/renderer must not import pkg/bot or pkg/controller.
package renderer

import "log/slog"

// Renderer holds shared resources used across all source types — primarily the
// shared Chromium instance managed by playwright-go. (ADR-001, ADR-006)
//
// One Chromium process is shared between pkg/bot (Zoom session context) and
// WebRenderer (source webpage context). Renderer manages the source context.
type Renderer struct {
	log *slog.Logger
	// playwright and browser are initialized lazily when WebRenderer is first used.
	// TODO(US-R01): add playwright.Playwright and playwright.Browser fields here
	//               once playwright-go dependency is wired in go.mod.
}

// New constructs a Renderer. Chromium is not launched until WebRenderer.Start()
// is called.
func New(log *slog.Logger) *Renderer {
	return &Renderer{log: log}
}
