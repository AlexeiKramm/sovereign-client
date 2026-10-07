package core

import (
	"path/filepath"
	"regexp"
	"testing"
)

var uuidV4Re = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestLoadOrCreateIdentityIsStable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "device.json")

	first, err := LoadOrCreateIdentity(path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !uuidV4Re.MatchString(first.DeviceID) {
		t.Fatalf("deviceId %q is not a v4 UUID", first.DeviceID)
	}
	if first.Name == "" {
		t.Fatal("name is empty")
	}

	second, err := LoadOrCreateIdentity(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if first != second {
		t.Fatalf("identity changed on reload: %+v != %+v", first, second)
	}
}

func TestNewUUIDIsUnique(t *testing.T) {
	seen := make(map[string]bool, 1000)
	for i := 0; i < 1000; i++ {
		id := newUUID()
		if !uuidV4Re.MatchString(id) {
			t.Fatalf("newUUID() = %q, not a v4 UUID", id)
		}
		if seen[id] {
			t.Fatalf("duplicate UUID %q", id)
		}
		seen[id] = true
	}
}
