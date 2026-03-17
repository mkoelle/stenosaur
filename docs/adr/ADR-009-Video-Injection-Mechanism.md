# ADR-009: Video Injection Mechanism

## Status
🟡 **Proposed**

---

## Context

ADR-002 defines the video format contract: YUV 4:2:0 planar (`.y4m`, pixel format tag `C420`), 1280×720, 30 FPS, BT.601 limited range, produced by `pkg/renderer` and buffered in a FIFO queue in `pkg/media`. ADR-002 does not specify how those frames are physically delivered from `pkg/bot` into Chromium's camera input once they leave the buffer.

This ADR concerns **Chromium A** — the Zoom session instance launched with fake media device flags (ADR-001, ADR-004). Chromium B (the renderer instance) runs without fake device flags and is not involved in video injection.

Unlike audio (ADR-008), video injection has a straightforward path: Chromium's `--use-file-for-fake-video-capture` flag **does** accept a named pipe (FIFO) as its argument and reads from it as a continuous stream. This asymmetry with the audio flag is non-obvious and worth stating explicitly as the basis for this decision.

---

## Constraint: Chromium's `.y4m` Pipe Behaviour

`--use-file-for-fake-video-capture=<path>` opens the path and reads `.y4m` frames from it sequentially. When the path is a named pipe (FIFO), Chromium blocks on `open()` until a writer connects, then reads frames continuously as they are written. This is the correct behaviour for a live stream.

Two details govern the wire format:

1. **Pixel format tag must be `C420`.** The `.y4m` header encodes the chroma subsampling scheme. Chromium's `libyuv` decoder accepts `C420` (standard 4:2:0). Tags `C420mpeg2` and `C420jpeg` use different chroma siting and cause colour shift or decoder errors. This is verified in US-T06.

2. **Frame rate in the `.y4m` header must match 30 FPS.** The header field `F30:1` tells Chromium the expected frame interval. A mismatch between the declared rate and the actual write rate causes Chromium's WebRTC layer to report frame timing anomalies to the Zoom session.

---

## Options Evaluated

### Option A: Named pipe (FIFO) ✅

A named pipe (`mkfifo`) is created at a known path inside the container at startup. Chromium is launched with:

```
--use-file-for-fake-video-capture=<fifo_path>
--use-fake-device-for-media-stream
--allow-file-access-from-files
```

`pkg/bot` opens the write end of the FIFO after Chromium has opened the read end (verified by the FIFO's `open()` unblocking), writes a single `.y4m` file header, then writes one YUV420p frame at a time from the video buffer at 30 FPS.

Unlike the audio FIFO case (ADR-008 Option A), video does not have a looping problem — Chromium reads the pipe as a continuous stream without attempting to replay it. The pipe buffer is large enough (64 kB kernel default, ~2 raw frames) to absorb minor timing jitter between the Go writer and Chromium's reader. The video buffer's underrun policy (repeat last frame) ensures the pipe always has data available at the 30 FPS cadence.

**Verdict: Selected.**

---

### Option B: Virtual V4L2 device (v4l2loopback)

A kernel module (`v4l2loopback`) creates a virtual `/dev/videoN` device on the host. Chromium uses it as a real camera. `pkg/bot` writes frames to the device file.

`v4l2loopback` requires a host kernel module load. This violates ADR-003's constraint that the container must have no host kernel dependencies. It also requires `--privileged` or a `devices:` block in docker-compose, which ADR-003 explicitly prohibits. The mechanism is also unavailable on macOS and Windows Docker Desktop, where the host kernel is a Linux VM that does not expose v4l2loopback to Docker.

**Verdict:** Incompatible with ADR-003. Rejected.

---

### Option C: WebRTC canvas capture via Playwright

Use `page.Evaluate()` to inject frames into the Zoom Web App via the Canvas Capture API: draw each YUV420p frame onto a hidden `<canvas>`, call `canvas.captureStream(30)`, and replace the Zoom Web App's video track with the captured stream via `RTCRtpSender.replaceTrack()`.

This eliminates the fake device layer for video entirely. However:
- Converting YUV420p to a canvas-drawable format (RGBA or ImageBitmap) requires a software colour space conversion on every frame — 30 times per second at 1280×720, this is CPU-intensive.
- `RTCRtpSender.replaceTrack()` is a live WebRTC API call on the Zoom Web App's peer connection. The Zoom Web App's internal peer connection management is not under our control; whether `replaceTrack` is permitted or survives Zoom Web App updates is not guaranteed.
- This approach is significantly more fragile to Zoom Web App changes than the FIFO path.

**Verdict:** Rejected for Phase 1. Noted as a fallback if Chromium's fake video device mechanism breaks.

---

## Decision

**Named pipe (FIFO) with `--use-file-for-fake-video-capture` (Option A).**

The FIFO path is the direct, supported use of Chromium's fake video device flag. It requires no host kernel dependencies, no additional container packages, and no inter-process protocol beyond the `.y4m` format already mandated by ADR-002. The video buffer's repeat-last-frame underrun policy ensures the pipe always has data at the required cadence.

---

## FIFO Lifecycle

`pkg/bot` manages the FIFO at a fixed path inside the container (e.g., `/tmp/stenosaur_video.y4m`):

- **Create:** `mkfifo /tmp/stenosaur_video.y4m` at process startup, before Chromium is launched.
- **Chromium launch:** pass `--use-file-for-fake-video-capture=/tmp/stenosaur_video.y4m` in the Playwright launch arguments. Chromium opens the FIFO read end and blocks until the write end is opened.
- **Writer goroutine:** `pkg/bot` opens the write end (unblocking Chromium), writes the `.y4m` file header exactly once, then enters the 30 FPS write loop consuming frames from the video buffer.
- **Source switch:** continue writing to the same FIFO; there is no need to close and reopen it. The ADR-002 buffer flush and PTS reset are sufficient to produce a clean cut from Chromium's perspective.
- **Stop:** close the write end of the FIFO. Chromium reads EOF and stops the camera stream. `pkg/bot` logs `info` with `component: bot`.
- **Cleanup:** delete the FIFO on process exit.

---

## `.y4m` Wire Format

The `.y4m` stream written to the FIFO has the following structure:

**File header (written once, immediately after the FIFO write end is opened):**
```
YUV4MPEG2 W1280 H720 F30:1 Ip A0:0 C420\n
```

| Field | Value | Meaning |
|---|---|---|
| `W1280` | 1280 | Frame width in pixels |
| `H720` | 720 | Frame height in pixels |
| `F30:1` | 30/1 | Frame rate: 30 FPS |
| `Ip` | progressive | Interlacing: none |
| `A0:0` | 0:0 | Pixel aspect ratio: square |
| `C420` | 4:2:0 | Chroma subsampling — **must be `C420`, not `C420mpeg2` or `C420jpeg`** |

**Per-frame structure (written at 30 FPS, one frame per buffer pop):**
```
FRAME\n
<Y plane: 1280 × 720 bytes>
<Cb plane: 640 × 360 bytes>
<Cr plane: 640 × 360 bytes>
```

Total bytes per frame: 5 + `\n` (frame marker) + 921,600 (Y) + 230,400 (Cb) + 230,400 (Cr) = **1,382,406 bytes**.

At 30 FPS the sustained write rate is approximately **41.5 MB/s**. The FIFO kernel buffer (64 kB) holds less than one frame; Chromium's read must keep pace. In practice Chromium reads frames as fast as they are available; the video buffer's repeat-last-frame policy ensures no write-stall from the Go side.

---

## Write Timing

The writer goroutine targets a 33.3 ms inter-frame interval (1000 ms / 30 FPS) using `time.Ticker`. On each tick:

1. Pop one frame from the video buffer (repeat last frame if empty — ADR-002).
2. Write the `FRAME\n` marker.
3. Write the Y, Cb, and Cr planes sequentially.
4. If the write takes longer than 33.3 ms, log `warn` with `component: bot` and the actual duration — this is the ADR-002 video delivery timing violation.

`time.Ticker` is used rather than `time.Sleep` to accumulate drift across ticks; if a write takes 35 ms, the next tick fires 1.7 ms later rather than waiting a full 33.3 ms.

---

## Consequences

**Positive:**
- Direct use of the documented Chromium fake video device mechanism; no additional OS dependencies
- No additional container packages required
- No host kernel module or device access required; compatible with macOS and Windows Docker Desktop (ADR-003)
- The repeat-last-frame underrun policy from ADR-002 maps cleanly onto the continuous-write requirement of the FIFO
- Source switches do not require FIFO reconnection; the stream is continuous

**Negative:**
- At 30 FPS, 1280×720 YUV420p, the sustained write rate is ~41.5 MB/s; the writer goroutine must not stall
- The `.y4m` file header is written once; if the FIFO is closed and reopened mid-session, a new header must be written before frames resume — this case should not occur under normal operation but must be handled defensively
- `C420` pixel format tag requirement is a subtle correctness constraint; a wrong tag produces colour-shifted or rejected frames with no obvious error

**Accepted trade-offs:**
- FIFO path over V4L2 loopback; sacrifices native camera device semantics for Docker portability and zero host kernel dependencies
- Raw YUV420p write rate over compressed formats; simplicity and zero encoding latency outweigh the bandwidth cost within a single container

---

## Revision Triggers

1. **SDK migration (ADR-004b):** the Zoom Meeting SDK provides its own video injection API (direct frame callbacks); the FIFO mechanism is replaced entirely; this ADR is superseded
2. **Chromium fake video device breaks:** if a Chromium update changes how `--use-file-for-fake-video-capture` handles FIFOs (e.g., falls back to file-only behaviour), evaluate Option C (canvas capture) or pin Chromium at the last known-good version until a fix is available
3. **Frame rate or resolution change:** the `.y4m` header values and per-frame byte sizes must be recalculated; update ADR-002 in the same revision

---

## Related ADRs

- **ADR-002 (Media Format Contracts):** defines the YUV420p format (pixel format, resolution, frame rate, colour range) that frames must conform to before being written to the FIFO; defines the video buffer and repeat-last-frame underrun policy that feeds the writer goroutine
- **ADR-003 (Docker Deployment):** the no-host-device-dependency constraint is the direct reason V4L2 loopback was rejected; the FIFO lives entirely within the container filesystem
- **ADR-004 (Zoom Integration):** the PWA/Playwright path places Chromium inside the container and makes `--use-file-for-fake-video-capture` available; this ADR's mechanism is specific to that path
- **ADR-008 (Audio Injection Mechanism):** the parallel decision for audio; audio and video use different OS-level delivery mechanisms despite sharing format contracts in ADR-002
