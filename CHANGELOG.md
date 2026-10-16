# Changelog

## v0.1.0 — 2026-10-16

First public release.

- Dictate into any Windows application from a global hotkey (`Ctrl+Alt+Space`),
  with tap-to-toggle, hold-to-talk and automatic detection.
- Connects directly to the phone over local Wi-Fi using encrypted WebSockets
  (`wss://`) with mutual fingerprint pairing.
- Floating click-through pill overlay showing settled and in-progress text.
- Clean insertion at the cursor: paste-insert with clipboard restoration, with a
  synthetic-typing fallback.
- Tray icon and control window: dictation history, processing latency, connection
  status and settings.
- Single static binary with no external runtime dependencies.
