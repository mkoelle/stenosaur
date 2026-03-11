# pkg/controller — Agent Instructions

@../../AGENTS.md

## Package Role

`pkg/controller` is the HTTP layer only. It owns no business logic. It reads state from `pkg/bot` and `pkg/media` and dispatches commands to them. It does not make decisions about how media is produced or how sessions are managed.

Allowed imports: standard library, `pkg/media`, `pkg/bot`, third-party HTTP/WebSocket libs.
Must not import: `pkg/renderer`.

## Endpoint Surface (ADR-005)

Do not add, remove, or rename endpoints without updating ADR-005.

| Endpoint | Auth | Notes |
|---|---|---|
| `GET /healthz` | None | Returns 200 if HTTP server is alive |
| `GET /readyz` | None | Returns 200 if all subsystems are ready |
| `GET /status` | Bearer | Session state + 7 pipeline metrics |
| `POST /start` | Bearer | Join meeting and begin media injection |
| `POST /stop` | Bearer | Leave meeting |
| `PATCH /config` | Bearer | Update meeting URL, display name, or source |
| `POST /media` | Bearer | Upload a media file |
| `WS /logs` | Bearer on upgrade | Live JSON log stream |

## Auth Rules (ADR-005)

- All authenticated endpoints use `crypto/subtle.ConstantTimeCompare` for token comparison — never `==`.
- Missing or invalid token: return HTTP 401 with body `{"error":"unauthorized","message":"valid Bearer token required"}`.
- This exact response body must be consistent across all endpoints.

## Secret Rules (ADR-003)

- Never log or reflect `ADMIN_TOKEN` in any response body.
- Strip password query parameters from `meeting_url` before serializing in `/status`.

## Log Component

All log entries from this package must use `slog.String("component", "controller")`.

## Do Not

- Add business logic (session management, media decisions) — that belongs in `pkg/bot` or `pkg/renderer`
- Use `html/template` — all HTML is in `pkg/controller/ui/` via Templ (ADR-007)
- Import `pkg/renderer`
- Compare tokens with `==`
- Add endpoints without updating ADR-005
