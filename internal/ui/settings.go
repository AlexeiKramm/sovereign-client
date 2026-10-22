package ui

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/alexeikramm/sovereign-client/internal/core/iface"
)

// Insertion methods for the finalized block.
const (
	// InsertPaste sets the clipboard and sends one Ctrl+V — fast and atomic for any length.
	InsertPaste = "paste"
	// InsertType types the text with SendInput/KEYEVENTF_UNICODE, leaving the clipboard untouched.
	InsertType = "type"
)

// Post-utterance separators appended after each finalized block, so two consecutive dictations are
// not glued together at the cursor. The stored value is a short name; separatorText maps it to the
// characters actually injected.
const (
	// SepNone appends nothing (the historical behaviour).
	SepNone = "none"
	// SepSpace appends a single space — the default, so consecutive prose utterances stay separate.
	SepSpace = "space"
	// SepNewline appends a line feed, for lists and notes where each utterance is its own line.
	SepNewline = "newline"
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

// Theme modes for light/dark appearance.
const (
	ThemeSystem = "system"
	ThemeDark   = "dark"
	ThemeLight  = "light"
)

// DefaultThemePreset is the default accent color preset (Sovereign Cyan).
const DefaultThemePreset = "cyan"

var (
	insertions       = []string{InsertPaste, InsertType}
	separators       = []string{SepNone, SepSpace, SepNewline}
	overlayPositions = []string{PosBottomRight, PosBottomLeft, PosTopRight, PosTopLeft}
	themeModes       = []string{ThemeSystem, ThemeDark, ThemeLight}
	themePresets     = []string{"cyan", "sky", "slate", "teal", "amber", "orange", "blue", "green", "coral", "lilac", "mint"}
)

// Settings is the tray app's user configuration, persisted as JSON beside the identity and
// credentials. It holds no secret, so unlike Credentials it is written in the clear.
//
// Dictation is always tap-or-hold on the same hotkey (a short tap toggles, a longer hold records
// while held), so there is no mode field: a "mode" key written by an older build is simply ignored
// when the file is decoded.
type Settings struct {
	Hotkey              string `json:"hotkey"`
	Insertion           string `json:"insertion"`
	Separator           string `json:"separator"` // appended after each finalized utterance; SepNone/SepSpace/SepNewline
	OverlayPosition     string `json:"overlayPosition"`
	ThemeMode           string `json:"themeMode"`
	ThemePreset         string `json:"themePreset"`
	FeedbackDurationSec int    `json:"feedbackDurationSec"`
	// Debug allows transcript-bearing log lines (committed/live/delivery text) in the tray log. Off
	// by default: regular logs carry only metadata. A truthy SOVEREIGN_DEBUG forces it on.
	Debug bool `json:"debug,omitempty"`
	// LastAddr is runtime state, not user configuration: the host:port of the last successful
	// connection, tried first by the on-demand discovery ladder so a reconnect skips mDNS and the
	// sweep. Empty until the first connect. It is stored here because it is non-secret and already
	// travels with the same atomic-ish save as the rest of the settings.
	LastAddr string `json:"lastAddr,omitempty"`
	// KnownAddrs holds recently successful host:ports (comma-separated, up to 4), probed concurrently
	// on the hint rung so switching between 5 GHz and 2.4 GHz (or different Wi-Fi networks) is instantaneous.
	KnownAddrs string `json:"knownAddrs,omitempty"`
}

// KnownAddresses returns the list of known phone addresses, with LastAddr guaranteed first.
func (s Settings) KnownAddresses() []string {
	var addrs []string
	seen := make(map[string]bool)
	last := strings.TrimSpace(s.LastAddr)
	if last != "" {
		seen[last] = true
		addrs = append(addrs, last)
	}
	if s.KnownAddrs != "" {
		for _, part := range strings.Split(s.KnownAddrs, ",") {
			part = strings.TrimSpace(part)
			if part != "" && !seen[part] {
				seen[part] = true
				addrs = append(addrs, part)
				if len(addrs) >= 4 {
					break
				}
			}
		}
	}
	return addrs
}

// AddKnownAddr prepends addr to KnownAddrs (most recent first, up to 4), updating LastAddr.
func (s *Settings) AddKnownAddr(addr string) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return
	}
	existing := s.KnownAddresses()
	s.LastAddr = addr
	list := []string{addr}
	seen := map[string]bool{addr: true}
	for _, a := range existing {
		if !seen[a] {
			seen[a] = true
			list = append(list, a)
			if len(list) >= 4 {
				break
			}
		}
	}
	s.KnownAddrs = strings.Join(list, ", ")
}

// DefaultSettings is the configuration used before the user changes anything.
func DefaultSettings() Settings {
	return Settings{
		Hotkey:              DefaultHotkey,
		Insertion:           InsertPaste,
		Separator:           SepSpace,
		OverlayPosition:     PosBottomRight,
		ThemeMode:           ThemeSystem,
		ThemePreset:         DefaultThemePreset,
		FeedbackDurationSec: 2,
	}
}

// Normalize replaces empty or unknown fields with their defaults, so a settings file written by an
// older or newer build can never put the app into an unrepresentable state. A hotkey that no longer
// parses falls back to the default rather than leaving the user with dead input.
func (s *Settings) Normalize() {
	if !contains(insertions, s.Insertion) {
		s.Insertion = InsertPaste
	}
	if !contains(separators, s.Separator) {
		s.Separator = SepSpace
	}
	if !contains(overlayPositions, s.OverlayPosition) {
		s.OverlayPosition = PosBottomRight
	}
	if !contains(themeModes, s.ThemeMode) {
		s.ThemeMode = ThemeSystem
	}
	if !contains(themePresets, s.ThemePreset) {
		s.ThemePreset = DefaultThemePreset
	}
	if s.FeedbackDurationSec == 0 {
		s.FeedbackDurationSec = 2
	} else if s.FeedbackDurationSec < -1 || s.FeedbackDurationSec > 5 {
		s.FeedbackDurationSec = 2
	}
	if _, err := iface.ParseHotkey(s.Hotkey); err != nil {
		s.Hotkey = DefaultHotkey
	}
	if s.KnownAddrs != "" || s.LastAddr != "" {
		addrs := s.KnownAddresses()
		if len(addrs) > 0 {
			s.LastAddr = addrs[0]
			s.KnownAddrs = strings.Join(addrs, ", ")
		} else {
			s.LastAddr = ""
			s.KnownAddrs = ""
		}
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

// separatorText maps a stored separator name to the characters appended after a finalized utterance.
// An unknown or empty name appends nothing, so a hand-edited settings file can never inject surprises.
func separatorText(name string) string {
	switch name {
	case SepSpace:
		return " "
	case SepNewline:
		return "\n"
	default:
		return ""
	}
}
