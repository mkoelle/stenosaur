# ADR-013: Audio Mixing, Ducking, and Sound Effect Overlay

## Status
🟡 **Proposed**

---

## Context

ADR-012 introduces a VAD signal that detects when meeting participants are speaking. That signal needs to modify the injected audio volume. Additionally, the use case of layering a sound effect over a primary audio stream (playlist, stream, or any other source) requires combining two `AudioSource` streams into one output frame sequence.

Both capabilities — gain-based ducking and multi-source mixing — are modifications to the audio frame stream that must be transparent to `pkg/bot` and the buffer layer. They must compose with each other and with the existing source types (`FileDecoder`, `StreamDecoder`, `Playlist`).

Three distinct concerns are addressed here:

1. **`DuckingSource`** — a wrapper `AudioSource` that applies a variable gain to frames from an inner source, driven by the `DuckingController` signal from ADR-012.
2. **`Mixer`** — a type that combines a primary `AudioSource` and an overlay `AudioSource` into a single output stream, applying ducking to the primary while the overlay is active.
3. **`POST /media` trigger path** — how an API call reaches the `Mixer` to start a sound effect overlay without going through the renderer or bot business logic.

---

## Constraint: Composability Over New Interfaces

The `AudioSource` interface (ADR-002) has exactly two methods: `ReadAudio` and `Close`. This ADR must not add methods to that interface — doing so would require all existing implementations to be updated.

All new types must implement `AudioSource` by wrapping other `AudioSource` instances. Gain control, mixing, and ducking are applied inside `ReadAudio` with no visibility to the caller. The bot consumes whatever `AudioSource` it receives without knowing whether it is a `FileDecoder`, a `Playlist`, a `DuckingSource`, or a `Mixer`.

---

## Part 1: `DuckingSource`

`DuckingSource` wraps any `AudioSource` and implements `DuckingController` (ADR-012). The VAD goroutine in `pkg/bot` calls `SetVADActive` on it; `ReadAudio` applies the current gain to each frame.

### Gain model

Two gain values are defined:

| Variable | Default | Description |
|---|---|---|
| `DUCK_GAIN` | `0.15` | Gain applied to primary source when voice is active (0.0–1.0) |
| `DUCK_RAMP_MS` | `200` | Milliseconds over which gain transitions between full and ducked |

Abrupt gain changes produce audible clicks. Gain transitions are applied as a linear ramp over `DUCK_RAMP_MS`. At 10 ms per frame, 200 ms = 20 frames. The ramp increments or decrements by `(1.0 - DUCK_GAIN) / rampFrames` per frame until the target gain is reached.

```go
type DuckingSource struct {
    inner      media.AudioSource
    currentGain float32      // current gain, updated per frame during ramp
    targetGain  float32      // 1.0 (unduck) or DUCK_GAIN (duck)
    rampStep    float32      // per-frame gain delta during ramp
    mu          sync.Mutex
}

func (d *DuckingSource) SetVADActive(active bool) {
    d.mu.Lock()
    defer d.mu.Unlock()
    if active {
        d.targetGain = d.duckGain
    } else {
        d.targetGain = 1.0
    }
}

func (d *DuckingSource) ReadAudio(ctx context.Context) (media.AudioFrame, error) {
    frame, err := d.inner.ReadAudio(ctx)
    if err != nil { return frame, err }
    d.mu.Lock()
    gain := d.stepGain()   // advance ramp by one frame
    d.mu.Unlock()
    for i := range frame.Samples {
        frame.Samples[i] = int16(float32(frame.Samples[i]) * gain)
    }
    return frame, nil
}
```

`DuckingSource` lives in `pkg/renderer`. It implements both `media.AudioSource` and `media.DuckingController`, satisfying ADR-012's contract.

### When VAD is disabled

When `VAD_ENABLED=false` (ADR-012), `DuckingSource` is not constructed. A no-op `DuckingController` is passed to `pkg/bot`. The audio source chain passes through to the buffer directly. No gain multiplication occurs. There is no performance cost.

---

## Part 2: `Mixer`

`Mixer` combines a primary `AudioSource` with a transient overlay `AudioSource`. The primary runs continuously. The overlay is nil until activated; when active, its frames are added to the primary's frames with the primary ducked to `DUCK_GAIN`.

### Composition

```
              ┌─────────────────────────────────┐
              │             Mixer               │
              │                                 │
 primary ─────┤ × duckGain (while overlay active)├──▶ mixed AudioFrame
 overlay ─────┤ × 1.0                           │
              └─────────────────────────────────┘
```

When no overlay is active, `Mixer.ReadAudio` is equivalent to reading directly from `primary` with no gain applied — zero overhead.

### Sample mixing and clipping

Mixing two int16 streams requires promotion to int32 to detect overflow before clamping back to int16:

```go
mixed := int32(float32(primary.Samples[i]) * gain) + int32(overlay.Samples[i])
if mixed >  32767 { mixed =  32767 }
if mixed < -32768 { mixed = -32768 }
frame.Samples[i] = int16(mixed)
```

Hard clipping is used rather than soft clipping. Sound effects are expected to be short and at a lower amplitude than the primary stream; hard clipping artefacts are unlikely in practice.

### Overlay lifecycle

```go
type Mixer struct {
    primary    media.AudioSource
    overlay    media.AudioSource   // nil when inactive
    overlayMu  sync.Mutex
    duckGain   float32
    rampFrames int
    currentGain float32
    targetGain  float32
}

func (m *Mixer) TriggerOverlay(source media.AudioSource) {
    m.overlayMu.Lock()
    defer m.overlayMu.Unlock()
    if m.overlay != nil {
        m.overlay.Close()   // discard any current overlay immediately
    }
    m.overlay = source
    m.targetGain = m.duckGain  // begin duck ramp
}

func (m *Mixer) ReadAudio(ctx context.Context) (media.AudioFrame, error) {
    frame, err := m.primary.ReadAudio(ctx)
    if err != nil { return frame, err }
    m.overlayMu.Lock()
    overlay := m.overlay
    gain := m.stepGain()
    m.overlayMu.Unlock()
    if overlay == nil { return frame, nil }
    ofr, oerr := overlay.ReadAudio(ctx)
    if oerr != nil {
        // overlay exhausted: close it, begin unduck ramp
        m.overlayMu.Lock()
        m.overlay.Close()
        m.overlay = nil
        m.targetGain = 1.0
        m.overlayMu.Unlock()
        return frame, nil  // return this frame unmodified; ramp begins next frame
    }
    // mix overlay into ducked primary
    for i := range frame.Samples {
        mixed := int32(float32(frame.Samples[i])*gain) + int32(ofr.Samples[i])
        if mixed >  32767 { mixed =  32767 }
        if mixed < -32768 { mixed = -32768 }
        frame.Samples[i] = int16(mixed)
    }
    return frame, nil
}
```

`Mixer` also implements `media.AudioSource` directly, so it can itself be wrapped by a `DuckingSource` if both VAD ducking and overlay mixing are active simultaneously.

### Composition with `DuckingSource`

When both VAD ducking (ADR-012) and overlay mixing are needed, the two are composed:

```
DuckingSource(Mixer(Playlist, overlay))
         │
         └── VAD signal from pkg/bot via DuckingController
```

`DuckingSource` applies VAD-driven gain to the entire mixed output. `Mixer` applies overlay-driven gain to the primary within the mix. The gains are independent and multiply: if the VAD ducks the output to 0.15 and the overlay ducks the primary to 0.2, the primary is heard at 0.15 × 0.2 = 0.03 of its original volume during active voice + active overlay — effectively silent. This is the expected behaviour: the sound effect is not buried under both the primary and the participants' voices.

---

## Part 3: `POST /media` Trigger Path

`POST /media` (ADR-005) currently uploads a file to `MEDIA_DIR`. This ADR extends it with an `action` field:

```json
{
  "file": "<filename in MEDIA_DIR>",
  "action": "overlay"
}
```

When `action: overlay`, `pkg/controller` calls `Mixer.TriggerOverlay(FileDecoder(file))` on the active audio source, if and only if the active source is a `Mixer` (or wraps one). If the active source does not support overlays, the endpoint returns HTTP 422 with:

```json
{ "error": "overlay_not_supported", "message": "active source does not support overlay injection" }
```

**Dependency direction:** `pkg/controller` must not import `pkg/renderer`. The `TriggerOverlay` call path is expressed through a new interface in `pkg/media`:

```go
// OverlayController is implemented by pkg/renderer.Mixer and optionally
// by pkg/renderer.DuckingSource (which delegates to its inner Mixer).
// pkg/controller uses this interface to trigger sound effect overlays
// without importing pkg/renderer. (ADR-013)
type OverlayController interface {
    TriggerOverlay(source AudioSource) error
}
```

`pkg/controller` receives an `OverlayController` at construction time (may be nil if no mixer is configured). The `POST /media` handler calls `TriggerOverlay` if available.

---

## Configuration

New environment variables (ADR-003):

| Variable | Default | Description |
|---|---|---|
| `DUCK_GAIN` | `0.15` | Primary source gain while ducked (0.0–1.0) |
| `DUCK_RAMP_MS` | `200` | Gain ramp duration in milliseconds |

Both apply to `DuckingSource` and `Mixer`. A single consistent duck gain is used across both to avoid surprising interactions when they are composed.

### `/status` extension

```json
"mixer": {
  "overlay_active": true,
  "primary_gain": 0.15,
  "overlay_track": "sfx_applause.mp3"
}
```

---

## Consequences

**Positive:**
- All new types implement `media.AudioSource`; `pkg/bot` is completely unchanged
- Composition is additive: `DuckingSource(Mixer(Playlist(...)))` — each layer wraps the previous cleanly
- `OverlayController` and `DuckingController` in `pkg/media` keep dependency direction correct
- When features are disabled (no VAD, no mixer), the source chain has zero overhead — no gain multiplication occurs

**Negative:**
- Sample-level int32 promotion and clamp on every frame (480 samples × 30 times/sec) adds a small but measurable CPU overhead when mixing is active
- Hard clipping may produce artefacts if overlay audio is unexpectedly loud; overlay files should be normalised before use
- Composed gain (VAD × overlay duck) can reduce primary to near-inaudible levels; operator must tune `DUCK_GAIN` appropriately for their use case
- `TriggerOverlay` replaces any currently-playing overlay immediately; there is no queuing of multiple pending overlays in Phase 1

**Accepted trade-offs:**
- Hard clipping over soft knee compression; simpler implementation, artefacts unlikely with normalised source material
- No overlay queue in Phase 1; concurrent overlay triggers discard the previous overlay; implement queue as future enhancement if needed

---

## Revision Triggers

1. **Overlay queueing required:** add a `[]AudioSource` queue to `Mixer`; advance to the next overlay when the current one exhausts; does not require interface changes
2. **Crossfade between playlist tracks required:** implement as a two-`Playlist` `Mixer` with the second playlist started one crossfade duration before the first exhausts; does not require changes to this ADR's interfaces
3. **Soft knee compression required:** replace the linear gain ramp and hard clip in `DuckingSource` and `Mixer` with a compressor curve; update `DUCK_GAIN` semantics
4. **SDK migration (ADR-004b):** no direct impact; `DuckingSource` and `Mixer` operate in `pkg/renderer` and are independent of the injection mechanism

---

## Related ADRs

- **ADR-002 (Media Format Contracts):** `AudioSource` interface is unchanged; `DuckingSource` and `Mixer` implement it; sample format (int16, 480 samples/frame) governs the mixing arithmetic
- **ADR-005 (Observability):** `POST /media` action field and `mixer`/ducking fields in `/status` extend the API surface defined in ADR-005
- **ADR-011 (Audio Source Sequencing):** `Playlist` is the expected primary source for `Mixer`; crossfade between tracks is a future `Mixer` operation
- **ADR-012 (Meeting Audio Monitoring and VAD):** `DuckingController` interface consumed by the VAD goroutine is implemented by `DuckingSource` here; `pkg/bot` calls `SetVADActive` on a `DuckingController` it receives at construction — it does not know whether the implementation is `DuckingSource` or a no-op
