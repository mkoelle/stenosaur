# Stenosaur: User Features

**Version:** 1.0 (8-phase roadmap)  
**Last Updated:** March 2026

---

## What is Stenosaur?

Stenosaur is a self-hosted Zoom meeting bot that joins meetings as a named participant and injects custom audio and video content. It's designed for creators, educators, and developers who need to:

- Stream pre-recorded media or live web content into Zoom meetings
- Play background music or sound effects during presentations
- Display dynamic web content (dashboards, visualizations, slides) as video
- Automate media playback in recurring meetings
- Create custom meeting experiences without manual control

The entire system runs in a single Docker container with no special hardware requirements. Control it via a web dashboard from any device on your local network.

---

## Core Features

### 🎯 Meeting Integration

**Automated Join**
- Joins any Zoom meeting automatically using the meeting URL
- Appears as a named participant (configurable display name)
- Handles waiting rooms and admission prompts
- Supports password-protected meetings
- Gracefully handles host ending meeting or removing bot

**Session Control**
- Start/stop sessions via web UI or REST API
- Real-time session state monitoring (joining, connected, streaming, error)
- Automatic reconnection on network drops (via container restart)
- Clean teardown on shutdown (no orphaned processes)

---

### 🎵 Audio Sources

**Supported Audio Types:**

**1. Local Files**
- Play MP3, WAV, M4A, FLAC, and other common audio formats
- Automatic resampling to Zoom's native 48 kHz format
- Gapless playback between playlist tracks
- Loop mode for continuous background audio

**2. Live Streams**
- YouTube videos (audio extracted automatically)
- Direct audio URLs (podcasts, radio streams, MP3 links)
- Platform-independent streaming via `yt-dlp` integration
- Automatic retry on stream connection failures

**3. Synthesized Audio**
- Built-in tone generator (440 Hz sine wave for testing)
- Programmable audio synthesis for alerts and notifications
- Zero-latency generation (no file I/O)

**Audio Quality:**
- 48 kHz mono (Zoom's native format)
- 16-bit PCM encoding
- 10ms frame delivery (low latency)
- Automatic drift correction keeps audio in sync

---

### 🎥 Video Sources

**Supported Video Types:**

**1. Web Content Rendering**
- Render any webpage as 1280×720 30fps video
- Perfect for dashboards, data visualizations, slides
- Live updates captured in real-time
- JavaScript animations and transitions supported

**2. Local Video Files**
- Play MP4, MKV, AVI, MOV, and other common formats
- Automatic scaling to 1280×720 resolution
- Frame rate conversion to 30 fps
- Synchronized A/V playback from single file

**3. Live Video Streams**
- YouTube videos
- Direct video URLs (M3U8, RTMP, MP4 streams)
- Platform streams resolved automatically

**4. Static Frames**
- SMPTE color bars (default when only audio is playing)
- Custom static images as placeholders
- Smooth frame-repeat when source can't keep up

**Video Quality:**
- 1280×720 resolution (720p)
- 30 frames per second
- YUV 4:2:0 color format (Zoom's native format)
- BT.601 limited range color space

---

### 📋 Playlist Management

**Multi-Track Sequences:**
- Queue multiple audio/video files or stream URLs
- Gapless transitions between tracks
- Mixed content (files + streams in same playlist)
- Loop mode plays playlist indefinitely

**Playlist Control:**
- Update playlist without stopping session
- Track position and progress monitoring
- Pre-loading prevents gaps between tracks
- Seamless PTS (presentation timestamp) continuity

**Configuration:**
```json
{
  "playlist": {
    "tracks": [
      { "file_path": "intro.mp3" },
      { "stream_url": "https://youtube.com/watch?v=..." },
      { "direct_url": "https://example.com/audio.mp3" },
      { "file_path": "outro.mp4" }
    ],
    "loop": true
  }
}
```

---

### 🔊 Smart Audio Ducking (Voice-Activated)

**Automatic Volume Control:**
- Detects when meeting participants speak
- Automatically lowers injected audio volume ("ducking")
- Returns to full volume after participant stops speaking
- Prevents feedback loop (bot doesn't duck its own audio)

**Configuration:**
- Enable/disable via `VAD_ENABLED` setting
- Adjust sensitivity threshold (default: RMS 500)
- Customize duck gain (default: 15% of normal volume)
- Control release timing (default: 800ms delay before unduck)

**How It Works:**
- Voice Activity Detection (VAD) monitors meeting audio in real-time
- RMS (root mean square) analysis detects speech vs. silence
- Smooth gain ramping prevents abrupt volume changes
- Configurable release delay avoids "pumping" during speech pauses

---

### 🎚️ Audio Mixing & Overlays

**Sound Effect Triggers:**
- Trigger overlay sounds on top of primary audio
- Perfect for applause, alerts, notification sounds
- Upload files via web UI or API
- Instant playback without interrupting primary stream

**Mixing Behavior:**
- Primary audio continues playing underneath
- Overlay mixed at configurable volume
- Hard clipping protection prevents distortion
- Primary auto-ducks during overlay (optional)

**Use Cases:**
- Add applause/cheering during presentations
- Play notification sounds for chat messages
- Trigger sound effects for audience interaction
- Layer background music under narration

---

### 🌐 Web Admin Dashboard

**Real-Time Monitoring:**
- Live session state display (disconnected, joining, connected, streaming)
- Pipeline health metrics:
  - Audio buffer depth (frames in queue)
  - Video buffer depth (frames in queue)
  - Underrun counts (missed frames)
  - Drift detection (timing accuracy)
- Meeting duration counter
- Current source/track information

**Session Control:**
- Start/Stop buttons
- Emergency disconnect
- Source switching during active session
- Media file upload interface

**Live Log Viewer:**
- Real-time structured log stream via WebSocket
- Filterable by component (bot, renderer, controller)
- Filterable by level (debug, info, warn, error)
- Automatic password redaction
- Color-coded by severity

**Access:**
- Available at `http://<host-ip>:8080` from any device on LAN
- Bearer token authentication
- No external dependencies (no CDN, fully embedded)
- Responsive design (works on mobile)

---

### 🔌 REST API

**All features accessible programmatically:**

**Health Checks (no auth required):**
- `GET /healthz` - Process liveness
- `GET /readyz` - Bot functional status

**Session Control (auth required):**
- `POST /start` - Join meeting and begin streaming
  - Optional body: `{"meeting_url": "...", "display_name": "..."}`
- `POST /stop` - Leave meeting and stop pipeline
- `GET /status` - Full session state + metrics

**Configuration (auth required):**
- `PATCH /config` - Update source/playlist without restart
  - Body: `{"playlist": {...}}` or `{"source": {...}}`
- `POST /media` - Upload file for use as source
  - With `action: "overlay"` triggers instant sound effect

**Monitoring (auth required):**
- `WebSocket /logs` - Live structured log stream

**Authentication:**
- Bearer token via `Authorization` header
- Configurable via `ADMIN_TOKEN` environment variable
- Constant-time comparison prevents timing attacks

---

### ⚙️ Configuration

**Environment Variables:**

**Required:**
- `BOT_DISPLAY_NAME` - Name shown in Zoom participant list
- `ADMIN_TOKEN` - Authentication token for web UI/API (min 16 chars)

**Optional - Meeting:**
- `MEETING_URL` - Zoom meeting URL (can also provide via API)
- `MEETING_PASSWORD` - Meeting password (or embed in URL as `?pwd=...`)

**Optional - Media Pipeline:**
- `MEDIA_DIR` - Path for media files (default: `/app/media`)
- `AUDIO_BUFFER_FRAMES` - Audio buffer size (default: 200 frames = 2 seconds)
- `VIDEO_BUFFER_FRAMES` - Video buffer size (default: 10 frames = 333ms)

**Optional - Voice Detection & Ducking:**
- `VAD_ENABLED` - Enable voice-activated ducking (default: false)
- `VAD_DUCK_THRESHOLD` - RMS threshold for speech detection (default: 500)
- `VAD_RELEASE_MS` - Silence duration before unduck (default: 800ms)
- `DUCK_GAIN` - Volume reduction during duck (default: 0.15 = 15%)
- `DUCK_RAMP_MS` - Gain change ramp duration (default: 200ms)

**Optional - Server:**
- `ADMIN_PORT` - Web UI port (default: 8080)
- `LOG_LEVEL` - Logging verbosity: debug/info/warn/error (default: info)

**Configuration File:**
- All settings stored in `.env` file
- `.env.example` template provided with documentation
- Never committed to version control (secrets protected)

---

### 📦 Deployment

**Container-Based:**
- Single Docker container, no orchestration needed
- Runs on Linux, macOS (Docker Desktop), Windows (Docker Desktop)
- No privileged mode required
- No host audio/video devices needed
- No kernel modules required

**Resource Requirements:**
- ~1-2 GB RAM (two Chromium instances)
- ~1 GB disk (container image)
- CPU: modest (single-session deployment)
- Network: must reach `app.zoom.us`

**Quick Start:**
```bash
# 1. Create config from template
cp .env.example .env

# 2. Edit .env with your settings
nano .env

# 3. Start container
docker compose up

# 4. Access web UI
open http://localhost:8080
```

**Updates:**
```bash
# Pull latest image and restart
docker compose pull
docker compose up -d
```

---

### 🔍 Observability

**Structured Logging:**
- JSON format for all log entries
- Consistent schema across all components
- Automatic timestamp and component tagging
- Secret redaction (passwords never logged)
- Configurable verbosity

**Health Monitoring:**
- Docker HEALTHCHECK integration
- Automatic restart on failure (configurable policy)
- Separate liveness vs. readiness checks
- Pipeline metrics exposed via API

**Metrics Tracked:**
- Session state and duration
- Audio/video buffer depths
- Frame underrun/overrun counts
- PTS drift (timing accuracy)
- Source type and current track
- VAD state (if enabled)
- Mixer state (if overlay active)

---

### 🔒 Security & Privacy

**Self-Hosted:**
- Runs entirely on your infrastructure
- No data sent to third-party services (except Zoom)
- Full control over meeting content
- No cloud dependencies

**Authentication:**
- Web UI and API require bearer token
- Tokens compared with constant-time algorithm
- No session cookies (stateless auth)
- LAN-only exposure by default

**Secret Management:**
- Passwords never appear in logs
- URL passwords stripped before logging
- CI validates no secret leakage
- Secrets stored in `.env` (gitignored)

**Limitations:**
- No HTTPS in base configuration (LAN assumed safe)
- Add reverse proxy for TLS if needed
- Single token (no user management)
- Token rotation requires container restart

---

## Use Cases

### 📚 Education & Training
- Stream lecture recordings during office hours
- Display live dashboard during group sessions
- Play background music during study groups
- Auto-duck music when instructor speaks

### 🎙️ Content Creation & Podcasting
- Stream pre-recorded content to Zoom webinar
- Display sponsor slides or promotions
- Trigger sound effects for audience engagement
- Mix music under spoken content

### 💼 Business & Events
- Play hold music during waiting rooms
- Display rotating slides/announcements
- Trigger applause for presentations
- Stream company radio during town halls

### 🎮 Streaming & Entertainment
- Share gameplay with video+audio
- Stream YouTube content to Zoom audience
- Create watch parties with synchronized playback
- Add sound board functionality

### 🤖 Automation & Integration
- Schedule automated content delivery
- API-driven media playback
- Integration with home automation
- Custom bot behaviors via API

---

## Roadmap Phases

The feature set described above is delivered across 8 implementation phases:

**Phase 1: Vertical Slice** ✓ Completed
- Basic meeting join
- Synthetic audio/video injection
- Core pipeline proven

**Phase 2: File Playback** ✓ Completed
- Local file support (audio/video)
- FFmpeg integration

**Phase 3: API and Admin UI** ✓ Completed
- Full REST API
- Web dashboard
- Live monitoring

**Phase 4: External Streams** ✓ Completed
- YouTube integration
- Direct URL streaming
- yt-dlp integration

**Phase 5: Playlist** ✓ Completed
- Multi-track sequencing
- Gapless playback
- Loop mode

**Phase 6: VAD Ducking** ✓ Completed
- Voice detection
- Auto-ducking
- Configurable sensitivity

**Phase 7: Mixing and Overlays** ✓ Completed
- Sound effect triggers
- Audio mixing
- Overlay API

**Phase 8: Hardening** → In Progress
- Production readiness
- Secret scanning
- Cross-platform testing
- Documentation polish

---

## Known Limitations

**Zoom Integration:**
- Uses Zoom Web App (not official SDK)
- Selectors may break on Zoom UI updates
- Automated participation not explicitly sanctioned by Zoom ToS
- For self-hosted/personal use only

**Media Constraints:**
- Video locked at 1280×720 @ 30fps
- Audio locked at 48kHz mono
- Formats limited to FFmpeg-supported codecs
- No hardware video encoding (CPU only)

**Session Management:**
- Single concurrent session per container
- No multi-meeting support
- Manual meeting URL configuration (or API)
- Container restart required for some config changes

**Network Requirements:**
- Must reach `app.zoom.us` (no offline mode)
- LAN access required for web UI
- No mobile app (web UI only)

**Platform Support:**
- Docker required (no native installation)
- Testing on free Zoom accounts limited (40-min meetings)
- No Windows/macOS native builds

---

## Future Enhancements

**Potential Extensions** (not in v1.0 scope):

- **Multi-Session Support:** Run multiple bots in parallel
- **Transcription:** Real-time speech-to-text
- **Recording:** Capture meeting audio/video
- **Discord/Teams Adapters:** Support other platforms
- **SDK Migration:** Switch to official Zoom Meeting SDK
- **Prometheus Metrics:** Advanced monitoring/alerting
- **Hardware Encoding:** GPU acceleration for video
- **Plugin System:** Custom source types
- **Scheduler:** Time-based automation
- **Mobile App:** Native iOS/Android control

---

## Support & Documentation

**Getting Started:**
- See `README.md` for installation guide
- See `.env.example` for configuration reference
- See `VERTICAL-SLICE.md` for architecture walkthrough

**Troubleshooting:**
- Check logs via web UI `/logs` endpoint
- Verify `docker compose logs` for startup issues
- Review ADRs for design decisions and constraints

**Contributing:**
- See `ARCHITECTURE.md` for system design
- See `PROJECT-PLAN.md` for development roadmap
- See `USER-STORIES.md` for implementation details
- See ADRs (Architecture Decision Records) for rationale

---

**License:** [Your License Here]  
**Repository:** [Your Repo URL]  
**Issues:** [Your Issue Tracker]