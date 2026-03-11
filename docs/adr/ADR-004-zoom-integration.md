# ADR-004: Zoom Integration Approach

## Status
🟡 **Proposed**

---

## Context

Stenosaur must join Zoom meetings as a named participant and inject audio and video media streams. The application is deployed exclusively in Docker containers and must work across Linux servers, macOS Docker Desktop, Windows Docker Desktop, and CI/CD pipelines.

A hard project constraint is that **the bot must not require registering a Zoom App in the Zoom App Marketplace**. Registration adds approval gates, licensing ambiguity, and deployment friction that is incompatible with self-hosted and open-source use.

Zoom offers four integration paths. This ADR evaluates each against the Docker-first, no-registration constraints.

---

## Non-Negotiable Constraints

| Constraint | Rationale |
|---|---|
| Docker container as sole deployment unit | All environments run the same image; no per-host setup |
| No Zoom App Marketplace registration | Avoids approval delays and licensing restrictions |
| Audio and video media injection required | Core requirement; read-only bots are not sufficient |
| No dependency on a host display server | CI/CD pipelines and cloud VMs have no physical or virtual display |

---

## Options Evaluated

### Option A: Zoom Desktop Client Automation

The native Zoom Linux desktop client is a GUI Electron application. Headless containerization requires Xvfb inside the container and v4l2loopback on the host kernel for virtual video.

v4l2loopback is a kernel module that must be loaded on the host kernel — it cannot be loaded from within a container without `--privileged` mode. macOS and Windows Docker Desktop run containers inside a Linux VM whose kernel does not expose v4l2loopback to the host OS. This path cannot provide video injection on macOS or Windows Docker hosts, and is blocked in most managed cloud and CI environments where privileged mode is unavailable.

Audio injection via in-container PulseAudio is tractable but insufficient without video.

**Verdict: Incompatible with the Docker constraint.** Video injection requires host kernel access that is unavailable across the required deployment targets.

---

### Option B: Zoom PWA / Web Client Automation ✅

The Zoom Web App (`app.zoom.us/wc/join`) allows joining meetings through a browser. A headless Chromium instance, controlled by Playwright, can navigate the join URL and participate as a named guest without installing the desktop client.

Chromium runs entirely inside the container with no host dependencies. Audio is injected via a PulseAudio virtual sink running in userspace inside the container (no host kernel dependency). Video is injected via Chromium's `--use-file-for-fake-video-capture` flag with a `.y4m` pipe. No v4l2loopback, no Xvfb, no host device access required. The container works identically on Linux, macOS Docker Desktop, Windows Docker Desktop, and CI.

No registration is required. The bot joins as a named guest, identical to a human opening the web client.

**Playwright is selected over Puppeteer** for its first-class support for browser launch flags, media permission handling, and built-in waiting strategies for dynamic page content. Both tools are technically capable; Playwright is better suited to the reliability requirements of an automated joining flow.

**Known risks:**
- The Zoom Web App UI is a React SPA; element selectors are fragile to A/B tests and releases
- Headless Chromium behaves differently from headed Chromium in some edge cases; both must be tested
- Zoom's Terms of Service do not explicitly sanction automated browser participation
- Zoom may add headless browser detection over time

**Verdict: Selected.**

---

### Option C: Zoom Meeting SDK for Linux

Zoom publishes an official Meeting SDK as a native C++ shared library with first-party headless Docker support. Raw audio and video are available via SDK callbacks, enabling direct programmatic media injection without virtual devices or kernel modules. This is technically the strongest option.

However, the SDK requires a Client ID and Client Secret from a registered Zoom App Marketplace application. This is enforced cryptographically at the SDK level — it cannot be worked around. The constraint is violated.

**Verdict: Technically preferred, blocked by no-registration constraint. Deferred to ADR-004b.**

---

### Option D: Zoom REST API

A management API for scheduling, user management, and reporting. Does not support joining meetings as a participant or injecting media. Cannot fulfill the core requirement.

**Verdict: Excluded.**

---

## Comparison Matrix

| Factor | Desktop Client | PWA Automation | Meeting SDK | REST API |
|---|---|---|---|---|
| Registration required | No | No | **Yes** | Yes |
| Docker-native (no host deps) | ❌ | ✅ | ✅ | ✅ |
| Works on macOS/Windows Docker | ❌ | ✅ | ✅ | N/A |
| Works in CI/headless | ❌ | ✅ | ✅ | N/A |
| Audio injection | PulseAudio (container) | Chromium fake device | SDK callback | ❌ |
| Video injection | ❌ v4l2loopback (host) | Chromium fake device | SDK callback | ❌ |
| UI fragility | High | Medium–High | None | None |
| First-party Docker support | No | No | **Yes** | Yes |

---

## Decision

**PWA automation via Playwright (Option B).**

This is the only path that satisfies both the Docker-only deployment requirement and the no-registration constraint. The decision is constraint-driven. The Meeting SDK is technically superior in every dimension except registration, and should be adopted immediately if that constraint is relaxed.

The bot must expose its Zoom session management behind a `ZoomClient` interface so the PWA implementation can be swapped for an SDK implementation without changing `pkg/bot`'s callers.

---

## Consequences

**Positive:**
- No Zoom App Marketplace registration; works immediately with any meeting URL
- Fully self-contained container; no host dependencies on any supported platform
- Audio and video injection achievable without kernel modules

**Negative:**
- Zoom Web App UI changes will break selectors; ongoing maintenance required
- Chromium adds ~300–400 MB to the image
- Fake media device flags are Chromium-internal; behavior is not guaranteed across all Chromium versions
- TOS ambiguity around automated browser participation is a legal/policy risk, not a technical one

**Accepted trade-offs:**
- UI automation fragility is accepted in exchange for zero registration friction
- The SDK is acknowledged as the better long-term path; this decision is explicitly temporary

---

## Revision Triggers

Supersede with **ADR-004b** if any of the following occur:

1. **Registration constraint is relaxed** — adopt the Meeting SDK immediately; it eliminates UI fragility and has first-party Docker support
2. **PWA automation breaks ≥ 3 times in 6 months** — sustained maintenance burden signals the path is not viable
3. **Zoom adds headless browser detection** — if the PWA path becomes technically blocked, reconsider the registration trade-off
4. **Zoom SDK becomes available without registration** — revisit if unauthenticated SDK access becomes available

---

## Related ADRs

- **ADR-001 (Service Decomposition):** `pkg/bot` is the consumer of the `ZoomClient` interface; the PWA implementation lives there
- **ADR-002 (Media Format Contracts):** audio and video sources injected via this path must conform to the 48 kHz / YUV420p contracts regardless of injection mechanism
- **ADR-003 (Docker Deployment):** PWA path requires no host device dependencies; this is the direct reason ADR-003 requires no `devices:` block
