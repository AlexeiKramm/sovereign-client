# Sovereign Desktop Client

The lightweight Windows companion for **[Sovereign](https://github.com/AlexeiKramm/sovereign)** — the private, on-device smart dictation app for Android.

Your phone acts as your microphone and on-device AI compute engine (speech recognition + local LLM cleanup), while this desktop companion allows you to dictate directly into any Windows application at your active cursor over your local network.

No cloud, no accounts, and no audio ever leaves your local Wi-Fi. Your PC does not need a microphone or AI-capable hardware.

<p align="center">
  <img src="assets/pill.gif" width="72%" alt="The desktop client's floating pill: Listening, then the green Inserted state reporting the word count" />
</p>

---

## The Sovereign Architecture

```mermaid
flowchart LR
  A["💻 1. Desktop Hotkey<br/>Press Ctrl+Alt+Space in any app"] -->|"Local Wi-Fi (wss://)"| B["📱 2. Phone (Sovereign Engine)<br/>Mic capture · sherpa-onnx ASR · LiteRT-LM polish"]
  B -->|"Polished text"| C["💻 3. Desktop Cursor<br/>Inserted directly into active window"]
```

<p align="center">
  <img src="assets/desktop-overview.png" width="100%" alt="The control window and floating pill on the PC, next to the phone's server screen showing the PC as connected" />
</p>

- **Main Repository & Documentation:** All architectural specifications, Android app code, model guides, and protocol definitions live in the primary repository: **[https://github.com/AlexeiKramm/sovereign](https://github.com/AlexeiKramm/sovereign)**.
- **This Repository:** Holds the standalone Windows desktop client (`sovereign-client.exe`), written in Go with zero external runtime dependencies.

---

## Features

- **Global Hotkey:** Press `Ctrl+Alt+Space` (customizable) to dictate into any window (Word, browser, code editors, chat).
- **Tap or Hold to Dictate:** One hotkey does both — a short tap toggles dictation on and off, holding records only while held.
- **Floating Pill Overlay:** Unobtrusive click-through overlay showing live settled text in white and in-progress speech in gray. Press `Esc` to cancel a dictation.
- **Clean Insertion:** Inserts text instantly at your cursor (paste-insert with clipboard restoration, or synthetic typing fallback).
- **Control Window:** Left-click the tray icon to view live dictation history, processing latency, connection status, and settings. It always opens on the history page and closes by itself when you click into another app.
- **Private & Direct:** Connects directly to your phone over local Wi-Fi with encrypted WebSockets (`wss://`) and mutual fingerprint pairing.

---

## Getting Started

### 1. Requirements
- A phone running **Sovereign** on the same local Wi-Fi network.
- Windows 10 or Windows 11.

### 2. Quick Setup

1. **Start the Phone Server:** On your phone, open Sovereign, go to the **Home** tab, and tap the **Sovereign Server** card to switch the server on.
2. **Download the Desktop Client:** Grab the latest `sovereign-client.exe` from the [Releases page](https://github.com/AlexeiKramm/sovereign-client/releases/latest).
3. **Run and Pair:**
   - Launch `sovereign-client.exe`. It starts in your Windows system tray.
   - Right-click the tray icon and select **Pair with phone…** (the same button is in the control window's **Settings** tab). The client finds your phone on the network by itself (mDNS).
   - Your phone shows a pairing request with a fingerprint. Check that it matches the one the client shows, then tap **Approve**.
4. **Dictate:**
   - Focus any text field on your PC and press `Ctrl+Alt+Space`.
   - Speak toward your phone. As soon as you finish, the polished text appears at your PC cursor.

<p align="center">
  <img src="assets/pairing.png" width="100%" alt="Pairing: the PC shows a fingerprint, the phone shows the same fingerprint in a pairing request with Deny and Approve buttons" />
</p>

---

## Settings & Controls

Left-click the tray icon to open the **Control Window**:

<p align="center">
  <img src="assets/control-window.png" width="46%" alt="The control window: connection status, the Dictations and Settings tabs, and the rolling dictation history with per-entry word counts" />
</p>

- **Set Custom Hotkey:** Rebind to your preferred keyboard combination. Dictation itself is always tap-or-hold.
- **Insertion Method:** Fast clipboard paste (restores previous clipboard) or direct typing.
- **After Each Utterance:** Automatically append a space, newline, or nothing.
- **Overlay Position:** Bottom Right, Bottom Left, Top Right, or Top Left.
- **Feedback Pill Duration:** How long the result pill stays on screen after insertion: 1 s, 2 s, 3 s, or Off.
- **Appearance:** System, Dark, or Light theme, plus an accent color.
- **Start with Windows:** Toggle automatic launch on user login.
- **Log Transcripts (debug):** Also write dictated text to the log file. Off by default.
- **Pair with phone…:** Pair with a new phone or re-pair the current one.

Diagnostics and logs are written to `%LOCALAPPDATA%\Sovereign\sovereign-client.log`.

---

## Build from Source

Requires Go 1.27+ on Windows:

```powershell
# Clone the repository
git clone https://github.com/AlexeiKramm/sovereign-client.git
cd sovereign-client

# Run tests
go test ./...

# Build the GUI binary (no console window).
# -trimpath is not optional: without it the build embeds your own filesystem
# path (C:\Users\<you>\...) into the binary you distribute.
go build -trimpath -ldflags "-s -w -H=windowsgui" -o sovereign-client.exe ./cmd/sovereign-client
```

---

## License

Apache-2.0 License. See [LICENSE](LICENSE). The main
[Sovereign repository](https://github.com/AlexeiKramm/sovereign) holds the Android app.
