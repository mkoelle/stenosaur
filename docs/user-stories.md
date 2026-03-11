# Stenosaur: Implementation User Stories

This document contains implementation detail extracted from the ADR set. Each story maps to one or more ADRs and represents concrete work to be done. Stories are grouped by package or concern. They do not restate architectural decisions — those live in the ADRs.

---

## pkg/media

**US-M01:** Define `AudioFrame` as a Go struct with fields `Samples []int16` and `PTS time.Duration`. Define `VideoFrame` as a Go struct with fields `Y []byte`, `Cb []byte`, `Cr []byte`, `Width int`, `Height int`, and `PTS time.Duration`.
*Ref: ADR-001, ADR-002*

**US-M02:** Define `AudioSource` and `VideoSource` interfaces in `pkg/media`. These are the interfaces implemented by `pkg/renderer` and consumed by `pkg/bot`. `pkg/media` must have no imports from other internal packages.
*Ref: ADR-001*

**US-M03:** Implement the audio ring buffer: 200-frame capacity, silence emission on underrun, oldest-frame discard on overrun, and metric counters for underruns and overruns. Buffer depth and capacity must be readable by the metrics layer.
*Ref: ADR-002*

**US-M04:** Implement the video FIFO queue: 10-frame capacity, last-frame repeat on underrun, oldest-frame discard on overrun, and metric counters for underruns and overruns.
*Ref: ADR-002*

**US-M05:** Implement PTS-based drift detection in the consumer (bot) goroutine. Emit a `warn` log when audio drift exceeds 40 ms; emit an `error` log when drift exceeds 200 ms. Drop or duplicate one frame to resynchronize when the warn threshold is crossed.
*Ref: ADR-002*

**US-M06:** Implement video delivery timing check: emit a `warn` log when the wall-clock delta between consecutive video frame deliveries exceeds one frame period (33.3 ms).
*Ref: ADR-002*

---

## pkg/renderer

**US-R01:** Implement `WebRenderer`: open a browser context in the shared Chromium instance (separate from the Zoom session context), navigate to a configured URL, capture frames at 30 FPS as YUV420p at 1280 × 720. If audio capture from the rendered page is required, capture via the Web Audio API at 48 kHz mono.
*Ref: ADR-001, ADR-002*

**US-R02:** Implement `FileDecoder`: accept an audio or video file path; decode audio to 48 kHz mono 16-bit PCM using FFmpeg bindings, resampling if the source differs; decode video to YUV420p and scale to 1280 × 720.
*Ref: ADR-002*

**US-R03:** Implement `AudioSynth`: synthesize audio programmatically at 48 kHz mono. Include a 440 Hz sine wave generator as a built-in test source that requires no external file.
*Ref: ADR-002*

**US-R04:** Implement frame-repeat logic: if a source cannot produce a video frame within the 33.3 ms window, repeat the last produced frame rather than reducing the frame rate or emitting a black frame.
*Ref: ADR-002*

**US-R05:** When switching sources, reset the PTS origin and flush both the audio and video buffers before resuming frame production to prevent cross-source sync discontinuity.
*Ref: ADR-002*

**US-R06:** Implement a static SMPTE color bar frame (1280 × 720, YUV420p) as the default video output when only an audio-only source is active (e.g., `AudioSynth` with no video source configured).
*Ref: ADR-002*

---

## pkg/bot

**US-B01:** Define the `ZoomClient` interface with at minimum: `Join(meetingURL, displayName string) error`, `StartMediaStream(audio AudioSource, video VideoSource) error`, `StopMediaStream() error`, `IsConnected() bool`, `Leave() error`. The PWA implementation must satisfy this interface so it can be swapped for an SDK implementation without changing callers.
*Ref: ADR-001, ADR-004*

**US-B02:** Implement the PWA join flow using Playwright: launch headless Chromium with fake media device flags, navigate to `app.zoom.us/wc/join/<meeting_id>`, enter the configured display name, handle audio/video permission prompts, and wait for meeting admission including waiting room handling.
*Ref: ADR-004*

**US-B03:** Configure Chromium launch flags for fake media injection:
```
--use-fake-device-for-media-stream
--use-file-for-fake-audio-capture=<path>
--use-file-for-fake-video-capture=<path>
--allow-file-access-from-files
```
Verify on launch that Chromium is reading from the configured fake devices and not emitting the default test tone into the meeting.
*Ref: ADR-002, ADR-004*

**US-B04:** Add comments to every Playwright UI selector noting the Zoom Web App version against which the selector was verified. Log a `warn` when a selector times out to distinguish selector breakage from network issues.
*Ref: ADR-004*

**US-B05:** In non-production builds, enable Playwright's `video: 'on'` option to record the browser session. This is the primary debugging tool for join flow failures.
*Ref: ADR-004*

**US-B06:** Add per-goroutine `recover` wrappers in the bot goroutine. Log the panic with `level: error` and `component: bot` before the goroutine exits. The container restart policy (ADR-003) handles process-level recovery.
*Ref: ADR-001, ADR-003*

---

## pkg/controller

**US-C01:** Implement the HTTP server listening on the port configured by `ADMIN_PORT` (default 8080). All endpoints defined in ADR-005 must be served from this server.
*Ref: ADR-003, ADR-005*

**US-C02:** Implement `/healthz`: no auth, returns `{"status": "ok"}` with HTTP 200 whenever the server is responding.
*Ref: ADR-005*

**US-C03:** Implement `/readyz`: no auth, checks Zoom session state, audio pipeline, video pipeline, and Chromium process; returns HTTP 200 with `{"status": "ready", "checks": {...}}` when all pass, HTTP 503 with the same schema when any fail.
*Ref: ADR-005*

**US-C04:** Implement `/status` with Bearer token auth: return current session state, source info, and all seven pipeline metrics defined in ADR-002. Strip any password query parameter from `meeting_url` before serialization.
*Ref: ADR-002, ADR-005*

**US-C05:** Implement `/start`, `/stop`, `/config`, `/media` with Bearer token auth. Return consistent JSON error bodies on 401. Use `subtle.ConstantTimeCompare` for all token comparisons.
*Ref: ADR-005*

**US-C06:** Implement the `/logs` WebSocket endpoint: authenticate on the HTTP upgrade request via `Authorization` header, then stream one JSON log line per message as they are emitted.
*Ref: ADR-005*

**US-C07:** Implement a shared structured logger used by all three packages. Logger must output one JSON line per entry conforming to the schema in ADR-005 (`ts`, `component`, `level`, `msg`, `ctx`). Logger must strip passwords from any URL value before writing. Logger must respect the `LOG_LEVEL` env var (ADR-003).
*Ref: ADR-003, ADR-005*

---

## Container and Deployment

**US-D01:** Write the `Dockerfile`: choose a base image that includes or can install Chromium and PulseAudio; copy the Go binary; add an entrypoint script. Pin the Chromium version and document it in `ARCHITECTURE.md`.
*Ref: ADR-003, ADR-004*

**US-D02:** Write the entrypoint script: start PulseAudio with a null sink (`module-null-sink`) as the default output, wait for PulseAudio readiness (`pactl info` in a loop with a timeout), then exec the bot binary. If PulseAudio does not become ready within the timeout, exit with a non-zero code and a specific error message.
*Ref: ADR-003*

**US-D03:** Configure the PulseAudio null sink so Chromium detects it as an audio input device via `--use-fake-device-for-media-stream`. Verify that Chromium reads from this sink on startup and does not fall back to silence or an error state.
*Ref: ADR-002, ADR-003, ADR-004*

**US-D04:** Add a `HEALTHCHECK` directive to the Dockerfile targeting `/healthz` with a start period of at least 15 seconds to allow PulseAudio and Chromium to initialize before the first check.
*Ref: ADR-003, ADR-005*

**US-D05:** Write `docker-compose.yml` with a single `stenosaur` service, `restart: unless-stopped`, port mapping for `ADMIN_PORT`, and a `./media:/app/media` volume mount.
*Ref: ADR-003*

**US-D06:** Write `.env.example` documenting every variable listed in ADR-003 with descriptions and placeholder values. Add `.env` to `.gitignore`.
*Ref: ADR-003*

**US-D07:** Commit `./media/.gitkeep` to the repository so the media directory exists before first run. Document in the README that this directory must not be deleted.
*Ref: ADR-003*

**US-D08:** Implement startup validation in `cmd/stenosaur/main.go` covering all checks listed in ADR-003: required env vars present, `MEETING_URL` format valid, `ADMIN_TOKEN` ≥ 16 characters, `MEDIA_DIR` readable, Chromium binary present, PulseAudio sink ready, HTTP server binds. Exit with a non-zero code and a specific actionable message on any failure.
*Ref: ADR-003*

---

## Testing and CI

**US-T01:** Write an integration test that starts all three packages in-process, produces frames from `AudioSynth` and the SMPTE color bar source, and verifies that frames flow through the buffer and are consumed by the bot goroutine without underruns.
*Ref: ADR-001, ADR-002*

**US-T02:** Write a Playwright test that runs the join flow in headed mode first, then in headless mode. Both must complete without error against a test Zoom meeting.
*Ref: ADR-004*

**US-T03:** Add a CI step that captures stdout from the integration test, scans for the literal values of `ADMIN_TOKEN` and `MEETING_PASSWORD` from the test `.env`, and fails the build if either is found in any log line.
*Ref: ADR-003, ADR-005*

**US-T04:** Verify the container starts without `--privileged` and without a `devices:` block on Linux, macOS Docker Desktop, and Windows Docker Desktop. Document any host-specific findings.
*Ref: ADR-003*

**US-T05:** Set a 6-month calendar review for ADR-004. Log any Playwright selector breakage incident as a GitHub issue tagged `adr-004-trigger`. Three incidents within the review period trigger ADR-004b.
*Ref: ADR-004*

**US-T06:** Verify the `.y4m` pixel format tag written by the video pipeline is `C420` (not `C420mpeg2` or `C420jpeg`) using a known-good `.y4m` file. Test that Chromium accepts and renders it at the correct frame rate.
*Ref: ADR-002*
