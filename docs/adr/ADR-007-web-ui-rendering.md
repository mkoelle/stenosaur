# ADR-007: Web Admin UI Rendering Approach

## Status
🟡 **Proposed**

---

## Context

ADR-001 establishes that `pkg/controller` serves a web admin UI from an embedded HTTP server. ADR-005 specifies the API endpoint surface and WebSocket log stream the UI consumes. Neither ADR specifies how the UI itself is built, rendered, or delivered to the browser.

This decision must be made before `pkg/controller` is implemented because it determines the build toolchain, the shape of the template and static file layer, and whether a JavaScript build pipeline is part of the project's development and CI workflow.

**The UI's functional scope**, derived from ADR-005:
- Display live bot status and session state
- Display live pipeline buffer metrics (seven values from `/status`)
- Display a live log tail (via `/logs` WebSocket)
- Trigger start, stop, and config update operations
- Upload media files

This is a monitoring and control dashboard. It has no complex client-side routing, no multi-user state, and no real-time collaborative features. The interactivity required is: button clicks that call API endpoints, a form for configuration, a polling or streaming status panel, and a live-updating log view.

---

## Options Evaluated

### Option A: JavaScript SPA (Vue, React, or Svelte) + Vite

A separate frontend application built with a modern JS framework, compiled by Vite into a `dist/` directory, and embedded into the Go binary via `//go:embed`.

This approach is well-understood and appropriate when the UI requires rich client-side state, complex routing, or a large interactive surface. For this UI it introduces costs without corresponding benefit:

- A `package.json`, `node_modules`, and Vite config to maintain alongside the Go project
- A Vite build step in the Dockerfile that requires Node.js at image build time (not runtime, but still a build dependency)
- A separate build artifact (`dist/`) whose embedding into the binary must be coordinated with the Go build
- TypeScript/framework knowledge required for contributors who may primarily work on Go code

The `//go:embed` path is available regardless of what generated the static files. If the UI grows beyond what a server-rendered approach can reasonably handle, migrating to a Vite-built SPA embedded via `//go:embed` is a contained change that does not affect the API or container architecture. This option is not rejected permanently — it is deferred until the UI complexity justifies it.

**Verdict:** Over-engineered for current UI scope; deferred.

---

### Option B: Go `html/template` + vanilla JS

The Go standard library's `html/template` package renders HTML server-side. JavaScript is written by hand for dynamic behavior. Static files are embedded via `//go:embed`.

This avoids all external tooling. However, `html/template` has no type safety — template variables are `interface{}`, errors in template expressions are caught only at render time, and there is no compile-time verification that the data passed to a template matches what the template expects. For a project where the rest of the codebase benefits from Go's static typing, this is an inconsistency that will surface as runtime panics rather than build failures.

Vanilla JavaScript for the WebSocket log tail and live metric updates is manageable but verbose. Without a library like HTMX, each dynamic behavior requires manual `fetch`, DOM manipulation, and event handling.

**Verdict:** Adequate but untyped; rejected in favor of a typed alternative.

---

### Option C: Templ + HTMX + Tailwind CSS ✅

Three tools with distinct responsibilities:

**Templ** is a Go-native HTML templating language. `.templ` files compile to Go functions via `templ generate`. Templates are type-checked at compile time — passing wrong data to a template is a build error, not a runtime panic. No `//go:embed` is needed for templates; they become ordinary Go code in the binary.

**HTMX** is a small JavaScript library (~14 kB) that exposes browser capabilities — HTTP requests, WebSocket connections, partial DOM replacement — via HTML attributes. The Stenosaur UI's dynamic behaviors map directly onto HTMX primitives: polling `/status` for metrics, connecting to `/logs` via WebSocket, and issuing `POST` requests for start/stop. No JavaScript needs to be written for these interactions.

**Alpine.js** (small companion to HTMX, ~15 kB) handles component-level interactivity that does not require a server round-trip: toggling UI state, showing confirmation dialogs before destructive actions (e.g., `/stop`), and managing ephemeral local state. Combined with HTMX it covers the full interactive surface of this UI without a framework.

**Tailwind CSS** provides utility-first styling. The Tailwind CLI (`tailwindcss`) scans `.templ` files for class names and generates a minimal CSS file containing only what is used. This file is embedded via `//go:embed`.

HTMX and Alpine.js are vendored into the repository as minified files under `pkg/controller/ui/static/`. They are not fetched from a CDN at runtime, avoiding a network dependency and ensuring consistent behavior across environments.

**Build flow:**
```
go generate ./...         # templ generate + tailwindcss build
go build ./cmd/stenosaur  # compiles templates (now Go code) + embeds static files
```

`go generate` is the only step that requires external tools (`templ` CLI, `tailwindcss` CLI). These run at development time and in CI; they are not present in the production container image. The production binary is self-contained.

**Verdict: Recommended.**

---

## Comparison Summary

| Factor | SPA (Vue/React) | html/template + JS | Templ + HTMX + Tailwind |
|---|---|---|---|
| Type-safe templates | ✅ (TypeScript) | ❌ (runtime) | ✅ (compile-time) |
| JS build toolchain in project | Yes (Vite + npm) | No | No (Tailwind CLI only) |
| Custom JavaScript required | Yes | Yes | Minimal (Alpine.js for local state) |
| WebSocket log tail | Manual or library | Manual | HTMX WebSocket extension |
| Live metric polling | Manual or library | Manual | HTMX polling extension |
| Embedded in Go binary | `//go:embed dist/` | `//go:embed` | Templ → Go code; CSS via `//go:embed` |
| CDN dependency at runtime | Optional | Optional | None (vendored) |
| Contributor requirements | Go + JS/framework | Go + JS | Go + HTML/CSS |
| Suitable for current UI scope | Over-engineered | Adequate | ✅ Appropriate |
| Upgrade path if UI grows | Already there | Migrate to SPA | Migrate to SPA + `//go:embed` |

---

## Decision

**Templ + HTMX + Alpine.js + Tailwind CSS.**

The admin UI's functional scope is a monitoring and control dashboard. It requires no client-side routing, no complex state management, and no framework-level abstractions. Templ provides type-safe server-side rendering in idiomatic Go. HTMX and Alpine.js cover all required dynamic behaviors without a JS build pipeline. Tailwind CSS provides styling via a single generated file embedded in the binary.

The JS SPA path remains available without architectural change if the UI grows beyond this scope — `//go:embed` accepts any static file directory regardless of how it was generated.

**File layout within `pkg/controller`:**

```
pkg/controller/
  ui/
    layout.templ          # base HTML shell; imports HTMX, Alpine.js, Tailwind CSS
    dashboard.templ       # status panel, start/stop controls, source config form
    logs.templ            # WebSocket log tail
    static/
      htmx.min.js         # vendored
      alpine.min.js       # vendored
      tw.css              # generated by Tailwind CLI; committed to repo
```

**`//go:embed` directive** in `pkg/controller`:
```go
//go:embed ui/static
var staticFiles embed.FS
```

Templ-generated `.go` files are committed to the repository alongside their `.templ` sources. The `tw.css` output is also committed, so the project builds with `go build` alone without requiring the Tailwind CLI to be installed (it is only needed when `.templ` files change and during CI generation checks).

---

## Consequences

**Positive:**
- Templates are type-checked at compile time; wrong data passed to a template is a build error
- No JavaScript build toolchain (`node_modules`, Vite, webpack) in the project
- No CDN dependency at runtime; static assets are vendored and embedded
- The UI compiles into the Go binary with no separate deployment artifact
- `go generate` is the single command for all code generation; consistent with Go tooling conventions
- Contributors need only Go and HTML/CSS knowledge; no framework expertise required

**Negative:**
- `templ` CLI must be installed for development and CI; it is an external tool dependency not managed by `go.mod`
- Tailwind CSS CLI must similarly be installed; generated output must be committed or regenerated in CI
- `.templ` → `.go` generated files must be kept in sync with their sources; stale generated files are a CI failure mode
- HTMX covers most dynamic behaviors but has a learning curve for contributors unfamiliar with hypermedia-driven UI patterns
- If the UI grows to require complex client-side state (e.g., real-time video preview, drag-and-drop playlist), this approach hits its limits and migration to a SPA is required

**Accepted trade-offs:**
- External CLI tools (`templ`, `tailwindcss`) accepted as dev dependencies; they do not appear in the production image
- Committed generated files (`*_templ.go`, `tw.css`) accepted; reduces CI friction at the cost of generated files in version control

---

## Revision Triggers

Create **ADR-007b** if any of the following occur:

1. **UI complexity outgrows server-rendering** — features such as real-time video preview, drag-and-drop media management, or multi-step wizards require rich client-side state that HTMX and Alpine.js cannot cleanly express; migrate to a Vite-built SPA embedded via `//go:embed`
2. **Templ falls behind Go releases** — if `templ` does not support a new Go version in use by the project within a reasonable window, evaluate migration to `html/template` with a typed wrapper or to a SPA approach
3. **Contributor friction is sustained** — if the hypermedia pattern proves consistently difficult for contributors, reassess the JS framework trade-off

---

## Related ADRs

- **ADR-001 (Service Decomposition):** establishes that `pkg/controller` serves the web UI from an embedded HTTP server; this ADR specifies how that UI is built and rendered
- **ADR-003 (Docker Deployment):** the production container image does not require Node.js, `templ` CLI, or Tailwind CLI; all build artifacts are compiled into the Go binary before the image is built
- **ADR-005 (Observability):** the UI consumes the `/status`, `/readyz`, `/logs` (WebSocket), `/start`, `/stop`, `/config`, and `/media` endpoints defined in ADR-005; this ADR does not change that surface
- **ADR-006 (Implementation Language):** Templ compiles to Go and integrates with `go generate` and `go build`; it is a natural extension of the Go toolchain decision
