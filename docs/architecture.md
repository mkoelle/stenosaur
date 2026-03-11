# Stenosaur: Architecture & Design

## Overview

**Stenosaur** is a containerized Zoom bot that:

1. Joins a Zoom meeting unattended, appearing as a named participant.
2. Injects audio from a configurable source (webpage, file, or synthesized tone).
3. Injects video rendered from a configurable webpage or file.
4. Exposes a web admin UI and HTTP API for configuration, start/stop control, and live monitoring.

The application runs as a **single Go process in a single Docker container**. Internal concerns are separated into packages (`pkg/bot`, `pkg/renderer`, `pkg/controller`, `pkg/media`) that communicate via in-process Go channels. No inter-container networking, virtual host devices, or kernel modules are required.

Zoom integration uses the **Zoom Web App** (`app.zoom.us`) via Playwright browser automation — no Zoom App Marketplace registration required. This is an explicitly temporary decision; the Zoom Meeting SDK is the preferred long-term path and should be adopted if the registration constraint is relaxed (see ADR-004).

**Language:** Go 1.21+, using `playwright-community/playwright-go` for browser automation. The playwright-go binding wraps the upstream Node.js Playwright driver via an RPC bridge; the container ships both the Go binary and a bundled Node.js runtime.

---

## Container Architecture

```
┌──────────────────────────────────────────────────────────────┐
│                      Docker Container                         │
│                                                              │
│  ┌────────────────────────────────────────────────────────┐  │
│  │                  stenosaur (Go process)                 │  │
│  │                                                        │  │
│  │  ┌──────────────────┐       ┌──────────────────────┐  │  │
│  │  │   pkg/renderer   │       │      pkg/bot         │  │  │
│  │  │                  │       │                      │  │  │
│  │  │  WebRenderer ────┼──────▶│  ZoomClient          │  │  │
│  │  │  FileDecoder ────┼──chan─▶│  (Playwright/PWA)   │  │  │
│  │  │  AudioSynth  ────┼──────▶│                      │  │  │
│  │  └──────────────────┘       └──────────────────────┘  │  │
│  │                                                        │  │
│  │  ┌────────────────────────────────────────────────┐   │  │
│  │  │  pkg/controller                                │   │  │
│  │  │  HTTP :8080 · REST API · WebSocket · Web UI    │   │  │
│  │  └────────────────────────────────────────────────┘   │  │
│  │                                                        │  │
│  │  ┌────────────────────────────────────────────────┐   │  │
│  │  │  pkg/media                                     │   │  │
│  │  │  AudioFrame · VideoFrame · AudioSource ·       │   │  │
│  │  │  VideoSource                                   │   │  │
│  │  └────────────────────────────────────────────────┘   │  │
│  └────────────────────────────────────────────────────────┘  │
│                                                              │
│  Chromium (one instance, managed by Playwright)              │
│  PulseAudio null sink (userspace; no host device required)   │
│  Node.js Playwright driver (RPC bridge; ~50 MB)              │
└──────────────────────────────────────────────────────────────┘
                              │
                    Docker bridge network
                              │
                    ┌─────────────────┐
                    │  Admin browser  │
                    │  (LAN machine)  │
                    └─────────────────┘
```

**Dependency direction:** `pkg/bot` and `pkg/controller` depend on `pkg/media`; `pkg/media` depends on nothing else internal.

**Single Chromium instance:** one browser process manages two browser contexts — one driving the Zoom Web App session (`pkg/bot`) and one rendering source webpages (`pkg/renderer.WebRenderer`).

---

## Package Responsibilities

### pkg/bot
Owns the Zoom session lifecycle. Implements the `ZoomClient` interface backed by the Playwright/PWA join flow. Consumes `AudioFrame` and `VideoFrame` channels from `pkg/renderer` and injects them into the Zoom session via Chromium's fake media device layer. Exposes session state to `pkg/controller`.

The `ZoomClient` interface is the swap point for a future SDK implementation (ADR-004). Changing the backing implementation does not affect `pkg/renderer` or `pkg/controller`.

### pkg/renderer
Produces media frames from three source types, normalizing all output to the internal format contracts (ADR-002):

- **WebRenderer** — renders a configured URL in a Chromium browser context; captures frames at 30 FPS as YUV420p at 1280 × 720
- **FileDecoder** — decodes audio/video files via FFmpeg; resamples/scales to contract values
- **AudioSynth** — synthesizes audio programmatically; includes a 440 Hz sine wave test source

Frames are produced on buffered Go channels. `pkg/renderer` has no knowledge of the Zoom session.

### pkg/controller
Embeds an HTTP server on the configured port (default 8080). Serves the static web admin UI, REST API endpoints, and a WebSocket log stream. Holds references to `pkg/bot` and `pkg/renderer` interfaces to read state and dispatch commands. Contains no business logic.

### pkg/media
Defines the shared types and interfaces used across all packages: `AudioFrame`, `VideoFrame`, `AudioSource`, `VideoSource`. Has no imports from other internal packages. Changes to this package affect the entire pipeline.

---

## Media Pipeline

### Audio

```
pkg/renderer                    pkg/media              pkg/bot
─────────────                   ─────────              ───────
WebRenderer  ─┐                 Ring buffer            Consume frames
FileDecoder  ─┼─ chan AudioFrame ──────────────────▶   Inject via
AudioSynth   ─┘  (200 frames)                         PulseAudio sink
                                                       → Chromium mic
```

| Property | Value |
|---|---|
| Encoding | PCM linear, 16-bit signed little-endian |
| Sample rate | 48,000 Hz (Chromium WebRTC native rate) |
| Channels | Mono (1) |
| Frame size | 480 samples (10 ms) |
| Buffer type | Ring buffer (circular) |
| Buffer capacity | 200 frames (2 seconds) |
| Low-water mark | 50 frames (500 ms) — `warn` log |
| Critical mark | 10 frames (100 ms) — `error` log |
| Underrun policy | Emit silence frame |
| Overrun policy | Discard oldest frame |

**48 kHz is non-negotiable.** Chromium's WebRTC stack operates natively at 48 kHz. Any other sample rate requires resampling at the injection boundary with undefined quality characteristics. All sources must normalize to 48 kHz before producing frames.

### Video

```
pkg/renderer                    pkg/media              pkg/bot
─────────────                   ─────────              ───────
WebRenderer  ─┐                 FIFO queue             Consume frames
FileDecoder  ─┼─ chan VideoFrame ──────────────────▶   Write to .y4m pipe
AudioSynth   ─┘  (10 frames)                          → Chromium camera
  (color bars)
```

| Property | Value |
|---|---|
| Pixel format | YUV 4:2:0 planar (`yuv420p`), `.y4m` format, tag `C420` |
| Resolution | 1280 × 720 (720p) |
| Frame rate | 30 FPS |
| Color range | Limited (BT.601) |
| Buffer type | FIFO queue |
| Buffer capacity | 10 frames (~333 ms; ~27 MB resident) |
| Low-water mark | 3 frames — `warn` log |
| Underrun policy | Repeat last delivered frame |
| Overrun policy | Discard oldest frame |

**YUV420p / `.y4m` is the internal pipeline format.** H.264 encoding happens inside Chromium's WebRTC stack for transmission to Zoom; Stenosaur does not encode video. The `.y4m` pixel format tag must be `C420` — not `C420mpeg2` or `C420jpeg` — for Chromium compatibility.

If a source cannot sustain 30 FPS, the last available frame is repeated. A reduced frame rate signals to Zoom's WebRTC layer that the camera is degraded; a repeated frame does not.

### Timing and Drift

Each frame carries a PTS (presentation timestamp) set at production time. The `pkg/bot` consumer goroutine compares PTS against wall clock:

| Threshold | Action |
|---|---|
| Audio drift > 40 ms | `warn` log; drop or duplicate one frame to resync |
| Audio drift > 200 ms | `error` log |
| Video delivery delta > 33.3 ms | `warn` log |

On source switch, `pkg/renderer` resets the PTS origin and flushes both buffers before resuming to prevent cross-source sync discontinuity.

---

## Zoom Integration

Stenosaur joins Zoom meetings by navigating the **Zoom Web App** (`app.zoom.us/wc/join`) in a headless Chromium browser controlled by Playwright. The bot appears as a named guest participant — no Zoom App Marketplace registration is required.

**Media injection** uses Chromium's fake media device layer:
- Audio: `--use-fake-device-for-media-stream` + PulseAudio null sink inside the container
- Video: `--use-file-for-fake-video-capture` with a `.y4m` named pipe

No host audio devices, no `/dev/video0`, no v4l2loopback kernel module, no Xvfb display server. The container is self-contained and runs identically on Linux servers, macOS Docker Desktop, Windows Docker Desktop, and CI/CD pipelines.

**Known risks and mitigation:**
- The Zoom Web App UI is a React SPA; Playwright selectors will break when Zoom releases updates. Every selector must be commented with the Zoom Web App version it was verified against. Three breakages within a 6-month window triggers migration to ADR-004b (Meeting SDK).
- Headless Chromium has edge-case behavioral differences from headed mode; both must be tested.
- Zoom's Terms of Service do not explicitly sanction automated browser participation.

**Future path:** The Zoom Meeting SDK for Linux has first-party headless Docker support and direct programmatic audio/video injection. It is technically superior in every dimension except registration. The `ZoomClient` interface in `pkg/bot` is designed as the swap point for an SDK implementation when the registration constraint is relaxed.

---

## Admin API

All endpoints are served by `pkg/controller` on port 8080.

### Health Endpoints (no auth)

| Endpoint | Method | Response |
|---|---|---|
| `/healthz` | GET | `{"status": "ok"}` — 200 if the process is alive |
| `/readyz` | GET | 200 if Zoom session, audio pipeline, video pipeline, and Chromium are all running; 503 otherwise |

`/healthz` and `/readyz` answer different questions. `/healthz` is for Docker's health check mechanism — it returns 200 whenever the HTTP server is responding. `/readyz` reflects functional bot state; a meeting that ends normally will return 503.

`/readyz` response:
```json
{
  "status": "ready",
  "checks": {
    "zoom_session": "connected",
    "audio_pipeline": "running",
    "video_pipeline": "running",
    "chromium": "running"
  }
}
```

### Authenticated Endpoints (Bearer token required)

All endpoints require `Authorization: Bearer <ADMIN_TOKEN>`. Missing or invalid token returns 401. All bodies are JSON. Tokens are compared using constant-time comparison.

| Endpoint | Method | Purpose |
|---|---|---|
| `/status` | GET | Session state, source info, and pipeline buffer metrics |
| `/start` | POST | Join the configured meeting and begin media injection |
| `/stop` | POST | Leave the meeting and stop the pipeline |
| `/config` | PATCH | Update meeting URL, display name, or media source |
| `/media` | POST | Upload a media file for use as audio or video source |
| `/logs` | WebSocket | Live structured log stream (auth on HTTP upgrade) |

`/status` response includes all seven pipeline metrics (see Observability section). `meeting_url` in the response must never include a password query parameter.

---

## Configuration

All configuration is supplied via environment variables loaded from `.env`. `.env` is excluded from version control; `.env.example` is committed with all variables documented.

### Required

| Variable | Description |
|---|---|
| `MEETING_URL` | Full Zoom meeting URL to join |
| `BOT_DISPLAY_NAME` | Name shown in the Zoom participant list |
| `ADMIN_TOKEN` | Bearer token for the admin API; minimum 16 characters |

### Optional (with defaults)

| Variable | Default | Description |
|---|---|---|
| `ADMIN_PORT` | `8080` | Host port mapped to the admin HTTP server |
| `LOG_LEVEL` | `info` | Log verbosity: `debug`, `info`, `warn`, `error` |
| `MEDIA_DIR` | `/app/media` | Container path for media source files |
| `AUDIO_BUFFER_FRAMES` | `200` | Audio ring buffer depth |
| `VIDEO_BUFFER_FRAMES` | `10` | Video FIFO queue depth |

### Secret Handling

`ADMIN_TOKEN` and `MEETING_PASSWORD` must never appear in any log line. Both are redacted at the logging layer. A CI step scans integration test log output for these values and fails the build if either is found.

---

## Deployment

### What the Operator Must Provide

| Requirement | Notes |
|---|---|
| Docker and Docker Compose | Docker Desktop on macOS/Windows; Docker Engine on Linux |
| `.env` populated from `.env.example` | Required before first run |
| Network access to `app.zoom.us` | Must be reachable from inside the container |
| Host port available (default 8080) | For LAN access to the admin UI |

No host audio devices. No kernel modules. No virtual cameras. No display server.

### Running

```bash
# Copy and populate config
cp .env.example .env
# edit .env with your MEETING_URL, BOT_DISPLAY_NAME, ADMIN_TOKEN

# Build and start
docker compose up

# Check liveness
curl http://localhost:8080/healthz

# Check bot readiness
curl http://localhost:8080/readyz

# Start streaming
curl -X POST -H "Authorization: Bearer <ADMIN_TOKEN>" http://localhost:8080/start

# Check status and pipeline metrics
curl -H "Authorization: Bearer <ADMIN_TOKEN>" http://localhost:8080/status

# Stop streaming
curl -X POST -H "Authorization: Bearer <ADMIN_TOKEN>" http://localhost:8080/stop
```

### Docker Compose

```yaml
services:
  stenosaur:
    build: .
    restart: unless-stopped
    ports:
      - "${ADMIN_PORT:-8080}:8080"
    volumes:
      - ./media:/app/media
    env_file:
      - .env
```

No `devices:` block. No `--privileged`. The container handles its own audio (PulseAudio null sink in userspace) and video (Chromium fake device flags) internally.

The `./media` directory is committed to the repository with a `.gitkeep`. It must not be deleted — if Docker Compose creates it, the directory will be root-owned and the container process cannot write to it.

### Container Internals

On startup, the container entrypoint:
1. Starts PulseAudio with a null sink as the default output device
2. Waits for PulseAudio readiness before proceeding
3. Execs the `stenosaur` Go binary

The Go process then validates configuration and internal dependencies before any Zoom connection is attempted (see Startup Validation below). A failing health check causes Docker to restart the container under the `unless-stopped` restart policy.

### Startup Validation

The process validates the following before joining any meeting:
- Required env vars present and non-empty
- `MEETING_URL` is a valid Zoom URL (format check; no network call)
- `ADMIN_TOKEN` is at least 16 characters
- `MEDIA_DIR` is readable
- Chromium binary is present at the expected path
- PulseAudio virtual sink has initialized
- HTTP server binds to the configured port

On any failure, the process exits with a non-zero code and a specific, actionable error message. No partial startup.

---

## Observability

### Log Format

All packages emit structured JSON to stdout. One entry per line.

```json
{
  "ts": "2026-03-10T14:32:01.412Z",
  "component": "bot",
  "level": "info",
  "msg": "joined Zoom meeting",
  "ctx": {
    "meeting_id": "123456789",
    "duration_ms": 2314
  }
}
```

| Field | Values |
|---|---|
| `ts` | RFC3339Nano, UTC |
| `component` | `bot`, `renderer`, or `controller` |
| `level` | `debug`, `info`, `warn`, `error` |
| `msg` | Human-readable; present tense; no trailing period |
| `ctx` | Optional key-value pairs; omitted if empty |

`ctx` conventions: durations use `_ms` suffix; counts use `_total` suffix; errors use key `err`; URLs use key `url` and must have passwords stripped before logging.

Implemented via `log/slog` (Go standard library, 1.21+). Log lines are streamed to connected admin UI clients via the `/logs` WebSocket endpoint.

### Pipeline Metrics

Exposed via `/status` and the `/logs` WebSocket stream:

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

## Testing Strategy

### Unit Tests
- Audio ring buffer: enqueue, dequeue, underrun silence, overrun discard, watermark thresholds
- Video FIFO queue: enqueue, dequeue, underrun repeat, overrun discard
- PTS drift detection: 40 ms and 200 ms thresholds; frame drop/duplicate logic
- Config validation and env var parsing; secret redaction in logger

### Integration Tests
- Start all packages in-process; produce frames from `AudioSynth` (440 Hz) and SMPTE color bar source; verify frames flow through buffers to the bot goroutine without underruns
- All API endpoints respond correctly with and without auth token
- `/readyz` returns 503 when bot is not in a session; 200 when connected

### End-to-End Tests
- Playwright join flow: run in headed mode first, then headless; both must complete against a test Zoom meeting
- Verify `.y4m` pixel format tag is `C420`; confirm Chromium renders at 30 FPS
- Verify container starts without `--privileged` and without `devices:` on Linux, macOS Docker Desktop, and Windows Docker Desktop

### CI
- Secret scan: capture stdout from integration tests; fail if `ADMIN_TOKEN` or `MEETING_PASSWORD` values appear in any log line
- Track Playwright selector breakage incidents as GitHub issues tagged `adr-004-trigger`; three incidents within 6 months triggers ADR-004b review

---

## Security

- **Secrets:** `ADMIN_TOKEN` and `MEETING_PASSWORD` are stored in `.env` (excluded from version control); never logged or reflected in API responses
- **Auth:** all mutating endpoints (`/start`, `/stop`, `/config`, `/media`) require Bearer token; tokens compared with constant-time comparison
- **No HTTPS in Phase 1:** LAN-only exposure is accepted; add a reverse proxy for TLS if the admin UI needs to be accessible beyond the LAN
- **No privileged mode:** the container runs without `--privileged` and without host device access

---

## Architecture Decision Records

The following ADRs govern this architecture. Consult them for full rationale and revision triggers.

| ADR | Decision |
|---|---|
| ADR-001 | Single container; single process; in-process package communication |
| ADR-002 | Audio: 48 kHz mono PCM; Video: YUV420p `.y4m`; buffer contracts and drift correction |
| ADR-003 | Docker Compose deployment; env var configuration; no host device dependencies |
| ADR-004 | Zoom PWA/Playwright integration; no Marketplace registration; `ZoomClient` interface for future SDK swap |
| ADR-005 | HTTP endpoint surface; log format schema; WebSocket log stream |
| ADR-006 | Go 1.21+; `playwright-community/playwright-go`; `log/slog` for structured logging |

---

## Appendix: Future Extensions

- **Playlist management** — queue multiple audio files; cycle source webpages
- **Transcription** — capture meeting audio; apply speech-to-text
- **Audio ducking** — detect speaker activity; lower playback volume during speech
- **Adapter pattern** — support Discord, file output, or custom WebRTC sinks via `ZoomClient`-style interfaces
- **Hot-reload** — update source URLs without restarting the Zoom session
- **Prometheus metrics** — export `/metrics` endpoint emitting pipeline metrics in exposition format
- **SDK migration (ADR-004b)** — replace PWA automation with Zoom Meeting SDK when registration constraint is relaxed

---

## Reference

- [Docker Compose](https://docs.docker.com/compose)
- [Playwright](https://playwright.dev)
- [playwright-go](https://github.com/playwright-community/playwright-go)
- [Zoom Web App](https://app.zoom.us)
- [PulseAudio null sink](https://www.freedesktop.org/wiki/Software/PulseAudio/Documentation/User/Modules/#module-null-sink)
