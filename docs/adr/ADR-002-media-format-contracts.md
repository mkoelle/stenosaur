# ADR-002: Media Format Contracts and Pipeline Buffering

## Status
🟡 **Proposed**

---

## Context

Stenosaur injects audio and video into a Zoom meeting by feeding media into a Chromium browser instance running the Zoom Web App (ADR-004). The renderer (`pkg/renderer`) produces frames from three source types — webpage rendering, file decoding, and programmatic synthesis — and passes them to the bot (`pkg/bot`) for injection. Both packages run in-process within a single container (ADR-001), communicating via Go channels.

Two questions must be answered before implementation begins:

1. **What format must audio and video frames conform to?** This is not a free choice — it is determined by what Chromium's fake media device pipeline accepts at the injection boundary, working backwards through the renderer's normalization layer.

2. **How are frames buffered between the renderer and the bot?** The renderer and bot run as separate goroutines at potentially different rates. Without explicit buffer contracts, the pipeline will exhibit timing gaps, frame drops, and audio dropouts under normal conditions.

---

## Constraint: The Injection Boundary Determines the Format

Under the PWA path, Stenosaur does not write audio/video directly to Zoom's media stack — it writes to Chromium's fake media device layer. The formats Chromium accepts at this boundary are fixed and not configurable:

- **Audio:** Chromium's fake audio capture expects WAV at **48 kHz, mono, 16-bit PCM**. Chromium's WebRTC stack operates natively at 48 kHz; any other rate requires resampling at the boundary with undefined quality characteristics.
- **Video:** Chromium's fake video capture expects **YUV 4:2:0 planar** (`.y4m`, pixel format tag `C420`). This is Chromium's `libyuv` native format; no transcoding is required. Alternative formats (`.mjpeg`) cause frame rate instability and higher CPU usage.

All renderer sources must normalize output to these formats. The format values are derived from the injection boundary, not chosen arbitrarily.

---

## Audio Contract

| Property | Value | Basis |
|---|---|---|
| Encoding | PCM linear | Lossless; no codec overhead in pipeline |
| Bit depth | 16-bit signed little-endian | WAV standard; Chromium fake audio requirement |
| Sample rate | 48,000 Hz | Chromium WebRTC native rate; no resampling at injection |
| Channels | Mono (1) | Zoom Web App mic input is mono; stereo adds complexity with no benefit |
| Frame size | 480 samples (10 ms) | Standard WebRTC processing block at 48 kHz |

---

## Video Contract

| Property | Value | Basis |
|---|---|---|
| Pixel format | YUV 4:2:0 planar (`yuv420p`) | Chromium `.y4m` requirement; `libyuv` native format |
| Resolution | 1280 × 720 (720p) | Zoom Web App standard HD tier |
| Frame rate | 30 FPS | Standard camera rate expected by the Zoom Web App |
| Color range | Limited (TV / studio swing, BT.601) | `.y4m` default |

If a source cannot sustain 30 FPS, the last available frame is repeated rather than reducing the frame rate. A reduced rate signals to Zoom's WebRTC layer that the camera is degraded; a static repeated frame does not.

---

## Buffer Contracts

The renderer and bot run as separate goroutines. Buffering decouples their rates and absorbs transient production gaps without causing injection underruns.

### Audio Buffer

| Property | Value |
|---|---|
| Type | Ring buffer (circular) |
| Capacity | 200 frames (2 seconds) |
| Low-water mark | 50 frames (500 ms) — emit `warn` log |
| Critical mark | 10 frames (100 ms) — emit `error` log |
| Underrun policy | Emit silence frame; increment underrun metric |
| Overrun policy | Discard oldest frame; increment overrun metric |

Emitting silence on underrun prevents Chromium's fake audio device from stalling, which produces worse artifacts than a brief gap.

### Video Buffer

| Property | Value |
|---|---|
| Type | FIFO queue |
| Capacity | 10 frames (~333 ms at 30 FPS; ~27 MB resident) |
| Low-water mark | 3 frames — emit `warn` log |
| Underrun policy | Repeat last delivered frame; increment underrun metric |
| Overrun policy | Discard oldest frame; increment overrun metric |

Repeating the last frame on underrun maintains visual continuity. A static frame is preferable to a flash to black during brief renderer hiccups.

---

## Timing and Drift

The renderer and Chromium's fake device layer are clocked independently and will drift over time.

Each frame carries a PTS (presentation timestamp) set at production time. The bot compares PTS against wall clock to detect drift:

| Threshold | Action |
|---|---|
| Audio drift > 40 ms | Emit `warn` log; drop or duplicate one frame to resync |
| Audio drift > 200 ms | Emit `error` log |
| Video delivery delta > 33.3 ms (one frame period) | Emit `warn` log |

When switching sources, the renderer resets the PTS origin and flushes both buffers before resuming production to prevent cross-source sync discontinuity.

---

## Pipeline Metrics

The buffer layer must emit the following metrics, exposed via the controller status API (ADR-005):

| Metric | Description |
|---|---|
| `audio_buffer_depth` | Current frames in ring buffer |
| `audio_underrun_total` | Cumulative silence frames emitted |
| `audio_overrun_total` | Cumulative frames discarded on full buffer |
| `audio_drift_ms` | Current drift between renderer PTS and wall clock |
| `video_buffer_depth` | Current frames in FIFO queue |
| `video_underrun_total` | Cumulative repeated frames emitted |
| `video_overrun_total` | Cumulative frames discarded on full buffer |

---

## Consequences

**Positive:**
- Formats are grounded in Chromium's actual requirements; no silent resampling at the injection boundary
- In-process channels eliminate serialization overhead and inter-container latency
- Buffer behavior is explicit and observable rather than implementation-defined

**Negative:**
- All sources must normalize to 48 kHz audio and YUV420p video; sources in other formats require explicit resampling/decoding in `pkg/renderer`
- 10-frame video buffer requires ~27 MB resident memory; must be accounted for in container memory limits (ADR-003)
- PTS-based drift correction adds implementation complexity; sessions longer than a few minutes will desync without it

**Accepted trade-offs:**
- Mono audio accepted over stereo; the Zoom Web App does not benefit from stereo mic input
- Fixed 720p resolution accepted; parameterization can be added later if needed

---

## Revision Triggers

1. **SDK migration (ADR-004b):** the Meeting SDK has different format requirements (typically 16 kHz mono audio, SDK-specific video callbacks); this ADR's contracts are specific to the Chromium injection path
2. **PulseAudio virtual sink replaces file-based injection:** if dynamic audio is fed via a PulseAudio virtual sink rather than a WAV pipe, the injection boundary changes and the audio contract must be updated
3. **Resolution or frame rate change:** product or Zoom Web App requirements change; update video contract and buffer sizing
4. **Buffer tuning from production data:** underrun/overrun metrics indicate capacities are wrong for real workloads; update with evidence from incident logs

---

## Related ADRs

- **ADR-001 (Service Decomposition):** establishes that renderer and bot are in-process packages communicating via Go channels; this ADR specifies what flows through those channels
- **ADR-003 (Docker Deployment):** container memory limits must account for the ~27 MB video buffer
- **ADR-004 (Zoom Integration):** the PWA/Chromium path is the direct source of the format requirements; these contracts are not portable to the SDK path
- **ADR-005 (Observability):** the seven pipeline metrics defined here are surfaced via the `/status` endpoint and WebSocket stream specified in ADR-005
