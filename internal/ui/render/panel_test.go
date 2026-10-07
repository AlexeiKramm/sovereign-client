package render

import (
	"image"
	"testing"
)

// TestRenderPanelLayout checks the panel's size, its controls and the hit-testing the controller
// relies on: the close affordance must be findable, the header must be draggable, and a point on the
// close button must not count as header (so a click closes rather than starts a drag).
func TestRenderPanelLayout(t *testing.T) {
	p, err := RenderPanel(PanelModel{
		Title:     "Sovereign",
		Status:    "Ready — Phone",
		Committed: "hello",
		Info:      []string{"Sessions 1"},
		Scale:     1,
	})
	if err != nil {
		t.Fatalf("RenderPanel: %v", err)
	}
	w, h := p.Frame.Size()
	if w != int(panelWidth) || h <= 0 {
		t.Fatalf("panel size = %dx%d, want width %v and a positive height", w, h, panelWidth)
	}
	if opaquePixels(p.Frame.Img) == 0 {
		t.Fatal("panel bitmap is fully transparent")
	}

	var closeCtl *Control
	for i := range p.Controls {
		if p.Controls[i].Kind == ControlClose {
			closeCtl = &p.Controls[i]
		}
	}
	if closeCtl == nil {
		t.Fatal("no close control in the panel")
	}
	// The centre of the close control must resolve to it, and must not be treated as the header.
	ccx, ccy := closeCtl.Rect.Min.X+closeCtl.Rect.Dx()/2, closeCtl.Rect.Min.Y+closeCtl.Rect.Dy()/2
	if got, ok := p.ControlAt(ccx, ccy); !ok || got.Kind != ControlClose {
		t.Fatalf("ControlAt(close centre) = %+v, %v; want the close control", got, ok)
	}
	if p.InHeader(ccx, ccy) {
		t.Fatal("the close control must not be a drag region")
	}

	// A point on the title (left of the header, away from the close button) is a drag region.
	tx := p.Header.Min.X + 20
	ty := p.Header.Min.Y + p.Header.Dy()/2
	if !p.InHeader(tx, ty) {
		t.Fatalf("InHeader(%d,%d) = false, want true for the title band", tx, ty)
	}
	// A point well below the card is outside everything.
	if _, ok := p.ControlAt(p.Frame.Pad, p.Frame.Pad+p.Frame.Img.Bounds().Dy()); ok {
		t.Fatal("a point below the card resolved to a control")
	}
}

// TestRenderPanelTallerWithInfo checks the footer grows the panel only when there are info lines.
func TestRenderPanelTallerWithInfo(t *testing.T) {
	base, err := RenderPanel(PanelModel{Title: "Sovereign", Status: "Ready", Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	more, err := RenderPanel(PanelModel{
		Title:  "Sovereign",
		Status: "Ready",
		Info:   []string{"Last ASR 1 ms", "Link rtt=2ms", "Sessions 3"},
		Scale:  1,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, bh := base.Frame.Size()
	_, mh := more.Frame.Size()
	if mh <= bh {
		t.Fatalf("info lines did not grow the panel: %d vs %d", bh, mh)
	}
}

// TestPanelControlAtEmpty asserts a panel with no controls hit-tests to nothing.
func TestPanelControlAtEmpty(t *testing.T) {
	p := Panel{Header: image.Rect(0, 0, 10, 10)}
	if _, ok := p.ControlAt(5, 5); ok {
		t.Fatal("empty panel returned a control")
	}
	if !p.InHeader(5, 5) {
		t.Fatal("InHeader(5,5) = false on a 10x10 header")
	}
}
