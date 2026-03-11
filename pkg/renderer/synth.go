package renderer

import (
	"context"
	"fmt"
	"math"
	"sync/atomic"
	"time"

	"github.com/mkoelle/stenosaur/pkg/media"
)

// AudioSynth synthesizes audio programmatically. (ADR-002, US-R03)
//
// Default output: 440 Hz sine wave at 48 kHz mono — a built-in test source
// that requires no external file. Pairs with SMPTESource for video output
// when no video source is configured. (US-R06)
type AudioSynth struct {
	freq      float64 // Hz; default 440
	sampleIdx atomic.Int64
	startTime time.Time
}

// NewAudioSynth constructs an AudioSynth generating the given frequency.
// Use 440.0 for the standard test tone.
func NewAudioSynth(freqHz float64) *AudioSynth {
	return &AudioSynth{freq: freqHz, startTime: time.Now()}
}

// ReadAudio implements media.AudioSource. Generates the next 480-sample frame
// of a sine wave at the configured frequency. Never blocks. (US-R03)
func (s *AudioSynth) ReadAudio(_ context.Context) (media.AudioFrame, error) {
	idx := s.sampleIdx.Load()
	var frame media.AudioFrame
	frame.PTS = time.Duration(idx) * time.Second / media.AudioSampleRate

	for i := range frame.Samples {
		t := float64(idx+int64(i)) / float64(media.AudioSampleRate)
		// Scale sine to int16 range at ~50% amplitude to avoid clipping.
		frame.Samples[i] = int16(math.Sin(2*math.Pi*s.freq*t) * 16383)
	}

	s.sampleIdx.Add(media.AudioFrameSamples)
	return frame, nil
}

// Close is a no-op for AudioSynth (no resources to release).
func (s *AudioSynth) Close() error { return nil }

// ── SMPTE Color Bar Source ────────────────────────────────────────────────────

// SMPTESource is a static VideoSource that emits a SMPTE color bar frame.
// Used when only an audio source is active (US-R06).
//
// The frame is generated once and repeated indefinitely.
type SMPTESource struct {
	frame media.VideoFrame
	start time.Time
	idx   atomic.Int64
}

// NewSMPTESource constructs a SMPTESource and pre-renders the color bar frame.
func NewSMPTESource() (*SMPTESource, error) {
	frame, err := renderSMPTEBars()
	if err != nil {
		return nil, fmt.Errorf("SMPTE color bar render: %w", err)
	}
	return &SMPTESource{frame: frame, start: time.Now()}, nil
}

// ReadVideo implements media.VideoSource. Returns the static SMPTE frame with
// a monotonically increasing PTS at 30 FPS. (US-R06)
func (s *SMPTESource) ReadVideo(_ context.Context) (media.VideoFrame, error) {
	i := s.idx.Add(1) - 1
	f := s.frame
	f.PTS = time.Duration(i) * time.Second / media.VideoFPS
	return f, nil
}

// Close is a no-op for SMPTESource.
func (s *SMPTESource) Close() error { return nil }

// renderSMPTEBars generates a 1280×720 YUV420p SMPTE color bar frame.
// The 7 standard bars are: grey, yellow, cyan, green, magenta, red, blue.
// TODO(US-R06): replace placeholder with full YUV420p SMPTE bar generation.
func renderSMPTEBars() (media.VideoFrame, error) {
	f := media.NewVideoFrame(0)

	// Placeholder: fill with grey (Y=128, Cb=128, Cr=128).
	// Full SMPTE bar implementation is tracked as US-R06.
	for i := range f.Y {
		f.Y[i] = 128
	}
	for i := range f.Cb {
		f.Cb[i] = 128
		f.Cr[i] = 128
	}

	return f, nil
}
