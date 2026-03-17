# ADR-011: Audio Source Sequencing (Playlist)

## Status
🟡 **Proposed**

---

## Context

The current `AudioSource` and `VideoSource` interfaces (ADR-002) model a single continuous source. `FileDecoder` and `StreamDecoder` (ADR-010) each represent one track. Playing a series of audio files or streams requires advancing from one source to the next when the current source is exhausted — a playlist.

This is not simply a matter of calling `FileDecoder.Close()` and opening a new one between sessions. The pipeline is continuous: `pkg/bot` consumes frames from the audio buffer at 30 Hz without interruption, and `pacat` (ADR-008) writes to PulseAudio's null sink at a steady 48 kHz rate. A track transition must be gapless from the buffer's perspective: the last frame of track N and the first frame of track N+1 must flow through the same ring buffer without a silence gap, a buffer flush observable to the downstream consumer, or a PTS discontinuity that triggers drift warnings.

Three questions must be answered:

1. **Where does sequencing logic live?** It must implement `AudioSource` (and optionally `VideoSource`) so it is transparent to the buffer and bot layers.
2. **How are track transitions handled at the PTS and buffer layer?** A new source starts at PTS 0; the buffer consumer is mid-stream at some wall-clock offset. Without care this produces a false drift alarm.
3. **How does the playlist integrate with the existing `/config` and `/media` API surface?**

---

## Decision

### Sequencing belongs in `pkg/renderer` as a `Playlist` type

`Playlist` implements `media.AudioSource` and `media.VideoSource`. It holds an ordered list of source factories (not pre-constructed sources), constructs each source on demand, reads from it until exhaustion, then advances. It is entirely transparent to `pkg/bot` — the bot consumes `Playlist` through the same `AudioSource` interface as any other source.

Source factories (not instances) are stored because some sources have non-trivial startup cost: `StreamDecoder` invokes `yt-dlp` and launches FFmpeg subprocesses. Constructing all sources eagerly at playlist creation time would block startup and waste resources for tracks that may never play.

```
Playlist
  tracks    []TrackSpec         // ordered list of track specifications
  current   media.AudioSource   // active source; nil if not started
  currentV  media.VideoSource   // active video source (may be SMPTESource if audio-only)
  idx       int                 // index into tracks
  ptsOffset time.Duration       // cumulative PTS across track boundaries
```

---

## Track Specification

A `TrackSpec` describes one track in the playlist without constructing it:

```go
type TrackSpec struct {
    // Exactly one of the following is set:
    FilePath  string  // local file in MEDIA_DIR
    StreamURL string  // platform page URL (resolved via yt-dlp, ADR-010)
    DirectURL string  // direct media URL (decoded by FFmpeg directly, ADR-010)
}
```

`Playlist.advance()` reads the next `TrackSpec`, constructs the appropriate source (`FileDecoder` or `StreamDecoder`), and sets it as the active source. Advance is called from within `ReadAudio` when the current source returns `io.EOF`.

---

## PTS Continuity Across Track Boundaries

ADR-002 requires that when a source switch occurs, the renderer resets the PTS origin and flushes both buffers. This rule exists to prevent cross-source PTS discontinuities from triggering drift alarms.

For a playlist, this rule is refined: **the buffer is not flushed at a track transition.** Flushing the buffer at every track boundary would drain the 2-second audio ring buffer to zero before the next track begins producing frames, creating an audible gap. Instead:

**PTS continuity** is maintained across track boundaries by accumulating a `ptsOffset`:

```
ptsOffset += lastFramePTS of completed track + one frame duration (10 ms)
```

Each frame produced by the new track has its `PTS` set to:
```
frame.PTS = trackLocalPTS + ptsOffset
```

From the bot's perspective, PTS increases monotonically across the entire playlist session. No drift alarm fires. No buffer flush occurs. The transition is seamless.

**Buffer flush is still required** in two cases:
- When the entire playlist is replaced via `PATCH /config` (not a track advance; a full source switch)
- When `Playlist.Stop()` is called explicitly

Both of these follow the existing ADR-002 source-switch protocol.

---

## Pre-loading

To avoid a silence gap during track advance, `Playlist` pre-loads the next track while the current track is still playing. Pre-loading begins when the current track's audio buffer depth falls below the low-water mark (50 frames, 500 ms — ADR-002), or when the current track signals it is within 1 second of EOF.

Pre-loading means constructing the next `TrackSpec`'s source and beginning to read frames from it into a staging buffer. When the current track hits EOF, the staging buffer is drained into the main ring buffer before the pre-loaded source becomes the active source. This ensures continuity even when `StreamDecoder` has a 1–3 second `yt-dlp` startup delay (ADR-010).

For `FileDecoder` sources (local files), pre-loading is near-instantaneous and the staging buffer is minimal. For `StreamDecoder` sources, pre-loading must begin early enough to absorb the resolution latency.

---

## Video Handling

A `Playlist` of audio-only tracks (MP3 files) must still produce `VideoFrame` values because the video buffer (ADR-002) and the `.y4m` FIFO (ADR-009) run continuously. For audio-only tracks, `Playlist` pairs a `SMPTESource` with the audio source, consistent with the existing design in `pkg/renderer` (US-R06).

For tracks with video (e.g., a YouTube video), `Playlist` sequences both audio and video sources together. Audio and video EOF are detected independently; if one stream exhausts before the other, the exhausted stream falls back to silence or SMPTE bars respectively until both are exhausted, at which point the track advance occurs.

---

## API Integration

### `PATCH /config` — replace playlist

The existing `PATCH /config` endpoint (ADR-005) is extended to accept a `playlist` field:

```json
{
  "playlist": {
    "tracks": [
      { "file_path": "track1.mp3" },
      { "file_path": "track2.mp3" },
      { "stream_url": "https://youtube.com/watch?v=..." }
    ],
    "loop": false
  }
}
```

`file_path` values are resolved relative to `MEDIA_DIR`. Setting a new playlist replaces the current one atomically: the current track finishes its current frame, the buffer is flushed (ADR-002 source-switch protocol), and the new playlist begins from track 0.

### `GET /status` — playlist state

The `/status` response (ADR-005) is extended with a `playlist` field:

```json
"playlist": {
  "track_index": 1,
  "track_total": 3,
  "current_track": "track2.mp3",
  "loop": false
}
```

### `POST /media` — upload and optionally enqueue

The existing `POST /media` endpoint (ADR-005) is extended with an optional `enqueue` parameter. When `enqueue=true`, the uploaded file is added to the end of the current playlist rather than replacing the current source. This enables live track addition without interrupting the current track.

---

## Loop Mode

`Playlist` supports a `loop` flag. When `loop: true`, after the last track exhausts, `idx` resets to 0 and the playlist begins again from the first track. PTS continuity is maintained across the loop boundary using the same `ptsOffset` mechanism.

---

## Consequences

**Positive:**
- `Playlist` is transparent to `pkg/bot` — no changes to the buffer, injection, or bot package
- PTS continuity across track boundaries prevents spurious drift alarms
- Pre-loading absorbs `StreamDecoder` startup latency; transitions are gapless for local file sources and near-gapless for stream sources
- Loop mode requires no additional mechanism beyond `idx` reset

**Negative:**
- Pre-loading introduces a staging buffer alongside the main ring buffer; peak memory usage increases by up to one additional ring buffer worth of frames during transitions
- PTS accumulation across long playlists (many hours) will eventually overflow `time.Duration` (max ~292 years); this is not a practical concern but worth noting
- `PATCH /config` playlist replacement flushes the buffer, causing a brief silence; this is consistent with the existing source-switch behaviour

**Accepted trade-offs:**
- No crossfade between tracks in Phase 1; abrupt cut at track boundaries is accepted; crossfade can be added as a `Mixer` operation (ADR-013) later
- Pre-load trigger based on low-water mark rather than a fixed time-before-EOF; this is simpler to implement and sufficient for the expected latency range

---

## Revision Triggers

1. **Crossfade required:** implement as a `Mixer` operation (ADR-013) layered over two `Playlist` instances with offset timing; does not require changes to this ADR
2. **Gapless streaming proves insufficient for `StreamDecoder` sources:** increase pre-load lead time or add a larger staging buffer; update pre-loading section
3. **Random/shuffle mode required:** add `shuffle` flag to `TrackSpec` list; generate a shuffled play order at playlist start; does not require interface changes

---

## Related ADRs

- **ADR-002 (Media Format Contracts):** PTS continuity rules and buffer flush protocol; the `ptsOffset` mechanism is an extension of the source-switch PTS reset rule
- **ADR-005 (Observability):** `/config`, `/status`, and `/media` endpoint extensions defined here; must be reflected in ADR-005 schema documentation
- **ADR-010 (External Stream Sources):** `StreamDecoder` is a valid `TrackSpec` source; pre-loading must account for its resolution latency
- **ADR-013 (Audio Mixing):** crossfade between tracks can be implemented as a `Mixer` operation over two `Playlist` instances; this ADR does not implement crossfade
