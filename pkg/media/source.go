package media

import "context"

// AudioSource is the interface implemented by all audio-producing components
// in pkg/renderer. pkg/bot consumes AudioSource via AudioBuffer. (ADR-001, ADR-002)
//
// Implementations: renderer.WebRenderer, renderer.FileDecoder, renderer.AudioSynth.
type AudioSource interface {
	// ReadAudio blocks until the next AudioFrame is available or ctx is done.
	// Returns the frame and nil on success.
	// Returns a zero AudioFrame and a non-nil error when the source is exhausted
	// or the context is cancelled.
	ReadAudio(ctx context.Context) (AudioFrame, error)

	// Close releases resources held by the source. Called once when the source
	// is no longer needed. Implementations must be safe to call after context
	// cancellation.
	Close() error
}

// VideoSource is the interface implemented by all video-producing components
// in pkg/renderer. pkg/bot consumes VideoSource via VideoBuffer. (ADR-001, ADR-002)
//
// Implementations: renderer.WebRenderer, renderer.FileDecoder.
// renderer.AudioSynth satisfies AudioSource only; it pairs with a static
// SMPTE color bar VideoSource for audio-only sources.
type VideoSource interface {
	// ReadVideo blocks until the next VideoFrame is available or ctx is done.
	// Returns the frame and nil on success.
	// Returns a zero VideoFrame and a non-nil error when the source is exhausted
	// or the context is cancelled.
	ReadVideo(ctx context.Context) (VideoFrame, error)

	// Close releases resources held by the source.
	Close() error
}
