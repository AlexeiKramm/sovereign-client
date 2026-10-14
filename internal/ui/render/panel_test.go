package render

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
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

// TestRenderPanelTallerWithVersion checks the footer is present only when Version is set.
func TestRenderPanelTallerWithVersion(t *testing.T) {
	base, err := RenderPanel(PanelModel{Title: "Sovereign", Status: "Ready", Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	more, err := RenderPanel(PanelModel{
		Title:   "Sovereign",
		Status:  "Ready",
		Version:   "v0.1.0",
		Scale:   1,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, bh := base.Frame.Size()
	_, mh := more.Frame.Size()
	if mh <= bh {
		t.Fatalf("version footer did not grow the panel: %d vs %d", bh, mh)
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

// TestRenderPanelRowsControls checks that every settings control becomes a hit-testable region with
// its id, at the rect it was drawn at.
func TestRenderPanelRowsControls(t *testing.T) {
	p, err := RenderPanel(PanelModel{
		Title:     "Sovereign",
		Status:    "Ready",
		ActiveTab: TabSettings,
		Rows: []Row{
			{Kind: RowChips, Text: "Mode", Options: []Option{
				{ID: "mode:auto", Text: "Auto", On: true},
				{ID: "mode:hold", Text: "Hold"},
			}},
			{Kind: RowToggle, ID: "startup", Text: "Start with Windows", On: true},
			{Kind: RowButton, ID: "quit", Text: "Quit"},
		},
		Scale: 1,
	})
	if err != nil {
		t.Fatalf("RenderPanel: %v", err)
	}
	want := []string{"close", "tab:dictations", "tab:settings", "mode:auto", "mode:hold", "startup", "quit"}
	for _, id := range want {
		c, ok := controlByID(p, id)
		if !ok {
			t.Fatalf("no control with id %q; got %v", id, controlIDs(p))
		}
		// The centre of the control must resolve back to it.
		cx, cy := c.Rect.Min.X+c.Rect.Dx()/2, c.Rect.Min.Y+c.Rect.Dy()/2
		got, ok := p.ControlAt(cx, cy)
		if !ok || got.ID != id {
			t.Fatalf("ControlAt(centre of %q) = %q, %v", id, got.ID, ok)
		}
	}
}

// TestRenderPanelRowHeader pins that section headers render without emitting interactive controls
// and that subsequent headers with dividers are taller than the first header.
func TestRenderPanelRowHeader(t *testing.T) {
	face, err := newFace(13)
	if err != nil {
		t.Fatal(err)
	}
	defer face.Close()

	hFirst := measureRow(Row{Kind: RowHeader, Text: "Voice Dictation", On: false}, 300, 1, face)
	hWithDiv := measureRow(Row{Kind: RowHeader, Text: "Text Output", On: true}, 300, 1, face)
	if hWithDiv <= hFirst {
		t.Fatalf("measureRow with divider = %v, want > first header %v", hWithDiv, hFirst)
	}

	p, err := RenderPanel(PanelModel{
		Title:     "Sovereign",
		Status:    "Ready",
		ActiveTab: TabSettings,
		Rows: []Row{
			{Kind: RowHeader, Text: "Section 1", On: false},
			{Kind: RowButton, ID: "btn1", Text: "Action 1"},
			{Kind: RowHeader, Text: "Section 2", On: true},
			{Kind: RowButton, ID: "btn2", Text: "Action 2"},
		},
		Scale: 1,
	})
	if err != nil {
		t.Fatalf("RenderPanel: %v", err)
	}
	for _, c := range p.Controls {
		if c.ID == "Section 1" || c.ID == "Section 2" {
			t.Fatalf("RowHeader unexpectedly registered as an interactive control: %+v", c)
		}
	}
	if _, ok := controlByID(p, "btn1"); !ok {
		t.Fatal("btn1 missing from controls")
	}
	if _, ok := controlByID(p, "btn2"); !ok {
		t.Fatal("btn2 missing from controls")
	}
}

// TestHistoryCardExpand checks expanding a history card reveals more of the utterance: the card must
// grow taller than the collapsed 4-line preview.
func TestHistoryCardExpand(t *testing.T) {
	long := strings.Repeat("word ", 120)
	base := PanelModel{
		Title: "Sovereign", Status: "Ready", ActiveTab: TabDictations,
		History: []PanelHistoryItem{{ID: "a", TimeAgo: "Just now", CleanText: long, Words: 120, ViewMode: "clean"}},
		Scale:   1,
	}
	collapsed, err := RenderPanel(base)
	if err != nil {
		t.Fatal(err)
	}
	base.History[0].Expanded = true
	expanded, err := RenderPanel(base)
	if err != nil {
		t.Fatal(err)
	}
	_, ch := collapsed.Frame.Size()
	_, eh := expanded.Frame.Size()
	if eh <= ch {
		t.Fatalf("expanded card is not taller: collapsed=%d expanded=%d", ch, eh)
	}
}

// TestHistoryScrollCapsAndClips checks that a MaxHeight cap bounds the panel, exposes a scroll range,
// keeps the whole-card toggle hittable on its visible part, and moves the bitmap when scrolled.
func TestHistoryScrollCapsAndClips(t *testing.T) {
	m := PanelModel{
		Title: "Sovereign", Status: "Ready", ActiveTab: TabDictations,
		History:   []PanelHistoryItem{{ID: "a", TimeAgo: "1 min ago", CleanText: strings.Repeat("word ", 400), Words: 400, ViewMode: "clean", Expanded: true}},
		Scale:     1,
		MaxHeight: 400,
	}
	p, err := RenderPanel(m)
	if err != nil {
		t.Fatal(err)
	}
	if p.MaxScroll <= 0 {
		t.Fatal("expected a scroll range once the list is capped")
	}
	if _, h := p.Frame.Size(); float64(h) > m.MaxHeight {
		t.Fatalf("panel height %d exceeds the %v cap", h, m.MaxHeight)
	}
	c, ok := controlByID(p, "expand:a")
	if !ok {
		t.Fatal("missing the whole-card expand control")
	}
	if c.Rect.Min.Y < p.ScrollArea.Min.Y || c.Rect.Max.Y > p.ScrollArea.Max.Y {
		t.Fatalf("card toggle not clipped into the viewport: %v vs %v", c.Rect, p.ScrollArea)
	}
	m.Scroll = p.MaxScroll
	scrolled, err := RenderPanel(m)
	if err != nil {
		t.Fatal(err)
	}
	if pixelsEqual(p.Frame.Img, scrolled.Frame.Img) {
		t.Fatal("scrolling did not change the rendered bitmap")
	}
}

func controlByID(p Panel, id string) (Control, bool) {
	for _, c := range p.Controls {
		if c.ID == id {
			return c, true
		}
	}
	return Control{}, false
}

func controlIDs(p Panel) []string {
	out := make([]string, 0, len(p.Controls))
	for _, c := range p.Controls {
		out = append(out, c.ID)
	}
	return out
}

func TestRenderPanelDropdown(t *testing.T) {
	opts := []Option{
		{ID: "preset:cyan", Text: "Cyan", On: true},
		{ID: "preset:sky", Text: "Sky"},
	}
	// Collapsed
	pClosed, err := RenderPanel(PanelModel{
		Title:     "Sovereign",
		ActiveTab: TabSettings,
		Rows: []Row{
			{Kind: RowDropdown, ID: "dropdown:accent", Text: "Accent color", Detail: "Cyan", Options: opts, On: false},
		},
		Scale: 1,
	})
	if err != nil {
		t.Fatalf("RenderPanel collapsed: %v", err)
	}
	if _, ok := controlByID(pClosed, "dropdown:accent"); !ok {
		t.Fatal("missing dropdown trigger control when collapsed")
	}
	if _, ok := controlByID(pClosed, "preset:sky"); ok {
		t.Fatal("option should not be present when dropdown is collapsed")
	}

	// Expanded
	pOpen, err := RenderPanel(PanelModel{
		Title:     "Sovereign",
		ActiveTab: TabSettings,
		Rows: []Row{
			{Kind: RowDropdown, ID: "dropdown:accent", Text: "Accent color", Detail: "Cyan", Options: opts, On: true},
		},
		Scale: 1,
	})
	if err != nil {
		t.Fatalf("RenderPanel expanded: %v", err)
	}
	if _, ok := controlByID(pOpen, "dropdown:accent"); !ok {
		t.Fatal("missing dropdown trigger control when expanded")
	}
	if _, ok := controlByID(pOpen, "preset:sky"); !ok {
		t.Fatal("missing option control when expanded")
	}
	_, hClosed := pClosed.Frame.Size()
	_, hOpen := pOpen.Frame.Size()
	if hOpen <= hClosed {
		t.Fatalf("expanded height %d should be greater than collapsed %d", hOpen, hClosed)
	}
}

func TestExportPanelPNG(t *testing.T) {
	presetOpts := make([]Option, len(Presets))
	for i, p := range Presets {
		presetOpts[i] = Option{ID: "preset:" + p.ID, Text: p.Name, On: p.ID == "cyan"}
	}
	settingsRows := []Row{
		{Kind: RowHeader, Text: "Voice Dictation", On: false},
		{Kind: RowChips, Text: "Dictation mode", Options: []Option{
			{ID: "mode:auto", Text: "Tap or hold", On: true},
			{ID: "mode:hold", Text: "Hold to talk"},
			{ID: "mode:toggle", Text: "Press to toggle"},
		}},
		{Kind: RowButton, ID: "hotkey:set", Text: "Set custom hotkey…", Detail: "Ctrl+Alt+Space"},

		{Kind: RowHeader, Text: "Text Output", On: true},
		{Kind: RowChips, Text: "Insertion", Options: []Option{
			{ID: "insertion:paste", Text: "Paste (fast)", On: true},
			{ID: "insertion:type", Text: "Type"},
		}},
		{Kind: RowChips, Text: "After each utterance", Options: []Option{
			{ID: "separator:space", Text: "Space", On: true},
		}},

		{Kind: RowHeader, Text: "Floating Pill", On: true},
		{Kind: RowChips, Text: "Overlay position", Options: []Option{
			{ID: "position:bottom-right", Text: "Bottom right", On: true},
			{ID: "position:bottom-left", Text: "Bottom left"},
			{ID: "position:top-right", Text: "Top right"},
			{ID: "position:top-left", Text: "Top left"},
		}},
		{Kind: RowChips, Text: "Feedback pill duration", Options: []Option{
			{ID: "feedback:1", Text: "1s"},
			{ID: "feedback:2", Text: "2s", On: true},
			{ID: "feedback:3", Text: "3s"},
			{ID: "feedback:0", Text: "Off"},
		}},

		{Kind: RowHeader, Text: "Appearance", On: true},
		{Kind: RowChips, Text: "Theme", Options: []Option{
			{ID: "theme:system", Text: "System", On: true},
			{ID: "theme:dark", Text: "Dark"},
			{ID: "theme:light", Text: "Light"},
		}},
		{Kind: RowDropdown, ID: "dropdown:accent", Text: "Accent color", Detail: "Cyan", Options: presetOpts, On: false},

		{Kind: RowHeader, Text: "System & Connection", On: true},
		{Kind: RowToggle, ID: "startup", Text: "Start with Windows", On: true},
		{Kind: RowButton, ID: "unpair", Text: "Unpair…", Detail: "Phone"},
		{Kind: RowButton, ID: "quit", Text: "Quit Sovereign"},
	}

	sampleHistory := []PanelHistoryItem{
		{
			ID:        "item-1",
			TimeAgo:   "Just now",
			CleanText: "The quick brown fox jumps over the lazy dog and tests our new speech dictation formatting.",
			RawText:   "the quick brown fox jumps over the lazy dog and tests our new speech dictation formatting",
			Words:     15,
			AsrMs:     280,
			CleanupMs: 140,
			ViewMode:  "clean",
		},
		{
			ID:        "item-2",
			TimeAgo:   "5 mins ago",
			CleanText: "Send an update to the design team regarding the Material 3 tonal palettes and high contrast cards.",
			RawText:   "send an update to the design team regarding the material three tonal palettes",
			Words:     18,
			AsrMs:     310,
			CleanupMs: 155,
			ViewMode:  "clean",
		},
	}

	// 1. Dictations Tab (Dark)
	pHistDark, err := RenderPanel(PanelModel{
		Title:     "Sovereign",
		Status:    "Ready — Phone",
		ActiveTab: TabDictations,
		History:   sampleHistory,
		Rows:      settingsRows,
		Version:   "v0.1.0",
		Dark:      true,
		Scale:     1,
	})
	if err != nil {
		t.Fatal(err)
	}

	// 2. Dictations Tab (Light)
	pHistLight, err := RenderPanel(PanelModel{
		Title:     "Sovereign",
		Status:    "Ready — Phone",
		ActiveTab: TabDictations,
		History:   sampleHistory,
		Rows:      settingsRows,
		Version:   "v0.1.0",
		Dark:      false,
		Scale:     1,
	})
	if err != nil {
		t.Fatal(err)
	}

	// 3. Settings Tab (Dark)
	pSettings, err := RenderPanel(PanelModel{
		Title:     "Sovereign",
		Status:    "Ready — Phone",
		ActiveTab: TabSettings,
		History:   sampleHistory,
		Rows:      settingsRows,
		Version:   "v0.1.0",
		Dark:      true,
		Scale:     1,
	})
	if err != nil {
		t.Fatal(err)
	}

	// 4. Settings with Dropdown Open
	openRows := make([]Row, len(settingsRows))
	copy(openRows, settingsRows)
	for i := range openRows {
		if openRows[i].ID == "dropdown:accent" {
			openRows[i].On = true
			break
		}
	}
	pSettingsOpen, err := RenderPanel(PanelModel{
		Title:     "Sovereign",
		Status:    "Ready — Phone",
		ActiveTab: TabSettings,
		History:   sampleHistory,
		Rows:      openRows,
		Version:   "v0.1.0",
		Dark:      true,
		Scale:     1,
	})
	if err != nil {
		t.Fatal(err)
	}

	// 5. Pill Overlays: Listening, Inserted Feedback, Error Feedback
	pillListening, _ := Render(Model{
		Status:      "Listening…",
		Provisional: "testing live dictation transcript",
		Dark:        true,
		Scale:       1,
	})
	pillInserted, _ := Render(Model{
		Committed:     "testing live dictation transcript",
		InsertedWords: 18,
		Dark:          true,
		Scale:         1,
	})
	pillError, _ := Render(Model{
		Status: "Phone offline — start server in Sovereign app",
		Error:  true,
		Dark:   true,
		Scale:  1,
	})
	pillProcessing, _ := Render(Model{
		Status:     "Finishing…",
		Committed:  "testing live dictation transcript",
		Processing: true,
		DotPhase:   1,
		Dark:       true,
		Scale:      1,
	})

	exports := map[string]image.Image{
		"panel_preview.png":           pHistDark.Frame.Img,
		"panel_preview_light.png":     pHistLight.Frame.Img,
		"panel_settings.png":          pSettings.Frame.Img,
		"panel_preview_open.png":      pSettingsOpen.Frame.Img,
		"pill_listening_preview.png":  pillListening.Img,
		"pill_inserted_preview.png":   pillInserted.Img,
		"pill_error_preview.png":      pillError.Img,
		"pill_processing_preview.png": pillProcessing.Img,
	}

	for name, img := range exports {
		f, err := os.Create(filepath.Join(t.TempDir(), name))
		if err == nil {
			_ = png.Encode(f, img)
			f.Close()
		}
		if outDir := os.Getenv("SOVEREIGN_EXPORT_DIR"); outDir != "" {
			if outF, err := os.Create(filepath.Join(outDir, name)); err == nil {
				_ = png.Encode(outF, img)
				outF.Close()
			}
		}
	}
}
