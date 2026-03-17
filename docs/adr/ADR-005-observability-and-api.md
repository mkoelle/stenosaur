# ADR-005: Observability, Health Endpoints, and Log Format

## Status
🟡 **Proposed**

---

## Context

Stenosaur runs unattended in a Docker container and is monitored from another machine on the local network. ADR-001 establishes that `pkg/controller` serves an HTTP API and web admin UI, but does not specify the endpoint surface. This ADR defines that surface and the log format shared across all packages.

Two decisions are made here:

1. **What HTTP endpoints exist, what they return, and which require authentication.** This must be decided before `pkg/controller` is implemented.
2. **What the structured log format looks like across all packages.** Without a shared schema, logs from `pkg/renderer`, `pkg/bot`, and `pkg/controller` will be inconsistent and harder to tail, search, or stream to the admin UI.

Configuration and secret handling are specified in ADR-003. Meeting URL and password are runtime parameters of `POST /start` — not environment variables — and are handled here.

---

## Decision

### Health Endpoints

| Endpoint | Method | Auth | Behaviour |
|---|---|---|---|
| `/healthz` | GET | None | Returns 200 if the HTTP server is responding; used by Docker health checks |
| `/readyz` | GET | None | Returns 200 if the bot is in `CONNECTED` or `STREAMING` state (ADR-014); 503 otherwise |

`/healthz` and `/readyz` are intentionally unauthenticated — they must be accessible to Docker's health check mechanism and basic monitoring tooling without credentials.

`/healthz` answers "is the process alive." `/readyz` answers "is the bot functional and in session." A meeting that ends normally transitions the bot to `IDLE` state, causing `/readyz` to return 503 — this is expected behaviour, not a container failure.

**Important:** the Docker HEALTHCHECK deliberately targets `/healthz`, not `/readyz`. `/readyz` returns 503 whenever no session is active — if the HEALTHCHECK targeted `/readyz`, Docker would restart the container after every normal session end. `/healthz` is the correct liveness signal for Docker.

**`/readyz` response schema:**
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

On failure, `status` reflects the session state name (`"idle"`, `"joining"`, `"error"`) and the failing check value is `"degraded"` or `"stopped"`.

### Status and Control Endpoints

All endpoints below require `Authorization: Bearer <ADMIN_TOKEN>`. Missing or invalid token returns 401. All request and response bodies are JSON.

| Endpoint | Method | Purpose |
|---|---|---|
| `/status` | GET | Current session state, source info, and pipeline buffer metrics |
| `/start` | POST | Join a meeting and begin media injection |
| `/stop` | POST | Leave the meeting, stop the media pipeline, and reset to `IDLE` state |
| `/config` | PATCH | Update media source while a session is active |
| `/media` | POST | Upload a media file, or trigger a sound effect overlay |

**`POST /start` request body:**

```json
{
  "meeting_url": "https://zoom.us/j/123456789?pwd=abc123xyz",
  "display_name": "My Bot"
}
```

`meeting_url` is required. `display_name` is optional and overrides `BOT_DISPLAY_NAME` if provided.

**Meeting password handling:** if the meeting URL contains a `pwd` query parameter (Zoom's standard password encoding), the password is extracted from the URL before the join attempt. No separate password field is accepted or needed. The password is extracted immediately on receipt of `POST /start`, used only for the join attempt, and never stored in application state. The `meeting_url` stored in session state and returned by `/status` always has the `pwd` parameter stripped. The raw URL (containing the password) is never logged.

**`POST /start` returns HTTP 409** if the bot is not in `IDLE` state (ADR-014). Returns HTTP 422 if `meeting_url` is missing or not a valid `zoom.us` HTTPS URL.

**`/status` response schema:**
```json
{
  "session": {
    "state": "streaming",
    "meeting_url": "https://zoom.us/j/123456789",
    "joined_at": "2026-03-10T14:00:00Z",
    "duration_seconds": 3620
  },
  "pipeline": {
    "audio_buffer_depth": 142,
    "audio_underrun_total": 0,
    "audio_overrun_total": 0,
    "audio_drift_ms": 3,
    "video_buffer_depth": 8,
    "video_underrun_total": 0,
    "video_overrun_total": 0
  },
  "source": {
    "type": "file",
    "path": "track1.mp3"
  }
}
```

`meeting_url` must never include a `pwd` parameter; it must be stripped before serialisation.

When a `Playlist` is the active source, a `playlist` object is included:
```json
"playlist": { "track_index": 1, "track_total": 3, "current_track": "track2.mp3", "loop": false }
```

When `VAD_ENABLED=true`, a `vad` object is included:
```json
"vad": { "enabled": true, "active": false, "rms_level": 142, "duck_threshold": 500 }
```

When a `Mixer` is active, a `mixer` object is included:
```json
"mixer": { "overlay_active": false, "primary_gain": 1.0, "overlay_track": null }
```

When Chromium A recovery is in progress, `session` includes a `recovery` sub-object:
```json
"recovery": { "attempt": 2, "max_attempts": 3, "last_error": "Chromium A exited with signal 11" }
```

**Auth error response (consistent across all endpoints):**
```json
{ "error": "unauthorized", "message": "valid Bearer token required" }
```

**State conflict error response (HTTP 409):**
```json
{ "error": "invalid_state", "current_state": "streaming", "message": "cannot call /start from STREAMING; call /stop first" }
```

Incoming tokens are compared against `ADMIN_TOKEN` using constant-time comparison (`crypto/subtle.ConstantTimeCompare`) to prevent timing attacks.

### WebSocket Endpoint

| Endpoint | Auth | Purpose |
|---|---|---|
| `/logs` | Bearer token on HTTP upgrade | Live structured log stream |

Each WebSocket message is one JSON log line. The web admin UI connects here to display a live log tail. Authentication is performed on the initial upgrade request via the `Authorization` header.

### Log Format

All packages emit structured JSON to stdout. One entry per line. No multi-line entries.

**Schema:**
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

| Field | Type | Constraints |
|---|---|---|
| `ts` | RFC3339Nano string | UTC always |
| `component` | string | One of: `bot`, `renderer`, `controller` |
| `level` | string | One of: `debug`, `info`, `warn`, `error` |
| `msg` | string | Human-readable; present tense; no trailing period |
| `ctx` | object | Optional; omitted if empty |

**`ctx` field conventions:**
- Durations: `_ms` suffix (e.g., `duration_ms`)
- Counts: `_total` suffix (e.g., `frames_total`)
- Errors: key `err`, string value
- URLs: key `url`; `pwd` query parameters must be stripped before any URL is logged

**Log level semantics:**
- `debug`: internal state transitions, frame counts, timing — disabled by default
- `info`: significant lifecycle events (session joined, source switched, server started, state transitions)
- `warn`: degraded but recoverable conditions (buffer low-water mark, drift threshold exceeded, Chromium restart attempt)
- `error`: failures requiring operator attention (session dropped, Chromium exited, restart limit reached)

**Secret redaction:** `ADMIN_TOKEN` values and meeting `pwd` query parameter values must never appear in any log line.

### Docker Health Check

The Dockerfile must declare a health check against `/healthz`. The start period must be at least 15 seconds to allow PulseAudio and both Chromium instances to initialise before the first check fires. The HEALTHCHECK targets `/healthz` — not `/readyz` — to avoid spurious container restarts after normal session end.

---

## Consequences

**Positive:**
- `/healthz` and `/readyz` let Docker and monitoring tooling distinguish a live process from a functional bot session
- Structured logs are consistent across packages and streamable to the admin UI via WebSocket
- Constant-time token comparison prevents timing-based enumeration
- Pipeline metrics in `/status` surface buffer health without log parsing
- Meeting URL as a `POST /start` parameter allows different meetings per session without container restart

**Negative:**
- No Prometheus `/metrics` endpoint in Phase 1; time-series metrics require polling `/status`
- WebSocket auth via the HTTP upgrade `Authorization` header is not supported by all client libraries
- `/readyz` returns 503 when no session is active; operators must understand this is expected, not a container failure

**Accepted trade-offs:**
- No HTTPS in Phase 1; Bearer tokens travel in plaintext over LAN; acceptable under LAN-only exposure assumption (ADR-003)
- No token rotation API; token changes require a `.env` edit and container restart

---

## Revision Triggers

1. **Prometheus requirement:** add a `/metrics` endpoint emitting ADR-002 buffer metrics in Prometheus exposition format
2. **HTTPS / public exposure:** TLS becomes required; Bearer token model must be hardened
3. **Multi-session support:** `/status` and `/readyz` schemas become session-scoped rather than singleton
4. **Log aggregation:** if logs are shipped to an external system (Loki, Datadog), review field names for schema compatibility

---

## Related ADRs

- **ADR-001 (Service Decomposition):** `pkg/controller` owns all endpoints defined here
- **ADR-002 (Media Format Contracts):** the seven pipeline metrics in `/status` are defined in ADR-002; this ADR specifies how they are exposed over HTTP
- **ADR-003 (Docker Deployment):** `ADMIN_TOKEN` sourcing, `LOG_LEVEL` env var, and container restart policy; meeting URL is a runtime parameter of `POST /start`
- **ADR-011 (Playlist):** `playlist` field in `/status`; `PATCH /config` playlist schema
- **ADR-012 (VAD):** `vad` field in `/status`
- **ADR-013 (Mixing):** `mixer` field in `/status`; `POST /media` overlay action
- **ADR-014 (Session State Machine):** session state names in `/status`; HTTP 409 on invalid state transitions; `/readyz` state mapping
