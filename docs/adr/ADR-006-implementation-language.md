# ADR-006: Implementation Language

## Status
🟡 **Proposed**

---

## Context

No implementation language has been explicitly decided. Prior ADRs use Go syntax in interface examples and imply Go throughout, but this has never been stated as a decision. It must be, because the language choice directly affects:

- How Playwright is integrated (ADR-004 selected Playwright; binding quality varies significantly by language)
- How FFmpeg is used for file and stream decoding (ADR-010)
- Whether the container ships a single self-contained binary or requires a runtime
- How the in-process media pipeline (ADR-001) is expressed — goroutines and channels vs. threads vs. async runtimes

The language must be decided before any code is written.

**Candidate languages** are those with Playwright support and FFmpeg integration sufficient for production use: Go, TypeScript/Node.js, and Python. These are the only realistic options given the Playwright constraint.

---

## Constraints Inherited from Prior ADRs

| Constraint | Source |
|---|---|
| Real-time in-process media pipeline with buffered channels | ADR-001, ADR-002 |
| Per-component panic isolation | ADR-001 |
| Playwright is the browser automation library | ADR-004 |
| Single compiled binary preferred for container simplicity | ADR-003 |
| FFmpeg invoked as a subprocess (decided — no CGo) | This ADR |

---

## Options Evaluated

### Option A: TypeScript (Node.js)

Playwright is a Microsoft project and Node.js/TypeScript is its first-class language. The Playwright API surface is most complete, best documented, and most promptly updated in TypeScript.

FFmpeg integration is available via `fluent-ffmpeg` or direct child process invocation — adequate for decoding.

The real-time media pipeline is the significant weakness. Node.js has a single-threaded event loop. True parallel execution of the renderer and bot goroutines requires Worker Threads, which communicate via `postMessage` and SharedArrayBuffer rather than in-process typed channels. The audio ring buffer and video FIFO queue (ADR-002) are expressible but awkward — shared memory between workers is more error-prone than Go channels. PTS-based drift correction in a timing-sensitive pipeline is harder to reason about when the execution model is cooperative rather than preemptive.

**Verdict:** Best Playwright integration; weakest fit for the concurrent real-time media pipeline.

---

### Option B: Python

Playwright has official Python bindings, well-maintained by Microsoft. FFmpeg integration is mature via `PyAV` or `ffmpeg-python`.

Python's concurrency story is worse than Node.js for this use case. The GIL prevents true parallel execution of CPU-bound threads. The audio synthesis and file decoding workloads in `pkg/renderer` are CPU-bound; running them concurrently with Playwright browser automation requires either multiprocessing (heavy) or async/await (cooperative, not parallel). Neither maps cleanly to the in-process buffered channel model specified in ADR-001 and ADR-002.

**Verdict:** Adequate Playwright support; poor fit for concurrent CPU-bound media pipeline; eliminated.

---

### Option C: Go ✅

Go's concurrency model — goroutines and buffered channels — maps directly to the architecture specified in ADR-001 and ADR-002. The renderer and bot run as goroutines; the audio ring buffer and video FIFO queue are natural buffered channels or purpose-built ring buffer structs. PTS-based drift correction is straightforward with `time.Duration` arithmetic. Per-goroutine `recover` for panic isolation (ADR-001) is idiomatic Go.

**FFmpeg integration:** FFmpeg is invoked as a subprocess. The `ffmpeg` binary is called as a child process; raw PCM (for audio) and raw YUV420p (for video) are piped from FFmpeg's stdout into `AudioFrame` and `VideoFrame` structs. This approach is decided here and is not open for implementation-level variation.

The CGo alternative — native bindings via `goav`/`go-libav` — was evaluated and rejected. CGo requires a C toolchain in the build environment, prevents static binary builds, complicates cross-compilation, and adds significant dependency management overhead. The subprocess approach sustains 48 kHz audio and 30 FPS 720p video with negligible latency on any modern host. If profiling reveals a genuine bottleneck, CGo can be reconsidered as a targeted optimisation (see Revision Triggers).

**Playwright:** Go is not an officially supported Playwright language. `playwright-community/playwright-go` is a community binding that wraps the upstream Node.js Playwright driver via an RPC bridge over stdio — the Go binary still ships and manages a bundled Node.js runtime. The RPC bridge is transparent to application code. The risk is lag between upstream Playwright releases and the community binding's updates; in practice the binding covers the full API surface needed for the join flow.

**Verdict: Selected.**

---

## Comparison Summary

| Factor | TypeScript / Node.js | Python | Go |
|---|---|---|---|
| Playwright support | First-class (official) | Official | Community binding (Node.js RPC bridge) |
| Concurrent media pipeline fit | Poor (event loop) | Poor (GIL) | **Excellent** (goroutines + channels) |
| FFmpeg integration | Adequate (child process) | Good (PyAV) | **Subprocess (decided; CGo available if needed)** |
| Container binary model | Runtime required | Runtime required | Single binary + Node.js Playwright driver |
| PTS / timing correctness | Moderate | Moderate | **Strong** (`time.Duration`, preemptive scheduler) |
| Panic isolation per component | No native equivalent | No native equivalent | `recover` per goroutine |

---

## Decision

**Go 1.21+** with `playwright-community/playwright-go` and FFmpeg subprocess integration.

The media pipeline is the dominant architectural concern. Go's runtime is the only realistic match for the concurrent, goroutine-based design with in-process buffered channels, PTS arithmetic, and per-component panic recovery specified in ADR-001 and ADR-002.

The playwright-go binding's Node.js RPC bridge is a known and accepted trade-off. It does not affect the application's API or behaviour; it adds ~50 MB to the container image and a Node.js process to the runtime dependency graph. This is acceptable given that Chromium itself dominates image size.

**Language version:** Go 1.21 minimum. Required for `log/slog` (ADR-005 log format). Pinned in `go.mod` and documented in `ARCHITECTURE.md`.

**playwright-go version** must be kept in lockstep with the Playwright driver version in the Dockerfile. The driver is pinned in the Dockerfile; the binding version in `go.mod` must match.

---

## Consequences

**Positive:**
- Goroutines and channels express the media pipeline directly; no translation layer
- Single compiled binary simplifies the container startup sequence
- `time.Duration` and the preemptive scheduler make PTS drift correction straightforward
- `recover` per goroutine provides idiomatic panic isolation

**Negative:**
- playwright-go is a community binding; API lag behind upstream Playwright is possible
- The Node.js Playwright driver ships inside the container; Go does not eliminate the Node.js dependency
- FFmpeg subprocess adds minor per-frame pipe overhead vs. CGo native bindings; accepted as sufficient
- Smaller ecosystem than TypeScript for web-adjacent tooling

**Accepted trade-offs:**
- Community Playwright binding over official TypeScript binding; the media pipeline fit outweighs the Playwright ecosystem advantage
- Node.js runtime in container accepted; dwarfed by Chromium in image size
- FFmpeg subprocess over CGo; simpler build, sufficient throughput for target workloads

---

## Revision Triggers

1. **playwright-go falls significantly behind upstream Playwright** — if the binding does not support a Playwright API needed for the Zoom Web App join flow within a reasonable window, evaluate switching to TypeScript for `pkg/bot` only (mixed-language build via subprocess), or revisit the language decision
2. **SDK migration (ADR-004b)** — the Zoom Meeting SDK for Linux is a C++ library; CGo or a subprocess bridge would be required; re-evaluate language fit at that point
3. **FFmpeg subprocess throughput is a proven bottleneck** — if profiling shows that pipe throughput or process startup latency is a production bottleneck for a specific workload, evaluate CGo bindings (`go-libav`) as a targeted replacement; this requires explicitly adding CGo to the build and a new ADR before implementation

---

## Related ADRs

- **ADR-001 (Service Decomposition):** `AudioFrame` and `VideoFrame` as Go structs; goroutines for renderer and bot; `sync` primitives for ring buffer and FIFO queue
- **ADR-002 (Media Format Contracts):** `time.Duration` for PTS; `sync` primitives for thread-safe buffers
- **ADR-003 (Docker Deployment):** single Go binary in the container; Node.js Playwright driver bundled alongside it
- **ADR-004 (Zoom Integration):** `playwright-community/playwright-go` is the specific binding used; version must stay in sync with the Dockerfile
- **ADR-005 (Observability):** `log/slog` (Go 1.21+) for structured logging; `net/http` for the HTTP server
- **ADR-010 (External Stream Sources):** `StreamDecoder` uses FFmpeg subprocess for decoding; `yt-dlp` subprocess for URL resolution
