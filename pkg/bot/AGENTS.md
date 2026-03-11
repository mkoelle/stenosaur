# pkg/bot — Agent Instructions

@../../AGENTS.md

## Package Role

`pkg/bot` owns the Zoom session lifecycle. It implements the `ZoomClient` interface backed by Playwright PWA automation, consumes media frames from `pkg/media` buffers, and injects them into the Zoom session via Chromium's fake media device layer.

Allowed imports: standard library, `pkg/media`, `playwright-community/playwright-go`, third-party.
Must not import: `pkg/renderer`, `pkg/controller`.

## The ZoomClient Interface is Sacred (ADR-004)

`ZoomClient` is the swap point for a future Zoom Meeting SDK implementation. Every caller uses the interface — never the concrete `PWAClient` type directly. Do not add methods to `ZoomClient` without considering whether they are SDK-portable.

## Playwright Selector Rules (ADR-004)

Every Playwright UI selector must follow these rules or the change will be rejected:

1. **Version comment required.** Every selector must have an inline comment:
   ```go
   // Verified against Zoom Web App v6.3.1 (2026-03-10)
   page.Locator(`[data-testid="join-btn"]`)
   ```
2. **Timeout warns, not errors.** A selector timeout must emit a `warn` log — this is the ADR-004 breakage signal. Log as a GitHub issue tagged `adr-004-trigger` in the incident runbook.
3. **Three incidents in 6 months triggers ADR-004b.** Track in GitHub Issues.

## Goroutine Panic Recovery (ADR-001)

Every goroutine launched in this package must have a `defer` panic recovery at its top:

```go
defer func() {
    if r := recover(); r != nil {
        log.Error("bot goroutine panic",
            slog.String("component", "bot"),
            slog.Any("panic", r),
        )
    }
}()
```

Never launch a goroutine without this wrapper.

## Media Injection (ADR-002, ADR-004)

Chromium must be launched with these flags — do not remove any of them:

```
--use-fake-device-for-media-stream
--use-file-for-fake-audio-capture=<path>
--use-file-for-fake-video-capture=<path>
--allow-file-access-from-files
```

Verify on launch that Chromium is reading from the configured fake devices.

## Do Not

- Call Playwright directly from outside this package
- Expose `PWAClient` as a concrete type to other packages
- Suppress or ignore selector timeout errors without logging `warn`
- Add Zoom REST API or SDK calls without an ADR update
