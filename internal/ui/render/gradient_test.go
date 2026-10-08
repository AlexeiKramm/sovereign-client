package render

import "testing"

// TestPanelBackgroundFadesToSurface checks the window background is a real gradient: the accent glow
// at the top must differ from the surface at the bottom, and the interior must stay opaque.
func TestPanelBackgroundFadesToSurface(t *testing.T) {
	th := ThemeForPreset("cyan", true)
	g := panelBackground(200, 400, th, true)
	top := g.RGBAAt(100, 0)
	bottom := g.RGBAAt(100, 399)
	if top == bottom {
		t.Fatal("background does not vary from the accent glow to the surface")
	}
	if bottom.A < 0xF0 {
		t.Fatalf("background bottom alpha = %d, want effectively opaque", bottom.A)
	}
}

// TestPressedControlHighlights checks a held control is visibly different, and that an unknown or
// empty press id leaves the cached frame untouched (so ordinary repaints stay allocation-free).
func TestPressedControlHighlights(t *testing.T) {
	m := PanelModel{
		Title:     "Sovereign",
		Status:    "Ready",
		ActiveTab: TabSettings,
		Rows:      []Row{{Kind: RowButton, ID: "quit", Text: "Quit Sovereign"}},
		Scale:     1,
	}
	p, err := RenderPanel(m)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := controlByID(p, "quit"); !ok {
		t.Fatal("no quit control to press")
	}
	pressed := WithPressHighlight(p.Frame.Img, p.Controls, "quit", true)
	if pixelsEqual(p.Frame.Img, pressed) {
		t.Fatal("pressing a control did not change the panel bitmap")
	}
	none := WithPressHighlight(p.Frame.Img, p.Controls, "does-not-exist", true)
	if !pixelsEqual(p.Frame.Img, none) {
		t.Fatal("an unknown pressed id changed the panel bitmap")
	}
	if got := WithPressHighlight(p.Frame.Img, p.Controls, "", true); got != p.Frame.Img {
		t.Fatal("an empty pressed id must return the same frame")
	}
}

// TestPanelCornersRemainTransparent guards the gradient fill's rounded clipping against filling the
// whole rectangular bitmap (which would square off the window's shadowed corners).
func TestPanelCornersRemainTransparent(t *testing.T) {
	p, err := RenderPanel(PanelModel{Title: "Sovereign", Status: "Ready", Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	if a := p.Frame.Img.RGBAAt(0, 0).A; a != 0 {
		t.Fatalf("panel corner alpha = %d, want transparent", a)
	}
}

// BenchmarkPanelBackground measures the gradient build for a full-height settings window, since the
// panel repaints it on every mouse move and live-text update.
func BenchmarkPanelBackground(b *testing.B) {
	th := ThemeForPreset("cyan", true)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		panelBackground(388, 700, th, true)
	}
}
