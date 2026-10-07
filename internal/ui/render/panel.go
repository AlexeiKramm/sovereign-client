package render

import (
	"image"
	"image/color"
	"math"
	"strings"

	"golang.org/x/image/font"
)

// Logical (96-DPI) metrics for the control window.
const (
	panelWidth   = 380.0
	panelPadding = 16.0
	panelRadius  = 16.0
	panelBorder  = 1.5

	panelHeaderH = 30.0
	panelClose   = 26.0
	panelTitleSz = 15.0

	panelStatusSz = 13.0
	panelStatusH  = 20.0
	panelDot      = 8.0
	panelDotGap   = 8.0

	panelLiveSz  = 14.5
	panelLiveLH  = 19.0
	panelLiveH   = 150.0
	panelLiveMax = 7 // wrapped lines shown before the oldest are dropped

	panelInfoSz = 12.5
	panelInfoLH = 17.0
)

// ControlKind identifies an interactive region of the panel.
type ControlKind int

const (
	// ControlClose is the window's close affordance.
	ControlClose ControlKind = iota
	// ControlButton is a generic pressable row/button (settings arrive in 4d-b).
	ControlButton
	// ControlToggle is a boolean row.
	ControlToggle
)

// Control is one interactive region, in the panel image's client coordinates (device pixels,
// including the transparent shadow margin), so a mouse event maps to it directly.
type Control struct {
	ID      string
	Kind    ControlKind
	Rect    image.Rectangle
	Label   string
	Checked bool
}

// PanelModel is the control window's content.
type PanelModel struct {
	Title       string
	Status      string
	Committed   string
	Provisional string
	Info        []string
	Error       bool
	Dark        bool
	Scale       float64
}

// Panel is a rendered control window plus its interactive regions and draggable header.
type Panel struct {
	Frame    Frame
	Controls []Control
	Header   image.Rectangle
}

// RenderPanel draws the control window. It reuses the pill's drawing primitives and text flow, so
// the two surfaces look like one product.
func RenderPanel(m PanelModel) (Panel, error) {
	faceMu.Lock()
	defer faceMu.Unlock()

	scale := m.Scale
	if scale <= 0 {
		scale = 1
	}
	theme := ThemeFor(m.Dark)

	titleFace, err := newFace(panelTitleSz * scale)
	if err != nil {
		return Panel{}, err
	}
	defer titleFace.Close()
	statusFace, err := newFace(panelStatusSz * scale)
	if err != nil {
		return Panel{}, err
	}
	defer statusFace.Close()
	bodyFace, err := newFace(panelLiveSz * scale)
	if err != nil {
		return Panel{}, err
	}
	defer bodyFace.Close()
	infoFace, err := newFace(panelInfoSz * scale)
	if err != nil {
		return Panel{}, err
	}
	defer infoFace.Close()

	pad := panelPadding * scale
	headerH := panelHeaderH * scale
	statusH := panelStatusH * scale
	liveH := panelLiveH * scale
	infoLH := panelInfoLH * scale
	closeSz := panelClose * scale

	cardW := panelWidth * scale
	innerW := cardW - 2*pad

	liveLines := wrap(bodyFace, bodySpans(m.Committed, m.Provisional, theme), innerW-2*10*scale)
	if len(liveLines) > panelLiveMax {
		liveLines = liveLines[len(liveLines)-panelLiveMax:]
	}
	info := trimInfo(m.Info)

	cardH := pad + headerH + 6*scale + 1 + 10*scale + statusH + 10*scale + liveH + 12*scale
	if n := len(info); n > 0 {
		cardH += float64(n)*infoLH + 4*scale
	}
	cardH += pad

	shadow := int(math.Ceil(shadowSpread * scale))
	totalW := int(math.Round(cardW)) + 2*shadow
	totalH := int(math.Round(cardH)) + 2*shadow
	img := image.NewRGBA(image.Rect(0, 0, totalW, totalH))

	cx, cy := float64(shadow), float64(shadow)
	drawShadow(img, cx, cy, cardW, cardH, panelRadius*scale, shadow, scale, theme)
	fillRoundRect(img, cx, cy, cardW, cardH, panelRadius*scale, theme.Border)
	bw := panelBorder * scale
	fillRoundRect(img, cx+bw, cy+bw, cardW-2*bw, cardH-2*bw, panelRadius*scale-bw, theme.Surface)

	controls := make([]Control, 0, 2)
	header := image.Rect(shadow, shadow, shadow+int(math.Round(cardW)), shadow+int(math.Round(headerH)))

	// Header: title on the left, close affordance on the right.
	titleY := cy + (headerH-float64(titleFace.Metrics().Height)/64)/2 + float64(titleFace.Metrics().Ascent)/64
	drawText(img, titleFace, theme.OnSurface, cx+pad, titleY, m.Title)

	closeRect := image.Rect(
		shadow+int(math.Round(cardW-pad-closeSz)),
		shadow+int(math.Round((headerH-closeSz)/2)),
		shadow+int(math.Round(cardW-pad)),
		shadow+int(math.Round((headerH+closeSz)/2)),
	)
	drawClose(img, closeRect, theme.OnSurfaceVariant, scale)
	controls = append(controls, Control{ID: "close", Kind: ControlClose, Rect: closeRect})

	// Separator under the header.
	sepY := cy + headerH + 6*scale
	fillRoundRect(img, cx+pad, sepY, innerW, 1, 0, theme.Border)

	// Status row: dot + label.
	statusTop := sepY + 1 + 10*scale
	dotCol := theme.Primary
	if m.Error {
		dotCol = theme.Error
	}
	fillRoundRect(img, cx+pad, statusTop+(statusH-panelDot*scale)/2, panelDot*scale, panelDot*scale, panelDot*scale/2, dotCol)
	statusCol := theme.OnSurfaceVariant
	if m.Error {
		statusCol = theme.Error
	}
	drawText(img, statusFace, statusCol,
		cx+pad+panelDot*scale+panelDotGap*scale,
		statusTop+baseline(statusFace, statusH), m.Status)

	// Live text box.
	liveTop := statusTop + statusH + 10*scale
	fillRoundRect(img, cx+pad, liveTop, innerW, liveH, 10*scale, theme.Inset)
	if len(liveLines) > 0 {
		asc := float64(bodyFace.Metrics().Ascent) / 64
		space := float64(font.MeasureString(bodyFace, " ").Ceil())
		x0 := cx + pad + 10*scale
		for i, line := range liveLines {
			base := liveTop + 8*scale + float64(i)*panelLiveLH*scale + asc
			x := x0
			for _, w := range line {
				drawText(img, bodyFace, w.col, x, base, w.text)
				x += w.width + space
			}
		}
	}

	// Footer info lines.
	infoTop := liveTop + liveH + 12*scale
	for i, line := range m.Info {
		drawText(img, infoFace, theme.OnSurfaceVariant,
			cx+pad, infoTop+float64(i)*infoLH+baseline(infoFace, infoLH), line)
	}

	return Panel{Frame: Frame{Img: img, Pad: shadow}, Controls: controls, Header: header}, nil
}

// drawClose draws an "×" centred in r.
func drawClose(img *image.RGBA, r image.Rectangle, col color.RGBA, scale float64) {
	face, err := newFace(15 * scale)
	if err != nil {
		return
	}
	defer face.Close()
	w := float64(font.MeasureString(face, "×").Ceil())
	h := float64(face.Metrics().Height) / 64
	x := float64(r.Min.X) + (float64(r.Dx())-w)/2
	y := float64(r.Min.Y) + (float64(r.Dy())-h)/2 + float64(face.Metrics().Ascent)/64
	drawText(img, face, col, x, y, "×")
}

// ControlAt returns the control whose rect contains the client point, if any.
func (p Panel) ControlAt(x, y int) (Control, bool) {
	for _, c := range p.Controls {
		if x >= c.Rect.Min.X && x < c.Rect.Max.X && y >= c.Rect.Min.Y && y < c.Rect.Max.Y {
			return c, true
		}
	}
	return Control{}, false
}

// InHeader reports whether the client point is in the draggable header band (and not on a control).
func (p Panel) InHeader(x, y int) bool {
	if _, ok := p.ControlAt(x, y); ok {
		return false
	}
	return x >= p.Header.Min.X && x < p.Header.Max.X && y >= p.Header.Min.Y && y < p.Header.Max.Y
}

// trimInfo drops blank info lines so the footer never reserves space it does not use.
func trimInfo(lines []string) []string {
	out := lines[:0:0]
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}
