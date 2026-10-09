package render

import (
	"image"
	"strings"
	"testing"

	"golang.org/x/image/font"
)

func TestInterFontParses(t *testing.T) {
	if !FontAvailable() {
		t.Fatalf("bundled Inter font did not parse: %v", parseErr)
	}
}

func TestRenderProducesCardAndShadow(t *testing.T) {
	f, err := Render(Model{
		Status:    "Listening…",
		Committed: "This is the settled text.",
		Dark:      true,
		Scale:     1,
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if f.Pad != 0 {
		t.Fatalf("expected zero shadow margin, got Pad=%d", f.Pad)
	}
	w, h := f.Size()
	if w != int(pillWidth) || h <= 0 {
		t.Fatalf("card size = %dx%d, want width %v and a positive height", w, h, pillWidth)
	}
	if got := f.Img.Bounds().Size(); got.X != w || got.Y != h {
		t.Fatalf("bitmap %v does not equal card %dx%d", got, w, h)
	}
	if opaquePixels(f.Img) == 0 {
		t.Fatal("rendered bitmap is fully transparent")
	}
	// The centre of the card must be opaque: it is the surface fill, not a transparent hole.
	if a := f.Img.RGBAAt(w/2, h/2).A; a < 0xF0 {
		t.Fatalf("card centre alpha = %d, want >= 240", a)
	}
}

func TestRenderTallerWithText(t *testing.T) {
	short, err := Render(Model{Status: "Listening…", Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	long, err := Render(Model{Status: "Listening…", Committed: "one two three four five six seven eight nine ten eleven twelve", Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	_, sh := short.Size()
	_, lh := long.Size()
	if lh <= sh {
		t.Fatalf("text should grow the card: empty=%d, with text=%d", sh, lh)
	}
}

func TestRenderClampsLongText(t *testing.T) {
	// Many wrapped lines must be clamped to the last maxBodyLines, so the card cannot grow without
	// bound while a long utterance streams in.
	words := strings.Repeat("word ", 400)
	f, err := Render(Model{Status: "Listening…", Committed: words, Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	_, h := f.Size()
	maxH := int(2*pillPadding+statusLine+bodyGapTop+float64(maxBodyLines)*bodyLine) + 1
	if h > maxH {
		t.Fatalf("card height %d exceeds the clamp %d", h, maxH)
	}
}

func TestRenderProvisionalDiffersFromCommitted(t *testing.T) {
	base := Model{Status: "Listening…", Committed: "hello world "}
	a, err := Render(base)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Render(Model{Status: "Listening…", Committed: "hello world ", Provisional: "and the live tail"})
	if err != nil {
		t.Fatal(err)
	}
	if pixelsEqual(a.Img, b.Img) {
		t.Fatal("adding a provisional tail changed nothing on screen")
	}
}

func TestRenderProcessingShowsCyclingDots(t *testing.T) {
	listen, err := Render(Model{Status: "Listening…", Dark: true, Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	proc, err := Render(Model{Status: "Finishing…", Processing: true, Dark: true, Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	if pixelsEqual(listen.Img, proc.Img) {
		t.Fatal("processing state rendered identically to listening")
	}

	// The mark must change with DotPhase, or the overlay's timer would draw a static picture.
	p0, err := Render(Model{Processing: true, DotPhase: 0, Dark: true, Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	p1, err := Render(Model{Processing: true, DotPhase: 1, Dark: true, Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	if pixelsEqual(p0.Img, p1.Img) {
		t.Fatal("processing mark did not change with DotPhase")
	}

	// An empty Status defaults to "Finishing…" while processing, so these must be identical.
	if !pixelsEqual(proc.Img, p0.Img) {
		t.Fatal("Processing with an empty Status should default to the Finishing… label")
	}
}

func TestWrapBreaksAtWidth(t *testing.T) {
	face, err := newFace(bodyFont)
	if err != nil {
		t.Fatal(err)
	}
	defer face.Close()
	const max = 160.0
	lines := wrap(face, []span{{text: strings.Repeat("alpha bravo charlie ", 12), col: DarkTheme.OnSurface}}, max)
	if len(lines) < 2 {
		t.Fatalf("expected wrapping into multiple lines, got %d", len(lines))
	}
	for i, line := range lines {
		total := 0.0
		space := float64(font.MeasureString(face, " ").Ceil())
		for j, w := range line {
			if j > 0 {
				total += space
			}
			total += w.width
		}
		if total > max && !(len(line) == 1 && line[0].width > max) {
			t.Fatalf("line %d is %v wide, over the %v limit: %q", i, total, max, lineText(line))
		}
	}
}

func TestWrapHonoursNewlines(t *testing.T) {
	face, err := newFace(bodyFont)
	if err != nil {
		t.Fatal(err)
	}
	defer face.Close()
	lines := wrap(face, []span{{text: "first\nsecond", col: DarkTheme.OnSurface}}, 1000)
	if len(lines) != 2 || lineText(lines[0]) != "first" || lineText(lines[1]) != "second" {
		t.Fatalf("newline not honoured: %v", lines)
	}
}

func TestTokenize(t *testing.T) {
	got := tokenize("a  b\nc")
	want := []string{"a", "b", "\n", "c"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("tokenize = %v, want %v", got, want)
	}
}

func lineText(line []word) string {
	parts := make([]string, len(line))
	for i, w := range line {
		parts[i] = w.text
	}
	return strings.Join(parts, " ")
}

func opaquePixels(img *image.RGBA) int {
	n := 0
	for i := 3; i < len(img.Pix); i += 4 {
		if img.Pix[i] != 0 {
			n++
		}
	}
	return n
}

func pixelsEqual(a, b *image.RGBA) bool {
	if a.Bounds() != b.Bounds() {
		return false
	}
	for i := range a.Pix {
		if a.Pix[i] != b.Pix[i] {
			return false
		}
	}
	return true
}
