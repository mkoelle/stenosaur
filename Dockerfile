# syntax=docker/dockerfile:1

# ── Build stage ──────────────────────────────────────────────────────────────
FROM golang:1.21-bookworm AS builder

WORKDIR /src

# Cache module downloads
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/stenosaur ./cmd/stenosaur

# ── Runtime stage ─────────────────────────────────────────────────────────────
# Pin Chromium via the Playwright-maintained image.
# Update this tag in lockstep with the playwright-go version in go.mod.
# Current pinned version: v1.47.0 (playwright-go v0.4702.0)
# Document version changes in ARCHITECTURE.md and this comment.
FROM mcr.microsoft.com/playwright:v1.47.0-jammy

# Install PulseAudio (userspace only; no host audio device required)
RUN apt-get update && apt-get install -y --no-install-recommends \
    pulseaudio \
    pulseaudio-utils \
    curl \
 && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Copy binary from builder
COPY --from=builder /out/stenosaur .

# Copy entrypoint
COPY scripts/entrypoint.sh .
RUN chmod +x entrypoint.sh

# Media volume mount point (populated via docker-compose volume)
RUN mkdir -p /app/media

# Expose admin HTTP server port
EXPOSE 8080

# Health check — /healthz is unauthenticated (ADR-005)
# start-period allows PulseAudio and Chromium to initialize before first check
HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
  CMD curl -f http://localhost:8080/healthz || exit 1

ENTRYPOINT ["./entrypoint.sh"]
