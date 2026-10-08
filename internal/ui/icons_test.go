package ui

import (
	"bytes"
	"encoding/binary"
	"image"
	"testing"
)

// parseICO is a minimal reader used only to check the encoder's structure.
type icoEntry struct {
	w, h       int
	bitCount   int
	bytesInRes int
	offset     int
}

func parseICO(t *testing.T, b []byte) []icoEntry {
	t.Helper()
	if len(b) < 6 {
		t.Fatalf("icon too short: %d bytes", len(b))
	}
	if b[0] != 0 || b[1] != 0 || b[2] != 1 || b[3] != 0 {
		t.Fatalf("bad ICONDIR header: % x", b[:4])
	}
	count := int(binary.LittleEndian.Uint16(b[4:6]))
	if count == 0 {
		t.Fatal("icon declares zero images")
	}
	if len(b) < 6+16*count {
		t.Fatalf("icon too short for %d entries", count)
	}
	entries := make([]icoEntry, count)
	for i := 0; i < count; i++ {
		e := b[6+16*i : 6+16*(i+1)]
		entries[i] = icoEntry{
			w:          dimFromByte(e[0]),
			h:          dimFromByte(e[1]),
			bitCount:   int(binary.LittleEndian.Uint16(e[6:8])),
			bytesInRes: int(binary.LittleEndian.Uint32(e[8:12])),
			offset:     int(binary.LittleEndian.Uint32(e[12:16])),
		}
	}
	return entries
}

func dimFromByte(v byte) int {
	if v == 0 {
		return 256
	}
	return int(v)
}

func TestEncodeICOStructure(t *testing.T) {
	c := stateColor(StateReady)
	imgs := []*image.NRGBA{circleIcon(c, 16), circleIcon(c, 32)}
	b := encodeICO(imgs)
	entries := parseICO(t, b)
	if len(entries) != 2 {
		t.Fatalf("entry count = %d, want 2", len(entries))
	}
	if entries[0].w != 16 || entries[0].h != 16 || entries[1].w != 32 || entries[1].h != 32 {
		t.Fatalf("sizes = %dx%d, %dx%d; want 16x16, 32x32",
			entries[0].w, entries[0].h, entries[1].w, entries[1].h)
	}
	for i, e := range entries {
		if e.bitCount != 32 {
			t.Errorf("entry %d bitCount = %d, want 32", i, e.bitCount)
		}
		if e.offset+e.bytesInRes > len(b) {
			t.Errorf("entry %d runs past EOF: offset %d + %d > %d", i, e.offset, e.bytesInRes, len(b))
		}
		// DIB must declare height = 2*h (XOR + AND planes).
		if got := binary.LittleEndian.Uint32(b[e.offset+8 : e.offset+12]); int(got) != 2*e.h {
			t.Errorf("entry %d biHeight = %d, want %d", i, got, 2*e.h)
		}
	}
	// The blobs must tile the file exactly: last entry ends at len(b).
	last := entries[len(entries)-1]
	if last.offset+last.bytesInRes != len(b) {
		t.Errorf("last entry ends at %d, file is %d bytes", last.offset+last.bytesInRes, len(b))
	}
}

func TestTrayIconIsDeterministic(t *testing.T) {
	a := trayIcon(StateDictating)
	b := trayIcon(StateDictating)
	if !bytes.Equal(a, b) {
		t.Fatal("trayIcon is not deterministic")
	}
	if bytes.Equal(a, trayIcon(StateReady)) {
		t.Fatal("distinct states produced identical icons")
	}
}

func TestCircleIconAlpha(t *testing.T) {
	img := circleIcon(stateColor(StateReady), 32)
	if a := img.NRGBAAt(16, 16).A; a < 240 {
		t.Errorf("centre alpha = %d, want opaque", a)
	}
	if a := img.NRGBAAt(0, 0).A; a != 0 {
		t.Errorf("corner alpha = %d, want 0", a)
	}
}

func TestStateColorsAreDistinct(t *testing.T) {
	states := []TrayState{StateOffline, StateConnecting, StateReady, StateDictating, StateError}
	seen := map[[4]uint8]TrayState{}
	for _, s := range states {
		c := stateColor(s)
		key := [4]uint8{c.R, c.G, c.B, c.A}
		if other, ok := seen[key]; ok {
			t.Errorf("states %v and %v share a colour", other, s)
		}
		seen[key] = s
	}
}

func TestTrayIconContainsSovereignMark(t *testing.T) {
	for _, st := range []TrayState{StateReady, StateDictating, StateOffline} {
		ico := trayIcon(st)
		entries := parseICO(t, ico)
		if len(entries) != 2 {
			t.Fatalf("trayIcon state %v: got %d entries, want 2", st, len(entries))
		}
		if entries[0].w != 16 || entries[1].w != 32 {
			t.Errorf("trayIcon sizes: %dx%d and %dx%d, want 16 and 32", entries[0].w, entries[0].w, entries[1].w, entries[1].w)
		}
	}
}
