# Stenosaur: Project Plan

This document organises the implementation user stories (USER-STORIES.md) into phases with milestones, dependency ordering, and acceptance criteria per phase. The vertical slice through Phase 1 is described separately in VERTICAL-SLICE.md.

---

## Phase Overview

| Phase | Name | Goal | Milestone |
|---|---|---|---|
| 0 | Foundation | Container boots; pipeline compiles; test harness runs | Green CI on empty pipeline |
| 1 | Vertical Slice | Bot joins a meeting and injects synthetic audio/video | Single meeting session with verifiable A/V |
| 2 | File Playback | Play a local audio or video file in a meeting | MP3/MP4 file plays end-to-end |
| 3 | API and Admin UI | Full REST API and web admin UI operational | Operator can manage a session without CLI |
| 4 | External Streams | Stream audio/video from a URL or YouTube | YouTube video plays in a meeting |
| 5 | Playlist | Queue and sequence multiple tracks | Gapless playlist transitions |
| 6 | VAD Ducking | Duck injected audio when participants speak | Meeting volume drives duck signal |
| 7 | Mixing and Overlays | Layer sound effects over primary stream | `POST /media` triggers overlay |
| 8 | Hardening | Production readiness, secret scanning, CI polish | All tests green on target platforms |

---

## Phase 0: Foundation

**Goal:** The container builds, starts, and reaches a healthy state. The Go pipeline compiles. Unit tests for `pkg/media` run in CI.

**Stories:** US-D01, US-D02, US-D04, US-D05, US-D06, US-D07, US-D08, US-M01, US-M02, US-M03, US-M04, US-C01, US-C02, US-C07

**Dependency notes:**
- US-D01 (Dockerfile) must precede all container stories.
- US-M01 and US-M02 (frame types and interfaces) must be the first Go code written; every other package imports from `pkg/media`.
- US-C07 (structured logger) must be available before any package emits logs.

**Acceptance criteria:**
- `docker compose up` produces a running container that passes `/healthz`.
- `go test ./pkg/media/...` passes with no failures.
- `go build ./...` completes without errors.
- `.env.example` documents every required variable.
- `./media/.gitkeep` is committed; the media volume mounts correctly.

---

## Phase 1: Vertical Slice

**Goal:** The bot joins a real Zoom meeting and injects a 440 Hz sine wave (audio) and SMPTE color bars (video) that participants can see and hear. This is the thinnest path through every layer of the system.

See **VERTICAL-SLICE.md** for the detailed path, sequencing rationale, and acceptance test.

**Stories:** US-M05, US-M06, US-R03, US-R04, US-R05, US-R06, US-B01, US-B02, US-B03, US-B04, US-B05, US-B06, US-C03, US-D03, US-T01, US-T06

**Dependency notes:**
- US-B01 (ZoomClient interface) must be written before US-B02 (PWA implementation) so the interface is the contract, not the implementation.
- US-B03 (Chromium flags + pacat + FIFO) depends on PulseAudio being provisioned (US-D02/US-D03) and the bot goroutine existing (US-B02).
- US-R03 (AudioSynth) and US-R06 (SMPTE source) can be implemented in parallel once US-M01 is done.
- US-T01 (in-process integration test) can be written as soon as US-R03 and the buffer layer exist; it should pass before US-B02 is attempted.
- US-T06 (`.y4m` format tag verification) is a prerequisite for US-B03; it prevents a class of silent injection failures.

**Acceptance criteria:**
- A named bot appears in a Zoom meeting room.
- Participants hear a 440 Hz sine tone from the bot.
- Participants see SMPTE color bars from the bot.
- The bot leaves cleanly when `StopMediaStream` is called.
- `/readyz` returns 200 while the session is active.
- No panic or goroutine leak on normal shutdown.

---

## Phase 2: File Playback

**Goal:** The bot can play a local MP3, WAV, or MP4 file in a meeting. The operator configures the file path via environment variable or API; playback begins at session start.

**Stories:** US-R02, US-D09 (ffmpeg only), US-T06 (extended to file sources)

**Dependency notes:**
- US-D09 (`ffmpeg` in Dockerfile) is a hard dependency for US-R02.
- US-R02 (`FileDecoder`) depends on US-M01 and US-M02 (frame types and interfaces).
- US-R05 (source switch flush + PTS reset) is required before the operator can switch from `AudioSynth` to `FileDecoder` at runtime; it may have been implemented in Phase 1 but must be verified here with a real source switch.

**Acceptance criteria:**
- A local MP3 file plays in a Zoom meeting with correct tempo and no audible dropouts.
- A local MP4 file plays video and audio in sync.
- A file with a different sample rate (e.g., 44.1 kHz) is correctly resampled to 48 kHz without distortion.
- A video with resolution other than 1280×720 is correctly scaled.
- The pipeline metrics (`audio_buffer_depth`, `audio_underrun_total`) are correct during playback.

---

## Phase 3: API and Admin UI

**Goal:** An operator can manage a session entirely through the web admin UI and REST API — no CLI access required. All ADR-005 endpoints are implemented and authenticated. The WebSocket log stream works.

**Stories:** US-C04, US-C05, US-C06, US-T02 (API coverage), US-T03 (secret scan)

**Dependency notes:**
- US-C04 (`/status`) depends on Phase 1 pipeline metrics being emitted.
- US-C05 (`/start`, `/stop`, `/config`, `/media`) depends on US-B01 (ZoomClient interface) being stable enough that the controller can call `Join` and `Leave`.
- US-C06 (`/logs` WebSocket) depends on US-C07 (structured logger) from Phase 0 being piped to a broadcast channel that the WebSocket handler can read from.
- US-T03 (secret scan CI step) should be added as soon as US-C07 is in place; it does not need to wait for the full API.

**Acceptance criteria:**
- All ADR-005 endpoints respond correctly with and without a valid `Authorization: Bearer` header.
- `POST /start` joins a meeting; `POST /stop` leaves it.
- `PATCH /config` accepts a new source configuration and applies it without restarting the session.
- `POST /media` uploads a file to `MEDIA_DIR`.
- `/logs` WebSocket streams JSON log lines in real time.
- The web admin UI renders correctly; start/stop controls work via HTMX.
- CI fails if `ADMIN_TOKEN` or `MEETING_PASSWORD` appear in any log output.

---

## Phase 4: External Streams

**Goal:** The bot can stream audio and video from a YouTube URL or any direct media URL. `StreamDecoder` resolves the URL via `yt-dlp` and decodes via FFmpeg.

**Stories:** US-R07, US-R08, US-R09, US-D10 (yt-dlp), US-T07, US-A01 (stream_url support in /config)

**Dependency notes:**
- US-D10 (`yt-dlp` in Dockerfile) is a hard dependency for US-R08.
- US-R07 (URL extension to `FileDecoder`) is independent of `yt-dlp` and can be implemented as soon as Phase 2 is done.
- US-R08 (`StreamDecoder`) depends on US-D10 and US-R07 for the FFmpeg path.
- US-A01 (playlist `/config` schema) needs the `stream_url` track type; implement the schema validation even if only `file_path` tracks are supported in Phase 2.

**Acceptance criteria:**
- A YouTube video URL plays audio and video in a Zoom meeting.
- A direct MP3 URL (e.g., a podcast feed) plays without downloading to disk first.
- The page URL is logged at `info`; the resolved stream URL is not present in any log line.
- `yt-dlp` failure produces an `error` log and a clean session stop, not a panic.
- Stream URL expiry (simulated 403) produces `io.EOF` and a clean stop.

---

## Phase 5: Playlist

**Goal:** The bot can play an ordered sequence of tracks (files and/or stream URLs) without audible gaps between tracks. The operator manages the playlist via `PATCH /config`.

**Stories:** US-R10, US-R11, US-R12, US-T08, US-A01 (full playlist schema), US-A03 (playlist status field)

**Dependency notes:**
- US-R10 (`TrackSpec`) is a value type; implement it before US-R11.
- US-R11 (`Playlist`) depends on US-R02 (`FileDecoder`) and US-R08 (`StreamDecoder`).
- US-R12 (pre-loading) depends on US-R11 being stable and tested; it adds complexity to the advance path.
- US-A03 (`/status` playlist field) depends on the `Playlist` type being queryable for its current index and track count.
- US-T08 (playlist unit tests) should be written before US-R12 is implemented; the tests define the PTS continuity invariant.

**Acceptance criteria:**
- A three-track local file playlist plays all tracks in order with no audible silence between tracks.
- A mixed playlist (file, stream URL) transitions correctly; pre-loading absorbs `StreamDecoder` startup latency.
- `PATCH /config` with a new playlist replaces the current one; the current track finishes its current frame before the switch.
- Loop mode cycles the playlist indefinitely without PTS reset or buffer flush.
- `/status` reports `track_index`, `track_total`, and `current_track` accurately.

---

## Phase 6: VAD Ducking

**Goal:** The bot automatically reduces injected audio volume when meeting participants speak. The operator enables this feature via `VAD_ENABLED=true`.

**Stories:** US-M07 (DuckingController interface), US-B07 (output sink), US-B08 (VAD goroutine), US-B09 (bot accepts DuckingController), US-R13 (DuckingSource), US-D11 (VAD env vars), US-T11, US-A03 (VAD status field)

**Dependency notes:**
- US-M07 must be implemented before US-B08 or US-R13, since both reference `media.DuckingController`.
- US-B07 (PulseAudio output sink + `pactl move-sink-input`) must be tested before US-B08; the VAD goroutine reads from the output sink's monitor.
- US-R13 (`DuckingSource`) depends on US-R11 or any other `AudioSource` to wrap; it can be implemented as soon as US-M07 is done.
- US-B09 (bot accepts `DuckingController`) is a refactor of `pkg/bot` construction; implement after US-B01 is stable.
- US-T11 (VAD integration test) requires a mock `pacat --record` subprocess or an in-process signal generator feeding the VAD goroutine directly.

**Acceptance criteria:**
- With `VAD_ENABLED=true`, the bot's injected audio volume drops when a participant speaks and recovers after `VAD_RELEASE_MS` of silence.
- With `VAD_ENABLED=false`, no `pacat --record` subprocess is launched; no gain multiplication occurs.
- The feedback loop is absent: the bot does not duck its own injected audio.
- `/status` shows `vad.active` transitioning correctly during a test.
- Gain transitions are smooth (no audible clicks) over the configured `DUCK_RAMP_MS`.

---

## Phase 7: Mixing and Overlays

**Goal:** The operator can trigger a sound effect over the primary stream via `POST /media`. The primary stream is ducked while the overlay plays and recovers when the overlay exhausts.

**Stories:** US-M07 (OverlayController, if not already done), US-R14 (Mixer), US-R15 (TriggerOverlay), US-R16 (gain ramping), US-T10, US-T12, US-A02 (POST /media overlay action), US-A03 (mixer status field)

**Dependency notes:**
- US-R14 (`Mixer`) depends on any two `AudioSource` instances; implement after Phase 5 so `Playlist` is available as a real primary source.
- US-R15 (`TriggerOverlay` + `OverlayController`) depends on US-M07 for the interface definition.
- US-R16 (gain ramping) applies to both `DuckingSource` and `Mixer`; implement consistently across both. If Phase 6 is done first, `DuckingSource` already has the ramp; `Mixer` reuses the same pattern.
- US-A02 (`POST /media` overlay action) depends on `OverlayController` being in `pkg/media` so `pkg/controller` can reference it.

**Acceptance criteria:**
- `POST /media` with `action: overlay` causes the primary stream to duck and the overlay audio to play.
- The overlay exhausts naturally; the primary stream ramps back to full volume.
- A second `POST /media overlay` while an overlay is active replaces the current overlay immediately.
- `/status` shows `mixer.overlay_active` toggling correctly.
- `POST /media overlay` against a source that is not a `Mixer` returns HTTP 422.

---

## Phase 8: Hardening

**Goal:** The system is production-ready. All platform targets pass. Secret scanning is enforced. ADR-004 monitoring is in place.

**Stories:** US-T02, US-T03, US-T04, US-T05

**Acceptance criteria:**
- Container starts without `--privileged` and without `devices:` on Linux, macOS Docker Desktop, and Windows Docker Desktop.
- CI fails on any log line containing `ADMIN_TOKEN` or `MEETING_PASSWORD` values.
- A 6-month ADR-004 review is calendared; GitHub issue template for `adr-004-trigger` is in place.
- All existing tests remain green after hardening changes.

---

## Cross-Cutting Dependencies

```
pkg/media (US-M01, US-M02, US-M07)
    └── required by everything

pkg/renderer sources
    FileDecoder (US-R02)     → requires ffmpeg (US-D09)
    StreamDecoder (US-R08)   → requires yt-dlp (US-D10), FileDecoder URL path (US-R07)
    Playlist (US-R11)        → requires FileDecoder + StreamDecoder
    DuckingSource (US-R13)   → requires DuckingController interface (US-M07)
    Mixer (US-R14)           → requires any AudioSource; best after Playlist (Phase 5)

pkg/bot
    ZoomClient interface (US-B01)
    PWA join flow (US-B02)   → requires B01
    Media injection (US-B03) → requires B02, D02/D03
    VAD goroutine (US-B08)   → requires B07 (output sink), M07 (DuckingController)

pkg/controller
    HTTP server (US-C01)     → Phase 0
    Full API (US-C05)        → requires stable ZoomClient (B01)
    Overlay trigger (US-A02) → requires OverlayController in pkg/media (US-M07)

Container
    ffmpeg (US-D09)          → required before Phase 2
    yt-dlp (US-D10)          → required before Phase 4
    VAD env vars (US-D11)    → required before Phase 6
```

---

## Risk Register

| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| Zoom Web App selectors break | High | Blocks Phase 1+ | US-B04 (version-comment selectors); US-T05 (ADR-004-trigger tracking) |
| `yt-dlp` extraction breaks | Medium | Blocks Phase 4 | Pin version; update on breakage; track as known maintenance cost |
| PulseAudio sink-input move unreliable | Low–Medium | Blocks Phase 6 | US-B07 logs warn and continues; VAD gracefully absent rather than broken |
| `pacat` subprocess unreliable | Low | Degrades Phase 1 audio | ADR-008 Option A (FIFO) documented as fallback |
| Stream URL TTL expires mid-session | Medium | Degrades Phase 4+ | Log and return EOF; playlist advances to next track naturally |
| FFmpeg version incompatibility | Low | Blocks Phase 2 | Pin version in Dockerfile; test in CI |
