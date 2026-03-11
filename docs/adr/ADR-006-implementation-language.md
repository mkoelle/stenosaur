# ADR-006: Implementation Language

## Status
🟡 **Proposed**

---

## Context

No implementation language has been explicitly decided. Prior ADRs use Go syntax in interface examples and imply Go throughout, but this has never been stated as a decision. It must be, because the language choice directly affects:

- How Playwright is integrated (ADR-004 selected Playwright; bindings quality varies significantly by language)
- How FFmpeg is used for file decoding (user story US-R02)
- Whether the container ships a single self-contained binary or requires a runtime
- How the in-process media pipeline (ADR-001) is expressed — goroutines and channels vs. threads vs. async runtimes

The language must be decided before any code is written.

**Candidate languages** are those with Playwright support and FFmpeg bindings sufficient for production use: Go, TypeScript/Node.js, and Python. These are the only realistic options given the Playwright constraint.

---

## Constraints Inherited from Prior ADRs

| Constraint | Source |
|---|---|
| Playwright is the browser automation library | ADR-004 |
| Single process; in-process channels for media pipeline | ADR-001 |
| Single self-contained container; minimal external dependencies | ADR-003 |
| Real-time audio/video pipeline with PTS-based drift correction | ADR-002 |
| Concurrent goroutine/thread model for renderer and bot | ADR-001, ADR-002 |

---

## Options Evaluated

### Option A: TypeScript (Node.js)

Playwright is a Microsoft project and Node.js/TypeScript is its first-class language. The Playwright API surface is most complete, best documented, and most promptly updated in TypeScript. There is no translation layer — the process calls Playwright directly.

FFmpeg integration is available via `fluent-ffmpeg` or direct `ffmpeg` child process invocation. Adequate for decoding, but less ergonomic than native bindings.

The real-time media pipeline (ADR-002) is the significant weakness. Node.js has a single-threaded event loop. True parallel execution of the renderer and bot goroutines requires Worker Threads, which communicate via `postMessage` and SharedArrayBuffer rather than in-process typed channels. The audio ring buffer and video FIFO queue (ADR-002) are expressible but awkward — shared memory between workers is lower-level and more error-prone than Go channels. PTS-based drift correction in a timing-sensitive pipeline is harder to reason about when the execution model is cooperative rather than preemptive.

Node.js also requires a runtime in the container, which is expected and standard.

**Verdict:** Best Playwright integration; weakest fit for the concurrent real-time media pipeline.

---

### Option B: Python

Playwright has official Python bindings (`playwright-python`), well-maintained by Microsoft. FFmpeg integration is mature via `PyAV` (native libav bindings) or `ffmpeg-python`.

Python's concurrency story is worse than Node.js for this use case. The GIL prevents true parallel execution of CPU-bound threads. The audio synthesis and file decoding workloads in `pkg/renderer` are CPU-bound; running them concurrently with the Playwright browser automation requires either multiprocessing (heavy) or async/await (cooperative, not parallel). Neither maps cleanly to the in-process buffered channel model specified in ADR-001 and ADR-002.

Python is also the heaviest container image of the three options when combined with Playwright's bundled browser binaries and native extension dependencies.

**Verdict:** Adequate Playwright support; poor fit for concurrent CPU-bound media pipeline; eliminated.

---

### Option C: Go

Go's concurrency model — goroutines and buffered channels — maps directly to the architecture specified in ADR-001 and ADR-002. The renderer and bot run as goroutines; the audio ring buffer and video FIFO queue are natural buffered channels or purpose-built ring buffer structs. PTS-based drift correction is straightforward with `time.Duration` arithmetic. Per-goroutine `recover` for panic isolation (ADR-001) is idiomatic Go.

FFmpeg integration is available via `ffmpeg-go` (a fluent wrapper around the FFmpeg binary) or `goav` / `go-libav` (native CGo bindings to libavcodec). For the decoding workloads in US-R02, the binary wrapper approach avoids CGo complexity while remaining adequate.

The single compiled binary simplifies the container image and startup sequence (ADR-003).

**Playwright:** Go is not an officially supported Playwright language. `playwright-community/playwright-go` is a community binding that wraps the upstream Node.js Playwright driver via an RPC bridge over stdio — the Go binary still ships and manages a bundled Node.js runtime. This is the most significant trade-off for Go: the "single binary" advantage is partially undermined by the Node.js driver dependency, and any gap between the Go binding's API surface and the upstream Playwright TypeScript API must be worked around rather than used directly.

In practice, the playwright-go binding covers the full API surface needed for the Zoom Web App join flow (navigation, locators, browser contexts, launch arguments, permissions). The RPC bridge is transparent to application code. The risk is lag between upstream Playwright releases and the community binding's updates.

**Verdict:** Best fit for the concurrent media pipeline; adequate Playwright support with a known trade-off; selected.

---

## Comparison Summary

| Factor | TypeScript / Node.js | Python | Go |
|---|---|---|---|
| Playwright support | First-class (official) | Official | Community binding (Node.js RPC bridge) |
| Concurrent media pipeline fit | Poor (event loop; Worker Threads) | Poor (GIL) | **Excellent** (goroutines + channels) |
| FFmpeg integration | Adequate (child process) | Good (PyAV) | Adequate (binary wrapper or CGo) |
| Container binary model | Runtime required | Runtime required | Single binary + Node.js driver |
| PTS / timing correctness | Moderate | Moderate | **Strong** (`time.Duration`, preemptive scheduler) |
| Panic isolation per component | No native equivalent | No native equivalent | `recover` per goroutine |

---

## Decision

**Go**, using `playwright-community/playwright-go` for Playwright integration.

The media pipeline is the dominant architectural concern. ADR-001 and ADR-002 together specify a concurrent, goroutine-based design with in-process buffered channels, PTS arithmetic, and per-component panic recovery. Go's runtime is the only realistic match for this model. TypeScript's event loop and Python's GIL both require significant workarounds to express the same architecture, introducing complexity and failure modes that Go avoids structurally.

The playwright-go binding's Node.js RPC bridge is a known and accepted trade-off. It does not affect the application's API or behavior; it adds ~50 MB to the container image and a Node.js process to the runtime dependency graph. This is acceptable given that Chromium itself already dominates image size (~300–400 MB).

**Language version:** the minimum Go version must be pinned in `go.mod`. The minimum version must support the standard library features used (particularly `sync`, `time`, `net/http`, and `subtle`). Go 1.21 or later is required for the `log/slog` structured logging package, which is the recommended implementation for the ADR-005 log format.

---

## Consequences

**Positive:**
- Goroutines and buffered channels directly express the ADR-001/ADR-002 media pipeline architecture
- `time.Duration` and the preemptive scheduler make PTS-based drift correction correct and straightforward
- Per-goroutine `recover` is idiomatic; panic isolation requires no external framework
- Single compiled binary simplifies the container entrypoint and startup validation (ADR-003)
- Strong static typing catches interface contract violations (ADR-001 `ZoomClient`, `pkg/media` types) at compile time

**Negative:**
- playwright-go is a community binding, not an official Microsoft release; API lag behind upstream Playwright is possible
- The Node.js Playwright driver ships inside the container; Go does not eliminate the Node.js dependency
- CGo (if used for FFmpeg native bindings) complicates cross-compilation and adds C toolchain dependency to the build; the binary wrapper approach avoids this at some cost to performance
- Smaller ecosystem than TypeScript for web-adjacent tooling; some Playwright-adjacent utilities (e.g., HAR processing libraries) may not have Go equivalents

**Accepted trade-offs:**
- Community Playwright binding accepted over official TypeScript binding; the media pipeline fit outweighs the Playwright ecosystem advantage
- Node.js runtime in container accepted; it is dwarfed by Chromium in image size

---

## Revision Triggers

1. **playwright-go falls significantly behind upstream Playwright** — if the binding does not support a Playwright API needed for the Zoom Web App join flow within a reasonable release window, evaluate switching to TypeScript for `pkg/bot` only (mixed-language build via subprocess), or revisit the language decision entirely
2. **SDK migration (ADR-004b)** — the Zoom Meeting SDK for Linux is a C++ library; CGo or a subprocess bridge would be required; re-evaluate language fit at that point
3. **FFmpeg CGo binding required** — if performance requirements for file decoding cannot be met with the binary wrapper approach, adding CGo changes the build complexity and must be explicitly decided

---

## Related ADRs

- **ADR-001 (Service Decomposition):** goroutines and channels are the Go-specific expression of the in-process pipeline this ADR's decision enables
- **ADR-002 (Media Format Contracts):** `AudioFrame` and `VideoFrame` as Go structs; `time.Duration` for PTS; `sync` primitives for ring buffer and FIFO queue
- **ADR-003 (Docker Deployment):** single Go binary in the container; Node.js Playwright driver bundled alongside it; Go version pinned in `go.mod` and documented in `ARCHITECTURE.md`
- **ADR-004 (Zoom Integration):** playwright-go (`github.com/playwright-community/playwright-go`) is the specific binding used; Playwright version must be kept in sync between `go.mod` and the Dockerfile
- **ADR-005 (Observability):** `log/slog` (Go 1.21+) is the recommended structured logger for the ADR-005 log schema; `net/http` for the HTTP server; `nhooyr.io/websocket` or `gorilla/websocket` for the WebSocket endpoint
