// Package bot owns the Zoom session lifecycle.
//
// It implements the ZoomClient interface using Playwright PWA automation
// (ADR-004) and consumes pkg/media buffers to inject audio and video into
// the Zoom Web App via Chromium's fake media device layer.
//
// The ZoomClient interface is the swap point for a future Zoom Meeting SDK
// implementation (ADR-004). All callers must use the interface, never the
// concrete PWAClient type.
//
// Allowed imports: stdlib, pkg/media, playwright-go, third-party.
// Must not import: pkg/renderer, pkg/controller.
package bot

import (
	"context"

	"github.com/mkoelle/stenosaur/pkg/media"
)

// ZoomClient is the interface that all Zoom session implementations must satisfy.
// The PWA implementation (PWAClient) satisfies this interface.
// A future Zoom Meeting SDK implementation will satisfy it without changing callers.
// (ADR-004, US-B01)
type ZoomClient interface {
	// Join navigates to the meeting URL and enters as a named guest participant.
	// Handles waiting room admission. Blocks until the bot is admitted or ctx
	// is cancelled.
	Join(ctx context.Context, meetingURL, displayName string) error

	// StartMediaStream begins consuming frames from the provided sources and
	// injecting them into the Zoom session via Chromium's fake device layer.
	StartMediaStream(ctx context.Context, audio media.AudioSource, video media.VideoSource) error

	// StopMediaStream halts media injection without leaving the meeting.
	StopMediaStream() error

	// IsConnected reports whether the bot is currently in an active session.
	IsConnected() bool

	// Leave exits the meeting and releases the browser session.
	Leave(ctx context.Context) error

	// Close releases all Playwright and Chromium resources.
	// Should be called once when the bot is done for the lifetime of the process.
	Close() error
}
