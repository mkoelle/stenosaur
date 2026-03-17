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

**US-R01:** Implement `WebRenderer`: launch a dedicated Chromium B instance (separate from Chromium A used for the Zoom session — see ADR-001) without fake media device flags. Navigate to a configured URL, capture frames at 30 FPS as YUV420p at 1280 × 720 via screenshot-based capture. If audio capture from the rendered page is required, capture via the Web Audio API at 48 kHz mono. Chromium B must not be passed `--use-file-for-fake-video-capture` or `--use-fake-device-for-media-stream`; those flags are exclusive to Chromium A.
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

**US-B03:** Configure Chromium launch flags and audio/video injection plumbing per ADR-002:

**Video injection (named pipe):** create a FIFO at a known path; launch Chromium with:
```
--use-fake-device-for-media-stream
--use-file-for-fake-video-capture=<fifo_path>
--allow-file-access-from-files
```
Write YUV420p `.y4m` frames into the FIFO from the bot goroutine at 30 FPS.

**Audio injection (PulseAudio):** do **not** use `--use-file-for-fake-audio-capture` — that flag only accepts a static WAV file and cannot stream live frames. Instead:
1. PulseAudio's `stenosaur_mic` virtual source (provisioned by the entrypoint) is automatically discovered by Chromium via `--use-fake-device-for-media-stream`.
2. Start a `pacat` subprocess with flags matching the audio contract: `--format=s16le --rate=48000 --channels=1 --device=stenosaur_sink --latency-msec=40`.
3. Write raw 16-bit little-endian PCM samples (no WAV header) from the audio ring buffer into `pacat`'s stdin.

Verify on launch that Chromium is reading from `stenosaur_mic` and that `pacat` is writing to `stenosaur_sink`. Log a `warn` and stop the session if either subprocess exits unexpectedly.
*Ref: ADR-002, ADR-004*

**US-B04:** Add comments to every Playwright UI selector noting the Zoom Web App version against which the selector was verified. Log a `warn` when a selector times out to distinguish selector breakage from network issues.
*Ref: ADR-004*

**US-B05:** In non-production builds, enable Playwright's `video: 'on'` option to record the browser session. This is the primary debugging tool for join flow failures.
*Ref: ADR-004*

**US-B06:** Add per-goroutine `recover` wrappers in all bot goroutines (join flow, media injection, VAD). Log the panic with `level: error` and `component: bot` before the goroutine exits. Go panics within the bot goroutines must not silently terminate injection; they must log and transition state to `ERROR` (ADR-014).
*Ref: ADR-001, ADR-003, ADR-014*

**US-B10:** Implement the session state machine defined in ADR-014. The `PWAClient` struct must hold a `SessionState` field (one of `IDLE`, `JOINING`, `CONNECTED`, `STREAMING`, `ERROR`) protected by a mutex. Every `ZoomClient` method must check the current state before executing and return a `StateError` on invalid transitions. State transitions must be logged at `info` level with `component: bot` and the new state name.
*Ref: ADR-014*

**US-B11:** Implement Chromium A crash recovery in `pkg/bot`. Register Playwright page event handlers (`framenavigated`, `crash`) on the Zoom session page to detect external session termination. On Chromium crash or unexpected external disconnect while in `CONNECTED` or `STREAMING`: transition to `ERROR`; attempt the recovery sequence (stop media, close Chromium A, wait `CHROMIUM_RESTART_DELAY_SECONDS`, re-launch, re-join, re-start media if it was active) up to `CHROMIUM_MAX_RESTARTS` times within `CHROMIUM_RESTART_WINDOW_SECONDS`. Log each recovery attempt at `warn` level. If recovery is exhausted, remain in `ERROR` and log at `error` level. Recovery sequence must be idempotent — safe to call from `IDLE` state after prior crash cleanup.
*Ref: ADR-014*

---

## pkg/controller

**US-C01:** Implement the HTTP server listening on the port configured by `ADMIN_PORT` (default 8080). All endpoints defined in ADR-005 must be served from this server.
*Ref: ADR-003, ADR-005*

**US-C02:** Implement `/healthz`: no auth, returns `{"status": "ok"}` with HTTP 200 whenever the server is responding.
*Ref: ADR-005*

**US-C03:** Implement `/readyz`: no auth. Map session state (ADR-014) to HTTP status: `CONNECTED` and `STREAMING` → 200 with `{"status":"ready",...}`; all other states → 503 with `{"status":"<state_name>",...}`. Include a `checks` object reporting `zoom_session`, `audio_pipeline`, `video_pipeline`, and `chromium` sub-statuses. Do not use this endpoint as the Docker HEALTHCHECK target — the HEALTHCHECK targets `/healthz` to avoid spurious container restarts after normal session end (ADR-005).
*Ref: ADR-005, ADR-014*

**US-C04:** Implement `/status` with Bearer token auth: return current session state, source info, and all seven pipeline metrics defined in ADR-002. Strip any password query parameter from `meeting_url` before serialization.
*Ref: ADR-002, ADR-005*

**US-C05:** Implement `/start`, `/stop`, `/config`, `/media` with Bearer token auth. Return consistent JSON error bodies on 401. Use `subtle.ConstantTimeCompare` for all token comparisons. `/start` accepts a JSON body `{"meeting_url": "...", "display_name": "..."}` — `meeting_url` is required; `display_name` is optional and overrides `BOT_DISPLAY_NAME`. Validate that `meeting_url` is a `zoom.us` HTTPS URL before calling `Join`; return HTTP 422 on invalid URL. `/stop` calls `Leave()` (if connected) then `reset()` to return to `IDLE`. Return HTTP 409 with a `StateError` body on invalid state transitions (ADR-014).
*Ref: ADR-005, ADR-014*

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

**US-D06:** Write `.env.example` documenting every variable listed in ADR-003 with descriptions and placeholder values. Variables to include: `BOT_DISPLAY_NAME`, `ADMIN_TOKEN`, `ADMIN_PORT`, `LOG_LEVEL`, `MEDIA_DIR`, `AUDIO_BUFFER_FRAMES`, `VIDEO_BUFFER_FRAMES`, `CHROMIUM_MAX_RESTARTS`, `CHROMIUM_RESTART_WINDOW_SECONDS`, `CHROMIUM_RESTART_DELAY_SECONDS`. Do not include `MEETING_URL` or `MEETING_PASSWORD` — these are runtime parameters of `POST /start`, not container configuration. Add `.env` to `.gitignore`.
*Ref: ADR-003, ADR-014*

**US-D07:** Commit `./media/.gitkeep` to the repository so the media directory exists before first run. Document in the README that this directory must not be deleted.
*Ref: ADR-003*

**US-D08:** Implement startup validation in `cmd/stenosaur/main.go` covering all checks listed in ADR-003: `BOT_DISPLAY_NAME` and `ADMIN_TOKEN` present and non-empty; `ADMIN_TOKEN` ≥ 16 characters; `MEDIA_DIR` readable; `ffmpeg` binary present and executable; `yt-dlp` binary present and executable; Chromium binary present; PulseAudio sink ready; HTTP server binds. Exit with a non-zero code and a specific actionable message on any failure. Note: `MEETING_URL` is not validated at startup — it is a runtime parameter of `POST /start`.
*Ref: ADR-003*

---

## Testing and CI

**US-T01:** Write an integration test that starts all three packages in-process, produces frames from `AudioSynth` and the SMPTE color bar source, and verifies that frames flow through the buffer and are consumed by the bot goroutine without underruns.
*Ref: ADR-001, ADR-002*

**US-T02:** Write a Playwright test that runs the join flow in headed mode first, then in headless mode. Both must complete without error against a test Zoom meeting. **Test account strategy:** a personal Zoom account is used for all live integration tests. The meeting URL and any password are stored in a local `.env.test` file that is never committed. CI does not run this test — it requires a live Zoom account and network access to `app.zoom.us`. Tag this test with `//go:build integration` so it is excluded from `go test ./...` by default and must be explicitly opted into with `-tags integration`.
*Ref: ADR-004*

**US-T03:** Add a CI step that captures stdout from the in-process integration test (US-T01), scans for the literal value of `ADMIN_TOKEN` from the test `.env`, and fails the build if it is found in any log line. Note: `MEETING_PASSWORD` is no longer an env var (ADR-003); the secret scan covers `ADMIN_TOKEN` only. If a test meeting URL with an embedded `pwd` parameter is used, the scan must also verify that the `pwd` value does not appear in any log line.
*Ref: ADR-003, ADR-005*

**US-T04:** Verify the container starts without `--privileged` and without a `devices:` block on Linux, macOS Docker Desktop, and Windows Docker Desktop. Document any host-specific findings.
*Ref: ADR-003*

**US-T05:** Set a 6-month calendar review for ADR-004. Log any Playwright selector breakage incident as a GitHub issue tagged `adr-004-trigger`. Three incidents within the review period trigger ADR-004b.
*Ref: ADR-004*

**US-T13:** Write unit tests for the session state machine (ADR-014). Test every valid transition produces the correct next state and log output. Test every invalid transition returns a `StateError` without side effects. Test that calling `Join` from `JOINING` or `STREAMING` returns an error immediately.
*Ref: ADR-014*

**US-T14:** Write an integration test for Chromium crash recovery. Simulate a Chromium A crash by sending SIGKILL to the Chromium process PID mid-session. Verify the state transitions to `ERROR`, recovery is attempted, and the bot re-joins and resumes streaming within `CHROMIUM_RESTART_WINDOW_SECONDS`. Verify the restart counter increments correctly and that the bot stays in `ERROR` after `CHROMIUM_MAX_RESTARTS` is exhausted.
*Ref: ADR-014*

**US-T06:** Verify the `.y4m` pixel format tag written by the video pipeline is `C420` (not `C420mpeg2` or `C420jpeg`) using a known-good `.y4m` file. Test that Chromium accepts and renders it at the correct frame rate.
*Ref: ADR-002*

---

## pkg/renderer — Stream Sources (ADR-010)

**US-R07:** Extend `FileDecoder` to detect whether its path argument is an HTTP/HTTPS URL. When a direct media URL is detected, invoke FFmpeg with the URL as input rather than a file path. The decoder must produce the same normalised audio and video output regardless of whether the source is a local file or a direct URL. No `yt-dlp` invocation is required for direct URLs.
*Ref: ADR-010*

**US-R08:** Implement `StreamDecoder` in `pkg/renderer/stream.go`. On `Start(ctx)`, invoke `yt-dlp --get-url --format "bestvideo[height<=720]+bestaudio/best[height<=720]" <page_url>` as a subprocess to resolve the stream URL(s). Pass resolved URL(s) to FFmpeg as inputs. Launch goroutines to read FFmpeg output and push normalised frames into `audioOut` and `videoOut` channels. Log the page URL at `info` level; never log the resolved stream URL (contains auth tokens).
*Ref: ADR-010*

**US-R09:** Implement `StreamDecoder` error handling: if `yt-dlp` exits non-zero, log the exit code and stderr at `error` level and return a descriptive error from `Start()`. If FFmpeg produces a 403 during streaming (stream URL TTL expired), log an `error` and return `io.EOF` from the relevant `ReadAudio`/`ReadVideo` call.
*Ref: ADR-010*

---

## pkg/renderer — Playlist (ADR-011)

**US-R10:** Define `TrackSpec` in `pkg/renderer`: a struct with mutually exclusive fields `FilePath string`, `StreamURL string`, `DirectURL string`. Exactly one field must be non-empty; validate at construction time and return an error if zero or more than one field is set.
*Ref: ADR-011*

**US-R11:** Implement `Playlist` in `pkg/renderer/playlist.go`. `Playlist` must implement `media.AudioSource` and `media.VideoSource`. It holds an ordered `[]TrackSpec` and a `ptsOffset time.Duration`. On first `ReadAudio`/`ReadVideo` call, construct the first track's source. When the current source returns `io.EOF`, advance to the next track: close the current source, increment `ptsOffset` by the last PTS plus one frame duration, construct the next source. When all tracks are exhausted and `loop: false`, return `io.EOF`. When `loop: true`, reset index to 0 and continue.
*Ref: ADR-011*

**US-R12:** Implement pre-loading in `Playlist`. When the current track's audio buffer depth falls below the low-water mark (50 frames) or the current source signals it is within 1 second of EOF, begin constructing the next track's source in a goroutine and buffering its frames into a staging buffer. On track advance, drain the staging buffer into the main ring buffer before switching the active source. Audio-only tracks must pair with `SMPTESource` for video.
*Ref: ADR-011*

---

## pkg/renderer — Ducking and Mixing (ADR-013)

**US-R13:** Implement `DuckingSource` in `pkg/renderer/ducking.go`. `DuckingSource` wraps any `media.AudioSource` and implements both `media.AudioSource` and `media.DuckingController`. On each `ReadAudio` call, advance the gain ramp by one frame (10 ms) toward `targetGain`, then multiply each sample in the frame by the current gain. `SetVADActive(true)` sets `targetGain = DUCK_GAIN`; `SetVADActive(false)` sets `targetGain = 1.0`. When `VAD_ENABLED=false`, `DuckingSource` must not be constructed; a no-op `DuckingController` must be passed to `pkg/bot` instead.
*Ref: ADR-013*

**US-R14:** Implement `Mixer` in `pkg/renderer/mixer.go`. `Mixer` wraps a primary `media.AudioSource` and an optional overlay `media.AudioSource` (nil when inactive). On `ReadAudio`: read a frame from primary; if overlay is nil, return the frame unmodified; if overlay is active, read a frame from overlay, apply `duckGain` to primary samples, add overlay samples with int32 promotion and hard clamp to int16 range, return the mixed frame. When overlay returns `io.EOF`, close it, set overlay to nil, and begin unduck ramp.
*Ref: ADR-013*

**US-R15:** Implement `Mixer.TriggerOverlay(source media.AudioSource)`. If an overlay is already active when triggered, close it immediately and replace it with the new source. Set `targetGain = DUCK_GAIN` at the moment of trigger; begin the unduck ramp when the overlay exhausts. `Mixer` must also implement `media.OverlayController` so `pkg/controller` can trigger overlays without importing `pkg/renderer`.
*Ref: ADR-013*

**US-R16:** Implement gain ramping in both `DuckingSource` and `Mixer`. Gain must change linearly over `DUCK_RAMP_MS` milliseconds (default 200 ms = 20 frames at 10 ms/frame). Gain must not jump instantaneously between values; abrupt changes produce audible clicks. The ramp step per frame is `(targetGain - currentGain) / remainingFrames`.
*Ref: ADR-013*

---

## pkg/media — New Interfaces (ADR-012, ADR-013)

**US-M07:** Define `DuckingController` interface in `pkg/media/controller.go`: one method, `SetVADActive(active bool)`. Define `OverlayController` interface in the same file: one method, `TriggerOverlay(source AudioSource) error`. Both interfaces must be in `pkg/media` so that `pkg/bot` and `pkg/controller` can reference them without importing `pkg/renderer`.
*Ref: ADR-012, ADR-013*

---

## pkg/bot — VAD and PulseAudio Output Isolation (ADR-012)

**US-B07:** Configure the PulseAudio entrypoint to provision a second null sink, `stenosaur_output_sink`, alongside the existing `stenosaur_sink`. After the Playwright join flow completes and Chromium has connected to meeting audio, use `pactl move-sink-input` to move Chromium's audio output stream to `stenosaur_output_sink`. Log a `warn` if the move fails; the session continues but VAD will be unavailable.
*Ref: ADR-012*

**US-B08:** Implement the VAD goroutine in `pkg/bot`. When `VAD_ENABLED=true`, launch a `pacat --record --device=stenosaur_output_sink.monitor --format=s16le --rate=48000 --channels=1` subprocess. Read 20 ms windows (960 samples) from its stdout. Compute RMS energy per window. If RMS exceeds `VAD_DUCK_THRESHOLD` for any window, call `duckingController.SetVADActive(true)`. If RMS remains below threshold for `VAD_RELEASE_MS` milliseconds, call `duckingController.SetVADActive(false)`. Stop the subprocess when `StopMediaStream` is called.
*Ref: ADR-012*

**US-B09:** Accept a `media.DuckingController` at `pkg/bot` construction time. When `VAD_ENABLED=false` or no ducking is configured, accept a no-op implementation that discards all calls. The bot package must not import `pkg/renderer`; all ducking interaction is through the `DuckingController` interface.
*Ref: ADR-012, ADR-013*

---

## Container and Deployment — New Dependencies (ADR-010, ADR-012, ADR-013)

**US-D09:** Add `ffmpeg` to the `Dockerfile` via `apt-get install -y --no-install-recommends ffmpeg`. Pin the `ffmpeg` version and document it in the Dockerfile. Add a startup validation check (US-D08 extension) that verifies `ffmpeg` is executable before proceeding.
*Ref: ADR-010*

**US-D10:** Add `yt-dlp` to the `Dockerfile` by downloading the pinned release binary from the `yt-dlp` GitHub releases page. Pin the version via a `YTDLP_VERSION` build arg. Make the binary executable at `/usr/local/bin/yt-dlp`. Document the pinned version in the Dockerfile comment.
*Ref: ADR-010*

**US-D11:** Add VAD and ducking environment variables to `.env.example` with descriptions: `VAD_ENABLED` (default `false`), `VAD_DUCK_THRESHOLD` (default `500`), `VAD_RELEASE_MS` (default `800`), `DUCK_GAIN` (default `0.15`), `DUCK_RAMP_MS` (default `200`). Add startup validation for each: numeric range check for threshold and gain; minimum `VAD_RELEASE_MS` of 100 ms.
*Ref: ADR-012, ADR-013*

---

## API Extensions (ADR-011, ADR-012, ADR-013)

**US-A01:** Extend `PATCH /config` to accept a `playlist` field: `{ "tracks": [ { "file_path": "..." } | { "stream_url": "..." } | { "direct_url": "..." } ], "loop": false }`. Validate: each track has exactly one non-empty path field; `file_path` values must resolve within `MEDIA_DIR`; `stream_url` values must be parseable URLs. On success, replace the active source atomically and return the new status. On validation failure, return HTTP 422 with a structured error body.
*Ref: ADR-011*

**US-A02:** Extend `POST /media` to accept an optional `action` field. When `action: "overlay"`, look up the file in `MEDIA_DIR`, construct a `FileDecoder`, and call `OverlayController.TriggerOverlay()` on the active source. If the active source does not implement `OverlayController`, return HTTP 422 with `{ "error": "overlay_not_supported" }`. If no `action` is specified, retain the existing behaviour (upload file to `MEDIA_DIR`).
*Ref: ADR-013*

**US-A03:** Extend `GET /status` to include a `playlist` object when a `Playlist` is the active source: `{ "track_index": N, "track_total": N, "current_track": "...", "loop": false }`. Include a `vad` object when `VAD_ENABLED=true`: `{ "enabled": true, "active": false, "rms_level": N, "duck_threshold": N }`. Include a `mixer` object when a `Mixer` is active: `{ "overlay_active": false, "primary_gain": 1.0, "overlay_track": null }`.
*Ref: ADR-011, ADR-012, ADR-013*

---

## Testing — New Source Types and Features

**US-T07:** Write unit tests for `StreamDecoder`: mock `yt-dlp` and FFmpeg subprocesses; verify that the page URL is logged and the resolved URL is not; verify that a non-zero `yt-dlp` exit returns an error from `Start()`; verify that FFmpeg 403 returns `io.EOF`.
*Ref: ADR-010*

**US-T08:** Write unit tests for `Playlist`: verify PTS continuity across track boundaries (no flush, monotonically increasing PTS); verify loop behaviour resets index without resetting PTS; verify pre-loading begins at the low-water mark; verify audio-only tracks produce SMPTE video frames.
*Ref: ADR-011*

**US-T09:** Write unit tests for `DuckingSource`: verify gain ramps linearly over `DUCK_RAMP_MS`; verify no instantaneous gain jump on `SetVADActive` calls; verify samples are correctly scaled by `DUCK_GAIN` when fully ducked; verify no gain multiplication occurs when gain is 1.0.
*Ref: ADR-013*

**US-T10:** Write unit tests for `Mixer`: verify overlay frames are mixed into ducked primary frames with correct int32 promotion and clamping; verify hard clipping at ±32767; verify overlay EOF triggers unduck ramp; verify `TriggerOverlay` replaces an active overlay immediately.
*Ref: ADR-013*

**US-T11:** Write a VAD integration test: feed a known PCM signal above `VAD_DUCK_THRESHOLD` into the VAD goroutine via a mock `pacat` subprocess; verify `SetVADActive(true)` is called within one window period; feed silence; verify `SetVADActive(false)` is called after `VAD_RELEASE_MS`.
*Ref: ADR-012*

**US-T12:** Write an end-to-end test for the overlay trigger path: configure a `Mixer` over a `Playlist` source; call `POST /media` with `action: overlay`; verify the `/status` response shows `overlay_active: true`; verify the overlay source exhausts and `overlay_active` returns to `false`.
*Ref: ADR-013*
