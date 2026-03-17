# ADR-008: Audio Injection Mechanism

## Status
🟡 **Proposed**

---

## Context

ADR-002 defines the audio format contract: 48 kHz, mono, 16-bit signed little-endian PCM, produced as 480-sample frames by `pkg/renderer` and buffered in a ring buffer in `pkg/media`. ADR-002 does not specify how those frames are physically delivered from `pkg/bot` into Chromium's microphone input once they leave the buffer.

This ADR concerns **Chromium A** — the Zoom session instance launched with fake media device flags (ADR-001, ADR-004). Chromium B (the renderer instance) has no fake audio device and is not involved in audio injection.

This is a non-trivial decision. There is a critical constraint that is not obvious from Chromium's documentation:

**`--use-file-for-fake-audio-capture` reads a static file from disk and loops it.** It does not accept a named pipe (FIFO) as input — the flag opens the path as a regular file at startup, reads it to completion, and loops. A FIFO would block on open until a writer arrives, which stalls Chromium's initialisation. Even if a WAV header is written first, the continuous PCM stream that follows cannot be consumed correctly through this mechanism. This flag is suitable only for fixed, pre-recorded audio clips and cannot carry a live, dynamically produced frame stream.

The audio injection mechanism must therefore bypass `--use-file-for-fake-audio-capture` entirely.

---

## Constraint: PulseAudio Is Already Required

Chromium's audio service tries to connect to a system audio backend (PulseAudio or ALSA) during initialisation, even in headless mode. In a plain Linux container with no audio devices configured, this fails with ALSA errors (`Unknown PCM default`, `cannot find card '0'`). Chromium does not crash, but its audio service is in a broken state and WebRTC audio behaves unreliably.

A PulseAudio null sink (`module-null-sink`) provisioned by the container entrypoint resolves this: Chromium's audio service initialises cleanly against a valid PulseAudio backend, and the null sink discards all audio written to it. This requirement exists regardless of the injection mechanism chosen.

Because PulseAudio is already present and mandatory for Chromium startup, the injection mechanism decision is whether to extend PulseAudio's role or introduce a separate delivery path.

---

## Options Evaluated

### Option A: Named pipe (FIFO) + `--use-file-for-fake-audio-capture=%noloop`

A named pipe (`mkfifo`) is created at a known path. `--use-file-for-fake-audio-capture=<path>%noloop` points Chromium at the pipe. The `%noloop` suffix suppresses Chromium's looping behaviour. Go writes a WAV header once at startup, then streams raw 16-bit PCM frames into the pipe continuously from the ring buffer.

**Why this does not work reliably:**

The FIFO pipe has a kernel buffer of 64 kB (Linux default). At 48 kHz mono 16-bit PCM, that buffer holds approximately 680 ms of audio. Chromium reads from the pipe at its own rate. If the Go writer stalls for any reason — GC pause, scheduler delay, source switch — the pipe buffer empties and Chromium's read blocks. The Go goroutine writing to the pipe also blocks on a full buffer if Chromium falls behind. There is no graceful recovery: re-establishing the WAV stream mid-session causes Chromium to treat the new header as audio data, producing a loud click and potentially desynchronising the WebRTC audio clock.

The `%noloop` flag behaviour is also Chromium-version-specific and not documented in stable API surface; it may silently stop working across Chromium updates.

**Verdict:** Fragile; not recommended.

---

### Option B: PulseAudio null sink + `pacat` ✅

PulseAudio is already running in the container. Its null sink can serve as the audio injection conduit with two additional steps:

1. **`module-remap-source`** exposes the null sink's monitor output as a named virtual microphone source (`stenosaur_mic`). Chromium, launched with `--use-fake-device-for-media-stream`, enumerates PulseAudio sources and selects `stenosaur_mic` as its microphone capture device.

2. **`pacat --playback`** is a PulseAudio command-line client included in `pulseaudio-utils` (already in the container image). `pkg/bot` launches `pacat` as a subprocess and writes raw PCM frames to its stdin. PulseAudio routes those frames through the null sink into the monitor source that Chromium is reading from.

PulseAudio manages the clock boundary between the Go writer and Chromium's reader. The PulseAudio server runs its own resampler and buffer, absorbing rate mismatches and transient stalls without blocking the Go goroutine. This is the mechanism PulseAudio is specifically designed for.

**Verdict: Selected.**

---

### Option C: Web Audio API injection via Playwright

Inject audio directly into the Zoom Web App's `AudioContext` via `page.Evaluate()`, constructing an `AudioWorklet` → `MediaStreamSource` → `RTCPeerConnection` chain in JavaScript. Eliminates the fake device layer for audio entirely.

**Why rejected for Phase 1:**

The Zoom Web App initialises its own `AudioContext` and WebRTC peer connection at join time. The initialisation order and internal graph structure are not documented and change with Zoom Web App releases. Injecting into this graph via `evaluate()` requires reverse-engineering internal JavaScript state, is fragile to Zoom Web App updates, and cannot be tested independently of a live Zoom session.

Option B is less coupled to Zoom's internal implementation. Option C is noted as a path worth revisiting if PulseAudio proves structurally unsuitable (e.g., SDK migration makes Chromium unavailable).

**Verdict:** Rejected for Phase 1.

---

## Decision

**PulseAudio null sink + `pacat` subprocess (Option B).**

PulseAudio is already present and mandatory for Chromium startup. Extending its role to audio injection adds no new container packages, no new OS dependencies, and no new process lifecycle concerns beyond `pacat` itself. The `pacat` subprocess is managed by `pkg/bot` alongside the Playwright browser process; its lifecycle is straightforward to supervise.

---

## PulseAudio Module Configuration

The container entrypoint provisions the following PulseAudio modules in order:

```bash
pulseaudio --start \
  --load="module-null-sink \
          sink_name=stenosaur_sink \
          sink_properties=device.description=StenosaurSink" \
  --load="module-remap-source \
          master=stenosaur_sink.monitor \
          source_name=stenosaur_mic \
          source_properties=device.description=StenosaurMic" \
  --load="module-native-protocol-unix" \
  --disallow-exit \
  --daemon
```

| Module | Name | Purpose |
|---|---|---|
| `module-null-sink` | `stenosaur_sink` | Virtual output device; Chromium audio backend; receives `pacat` playback |
| `module-remap-source` | `stenosaur_mic` | Exposes `stenosaur_sink.monitor` as a named capture source; selected by Chromium as the meeting microphone |
| `module-native-protocol-unix` | — | Unix socket for `pacat` and other PulseAudio clients to connect |

The same null sink serves both purposes (Chromium startup and audio injection). No additional modules are required.

---

## Chromium Launch Flags

Audio injection via PulseAudio does **not** use `--use-file-for-fake-audio-capture`. The relevant flags are:

```
--use-fake-device-for-media-stream
```

This flag instructs Chromium to use its fake device implementation for `getUserMedia`. Combined with a running PulseAudio server, Chromium enumerates PulseAudio sources and selects `stenosaur_mic` (the highest-priority non-default source) as the capture device. No explicit device selection flag is needed; PulseAudio's device naming ensures `stenosaur_mic` is selected.

If device selection is ambiguous in a specific Chromium version, the source can be forced via `--alsa-input-device=pulse:stenosaur_mic` or via a PulseAudio default-source configuration.

---

## Wire Format

`pacat` is invoked by `pkg/bot` with flags matching the ADR-002 audio contract:

```
pacat --playback \
      --device=stenosaur_sink \
      --format=s16le \
      --rate=48000 \
      --channels=1 \
      --latency-msec=40
```

`pkg/bot` writes raw 16-bit little-endian PCM samples with **no WAV header** to `pacat`'s stdin. The `--latency-msec=40` target sets the PulseAudio playback buffer to 40 ms, deliberately aligned with the ADR-002 audio drift warn threshold. If the Go writer stalls for longer than 40 ms, PulseAudio plays silence (rather than blocking), and the ADR-002 drift metric will cross the warn threshold and log accordingly.

---

## `pacat` Subprocess Lifecycle

`pkg/bot` is responsible for `pacat`'s lifecycle:

- **Start:** launch `pacat` immediately after PulseAudio readiness is confirmed and before `StartMediaStream` is called.
- **Supervision:** if `pacat` exits unexpectedly, log `error` with `component: bot` and set session state to degraded; do not attempt silent restart.
- **Stop:** send `SIGTERM` to `pacat` when `StopMediaStream` is called or the session ends. `pacat` flushes its buffer and exits cleanly on SIGTERM.
- **Source switch:** when switching audio sources, pause writing to `pacat`'s stdin (the PulseAudio buffer plays out naturally), flush the ADR-002 ring buffer, reset the PTS origin, then resume writing with the new source's frames.

---

## Consequences

**Positive:**
- PulseAudio absorbs rate mismatches between the Go writer and Chromium's reader without blocking either
- `pacat` failure is detectable (subprocess exit) and distinguishable from source underruns
- No new container packages beyond `pulseaudio-utils` (already required for `pactl` in the entrypoint)
- The same PulseAudio instance serves both the Chromium startup requirement and the injection path

**Negative:**
- `pacat` is a subprocess; `pkg/bot` must manage its stdin pipe, supervise its exit, and handle its error output
- PulseAudio latency (target 40 ms) adds a fixed offset to audio sync; this is within the ADR-002 drift tolerance but non-zero
- PulseAudio device selection relies on `stenosaur_mic` being Chromium's preferred source; this may require explicit configuration if the Playwright-managed Chromium instance enumerates devices differently across versions

**Accepted trade-offs:**
- `pacat` subprocess over a native Go PulseAudio binding (`github.com/jfreymuth/pulse`); the subprocess is simpler to implement, debug, and replace, and `pacat` is a stable, well-tested tool

---

## Revision Triggers

1. **SDK migration (ADR-004b):** the Zoom Meeting SDK provides its own audio injection API (direct PCM callbacks); PulseAudio is not needed on the SDK path and this entire mechanism is replaced
2. **`pacat` proves unreliable in production:** if sustained audio dropout is traced to `pacat` subprocess management or PulseAudio latency rather than source underruns, evaluate a native Go PulseAudio binding (`github.com/jfreymuth/pulse`) or fall back to Option A (FIFO pipe) with explicit stall recovery
3. **Chromium device selection breaks:** if a Chromium update changes how `--use-fake-device-for-media-stream` enumerates PulseAudio sources and `stenosaur_mic` is no longer selected automatically, add explicit device selection via `--alsa-input-device` or PulseAudio default-source configuration

---

## Related ADRs

- **ADR-002 (Media Format Contracts):** defines the PCM format (48 kHz, mono, 16-bit LE, 480 samples/frame) that frames must conform to before being written to `pacat`; defines the ring buffer that decouples `pkg/renderer` from `pkg/bot`
- **ADR-003 (Docker Deployment):** the entrypoint script provisions PulseAudio; `pulseaudio-utils` must be present in the container image
- **ADR-004 (Zoom Integration):** the PWA/Playwright path is the reason Chromium runs inside the container and requires a PulseAudio backend; this ADR's mechanism is specific to that path
- **ADR-009 (Video Injection Mechanism):** the parallel decision for video; audio and video use different OS-level delivery mechanisms
