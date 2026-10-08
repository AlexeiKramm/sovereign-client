package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Credentials is the desktop's post-pairing secret state (PROTOCOL.md §4): the pinned server
// identity plus the bearer token. It is persisted protected at rest — DPAPI on Windows — and
// the token is never written in the clear.
type Credentials struct {
	ServerID   string `json:"serverId"`   // the paired phone's deviceId
	ServerName string `json:"serverName"` // the phone's human label
	ServerSpki string `json:"serverSpki"` // pinned SPKI fingerprint (base64url SHA-256)
	Token      string `json:"token"`      // 32-byte bearer token, base64url
}

// Complete reports whether c is a usable pairing (pin + token present).
func (c Credentials) Complete() bool { return c.ServerSpki != "" && c.Token != "" }

// ProtectAtRest seals a blob with the per-user OS secret store (Windows DPAPI). The credential file
// and the local dictation history both use it, so neither a token nor a transcript is written to
// disk in the clear. On a platform without an implemented store it returns an error rather than
// writing in the clear.
func ProtectAtRest(plain []byte) ([]byte, error) { return protect(plain) }

// UnprotectAtRest reverses ProtectAtRest.
func UnprotectAtRest(blob []byte) ([]byte, error) { return unprotect(blob) }

// CredentialStore persists Credentials in one OS-protected file.
type CredentialStore struct{ Path string }

// DefaultCredentialStore returns the per-user credential store. On Windows this lives under
// %LOCALAPPDATA%\Sovereign — local, not roaming, because DPAPI blobs are bound to the machine
// and user and must not sync elsewhere.
func DefaultCredentialStore() (CredentialStore, error) {
	dir, err := DataDir()
	if err != nil {
		return CredentialStore{}, err
	}
	return CredentialStore{Path: filepath.Join(dir, "credentials.bin")}, nil
}

// DataDir returns the per-user Sovereign directory, creating nothing.
func DataDir() (string, error) {
	if local := os.Getenv("LOCALAPPDATA"); local != "" {
		return filepath.Join(local, "Sovereign"), nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "Sovereign"), nil
}

// Load reads and unprotects the stored credentials. A missing file is not an error: it returns
// (Credentials{}, false, nil) so callers treat "never paired" as empty state.
func (s CredentialStore) Load() (Credentials, bool, error) {
	blob, err := os.ReadFile(s.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return Credentials{}, false, nil
	}
	if err != nil {
		return Credentials{}, false, err
	}
	plain, err := unprotect(blob)
	if err != nil {
		return Credentials{}, false, fmt.Errorf("unprotect credentials: %w", err)
	}
	var c Credentials
	if err := json.Unmarshal(plain, &c); err != nil {
		return Credentials{}, false, fmt.Errorf("parse credentials: %w", err)
	}
	return c, true, nil
}

// Save protects and writes the credentials, creating the directory as needed.
func (s CredentialStore) Save(c Credentials) error {
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return err
	}
	plain, err := json.Marshal(c)
	if err != nil {
		return err
	}
	blob, err := protect(plain)
	if err != nil {
		return err
	}
	return os.WriteFile(s.Path, blob, 0o600)
}

// Delete removes the stored credentials (unpair). A missing file is not an error.
func (s CredentialStore) Delete() error {
	err := os.Remove(s.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}
