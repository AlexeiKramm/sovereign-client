// Package render draws the desktop pill bitmap with pure Go (golang.org/x/image). It is the one
// drawing path shared by the floating overlay (Chunk 4c) and the future control window (Chunk 4d),
// and it mirrors the phone's design tokens so the two pills look alike.
//
// It has no platform code: the Windows layered window that shows the bitmap lives in
// internal/windows, and anything that draws calls Render.
package render

import (
	_ "embed"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
)

// interTTF is the Inter variable font the phone bundles (res/font/inter_variable.ttf, OFL — see
// assets/LICENSE-Inter-OFL.txt). Bundling the same file keeps the desktop pill's type identical to
// the phone's and leaves the binary self-contained.
//
//go:embed assets/inter_variable.ttf
var interTTF []byte

var (
	parseOnce sync.Once
	parsed    *opentype.Font
	parseErr  error
)

// interPalette is the package-wide font-face lock. x/image's font.Face is not safe for concurrent
// use, and the parsed font is shared, so every Render serialises through here. Rendering happens a
// few times a second at most, so the lock costs nothing.
var faceMu sync.Mutex

// parseInter parses the bundled font once.
func parseInter() (*opentype.Font, error) {
	parseOnce.Do(func() {
		parsed, parseErr = opentype.Parse(interTTF)
	})
	return parsed, parseErr
}

// newFace builds an Inter face at px pixels. The DPI is fixed at 72 so that Size is the em size in
// pixels; callers scale the size by the monitor DPI themselves.
func newFace(px float64) (font.Face, error) {
	f, err := parseInter()
	if err != nil {
		return nil, err
	}
	return opentype.NewFace(f, &opentype.FaceOptions{
		Size:    px,
		DPI:     72,
		Hinting: font.HintingFull,
	})
}

// FontAvailable reports whether the bundled font parsed, so the caller can log a renderer that
// degraded rather than failing silently.
func FontAvailable() bool {
	_, err := parseInter()
	return err == nil
}
