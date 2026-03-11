package media

import (
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// BufferConfig holds operator-tunable buffer depths loaded from environment
// variables. Defaults match ADR-002 specifications.
type BufferConfig struct {
	AudioFrames int // default 200
	VideoFrames int // default 10
}

// ── Audio Buffer ──────────────────────────────────────────────────────────────

// audioLowWater and audioCritical are the warning thresholds defined in ADR-002.
const (
	audioLowWater = 50 // frames remaining → warn
	audioCritical = 10 // frames remaining → error
	videoLowWater = 3  // frames remaining → warn
)

// AudioBuffer is a fixed-capacity ring buffer for AudioFrames. (ADR-002, US-M03)
//
// Underrun policy: emit a silence frame; increment UnderrunTotal.
// Overrun policy:  discard the oldest frame; increment OverrunTotal.
// Thread-safe.
type AudioBuffer struct {
	mu           sync.Mutex
	frames       []AudioFrame
	head, tail   int
	count        int
	cap          int
	log          *slog.Logger
	UnderrunTotal atomic.Int64
	OverrunTotal  atomic.Int64
}

// NewAudioBuffer constructs an AudioBuffer with the given capacity.
// capacity should be AUDIO_BUFFER_FRAMES (default 200). (ADR-003)
func NewAudioBuffer(capacity int, log *slog.Logger) *AudioBuffer {
	return &AudioBuffer{
		frames: make([]AudioFrame, capacity),
		cap:    capacity,
		log:    log,
	}
}

// Push adds a frame to the buffer. If the buffer is full, the oldest frame is
// discarded (overrun policy). Safe to call from pkg/renderer goroutine.
func (b *AudioBuffer) Push(f AudioFrame) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.count == b.cap {
		// Overrun: advance head to discard oldest frame.
		b.head = (b.head + 1) % b.cap
		b.count--
		b.OverrunTotal.Add(1)
		b.log.Warn("audio buffer overrun",
			slog.String("component", "renderer"),
			slog.Int("capacity", b.cap),
		)
	}

	b.frames[b.tail] = f
	b.tail = (b.tail + 1) % b.cap
	b.count++

	remaining := b.count
	switch {
	case remaining <= audioCritical:
		b.log.Error("audio buffer critical",
			slog.String("component", "bot"),
			slog.Int("depth", remaining),
		)
	case remaining <= audioLowWater:
		b.log.Warn("audio buffer low",
			slog.String("component", "bot"),
			slog.Int("depth", remaining),
		)
	}
}

// Pop removes and returns the next frame. If the buffer is empty, it returns a
// silence frame (underrun policy) and increments UnderrunTotal.
// Safe to call from pkg/bot goroutine.
func (b *AudioBuffer) Pop(wallClock time.Duration) AudioFrame {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.count == 0 {
		b.UnderrunTotal.Add(1)
		b.log.Warn("audio buffer underrun — emitting silence",
			slog.String("component", "bot"),
		)
		return SilenceFrame(wallClock)
	}

	f := b.frames[b.head]
	b.head = (b.head + 1) % b.cap
	b.count--
	return f
}

// Depth returns the number of frames currently in the buffer.
func (b *AudioBuffer) Depth() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.count
}

// Flush discards all frames. Called on source switch. (ADR-002, US-R05)
func (b *AudioBuffer) Flush() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.head, b.tail, b.count = 0, 0, 0
}

// ── Video Buffer ──────────────────────────────────────────────────────────────

// VideoBuffer is a fixed-capacity FIFO queue for VideoFrames. (ADR-002, US-M04)
//
// Underrun policy: repeat the last delivered frame; increment UnderrunTotal.
// Overrun policy:  discard the oldest frame; increment OverrunTotal.
// Thread-safe.
type VideoBuffer struct {
	mu            sync.Mutex
	frames        []VideoFrame
	head, tail    int
	count         int
	cap           int
	lastFrame     *VideoFrame
	log           *slog.Logger
	UnderrunTotal atomic.Int64
	OverrunTotal  atomic.Int64
}

// NewVideoBuffer constructs a VideoBuffer with the given capacity.
// capacity should be VIDEO_BUFFER_FRAMES (default 10). (ADR-003)
func NewVideoBuffer(capacity int, log *slog.Logger) *VideoBuffer {
	return &VideoBuffer{
		frames: make([]VideoFrame, capacity),
		cap:    capacity,
		log:    log,
	}
}

// Push adds a frame to the buffer. If the buffer is full, the oldest frame is
// discarded (overrun policy). Safe to call from pkg/renderer goroutine.
func (b *VideoBuffer) Push(f VideoFrame) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.count == b.cap {
		b.head = (b.head + 1) % b.cap
		b.count--
		b.OverrunTotal.Add(1)
		b.log.Warn("video buffer overrun",
			slog.String("component", "renderer"),
			slog.Int("capacity", b.cap),
		)
	}

	b.frames[b.tail] = f
	b.tail = (b.tail + 1) % b.cap
	b.count++

	if b.count <= videoLowWater {
		b.log.Warn("video buffer low",
			slog.String("component", "bot"),
			slog.Int("depth", b.count),
		)
	}
}

// Pop removes and returns the next frame. If the buffer is empty and a
// previous frame exists, it repeats the last frame (underrun policy).
// Safe to call from pkg/bot goroutine.
func (b *VideoBuffer) Pop() (VideoFrame, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.count == 0 {
		if b.lastFrame != nil {
			b.UnderrunTotal.Add(1)
			b.log.Warn("video buffer underrun — repeating last frame",
				slog.String("component", "bot"),
			)
			return *b.lastFrame, true
		}
		return VideoFrame{}, false
	}

	f := b.frames[b.head]
	b.head = (b.head + 1) % b.cap
	b.count--
	copy := f
	b.lastFrame = &copy
	return f, true
}

// Depth returns the number of frames currently in the buffer.
func (b *VideoBuffer) Depth() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.count
}

// Flush discards all frames and clears the last-frame reference.
// Called on source switch. (ADR-002, US-R05)
func (b *VideoBuffer) Flush() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.head, b.tail, b.count = 0, 0, 0
	b.lastFrame = nil
}
