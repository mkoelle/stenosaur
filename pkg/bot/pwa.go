package bot

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"

	"github.com/mkoelle/stenosaur/pkg/media"
)

// PWAClient implements ZoomClient using Playwright browser automation against
// the Zoom Web App (app.zoom.us). (ADR-004, US-B02)
//
// Media injection uses Chromium's fake media device flags:
//
//	--use-fake-device-for-media-stream
//	--use-file-for-fake-audio-capture=<path>
//	--use-file-for-fake-video-capture=<path>
//	--allow-file-access-from-files
//
// The PWAClient satisfies the ZoomClient interface. Callers must use the
// interface type — never reference PWAClient directly.
type PWAClient struct {
	log       *slog.Logger
	connected atomic.Bool
	// TODO(US-B02): add playwright.Playwright, playwright.Browser,
	//               playwright.BrowserContext, playwright.Page fields
	//               once playwright-go is wired into go.mod.
}

// NewPWAClient constructs a PWAClient. Playwright and Chromium are not
// launched until Join() is called.
func NewPWAClient(log *slog.Logger) (*PWAClient, error) {
	return &PWAClient{log: log}, nil
}

// Join implements ZoomClient.Join. (US-B02)
//
// Flow:
//  1. Launch headless Chromium with fake media device flags (US-B03)
//  2. Navigate to app.zoom.us/wc/join/<meeting_id>
//  3. Enter displayName in the name field
//  4. Handle audio/video permission prompts
//  5. Wait for meeting admission (including waiting room)
//
// Every Playwright selector used here must have a version comment. (ADR-004)
// Log a warn on any selector timeout — that is a breakage incident. (ADR-004)
func (c *PWAClient) Join(ctx context.Context, meetingURL, displayName string) error {
	// Per-goroutine panic recovery (ADR-001, US-B06).
	defer func() {
		if r := recover(); r != nil {
			c.log.Error("bot goroutine panic in Join",
				slog.String("component", "bot"),
				slog.Any("panic", r),
			)
		}
	}()

	// TODO(US-B02): implement Playwright join flow.
	// TODO(US-B03): configure Chromium fake device launch flags.
	_ = meetingURL
	_ = displayName
	return fmt.Errorf("PWAClient.Join: not yet implemented (US-B02)")
}

// StartMediaStream implements ZoomClient.StartMediaStream. (US-B03)
// TODO(US-B03): wire audio and video sources to fake device pipes.
func (c *PWAClient) StartMediaStream(_ context.Context, _ media.AudioSource, _ media.VideoSource) error {
	return fmt.Errorf("PWAClient.StartMediaStream: not yet implemented (US-B03)")
}

// StopMediaStream implements ZoomClient.StopMediaStream.
func (c *PWAClient) StopMediaStream() error {
	// TODO(US-B03): stop writing to fake device pipes.
	return nil
}

// IsConnected implements ZoomClient.IsConnected.
func (c *PWAClient) IsConnected() bool {
	return c.connected.Load()
}

// Leave implements ZoomClient.Leave.
// TODO(US-B02): click the leave button via Playwright before closing.
func (c *PWAClient) Leave(_ context.Context) error {
	c.connected.Store(false)
	return nil
}

// Close implements ZoomClient.Close. Releases all Playwright resources.
// TODO(US-B02): call playwright.Stop() and browser.Close().
func (c *PWAClient) Close() error {
	return nil
}
