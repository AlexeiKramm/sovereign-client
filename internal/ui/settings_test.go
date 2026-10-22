package ui

import (
	"os"
	"path/filepath"
	"strings"
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
}

// TestLegacyModeKeyIsIgnored pins the retirement of the dictation-mode setting: a settings file
// written by an older build still decodes, and its "mode" value has no effect (the trigger is
// always tap-or-hold now).
func TestLegacyModeKeyIsIgnored(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{"mode":"toggle","hotkey":"F9"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := LoadSettings(path)
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if got.Hotkey != "F9" {
		t.Fatalf("hotkey = %q, want F9 (the rest of the file must still load)", got.Hotkey)
	}

	// And saving must drop the retired key rather than preserving it.
	if err := SaveSettings(path, got); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), `"mode"`) {
		t.Fatalf("saved settings still carry a mode key: %s", b)
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

func TestSettingsKnownAddresses(t *testing.T) {
	s := DefaultSettings()
	s.AddKnownAddr("192.168.1.51:8765")
	if len(s.KnownAddresses()) != 1 || s.KnownAddresses()[0] != "192.168.1.51:8765" {
		t.Fatalf("KnownAddresses = %v", s.KnownAddresses())
	}

	// Add second address (e.g. 2.4 GHz IP)
	s.AddKnownAddr("192.168.1.50:8765")
	addrs := s.KnownAddresses()
	if len(addrs) != 2 || addrs[0] != "192.168.1.50:8765" || addrs[1] != "192.168.1.51:8765" {
		t.Fatalf("KnownAddresses after second addr = %v", addrs)
	}
	if s.LastAddr != "192.168.1.50:8765" {
		t.Fatalf("LastAddr = %q", s.LastAddr)
	}

	// Re-adding existing moves it to the front
	s.AddKnownAddr("192.168.1.51:8765")
	addrs = s.KnownAddresses()
	if len(addrs) != 2 || addrs[0] != "192.168.1.51:8765" || addrs[1] != "192.168.1.50:8765" {
		t.Fatalf("KnownAddresses after re-add = %v", addrs)
	}

	// Persist and reload
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := SaveSettings(path, s); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
	loaded, err := LoadSettings(path)
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if loaded.LastAddr != s.LastAddr || loaded.KnownAddrs != s.KnownAddrs {
		t.Fatalf("loaded = %+v, want %+v", loaded, s)
	}
}

func TestSettingsKnownAddressesCap(t *testing.T) {
	s := DefaultSettings()
	s.AddKnownAddr("10.0.0.1:8765")
	s.AddKnownAddr("10.0.0.2:8765")
	s.AddKnownAddr("10.0.0.3:8765")
	s.AddKnownAddr("10.0.0.4:8765")
	s.AddKnownAddr("10.0.0.5:8765") // 5th address should push out the oldest (10.0.0.1)

	addrs := s.KnownAddresses()
	if len(addrs) != 4 {
		t.Fatalf("len(addrs) = %d, want 4", len(addrs))
	}
	want := []string{"10.0.0.5:8765", "10.0.0.4:8765", "10.0.0.3:8765", "10.0.0.2:8765"}
	for i, w := range want {
		if addrs[i] != w {
			t.Errorf("addrs[%d] = %q, want %q", i, addrs[i], w)
		}
	}
}

func TestSettingsNormalizeLegacyAndDirtyAddrs(t *testing.T) {
	// Case 1: Legacy upgrade where only LastAddr is set
	s1 := DefaultSettings()
	s1.LastAddr = "192.168.1.10:8765"
	s1.KnownAddrs = ""
	s1.Normalize()
	if s1.KnownAddrs != "192.168.1.10:8765" || s1.LastAddr != "192.168.1.10:8765" {
		t.Fatalf("Normalize legacy: LastAddr=%q KnownAddrs=%q", s1.LastAddr, s1.KnownAddrs)
	}

	// Case 2: Dirty whitespace & duplicates
	s2 := DefaultSettings()
	s2.LastAddr = " 192.168.1.10:8765 "
	s2.KnownAddrs = " 192.168.1.10:8765 , 192.168.1.20:8765 , 192.168.1.20:8765 , "
	s2.Normalize()
	if s2.LastAddr != "192.168.1.10:8765" {
		t.Errorf("Normalize dirty: LastAddr=%q", s2.LastAddr)
	}
	if s2.KnownAddrs != "192.168.1.10:8765, 192.168.1.20:8765" {
		t.Errorf("Normalize dirty: KnownAddrs=%q", s2.KnownAddrs)
	}
}
