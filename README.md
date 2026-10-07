# Sovereign Desktop

The Windows-first desktop client for **Sovereign**'s phone-as-dictation-server feature. Your phone
does the listening — streaming ASR plus on-device LLM cleanup — while this tray app discovers it on
your LAN, connects over pinned `wss://`, shows a floating pill while you speak, and types the finished
text at your cursor when you stop. No audio and no keystrokes ever touch your document mid-sentence.

> **Status: early.** The wire-protocol codec is implemented and tested; transport, pairing, the
> overlay and hotkeys are in progress. See the plan in the sibling `local-voice-ime` repo under
> `docs/work/phone-server-desktop/`.

## Stack

Go, shipped as a single self-contained `.exe` — no runtime to install. See the Android repo's
`PLAN.md §8` and decision **D-117** for why Go rather than .NET/WPF.

## Build and test

Requires Go 1.27+ (installed per-user at `%LOCALAPPDATA%\Programs\go`).

```powershell
go vet ./...
go test ./...
go build ./...
```

## The wire contract

The phone↔desktop protocol is frozen in the sibling repo at
`docs/work/phone-server-desktop/PROTOCOL.md`. Its golden vectors are vendored under
`internal/core/testdata/protocol-vectors/` and exercised by `internal/core/protocol_test.go`, so this
repo's codec cannot drift from the phone's.
