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
		Mode:                ModeToggle,
		Hotkey:              "F9",
		Insertion:           InsertType,
		Separator:           SepNewline,
		OverlayPosition:     PosTopLeft,
		ThemeMode:           ThemeDark,
		ThemePreset:         "amber",
		FeedbackDurationSec: 3,
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

func TestSettingsPersistsLastAddress(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	s := DefaultSettings()
	s.LastAddr = "192.168.1.42:8765"
	if err := SaveSettings(path, s); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
	got, err := LoadSettings(path)
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if got.LastAddr != s.LastAddr {
		t.Fatalf("LastAddr = %q, want %q", got.LastAddr, s.LastAddr)
	}
}

func TestSettingsPersistsDebugFlag(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	s := DefaultSettings()
	s.Debug = true
	if err := SaveSettings(path, s); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
	got, err := LoadSettings(path)
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if !got.Debug {
		t.Fatal("Debug flag was not persisted")
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

// TestSeparatorText pins the mapping from the stored separator name to the characters injected after
// each finalized utterance, and that an unknown name appends nothing.
func TestSeparatorText(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{SepNone, ""},
		{SepSpace, " "},
		{SepNewline, "\n"},
		{"", ""},
		{"bogus", ""},
	}
	for _, c := range cases {
		if got := separatorText(c.name); got != c.want {
			t.Errorf("separatorText(%q) = %q, want %q", c.name, got, c.want)
		}
	}
}

// TestSettingsDefaultsSeparator checks the out-of-the-box separator is a space, so consecutive
// dictations are separated without the user configuring anything.
func TestSettingsDefaultsSeparator(t *testing.T) {
	if got := DefaultSettings().Separator; got != SepSpace {
		t.Fatalf("default separator = %q, want %q", got, SepSpace)
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
	want := DefaultSettings()
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
