package ui

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// Dictation trigger modes (PLAN.md §3): hold-to-talk or press-to-toggle.
const (
	ModeHold   = "hold"
	ModeToggle = "toggle"
)

// Overlay anchor positions (consumed by the pill overlay). The user picks one from the tray.
const (
	PosBottomRight = "bottom-right"
	PosBottomLeft  = "bottom-left"
	PosTopRight    = "top-right"
	PosTopLeft     = "top-left"
)

// DefaultHotkey is the hotkey bound until the user picks another preset.
const DefaultHotkey = "Ctrl+Alt+Space"

// HotkeyPresets are the bindings offered in the tray. Free-form capture arrives with the keyboard
// hook; for now the user chooses from this fixed set.
var HotkeyPresets = []string{"Ctrl+Alt+Space", "Ctrl+Shift+Space", "Ctrl+Alt+D", "F9"}

var overlayPositions = []string{PosBottomRight, PosBottomLeft, PosTopRight, PosTopLeft}

// Settings is the tray app's user configuration, persisted as JSON beside the identity and
// credentials. It holds no secret, so unlike Credentials it is written in the clear.
type Settings struct {
	Mode              string `json:"mode"`
	Hotkey            string `json:"hotkey"`
	InjectionFallback bool   `json:"injectionFallback"`
	OverlayPosition   string `json:"overlayPosition"`
}

// DefaultSettings is the configuration used before the user changes anything.
func DefaultSettings() Settings {
	return Settings{
		Mode:            ModeHold,
		Hotkey:          DefaultHotkey,
		OverlayPosition: PosBottomRight,
	}
}

// Normalize replaces empty or unknown fields with their defaults, so a settings file written by an
// older or newer build can never put the app into an unrepresentable state.
func (s *Settings) Normalize() {
	if s.Mode != ModeHold && s.Mode != ModeToggle {
		s.Mode = ModeHold
	}
	if !contains(HotkeyPresets, s.Hotkey) {
		s.Hotkey = DefaultHotkey
	}
	if !contains(overlayPositions, s.OverlayPosition) {
		s.OverlayPosition = PosBottomRight
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
