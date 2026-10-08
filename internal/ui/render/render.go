package render

import (
	"image"
	"image/color"
	"math"
	"strconv"
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
	// Status is the header line, e.g. "Listening…", "Phone offline", etc.
	Status string
	// Committed is the settled (cleaned) text, drawn in the near-white OnSurface colour.
	Committed string
	// Provisional is the live tail of the unit in flight, drawn in the muted OnSurfaceVariant.
	Provisional string
	// InsertedWords, when > 0, renders the success "Inserted X words" state.
	InsertedWords int
	// Error switches the header dot + label to the error colour.
	Error bool
	// Dark selects the dark palette (the default for a floating overlay).
	Dark bool
	// Preset selects the M3 accent color preset (default cyan).
	Preset string
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
	theme := ThemeForPreset(m.Preset, m.Dark)

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

	// Header label determination
	label := m.Status
	isInserted := m.InsertedWords > 0
	if isInserted {
		if m.InsertedWords == 1 {
			label = "Inserted 1 word"
		} else {
			label = "Inserted " + strconv.Itoa(m.InsertedWords) + " words"
		}
	} else if label == "" {
		if m.Error {
			label = "Dictation error"
		} else {
			label = "Listening…"
		}
	}

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

	totalW := int(math.Round(cardW))
	totalH := int(math.Round(cardH))

	img := image.NewRGBA(image.Rect(0, 0, totalW, totalH))
	cardX := 0.0
	cardY := 0.0

	// Outer border: highlight border on error/success
	borderColor := theme.Border
	if m.Error {
		borderColor = theme.Error
	} else if isInserted {
		borderColor = theme.Success
	}
	fillRoundRect(img, cardX, cardY, cardW, cardH, pillRadius*scale, borderColor)
	bw := pillBorder * scale
	fillRoundRect(img, cardX+bw, cardY+bw, cardW-2*bw, cardH-2*bw, pillRadius*scale-bw, theme.Surface)

	// Status icon / dot:
	dotX := cardX + innerPad
	dotY := cardY + innerPad + (statusH-statusDot*scale)/2

	var statusCol color.RGBA
	if m.Error {
		statusCol = theme.Error
		fillRoundRect(img, dotX, dotY, statusDot*scale, statusDot*scale, statusDot*scale/2, theme.Error)
	} else if isInserted {
		statusCol = theme.Success
		drawCheckmark(img, dotX+statusDot*scale/2, dotY+statusDot*scale/2, statusDot*scale*1.4, theme.Success)
	} else {
		// Active Listening: theme-colored pulsing halo + center dot
		statusCol = theme.Primary
		haloSz := statusDot * scale * 1.8
		haloCol := premul(theme.Primary.R, theme.Primary.G, theme.Primary.B, 0x40)
		fillRoundRect(img, dotX-(haloSz-statusDot*scale)/2, dotY-(haloSz-statusDot*scale)/2, haloSz, haloSz, haloSz/2, haloCol)
		fillRoundRect(img, dotX, dotY, statusDot*scale, statusDot*scale, statusDot*scale/2, theme.Primary)
	}

	statusBase := cardY + innerPad + baseline(statusFace, statusH)
	drawText(img, statusFace, statusCol, dotX+statusDot*scale+statusDotGap*scale, statusBase, label)

	// In listening mode, show a subtle "Esc" hint on the right edge from frame 0
	if !isInserted && !m.Error {
		escW := float64(font.MeasureString(statusFace, "Esc").Ceil())
		escX := cardX + cardW - innerPad - escW
		drawText(img, statusFace, theme.OnSurfaceVariant, escX, statusBase, "Esc")
	}

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
				col := w.col
				if isInserted {
					col = theme.OnSurfaceVariant // subtle muted tone confirming settled/inserted text
				}
				drawText(img, bodyFace, col, x, base, w.text)
				x += w.width + float64(font.MeasureString(bodyFace, " ").Ceil())
			}
		}
	}

	return Frame{Img: img, Pad: 0}, nil
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
	text       string
	col        color.RGBA
	width      float64
	strike     bool
	glueToNext bool
}

// wrap flows the spans into lines that fit maxWidth, keeping each word's colour.
func wrap(face font.Face, spans []span, maxWidth float64) [][]word {
	if maxWidth <= 0 {
		return nil
	}
	var words []word
	for _, sp := range spans {
		for _, tok := range tokenize(sp.text) {
			if tok == "\n" {
				words = append(words, word{text: "\n"})
				continue
			}
			w := float64(font.MeasureString(face, tok).Ceil())
			words = append(words, word{text: tok, col: sp.col, width: w})
		}
	}
	return wrapWords(face, words, maxWidth)
}

// wrapWords flows words into lines up to maxWidth, preserving glued clusters together on the same line.
func wrapWords(face font.Face, words []word, maxWidth float64) [][]word {
	if maxWidth <= 0 || len(words) == 0 {
		return nil
	}
	spaceW := float64(font.MeasureString(face, " ").Ceil())

	// Group contiguous words connected by glueToNext into unbreakable clusters
	type cluster struct {
		words []word
		width float64
	}
	var clusters []cluster
	var curCluster []word
	curClusterW := 0.0

	for _, w := range words {
		if w.text == "\n" {
			if len(curCluster) > 0 {
				clusters = append(clusters, cluster{words: curCluster, width: curClusterW})
				curCluster = nil
				curClusterW = 0
			}
			clusters = append(clusters, cluster{words: []word{w}, width: 0})
			continue
		}
		curCluster = append(curCluster, w)
		curClusterW += w.width
		if !w.glueToNext {
			clusters = append(clusters, cluster{words: curCluster, width: curClusterW})
			curCluster = nil
			curClusterW = 0
		}
	}
	if len(curCluster) > 0 {
		clusters = append(clusters, cluster{words: curCluster, width: curClusterW})
	}

	var lines [][]word
	var curLine []word
	curLineW := 0.0

	flush := func() {
		if len(curLine) > 0 {
			lines = append(lines, curLine)
		}
		curLine = nil
		curLineW = 0
	}

	for _, cl := range clusters {
		if len(cl.words) == 1 && cl.words[0].text == "\n" {
			flush()
			continue
		}
		if len(curLine) > 0 {
			if curLineW+spaceW+cl.width > maxWidth {
				flush()
			} else {
				curLineW += spaceW
			}
		}
		curLine = append(curLine, cl.words...)
		curLineW += cl.width
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

// fillRoundRect fills an anti-aliased rounded rectangle by compositing col with per-pixel coverage
// from a signed-distance field. It touches only the rect's bounding box. The previous vector-based
// version reset its rasterizer to the whole image and scanned every pixel for every fill, which
// dominated a panel repaint; this is the hot path, so it stays allocation-free and fast-paths the
// flat interior (coverage 1, no square root, and an opaque fill is a direct store).
func fillRoundRect(img *image.RGBA, x, y, w, h, r float64, col color.RGBA) {
	if w <= 0 || h <= 0 || col.A == 0 {
		return
	}
	if r > w/2 {
		r = w / 2
	}
	if r > h/2 {
		r = h / 2
	}
	x0, y0, x1, y1 := roundedRectBounds(img.Bounds(), x, y, w, h)
	// The inner rectangle whose pixels are entirely inside the perimeter, so coverage is 1 there.
	ix0, iy0, ix1, iy1 := roundRectCore(x0, y0, x1, y1, x, y, w, h, r)
	opaque := col.A == 0xFF
	for py := y0; py < y1; py++ {
		coreRow := py >= iy0 && py < iy1
		for px := x0; px < x1; px++ {
			if coreRow && px >= ix0 && px < ix1 {
				off := img.PixOffset(px, py)
				if opaque {
					img.Pix[off] = col.R
					img.Pix[off+1] = col.G
					img.Pix[off+2] = col.B
					img.Pix[off+3] = 0xFF
				} else {
					blendPixel(img, px, py, col, 1)
				}
				continue
			}
			if cov := roundedRectCoverage(px, py, x, y, w, h, r); cov > 0 {
				blendPixel(img, px, py, col, cov)
			}
		}
	}
}

// roundedRectBounds returns the integer pixel box a rect can affect, expanded by 1px for the
// anti-aliased edge and clipped to img.
func roundedRectBounds(img image.Rectangle, x, y, w, h float64) (x0, y0, x1, y1 int) {
	x0 = maxInt(int(math.Floor(x))-1, img.Min.X)
	y0 = maxInt(int(math.Floor(y))-1, img.Min.Y)
	x1 = minInt(int(math.Ceil(x+w))+1, img.Max.X)
	y1 = minInt(int(math.Ceil(y+h))+1, img.Max.Y)
	return
}

// roundRectCore returns the inner rectangle of a rounded rect whose pixels all have coverage 1, so
// callers can fill it without the signed-distance edge math.
func roundRectCore(x0, y0, x1, y1 int, x, y, w, h, r float64) (ix0, iy0, ix1, iy1 int) {
	ix0 = maxInt(int(math.Ceil(x+r)), x0)
	iy0 = maxInt(int(math.Ceil(y+r)), y0)
	ix1 = minInt(int(math.Floor(x+w-r)), x1)
	iy1 = minInt(int(math.Floor(y+h-r)), y1)
	return
}

// roundedRectCoverage returns the anti-aliased coverage of the pixel centre (px+0.5, py+0.5):
// 1 fully inside, 0 fully outside, a linear ramp across the 1px edge.
func roundedRectCoverage(px, py int, x, y, w, h, r float64) float64 {
	cx := x + w/2
	cy := y + h/2
	qx := math.Abs(float64(px)+0.5-cx) - (w/2 - r)
	qy := math.Abs(float64(py)+0.5-cy) - (h/2 - r)
	ax, ay := math.Max(qx, 0), math.Max(qy, 0)
	d := math.Sqrt(ax*ax+ay*ay) + math.Min(math.Max(qx, qy), 0) - r
	cov := 0.5 - d
	switch {
	case cov <= 0:
		return 0
	case cov >= 1:
		return 1
	default:
		return cov
	}
}

// blendPixel composites a premultiplied colour over the destination pixel, scaling its alpha by cov.
// It uses fixed-point arithmetic: this runs once per pixel per fill, so it avoids float conversions.
func blendPixel(img *image.RGBA, px, py int, col color.RGBA, cov float64) {
	c := uint32(cov*255 + 0.5)
	if c == 0 {
		return
	}
	if c > 255 {
		c = 255
	}
	off := img.PixOffset(px, py)
	srcR := uint32(col.R) * c / 255
	srcG := uint32(col.G) * c / 255
	srcB := uint32(col.B) * c / 255
	srcA := uint32(col.A) * c / 255
	k := 255 - srcA
	img.Pix[off] = uint8(srcR + uint32(img.Pix[off])*k/255)
	img.Pix[off+1] = uint8(srcG + uint32(img.Pix[off+1])*k/255)
	img.Pix[off+2] = uint8(srcB + uint32(img.Pix[off+2])*k/255)
	img.Pix[off+3] = uint8(srcA + uint32(img.Pix[off+3])*k/255)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// fillPathUniform rasterizes the path built by build into img, bounded to bbox (clipped to img).
// build receives the bbox origin to subtract from image coordinates, so the path lands in the
// rasterizer's local space. Bounding the mask keeps a small glyph (a chevron, a checkmark, the logo
// stencil) from allocating and scanning a full-image buffer, which dominated panel repaints.
func fillPathUniform(img *image.RGBA, bbox image.Rectangle, col color.RGBA, build func(z *vector.Rasterizer, ox, oy float64)) {
	if col.A == 0 {
		return
	}
	bbox = bbox.Intersect(img.Bounds())
	if bbox.Empty() {
		return
	}
	var z vector.Rasterizer
	z.Reset(bbox.Dx(), bbox.Dy())
	build(&z, float64(bbox.Min.X), float64(bbox.Min.Y))
	z.Draw(img, bbox, image.NewUniform(col), image.Point{})
}

// drawText draws s with its baseline at (x, y).
// drawText draws s with its baseline at (x, y) using gamma-corrected linear-light antialiasing.
func drawText(img *image.RGBA, face font.Face, col color.RGBA, x, y float64, s string) {
	dot := fixed.Point26_6{
		X: fixed.Int26_6(math.Round(x * 64)),
		Y: fixed.Int26_6(math.Round(y * 64)),
	}
	drawTextGamma(img, face, col, dot, s)
}

// drawTextEmboldened draws s with its baseline at (x, y) with synthesized Medium/SemiBold weight.
func drawTextEmboldened(img *image.RGBA, face font.Face, col color.RGBA, x, y float64, s string) {
	dot := fixed.Point26_6{
		X: fixed.Int26_6(math.Round(x * 64)),
		Y: fixed.Int26_6(math.Round(y * 64)),
	}
	drawTextBold(img, face, col, dot, s)
}

// baseline returns the drop from the top of a lineHeight-tall row to the text baseline, so a line
// is vertically centred in its row.
func baseline(face font.Face, lineHeight float64) float64 {
	m := face.Metrics()
	asc := float64(m.Ascent) / 64
	desc := float64(m.Descent) / 64
	return (lineHeight-(asc+desc))/2 + asc
}
