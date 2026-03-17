# ADR-010: External Stream Sources

## Status
🟡 **Proposed**

---

## Context

ADR-002 defines `FileDecoder` as a source type that decodes a local file path via FFmpeg and normalises output to the pipeline format contracts. The use case of streaming a YouTube video — audio and video together — requires decoding media from a remote URL rather than a local file path.

This is not simply a matter of passing a URL to `FileDecoder`. Two problems must be solved:

1. **Stream URL resolution.** YouTube and similar platforms do not expose a direct media URL. The playable stream URL must be resolved at runtime from a page URL using a tool like `yt-dlp`. The resolved URL is time-limited (typically 6 hours); it cannot be resolved at config time and cached.

2. **FFmpeg URL decoding.** Once a direct stream URL is available, FFmpeg can decode from it over HTTP/HTTPS directly without downloading the file first. This is a different invocation path from local file decoding but produces the same normalised output.

These two concerns — URL resolution and stream decoding — are separable. URL resolution is a pre-processing step; stream decoding is an extension of the existing `FileDecoder` path. This ADR decides how both are handled and what new container dependencies they introduce.

---

## Constraint: No New Interface Required

`FileDecoder` already satisfies `media.AudioSource` and `media.VideoSource`. A stream source that resolves a URL and decodes it via FFmpeg produces the same frame types through the same interface. No changes to `pkg/media` interfaces are required.

The distinction between a local file path and a remote stream URL is an implementation detail of `FileDecoder` and its URL-resolving wrapper — invisible to `pkg/bot` and `pkg/controller`.

---

## Options Evaluated

### Option A: Extend `FileDecoder` to accept URLs directly

Pass a URL string to `FileDecoder`. If the path begins with `http://` or `https://`, invoke FFmpeg with the URL as input rather than a file path. FFmpeg handles HTTP streaming natively.

This handles direct media URLs (e.g., a plain MP3 at a known URL, an HLS stream endpoint) but does not handle platform URLs (YouTube, SoundCloud, Vimeo) where the playable stream URL must be resolved first.

**Verdict:** Covers direct URLs only. Insufficient for YouTube. Not rejected — this path is a subset of the selected option.

---

### Option B: `yt-dlp` resolver + FFmpeg decoder ✅

`yt-dlp` is a maintained fork of `youtube-dl` that resolves playable stream URLs from platform page URLs. It outputs the direct media URL(s) to stdout; FFmpeg then decodes from that URL.

The resolution and decoding steps are composed as follows:

1. `yt-dlp --get-url --format "bestvideo[height<=720]+bestaudio/best[height<=720]" <page_url>` outputs one or two direct stream URLs (video URL and audio URL may be separate for YouTube's DASH format).
2. FFmpeg is invoked with those URLs as inputs, performs demux, decode, colour space conversion, and resampling, and writes normalised output to a pipe.
3. `StreamDecoder` (a new type wrapping this pipeline) presents the result as `media.AudioSource` and `media.VideoSource`.

`yt-dlp` is invoked once at `StreamDecoder.Start()` time to resolve the URL. The resolved URL is used for the lifetime of the stream. If the session runs longer than the URL's TTL, the stream will fail with a 403 — the `StreamDecoder` logs this as an `error` and returns `io.EOF` to the caller (which triggers the playlist advance or session stop depending on context).

**Verdict: Selected.**

---

### Option C: Headless browser-based capture

Navigate to the YouTube URL in a Chromium browser context (using the existing `WebRenderer`) and capture the video frames via screenshot and the audio via Web Audio API.

Screenshot-based capture at 30 FPS is CPU-intensive and produces frames that include the YouTube player UI (controls, progress bar, ads). Web Audio API capture from the page depends on YouTube's internal audio routing not blocking cross-context capture.

This approach is significantly more fragile than the FFmpeg path and produces lower-quality output. It is the approach that would be taken if `yt-dlp` were unavailable or if the use case required capturing a live interactive page rather than a media stream.

**Verdict:** Rejected. `WebRenderer` is appropriate for rendering a display webpage (dashboards, presentations); it is not appropriate for extracting a media stream from a video platform.

---

## Decision

**`yt-dlp` for URL resolution + FFmpeg for stream decoding, presented as `StreamDecoder` implementing `media.AudioSource` and `media.VideoSource`.**

`StreamDecoder` is a new type in `pkg/renderer` that wraps the `yt-dlp` → FFmpeg pipeline. It satisfies the same interfaces as `FileDecoder` and is transparent to all callers. `FileDecoder` is extended to accept direct media URLs (Option A) as a simpler path for sources that do not require platform URL resolution.

---

## New Type: `StreamDecoder`

`StreamDecoder` lives in `pkg/renderer/stream.go`. It accepts a page URL, resolves it via `yt-dlp`, and decodes the resulting stream via FFmpeg.

```
StreamDecoder
  pageURL    string            // the platform page URL (e.g. youtube.com/watch?v=...)
  ytdlpPath  string            // path to yt-dlp binary in container
  ffmpegPath string            // path to ffmpeg binary in container
  audioOut   chan media.AudioFrame
  videoOut   chan media.VideoFrame
```

**Lifecycle:**
- `Start(ctx)`: invoke `yt-dlp` subprocess to resolve stream URL(s); invoke `ffmpeg` subprocess with resolved URLs; launch goroutines to read FFmpeg output and push frames into channels.
- `ReadAudio(ctx)`: receive from `audioOut` channel; block until a frame is available or ctx is done.
- `ReadVideo(ctx)`: receive from `videoOut` channel.
- `Close()`: cancel context; wait for subprocesses to exit; drain channels.

---

## `FileDecoder` Extension

`FileDecoder` is extended to detect whether its `path` field is a URL (begins with `http://` or `https://`). If so, FFmpeg is invoked with the URL as direct input without `yt-dlp` resolution. This covers HLS streams, direct MP3/MP4 URLs, and RTMP sources that FFmpeg can decode natively.

The distinction is transparent to callers: `NewFileDecoder(path, renderer)` accepts either a file path or a direct media URL.

---

## Container Dependencies

Two new binaries must be present in the container image:

| Binary | Package | Purpose |
|---|---|---|
| `ffmpeg` | `ffmpeg` (apt) | Stream decode, resample, colour space conversion for all source types |
| `yt-dlp` | `yt-dlp` (pip or binary release) | Platform URL resolution for YouTube, SoundCloud, Vimeo, etc. |

`ffmpeg` is already implicitly required by the existing `FileDecoder` stub (US-R02). This ADR makes the dependency explicit and adds it to the `Dockerfile`.

`yt-dlp` is installed from its GitHub release binary rather than via pip to avoid adding Python to the container image. The pinned version must be documented in the `Dockerfile` and updated when platform changes break extraction.

**Dockerfile additions:**
```dockerfile
# FFmpeg for media decoding (ADR-010)
RUN apt-get install -y --no-install-recommends ffmpeg

# yt-dlp for platform URL resolution (ADR-010)
# Pin version; update when platform extraction breaks
ARG YTDLP_VERSION=2024.11.18
RUN curl -L "https://github.com/yt-dlp/yt-dlp/releases/download/${YTDLP_VERSION}/yt-dlp_linux" \
    -o /usr/local/bin/yt-dlp && chmod +x /usr/local/bin/yt-dlp
```

---

## URL Logging and Privacy

YouTube URLs may contain video IDs and playlist parameters that are not sensitive, but `yt-dlp`-resolved stream URLs contain time-limited authentication tokens in query parameters. These resolved URLs must never be logged. The logging rule is:

- Log the **page URL** (e.g., `https://youtube.com/watch?v=dQw4w9WgXcQ`) at `info` level when a `StreamDecoder` is started.
- Never log the resolved stream URL. It contains auth tokens.
- If `yt-dlp` fails, log the exit code and stderr (which does not contain stream URLs) at `error` level.

This follows the existing convention in ADR-005 of stripping sensitive values from URLs before logging.

---

## Consequences

**Positive:**
- YouTube, SoundCloud, Vimeo, and hundreds of other platforms become valid sources without changing any interfaces
- `StreamDecoder` is invisible to `pkg/bot` and `pkg/controller` — it is just another `AudioSource`/`VideoSource`
- Direct media URL support in `FileDecoder` covers HLS, RTMP, and plain HTTP streams without `yt-dlp`

**Negative:**
- `yt-dlp` binary adds ~15 MB to the container image and must be updated when YouTube changes its extraction logic (typically several times per year)
- Resolved stream URLs are time-limited; sessions longer than ~6 hours will fail with a 403 and require a `StreamDecoder` restart
- `yt-dlp` invocation adds ~1–3 seconds of startup latency before the first frame is produced
- `yt-dlp` is a third-party tool; its continued maintenance and YouTube compatibility is not guaranteed

**Accepted trade-offs:**
- `yt-dlp` subprocess over an embedded Go YouTube extraction library; `yt-dlp` is significantly more maintained and covers far more platforms
- Stream URL TTL limitation accepted for Phase 1; automatic URL refresh on 403 is a future enhancement

---

## Revision Triggers

1. **`yt-dlp` extraction breaks for a required platform:** pin a working version in the Dockerfile until a fix is released; if breakage is sustained, evaluate platform-specific Go extraction libraries
2. **Stream URL TTL causes session failures:** implement automatic `yt-dlp` re-resolution on 403 error; update `StreamDecoder` lifecycle
3. **SDK migration (ADR-004b):** no direct impact on this ADR; `StreamDecoder` is independent of the Zoom integration path

---

## Related ADRs

- **ADR-002 (Media Format Contracts):** `StreamDecoder` must produce frames conforming to the 48 kHz PCM and YUV420p contracts; FFmpeg's output flags are set to enforce this
- **ADR-003 (Docker Deployment):** `ffmpeg` and `yt-dlp` binaries must be added to the `Dockerfile`; their versions must be pinned and documented
- **ADR-011 (Audio Source Sequencing):** `StreamDecoder` is a valid track source for the playlist; URL resolution latency must be accounted for in playlist advance timing
