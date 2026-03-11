# ADR-003: Docker Deployment and Configuration

## Status
🟡 **Proposed**

---

## Context

Stenosaur must run consistently across development, CI, and production environments on Linux servers, macOS Docker Desktop, and Windows Docker Desktop. The deployment model must require no host-level setup beyond installing Docker.

The PWA integration path (ADR-004) uses Playwright/Chromium running inside the container. Audio and video are injected via in-container mechanisms (PulseAudio userspace virtual sink; Chromium fake device flags). No host virtual devices, kernel modules, or display servers are required. The container is fully self-contained.

This ADR decides:
- How the container is composed and run
- How runtime configuration is supplied and validated
- What the operator must provide before first run

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

**Required variables:**

| Variable | Description |
|---|---|
| `MEETING_URL` | Full Zoom meeting URL to join |
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

**Variables that must never appear in logs:**

| Variable | Policy |
|---|---|
| `ADMIN_TOKEN` | Validated at startup; value never logged or reflected in API responses |
| `MEETING_PASSWORD` | Used only in the join flow; never stored in application state or logged |

### 3. Startup Validation

The process validates configuration and internal dependencies before any Zoom connection is attempted. On failure, it exits with a non-zero code and a specific, actionable error message. The bot does not start partially — no attempt is made to join a meeting if validation fails.

Validation covers: required variables present and non-empty; `MEETING_URL` is a valid Zoom URL (format check only); `ADMIN_TOKEN` meets the minimum length; `MEDIA_DIR` is readable; Chromium binary is present; PulseAudio virtual sink initializes; HTTP server binds to the configured port.

### 4. Media File Access

Media source files are made available to the container via a bind mount. The operator places files in `./media` on the host; the container reads from `/app/media`. Files uploaded via the web admin UI are written to this volume and persist across restarts.

The `./media` directory must exist before `docker compose up` is run. If Compose creates it, it will be root-owned and the container process cannot write to it. The directory is committed to the repository with a `.gitkeep` to prevent this.

### 5. Port Exposure

The admin HTTP server (ADR-001 `pkg/controller`) listens on port 8080 inside the container, mapped to the host via Compose. This makes it accessible from other machines on the local network via the host's IP address. No other ports are exposed.

---

## What the Operator Must Provide

| Requirement | Notes |
|---|---|
| Docker and Docker Compose | Docker Desktop on macOS/Windows; Docker Engine on Linux |
| `.env` populated from `.env.example` | Required before first run |
| `./media` directory exists | Committed to repo as `./media/.gitkeep` |
| Network access to `app.zoom.us` from the container | Required for PWA path; must not be firewalled |
| Host port available (default 8080) | For LAN access to the admin UI |

No host audio devices. No kernel modules. No virtual cameras. No display server.

---

## Consequences

**Positive:**
- `docker compose up` is the complete run command after `.env` is configured
- Identical behavior on Linux, macOS Docker Desktop, and Windows Docker Desktop
- No privileged mode or host device access required
- Startup validation fails fast with specific error messages

**Negative:**
- PulseAudio must initialize inside the container before Chromium launches; the container entrypoint must handle this sequencing
- Network access to `app.zoom.us` must be available from inside the container; strict egress environments require explicit allowlisting
- `ADMIN_TOKEN` has no rotation mechanism; a compromised token requires a `.env` edit and container restart
- No HTTPS on the admin UI; LAN-only exposure is accepted for Phase 1

---

## Revision Triggers

1. **SDK migration (ADR-004b):** the Meeting SDK may require specific Linux capabilities or host device access; revisit the `devices:` block and privilege model
2. **Multi-session requirement:** multiple concurrent bots would require Compose profiles or Kubernetes; revisit orchestration
3. **Public-facing admin UI:** TLS termination required; revisit port exposure and auth model (ADR-005)
4. **CI/CD pipeline integration:** document runner-specific requirements (Docker-in-Docker vs. host socket; `app.zoom.us` reachability)

---

## Related ADRs

- **ADR-001 (Service Decomposition):** single container, single service; this ADR's Compose structure reflects that directly
- **ADR-002 (Media Format Contracts):** `AUDIO_BUFFER_FRAMES` and `VIDEO_BUFFER_FRAMES` expose ADR-002 buffer depths as operator-tunable configuration
- **ADR-004 (Zoom Integration):** PWA path is the reason no host virtual devices are required; this ADR's simplicity is a direct consequence
- **ADR-005 (Observability):** the admin HTTP server port exposed here serves the endpoints defined in ADR-005
