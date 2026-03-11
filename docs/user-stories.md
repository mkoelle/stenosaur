# Stenosaur: Implementation User Stories

This document contains implementation detail extracted from the ADR set. Each story represents concrete work to be IN PROGRESS. Stories include their current implementation status and a **Definition of IN PROGRESS (DOD)** for verification.

---

## pkg/media

**US-M01: Define Media Frame Structs**
- **Status**: [IN PROGRESS]
- **Description**: Define `AudioFrame` (480 samples, 16-bit PCM) and `VideoFrame` (YUV420p) structs in `pkg/media`.
- **DOD**: 
    - Structs exist and conform to ADR-002.
    - `NewVideoFrame` allocates correctly sized planes.
    - Unit tests verify allocation.

**US-M02: Define Source Interfaces**
- **Status**: [IN PROGRESS]
- **Description**: Define `AudioSource` and `VideoSource` interfaces. `pkg/media` must have no internal imports.
- **DOD**:
    - Interfaces defined in `pkg/media/source.go`.
    - No circular dependencies or internal imports in `pkg/media`.

**US-M03: Implement Audio Ring Buffer**
- **Status**: [IN PROGRESS]
- **Description**: 200-frame capacity ring buffer. Silence on underrun, discard oldest on overrun.
- **DOD**:
    - `AudioBuffer` implements logic from ADR-002.
    - Metrics `UnderrunTotal` and `OverrunTotal` are incremented.
    - Unit tests verify buffering behavior, silence emission, and metric counters.

**US-M04: Implement Video FIFO Queue**
- **Status**: [TODO]
- **Description**: 10-frame capacity FIFO. Repeat last on underrun, discard oldest on overrun.
- **DOD**:
    - `VideoBuffer` implements logic from ADR-002.
    - Metrics are incremented.
    - Unit tests verify buffering and repetition logic.
    - **Verification**: Ensure the `.y4m` format tag `C420` is respected (from US-T06).

**US-M05: Implement PTS-based Drift Detection**
- **Status**: [TODO]
- **Description**: Compare PTS against wall-clock in the consumer (bot). Log `warn` at 40ms, `error` at 200ms.
- **DOD**:
    - Log entries appear when thresholds are crossed.
    - Frame drop/duplicate logic for resync is implemented.
    - Verified via simulation in unit tests.

**US-M06: Video Delivery Timing Check**
- **Status**: [TODO]
- **Description**: Log `warn` when wall-clock delta between video frames exceeds 33.3ms.
- **DOD**:
    - Log entry emitted on timing violations.
    - Verified by artificial delay in integration tests.

---

## pkg/renderer

**US-R01: WebRenderer Implementation**
- **Status**: [TODO]
- **Description**: Open Chromium context, navigate to URL, capture YUV420p frames at 30 FPS.
- **DOD**:
    - Functional browser capture in `pkg/renderer/web.go`.
    - Captured frames match ADR-002 dimensions (1280x720).
    - Verified by rendering a moving test page and checking frame flow.

**US-R02: FileDecoder Implementation**
- **Status**: [TODO]
- **Description**: Decode audio/video files via FFmpeg to ADR-002 formats.
- **DOD**:
    - `pkg/renderer/file.go` can decode `.mp4` and `.wav` files.
    - Verified by decoding a sample file and comparing output frame count.

**US-R03: AudioSynth implementation**
- **Status**: [IN PROGRESS]
- **Description**: Synthesize 440 Hz sine wave test tone.
- **DOD**:
    - `AudioSynth` produces valid PCM samples.
    - Unit tests verify frequency calculation and PTS monotonicity.

**US-R04: Renderer Repeat Logic**
- **Status**: [TODO]
- **Description**: If a source is slow, ensure the pipeline receives a repeated frame to maintain 30 FPS.
- **DOD**:
    - Pipeline maintains constant timestamp progression even if producer lags.
    - Verified by stress-testing the renderer in integration tests.

**US-R05: Buffer Flush on Switch**
- **Status**: [TODO]
- **Description**: Flush audio/video buffers and reset PTS origin when switching sources.
- **DOD**:
    - `AudioBuffer.Flush()` and `VideoBuffer.Flush()` are called by the coordinator.
    - No "audio pop" or visual stutter on source switch.

**US-R06: SMPTE Color Bar Source**
- **Status**: [IN PROGRESS]
- **Description**: Default YUV420p SMPTE color bar frame for audio-only sources.
- **DOD**:
    - Pre-rendered frame is correctly formatted.
    - **Verification**: Chromium renders the bars without artifacts in a test meeting.

**US-R07: Pipeline Coordinator**
- **Status**: [TODO]
- **Description**: Internal logic to manage active sources and pipe them into the buffers.
- **DOD**:
    - Can transition between `WebRenderer` and `AudioSynth` programmatically.
    - Verified by an integration test flowing frames from renderer to bot (from US-T01).

---

## pkg/bot

**US-B01: Define ZoomClient Interface**
- **Status**: [IN PROGRESS]
- **Description**: Define interface for session lifecycle (`Join`, `StartMediaStream`, `Leave`).
- **DOD**:
    - Interface in `pkg/bot/client.go` abstracts Playwright details.

**US-B02: PWA Join Flow**
- **Status**: [TODO]
- **Description**: Playwright logic to join `app.zoom.us`, handle name entry, and waiting rooms.
- **DOD**:
    - Bot successfully appears in a Zoom meeting.
    - **Verification**: Run join flow in both headed and headless modes (from US-T02).
    - Selector versioning comments present (US-B04).

**US-B03: Fake Media Injection**
- **Status**: [TODO]
- **Description**: Launch Chromium with `--use-fake-device-for-media-stream` and named pipe flags.
- **DOD**:
    - Chromium recognizes the PulseAudio sink and Y4M pipe.
    - Audio/Video produced by renderer appears in the meeting.

**US-B04: Selector Governance**
- **Status**: [TODO]
- **Description**: Annotate every selector with Zoom version. Log `warn` on timeout.
- **DOD**:
    - No selectors without version annotations.
    - CI tracks timeout incidents for ADR-004 reviews (US-T05).

**US-B05: Session Recording**
- **Status**: [TODO]
- **Description**: Capture Playwright video in non-production builds for debugging.
- **DOD**:
    - Video file saved to `/app/recordings` when enabled.

**US-B06: Panic Recovery**
- **Status**: [IN PROGRESS]
- **Description**: `recover` wrappers for all bot goroutines.
- **DOD**:
    - Panic in a bot goroutine logs an `error` but doesn't kill the whole process.

---

## pkg/controller

**US-C01: Admin HTTP Server**
- **Status**: [IN PROGRESS]
- **Description**: Serve API and UI on `ADMIN_PORT`.
- **DOD**:
    - Server listens on configured port.

**US-C02: Health Endpoints**
- **Status**: [IN PROGRESS]
- **Description**: Implement `/healthz` (200 OK).
- **DOD**:
    - `curl /healthz` returns `{"status":"ok"}`.

**US-C03: Readiness Endpoint**
- **Status**: [IN PROGRESS]
- **Description**: Implement `/readyz` checking Zoom, Pipeline, and Chromium state.
- **DOD**:
    - Returns 503 if bot is disconnected or Chromium crashed.
    - Returns 200 only when pipeline is functional and bot is in-meeting.

**US-C04: Status API**
- **Status**: [IN PROGRESS]
- **Description**: Authenticated `/status` returning session state and pipeline metrics.
- **DOD**:
    - JSON includes all 7 metrics from ADR-002.
    - No passwords in the response.

**US-C05: Control API**
- **Status**: [TODO]
- **Description**: Implement `/start`, `/stop`, `/config`, and `/media` endpoints.
- **DOD**:
    - POST `/start` initiates Zoom join.
    - Constant-time comparison used for tokens.

**US-C06: Log WebSocket**
- **Status**: [TODO]
- **Description**: authenticated `/logs` stream.
- **DOD**:
    - Client receives live JSON logs in real-time.

**US-C07: Structured Logging & Secret Redaction**
- **Status**: [IN PROGRESS]
- **Description**: Schematized JSON logs. Redact passwords and tokens.
- **DOD**:
    - Logs conform to ADR-005.
    - **Verification**: CI job scans logs for `ADMIN_TOKEN` and fails on leak (from US-T03).

**US-C08: Web Admin UI**
- **Status**: [TODO]
- **Description**: Dashboard rendered via Templ with HTMX/Alpine for live updates.
- **DOD**:
    - Dashboard visible at root `/`.
    - Metrics update without full page refresh.

---

## Container and Deployment

**US-D01: Docker Environment**
- **Status**: [IN PROGRESS]
- **Description**: Multi-stage build with Chromium and PulseAudio.
- **DOD**:
    - Image builds and runs on Linux/macOS/Windows (US-T04).
    - `HEALTHCHECK` directive present (US-D04).

**US-D02: Entrypoint Sequence**
- **Status**: [IN PROGRESS]
- **Description**: Start PulseAudio null sink and wait for readiness.
- **DOD**:
    - `pactl info` confirms sink is ready before Go binary starts.

**US-D03: Startup Validation**
- **Status**: [IN PROGRESS]
- **Description**: Validate env vars, file paths, and external dependencies on launch.
- **DOD**:
    - Process exits with clear error if `ADMIN_TOKEN` is too short or `MEDIA_DIR` is missing.
    - Verified by running container with invalid `.env`.

**US-D04: Media Directory Management**
- **Status**: [IN PROGRESS]
- **Description**: Ensure `./media` exists and is mounted correctly.
- **DOD**:
    - `.gitkeep` ensures directory exists on clone.
    - `docker-compose.yml` mounts it as a volume.
