package renderer

import (
	"context"
	"fmt"

	"github.com/mkoelle/stenosaur/pkg/media"
)

// FileDecoder decodes audio or video files using FFmpeg and normalizes output
// to the ADR-002 pipeline format. (US-R02)
//
//   - Audio: resampled to 48 kHz mono 16-bit PCM if source differs
//   - Video: decoded to YUV420p and scaled to 1280×720 if source differs
type FileDecoder struct {
	path string
	log  interface{ Error(string, ...any) }
	// TODO(US-R02): add ffmpeg process or binding handle
}

// NewFileDecoder constructs a FileDecoder for the given file path.
// The FFmpeg process is not started until the first Read call.
func NewFileDecoder(path string, r *Renderer) *FileDecoder {
	return &FileDecoder{path: path, log: r.log}
}

// ReadAudio implements media.AudioSource. (US-R02)
// TODO(US-R02): decode audio via FFmpeg; resample to 48 kHz mono PCM.
func (f *FileDecoder) ReadAudio(_ context.Context) (media.AudioFrame, error) {
	return media.AudioFrame{}, fmt.Errorf("FileDecoder.ReadAudio: not yet implemented (US-R02)")
}

// ReadVideo implements media.VideoSource. (US-R02)
// TODO(US-R02): decode video via FFmpeg; scale to 1280×720 YUV420p.
func (f *FileDecoder) ReadVideo(_ context.Context) (media.VideoFrame, error) {
	return media.VideoFrame{}, fmt.Errorf("FileDecoder.ReadVideo: not yet implemented (US-R02)")
}

// Close terminates the FFmpeg process and releases file handles.
func (f *FileDecoder) Close() error {
	// TODO(US-R02): terminate ffmpeg process
	return nil
}
