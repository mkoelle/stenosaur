#!/usr/bin/env bash
# entrypoint.sh — container startup sequence
# 1. Start PulseAudio with a null sink (no host audio device required)
# 2. Wait for PulseAudio readiness before proceeding
# 3. Exec the stenosaur binary (replaces this shell process)
#
# See ADR-003 and US-D02 for rationale.

set -euo pipefail

PULSE_TIMEOUT=${PULSE_TIMEOUT:-15}

# Start PulseAudio in daemon mode with a null sink as the default device.
# module-null-sink creates a virtual audio device inside the container.
# Chromium will discover it via --use-fake-device-for-media-stream (ADR-004).
pulseaudio --start \
  --load="module-null-sink sink_name=stenosaur_sink sink_properties=device.description=StenosaurSink" \
  --load="module-native-protocol-unix" \
  --log-target=stderr \
  --log-level=warn \
  --disallow-exit \
  --daemon

# Wait for PulseAudio to become ready.
# pactl info returns non-zero until the daemon is accepting connections.
elapsed=0
until pactl info > /dev/null 2>&1; do
  if [ "$elapsed" -ge "$PULSE_TIMEOUT" ]; then
    echo '{"ts":"'"$(date -u +%FT%T.000Z)"'","component":"entrypoint","level":"error","msg":"PulseAudio did not become ready","ctx":{"timeout_seconds":'"$PULSE_TIMEOUT"'}}' >&2
    exit 1
  fi
  sleep 1
  elapsed=$((elapsed + 1))
done

echo '{"ts":"'"$(date -u +%FT%T.000Z)"'","component":"entrypoint","level":"info","msg":"PulseAudio ready","ctx":{"elapsed_seconds":'"$elapsed"'}}' >&2

# Exec the binary — this replaces the shell process so Docker signals
# (SIGTERM on `docker compose down`) reach stenosaur directly.
exec /app/stenosaur "$@"
