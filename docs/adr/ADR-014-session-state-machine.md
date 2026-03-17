# ADR-014: Bot Session State Machine

## Status
🟡 **Proposed**

---

## Context

The `ZoomClient` interface (ADR-001, ADR-004) exposes `Join`, `StartMediaStream`, `StopMediaStream`, `IsConnected`, `Leave`, and `Close`. Without a formal state model, the implementation of these methods and their callers must independently invent transition rules. This leads to:

- `pkg/controller` calling `StartMediaStream` when the bot has not yet joined, or calling `Join` when already connected
- `pkg/bot` returning ambiguous errors on invalid transitions with no shared vocabulary for what went wrong
- `/readyz` and `/status` (ADR-005) reporting states that don't map onto a consistent model visible to operators

A formal state machine defines the valid states, which transitions are permitted, what triggers each transition, and what happens on external disruption (meeting ended by host, network loss, Chromium crash).

---

## States

```
IDLE ──join──▶ JOINING ──admitted──▶ CONNECTED ──start──▶ STREAMING
                  │                      │                     │
               timeout/             host ends /           host ends /
               error                network drop /         network drop /
                  │                 Leave()                Chromium crash /
                  ▼                     │                  StopMediaStream()
               ERROR ◀──────────────────┴──────────────────────┤
                  │                                             │
               reset()                                       Leave()
                  │                                             ▼
                  ▼                                          CONNECTED
                IDLE                                            │
                                                             Leave()
                                                                ▼
                                                             IDLE
```

| State | Meaning |
|---|---|
| `IDLE` | No Zoom session active; Chromium A not launched |
| `JOINING` | Chromium A launched; Playwright navigating join flow; waiting room possible |
| `CONNECTED` | Bot is admitted to the meeting; media injection not yet started |
| `STREAMING` | Media injection active; `pacat` and video FIFO goroutine running |
| `ERROR` | Unrecoverable condition; session ended abnormally; awaiting operator reset |

---

## Transitions

| From | Event | To | Action |
|---|---|---|---|
| `IDLE` | `Join()` called | `JOINING` | Launch Chromium A; begin Playwright join flow |
| `JOINING` | Bot admitted to meeting | `CONNECTED` | Log `info`; signal readiness |
| `JOINING` | Timeout / selector failure / join error | `ERROR` | Log `error`; close Chromium A |
| `CONNECTED` | `StartMediaStream()` called | `STREAMING` | Launch `pacat`; open video FIFO; begin frame injection |
| `CONNECTED` | `Leave()` called | `IDLE` | Navigate away; close Chromium A |
| `CONNECTED` | External disconnect (host ends meeting, network drop) | `ERROR` | Log `error`; close Chromium A |
| `STREAMING` | `StopMediaStream()` called | `CONNECTED` | Stop `pacat`; close video FIFO goroutine; leave media running |
| `STREAMING` | `Leave()` called | `IDLE` | Stop media; navigate away; close Chromium A |
| `STREAMING` | External disconnect | `ERROR` | Log `error`; stop media goroutines; close Chromium A |
| `STREAMING` | Chromium A crash | `ERROR` | Log `error`; attempt recovery (see below) |
| `ERROR` | `reset()` called (via `POST /stop`) | `IDLE` | Ensure Chromium A is closed; clean up FIFO and `pacat` |

Transitions not listed above are invalid. Calling `StartMediaStream` from `IDLE` or `Join` from `STREAMING` must return an error immediately without side effects.

---

## External Disconnect Detection

Zoom session termination is detected via Playwright page event monitoring. `pkg/bot` registers a handler on the Chromium A page for:

1. **Page navigation away from `app.zoom.us`** — Zoom's Web App navigates to a "meeting ended" or "you have been removed" page when the session terminates externally. The URL change is observable via Playwright's `page.on("framenavigated", ...)` event.
2. **Page crash or `page.on("crash", ...)`** — Playwright fires this event when the renderer process for that page crashes.
3. **Playwright context close** — if the browser context closes unexpectedly, all pages in it are invalidated.

When any of these events fires while in `CONNECTED` or `STREAMING`, the state transitions to `ERROR`. The event handler logs `error` with `component: bot` and the event details.

---

## Chromium Crash Recovery

If Chromium A crashes while in `STREAMING` or `CONNECTED`, the state moves to `ERROR` and a recovery sequence is attempted automatically, up to `CHROMIUM_MAX_RESTARTS` times (default: 3) within a `CHROMIUM_RESTART_WINDOW_SECONDS` window (default: 300 seconds).

**Recovery sequence:**

1. Stop media injection goroutines (`pacat`, video FIFO writer) if running.
2. Close the crashed Chromium A context and browser (Playwright cleanup).
3. Wait `CHROMIUM_RESTART_DELAY_SECONDS` (default: 5 seconds).
4. Re-launch Chromium A with the same launch arguments.
5. Re-execute the join flow (`Join`) with the original meeting URL and display name.
6. On successful re-admission, transition back to `CONNECTED`.
7. If `StartMediaStream` was active at crash time, automatically call `StartMediaStream` again with the same sources.

If recovery fails or the restart count is exceeded, remain in `ERROR` and log `error`. The operator must call `POST /stop` (which triggers `reset()`) and then `POST /start` to begin a fresh session.

**New environment variables (ADR-003):**

| Variable | Default | Description |
|---|---|---|
| `CHROMIUM_MAX_RESTARTS` | `3` | Maximum automatic Chromium restart attempts within the window |
| `CHROMIUM_RESTART_WINDOW_SECONDS` | `300` | Time window for restart counting (5 minutes) |
| `CHROMIUM_RESTART_DELAY_SECONDS` | `5` | Seconds to wait before re-launching Chromium after a crash |

---

## State Exposure

### `/readyz`

`/readyz` maps states to HTTP status as follows:

| State | HTTP Status | `status` field |
|---|---|---|
| `IDLE` | 503 | `"not_started"` |
| `JOINING` | 503 | `"joining"` |
| `CONNECTED` | 200 | `"ready"` |
| `STREAMING` | 200 | `"ready"` |
| `ERROR` | 503 | `"error"` |

`CONNECTED` and `STREAMING` both return 200 because the bot is functional in both states. `/readyz` returning 503 after a normal `Leave()` (→ `IDLE`) is expected and not a container restart trigger — the Docker HEALTHCHECK intentionally targets `/healthz`, not `/readyz` (ADR-005).

### `/status`

The `session.state` field in `/status` exposes the state name directly: `"idle"`, `"joining"`, `"connected"`, `"streaming"`, `"error"`. A `recovery` sub-object is included when in `ERROR` with automatic restart in progress:

```json
"session": {
  "state": "error",
  "recovery": {
    "attempt": 2,
    "max_attempts": 3,
    "last_error": "Chromium A exited with signal 11"
  }
}
```

---

## Invalid Transition Errors

All `ZoomClient` methods return a typed error on invalid transitions:

```go
type StateError struct {
    Current  SessionState
    Attempted string  // method name
    Message  string
}
```

`pkg/controller` maps `StateError` to HTTP 409 Conflict with a body of:
```json
{ "error": "invalid_state", "current_state": "idle", "message": "cannot call StartMediaStream from IDLE; call /start first" }
```

---

## Consequences

**Positive:**
- All three packages share a single vocabulary for session state; `/status` and `/readyz` are unambiguous
- Invalid transitions fail immediately with a specific error; no partial execution
- Automatic Chromium crash recovery reduces operator intervention for transient failures
- The state machine is the authoritative source for what `pkg/controller` can and cannot call

**Negative:**
- Recovery sequence adds complexity to `pkg/bot`; the re-join flow must be idempotent (safe to call from `IDLE` again after crash cleanup)
- `CHROMIUM_MAX_RESTARTS` and restart window add three new env vars to document and validate
- `ERROR` state requires explicit operator reset via `POST /stop`; the bot will not self-recover beyond the configured restart limit

**Accepted trade-offs:**
- Automatic restart limited to Chromium crashes, not join flow failures; join failures (selector timeout, wrong password) require operator intervention since the cause is likely configuration, not transience

---

## Revision Triggers

1. **Multi-session support:** the state machine becomes per-session rather than singleton; each `ZoomClient` instance carries its own state; the `/status` schema becomes a list
2. **SDK migration (ADR-004b):** external disconnect detection changes from Playwright page events to SDK callbacks; the state transition triggers are updated but the state names and transition rules remain the same
3. **Restart strategy insufficient:** if `CHROMIUM_MAX_RESTARTS` and window tuning are not sufficient for a deployment's reliability requirements, evaluate exponential backoff or a supervisor process

---

## Related ADRs

- **ADR-001 (Service Decomposition):** `pkg/bot` owns the state machine; `pkg/controller` reads state and calls transitions via `ZoomClient`
- **ADR-003 (Docker Deployment):** three new env vars (`CHROMIUM_MAX_RESTARTS`, `CHROMIUM_RESTART_WINDOW_SECONDS`, `CHROMIUM_RESTART_DELAY_SECONDS`) must be added to `.env.example` and startup validation
- **ADR-004 (Zoom Integration):** external disconnect detection relies on Playwright page events; this is PWA-path-specific
- **ADR-005 (Observability):** `/readyz` state mapping and `/status` `session.state` field are specified here; ADR-005 exposes them
