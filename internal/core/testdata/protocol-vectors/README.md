# Protocol golden vectors

Machine-checkable companions to [`../PROTOCOL.md`](../PROTOCOL.md) (v1, frozen). The Android repo
and the separate desktop repo both run a test that reads this directory, so the contract cannot drift
from what either side actually encodes.

**Do not hand-edit a vector without also editing `PROTOCOL.md`.** Adding a message means adding a
vector. A **breaking** change (one an old receiver would misinterpret) bumps `protover`; an
**additive** message that an old receiver must ignore (PROTOCOL.md §3) does not — see the v1.1
addendum there (`SERVER_STATE`).

## Layout

- `manifest.json` — the index. Every case names a `file` in this directory plus:
  - `expect`: `valid` | `ignored` | `invalid`
  - `type` / `direction` (`C2S` = desktop→phone, `S2C` = phone→desktop) — present for `valid` cases
  - `fault` — present for `invalid` cases: the codec-level rejection reason
- `*.json` — one file per case. Each is the exact bytes that would cross the wire.

Every message type in the PROTOCOL.md catalogue has exactly one `valid` vector.

## What a conforming implementation must do

| `expect` | Requirement |
|---|---|
| `valid` | Decode the envelope and payload; re-encoding produces an equivalent JSON document (same fields and values; key order is not significant). |
| `ignored` | The `type` is unknown to a v1 receiver. Per PROTOCOL.md §3 the receiver MUST ignore it — it is **not** an error and MUST NOT be acted on. |
| `invalid` | Decoding MUST be rejected with the named `fault`. |

## Faults (codec-level)

These are the reasons an envelope is refused at parse time. They are distinct from the on-wire
`ERROR.code` values in PROTOCOL.md §8 (which a live session sends on the wire).

| Fault | Meaning |
|---|---|
| `PROTO_VERSION` | `v` differs from this receiver's version (also an on-wire `ERROR.code`). |
| `TOO_LARGE` | Frame exceeds `maxFrameBytes` (256 KiB; also an on-wire `ERROR.code`). |
| `MALFORMED_ENVELOPE` | Not a JSON object, or missing one of the required envelope keys `v`/`type`/`id`/`ts`/`payload`. |
| `MISSING_FIELD` | A required payload field is absent. |
| `BAD_PAYLOAD` | A payload field has the wrong JSON type or an unknown enum value. |

Unknown *fields* are ignored everywhere (forward compatibility, PROTOCOL.md §3) — only missing
required fields are a fault.
