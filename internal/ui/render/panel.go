package render

import (
	"image"
	"image/color"
	"math"

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

	panelLiveSz  = 14.0
	panelLiveLH  = 18.0
	panelLiveH   = 96.0
	panelLiveMax = 5

	rowLabelSz  = 11.5
	rowLabelH   = 20.0
	rowChipsH   = 26.0
	rowChipSz   = 12.0
	rowChipPadX = 11.0
	rowChipGap  = 6.0
	rowTextSz   = 13.5
	rowToggleH  = 30.0
	rowSwitchW  = 34.0
	rowSwitchH  = 18.0
	rowButtonH  = 34.0
	rowGap      = 4.0

	panelInfoSz = 12.0
	panelInfoLH = 16.0
)

// ControlKind identifies an interactive region of the panel.
type ControlKind int

const (
	// ControlClose is the window's close affordance.
	ControlClose ControlKind = iota
	// ControlChip is one option of a row group.
	ControlChip
	// ControlToggle is a boolean row.
	ControlToggle
	// ControlButton is a pressable row.
	ControlButton
)

// Control is one interactive region, in the panel image's client coordinates (device pixels,
// including the transparent shadow margin), so a mouse event maps to it directly.
type Control struct {
	ID   string
	Kind ControlKind
	Rect image.Rectangle
}

// RowKind is the kind of settings row.
type RowKind int

const (
	// RowChips is a labelled group of mutually exclusive options.
	RowChips RowKind = iota
	// RowToggle is a boolean switch.
	RowToggle
	// RowButton is a pressable row.
	RowButton
)

// Option is one chip in a RowChips group.
type Option struct {
	ID   string
	Text string
	On   bool
}

// Row is one settings row.
type Row struct {
	Kind    RowKind
	ID      string   // RowToggle/RowButton action id
	Text    string   // group label (RowChips) or button text (RowButton)
	Detail  string   // right-aligned value (RowButton)
	Options []Option // RowChips
	On      bool     // RowToggle
}

// PanelModel is the control window's content.
type PanelModel struct {
	Title       string
	Status      string
	Committed   string
	Provisional string
	Rows        []Row
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
// the two surfaces look like one product, and returns every interactive region so the controller
// hit-tests exactly what it drew.
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
	labelFace, err := newFace(rowLabelSz * scale)
	if err != nil {
		return Panel{}, err
	}
	defer labelFace.Close()
	chipFace, err := newFace(rowChipSz * scale)
	if err != nil {
		return Panel{}, err
	}
	defer chipFace.Close()
	rowFace, err := newFace(rowTextSz * scale)
	if err != nil {
		return Panel{}, err
	}
	defer rowFace.Close()
	infoFace, err := newFace(panelInfoSz * scale)
	if err != nil {
		return Panel{}, err
	}
	defer infoFace.Close()

	pad := panelPadding * scale
	headerH := panelHeaderH * scale
	statusH := panelStatusH * scale
	liveH := panelLiveH * scale
	closeSz := panelClose * scale
	cardW := panelWidth * scale
	innerW := cardW - 2*pad

	liveLines := wrap(bodyFace, bodySpans(m.Committed, m.Provisional, theme), innerW-2*10*scale)
	if len(liveLines) > panelLiveMax {
		liveLines = liveLines[len(liveLines)-panelLiveMax:]
	}
	info := trimInfo(m.Info)

	// Measure: rows are a fixed height per kind, so the card can be sized before drawing.
	rowH := make([]float64, len(m.Rows))
	for i, r := range m.Rows {
		rowH[i] = measureRow(r, scale)
	}
	cardH := pad + headerH + 6*scale + 1 + 10*scale + statusH + 10*scale + liveH + 12*scale
	for _, h := range rowH {
		cardH += h + rowGap*scale
	}
	if len(info) > 0 {
		cardH += 12*scale + float64(len(info))*panelInfoLH*scale + 4*scale
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

	controls := make([]Control, 0, 12)
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

	// Settings rows.
	y := liveTop + liveH + 12*scale
	left := cx + pad
	for i, r := range m.Rows {
		controls = append(controls, drawRow(img, r, left, y, innerW, rowH[i], shadow, scale, theme, labelFace, chipFace, rowFace)...)
		y += rowH[i] + rowGap*scale
	}

	// Footer info lines.
	infoTop := y + 12*scale
	for i, line := range info {
		drawText(img, infoFace, theme.OnSurfaceVariant,
			left, infoTop+float64(i)*panelInfoLH*scale+baseline(infoFace, panelInfoLH*scale), line)
	}

	return Panel{Frame: Frame{Img: img, Pad: shadow}, Controls: controls, Header: header}, nil
}

// measureRow returns the logical height of a row, scaled.
func measureRow(r Row, scale float64) float64 {
	switch r.Kind {
	case RowChips:
		return (rowLabelH + rowChipsH) * scale
	case RowToggle:
		return rowToggleH * scale
	default: // RowButton
		return rowButtonH * scale
	}
}

// drawRow draws one settings row and returns its interactive regions (in image coordinates).
func drawRow(img *image.RGBA, r Row, x, y, w, h float64, shadow int, scale float64, theme Theme,
	labelFace, chipFace, rowFace font.Face) []Control {

	switch r.Kind {
	case RowChips:
		drawText(img, labelFace, theme.OnSurfaceVariant, x, y+baseline(labelFace, rowLabelH*scale), r.Text)
		chipY := y + rowLabelH*scale
		chH := rowChipsH * scale
		cx := x
		var out []Control
		for _, opt := range r.Options {
			cw := float64(font.MeasureString(chipFace, opt.Text).Ceil()) + 2*rowChipPadX*scale
			rect := image.Rect(
				int(math.Round(cx)), int(math.Round(chipY)),
				int(math.Round(cx+cw)), int(math.Round(chipY+chH)),
			)
			if opt.On {
				fillRoundRect(img, cx, chipY, cw, chH, chH/2, theme.Primary)
				drawText(img, chipFace, theme.OnPrimary,
					cx+rowChipPadX*scale, chipY+baseline(chipFace, chH), opt.Text)
			} else {
				fillRoundRect(img, cx, chipY, cw, chH, chH/2, theme.Border)
				inner := 1.0 * scale
				fillRoundRect(img, cx+inner, chipY+inner, cw-2*inner, chH-2*inner, chH/2-inner, theme.Inset)
				drawText(img, chipFace, theme.OnSurfaceVariant,
					cx+rowChipPadX*scale, chipY+baseline(chipFace, chH), opt.Text)
			}
			out = append(out, Control{ID: opt.ID, Kind: ControlChip, Rect: rect})
			cx += cw + rowChipGap*scale
		}
		return out

	case RowToggle:
		drawText(img, rowFace, theme.OnSurface, x, y+baseline(rowFace, h), r.Text)
		swW, swH := rowSwitchW*scale, rowSwitchH*scale
		sx := x + w - swW
		sy := y + (h-swH)/2
		track := theme.Border
		knob := theme.OnSurfaceVariant
		if r.On {
			track = theme.Primary
			knob = theme.OnPrimary
		}
		fillRoundRect(img, sx, sy, swW, swH, swH/2, track)
		kr := (swH - 4*scale) / 2
		kxc := sx + 2*scale + kr
		if r.On {
			kxc = sx + swW - 2*scale - kr
		}
		fillRoundRect(img, kxc-kr, sy+2*scale, kr*2, kr*2, kr, knob)
		return []Control{{ID: r.ID, Kind: ControlToggle,
			Rect: image.Rect(int(math.Round(x)), int(math.Round(y)), int(math.Round(x+w)), int(math.Round(y+h)))}}

	default: // RowButton
		fillRoundRect(img, x, y, w, h, 10*scale, theme.Border)
		inner := 1.0 * scale
		fillRoundRect(img, x+inner, y+inner, w-2*inner, h-2*inner, 10*scale-inner, theme.Inset)
		drawText(img, rowFace, theme.OnSurface, x+12*scale, y+baseline(rowFace, h), r.Text)
		if r.Detail != "" {
			dw := float64(font.MeasureString(rowFace, r.Detail).Ceil())
			drawText(img, rowFace, theme.OnSurfaceVariant, x+w-12*scale-dw, y+baseline(rowFace, h), r.Detail)
		}
		return []Control{{ID: r.ID, Kind: ControlButton,
			Rect: image.Rect(int(math.Round(x)), int(math.Round(y)), int(math.Round(x+w)), int(math.Round(y+h)))}}
	}
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
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}
