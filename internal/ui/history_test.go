package ui

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// fakeCipher is a reversible, non-plaintext transform so the store's persistence logic is tested
// without DPAPI (which is Windows-only). It rejects unsealed input, mirroring DPAPI's failure on a
// plaintext file, so the legacy-import path is exercised.
type fakeCipher struct{}

var fakeMagic = []byte("fake-sealed\x00")

func (fakeCipher) seal(p []byte) ([]byte, error) {
	out := append([]byte{}, fakeMagic...)
	for _, b := range p {
		out = append(out, b^0x5a)
	}
	return out, nil
}

func (fakeCipher) open(b []byte) ([]byte, error) {
	if !bytes.HasPrefix(b, fakeMagic) {
		return nil, errors.New("fakeCipher: input is not sealed")
	}
	out := make([]byte, 0, len(b)-len(fakeMagic))
	for _, v := range b[len(fakeMagic):] {
		out = append(out, v^0x5a)
	}
	return out, nil
}

func TestHistoryStoreAddAndCap(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "history.json")
	hs := newHistoryStore(tmp, fakeCipher{})

	for i := 1; i <= 10; i++ {
		hs.Add("clean utterance number", "raw utterance number", int64(i*10), int64(i*5))
	}

	items := hs.Items()
	if len(items) != HistoryMaxItems {
		t.Fatalf("expected %d items, got %d", HistoryMaxItems, len(items))
	}

	// First item should be the latest (number 10)
	if items[0].AsrMs != 100 {
		t.Errorf("latest item ASR = %d, want 100", items[0].AsrMs)
	}
	if items[0].Words != 3 {
		t.Errorf("word count = %d, want 3", items[0].Words)
	}

	// Persistence reload
	hs2 := newHistoryStore(tmp, fakeCipher{})
	if len(hs2.Items()) != HistoryMaxItems {
		t.Fatalf("reloaded store items = %d, want %d", len(hs2.Items()), HistoryMaxItems)
	}
}

func TestHistoryStoreViewMode(t *testing.T) {
	hs := NewHistoryStore("")
	item := hs.Add("Clean text", "Raw text", 100, 20)

	if item.ViewMode != ViewClean {
		t.Fatalf("initial view mode = %q, want %q", item.ViewMode, ViewClean)
	}

	hs.SetViewMode(item.ID, ViewDiff)
	items := hs.Items()
	if items[0].ViewMode != ViewDiff {
		t.Fatalf("updated view mode = %q, want %q", items[0].ViewMode, ViewDiff)
	}
}

// TestHistoryStoreSealsAtRest pins that transcripts are not written to disk in the clear.
func TestHistoryStoreSealsAtRest(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "history.json")
	hs := newHistoryStore(tmp, fakeCipher{})
	const marker = "SECRET-DICTATED-PHRASE"
	hs.Add(marker, marker+" raw", 10, 5)

	blob, err := os.ReadFile(tmp)
	if err != nil {
		t.Fatalf("read persisted history: %v", err)
	}
	if len(blob) == 0 {
		t.Fatal("history file is empty")
	}
	if bytes.Contains(blob, []byte(marker)) {
		t.Fatal("transcript was written to disk in the clear")
	}
}

// TestHistoryStoreImportsLegacyPlaintext pins that a pre-encryption plaintext history file is
// imported rather than silently dropped.
func TestHistoryStoreImportsLegacyPlaintext(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "history.json")
	if err := os.WriteFile(tmp, []byte(`[{"id":"1","clean_text":"legacy text","view_mode":"clean"}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	hs := newHistoryStore(tmp, fakeCipher{})
	items := hs.Items()
	if len(items) != 1 || items[0].CleanText != "legacy text" {
		t.Fatalf("legacy history not imported: %+v", items)
	}
}

// TestHistoryStoreReSealsLegacyPlaintext pins that importing a legacy plaintext file also rewrites
// it encrypted, so the cleartext does not linger on disk.
func TestHistoryStoreReSealsLegacyPlaintext(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "history.json")
	const marker = "LEGACY-PLAINTEXT-PHRASE"
	if err := os.WriteFile(tmp, []byte(`[{"id":"1","clean_text":"`+marker+`","view_mode":"clean"}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	hs := newHistoryStore(tmp, fakeCipher{})
	if len(hs.Items()) != 1 {
		t.Fatalf("legacy history not imported: %+v", hs.Items())
	}
	blob, err := os.ReadFile(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(blob, []byte(marker)) {
		t.Fatal("legacy plaintext was not re-sealed on load")
	}
}

// TestHistoryStoreDPAPIAtRest proves the production cipher seals transcripts on Windows.
func TestHistoryStoreDPAPIAtRest(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skipf("DPAPI is Windows-only; got %s", runtime.GOOS)
	}
	tmp := filepath.Join(t.TempDir(), "history.json")
	hs := NewHistoryStore(tmp)
	const marker = "DPAPI-SECRET-PHRASE"
	hs.Add(marker, marker, 1, 1)

	blob, err := os.ReadFile(tmp)
	if err != nil {
		t.Fatalf("read persisted history: %v", err)
	}
	if bytes.Contains(blob, []byte(marker)) {
		t.Fatal("transcript was written in the clear")
	}
	if len(NewHistoryStore(tmp).Items()) != 1 {
		t.Fatal("history did not survive an encrypted round trip")
	}
}
