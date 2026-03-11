# ADR-005: Observability, Health Endpoints, and Log Format

## Status
🟡 **Proposed**

---

## Context

Stenosaur runs unattended in a Docker container and is monitored from another machine on the local network. ADR-001 establishes that `pkg/controller` serves an HTTP API and web admin UI, but does not specify the endpoint surface. This ADR defines that surface and the log format shared across all packages.

Two decisions are made here:

1. **What HTTP endpoints exist, what they return, and which require authentication.** This must be decided before `pkg/controller` is implemented.
2. **What the structured log format looks like across all packages.** Without a shared schema, logs from `pkg/renderer`, `pkg/bot`, and `pkg/controller` will be inconsistent and harder to tail, search, or stream to the admin UI.

Configuration, secret handling, and the `ADMIN_TOKEN` source are specified in ADR-003 and not repeated here.

---

## Decision

### Health Endpoints

| Endpoint | Method | Auth | Behaviour |
|---|---|---|---|
| `/healthz` | GET | None | Returns 200 if the HTTP server is responding; used by Docker health checks |
| `/readyz` | GET | None | Returns 200 if the Zoom session, audio pipeline, video pipeline, and Chromium are all running; 503 otherwise |

`/healthz` and `/readyz` are intentionally unauthenticated — they must be accessible to Docker's health check mechanism and basic monitoring tooling without credentials.

`/healthz` answers "is the process alive." `/readyz` answers "is the bot functional." These are distinct questions. A meeting that ends normally will cause `/readyz` to return 503 even though the process is healthy; operators must understand this distinction.

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

On failure, `status` is `"degraded"` and the failing check value is `"degraded"` or `"stopped"`.

### Status and Control Endpoints

All endpoints below require `Authorization: Bearer <ADMIN_TOKEN>`. Missing or invalid token returns 401. All request and response bodies are JSON.

| Endpoint | Method | Purpose |
|---|---|---|
| `/status` | GET | Current session state, source info, and pipeline buffer metrics (ADR-002) |
| `/start` | POST | Join the configured meeting and begin media injection |
| `/stop` | POST | Leave the meeting and stop the media pipeline |
| `/config` | PATCH | Update meeting URL, display name, or media source |
| `/media` | POST | Upload a media file for use as audio or video source |

**`/status` response schema:**
```json
{
  "session": {
    "state": "streaming",
    "meeting_url": "https://zoom.us/j/...",
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
    "type": "web",
    "url": "https://example.com/display"
  }
}
```

`meeting_url` must never include a password query parameter; it must be stripped before serialization.

**Auth error response (consistent across all endpoints):**
```json
{
  "error": "unauthorized",
  "message": "valid Bearer token required"
}
```

Incoming tokens are compared against `ADMIN_TOKEN` using constant-time comparison to prevent timing attacks.

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
- URLs: key `url`; meeting passwords must be stripped before any URL is logged

**Log level semantics:**
- `debug`: internal state transitions, frame counts, timing — disabled by default; enabled via `LOG_LEVEL=debug` (ADR-003)
- `info`: significant lifecycle events (session joined, source switched, server started)
- `warn`: degraded but recoverable conditions (buffer low-water mark, drift threshold exceeded)
- `error`: failures requiring operator attention (session dropped, Chromium exited)

**Secret redaction:** `ADMIN_TOKEN` and `MEETING_PASSWORD` values must never appear in any log line. A CI step must verify this by capturing integration test log output and scanning for known secret patterns.

### Docker Health Check

The Dockerfile must declare a health check against `/healthz`. The start period must be long enough for PulseAudio and Chromium to initialize before the first check fires. A failing health check causes Docker to restart the container under the `unless-stopped` policy (ADR-003).

---

## Consequences

**Positive:**
- `/healthz` and `/readyz` let Docker and monitoring tooling distinguish a live process from a functional bot
- Structured logs are consistent across packages and streamable to the admin UI via WebSocket
- Constant-time token comparison prevents timing-based enumeration
- Pipeline metrics in `/status` surface ADR-002 buffer health without log parsing

**Negative:**
- No Prometheus `/metrics` endpoint in Phase 1; time-series metrics require parsing `/status` responses
- WebSocket auth via the HTTP upgrade `Authorization` header is not supported by all client libraries; the admin UI must use one that does
- `/readyz` returns 503 when a meeting ends normally; operators must understand this is expected behavior and not a process failure

**Accepted trade-offs:**
- No HTTPS in Phase 1; Bearer tokens travel in plaintext over LAN; acceptable under LAN-only exposure assumption (ADR-003)
- No token rotation API; token changes require a `.env` edit and container restart

---

## Revision Triggers

1. **Prometheus requirement:** add a `/metrics` endpoint emitting ADR-002 buffer metrics in Prometheus exposition format
2. **HTTPS / public exposure:** TLS becomes required; Bearer token model must be hardened (ADR-003 revision trigger 3)
3. **Multi-session support:** `/status` and `/readyz` schemas become session-scoped rather than singleton
4. **Log aggregation:** if logs are shipped to an external system (Loki, Datadog), review field names for schema compatibility

---

## Related ADRs

- **ADR-001 (Service Decomposition):** `pkg/controller` owns all endpoints defined here
- **ADR-002 (Media Format Contracts):** the seven pipeline metrics in `/status` are the same metrics defined in ADR-002; this ADR specifies how they are exposed over HTTP
- **ADR-003 (Docker Deployment):** `ADMIN_TOKEN` sourcing, `LOG_LEVEL` env var, and the container restart policy referenced by the Docker health check are all specified in ADR-003
