package renderer

import (
	"context"
	"fmt"

	"github.com/mkoelle/stenosaur/pkg/media"
)

// WebRenderer renders a configured URL in a Chromium browser context and
// captures frames at 30 FPS as YUV420p 1280×720. (ADR-002, US-R01)
//
// It opens a browser context separate from the Zoom session context so that
// one Chromium instance serves both purposes. (ADR-001)
type WebRenderer struct {
	url string
	log interface{ Error(string, ...any) } // accepts *slog.Logger
	// TODO(US-R01): add playwright BrowserContext field
}

// NewWebRenderer constructs a WebRenderer for the given URL.
// The browser context is not opened until Start() is called.
func NewWebRenderer(url string, r *Renderer) *WebRenderer {
	return &WebRenderer{url: url, log: r.log}
}

// ReadAudio implements media.AudioSource. (US-R01)
// TODO(US-R01): implement Web Audio API capture at 48 kHz mono.
func (w *WebRenderer) ReadAudio(_ context.Context) (media.AudioFrame, error) {
	return media.AudioFrame{}, fmt.Errorf("WebRenderer.ReadAudio: not yet implemented (US-R01)")
}

// ReadVideo implements media.VideoSource. (US-R01)
// TODO(US-R01): implement frame capture as YUV420p at 30 FPS.
func (w *WebRenderer) ReadVideo(_ context.Context) (media.VideoFrame, error) {
	return media.VideoFrame{}, fmt.Errorf("WebRenderer.ReadVideo: not yet implemented (US-R01)")
}

// Close releases the browser context and any associated resources.
func (w *WebRenderer) Close() error {
	// TODO(US-R01): close the playwright BrowserContext
	return nil
}
