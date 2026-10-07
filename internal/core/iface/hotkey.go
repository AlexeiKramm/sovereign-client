// Package iface holds the platform-neutral pieces the desktop client shares across OSes: the
// hotkey model and its event detector, and the UTF-16 helpers injection needs. The concrete Win32
// implementations live in internal/windows.
package iface

import (
	"fmt"
	"strconv"
	"strings"
)

// Modifiers is the set of modifier keys a chord can require.
type Modifiers struct {
	Ctrl  bool
	Alt   bool
	Shift bool
	Win   bool
}

// Has reports whether m contains every modifier in want.
func (m Modifiers) Has(want Modifiers) bool {
	if want.Ctrl && !m.Ctrl {
		return false
	}
	if want.Alt && !m.Alt {
		return false
	}
	if want.Shift && !m.Shift {
		return false
	}
	if want.Win && !m.Win {
		return false
	}
	return true
}

func (m Modifiers) String() string {
	var parts []string
	if m.Ctrl {
		parts = append(parts, "Ctrl")
	}
	if m.Alt {
		parts = append(parts, "Alt")
	}
	if m.Shift {
		parts = append(parts, "Shift")
	}
	if m.Win {
		parts = append(parts, "Win")
	}
	return strings.Join(parts, "+")
}

// Chord is a global key combination: a set of required modifiers plus one non-modifier key, named
// by its Windows virtual-key code.
type Chord struct {
	Mods Modifiers
	VK   uint32
	Key  string // canonical key name, e.g. "Space", "F9", "D"
}

func (c Chord) String() string {
	if c.Mods == (Modifiers{}) {
		return c.Key
	}
	return c.Mods.String() + "+" + c.Key
}

// Event is an edge of the chord: the moment it becomes fully pressed, or fully released.
type Event int

const (
	Press Event = iota
	Release
)

// ParseHotkey parses a spec like "Ctrl+Alt+Space", "Ctrl+Shift+D" or "F9". Modifier order is
// normalised; the key may be a letter, a digit, F1–F24, or a named key.
func ParseHotkey(spec string) (Chord, error) {
	fields := strings.Split(spec, "+")
	var c Chord
	seenKey := false
	for _, raw := range fields {
		tok := strings.TrimSpace(raw)
		if tok == "" {
			return Chord{}, fmt.Errorf("empty component in hotkey %q", spec)
		}
		switch strings.ToLower(tok) {
		case "ctrl", "control":
			c.Mods.Ctrl = true
		case "alt":
			c.Mods.Alt = true
		case "shift":
			c.Mods.Shift = true
		case "win", "meta", "super":
			c.Mods.Win = true
		default:
			if seenKey {
				return Chord{}, fmt.Errorf("hotkey %q has more than one non-modifier key", spec)
			}
			vk, name, err := parseKey(tok)
			if err != nil {
				return Chord{}, err
			}
			c.VK, c.Key = vk, name
			seenKey = true
		}
	}
	if !seenKey {
		return Chord{}, fmt.Errorf("hotkey %q has no non-modifier key", spec)
	}
	return c, nil
}

func parseKey(tok string) (uint32, string, error) {
	upper := strings.ToUpper(tok)
	if len(upper) == 1 {
		ch := upper[0]
		switch {
		case ch >= 'A' && ch <= 'Z':
			return uint32(ch), string(ch), nil
		case ch >= '0' && ch <= '9':
			return uint32(ch), string(ch), nil
		}
	}
	switch upper {
	case "SPACE":
		return 0x20, "Space", nil
	case "ENTER", "RETURN":
		return 0x0D, "Enter", nil
	case "TAB":
		return 0x09, "Tab", nil
	case "ESC", "ESCAPE":
		return 0x1B, "Esc", nil
	case "BACKSPACE":
		return 0x08, "Backspace", nil
	}
	if len(upper) >= 2 && upper[0] == 'F' {
		if n, err := strconv.Atoi(upper[1:]); err == nil && n >= 1 && n <= 24 {
			return uint32(0x70 + n - 1), fmt.Sprintf("F%d", n), nil
		}
	}
	return 0, "", fmt.Errorf("unknown key %q", tok)
}

// Detector turns a stream of raw key events into chord Press/Release edges. Feeding it every key
// event (as the low-level hook does) means auto-repeat is naturally ignored — a held key keeps
// reporting down without re-emitting Press — and releasing a required modifier also releases the
// chord, so hold-to-talk stops even if the main key is still down.
type Detector struct {
	chord Chord
	held  bool
}

// NewDetector returns a detector for the chord.
func NewDetector(c Chord) *Detector { return &Detector{chord: c} }

// Key feeds one key event and reports whether it produced an edge.
func (d *Detector) Key(vk uint32, down bool, mods Modifiers) (Event, bool) {
	if d.held {
		if !mods.Has(d.chord.Mods) {
			d.held = false
			return Release, true
		}
		if vk == d.chord.VK && !down {
			d.held = false
			return Release, true
		}
		return Release, false // auto-repeat or unrelated key: no new edge
	}
	if vk == d.chord.VK && down && mods.Has(d.chord.Mods) {
		d.held = true
		return Press, true
	}
	return Press, false
}

// Held reports whether the chord is currently pressed.
func (d *Detector) Held() bool { return d.held }
