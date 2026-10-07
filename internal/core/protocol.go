// Package core implements the frozen Sovereign phone <-> desktop wire protocol, v1.
//
// The authoritative spec is local-voice-ime's
// docs/work/phone-server-desktop/PROTOCOL.md (v1, frozen 2026-10-06). This package is
// machine-checked against the golden vectors vendored in testdata/protocol-vectors/, which
// are a byte-for-byte copy of the Android repo's protocol-vectors/. The Android codec
// (server/ServerProtocol.kt) runs the same vectors, so the two implementations cannot drift.
//
// Scope: envelope + message catalogue + parse-time validation only. Transport (TLS,
// WebSocket, mDNS), session state, pairing logic and auth enforcement live above this.
//
// Nothing here depends on Windows or any third-party module: it is stdlib-only and
// cross-platform, so it is testable on any host.
package core

import (
	"encoding/json"
	"fmt"
)

const (
	// Version is the wire version. A mismatch is refused with FaultProtoVersion.
	Version = 1

	// MaxFrameBytes is the maximum UTF-8 size of one application frame (PROTOCOL.md §1/§9).
	MaxFrameBytes = 256 * 1024

	// MaxProvisionalChars is the server-side truncation limit for PROVISIONAL_TEXT.text
	// (PROTOCOL.md §9).
	MaxProvisionalChars = 8 * 1024

	// DefaultPort is the default WebSocket port; the authoritative value is carried by the
	// mDNS SRV record (PROTOCOL.md §1).
	DefaultPort = 8765

	// ServiceType is the DNS-SD service type (PROTOCOL.md §2).
	ServiceType = "_sovereign._tcp.local"

	// Path is the WebSocket path (PROTOCOL.md §1).
	Path = "/sovereign"
)

// Direction records which peer sends a message. Documentation + vector metadata only; it is
// not enforced by the codec.
type Direction string

const (
	// C2S is desktop -> phone.
	C2S Direction = "C2S"
	// S2C is phone -> desktop.
	S2C Direction = "S2C"
)

// Fault is a parse-time rejection reason. These are distinct from the on-wire ERROR.code
// values (PROTOCOL.md §8).
type Fault string

const (
	// FaultProtoVersion means `v` differs from Version.
	FaultProtoVersion Fault = "PROTO_VERSION"
	// FaultTooLarge means the frame exceeds MaxFrameBytes.
	FaultTooLarge Fault = "TOO_LARGE"
	// FaultMalformedEnvelope means the frame is not a JSON object, or a required envelope
	// key (v/type/id/ts/payload) is missing or the wrong JSON type.
	FaultMalformedEnvelope Fault = "MALFORMED_ENVELOPE"
	// FaultMissingField means a required payload field is absent.
	FaultMissingField Fault = "MISSING_FIELD"
	// FaultBadPayload means a payload field has the wrong JSON type or an unknown enum value.
	FaultBadPayload Fault = "BAD_PAYLOAD"
)

// DecodeResult is the outcome of Decode. Exactly one of Decoded, Ignored or Rejected.
type DecodeResult interface{ decodeResult() }

// Decoded is a catalogued message.
type Decoded struct{ Message Message }

// Ignored is an unknown `type`. Per PROTOCOL.md §3 a v1 receiver MUST ignore it; it is not
// an error.
type Ignored struct{ Type string }

// Rejected is a frame that cannot be acted on, with the reason.
type Rejected struct {
	Fault  Fault
	Detail string
}

func (Decoded) decodeResult()  {}
func (Ignored) decodeResult()  {}
func (Rejected) decodeResult() {}

// Message is one decoded catalogued message, without its envelope version.
type Message struct {
	Type    MessageType
	ID      string
	Ts      int64
	Payload Payload
}

// Payload is the marker interface for every catalogued payload.
type Payload interface{ payload() }

// --- Enum values (PROTOCOL.md §4–§6, §8) ------------------------------------------

// PairFail.reason
const (
	PairFailDenied  = "DENIED"
	PairFailLocked  = "LOCKED"
	PairFailVersion = "VERSION"
)

// AuthFail.reason
const (
	AuthFailBadToken     = "BAD_TOKEN"
	AuthFailDisabled     = "DISABLED"
	AuthFailProtoVersion = "PROTO_VERSION"
)

// DictationMode
const (
	ModeHold   = "HOLD"
	ModeToggle = "TOGGLE"
)

// SessionReleased.reason
const (
	ReleaseStopped   = "STOPPED"
	ReleaseCancelled = "CANCELLED"
	ReleaseTakeover  = "TAKEOVER"
	ReleaseTimeout   = "TIMEOUT"
	ReleaseError     = "ERROR"
)

// PipelineState
const (
	StateWarming    = "WARMING"
	StateListening  = "LISTENING"
	StateProcessing = "PROCESSING"
	StateIdle       = "IDLE"
	StateCancelled  = "CANCELLED"
	StateError      = "ERROR"
)

// ServerState (SERVER_STATE, PROTOCOL.md §6 v1.1 addendum). Advisory availability of the phone's
// one pipeline: IDLE = free, RESERVED = a phone-local capture (pill/transcribe), BUSY = a desktop
// dictation session.
const (
	ServerStateIdle     = "IDLE"
	ServerStateReserved = "RESERVED"
	ServerStateBusy     = "BUSY"
)

// UtteranceDone.reason
const (
	DoneFinal     = "FINAL"
	DoneCancelled = "CANCELLED"
	DoneAborted   = "ABORTED"
	DoneError     = "ERROR"
)

// ERROR.code (PROTOCOL.md §8)
const (
	CodeProtoVersion      = "PROTO_VERSION"
	CodeAuthRequired      = "AUTH_REQUIRED"
	CodeAuthFailed        = "AUTH_FAILED"
	CodeSessionBusy       = "SESSION_BUSY"
	CodeSessionUnknown    = "SESSION_UNKNOWN"
	CodeMicBusy           = "MIC_BUSY"
	CodeEngineUnavailable = "ENGINE_UNAVAILABLE"
	CodeTooLarge          = "TOO_LARGE"
	CodeRateLimited       = "RATE_LIMITED"
	CodeInternal          = "INTERNAL"
)

// --- Payloads (PROTOCOL.md §4–§6) -------------------------------------------------

// Pairing (PROTOCOL.md §4)

type PairHelloPayload struct {
	DeviceID string `json:"deviceId"`
	Name     string `json:"name"`
	Protover int64  `json:"protover"`
}

type PairChallengePayload struct {
	ServerNonce string `json:"serverNonce"`
	ServerSpki  string `json:"serverSpki"`
}

type PairResponsePayload struct {
	DeviceID   string `json:"deviceId"`
	ServerName string `json:"serverName"`
	Token      string `json:"token"`
}

type PairDonePayload struct {
	Proof string `json:"proof"`
}

type PairFailPayload struct {
	Reason       string `json:"reason"`
	AttemptsLeft int64  `json:"attemptsLeft"`
}

// Authentication (PROTOCOL.md §5)

type HelloPayload struct {
	Token        string   `json:"token"`
	DeviceID     string   `json:"deviceId"`
	Protover     int64    `json:"protover"`
	Capabilities []string `json:"capabilities"`
}

// MarshalJSON guarantees `capabilities` is a JSON array, never `null`. The phone's kotlinx
// serializer rejects `null` for a List<String> — a HELLO built with a nil slice made the real
// server answer ERROR{INTERNAL}, so a zero-valued HelloPayload must still encode `[]`.
func (h HelloPayload) MarshalJSON() ([]byte, error) {
	caps := h.Capabilities
	if caps == nil {
		caps = []string{}
	}
	return json.Marshal(struct {
		Token        string   `json:"token"`
		DeviceID     string   `json:"deviceId"`
		Protover     int64    `json:"protover"`
		Capabilities []string `json:"capabilities"`
	}{h.Token, h.DeviceID, h.Protover, caps})
}

type WelcomePayload struct {
	Protover      int64    `json:"protover"`
	DeviceID      string   `json:"deviceId"`
	ServerName    string   `json:"serverName"`
	Capabilities  []string `json:"capabilities"`
	MaxFrameBytes int64    `json:"maxFrameBytes"`
}

type AuthFailPayload struct {
	Reason string `json:"reason"`
}

// Dictation session (PROTOCOL.md §6)

type StartDictationPayload struct {
	RequestID string `json:"requestId"`
	Mode      string `json:"mode"`
}

type StartAckPayload struct {
	SessionID string `json:"sessionId"`
}

type SessionBusyPayload struct {
	HolderDeviceID string `json:"holderDeviceId"`
	HolderName     string `json:"holderName"`
	CanTakeover    bool   `json:"canTakeover"`
}

type StopDictationPayload struct {
	SessionID string `json:"sessionId"`
}

type CancelDictationPayload struct {
	SessionID string `json:"sessionId"`
}

type TakeoverPayload struct {
	SessionID string `json:"sessionId"`
	Force     bool   `json:"force"`
}

type SessionReleasedPayload struct {
	SessionID string `json:"sessionId"`
	Reason    string `json:"reason"`
}

type StateChangedPayload struct {
	SessionID string `json:"sessionId"`
	State     string `json:"state"`
}

// ServerStatePayload is the unsolicited pipeline-availability frame (SERVER_STATE, PROTOCOL.md §6
// v1.1 addendum). State ∈ IDLE | RESERVED | BUSY; HolderName/HolderDeviceID are empty when IDLE.
type ServerStatePayload struct {
	State          string `json:"state"`
	HolderDeviceID string `json:"holderDeviceId"`
	HolderName     string `json:"holderName"`
}

func (*ServerStatePayload) payload() {}

type ProvisionalTextPayload struct {
	SessionID string `json:"sessionId"`
	Seq       int64  `json:"seq"`
	Text      string `json:"text"`
}

// CommitPerf is the per-unit timing carried by COMMIT_TEXT.
type CommitPerf struct {
	AsrMs     int64 `json:"asrMs"`
	CleanupMs int64 `json:"cleanupMs"`
}

type CommitTextPayload struct {
	SessionID string     `json:"sessionId"`
	Seq       int64      `json:"seq"`
	Text      string     `json:"text"`
	Perf      CommitPerf `json:"perf"`
}

type UtteranceDonePayload struct {
	SessionID    string `json:"sessionId"`
	DeliveryText string `json:"deliveryText"`
	RawText      string `json:"rawText"`
	Reason       string `json:"reason"`
}

type ErrorPayload struct {
	Code      string  `json:"code"`
	Message   string  `json:"message"`
	Fatal     bool    `json:"fatal"`
	SessionID *string `json:"sessionId,omitempty"`
}

// Ops (PROTOCOL.md §6)

type PingPayload struct {
	ClientTs int64 `json:"clientTs"`
}

type PongPayload struct {
	ClientTs int64 `json:"clientTs"`
	ServerTs int64 `json:"serverTs"`
}

func (*PairHelloPayload) payload()        {}
func (*PairChallengePayload) payload()    {}
func (*PairResponsePayload) payload()     {}
func (*PairDonePayload) payload()         {}
func (*PairFailPayload) payload()         {}
func (*HelloPayload) payload()            {}
func (*WelcomePayload) payload()          {}
func (*AuthFailPayload) payload()         {}
func (*StartDictationPayload) payload()   {}
func (*StartAckPayload) payload()         {}
func (*SessionBusyPayload) payload()      {}
func (*StopDictationPayload) payload()    {}
func (*CancelDictationPayload) payload()  {}
func (*TakeoverPayload) payload()         {}
func (*SessionReleasedPayload) payload()  {}
func (*StateChangedPayload) payload()     {}
func (*ProvisionalTextPayload) payload()  {}
func (*CommitTextPayload) payload()       {}
func (*UtteranceDonePayload) payload()    {}
func (*ErrorPayload) payload()            {}
func (*PingPayload) payload()             {}
func (*PongPayload) payload()             {}

// --- Catalogue --------------------------------------------------------------------

type fieldType int

const (
	ftString fieldType = iota
	ftInt
	ftBool
	ftStringSlice
	ftObject
)

type field struct {
	name   string
	typ    fieldType
	enum   []string
	nested []field
}

func str(name string) field     { return field{name: name, typ: ftString} }
func integer(name string) field { return field{name: name, typ: ftInt} }
func boolean(name string) field { return field{name: name, typ: ftBool} }
func strs(name string) field    { return field{name: name, typ: ftStringSlice} }
func obj(name string, nested ...field) field {
	return field{name: name, typ: ftObject, nested: nested}
}
func enum(name string, values ...string) field {
	return field{name: name, typ: ftString, enum: values}
}

// MessageType is one entry in the v1 catalogue.
type MessageType struct {
	Wire      string
	Direction Direction
	newValue  func() Payload
	fields    []field
}

// NewPayload returns a fresh, empty payload value for this message type.
func (t MessageType) NewPayload() Payload { return t.newValue() }

var catalogue = []MessageType{
	// Pairing (§4)
	{"PAIR_HELLO", C2S, func() Payload { return &PairHelloPayload{} },
		[]field{str("deviceId"), str("name"), integer("protover")}},
	{"PAIR_CHALLENGE", S2C, func() Payload { return &PairChallengePayload{} },
		[]field{str("serverNonce"), str("serverSpki")}},
	{"PAIR_RESPONSE", S2C, func() Payload { return &PairResponsePayload{} },
		[]field{str("deviceId"), str("serverName"), str("token")}},
	{"PAIR_DONE", C2S, func() Payload { return &PairDonePayload{} },
		[]field{str("proof")}},
	{"PAIR_FAIL", S2C, func() Payload { return &PairFailPayload{} },
		[]field{enum("reason", PairFailDenied, PairFailLocked, PairFailVersion), integer("attemptsLeft")}},

	// Authentication (§5)
	{"HELLO", C2S, func() Payload { return &HelloPayload{} },
		[]field{str("token"), str("deviceId"), integer("protover"), strs("capabilities")}},
	{"WELCOME", S2C, func() Payload { return &WelcomePayload{} },
		[]field{integer("protover"), str("deviceId"), str("serverName"), strs("capabilities"), integer("maxFrameBytes")}},
	{"AUTH_FAIL", S2C, func() Payload { return &AuthFailPayload{} },
		[]field{enum("reason", AuthFailBadToken, AuthFailDisabled, AuthFailProtoVersion)}},

	// Dictation session (§6)
	{"START_DICTATION", C2S, func() Payload { return &StartDictationPayload{} },
		[]field{str("requestId"), enum("mode", ModeHold, ModeToggle)}},
	{"START_ACK", S2C, func() Payload { return &StartAckPayload{} },
		[]field{str("sessionId")}},
	{"SESSION_BUSY", S2C, func() Payload { return &SessionBusyPayload{} },
		[]field{str("holderDeviceId"), str("holderName"), boolean("canTakeover")}},
	{"STOP_DICTATION", C2S, func() Payload { return &StopDictationPayload{} },
		[]field{str("sessionId")}},
	{"CANCEL_DICTATION", C2S, func() Payload { return &CancelDictationPayload{} },
		[]field{str("sessionId")}},
	{"TAKEOVER", C2S, func() Payload { return &TakeoverPayload{} },
		[]field{str("sessionId"), boolean("force")}},
	{"SESSION_RELEASED", S2C, func() Payload { return &SessionReleasedPayload{} },
		[]field{str("sessionId"), enum("reason", ReleaseStopped, ReleaseCancelled, ReleaseTakeover, ReleaseTimeout, ReleaseError)}},
	{"STATE_CHANGED", S2C, func() Payload { return &StateChangedPayload{} },
		[]field{str("sessionId"), enum("state", StateWarming, StateListening, StateProcessing, StateIdle, StateCancelled, StateError)}},
	{"SERVER_STATE", S2C, func() Payload { return &ServerStatePayload{} },
		[]field{enum("state", ServerStateIdle, ServerStateReserved, ServerStateBusy), str("holderDeviceId"), str("holderName")}},
	{"PROVISIONAL_TEXT", S2C, func() Payload { return &ProvisionalTextPayload{} },
		[]field{str("sessionId"), integer("seq"), str("text")}},
	{"COMMIT_TEXT", S2C, func() Payload { return &CommitTextPayload{} },
		[]field{str("sessionId"), integer("seq"), str("text"), obj("perf", integer("asrMs"), integer("cleanupMs"))}},
	{"UTTERANCE_DONE", S2C, func() Payload { return &UtteranceDonePayload{} },
		[]field{str("sessionId"), str("deliveryText"), str("rawText"), enum("reason", DoneFinal, DoneCancelled, DoneAborted, DoneError)}},
	{"ERROR", S2C, func() Payload { return &ErrorPayload{} },
		[]field{enum("code", CodeProtoVersion, CodeAuthRequired, CodeAuthFailed, CodeSessionBusy,
			CodeSessionUnknown, CodeMicBusy, CodeEngineUnavailable, CodeTooLarge, CodeRateLimited, CodeInternal),
			str("message"), boolean("fatal")}},

	// Ops (§6)
	{"PING", C2S, func() Payload { return &PingPayload{} }, []field{integer("clientTs")}},
	{"PONG", S2C, func() Payload { return &PongPayload{} }, []field{integer("clientTs"), integer("serverTs")}},
}

var byWire = func() map[string]MessageType {
	m := make(map[string]MessageType, len(catalogue))
	for _, t := range catalogue {
		m[t.Wire] = t
	}
	return m
}()

// MessageTypes returns the v1 catalogue in declaration order.
func MessageTypes() []MessageType { return append([]MessageType(nil), catalogue...) }

// Lookup returns the catalogue entry for a wire type, if known.
func Lookup(wire string) (MessageType, bool) {
	t, ok := byWire[wire]
	return t, ok
}

// --- Codec ------------------------------------------------------------------------

// Decode parses one application frame. It never panics: an unknown `type` yields Ignored
// (PROTOCOL.md §3) and a malformed/refused frame yields Rejected with a Fault.
func Decode(frame []byte) DecodeResult {
	if len(frame) > MaxFrameBytes {
		return Rejected{FaultTooLarge, fmt.Sprintf("frame exceeds %d bytes", MaxFrameBytes)}
	}

	var root map[string]json.RawMessage
	if err := json.Unmarshal(frame, &root); err != nil {
		return Rejected{FaultMalformedEnvelope, "not a JSON object: " + err.Error()}
	}

	vRaw, ok := root["v"]
	if !ok {
		return Rejected{FaultMalformedEnvelope, "missing `v`"}
	}
	var v int64
	if err := json.Unmarshal(vRaw, &v); err != nil {
		return Rejected{FaultMalformedEnvelope, "non-integer `v`"}
	}
	if v != Version {
		return Rejected{FaultProtoVersion, fmt.Sprintf("protocol version %d != %d", v, Version)}
	}

	typeRaw, ok := root["type"]
	if !ok {
		return Rejected{FaultMalformedEnvelope, "missing `type`"}
	}
	var wire string
	if err := json.Unmarshal(typeRaw, &wire); err != nil {
		return Rejected{FaultMalformedEnvelope, "non-string `type`"}
	}

	idRaw, ok := root["id"]
	if !ok {
		return Rejected{FaultMalformedEnvelope, "missing `id`"}
	}
	var id string
	if err := json.Unmarshal(idRaw, &id); err != nil {
		return Rejected{FaultMalformedEnvelope, "non-string `id`"}
	}

	tsRaw, ok := root["ts"]
	if !ok {
		return Rejected{FaultMalformedEnvelope, "missing `ts`"}
	}
	var ts int64
	if err := json.Unmarshal(tsRaw, &ts); err != nil {
		return Rejected{FaultMalformedEnvelope, "non-integer `ts`"}
	}

	payloadRaw, ok := root["payload"]
	if !ok {
		return Rejected{FaultMalformedEnvelope, "missing `payload`"}
	}

	mt, ok := byWire[wire]
	if !ok {
		return Ignored{Type: wire} // §3: ignore, do not error.
	}

	if rej := validate(mt.fields, payloadRaw); rej != nil {
		return *rej
	}
	value := mt.newValue()
	if err := json.Unmarshal(payloadRaw, value); err != nil {
		return Rejected{FaultBadPayload, err.Error()}
	}
	return Decoded{Message{Type: mt, ID: id, Ts: ts, Payload: value}}
}

// validate checks required-field presence, JSON types and enum membership against the
// catalogue's schema for one payload object.
func validate(fields []field, raw json.RawMessage) *Rejected {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return &Rejected{FaultBadPayload, "payload is not a JSON object"}
	}
	for _, f := range fields {
		value, ok := obj[f.name]
		if !ok {
			return &Rejected{FaultMissingField, "missing field `" + f.name + "`"}
		}
		switch f.typ {
		case ftString:
			var s string
			if err := json.Unmarshal(value, &s); err != nil {
				return &Rejected{FaultBadPayload, "field `" + f.name + "` is not a string"}
			}
			if len(f.enum) > 0 && !contains(f.enum, s) {
				return &Rejected{FaultBadPayload, "field `" + f.name + "` has unknown value `" + s + "`"}
			}
		case ftInt:
			var n int64
			if err := json.Unmarshal(value, &n); err != nil {
				return &Rejected{FaultBadPayload, "field `" + f.name + "` is not an integer"}
			}
		case ftBool:
			var b bool
			if err := json.Unmarshal(value, &b); err != nil {
				return &Rejected{FaultBadPayload, "field `" + f.name + "` is not a boolean"}
			}
		case ftStringSlice:
			var ss []string
			if err := json.Unmarshal(value, &ss); err != nil {
				return &Rejected{FaultBadPayload, "field `" + f.name + "` is not a string array"}
			}
		case ftObject:
			if rej := validate(f.nested, value); rej != nil {
				return rej
			}
		}
	}
	return nil
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// Encode renders a catalogued message into its envelope form.
func Encode(m Message) ([]byte, error) {
	payload, err := json.Marshal(m.Payload)
	if err != nil {
		return nil, err
	}
	env := struct {
		V       int             `json:"v"`
		Type    string          `json:"type"`
		ID      string          `json:"id"`
		Ts      int64           `json:"ts"`
		Payload json.RawMessage `json:"payload"`
	}{Version, m.Type.Wire, m.ID, m.Ts, payload}
	return json.Marshal(env)
}

// DecodeString is a convenience wrapper for a UTF-8 frame held as a string.
func DecodeString(frame string) DecodeResult { return Decode([]byte(frame)) }

// EncodeString is a convenience wrapper around Encode.
func EncodeString(m Message) (string, error) {
	b, err := Encode(m)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
