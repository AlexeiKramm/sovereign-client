package render

import (
	"image"
	"image/color"
	"math"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
	"golang.org/x/image/vector"
)

// Logical (96-DPI) metrics. They mirror the phone's card tokens (floating/PillGeometry.kt):
// the card radius, the 8px status dot, the 15sp header and the muted grey live tail. Every value
// is multiplied by the monitor scale before drawing.
const (
	pillWidth    = 420.0 // fixed card width, as the phone's 340dp card is fixed
	pillPadding  = 14.0
	pillRadius   = 18.0
	pillBorder   = 1.5
	statusFont   = 12.5
	statusLine   = 18.0
	bodyFont     = 15.0
	bodyLine     = 20.0
	bodyGapTop   = 6.0
	statusDot    = 8.0
	statusDotGap = 7.0
	shadowSpread = 8.0
	maxBodyLines = 8
)

// Model is what the pill draws, in logical units. Scale is the monitor DPI scale (1.0 = 96 DPI);
// Render multiplies every metric by it, so the caller passes device-independent content only.
type Model struct {
	// Status is the header line, e.g. "Listening…" or "Polishing…".
	Status string
	// Committed is the settled (cleaned) text, drawn in the near-white OnSurface colour.
	Committed string
	// Provisional is the live tail of the unit in flight, drawn in the muted OnSurfaceVariant.
	Provisional string
	// Error switches the header dot + label to the error colour.
	Error bool
	// Dark selects the dark palette (the default for a floating overlay).
	Dark bool
	// Scale is the monitor DPI scale.
	Scale float64
}

// Frame is a rendered pill: the premultiplied-RGBA bitmap and the transparent margin Render added
// around the card for its drop shadow. A layered window shows the whole bitmap, so the controller
// must offset the window by Pad to place the card's top-left corner where it wants it.
type Frame struct {
	Img *image.RGBA
	Pad int
}

// Size returns the on-screen size of the card itself, excluding the shadow margin.
func (f Frame) Size() (w, h int) {
	b := f.Img.Bounds().Size()
	return b.X - 2*f.Pad, b.Y - 2*f.Pad
}

// Render draws the pill. It returns a bitmap ready for UpdateLayeredWindow (premultiplied BGRA when
// reinterpreted as 32-bit, which the windows package does).
func Render(m Model) (Frame, error) {
	faceMu.Lock()
	defer faceMu.Unlock()

	scale := m.Scale
	if scale <= 0 {
		scale = 1
	}
	theme := ThemeFor(m.Dark)

	statusFace, err := newFace(statusFont * scale)
	if err != nil {
		return Frame{}, err
	}
	defer statusFace.Close()
	bodyFace, err := newFace(bodyFont * scale)
	if err != nil {
		return Frame{}, err
	}
	defer bodyFace.Close()

	innerPad := pillPadding * scale
	cardW := pillWidth * scale
	contentW := cardW - 2*innerPad

	lines := wrap(bodyFace, bodySpans(m.Committed, m.Provisional, theme), contentW)
	ellipsis := false
	if len(lines) > maxBodyLines {
		lines = lines[len(lines)-maxBodyLines:]
		ellipsis = true
	}

	statusH := statusLine * scale
	lineH := bodyLine * scale
	bodyH := float64(len(lines)) * lineH
	cardH := 2*innerPad + statusH
	if len(lines) > 0 {
		cardH += bodyGapTop*scale + bodyH
	}

	shadow := int(math.Ceil(shadowSpread * scale))
	totalW := int(math.Round(cardW)) + 2*shadow
	totalH := int(math.Round(cardH)) + 2*shadow

	img := image.NewRGBA(image.Rect(0, 0, totalW, totalH))
	cardX := float64(shadow)
	cardY := float64(shadow)

	drawShadow(img, cardX, cardY, cardW, cardH, pillRadius*scale, shadow, scale, theme)
	// Border: an outer rounded rect in the border colour, then the surface inset by the border
	// width, which leaves a hairline ring that follows the corner radius.
	fillRoundRect(img, cardX, cardY, cardW, cardH, pillRadius*scale, theme.Border)
	bw := pillBorder * scale
	fillRoundRect(img, cardX+bw, cardY+bw, cardW-2*bw, cardH-2*bw, pillRadius*scale-bw, theme.Surface)

	// Header: dot + status label.
	dotCol := theme.Primary
	label := m.Status
	if m.Error {
		dotCol = theme.Error
	}
	if label == "" {
		label = "Listening…"
	}
	dotX := cardX + innerPad
	dotY := cardY + innerPad + (statusH-statusDot*scale)/2
	fillRoundRect(img, dotX, dotY, statusDot*scale, statusDot*scale, statusDot*scale/2, dotCol)

	statusCol := theme.OnSurfaceVariant
	if m.Error {
		statusCol = theme.Error
	}
	statusBase := cardY + innerPad + baseline(statusFace, statusH)
	drawText(img, statusFace, statusCol, dotX+statusDot*scale+statusDotGap*scale, statusBase, label)

	// Body: settled text in white, live tail in grey, wrapped to the card width.
	if len(lines) > 0 {
		bodyTop := cardY + innerPad + statusH + bodyGapTop*scale
		asc := float64(bodyFace.Metrics().Ascent) / 64
		for i, line := range lines {
			base := bodyTop + float64(i)*lineH + asc
			x := cardX + innerPad
			if i == 0 && ellipsis {
				drawText(img, bodyFace, theme.OnSurfaceVariant, x, base, "…")
				x += float64(font.MeasureString(bodyFace, "…").Ceil())
			}
			for _, w := range line {
				drawText(img, bodyFace, w.col, x, base, w.text)
				x += w.width + float64(font.MeasureString(bodyFace, " ").Ceil())
			}
		}
	}

	return Frame{Img: img, Pad: shadow}, nil
}

// bodySpans is the committed/provisional split: the settled text in OnSurface, the live tail in
// OnSurfaceVariant. This is the same white/grey split the phone's card draws, shared by the pill and
// the control window.
func bodySpans(committed, provisional string, theme Theme) []span {
	var spans []span
	if s := strings.TrimRight(committed, " \t\r\n"); s != "" {
		spans = append(spans, span{text: s, col: theme.OnSurface})
	}
	if s := strings.Trim(provisional, " \t\r\n"); s != "" {
		spans = append(spans, span{text: s, col: theme.OnSurfaceVariant})
	}
	return spans
}

type span struct {
	text string
	col  color.RGBA
}

type word struct {
	text  string
	col   color.RGBA
	width float64
}

// wrap flows the spans into lines that fit maxWidth, keeping each word's colour. Words are split on
// runs of whitespace; "\n" forces a line break. It is deliberately simple: the pill shows prose, so
// per-word flow is enough and no hyphenation is attempted.
func wrap(face font.Face, spans []span, maxWidth float64) [][]word {
	if maxWidth <= 0 {
		return nil
	}
	spaceW := float64(font.MeasureString(face, " ").Ceil())
	var lines [][]word
	var cur []word
	curW := 0.0
	flush := func() {
		if len(cur) > 0 {
			lines = append(lines, cur)
		}
		cur = nil
		curW = 0
	}
	for _, sp := range spans {
		for _, tok := range tokenize(sp.text) {
			if tok == "\n" {
				flush()
				continue
			}
			w := float64(font.MeasureString(face, tok).Ceil())
			if len(cur) > 0 {
				if curW+spaceW+w > maxWidth {
					flush()
				} else {
					curW += spaceW
				}
			}
			cur = append(cur, word{text: tok, col: sp.col, width: w})
			curW += w
		}
	}
	flush()
	return lines
}

// tokenize splits text into words, emitting "\n" as its own token for a hard break.
func tokenize(s string) []string {
	var out []string
	var b strings.Builder
	flush := func() {
		if b.Len() > 0 {
			out = append(out, b.String())
			b.Reset()
		}
	}
	for _, r := range s {
		switch r {
		case '\n':
			flush()
			out = append(out, "\n")
		case ' ', '\t', '\r':
			flush()
		default:
			b.WriteRune(r)
		}
	}
	flush()
	return out
}

// drawShadow stacks a few translucent, expanding rounded rects under the card. It is not a real
// Gaussian blur, but at a 1px-ish visual thickness it reads as a soft edge, and it is pure Go.
func drawShadow(img *image.RGBA, x, y, w, h, radius float64, spread int, scale float64, theme Theme) {
	base := theme.Shadow
	for i := spread; i >= 1; i-- {
		f := float64(i)
		a := uint8(0)
		switch {
		case f <= float64(spread)/3:
			a = base.A
		case f <= 2*float64(spread)/3:
			a = uint8(uint32(base.A) * 2 / 3)
		default:
			a = uint8(uint32(base.A) / 3)
		}
		col := premul(0, 0, 0, a)
		fillRoundRect(img, x-f, y-f+1.5*scale, w+2*f, h+2*f, radius+f, col)
	}
}

// fillRoundRect fills a rounded rectangle with the antialiased vector rasterizer. DrawOp is Over
// after Reset, so it composites rather than replaces.
func fillRoundRect(img *image.RGBA, x, y, w, h, r float64, col color.RGBA) {
	if w <= 0 || h <= 0 {
		return
	}
	if r > w/2 {
		r = w / 2
	}
	if r > h/2 {
		r = h / 2
	}
	var z vector.Rasterizer
	b := img.Bounds()
	z.Reset(b.Dx(), b.Dy())
	fx, fy, fw, fh, fr := float32(x), float32(y), float32(w), float32(h), float32(r)
	z.MoveTo(fx+fr, fy)
	z.LineTo(fx+fw-fr, fy)
	z.QuadTo(fx+fw, fy, fx+fw, fy+fr)
	z.LineTo(fx+fw, fy+fh-fr)
	z.QuadTo(fx+fw, fy+fh, fx+fw-fr, fy+fh)
	z.LineTo(fx+fr, fy+fh)
	z.QuadTo(fx, fy+fh, fx, fy+fh-fr)
	z.LineTo(fx, fy+fr)
	z.QuadTo(fx, fy, fx+fr, fy)
	z.ClosePath()
	z.Draw(img, b, image.NewUniform(col), image.Point{})
}

// drawText draws s with its baseline at (x, y).
func drawText(img *image.RGBA, face font.Face, col color.RGBA, x, y float64, s string) {
	d := &font.Drawer{
		Dst:  img,
		Src:  image.NewUniform(col),
		Face: face,
		Dot:  fixed.Point26_6{X: fixed.Int26_6(math.Round(x * 64)), Y: fixed.Int26_6(math.Round(y * 64))},
	}
	d.DrawString(s)
}

// baseline returns the drop from the top of a lineHeight-tall row to the text baseline, so a line
// is vertically centred in its row.
func baseline(face font.Face, lineHeight float64) float64 {
	m := face.Metrics()
	asc := float64(m.Ascent) / 64
	desc := float64(m.Descent) / 64
	return (lineHeight-(asc+desc))/2 + asc
}
