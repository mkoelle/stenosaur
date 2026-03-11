package main

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
)

// config holds validated runtime configuration loaded from environment
// variables. All fields have been validated before this struct is returned.
// (ADR-003, US-D08)
type config struct {
	MeetingURL        string
	BotDisplayName    string
	AdminToken        string
	AdminPort         string
	MediaDir          string
	AudioBufferFrames int
	VideoBufferFrames int
}

// loadConfig reads and validates all environment variables defined in ADR-003.
// Returns a non-nil error with a specific, actionable message on any failure.
// The process must not start partially — if this returns an error, main exits.
func loadConfig() (*config, error) {
	var errs []string

	// ── Required variables ────────────────────────────────────────────────────

	meetingURL := os.Getenv("MEETING_URL")
	if meetingURL == "" {
		errs = append(errs, "MEETING_URL is required (full Zoom meeting URL)")
	} else if err := validateMeetingURL(meetingURL); err != nil {
		errs = append(errs, fmt.Sprintf("MEETING_URL is invalid: %v", err))
	}

	displayName := os.Getenv("BOT_DISPLAY_NAME")
	if displayName == "" {
		errs = append(errs, "BOT_DISPLAY_NAME is required (name shown in Zoom participant list)")
	}

	adminToken := os.Getenv("ADMIN_TOKEN")
	if adminToken == "" {
		errs = append(errs, "ADMIN_TOKEN is required (Bearer token for admin API)")
	} else if len(adminToken) < 16 {
		errs = append(errs, "ADMIN_TOKEN must be at least 16 characters")
	}

	// ── Optional variables with defaults ─────────────────────────────────────

	adminPort := envOrDefault("ADMIN_PORT", "8080")
	mediaDir := envOrDefault("MEDIA_DIR", "/app/media")

	audioBufferFrames, err := envIntOrDefault("AUDIO_BUFFER_FRAMES", 200)
	if err != nil {
		errs = append(errs, "AUDIO_BUFFER_FRAMES must be a positive integer")
	}

	videoBufferFrames, err := envIntOrDefault("VIDEO_BUFFER_FRAMES", 10)
	if err != nil {
		errs = append(errs, "VIDEO_BUFFER_FRAMES must be a positive integer")
	}

	// ── File system checks ────────────────────────────────────────────────────

	if mediaDir != "" {
		if _, statErr := os.Stat(mediaDir); statErr != nil {
			errs = append(errs, fmt.Sprintf("MEDIA_DIR %q is not readable: %v", mediaDir, statErr))
		}
	}

	if len(errs) > 0 {
		return nil, errors.New("configuration errors:\n  - " + strings.Join(errs, "\n  - "))
	}

	return &config{
		MeetingURL:        meetingURL,
		BotDisplayName:    displayName,
		AdminToken:        adminToken,
		AdminPort:         adminPort,
		MediaDir:          mediaDir,
		AudioBufferFrames: audioBufferFrames,
		VideoBufferFrames: videoBufferFrames,
	}, nil
}

// validateMeetingURL performs a format-only check that the value looks like a
// Zoom meeting URL. It does not make any network calls. (ADR-003)
func validateMeetingURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("not a valid URL: %w", err)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("must be https://")
	}
	if !strings.HasSuffix(u.Host, "zoom.us") {
		return fmt.Errorf("host must be a zoom.us domain, got %q", u.Host)
	}
	return nil
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envIntOrDefault(key string, def int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("invalid value for %s: %q", key, v)
	}
	return n, nil
}
