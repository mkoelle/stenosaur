# ADR-012: Meeting Audio Monitoring and Voice Activity Detection

## Status
🟡 **Proposed**

---

## Context

The current pipeline is strictly unidirectional: `pkg/renderer` produces frames, `pkg/bot` injects them into the Zoom session. There is no path from the meeting back into the pipeline.

The use case of volume ducking — reducing the injected audio volume when meeting participants are speaking — requires observing the meeting's incoming audio and feeding a signal back to modify what is being injected. This introduces a feedback path that does not exist in the current architecture.

Three distinct problems must be solved independently:

1. **Capturing meeting audio output** without introducing a feedback loop (bot hears its own injected audio and ducks continuously).
2. **Detecting voice activity** in the captured audio to produce a duck/unduck signal.
3. **Applying a gain modifier** to the injected audio source in response to that signal.

Each of these is an architectural decision. This ADR addresses problems 1 and 2. Problem 3 (applying gain) is addressed in ADR-013, which introduces the `DuckingSource` wrapper that consumes the VAD signal produced here.

---

## Problem 1: Capturing Meeting Audio Without Feedback

### The feedback loop problem

The Zoom Web App plays incoming participant audio through Chromium's audio output, which routes to PulseAudio's `stenosaur_sink` (ADR-008). The same `stenosaur_sink` is where `pacat` writes the injected audio. Monitoring `stenosaur_sink.monitor` would capture both the incoming meeting audio and the bot's own injected audio mixed together.

If the VAD monitors this mix, it will detect the bot's own audio as voice activity and duck the bot's own stream — a feedback loop. The bot would permanently duck its own output, producing silence.

### Decision: isolated output sink

A second PulseAudio null sink, `stenosaur_output_sink`, is created to receive Chromium's audio output specifically. Chromium is configured to route its audio output to `stenosaur_output_sink` rather than `stenosaur_sink`. The VAD monitors `stenosaur_output_sink.monitor`, which contains only incoming participant audio — not the bot's injected stream.

**PulseAudio module additions** (entrypoint script, alongside ADR-008 modules):

```bash
# Isolated sink for Chromium's audio output (meeting participants)
# The VAD monitors this sink's monitor source (ADR-012)
--load="module-null-sink \
        sink_name=stenosaur_output_sink \
        sink_properties=device.description=StenosaurOutputSink"
```

Chromium's audio output is directed to `stenosaur_output_sink` via a PulseAudio sink-input move after Chromium launches. The `pactl move-sink-input` command identifies Chromium's audio stream by process name and moves it:

```bash
pactl move-sink-input \
  "$(pactl list sink-inputs | grep -B5 'application.name = "Chromium"' | grep 'Sink Input' | awk '{print $3}')" \
  stenosaur_output_sink
```

This is performed by `pkg/bot` after the Playwright join flow completes and Chromium has connected to the meeting audio stream.

### Two-sink configuration summary

| Sink | Monitor source | Contains | Used by |
|---|---|---|---|
| `stenosaur_sink` | `stenosaur_mic` | Bot's injected audio only | Chromium microphone input (ADR-008) |
| `stenosaur_output_sink` | `stenosaur_output_mic` | Incoming participant audio only | VAD goroutine (this ADR) |

---

## Problem 2: Voice Activity Detection

### VAD approach

The VAD goroutine reads from `stenosaur_output_sink.monitor` via a `pacat --record` subprocess, buffers 20 ms windows of PCM, computes the RMS energy of each window, and compares it against a configurable threshold.

Simple energy-based VAD is sufficient for this use case. Full neural VAD (e.g., Silero VAD) would be more accurate in noisy conditions but adds a significant dependency and latency. The Zoom meeting's own noise suppression and acoustic echo cancellation are applied before audio reaches the participants' microphones; by the time audio arrives at the meeting output, background noise is already reduced.

**RMS threshold VAD:**

```
window size: 20 ms (960 samples at 48 kHz mono)
RMS = sqrt(mean(samples²))
if RMS > DUCK_THRESHOLD: voice active
if RMS < DUCK_THRESHOLD for RELEASE_MS: voice inactive
```

`DUCK_THRESHOLD` and `RELEASE_MS` are configurable via environment variables (ADR-003). Defaults:

| Variable | Default | Description |
|---|---|---|
| `VAD_DUCK_THRESHOLD` | `500` | RMS level (0–32767) above which voice is considered active |
| `VAD_RELEASE_MS` | `800` | Milliseconds of silence before voice is considered inactive |

The release delay prevents rapid duck/unduck cycling during brief pauses in speech.

### VAD goroutine lifecycle

The VAD goroutine lives in `pkg/bot`. It is started when `StartMediaStream` is called and stopped when `StopMediaStream` is called.

```
VAD goroutine:
  pacat := start("pacat --record --device=stenosaur_output_sink.monitor
                        --format=s16le --rate=48000 --channels=1")
  for each 20ms window read from pacat.stdout:
    rms = computeRMS(window)
    state = updateVADState(rms, threshold, releaseMs)
    if state changed:
      duckingController.SetVADActive(state == VoiceActive)
```

The `duckingController` is an interface passed into `pkg/bot` at construction time, implemented by `DuckingSource` in `pkg/renderer` (ADR-013). This keeps the dependency direction correct: `pkg/bot` calls into an interface it receives; it does not import `pkg/renderer`.

### VAD signal interface

A new interface in `pkg/media` (the only package both `pkg/bot` and `pkg/renderer` can share):

```go
// DuckingController is implemented by pkg/renderer.DuckingSource and
// consumed by the VAD goroutine in pkg/bot. (ADR-012, ADR-013)
type DuckingController interface {
    SetVADActive(active bool)
}
```

`pkg/bot` receives a `DuckingController` at construction time. When no ducking is configured, a no-op implementation is passed. This keeps ducking entirely optional — the core bot path is unchanged when ducking is not in use.

---

## API Integration

### Configuration

Two new optional environment variables (ADR-003):

| Variable | Default | Description |
|---|---|---|
| `VAD_DUCK_THRESHOLD` | `500` | RMS energy threshold for voice detection |
| `VAD_RELEASE_MS` | `800` | Silence hold time before unduck (ms) |
| `VAD_ENABLED` | `false` | Enable VAD ducking; off by default |

VAD is disabled by default. Setting `VAD_ENABLED=true` activates the VAD goroutine and the second PulseAudio output sink. When disabled, `stenosaur_output_sink` is still provisioned (it costs nothing) but the VAD goroutine is not started and no `pacat --record` subprocess is launched.

### `/status` extension

The `/status` response is extended with a `vad` field when `VAD_ENABLED=true`:

```json
"vad": {
  "enabled": true,
  "active": false,
  "rms_level": 142,
  "duck_threshold": 500
}
```

`active: true` means voice is currently detected and the duck gain is being applied.

---

## Consequences

**Positive:**
- Feedback loop is prevented by sink isolation; the VAD cannot detect the bot's own audio
- `DuckingController` interface in `pkg/media` keeps the dependency direction correct; `pkg/bot` does not import `pkg/renderer`
- VAD is opt-in; the core pipeline is unchanged when disabled
- Simple RMS VAD has zero external dependencies and deterministic latency

**Negative:**
- Chromium sink-input move relies on `pactl` process-name matching; this may be fragile if the Chromium process name changes or if multiple Chromium contexts are running
- 20 ms VAD window adds 20 ms of detection latency; combined with the `RELEASE_MS` hold time, the duck-to-unduck transition can lag speech by up to `RELEASE_MS + 20 ms`
- A second `pacat --record` subprocess runs continuously when VAD is enabled; minor CPU overhead

**Accepted trade-offs:**
- RMS-based VAD over neural VAD; simpler, no additional dependencies, sufficient for Zoom meeting conditions
- Sink-input move via `pactl` shell invocation over a native PulseAudio binding; consistent with ADR-008's `pacat` subprocess approach

---

## Revision Triggers

1. **Chromium sink-input move is unreliable:** if `pactl move-sink-input` fails to correctly target Chromium's audio stream across Chromium versions, investigate PulseAudio's `module-stream-restore` or a native PulseAudio client binding to identify and move the correct sink input by PID rather than process name
2. **RMS VAD produces too many false positives in noisy meetings:** increase `VAD_DUCK_THRESHOLD` default; if threshold tuning is insufficient, evaluate WebRTC's built-in VAD library (`libwebrtc`) via CGo or a subprocess wrapper
3. **SDK migration (ADR-004b):** the Meeting SDK provides audio callbacks for incoming participant audio directly; the `stenosaur_output_sink` and `pacat --record` path is replaced by an SDK callback; update this ADR

---

## Related ADRs

- **ADR-003 (Docker Deployment):** new environment variables `VAD_ENABLED`, `VAD_DUCK_THRESHOLD`, `VAD_RELEASE_MS` must be added to `.env.example` and startup validation
- **ADR-008 (Audio Injection Mechanism):** the two-sink configuration extends the PulseAudio setup defined there; `stenosaur_output_sink` is provisioned alongside `stenosaur_sink` in the entrypoint
- **ADR-013 (Audio Mixing and Ducking):** `DuckingSource` implements `DuckingController`; it consumes the VAD signal produced by this ADR and applies gain to the injected audio stream
