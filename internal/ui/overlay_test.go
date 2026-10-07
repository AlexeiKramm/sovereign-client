package ui

import (
	"testing"

	winplat "github.com/alexeikramm/sovereign-desktop/internal/windows"
)

// TestAnchorPos pins the four overlay anchors against a known work area, including the DPI-scaled
// margin, so the pill lands where the user chose on any monitor scale.
func TestAnchorPos(t *testing.T) {
	work := winplat.Rect{X: 100, Y: 50, W: 1000, H: 800}
	const cw, ch = 420, 120
	const dpi = 96
	m := int(pillMargin)
	cases := []struct {
		pos  string
		want [2]int
	}{
		{PosBottomRight, [2]int{100 + 1000 - m - cw, 50 + 800 - m - ch}},
		{PosBottomLeft, [2]int{100 + m, 50 + 800 - m - ch}},
		{PosTopRight, [2]int{100 + 1000 - m - cw, 50 + m}},
		{PosTopLeft, [2]int{100 + m, 50 + m}},
	}
	for _, c := range cases {
		x, y := anchorPos(work, cw, ch, c.pos, dpi)
		if x != c.want[0] || y != c.want[1] {
			t.Errorf("anchorPos(%s) = (%d,%d), want (%d,%d)", c.pos, x, y, c.want[0], c.want[1])
		}
	}
}

// TestAnchorPosScalesMargin checks that the gap scales with the monitor DPI while the work area is
// already in physical pixels.
func TestAnchorPosScalesMargin(t *testing.T) {
	work := winplat.Rect{X: 0, Y: 0, W: 1000, H: 800}
	x96, _ := anchorPos(work, 420, 120, PosTopLeft, 96)
	x192, _ := anchorPos(work, 420, 120, PosTopLeft, 192)
	if x192 <= x96 {
		t.Fatalf("margin did not grow with DPI: 96dpi x=%d, 192dpi x=%d", x96, x192)
	}
}
