# ADR-001: Service Decomposition and Process Topology

## Status
🟡 **Proposed**

---

## Context

Stenosaur is a Zoom meeting bot that joins meetings as a named participant and injects audio and video media streams. It is operated via a web admin UI accessible from other machines on the local network.

The process and container topology must be decided before implementation begins. This decision constrains how components communicate, how the application is deployed, and how concerns are separated in the codebase.

**Fixed constraints:**
- Deployed exclusively as Docker containers with no host-level device dependencies (ADR-004)
- Single concurrent Zoom session per deployment
- Chromium runs inside the container to drive the Zoom Web App (ADR-004)
- Web admin UI must be reachable from other machines on the local network

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

One Chromium instance serves both the Zoom session (via one browser context) and the webpage renderer (via a separate browser context). Media frames pass between packages as in-memory buffers with no serialization.

**Verdict: Recommended.**

---

## Decision

**Single container with embedded HTTP server.**

The decomposition into multiple containers is appropriate when components have meaningfully different scaling, hardware, or lifecycle requirements. None of those conditions exist for a single-session deployment. The renderer shares a Chromium dependency with the bot; the controller has no lifecycle independent of the bot. Separating them adds IPC complexity and Chromium duplication with no practical benefit.

Package boundaries within the codebase enforce separation of concerns. This is a decision about process and deployment topology, not about code structure.

**Package ownership:**

| Package | Responsibility |
|---|---|
| `pkg/bot` | Zoom session lifecycle; consuming media frames and injecting them into the Zoom session |
| `pkg/renderer` | Producing media frames from all source types; normalizing to internal format |
| `pkg/controller` | HTTP server; REST API; web admin UI; WebSocket log stream |
| `pkg/media` | Shared frame types and source interfaces; no internal dependencies |

**Dependency direction:** `pkg/bot` and `pkg/controller` depend on `pkg/media`; `pkg/media` depends on nothing else internal.

---

## Consequences

**Positive:**
- Single container; no inter-container networking or IPC to configure
- One Chromium process; no duplication, no cross-process frame transport
- Media pipeline entirely in-process; no serialization overhead
- One log stream, one binary, one restart unit

**Negative:**
- A crash in any package crashes the process; mitigated by per-goroutine panic recovery and a container restart policy (ADR-003)
- All packages redeploy together; renderer cannot be updated without interrupting an active session
- Multi-session support requires architectural revision (see Revision Triggers)

---

## Revision Triggers

Create **ADR-001b** if any of the following occur:

1. **Concurrent session requirement** — 2+ simultaneous sessions make renderer pool scaling meaningful
2. **GPU rendering requirement** — video rendering becomes GPU-bound and requires separate hardware
3. **Multi-tenant deployment** — independent operators need isolated controller instances
4. **SDK migration** — ADR-004b adopts the Meeting SDK; re-evaluate whether its media model benefits from process separation
5. **Public admin UI** — web UI exposed beyond the LAN; consider extracting the controller behind an auth proxy

---

## Related ADRs

- **ADR-002 (Media Format Contracts):** defines the frame types and channel contracts that flow between `pkg/renderer` and `pkg/bot`
- **ADR-003 (Docker Deployment):** defines the single-service Compose structure and port mapping that this topology requires
- **ADR-004 (Zoom Integration):** the PWA/Playwright decision places Chromium in the container; this ADR's single-container conclusion follows directly from it
- **ADR-005 (Observability):** defines the HTTP endpoint surface and WebSocket log stream served by `pkg/controller`
