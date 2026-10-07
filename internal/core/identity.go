package core

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Identity is this desktop's stable, non-secret identity (PROTOCOL.md §0): a per-install UUID
// generated on first run and never reused, plus the human label the phone shows in its pairing
// approval dialog. Unlike Credentials it holds no secret, so it is stored in the clear.
type Identity struct {
	DeviceID string `json:"deviceId"`
	Name     string `json:"name"`
}

// DefaultIdentityPath returns the per-user identity file path.
func DefaultIdentityPath() (string, error) {
	dir, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "device.json"), nil
}

// LoadOrCreateIdentity reads the identity file, creating one on first run with a fresh UUID v4
// and the host name as the default label.
func LoadOrCreateIdentity(path string) (Identity, error) {
	b, err := os.ReadFile(path)
	switch {
	case err == nil:
		var id Identity
		if err := json.Unmarshal(b, &id); err != nil {
			return Identity{}, fmt.Errorf("parse identity: %w", err)
		}
		if id.DeviceID == "" {
			return Identity{}, errors.New("identity file has no deviceId")
		}
		return id, nil
	case !errors.Is(err, fs.ErrNotExist):
		return Identity{}, err
	}

	name, _ := os.Hostname()
	if name == "" {
		name = "Desktop"
	}
	id := Identity{DeviceID: newUUID(), Name: name}
	out, err := json.MarshalIndent(id, "", "  ")
	if err != nil {
		return Identity{}, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return Identity{}, err
	}
	if err := os.WriteFile(path, out, 0o600); err != nil {
		return Identity{}, err
	}
	return id, nil
}

// newUUID returns a random RFC 4122 version-4 UUID, formatted canonically. crypto/rand is the
// only dependency, keeping the module free of a UUID package.
func newUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand on a supported OS does not fail; a failure here is unrecoverable.
		panic("sovereign: crypto/rand unavailable: " + err.Error())
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
