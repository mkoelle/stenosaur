# ADR-003: Docker Deployment and Configuration

## Status
🟡 **Proposed**

---

## Context

Stenosaur must run consistently across development, CI, and production environments on Linux servers, macOS Docker Desktop, and Windows Docker Desktop. The deployment model must require no host-level setup beyond installing Docker.

The PWA integration path (ADR-004) uses Playwright/Chromium running inside the container. Two Chromium instances run in-process (ADR-001): one for the Zoom session with fake device flags, one for the webpage renderer without device flags. Audio and video are injected via in-container mechanisms — PulseAudio userspace virtual sinks (ADR-008) and a named pipe (ADR-009). No host virtual devices, kernel modules, or display servers are required.

This ADR decides:
- How the container is composed and run
- How runtime configuration is supplied and validated at startup
- What the operator must provide before first run
- Note: meeting URL and password are runtime parameters of `POST /start` (ADR-005), not environment variables

---

## Decision

### 1. Single Service via Docker Compose

The application runs as one process in one container (ADR-001). Docker Compose manages port mapping, volume mounts, environment variables, and restart policy. No multi-service networking is required.

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

No `devices:` block. No `--privileged` mode. No host kernel module dependencies.

### 2. Configuration via Environment Variables

All runtime configuration is supplied via `.env`, loaded by Compose. `.env` is excluded from version control. `.env.example` is committed with every variable documented and placeholder values provided.

Meeting URL and password are **not** environment variables. They are supplied as parameters to `POST /start` at runtime, allowing the operator to join different meetings without restarting the container. The bot does not store meeting URL or password in application state beyond the duration of a single join attempt.

**Required variables:**

| Variable | Description |
|---|---|
| `BOT_DISPLAY_NAME` | Name shown in the Zoom participant list |
| `ADMIN_TOKEN` | Bearer token for web admin UI; minimum 16 characters |

**Optional variables with defaults:**

| Variable | Default | Description |
|---|---|---|
| `ADMIN_PORT` | `8080` | Host port mapped to the admin HTTP server |
| `LOG_LEVEL` | `info` | Log verbosity: `debug`, `info`, `warn`, `error` |
| `MEDIA_DIR` | `/app/media` | Container path for media source files |
| `AUDIO_BUFFER_FRAMES` | `200` | Audio ring buffer depth (ADR-002) |
| `VIDEO_BUFFER_FRAMES` | `10` | Video FIFO queue depth (ADR-002) |
| `CHROMIUM_MAX_RESTARTS` | `3` | Max automatic Chromium A restart attempts within window (ADR-014) |
| `CHROMIUM_RESTART_WINDOW_SECONDS` | `300` | Restart counting window in seconds (ADR-014) |
| `CHROMIUM_RESTART_DELAY_SECONDS` | `5` | Delay before re-launching Chromium A after crash (ADR-014) |
| `VAD_ENABLED` | `false` | Enable voice activity detection ducking (ADR-012) |
| `VAD_DUCK_THRESHOLD` | `500` | RMS energy threshold for voice detection (ADR-012) |
| `VAD_RELEASE_MS` | `800` | Silence hold time before unduck in ms (ADR-012) |
| `DUCK_GAIN` | `0.15` | Primary source gain while ducked, 0.0–1.0 (ADR-013) |
| `DUCK_RAMP_MS` | `200` | Gain ramp duration in milliseconds (ADR-013) |

**Variables that must never appear in logs:**

| Variable | Policy |
|---|---|
| `ADMIN_TOKEN` | Validated at startup; value never logged or reflected in API responses |
| Meeting password (if present in `POST /start` URL) | Extracted from URL `pwd` param; never stored, never logged |

### 3. Startup Validation

The process validates configuration and internal dependencies before any Zoom connection is attempted. On failure it exits with a non-zero code and a specific actionable error message. The bot does not start partially.

Validation covers: required variables present and non-empty; `ADMIN_TOKEN` minimum length; `MEDIA_DIR` readable; `ffmpeg` binary present and executable; `yt-dlp` binary present and executable; Chromium binary present; PulseAudio virtual sink initialises; HTTP server binds to the configured port.

Meeting URL is validated at `POST /start` time, not at startup.

### 4. Media File Access

Media source files are made available to the container via a bind mount. The operator places files in `./media` on the host; the container reads from `/app/media`. Files uploaded via the web admin UI are written to this volume and persist across restarts.

The `./media` directory must exist before `docker compose up` is run. If Compose creates it, it will be root-owned and the container process cannot write to it. The directory is committed to the repository with a `.gitkeep` to prevent this.

### 5. Port Exposure

The admin HTTP server listens on port 8080 inside the container, mapped to the host via Compose. This makes it accessible from other machines on the local network. No other ports are exposed.

---

## What the Operator Must Provide

| Requirement | Notes |
|---|---|
| Docker and Docker Compose | Docker Desktop on macOS/Windows; Docker Engine on Linux |
| `.env` populated from `.env.example` | Required before first run; meeting URL is a runtime parameter of `POST /start` |
| `./media` directory exists | Committed to repo as `./media/.gitkeep` |
| Network access to `app.zoom.us` from the container | Required for PWA path; must not be firewalled |
| Host port available (default 8080) | For LAN access to the admin UI |
| Zoom meeting URL (and password if required) | Supplied at runtime via `POST /start`; not stored in `.env` |

No host audio devices. No kernel modules. No virtual cameras. No display server.

---

## Consequences

**Positive:**
- `docker compose up` is the complete run command after `.env` is configured
- Identical behaviour on Linux, macOS Docker Desktop, and Windows Docker Desktop
- No privileged mode or host device access required
- Startup validation fails fast with specific error messages
- Meeting URL as a runtime parameter allows joining different meetings without restarting

**Negative:**
- PulseAudio must initialise before Chromium launches; the entrypoint handles this sequencing
- Network access to `app.zoom.us` must be available from inside the container
- `ADMIN_TOKEN` has no rotation mechanism; a compromised token requires a `.env` edit and container restart
- No HTTPS on the admin UI; LAN-only exposure is accepted for Phase 1

---

## Revision Triggers

1. **SDK migration (ADR-004b):** the Meeting SDK may require specific Linux capabilities or host device access; revisit the `devices:` block and privilege model
2. **Multi-session requirement:** multiple concurrent bots require Compose profiles or Kubernetes; revisit orchestration
3. **Public-facing admin UI:** TLS termination required; revisit port exposure and auth model (ADR-005)
4. **CI/CD pipeline integration:** document runner-specific requirements (Docker-in-Docker vs. host socket; `app.zoom.us` reachability)

---

## Related ADRs

- **ADR-001 (Service Decomposition):** single container, single service; this ADR's Compose structure reflects that directly
- **ADR-002 (Media Format Contracts):** `AUDIO_BUFFER_FRAMES` and `VIDEO_BUFFER_FRAMES` expose buffer depths as operator-tunable configuration
- **ADR-004 (Zoom Integration):** PWA path is the reason no host virtual devices are required
- **ADR-005 (Observability):** the admin HTTP server port exposed here serves the endpoints defined in ADR-005; meeting URL and password are runtime parameters of `POST /start`
- **ADR-012 (VAD):** `VAD_ENABLED`, `VAD_DUCK_THRESHOLD`, `VAD_RELEASE_MS` env vars defined here
- **ADR-013 (Mixing):** `DUCK_GAIN`, `DUCK_RAMP_MS` env vars defined here
- **ADR-014 (Session State Machine):** `CHROMIUM_MAX_RESTARTS`, `CHROMIUM_RESTART_WINDOW_SECONDS`, `CHROMIUM_RESTART_DELAY_SECONDS` env vars defined here
