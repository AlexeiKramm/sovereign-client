package render

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/vector"
)

// Logical (96-DPI) metrics for the control window.
const (
	panelWidth   = 390.0
	panelPadding = 16.0
	panelRadius  = 16.0
	panelBorder  = 1.5

	panelHeaderH = 32.0
	panelClose   = 26.0
	panelTitleSz = 16.0

	panelStatusSz = 13.5
	panelStatusH  = 20.0
	panelDot      = 8.0
	panelDotGap   = 8.0

	panelTabH = 32.0

	panelLiveSz  = 14.5
	panelLiveLH  = 19.0
	panelLiveH   = 80.0
	panelLiveMax = 4

	rowLabelSz  = 13.0
	rowLabelH   = 22.0
	rowChipsH   = 28.0
	rowChipSz   = 13.0
	rowChipPadX = 12.0
	rowChipGap  = 6.0
	rowTextSz   = 14.0
	rowToggleH  = 32.0
	rowSwitchW  = 36.0
	rowSwitchH  = 20.0
	rowButtonH  = 36.0
	rowGap      = 5.0

	panelInfoSz = 12.0
	panelInfoLH = 16.0
)

// Panel tab identifiers.
const (
	TabDictations = 0
	TabSettings   = 1
)

// PanelHistoryItem represents one dictation entry rendered in the Dictations tab.
type PanelHistoryItem struct {
	ID        string
	TimeAgo   string
	CleanText string
	RawText   string
	Words     int
	AsrMs     int64
	CleanupMs int64
	ViewMode  string // "clean", "raw", "diff"
}

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
	// RowDropdown is an expandable dropdown selection row.
	RowDropdown
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
	ActiveTab   int
	History     []PanelHistoryItem
	CopiedID    string
	Rows        []Row
	Info        []string
	Version     string
	Error       bool
	Dark        bool
	Preset      string
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
	theme := ThemeForPreset(m.Preset, m.Dark)

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
	smallFace, err := newFace(11.5 * scale)
	if err != nil {
		return Panel{}, err
	}
	defer smallFace.Close()

	pad := panelPadding * scale
	headerH := panelHeaderH * scale
	statusH := panelStatusH * scale
	tabH := panelTabH * scale
	closeSz := panelClose * scale
	cardW := panelWidth * scale
	innerW := cardW - 2*pad

	// Measure content area based on active tab
	contentH := 0.0
	var histH []float64
	var rowH []float64

	if m.ActiveTab == TabDictations {
		if len(m.History) == 0 {
			contentH = 76.0 * scale
		} else {
			histH = make([]float64, len(m.History))
			for i, item := range m.History {
				histH[i] = measureHistoryItem(item, innerW, scale, theme, bodyFace)
				contentH += histH[i] + 8.0*scale
			}
		}
	} else {
		// TabSettings
		rowH = make([]float64, len(m.Rows))
		for i, r := range m.Rows {
			rowH[i] = measureRow(r, innerW, scale, chipFace)
			contentH += rowH[i] + rowGap*scale
		}
	}

	hasFooter := m.Version != ""
	footerH := 0.0
	if hasFooter {
		footerH = 1 + 6*scale + panelInfoLH*scale + 6*scale
	}

	cardH := headerH + 6*scale + 1 + 8*scale + statusH + 8*scale + tabH + 12*scale + contentH
	if hasFooter {
		cardH += footerH
	} else {
		cardH += 6 * scale
	}

	totalW := int(math.Round(cardW))
	totalH := int(math.Round(cardH))
	img := image.NewRGBA(image.Rect(0, 0, totalW, totalH))

	cx, cy := 0.0, 0.0
	fillRoundRect(img, cx, cy, cardW, cardH, panelRadius*scale, theme.Border)
	bw := panelBorder * scale
	fillRoundRect(img, cx+bw, cy+bw, cardW-2*bw, cardH-2*bw, panelRadius*scale-bw, theme.Surface)

	controls := make([]Control, 0, 16)
	header := image.Rect(0, 0, totalW, int(math.Round(headerH)))

	// Header: Sovereign brand mark + title on the left, close affordance on the right.
	logoSz := 20.0 * scale
	logoX := cx + pad
	logoY := cy + (headerH-logoSz)/2
	DrawSovereignLogo(img, logoX, logoY, logoSz)

	titleX := logoX + logoSz + 8.0*scale
	titleY := cy + (headerH-float64(titleFace.Metrics().Height)/64)/2 + float64(titleFace.Metrics().Ascent)/64
	drawTextEmboldened(img, titleFace, theme.OnSurface, titleX, titleY, m.Title)

	closeRect := image.Rect(
		int(math.Round(cardW-pad-closeSz)),
		int(math.Round((headerH-closeSz)/2)),
		int(math.Round(cardW-pad)),
		int(math.Round((headerH+closeSz)/2)),
	)
	drawClose(img, closeRect, theme.OnSurfaceVariant, scale)
	controls = append(controls, Control{ID: "close", Kind: ControlClose, Rect: closeRect})

	// Separator under the header.
	sepY := cy + headerH + 6*scale
	fillRoundRect(img, cx+pad, sepY, innerW, 1, 0, theme.Border)

	// Status row: dot + label.
	statusTop := sepY + 1 + 8*scale
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

	curY := statusTop + statusH + 8*scale

	// Segmented Tab Bar: [ Dictations (N) ] and [ Settings ]
	controls = append(controls, drawTabBar(img, m.ActiveTab, cx+pad, curY, innerW, tabH, scale, theme, len(m.History), chipFace)...)
	curY += tabH + 12*scale

	// Tab content
	left := cx + pad
	if m.ActiveTab == TabDictations {
		if len(m.History) == 0 {
			drawEmptyHistory(img, left, curY, innerW, 76.0*scale, scale, theme, rowFace, smallFace)
			curY += 76.0 * scale
		} else {
			for i, item := range m.History {
				isCopied := m.CopiedID == item.ID
				controls = append(controls, drawHistoryItem(img, item, left, curY, innerW, histH[i], scale, theme, isCopied, bodyFace, chipFace, smallFace)...)
				curY += histH[i] + 8.0*scale
			}
		}
	} else {
		// TabSettings
		for i, r := range m.Rows {
			controls = append(controls, drawRow(img, r, left, curY, innerW, rowH[i], 0, scale, theme, labelFace, chipFace, rowFace)...)
			curY += rowH[i] + rowGap*scale
		}
	}

	if hasFooter {
		// Footer Separator
		fillRoundRect(img, cx+pad, curY, innerW, 1, 0, theme.Border)
		curY += 1.0 + 6.0*scale

		// Footer: Version only (right-aligned, close to bottom)
		if m.Version != "" {
			vw := float64(font.MeasureString(smallFace, m.Version).Ceil())
			vx := cx + pad + innerW - vw
			drawText(img, smallFace, theme.OnSurfaceVariant, vx, curY+baseline(smallFace, panelInfoLH*scale), m.Version)
		}
	}

	return Panel{Frame: Frame{Img: img, Pad: 0}, Controls: controls, Header: header}, nil
}

// historyItemWords builds the styled words for a history card based on its ViewMode (Clean, Raw, Diff).
func historyItemWords(face font.Face, item PanelHistoryItem, theme Theme) []word {
	switch item.ViewMode {
	case "raw":
		t := strings.TrimSpace(item.RawText)
		if t == "" {
			t = strings.TrimSpace(item.CleanText)
		}
		if t == "" {
			t = "—"
		}
		return textToWords(face, t, theme.OnSurfaceVariant)

	case "diff":
		raw := strings.TrimSpace(item.RawText)
		clean := strings.TrimSpace(item.CleanText)
		if raw == "" && clean == "" {
			return textToWords(face, "—", theme.OnSurfaceVariant)
		}
		diff := ComputeInlineDiff(raw, clean)
		if !diff.HasEdits {
			t := clean
			if t == "" {
				t = raw
			}
			return textToWords(face, t, theme.OnSurface)
		}
		return DiffWords(face, diff, theme)

	default: // "clean"
		t := strings.TrimSpace(item.CleanText)
		if t == "" {
			t = strings.TrimSpace(item.RawText)
		}
		if t == "" {
			t = "—"
		}
		return textToWords(face, t, theme.OnSurface)
	}
}

func textToWords(face font.Face, text string, col color.RGBA) []word {
	var words []word
	for _, tok := range tokenize(text) {
		if tok == "\n" {
			words = append(words, word{text: "\n"})
			continue
		}
		w := float64(font.MeasureString(face, tok).Ceil())
		words = append(words, word{text: tok, col: col, width: w})
	}
	return words
}

// measureHistoryItem computes the rendered height of one dictation history card.
func measureHistoryItem(item PanelHistoryItem, maxWidth, scale float64, theme Theme, bodyFace font.Face) float64 {
	innerPad := 12.0 * scale
	topH := 22.0 * scale
	botH := 16.0 * scale
	words := historyItemWords(bodyFace, item, theme)
	lines := wrapWords(bodyFace, words, maxWidth-2*innerPad)
	if len(lines) > 4 {
		lines = lines[:4]
	}
	if len(lines) == 0 {
		lines = [][]word{{}}
	}
	lineH := 18.0 * scale
	return 2*innerPad + topH + 8.0*scale + float64(len(lines))*lineH + 8.0*scale + botH
}

// drawHistoryItem draws one elevated card with timestamp, view toggle, copy button, text, and telemetry.
func drawHistoryItem(img *image.RGBA, item PanelHistoryItem, x, y, w, h float64, scale float64, theme Theme, isCopied bool,
	bodyFace, chipFace, smallFace font.Face) []Control {

	var out []Control
	rad := 12.0 * scale
	innerPad := 12.0 * scale

	// Elevated card surface (lighter than window canvas in dark mode, pure white in light mode!)
	fillRoundRect(img, x, y, w, h, rad, theme.CardBorder)
	bw := 1.0 * scale
	fillRoundRect(img, x+bw, y+bw, w-2*bw, h-2*bw, rad-bw, theme.Card)

	// Top bar: relative time on left, mode toggle + copy button on right
	topY := y + innerPad
	topH := 22.0 * scale
	timeText := item.TimeAgo
	if timeText == "" {
		timeText = "Just now"
	}
	drawText(img, smallFace, theme.OnSurfaceVariant, x+innerPad, topY+baseline(smallFace, topH), timeText)

	// Right buttons: Copy and ViewMode
	btnH := 22.0 * scale
	btnY := topY

	// Copy button
	copyText := "Copy"
	copyCol := theme.OnSurface
	copyBg := theme.Inset
	if isCopied {
		copyText = "Copied!"
		copyCol = theme.Success
	}
	copyW := float64(font.MeasureString(smallFace, copyText).Ceil()) + 16.0*scale
	copyX := x + w - innerPad - copyW
	fillRoundRect(img, copyX, btnY, copyW, btnH, 6.0*scale, theme.CardBorder)
	fillRoundRect(img, copyX+bw, btnY+bw, copyW-2*bw, btnH-2*bw, 6.0*scale-bw, copyBg)
	drawText(img, smallFace, copyCol, copyX+8.0*scale, btnY+baseline(smallFace, btnH), copyText)
	out = append(out, Control{ID: "copy:" + item.ID, Kind: ControlButton, Rect: image.Rect(
		int(math.Round(copyX)), int(math.Round(btnY)),
		int(math.Round(copyX+copyW)), int(math.Round(btnY+btnH)),
	)})

	// Mode toggle button (Clean / Raw / Diff)
	modeText := "Clean"
	modeCol := theme.OnSurfaceVariant
	if item.ViewMode == "raw" {
		modeText = "Raw"
	} else if item.ViewMode == "diff" {
		modeText = "Diff"
		modeCol = theme.Primary
	}
	modeW := float64(font.MeasureString(smallFace, modeText).Ceil()) + 14.0*scale
	modeX := copyX - 6.0*scale - modeW
	fillRoundRect(img, modeX, btnY, modeW, btnH, 6.0*scale, theme.CardBorder)
	fillRoundRect(img, modeX+bw, btnY+bw, modeW-2*bw, btnH-2*bw, 6.0*scale-bw, theme.Inset)
	drawText(img, smallFace, modeCol, modeX+7.0*scale, btnY+baseline(smallFace, btnH), modeText)
	out = append(out, Control{ID: "histmode:" + item.ID, Kind: ControlButton, Rect: image.Rect(
		int(math.Round(modeX)), int(math.Round(btnY)),
		int(math.Round(modeX+modeW)), int(math.Round(btnY+btnH)),
	)})

	// Body text with inline diff / strike-through support:
	bodyTop := topY + topH + 8.0*scale
	words := historyItemWords(bodyFace, item, theme)
	lines := wrapWords(bodyFace, words, w-2*innerPad)
	if len(lines) > 4 {
		lines = lines[:4]
	}
	lineH := 18.0 * scale
	asc := float64(bodyFace.Metrics().Ascent) / 64
	spaceW := float64(font.MeasureString(bodyFace, " ").Ceil())
	for i, line := range lines {
		base := bodyTop + float64(i)*lineH + asc
		lx := x + innerPad
		for j, wrd := range line {
			drawText(img, bodyFace, wrd.col, lx, base, wrd.text)
			if wrd.strike {
				strikeY := base - asc*0.35
				strikeH := math.Max(1.0, 1.2*scale)
				fillRoundRect(img, lx, strikeY, wrd.width, strikeH, 0, wrd.col)
			}
			lx += wrd.width
			if !wrd.glueToNext && j < len(line)-1 {
				lx += spaceW
			}
		}
	}

	// Bottom telemetry bar:
	botY := bodyTop + float64(len(lines))*lineH + 8.0*scale
	botH := 16.0 * scale
	telemetry := fmt.Sprintf("ASR %dms · LLM %dms", item.AsrMs, item.CleanupMs)
	drawText(img, smallFace, theme.OnSurfaceVariant, x+innerPad, botY+baseline(smallFace, botH), telemetry)

	wordsStr := fmt.Sprintf("%d words", item.Words)
	if item.Words == 1 {
		wordsStr = "1 word"
	}
	ww := float64(font.MeasureString(smallFace, wordsStr).Ceil())
	drawText(img, smallFace, theme.OnSurfaceVariant, x+w-innerPad-ww, botY+baseline(smallFace, botH), wordsStr)
	return out
}

// drawTabBar draws the segmented control for switching between Dictations and Settings tabs.
func drawTabBar(img *image.RGBA, activeTab int, x, y, w, h float64, scale float64, theme Theme, count int, chipFace font.Face) []Control {
	var out []Control
	rad := 8.0 * scale
	tabW := (w - 4.0*scale) / 2.0
	bw := 1.0 * scale

	// Background container
	fillRoundRect(img, x, y, w, h, rad, theme.Inset)

	tab1Title := fmt.Sprintf("Dictations (%d)", count)
	tab2Title := "Settings"

	tab1X := x + 2.0*scale
	tab1Y := y + 2.0*scale
	tab1H := h - 4.0*scale

	// Tab 1: Dictations
	if activeTab == TabDictations {
		fillRoundRect(img, tab1X, tab1Y, tabW, tab1H, rad-2.0*scale, theme.CardBorder)
		fillRoundRect(img, tab1X+bw, tab1Y+bw, tabW-2*bw, tab1H-2*bw, rad-2.0*scale-bw, theme.Card)
		t1w := float64(font.MeasureString(chipFace, tab1Title).Ceil())
		drawText(img, chipFace, theme.Primary, tab1X+(tabW-t1w)/2, tab1Y+baseline(chipFace, tab1H), tab1Title)
	} else {
		t1w := float64(font.MeasureString(chipFace, tab1Title).Ceil())
		drawText(img, chipFace, theme.OnSurfaceVariant, tab1X+(tabW-t1w)/2, tab1Y+baseline(chipFace, tab1H), tab1Title)
	}
	out = append(out, Control{ID: "tab:dictations", Kind: ControlButton, Rect: image.Rect(
		int(math.Round(tab1X)), int(math.Round(tab1Y)),
		int(math.Round(tab1X+tabW)), int(math.Round(tab1Y+tab1H)),
	)})

	// Tab 2: Settings
	tab2X := tab1X + tabW
	if activeTab == TabSettings {
		fillRoundRect(img, tab2X, tab1Y, tabW, tab1H, rad-2.0*scale, theme.CardBorder)
		fillRoundRect(img, tab2X+bw, tab1Y+bw, tabW-2*bw, tab1H-2*bw, rad-2.0*scale-bw, theme.Card)
		t2w := float64(font.MeasureString(chipFace, tab2Title).Ceil())
		drawText(img, chipFace, theme.Primary, tab2X+(tabW-t2w)/2, tab1Y+baseline(chipFace, tab1H), tab2Title)
	} else {
		t2w := float64(font.MeasureString(chipFace, tab2Title).Ceil())
		drawText(img, chipFace, theme.OnSurfaceVariant, tab2X+(tabW-t2w)/2, tab1Y+baseline(chipFace, tab1H), tab2Title)
	}
	out = append(out, Control{ID: "tab:settings", Kind: ControlButton, Rect: image.Rect(
		int(math.Round(tab2X)), int(math.Round(tab1Y)),
		int(math.Round(tab2X+tabW)), int(math.Round(tab1Y+tab1H)),
	)})

	return out
}

// drawEmptyHistory renders an empty state notice when there are no recent dictations recorded.
func drawEmptyHistory(img *image.RGBA, x, y, w, h float64, scale float64, theme Theme, face, smallFace font.Face) {
	rad := 12.0 * scale
	bw := 1.0 * scale
	fillRoundRect(img, x, y, w, h, rad, theme.CardBorder)
	fillRoundRect(img, x+bw, y+bw, w-2*bw, h-2*bw, rad-bw, theme.Card)

	line1 := "No dictations in local history"
	line2 := "Dictate using your hotkey to see transcripts here."

	l1w := float64(font.MeasureString(face, line1).Ceil())
	l2w := float64(font.MeasureString(smallFace, line2).Ceil())

	midY := y + h/2
	drawText(img, face, theme.OnSurface, x+(w-l1w)/2, midY-4*scale, line1)
	drawText(img, smallFace, theme.OnSurfaceVariant, x+(w-l2w)/2, midY+16*scale, line2)
}

// measureRow returns the logical height of a row, scaled, wrapping chips onto multiple lines if needed.
func measureRow(r Row, maxWidth, scale float64, chipFace font.Face) float64 {
	switch r.Kind {
	case RowChips:
		lines := 1
		cx := 0.0
		for _, opt := range r.Options {
			cw := float64(font.MeasureString(chipFace, opt.Text).Ceil()) + 2*rowChipPadX*scale
			if cx > 0 && cx+cw > maxWidth {
				lines++
				cx = cw + rowChipGap*scale
			} else {
				cx += cw + rowChipGap*scale
			}
		}
		return (rowLabelH + float64(lines)*rowChipsH + float64(lines-1)*rowGap) * scale
	case RowToggle:
		return rowToggleH * scale
	case RowDropdown:
		if !r.On {
			return rowButtonH * scale
		}
		rowsCount := (len(r.Options) + 1) / 2
		itemH := 30.0 * scale
		itemGap := 4.0 * scale
		menuPad := 8.0 * scale
		gridH := float64(rowsCount)*itemH + float64(rowsCount-1)*itemGap + 2*menuPad
		return (rowButtonH+4.0)*scale + gridH
	default: // RowButton
		return rowButtonH * scale
	}
}

// drawRow draws one settings row and returns its interactive regions (in image coordinates).
func drawRow(img *image.RGBA, r Row, x, y, w, h float64, shadow int, scale float64, theme Theme,
	labelFace, chipFace, rowFace font.Face) []Control {

	switch r.Kind {
	case RowChips:
		drawTextEmboldened(img, labelFace, theme.OnSurfaceVariant, x, y+baseline(labelFace, rowLabelH*scale), r.Text)
		chipY := y + rowLabelH*scale
		chH := rowChipsH * scale
		cx := x
		var out []Control
		for _, opt := range r.Options {
			cw := float64(font.MeasureString(chipFace, opt.Text).Ceil()) + 2*rowChipPadX*scale
			if cx > x && cx+cw > x+w {
				cx = x
				chipY += chH + rowGap*scale
			}
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

	case RowDropdown:
		trigH := rowButtonH * scale
		fillRoundRect(img, x, y, w, trigH, 10*scale, theme.Border)
		inner := 1.0 * scale
		fillRoundRect(img, x+inner, y+inner, w-2*inner, trigH-2*inner, 10*scale-inner, theme.Inset)
		drawText(img, rowFace, theme.OnSurface, x+12*scale, y+baseline(rowFace, trigH), r.Text)

		// Right-aligned pill: color dot + selected name + vector chevron
		pillText := r.Detail
		dw := float64(font.MeasureString(chipFace, pillText).Ceil())
		dotSz := 10.0 * scale
		dotGap := 7.0 * scale
		chevSz := 10.0 * scale
		chevGap := 7.0 * scale
		pillPadX := 10.0 * scale
		pillW := dw + dotSz + dotGap + chevSz + chevGap + 2*pillPadX
		pillH := 26.0 * scale
		pillX := x + w - 8.0*scale - pillW
		pillY := y + (trigH-pillH)/2

		fillRoundRect(img, pillX, pillY, pillW, pillH, pillH/2, theme.Border)
		fillRoundRect(img, pillX+inner, pillY+inner, pillW-2*inner, pillH-2*inner, pillH/2-inner, theme.Surface)

		dotX := pillX + pillPadX
		dotY := pillY + (pillH-dotSz)/2
		fillRoundRect(img, dotX, dotY, dotSz, dotSz, dotSz/2, theme.Primary)

		textX := dotX + dotSz + dotGap
		drawText(img, chipFace, theme.OnSurface, textX, pillY+baseline(chipFace, pillH), pillText)

		chevX := textX + dw + chevGap + chevSz/2
		chevY := pillY + pillH/2
		drawChevron(img, chevX, chevY, chevSz, theme.OnSurfaceVariant, r.On)

		var out []Control
		out = append(out, Control{ID: r.ID, Kind: ControlButton,
			Rect: image.Rect(int(math.Round(x)), int(math.Round(y)), int(math.Round(x+w)), int(math.Round(y+trigH)))})

		if r.On {
			menuY := y + trigH + 4.0*scale
			rowsCount := (len(r.Options) + 1) / 2
			itemH := 30.0 * scale
			itemGap := 4.0 * scale
			menuPad := 8.0 * scale
			gridH := float64(rowsCount)*itemH + float64(rowsCount-1)*itemGap + 2*menuPad

			fillRoundRect(img, x, menuY, w, gridH, 10*scale, theme.Border)
			fillRoundRect(img, x+inner, menuY+inner, w-2*inner, gridH-2*inner, 10*scale-inner, theme.Inset)

			colGap := 8.0 * scale
			colW := (w - 2*menuPad - colGap) / 2

			for idx, opt := range r.Options {
				col := idx % 2
				rowIdx := idx / 2
				ix := x + menuPad + float64(col)*(colW+colGap)
				iy := menuY + menuPad + float64(rowIdx)*(itemH+itemGap)

				iRect := image.Rect(int(math.Round(ix)), int(math.Round(iy)), int(math.Round(ix+colW)), int(math.Round(iy+itemH)))

				presetID := strings.TrimPrefix(opt.ID, "preset:")
				pCol := theme.Primary
				for _, p := range Presets {
					if p.ID == presetID {
						if theme.Surface.R < 128 {
							pCol = p.DarkPrim
						} else {
							pCol = p.LightPrim
						}
						break
					}
				}

				if opt.On {
					fillRoundRect(img, ix, iy, colW, itemH, 6*scale, theme.Border)
					fillRoundRect(img, ix+inner, iy+inner, colW-2*inner, itemH-2*inner, 6*scale-inner, theme.Surface)
				}

				pDotSz := 10.0 * scale
				pDotX := ix + 8.0*scale
				pDotY := iy + (itemH-pDotSz)/2
				fillRoundRect(img, pDotX, pDotY, pDotSz, pDotSz, pDotSz/2, pCol)

				nameX := pDotX + pDotSz + 8.0*scale
				if opt.On {
					drawText(img, chipFace, theme.Primary, nameX, iy+baseline(chipFace, itemH), opt.Text)
					drawCheckmark(img, ix+colW-16.0*scale, iy+itemH/2, 10.0*scale, theme.Primary)
				} else {
					drawText(img, chipFace, theme.OnSurface, nameX, iy+baseline(chipFace, itemH), opt.Text)
				}

				out = append(out, Control{ID: opt.ID, Kind: ControlButton, Rect: iRect})
			}
		}
		return out

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

// drawChevron draws a clean downward or upward pointing arrow centered at (cx, cy).
func drawChevron(img *image.RGBA, cx, cy, sz float64, col color.RGBA, up bool) {
	var z vector.Rasterizer
	b := img.Bounds()
	z.Reset(b.Dx(), b.Dy())
	hw := float32(sz * 0.38)
	hh := float32(sz * 0.22)
	th := float32(1.5)
	fcx, fcy := float32(cx), float32(cy)
	if up {
		hh = -hh
	}
	z.MoveTo(fcx-hw, fcy-hh)
	z.LineTo(fcx, fcy+hh)
	z.LineTo(fcx+hw, fcy-hh)
	z.LineTo(fcx+hw, fcy-hh+th)
	z.LineTo(fcx, fcy+hh+th)
	z.LineTo(fcx-hw, fcy-hh+th)
	z.ClosePath()
	z.Draw(img, b, image.NewUniform(col), image.Point{})
}

// drawCheckmark draws a crisp checkmark centered at (cx, cy).
func drawCheckmark(img *image.RGBA, cx, cy, sz float64, col color.RGBA) {
	var z vector.Rasterizer
	b := img.Bounds()
	z.Reset(b.Dx(), b.Dy())
	th := float32(1.6)
	fcx, fcy := float32(cx), float32(cy)
	w, h := float32(sz*0.4), float32(sz*0.35)
	z.MoveTo(fcx-w, fcy)
	z.LineTo(fcx-w*0.2, fcy+h)
	z.LineTo(fcx+w, fcy-h)
	z.LineTo(fcx+w, fcy-h+th)
	z.LineTo(fcx-w*0.2, fcy+h+th)
	z.LineTo(fcx-w, fcy+th)
	z.ClosePath()
	z.Draw(img, b, image.NewUniform(col), image.Point{})
}
