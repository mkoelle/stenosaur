# Stenosaur: Path to the First Vertical Slice

The vertical slice is the thinnest path through every layer of the system that produces a real, verifiable outcome: **a named bot appears in a Zoom meeting and injects a 440 Hz sine tone and SMPTE color bars that meeting participants can see and hear.**

This document defines the slice precisely, explains why each step is ordered the way it is, and gives the acceptance test.

---

## What the Slice Proves

A working vertical slice validates six things that nothing else can:

1. The container boots and PulseAudio initialises correctly inside it.
2. Playwright can drive Chromium to join a real Zoom meeting.
3. The `.y4m` FIFO and `pacat` injection paths each carry frames that Zoom accepts as a valid camera and microphone.
4. The audio ring buffer and video FIFO queue drain at the correct rates without underrun or overflow under steady-state conditions.
5. The structured logger, `/healthz`, and `/readyz` endpoints work end-to-end.
6. The bot shuts down cleanly without panics or goroutine leaks.

All of this is worth proving before touching FFmpeg, `yt-dlp`, playlist logic, or VAD — those features add complexity on top of a foundation that may not yet be solid.

---

## What the Slice Deliberately Excludes

- File decoding (FFmpeg) — the `AudioSynth` sine wave and SMPTE source are in-process and need no external binary.
- The full REST API (`/start`, `/stop`, `/config`, `/media`) — the bot is started programmatically in the test; the admin UI is not required for the slice to prove out.
- PTS drift correction — the slice runs long enough to verify steady-state, not long enough to accumulate meaningful drift.
- Any renderer source other than `AudioSynth` and SMPTE bars.

These are all Phase 2+ stories. The slice must not expand to include them.

---

## Implementation Order

The order below is a strict dependency chain. Each step has a clear "done" signal before the next begins.

### Step 1 — `pkg/media`: Frame types and interfaces

**Stories:** US-M01, US-M02, US-M03, US-M04

Write `AudioFrame`, `VideoFrame`, `AudioSource`, `VideoSource`, `AudioBuffer`, and `VideoBuffer`. These are pure Go with no external dependencies. Write unit tests for the buffer behaviours (underrun silence, overrun discard, watermark logs) immediately — these tests must pass before anything downstream is written.

**Done when:** `go test ./pkg/media/...` passes. Buffer watermark logs appear in test output at the correct thresholds.

---

### Step 2 — `pkg/controller`: Logger and health endpoints

**Stories:** US-C07, US-C01, US-C02, US-D06

Write the structured logger (`log/slog` wrapper with `component`, `ts`, `level`, `msg`, `ctx` fields and password-stripping). Write the HTTP server and `/healthz`. The logger is a shared dependency of all three other packages; establishing it before any of them prevents later refactoring.

**Done when:** `GET /healthz` returns `{"status":"ok"}`. Logger output matches the ADR-005 schema in a unit test.

---

### Step 3 — Container skeleton

**Stories:** US-D01, US-D02, US-D03, US-D04, US-D05, US-D07, US-D08

Write the Dockerfile (Playwright base image, PulseAudio, entrypoint script), `docker-compose.yml`, `.env.example`, and startup validation. Run `docker compose up` and verify that the container reaches a healthy state — `/healthz` passes the Docker HEALTHCHECK.

PulseAudio must be verified ready in this step. The `module-null-sink` (stenosaur_sink) and `module-remap-source` (stenosaur_mic) must both be provisioned. Verify with `pactl info` from inside the running container.

**Done when:** `docker compose up` → container status `healthy`. `docker exec <ctr> pactl list sinks short` shows `stenosaur_sink`. `docker exec <ctr> pactl list sources short` shows `stenosaur_mic`.

---

### Step 4 — `pkg/renderer`: Synthetic sources

**Stories:** US-R03, US-R06, US-R04, US-R05

Write `AudioSynth` (440 Hz sine wave generator) and `SMPTESource` (static YUV420p color bar frame). Write the frame-repeat logic (US-R04) and the source-switch flush/PTS-reset (US-R05). These are pure Go with no subprocess dependencies.

Write the in-process integration test (US-T01) in this step: wire `AudioSynth` → `AudioBuffer` → mock consumer, and `SMPTESource` → `VideoBuffer` → mock consumer. Run for 5 seconds. Assert zero underruns.

**Done when:** US-T01 passes. Buffer depth stays above the low-water mark throughout the 5-second run.

---

### Step 5 — `.y4m` format verification

**Story:** US-T06

Before writing any Playwright code, verify the `.y4m` stream format independently. Write a small Go program (or test) that opens a FIFO, writes the correct `.y4m` header (`C420`, 1280×720, F30:1), and writes 30 SMPTE color bar frames. Use `ffplay` or `ffprobe` inside the container to verify the pipe is readable and the pixel format is `C420`. This test is cheap to run and prevents a class of silent injection failures.

**Done when:** `ffprobe` reports `yuv420p` and `30 fps` from the FIFO. `ffplay` renders the color bars correctly.

---

### Step 6 — `pkg/bot`: ZoomClient interface and PWA join flow

**Stories:** US-B01, US-B02, US-B04, US-B05

Define the `ZoomClient` interface (US-B01) as the first piece of `pkg/bot`. Then implement `PWAClient`, the Playwright-backed implementation, covering the join flow: launch Chromium with fake device flags (`--use-fake-device-for-media-stream`, `--use-file-for-fake-video-capture`), navigate to `app.zoom.us/wc/join/<meeting_id>`, handle display name entry, audio/video permission prompts, and waiting room.

Develop this step in headed Playwright mode first (`video: 'on'`). The Playwright session recording is the primary debugging tool when the join flow breaks. Do not attempt headless until headed mode works reliably.

Add version comments to every Playwright selector (US-B04) from the start — retrofitting them later is error-prone.

**Done when:** The bot appears as a named participant in a real Zoom meeting in headed Chromium mode. The Playwright recording confirms the full join flow completed without selector timeouts.

---

### Step 7 — `pkg/bot`: Audio and video injection

**Story:** US-B03

With the join flow working, wire the media injection paths:

**Video (FIFO pipe):** create the FIFO at startup, pass the path to Chromium via `--use-file-for-fake-video-capture`, open the write end after Chromium opens the read end, write the `.y4m` file header once, then loop writing `VideoFrame` data from the `VideoBuffer` at 30 FPS using `time.Ticker`.

**Audio (PulseAudio / pacat):** launch `pacat --playback --device=stenosaur_sink --format=s16le --rate=48000 --channels=1 --latency-msec=40` as a subprocess. Write raw int16 PCM from `AudioBuffer` pops to `pacat`'s stdin. No WAV header.

Add the panic recovery wrapper (US-B06) to the bot goroutine before testing injection; any panic in the injection loop must log an error and not silently stop the goroutine.

**Done when:** Inside the Zoom meeting, the bot's video shows SMPTE color bars and the bot's audio produces an audible 440 Hz tone. Confirm via a second participant in the meeting.

---

### Step 8 — `/readyz` and observability

**Stories:** US-C03, US-M05, US-M06

With injection working, implement `/readyz`. It checks: Zoom session connected, `pacat` subprocess running, video FIFO write goroutine running, audio buffer depth above critical mark. Return 503 if any check fails.

Implement PTS drift detection (US-M05) and video delivery timing check (US-M06). Let the synthetic source run for 60 seconds and verify the drift metrics stay below the warn threshold.

**Done when:** `/readyz` returns 200 while the bot is in session and 503 after `StopMediaStream`. No warn-level drift logs appear in a 60-second synthetic run.

---

### Step 9 — Headless mode verification

**Stories:** US-B05, US-T04

Verify the full slice in headless Chromium mode — the production configuration. Verify the container runs without `--privileged` on the target platforms.

**Done when:** The bot joins in headless mode; audio and video injection verified by a second participant. Container starts without `--privileged` on Linux Docker.

---

## Slice Acceptance Test

The following test procedure defines "the vertical slice is done":

1. `docker compose up -d` on a Linux host without `--privileged`.
2. `curl -s http://localhost:8080/healthz` → `{"status":"ok"}`.
3. Set `MEETING_URL` to a real Zoom meeting link in `.env`. Start the session programmatically (in Phase 1 this is acceptable via a test binary or a direct `go test` call against the running container; the full `/start` API comes in Phase 3).
4. `curl -s http://localhost:8080/readyz` → HTTP 200.
5. A second participant joins the meeting. They observe:
   - Bot named correctly (matching `BOT_DISPLAY_NAME`).
   - Bot's video: SMPTE color bars.
   - Bot's audio: 440 Hz sine tone, audible and steady.
6. Stop the session. `curl -s http://localhost:8080/readyz` → HTTP 503.
7. `docker compose logs stenosaur` contains no `panic`, no `error`-level log lines, and no occurrences of the `ADMIN_TOKEN` value.
8. `docker stats stenosaur --no-stream` shows memory below 512 MB (Playwright + Chromium + Node.js runtime; this is expected and acceptable).

---

## What to Build Next After the Slice

Once the slice passes its acceptance test:

- **Phase 2 (File Playback):** Add `ffmpeg` to the Dockerfile and implement `FileDecoder`. This is the highest-value feature after the slice — it unlocks actual use.
- **Phase 3 (API and Admin UI):** Implement the full REST API and admin UI so the bot can be operated without code changes.

Do not start Phase 4 (External Streams) or Phase 5 (Playlist) until Phase 3 is complete. The API must be stable before adding source types that require new `/config` schema fields.
