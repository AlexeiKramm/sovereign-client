package ui

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/alexeikramm/sovereign-desktop/internal/core/iface"
)

// Dictation trigger modes.
const (
	// ModeAuto is the default: a tap toggles dictation on/off, a hold records until release — both
	// on the same hotkey.
	ModeAuto   = "auto"
	ModeHold   = "hold"
	ModeToggle = "toggle"
)

// Insertion methods for the finalized block.
const (
	// InsertPaste sets the clipboard and sends one Ctrl+V — fast and atomic for any length.
	InsertPaste = "paste"
	// InsertType types the text with SendInput/KEYEVENTF_UNICODE, leaving the clipboard untouched.
	InsertType = "type"
)

// Overlay anchor positions (consumed by the pill overlay). The user picks one from the tray.
const (
	PosBottomRight = "bottom-right"
	PosBottomLeft  = "bottom-left"
	PosTopRight    = "top-right"
	PosTopLeft     = "top-left"
)

// DefaultHotkey is the hotkey bound until the user picks another.
const DefaultHotkey = "Ctrl+Alt+Space"

// HotkeyPresets are the bindings offered in the tray; the user can also capture a custom one.
var HotkeyPresets = []string{"Ctrl+Alt+Space", "Ctrl+Shift+Space", "Ctrl+Alt+D", "F9"}

var (
	dictationModes   = []string{ModeAuto, ModeHold, ModeToggle}
	insertions       = []string{InsertPaste, InsertType}
	overlayPositions = []string{PosBottomRight, PosBottomLeft, PosTopRight, PosTopLeft}
)

// Settings is the tray app's user configuration, persisted as JSON beside the identity and
// credentials. It holds no secret, so unlike Credentials it is written in the clear.
type Settings struct {
	Mode            string `json:"mode"`
	Hotkey          string `json:"hotkey"`
	Insertion       string `json:"insertion"`
	OverlayPosition string `json:"overlayPosition"`
}

// DefaultSettings is the configuration used before the user changes anything.
func DefaultSettings() Settings {
	return Settings{
		Mode:            ModeAuto,
		Hotkey:          DefaultHotkey,
		Insertion:       InsertPaste,
		OverlayPosition: PosBottomRight,
	}
}

// Normalize replaces empty or unknown fields with their defaults, so a settings file written by an
// older or newer build can never put the app into an unrepresentable state. A hotkey that no longer
// parses falls back to the default rather than leaving the user with dead input.
func (s *Settings) Normalize() {
	if !contains(dictationModes, s.Mode) {
		s.Mode = ModeAuto
	}
	if !contains(insertions, s.Insertion) {
		s.Insertion = InsertPaste
	}
	if !contains(overlayPositions, s.OverlayPosition) {
		s.OverlayPosition = PosBottomRight
	}
	if _, err := iface.ParseHotkey(s.Hotkey); err != nil {
		s.Hotkey = DefaultHotkey
	}
}

// LoadSettings reads the settings file. A missing file is not an error: it yields the defaults.
func LoadSettings(path string) (Settings, error) {
	s := DefaultSettings()
	b, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return s, nil
	case err != nil:
		return s, err
	}
	if err := json.Unmarshal(b, &s); err != nil {
		return DefaultSettings(), err
	}
	s.Normalize()
	return s, nil
}

// SaveSettings writes the settings, creating the directory as needed.
func SaveSettings(path string, s Settings) error {
	s.Normalize()
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
