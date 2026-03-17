# ADR-001: Service Decomposition and Process Topology

## Status
🟡 **Proposed**

---

## Context

Stenosaur is a Zoom meeting bot that joins meetings as a named participant and injects audio and video media streams. It is operated via a web admin UI accessible from other machines on the local network.

The process and container topology must be decided before implementation begins. This decision constrains how components communicate, how the application is deployed, and how concerns are separated in the codebase.

**Fixed constraints:**
- Deployed exclusively as Docker containers with no host-level device dependencies (ADR-003)
- Single concurrent Zoom session per deployment
- Chromium is required inside the container to drive the Zoom Web App (ADR-004)
- Web admin UI must be reachable from other machines on the local network
- Audio injection uses `--use-fake-device-for-media-stream` and PulseAudio (ADR-008); video injection uses `--use-file-for-fake-video-capture` with a `.y4m` named pipe (ADR-009) — both are browser-level Chromium launch flags that apply to the entire browser process, not per context

---

## Options Evaluated

### Option A: Three Separate Containers

Bot, renderer, and controller each run in their own container and communicate over a Docker network.

ADR-004 places a Chromium instance inside the bot container to drive the Zoom Web App. This option requires a second Chromium instance in the renderer container, with media frames serialized and transported across the Docker network for injection. Cross-container frame transport is the worst possible IPC mechanism for a real-time media pipeline: it adds serialization overhead, latency, and a versioned wire format to the most timing-sensitive part of the system.

The resilience argument — that a renderer crash does not crash the bot — does not hold. If the renderer fails, the bot has no media and is operationally non-functional regardless of whether its process stays alive. Container isolation obscures the failure without improving it.

With one concurrent session, the scaling argument (renderer on different hardware) never applies.

**Verdict:** Not recommended.

---

### Option B: Two Containers (bot + renderer combined, controller separate)

Eliminates Chromium duplication and cross-container media IPC. However, the controller has no workload independence from the bot — it reads bot state and issues commands to it. Separating them requires an IPC mechanism between two containers for a capability that an embedded HTTP server provides for free. An HTTP server bound to a mapped Docker port is accessible from the LAN identically to a standalone container.

**Verdict:** Eliminates the worst problems of Option A but retains unnecessary separation. Not recommended.

---

### Option C: Single Container with Embedded HTTP Server ✅

One container, one process. Internal concerns are separated into distinct packages communicating via in-process channels and shared interfaces. The admin HTTP server is embedded in the process and exposed via a mapped Docker port.

Media frames pass between packages as in-memory buffers with no serialization. Two Chromium instances run inside the same container, managed by the same Playwright runtime — see Chromium Instance Model below.

**Verdict: Recommended.**

---

## Decision

**Single container with embedded HTTP server.**

The decomposition into multiple containers is appropriate when components have meaningfully different scaling, hardware, or lifecycle requirements. None of those conditions exist for a single-session deployment. The controller has no lifecycle independent of the bot. Separating packages into separate containers adds IPC complexity with no practical benefit.

Package boundaries within the codebase enforce separation of concerns. This is a decision about process and deployment topology, not about code structure.

**Package ownership:**

| Package | Responsibility |
|---|---|
| `pkg/bot` | Zoom session lifecycle; consuming media frames and injecting them into the Zoom session; session state machine (ADR-014) |
| `pkg/renderer` | Producing media frames from all source types; normalizing to internal format contracts |
| `pkg/controller` | HTTP server; REST API; web admin UI; WebSocket log stream |
| `pkg/media` | Shared frame types, source interfaces, and signal interfaces; no internal dependencies |

**Dependency direction:** `pkg/bot` and `pkg/controller` depend on `pkg/media`. `pkg/renderer` depends on `pkg/media` only. `pkg/media` depends on nothing else internal. No circular imports.

---

## Chromium Instance Model

Two Chromium instances run inside the container, both managed by the single `playwright-go` Node.js RPC bridge process:

**Chromium A — zoom instance** (`pkg/bot`)
Launched with fake media device flags:
```
--use-fake-device-for-media-stream
--use-file-for-fake-video-capture=<fifo_path>
--allow-file-access-from-files
```
Drives the Zoom Web App session. Audio injection is via PulseAudio (ADR-008); video injection is via the `.y4m` FIFO (ADR-009).

**Chromium B — renderer instance** (`pkg/renderer`)
Launched without fake media device flags. Navigates to configured source URLs. Captures frames via screenshot-based rendering. Has no awareness of the Zoom session.

**Why two instances are required:** `--use-file-for-fake-video-capture` is a browser-level launch argument — it applies to the entire Chromium process, not per browser context. If both the Zoom session and `WebRenderer` shared one Chromium instance, the `.y4m` injection pipe would also be the "camera" seen by the rendered webpage, creating a conflict with no workaround. Two separate instances with separate flag profiles resolve this cleanly.

The two instances add approximately 600–800 MB combined resident memory. This is accepted: Chromium is required regardless of the number of instances, and the flag conflict has no in-process solution.

---

## Architecture

```
┌─────────────────────────────────────────────────────┐
│                  Docker Container                    │
│                                                     │
│  ┌─────────────────────────────────────────────┐   │
│  │              stenosaur process               │   │
│  │                                             │   │
│  │  pkg/renderer          pkg/bot              │   │
│  │  ┌──────────────┐      ┌─────────────────┐  │   │
│  │  │ WebRenderer  │─────▶│  ZoomClient     │  │   │
│  │  │ FileDecoder  │ buf  │  (Playwright/   │  │   │
│  │  │ AudioSynth   │      │   PWA)          │  │   │
│  │  │ StreamDecoder│      └─────────────────┘  │   │
│  │  │ Playlist     │                           │   │
│  │  └──────────────┘                           │   │
│  │                                             │   │
│  │  pkg/controller                             │   │
│  │  ┌──────────────┐                           │   │
│  │  │  HTTP :8080  │                           │   │
│  │  └──────────────┘                           │   │
│  └─────────────────────────────────────────────┘   │
│                                                     │
│  Chromium A — zoom instance (fake device flags)     │
│  Chromium B — renderer instance (no device flags)   │
│  PulseAudio virtual sinks (userspace, no host deps) │
│  Node.js Playwright driver (shared RPC bridge)      │
└─────────────────────────────────────────────────────┘
```

**Media pipeline (in-process):**
```
renderer.WebRenderer   ─┐   (Chromium B: screenshot)
renderer.FileDecoder   ─┤
renderer.StreamDecoder ─┼─▶ AudioFrame/VideoFrame channels ─▶ bot.InjectMedia()
renderer.Playlist      ─┤                                          │
renderer.AudioSynth    ─┘                                    Chromium A
                                                         (fake device injection)
```

---

## Consequences

**Positive:**
- Single container; no inter-container networking or IPC to configure
- Media pipeline entirely in-process; no serialization overhead
- One log stream, one binary, one restart unit
- Chromium B operates as a clean browser with no fake device constraints

**Negative:**
- Two Chromium instances add ~600–800 MB combined resident memory; container memory limits must account for this
- A crash in any package crashes the process; mitigated by per-goroutine panic recovery (ADR-014) and container restart policy (ADR-003)
- All packages redeploy together; renderer cannot be updated without interrupting an active session
- Multi-session support requires architectural revision (see Revision Triggers)

---

## Revision Triggers

Create **ADR-001b** if any of the following occur:

1. **Concurrent session requirement** — 2+ simultaneous sessions make renderer pool scaling meaningful
2. **GPU rendering requirement** — video rendering becomes GPU-bound and requires separate hardware
3. **SDK migration (ADR-004b)** — Zoom Meeting SDK adoption; re-evaluate whether its media model benefits from process separation
4. **Public admin UI** — web UI exposed beyond the LAN; consider extracting the controller behind an auth proxy

---

## Related ADRs

- **ADR-002 (Media Format Contracts):** defines the frame types and buffer contracts that flow between `pkg/renderer` and `pkg/bot` via in-process channels
- **ADR-003 (Docker Deployment):** defines the single-service Compose structure, port mapping, and environment variable model
- **ADR-004 (Zoom Integration):** the PWA/Playwright decision places Chromium in the container; the fake device flag constraint drives the two-instance model
- **ADR-005 (Observability):** defines the HTTP endpoint surface served by `pkg/controller`
- **ADR-008 (Audio Injection):** Chromium A fake audio device; PulseAudio null sink
- **ADR-009 (Video Injection):** Chromium A fake video device; `.y4m` FIFO
- **ADR-014 (Session State Machine):** defines the state model and crash recovery logic owned by `pkg/bot`
