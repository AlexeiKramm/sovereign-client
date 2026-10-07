# Sovereign Desktop

The Windows-first desktop client for **Sovereign**'s phone-as-dictation-server feature. Your phone
does the listening — streaming ASR plus on-device LLM cleanup — while this tray app discovers it on
your LAN, connects over pinned `wss://`, shows a floating pill while you speak, and types the finished
text at your cursor when you stop. No audio and no keystrokes ever touch your document mid-sentence.

> **Status: early.** The wire-protocol codec and the transport core — mDNS discovery, pinned-TLS
> `wss://`, mutual-accept pairing, DPAPI token storage, self-healing reconnect, heartbeat and
> latency logging — are implemented and host-tested. The tray and its lifecycle (state icon,
> pairing, settings, single-instance, autostart) and the global hold/toggle hotkeys with `SendInput`
> injection are in; the floating pill overlay (Chunk 4c) is next.

## Where the docs live

This repository is the **desktop client only**. The feature's plan, work order, frozen wire contract
and decision log live in the sibling Android repo, **https://github.com/AlexeiKramm/sovereign**:

| What | Where (in `sovereign`) |
|---|---|
| Plan / design | `docs/work/phone-server-desktop/PLAN.md` (§8 is this repo) |
| Work order (chunks) | `docs/work/phone-server-desktop/CHUNKS.md` |
| Frozen wire contract | `docs/work/phone-server-desktop/PROTOCOL.md` |
| Decision log | `docs/DECISIONS.md` |

**If you need more context than this README gives, read those docs first.** This repo deliberately
keeps no copy of the plan, so there is one source of truth and nothing to drift.

## Stack

Go, shipped as a single self-contained `.exe` — no runtime to install. See `PLAN.md §8` and decision
**D-117** in the Android repo for why Go rather than .NET/WPF.

## Build and test

Requires Go 1.27+ (installed per-user at `%LOCALAPPDATA%\Programs\go`).

```powershell
go vet ./...
go test ./...
go build ./...
```

## Run

With no arguments the binary runs the tray app: it claims a single-instance mutex, loads any DPAPI
credentials, discovers the phone (mDNS, with the LAN-probe fallback), connects, and shows state in
the tray. Its log is at `%LOCALAPPDATA%\Sovereign\sovereign-desktop.log` (a GUI-subsystem binary has
no console).

```powershell
# Tray app (default):
go run ./cmd/sovereign-desktop
go run ./cmd/sovereign-desktop -tray -addr 192.168.1.5:8765   # skip discovery
```

The tray menu carries: the live state, a 5-second **Test dictation**, **Pair with phone…** /
**Unpair…**, the dictation mode, hotkey preset, clipboard fallback and overlay position settings,
and **Start with Windows**.

## Dictating

With the phone connected, press and hold the hotkey to dictate (or press it once to toggle, in
toggle mode), speak, and release: the finished block is typed at the cursor of the window you
started in. Injection is only attempted if that window is still focused, and if `SendInput` is
refused (an elevated target) the tray reports it — and, with the opt-in clipboard fallback on, pastes
via the clipboard instead. Injection is N synthetic keystrokes, so it is **not** a single undo step.

The `-ldflags "-s -w -H=windowsgui"` build has no console; diagnostics go to
`%LOCALAPPDATA%\Sovereign\sovereign-desktop.log`.

The flags below drive the same transport headlessly for development and verification:

```powershell
# Manual host:port (mDNS is blocked by AP/client isolation and misrouted by VPNs):
go run ./cmd/sovereign-desktop -addr 192.168.1.5:8765 -pin <server-spki>

# First-time mutual-accept pairing: the phone shows an approval dialog; the fingerprint
# printed here must match the phone's screen.
go run ./cmd/sovereign-desktop -pair -addr 192.168.1.5:8765

# Browse only, and a one-shot dictation demo:
go run ./cmd/sovereign-desktop -discover
go run ./cmd/sovereign-desktop -once -seconds 5 -v
```

Credentials are stored DPAPI-protected at `%LOCALAPPDATA%\Sovereign\credentials.bin`; the
non-secret device identity sits beside it in `device.json` and the tray settings in `settings.json`.
`-unpair` deletes the desktop's copy.

## The wire contract

The phone↔desktop protocol is frozen in `sovereign` at
`docs/work/phone-server-desktop/PROTOCOL.md`. Its golden vectors are vendored here under
`internal/core/testdata/protocol-vectors/` and exercised by `internal/core/protocol_test.go`, so this
repo's codec cannot drift from the phone's. **Never hand-edit the vendored vectors** — a protocol
change starts in the Android repo, bumps `protover`, and adds a vector there first.
