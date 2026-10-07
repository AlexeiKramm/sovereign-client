package ui

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSettingsMissingFileYieldsDefaults(t *testing.T) {
	got, err := LoadSettings(filepath.Join(t.TempDir(), "settings.json"))
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if got != DefaultSettings() {
		t.Fatalf("missing file: got %+v, want %+v", got, DefaultSettings())
	}
}

func TestSettingsRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	want := Settings{
		Mode:            ModeToggle,
		Hotkey:          "F9",
		Insertion:       InsertType,
		OverlayPosition: PosTopLeft,
	}
	if err := SaveSettings(path, want); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
	got, err := LoadSettings(path)
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if got != want {
		t.Fatalf("round trip: got %+v, want %+v", got, want)
	}
}

func TestSettingsKeepsCustomHotkey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := SaveSettings(path, Settings{Hotkey: "Ctrl+Alt+J"}); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
	got, err := LoadSettings(path)
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if got.Hotkey != "Ctrl+Alt+J" {
		t.Fatalf("custom hotkey lost: %q", got.Hotkey)
	}
}

func TestSaveSettingsDefaultsInsertion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := SaveSettings(path, Settings{Insertion: "teleport"}); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
	got, err := LoadSettings(path)
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if got.Insertion != InsertPaste {
		t.Fatalf("unknown insertion not defaulted: %q", got.Insertion)
	}
	if got.Mode != ModeAuto {
		t.Fatalf("default mode = %q, want %q", got.Mode, ModeAuto)
	}
}

func TestLoadSettingsNormalizesUnknownValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	// A file written by a different build: unknown mode, hotkey and position all fall back.
	if err := os.WriteFile(path, []byte(`{"mode":"telepathy","hotkey":"Hyper+X","overlayPosition":"middle"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := LoadSettings(path)
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	want := Settings{Mode: ModeAuto, Hotkey: DefaultHotkey, Insertion: InsertPaste, OverlayPosition: PosBottomRight}
	if got != want {
		t.Fatalf("normalize: got %+v, want %+v", got, want)
	}
}

func TestSaveSettingsNormalizesBeforeWriting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := SaveSettings(path, Settings{}); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
	got, err := LoadSettings(path)
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if got != DefaultSettings() {
		t.Fatalf("empty settings: got %+v, want %+v", got, DefaultSettings())
	}
}
