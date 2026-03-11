# pkg/media — Agent Instructions

@../../AGENTS.md

## Package Role

`pkg/media` defines the shared frame types and source interfaces used across all packages. It is the **dependency floor** of the project: it must never import any other internal package (`pkg/bot`, `pkg/renderer`, `pkg/controller`).

Violating the import rule creates a circular dependency that breaks the build. Check `go list -deps ./pkg/media/...` after any change to confirm no internal packages are pulled in.

## What Lives Here

| File | Contents |
|---|---|
| `frame.go` | `AudioFrame` and `VideoFrame` struct definitions |
| `source.go` | `AudioSource` and `VideoSource` interfaces |
| `buffer.go` | `AudioBuffer` (ring) and `VideoBuffer` (FIFO) implementations and constructors |

## Frame Format Invariants (ADR-002)

These are not configurable. Do not add parameters that change them.

- `AudioFrame.Samples` is always 480 `int16` values (10 ms at 48 kHz mono)
- `VideoFrame` is always 1280 × 720 YUV 4:2:0 planar (`yuv420p`)
- Every frame carries a `PTS time.Duration` set at production time

## Buffer Rules (ADR-002)

**AudioBuffer (ring buffer):**
- Capacity: configurable, default 200 frames
- Underrun: emit silence frame, increment `UnderrunTotal`
- Overrun: discard oldest, increment `OverrunTotal`
- Emit `warn` log at 50 frames remaining; `error` log at 10 frames

**VideoBuffer (FIFO queue):**
- Capacity: configurable, default 10 frames
- Underrun: repeat last frame, increment `UnderrunTotal`
- Overrun: discard oldest, increment `OverrunTotal`
- Emit `warn` log at 3 frames remaining

## Do Not

- Import `pkg/bot`, `pkg/renderer`, or `pkg/controller`
- Add business logic — this package is types and buffers only
- Change `AudioFrame` sample count or `VideoFrame` dimensions without updating ADR-002
