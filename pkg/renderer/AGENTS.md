# pkg/renderer — Agent Instructions

@../../AGENTS.md

## Package Role

`pkg/renderer` produces `media.AudioFrame` and `media.VideoFrame` values from three source types and writes them into the shared buffers in `pkg/media`. It has no knowledge of the Zoom session.

Allowed imports: standard library, `pkg/media`, third-party (FFmpeg bindings, Playwright-go for WebRenderer).
Must not import: `pkg/bot`, `pkg/controller`.

## Source Types

| Type | File | Produces |
|---|---|---|
| `WebRenderer` | `web.go` | Audio + Video from a live webpage rendered in Chromium |
| `FileDecoder` | `file.go` | Audio + Video decoded from a local file via FFmpeg |
| `AudioSynth` | `synth.go` | Audio only (pairs with SMPTE color bar for video) |

All three implement `media.AudioSource` and/or `media.VideoSource`.

## Normalization Rules (ADR-002)

Every source **must** normalize output to these values before writing frames. These are non-negotiable — they are set by Chromium's injection boundary.

- Audio: 48,000 Hz · mono · 16-bit signed PCM · 480 samples per frame
- Video: 1280 × 720 · YUV 4:2:0 planar · `.y4m` · tag `C420`

If a source cannot sustain 30 FPS video, **repeat the last frame** — do not reduce the frame rate or emit a black frame.

## PTS Rules (ADR-002)

- Set `PTS` on every frame at the moment of production using a monotonic clock.
- On source switch: reset the PTS origin to zero **and** call `Flush()` on both buffers before resuming production. See `US-R05`.

## SMPTE Color Bar (US-R06)

When only an audio source is active (`AudioSynth` with no video source), `pkg/renderer` must supply a static SMPTE color bar `VideoFrame` (1280 × 720, YUV420p). Implement this in `synth.go` alongside `AudioSynth`.

## Do Not

- Return frames in any format other than the normalized values above
- Reduce video frame rate — always repeat the last frame on source lag
- Import `pkg/bot` or `pkg/controller`
- Block indefinitely without respecting the `context.Context` passed to `ReadAudio`/`ReadVideo`
