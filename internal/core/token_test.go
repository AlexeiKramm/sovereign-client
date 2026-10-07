package core

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestCredentialStoreRoundTrip(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skipf("DPAPI is Windows-only; got %s", runtime.GOOS)
	}
	store := CredentialStore{Path: filepath.Join(t.TempDir(), "credentials.bin")}

	if _, found, err := store.Load(); err != nil || found {
		t.Fatalf("missing store: found=%v err=%v", found, err)
	}

	want := Credentials{
		ServerID:   "phone-1",
		ServerName: "Phone",
		ServerSpki: "spki-fingerprint",
		Token:      "sekrit-token-value",
	}
	if err := store.Save(want); err != nil {
		t.Fatalf("save: %v", err)
	}

	blob, err := os.ReadFile(store.Path)
	if err != nil {
		t.Fatalf("read blob: %v", err)
	}
	if bytes.Contains(blob, []byte(want.Token)) {
		t.Fatal("token was written in the clear")
	}
	if bytes.Contains(blob, []byte(want.ServerName)) {
		t.Fatal("credentials were written in the clear")
	}

	got, found, err := store.Load()
	if err != nil || !found {
		t.Fatalf("load: found=%v err=%v", found, err)
	}
	if got != want {
		t.Fatalf("round trip = %+v, want %+v", got, want)
	}
	if !got.Complete() {
		t.Fatal("Complete() = false for a full credential set")
	}

	if err := store.Delete(); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, found, _ := store.Load(); found {
		t.Fatal("still found after delete")
	}
	if err := store.Delete(); err != nil {
		t.Fatalf("second delete: %v", err)
	}
}
