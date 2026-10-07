# Sovereign Desktop

The Windows-first desktop client for **Sovereign**'s phone-as-dictation-server feature. Your phone
does the listening — streaming ASR plus on-device LLM cleanup — while this tray app discovers it on
your LAN, connects over pinned `wss://`, shows a floating pill while you speak, and types the finished
text at your cursor when you stop. No audio and no keystrokes ever touch your document mid-sentence.

> **Status: early.** The wire-protocol codec and the transport core — mDNS discovery, pinned-TLS
> `wss://`, mutual-accept pairing, DPAPI token storage, self-healing reconnect, heartbeat and
> latency logging — are implemented and host-tested. The tray and its lifecycle (state icon,
> pairing, settings, single-instance, autostart), the global hold/toggle hotkeys with `SendInput`
> injection, the floating pill overlay, and the control window — which holds the settings — are all
> in. The phone's advertised availability (`SERVER_STATE`) is surfaced in the control window and on
> a refused dictation. What remains is polishing.

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

The tray menu is deliberately small: the live state, a 5-second **Test dictation**, **Pair with
phone…** (before pairing) and **Quit**. Left-clicking the icon — or **Open control panel** — opens
the control window, which holds the settings (dictation mode, hotkey, insertion method, overlay
position, **Start with Windows**, unpair and quit).

## Dictating

With the phone connected, use the hotkey (default `Ctrl+Alt+Space`, rebindable from the control
window's **Set custom hotkey…**). The default **auto** mode uses one key for both gestures: a quick
**tap** starts dictation and leaves it running (tap again to stop), while **holding** records until
you release. `Hold to talk` and `Press to toggle` remain selectable modes.

While you speak, a floating **pill** appears at the corner chosen under **Overlay position** (bottom
right by default). It shows the settled text in white and the live tail of the unit still being
recognised in grey — the same split as the phone's card. The pill is a layered window: it is
click-through, never takes focus from the field you are dictating into, and is dismissed by **Esc**
(which ends the session without inserting) or by ending dictation. Its status line tracks the phone
(`Warming up…` → `Listening…` → `Finishing…`).

If the phone is already taken — its own pill is dictating, or another desktop is — the phone refuses
the session and the pill shows **Phone busy — …** for a couple of seconds instead of listening, the
same way the phone's own pill marks a refused tap. The desktop deliberately *asks anyway* rather
than trusting its cached copy of the phone's state: the reply to `START_DICTATION` is authoritative,
so a stale "busy" can never block a dictation that would succeed. The control window's footer shows
the phone's advertised availability while it is open.

**The control window** opens on a left click of the tray icon (or **Open control panel** in its
menu) — systray reports a single tap, not a double-click. It shows the same live transcript in a
larger, focusable window plus the connection status, the last utterance's ASR/cleanup timings, the
link RTT and a session counter — and it is where every setting lives: dictation mode, **Set custom
hotkey…** (press the combination you want), insertion method, overlay position, **Start with
Windows**, pair/unpair, and **Quit**. Drag it by its header; close it with the × or Esc. Closing it
leaves the tray running.

The finished block is inserted at the cursor of the window you started in. Insertion defaults to
**paste** — one Ctrl+V, fast and atomic at any length — with the clipboard restored afterwards; set
**Insertion → Type** to type it character-by-character and leave the clipboard untouched. Whichever
you pick, the other is tried as a fallback so text is never dropped, and if the target window lost
focus since you started, nothing is written and the tray reports it. Typing is N synthetic
keystrokes, so it is **not** a single undo step.

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
