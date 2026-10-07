package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// vectorsDir holds a byte-for-byte vendored copy of the Android repo's
// docs/work/phone-server-desktop/protocol-vectors/ (PROTOCOL.md §11). Both repos run the
// same vectors, so this is the cross-repo contract test.
const vectorsDir = "testdata/protocol-vectors"

type manifestCase struct {
	File      string `json:"file"`
	Type      string `json:"type"`
	Direction string `json:"direction"`
	Expect    string `json:"expect"`
	Fault     string `json:"fault"`
}

type manifestFile struct {
	Protover int            `json:"protover"`
	Cases    []manifestCase `json:"cases"`
}

func loadManifest(t *testing.T) manifestFile {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(vectorsDir, "manifest.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var m manifestFile
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("parse manifest: %v", err)
	}
	return m
}

func caseBytes(t *testing.T, c manifestCase) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(vectorsDir, c.File))
	if err != nil {
		t.Fatalf("%s: read: %v", c.File, err)
	}
	return b
}

func TestManifestDeclaresThisProtocolVersion(t *testing.T) {
	m := loadManifest(t)
	if m.Protover != Version {
		t.Fatalf("manifest protover %d != codec Version %d", m.Protover, Version)
	}
}

func TestValidVectorsCoverCatalogueExactly(t *testing.T) {
	m := loadManifest(t)
	valid := map[string]int{}
	for _, c := range m.Cases {
		if c.Expect == "valid" {
			valid[c.Type]++
		}
	}
	for wire, n := range valid {
		if n != 1 {
			t.Errorf("%s: %d valid vectors, want exactly 1", wire, n)
		}
	}
	want := map[string]bool{}
	for _, mt := range MessageTypes() {
		want[mt.Wire] = true
	}
	for wire := range want {
		if valid[wire] == 0 {
			t.Errorf("catalogue type %s has no valid vector", wire)
		}
	}
	for wire := range valid {
		if !want[wire] {
			t.Errorf("vector for unknown catalogue type %s", wire)
		}
	}
}

func TestEveryCaseExistsAndNoOrphans(t *testing.T) {
	m := loadManifest(t)
	referenced := map[string]bool{}
	for _, c := range m.Cases {
		referenced[c.File] = true
		if _, err := os.Stat(filepath.Join(vectorsDir, c.File)); err != nil {
			t.Errorf("manifest references missing file %s", c.File)
		}
	}
	entries, err := os.ReadDir(vectorsDir)
	if err != nil {
		t.Fatalf("read vectors dir: %v", err)
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".json") || name == "manifest.json" {
			continue
		}
		if !referenced[name] {
			t.Errorf("orphan vector file not in manifest: %s", name)
		}
	}
}

func TestValidVectorsRoundTrip(t *testing.T) {
	m := loadManifest(t)
	for _, c := range m.Cases {
		if c.Expect != "valid" {
			continue
		}
		original := caseBytes(t, c)

		decoded, ok := Decode(original).(Decoded)
		if !ok {
			t.Fatalf("%s: expected Decoded, got %T", c.File, Decode(original))
		}
		if decoded.Message.Type.Wire != c.Type {
			t.Errorf("%s: decoded type %s, want %s", c.File, decoded.Message.Type.Wire, c.Type)
		}

		reencoded, err := Encode(decoded.Message)
		if err != nil {
			t.Fatalf("%s: encode: %v", c.File, err)
		}
		if !jsonEqual(t, original, reencoded) {
			t.Errorf("%s: re-encoded JSON differs\n  original:   %s\n  re-encoded: %s", c.File, original, reencoded)
		}

		again, ok := Decode(reencoded).(Decoded)
		if !ok {
			t.Fatalf("%s: re-decode not Decoded", c.File)
		}
		if !sameMessage(decoded.Message, again.Message) {
			t.Errorf("%s: decode(encode(m)) != m", c.File)
		}
	}
}

func TestUnknownMessageTypesAreIgnored(t *testing.T) {
	for _, c := range loadManifest(t).Cases {
		if c.Expect != "ignored" {
			continue
		}
		if _, ok := Decode(caseBytes(t, c)).(Ignored); !ok {
			t.Errorf("%s: expected Ignored, got %T", c.File, Decode(caseBytes(t, c)))
		}
	}
}

func TestInvalidVectorsAreRejectedWithExpectedFault(t *testing.T) {
	for _, c := range loadManifest(t).Cases {
		if c.Expect != "invalid" {
			continue
		}
		rej, ok := Decode(caseBytes(t, c)).(Rejected)
		if !ok {
			t.Fatalf("%s: expected Rejected, got %T", c.File, Decode(caseBytes(t, c)))
		}
		if string(rej.Fault) != c.Fault {
			t.Errorf("%s: fault %s, want %s", c.File, rej.Fault, c.Fault)
		}
	}
}

func TestUnknownFieldsAreIgnored(t *testing.T) {
	var ping map[string]any
	if err := json.Unmarshal(caseBytes(t, manifestCase{File: "ping.json"}), &ping); err != nil {
		t.Fatalf("parse ping: %v", err)
	}
	ping["futureField"] = 42
	payload, ok := ping["payload"].(map[string]any)
	if !ok {
		t.Fatal("ping payload is not an object")
	}
	payload["anotherFutureField"] = "ok"
	mutated, err := json.Marshal(ping)
	if err != nil {
		t.Fatalf("marshal mutated: %v", err)
	}
	if _, ok := Decode(mutated).(Decoded); !ok {
		t.Fatalf("unknown fields must be ignored, got %T", Decode(mutated))
	}
}

func TestOversizedFramesAreRejectedAsTooLarge(t *testing.T) {
	huge := strings.Repeat("x", MaxFrameBytes+1)
	frame := `{"v":1,"type":"PROVISIONAL_TEXT","id":"x","ts":1,"payload":{"sessionId":"s","seq":1,"text":"` + huge + `"}}`
	rej, ok := Decode([]byte(frame)).(Rejected)
	if !ok || rej.Fault != FaultTooLarge {
		t.Fatalf("expected TOO_LARGE, got %T", Decode([]byte(frame)))
	}
}

func TestMalformedJSONIsRejected(t *testing.T) {
	rej, ok := Decode([]byte("{not json")).(Rejected)
	if !ok || rej.Fault != FaultMalformedEnvelope {
		t.Fatalf("expected MALFORMED_ENVELOPE, got %T", Decode([]byte("{not json")))
	}
}

// sameMessage compares everything except MessageType, whose newValue func field makes the
// struct non-comparable and non-DeepEqual.
func sameMessage(a, b Message) bool {
	return a.Type.Wire == b.Type.Wire &&
		a.ID == b.ID &&
		a.Ts == b.Ts &&
		reflect.DeepEqual(a.Payload, b.Payload)
}

func jsonEqual(t *testing.T, a, b []byte) bool {
	t.Helper()
	var av, bv any
	if err := json.Unmarshal(a, &av); err != nil {
		t.Fatalf("unmarshal a: %v", err)
	}
	if err := json.Unmarshal(b, &bv); err != nil {
		t.Fatalf("unmarshal b: %v", err)
	}
	return reflect.DeepEqual(av, bv)
}
