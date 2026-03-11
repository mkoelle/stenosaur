// Package media defines the shared frame types and source interfaces used
// across all stenosaur packages.
//
// Import rules (ADR-001):
//   - pkg/media must not import any other internal package.
//   - pkg/bot and pkg/controller may import pkg/media.
//   - pkg/renderer may import pkg/media only.
package media

import "time"

// AudioSampleRate is the fixed sample rate for all audio frames. (ADR-002)
// This value is determined by Chromium's WebRTC native rate and is not
// configurable without updating ADR-002.
const AudioSampleRate = 48_000

// AudioFrameSamples is the fixed number of samples per audio frame. (ADR-002)
// 480 samples at 48 kHz = 10 ms, the standard WebRTC processing block size.
const AudioFrameSamples = 480

// VideoWidth and VideoHeight are the fixed video frame dimensions. (ADR-002)
const (
	VideoWidth  = 1280
	VideoHeight = 720
)

// VideoFPS is the fixed target frame rate for all video frames. (ADR-002)
const VideoFPS = 30

// AudioFrame is a single 10 ms block of audio in the pipeline format
// required by Chromium's fake audio capture device. (ADR-002)
//
// Format: 48 kHz, mono, 16-bit signed little-endian PCM.
// Samples always contains exactly AudioFrameSamples (480) values.
type AudioFrame struct {
	// Samples contains exactly 480 int16 PCM samples (10 ms at 48 kHz mono).
	Samples [AudioFrameSamples]int16

	// PTS is the presentation timestamp set by pkg/renderer at production time.
	// pkg/bot compares PTS against wall clock to detect drift. (ADR-002)
	PTS time.Duration
}

// SilenceFrame returns an AudioFrame with zero samples and the given PTS.
// Used by AudioBuffer on underrun to avoid stalling Chromium's fake device.
func SilenceFrame(pts time.Duration) AudioFrame {
	return AudioFrame{PTS: pts}
}

// VideoFrame is a single YUV 4:2:0 planar video frame in the pipeline format
// required by Chromium's fake video capture device. (ADR-002)
//
// Format: yuv420p, 1280×720, .y4m pixel format tag C420, BT.601 limited range.
// The three planes are sized for 4:2:0 chroma subsampling:
//
//	Y:  VideoWidth * VideoHeight bytes        (1 byte per luma sample)
//	Cb: (VideoWidth/2) * (VideoHeight/2) bytes
//	Cr: (VideoWidth/2) * (VideoHeight/2) bytes
type VideoFrame struct {
	// Y is the luma plane: VideoWidth × VideoHeight bytes.
	Y []byte
	// Cb is the blue-difference chroma plane: (VideoWidth/2) × (VideoHeight/2) bytes.
	Cb []byte
	// Cr is the red-difference chroma plane: (VideoWidth/2) × (VideoHeight/2) bytes.
	Cr []byte

	Width  int
	Height int

	// PTS is the presentation timestamp set by pkg/renderer at production time.
	PTS time.Duration
}

// NewVideoFrame allocates a VideoFrame with correctly sized planes for the
// fixed 1280×720 resolution. Callers fill the planes after construction.
func NewVideoFrame(pts time.Duration) VideoFrame {
	lumaSize := VideoWidth * VideoHeight
	chromaSize := (VideoWidth / 2) * (VideoHeight / 2)
	return VideoFrame{
		Y:      make([]byte, lumaSize),
		Cb:     make([]byte, chromaSize),
		Cr:     make([]byte, chromaSize),
		Width:  VideoWidth,
		Height: VideoHeight,
		PTS:    pts,
	}
}
