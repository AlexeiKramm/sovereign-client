# Sovereign Desktop

The Windows-first desktop client for **Sovereign**'s phone-as-dictation-server feature. Your phone
does the listening — streaming ASR plus on-device LLM cleanup — while this tray app discovers it on
your LAN, connects over pinned `wss://`, shows a floating pill while you speak, and types the finished
text at your cursor when you stop. No audio and no keystrokes ever touch your document mid-sentence.

> **Status: early.** The wire-protocol codec is implemented and tested; transport, pairing, the
> overlay and hotkeys are in progress.

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

## The wire contract

The phone↔desktop protocol is frozen in `sovereign` at
`docs/work/phone-server-desktop/PROTOCOL.md`. Its golden vectors are vendored here under
`internal/core/testdata/protocol-vectors/` and exercised by `internal/core/protocol_test.go`, so this
repo's codec cannot drift from the phone's. **Never hand-edit the vendored vectors** — a protocol
change starts in the Android repo, bumps `protover`, and adds a vector there first.
