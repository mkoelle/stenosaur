# Stenosaur

A containerized Zoom meeting bot that joins meetings unattended, injects audio and video from configurable sources, and exposes a web admin UI for monitoring and control.

## Quickstart

### Prerequisites

- Docker and Docker Compose
- Network access to `app.zoom.us` from the container

No host audio devices, kernel modules, or virtual cameras required.

### Setup

```bash
# 1. Clone the repository
git clone https://github.com/mkoelle/stenosaur.git
cd stenosaur

# 2. Copy and populate the config file
cp .env.example .env
# Edit .env — set MEETING_URL, BOT_DISPLAY_NAME, and ADMIN_TOKEN (min 16 chars)

# 3. Build and start
docker compose up
```

### Usage

```bash
# Check liveness
curl http://localhost:8080/healthz

# Check bot readiness (Zoom connected, pipelines running)
curl http://localhost:8080/readyz

# Start streaming
curl -X POST -H "Authorization: Bearer <ADMIN_TOKEN>" http://localhost:8080/start

# View status and pipeline metrics
curl -H "Authorization: Bearer <ADMIN_TOKEN>" http://localhost:8080/status

# Stop streaming
curl -X POST -H "Authorization: Bearer <ADMIN_TOKEN>" http://localhost:8080/stop

# Open the web admin UI
open http://localhost:8080
```

## Documentation

- [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) — full system design
- [`docs/adr/`](docs/adr/) — architecture decision records (ADR-001 through ADR-007)
- [`docs/USER-STORIES.md`](docs/USER-STORIES.md) — implementation user stories

## Development

```bash
# Install code generation tools (one-time)
go install github.com/a-h/templ/cmd/templ@latest
# Install tailwindcss CLI: https://tailwindcss.com/docs/installation

# Generate, build, test
make generate
make build
make test
```

See [`AGENTS.md`](AGENTS.md) for full conventions, ADR index, and agentic coding tool instructions.
