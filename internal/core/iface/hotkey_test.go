package iface

import "testing"

func TestParseHotkey(t *testing.T) {
	cases := []struct {
		spec string
		want string
		vk   uint32
	}{
		{"Ctrl+Alt+Space", "Ctrl+Alt+Space", 0x20},
		{"ctrl+shift+d", "Ctrl+Shift+D", 0x44},
		{"F9", "F9", 0x78},
		{"Ctrl+Alt+F12", "Ctrl+Alt+F12", 0x7B},
		{"Win+1", "Win+1", 0x31},
		{"shift+escape", "Shift+Esc", 0x1B},
	}
	for _, c := range cases {
		got, err := ParseHotkey(c.spec)
		if err != nil {
			t.Errorf("ParseHotkey(%q): %v", c.spec, err)
			continue
		}
		if got.String() != c.want {
			t.Errorf("ParseHotkey(%q).String() = %q, want %q", c.spec, got.String(), c.want)
		}
		if got.VK != c.vk {
			t.Errorf("ParseHotkey(%q).VK = %#x, want %#x", c.spec, got.VK, c.vk)
		}
	}
}

func TestParseHotkeyRejectsBadSpecs(t *testing.T) {
	for _, spec := range []string{"", "Ctrl", "Ctrl+", "Ctrl+A+B", "Ctrl+Nonsense", "F25"} {
		if _, err := ParseHotkey(spec); err == nil {
			t.Errorf("ParseHotkey(%q) = nil error, want rejection", spec)
		}
	}
}

func TestDetectorPressRelease(t *testing.T) {
	d := NewDetector(Chord{Mods: Modifiers{Ctrl: true, Alt: true}, VK: 0x20, Key: "Space"})
	mods := Modifiers{Ctrl: true, Alt: true}

	// Space down with modifiers held -> Press.
	if ev, ok := d.Key(0x20, true, mods); !ok || ev != Press {
		t.Fatalf("space down: got (%v, %v), want Press", ev, ok)
	}
	// Auto-repeat: further downs do not re-emit.
	if _, ok := d.Key(0x20, true, mods); ok {
		t.Fatal("auto-repeat emitted an edge")
	}
	// Space up -> Release.
	if ev, ok := d.Key(0x20, false, mods); !ok || ev != Release {
		t.Fatalf("space up: got (%v, %v), want Release", ev, ok)
	}
}

func TestDetectorIgnoresUnmodifiedKey(t *testing.T) {
	d := NewDetector(Chord{Mods: Modifiers{Ctrl: true}, VK: 0x44, Key: "D"})
	if _, ok := d.Key(0x44, true, Modifiers{}); ok {
		t.Fatal("plain D emitted an edge for a Ctrl+D chord")
	}
}

func TestDetectorModifierReleaseEndsHold(t *testing.T) {
	d := NewDetector(Chord{Mods: Modifiers{Ctrl: true, Alt: true}, VK: 0x20, Key: "Space"})
	mods := Modifiers{Ctrl: true, Alt: true}
	if _, ok := d.Key(0x20, true, mods); !ok {
		t.Fatal("chord did not press")
	}
	// The user releases Ctrl while still holding Space: the chord must end.
	ev, ok := d.Key(0x11, false, Modifiers{Alt: true})
	if !ok || ev != Release {
		t.Fatalf("modifier release: got (%v, %v), want Release", ev, ok)
	}
	if d.Held() {
		t.Fatal("detector still reports held after modifier release")
	}
}
