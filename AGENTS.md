# Stenosaur — Agent Instructions

Stenosaur is a containerized Zoom meeting bot written in Go. It joins meetings via the Zoom Web App (Playwright/PWA automation), injects audio and video from configurable sources, and exposes a web admin UI for monitoring and control.

Read `docs/ARCHITECTURE.md` for the full system design. All architectural decisions are recorded in `docs/adr/` (ADR-001 through ADR-014). Do not make structural changes that contradict a Proposed or Accepted ADR without flagging the conflict.

---

## Repository Layout

```
cmd/stenosaur/        # binary entrypoint; startup validation; wires packages
pkg/media/            # shared frame types and source interfaces (no internal imports)
pkg/renderer/         # media frame production: WebRenderer, FileDecoder, AudioSynth
pkg/bot/              # Zoom session lifecycle; ZoomClient interface + PWA implementation
pkg/controller/       # HTTP server, REST API, WebSocket log stream, web admin UI
pkg/controller/ui/    # Templ templates, HTMX + Alpine.js + Tailwind CSS
docs/adr/             # Architecture Decision Records ADR-001 through ADR-007
docs/                 # ARCHITECTURE.md, USER-STORIES.md
media/                # bind-mounted media source files (committed as .gitkeep only)
```

**Dependency direction (strict):** `pkg/bot` and `pkg/controller` may import `pkg/media`. `pkg/media` must never import any other internal package. `pkg/renderer` may import `pkg/media` only. No package may create a circular import.

---

## Build and Development Commands

```bash
# Install code generation tools (one-time, not in container)
go install github.com/a-h/templ/cmd/templ@latest
# install tailwindcss CLI from https://tailwindcss.com/docs/installation

# Generate code (run after editing .templ files or when tw.css is stale)
task generate

# Build binary
task build

# Run all tests
task test

# Run tests with race detector
task test-race

# Start with Docker Compose (production-like)
task up

# Stop
task down

# Build container image only
task image
```

---

## Go Conventions

- **Go 1.26 minimum.** Use `log/slog` for all structured logging — never `fmt.Println`, `log.Printf`, or third-party loggers.
- **Error handling:** always return errors; never swallow them silently. Use `fmt.Errorf("context: %w", err)` for wrapping.
- **Goroutine discipline:** every goroutine launched must have a `defer recover()` that logs the panic with `level: error` before exiting. Never launch a goroutine without panic recovery.
- **Interfaces over concrete types** at package boundaries. Accept interfaces, return concrete types.
- **No global state** outside of `cmd/stenosaur/main.go` initialization.
- **Table-driven tests** preferred. Test files live alongside source files (`foo_test.go`).
- Run `go vet ./...` and `go test ./...` before considering any change complete.

---

## Log Format (ADR-005)

All log output must conform to this schema. Use `log/slog` with a JSON handler.

```json
{
  "ts": "2026-03-10T14:32:01.412Z",
  "component": "bot",
  "level": "info",
  "msg": "joined Zoom meeting",
  "ctx": { "meeting_id": "123456789", "duration_ms": 2314 }
}
```

- `component` must be one of: `bot`, `renderer`, `controller`
- `msg`: present tense, no trailing period
- `ctx`: optional; omit if empty
- **Never log `ADMIN_TOKEN` or `MEETING_PASSWORD` values under any circumstances.**
- Strip password query parameters from any URL before logging.

---

## Secret Rules (ADR-003)

- `ADMIN_TOKEN` and `MEETING_PASSWORD` must never appear in log output, API responses, or error messages.
- These values are compared using `crypto/subtle.ConstantTimeCompare` — never `==`.
- Do not add new secret variables without updating `.env.example` and the startup validation in `cmd/stenosaur/main.go`.

---

## Media Format Contracts (ADR-002)

These values are fixed by Chromium's injection boundary. Do not change them without updating ADR-002.

| Property | Value |
|---|---|
| Audio sample rate | 48,000 Hz |
| Audio bit depth | 16-bit signed little-endian PCM |
| Audio channels | Mono (1) |
| Audio frame size | 480 samples (10 ms) |
| Video pixel format | YUV 4:2:0 planar (`yuv420p`), `.y4m`, tag `C420` |
| Video resolution | 1280 × 720 |
| Video frame rate | 30 FPS |

---

## Zoom Integration Rules (ADR-004)

- The Zoom session is always accessed through the `bot.ZoomClient` interface. Never call Playwright directly from outside `pkg/bot`.
- Every Playwright UI selector must have a comment noting the Zoom Web App version it was verified against.
- Log a `warn` when any selector times out — this is the signal for an ADR-004 breakage incident.
- Three selector breakage incidents within 6 months trigger an ADR-004b review. Track incidents as GitHub issues tagged `adr-004-trigger`.

---

## Web UI Rules (ADR-007)

- All HTML is rendered via **Templ** (`.templ` files in `pkg/controller/ui/`). Never use `html/template` directly.
- Dynamic behavior uses **HTMX** attributes and **Alpine.js** for local component state. Do not add jQuery or other JS libraries.
- Styling uses **Tailwind CSS** utility classes only. Do not write custom CSS outside of `tw.css` generation.
- HTMX and Alpine.js are vendored in `pkg/controller/ui/static/`. Do not fetch from CDN at runtime.
- After editing `.templ` files, run `task generate` to regenerate `*_templ.go` files before building.

---

## What Requires an ADR

Do not implement the following without first flagging it for an ADR update:

- Changing the audio sample rate, bit depth, or frame size (ADR-002)
- Changing the video resolution, frame rate, or pixel format (ADR-002)
- Changing the audio injection mechanism (PulseAudio/pacat — ADR-008)
- Changing the video injection mechanism (FIFO pipe — ADR-009)
- Replacing FFmpeg subprocess with CGo native bindings (ADR-006)
- Adding a second container or process (ADR-001)
- Changing the number of Chromium instances or their flag assignments (ADR-001)
- Adding any Zoom API or SDK integration (ADR-004)
- Changing the HTTP endpoint surface or request/response schemas (ADR-005)
- Changing the log format schema (ADR-005)
- Adding a new audio source type beyond FileDecoder, StreamDecoder, AudioSynth (ADR-010/011)
- Adding or removing session states or transitions (ADR-014)
- Changing the Chromium crash recovery model (ADR-014)
- Adding new PulseAudio modules to the entrypoint (ADR-008/012)
- Adding a JS framework (Vue, React, Svelte) to the UI (ADR-007)
- Adding CGo dependencies of any kind (ADR-006)
- Changing the gain/mixing model for DuckingSource or Mixer (ADR-013)

---

## ADR Index

| ADR | Decision |
|---|---|
| ADR-001 | Single container; single process; **two Chromium instances** (Chromium A: Zoom session with fake device flags; Chromium B: WebRenderer without flags) |
| ADR-002 | Audio 48 kHz PCM; Video YUV420p `.y4m`; buffer contracts; drift correction |
| ADR-003 | Docker Compose; env var config; no host device dependencies; `MEETING_URL` is a runtime param of `POST /start`, not an env var |
| ADR-004 | Zoom PWA/Playwright; no Marketplace registration; `ZoomClient` interface |
| ADR-005 | HTTP endpoint surface; `POST /start` accepts meeting URL; log format; WebSocket log stream |
| ADR-006 | Go 1.21+; `playwright-community/playwright-go`; `log/slog`; **FFmpeg via subprocess (decided — no CGo)** |
| ADR-007 | Templ + HTMX + Alpine.js + Tailwind CSS for web admin UI |
| ADR-008 | Audio injection via PulseAudio null sink + `pacat` subprocess; wire format: raw 16-bit LE PCM, no WAV header |
| ADR-009 | Video injection via `.y4m` named pipe (FIFO); `--use-file-for-fake-video-capture` on Chromium A only |
| ADR-010 | External stream sources: `yt-dlp` for URL resolution + FFmpeg subprocess for decoding; `StreamDecoder` type |
| ADR-011 | Audio source sequencing: `Playlist` type; PTS continuity across track boundaries; pre-loading |
| ADR-012 | Meeting audio monitoring: isolated `stenosaur_output_sink`; RMS VAD goroutine; `DuckingController` interface |
| ADR-013 | Audio mixing: `DuckingSource` wrapper; `Mixer` type; `OverlayController` interface; gain ramping |
| ADR-014 | Session state machine: `IDLE → JOINING → CONNECTED → STREAMING → ERROR`; Chromium crash recovery with auto-restart |
