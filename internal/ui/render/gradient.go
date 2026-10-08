package render

import (
	"image"
	"image/color"
	"math"
)

// gradStop is one colour stop of a radial gradient, positioned at t in [0,1].
type gradStop struct {
	t   float64
	col color.RGBA
}

// radialGradient renders an opaque radial gradient into a new w×h RGBA image. Colours between stops
// are interpolated in linear light, matching the Android Compose gradient.
func radialGradient(w, h int, cx, cy, radius float64, stops []gradStop) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	if w <= 0 || h <= 0 || len(stops) == 0 || radius <= 0 {
		return img
	}
	// Resolve the stops once into a small ramp, so the per-pixel work is a subtract, a Hypot and a
	// table lookup — the panel repaints on every mouse move and live-text update.
	lut := buildGradientLUT(stops)
	for y := 0; y < h; y++ {
		dy := float64(y) + 0.5 - cy
		row := img.Pix[y*img.Stride : y*img.Stride+w*4]
		for x := 0; x < w; x++ {
			dx := float64(x) + 0.5 - cx
			col := sampleLUT(lut, math.Sqrt(dx*dx+dy*dy)/radius)
			o := x * 4
			row[o] = col.R
			row[o+1] = col.G
			row[o+2] = col.B
			row[o+3] = col.A
		}
	}
	return img
}

// gradientLUTSize is the resolution of the precomputed gradient ramp; far finer than the 8-bit
// output, so it introduces no visible banding.
const gradientLUTSize = 1024

// buildGradientLUT samples the stops across t in [0,1] into a ramp.
func buildGradientLUT(stops []gradStop) []color.RGBA {
	lut := make([]color.RGBA, gradientLUTSize)
	for i := range lut {
		lut[i] = sampleGradient(stops, float64(i)/float64(gradientLUTSize-1))
	}
	return lut
}

// sampleLUT returns the ramp colour at t, clamped to [0,1].
func sampleLUT(lut []color.RGBA, t float64) color.RGBA {
	if t <= 0 {
		return lut[0]
	}
	if t >= 1 {
		return lut[len(lut)-1]
	}
	return lut[int(t*float64(len(lut)-1))]
}

// sampleGradient interpolates between the stops at position t (clamped to the stop range).
func sampleGradient(stops []gradStop, t float64) color.RGBA {
	if t <= stops[0].t {
		return stops[0].col
	}
	last := stops[len(stops)-1]
	if t >= last.t {
		return last.col
	}
	for i := 1; i < len(stops); i++ {
		if t <= stops[i].t {
			a, b := stops[i-1], stops[i]
			f := 0.0
			if span := b.t - a.t; span > 0 {
				f = (t - a.t) / span
			}
			return mixRGB(a.col, b.col, f)
		}
	}
	return last.col
}

// mixRGB blends two opaque colours in linear light by fraction f in [0,1].
func mixRGB(a, b color.RGBA, f float64) color.RGBA {
	ar, ag, ab := unpremulRGB(a)
	br, bg, bb := unpremulRGB(b)
	mix := func(x, y uint8) uint8 {
		lx := sRGBToLinear(x)
		return linearToSRGB(lx + (sRGBToLinear(y)-lx)*float32(f))
	}
	return premul(mix(ar, br), mix(ag, bg), mix(ab, bb), 0xFF)
}

// blendOver composites top over base at the given opacity, returning an opaque colour. It stands in
// for Compose drawing a translucent gradient stop over the window background.
func blendOver(base, top color.RGBA, alpha float64) color.RGBA {
	br, bg, bb := unpremulRGB(base)
	tr, tg, tb := unpremulRGB(top)
	mix := func(x, y uint8) uint8 {
		lx := sRGBToLinear(x)
		return linearToSRGB(lx + (sRGBToLinear(y)-lx)*float32(alpha))
	}
	return premul(mix(br, tr), mix(bg, tg), mix(bb, tb), 0xFF)
}

// panelBackground builds the radial gradient behind the control window, mirroring the phone's
// MainScreen background: a restrained accent glow at the top-centre fading through the elevated card
// tone into the surface. It is intentionally TonalSpot-muted — low chroma and low opacity — so the
// canvas stays quiet and text keeps its contrast, rather than a vibrant band behind the header.
func panelBackground(w, h int, theme Theme, dark bool) *image.RGBA {
	// theme.Primary is already the TonalSpot accent, so it is used as-is: the low opacity keeps the
	// glow a tint rather than a coloured band.
	glow := theme.Primary
	var stops []gradStop
	if dark {
		stops = []gradStop{
			{t: 0.0, col: blendOver(theme.Surface, glow, 0.14)},
			{t: 0.30, col: blendOver(theme.Surface, glow, 0.05)},
			{t: 0.62, col: blendOver(theme.Surface, theme.Card, 0.85)},
			{t: 1.0, col: theme.Surface},
		}
	} else {
		stops = []gradStop{
			{t: 0.0, col: blendOver(theme.Surface, glow, 0.12)},
			{t: 0.30, col: blendOver(theme.Surface, glow, 0.04)},
			{t: 0.66, col: blendOver(theme.Surface, theme.Card, 1.0)},
			{t: 1.0, col: theme.Surface},
		}
	}
	cx := float64(w) * 0.5
	cy := float64(h) * 0.02
	radius := float64(w)
	if h > w {
		radius = float64(h)
	}
	radius *= 0.75
	return radialGradient(w, h, cx, cy, radius, stops)
}

// fillRoundRectGradient fills a rounded rectangle with a gradient image, clipped to the rounded
// path so the card keeps its corners. Like fillRoundRect it stays within the rect's bounding box.
func fillRoundRectGradient(img *image.RGBA, x, y, w, h, r float64, grad *image.RGBA) {
	if w <= 0 || h <= 0 || grad == nil {
		return
	}
	gw, gh := grad.Bounds().Dx(), grad.Bounds().Dy()
	if gw <= 0 || gh <= 0 {
		return
	}
	if r > w/2 {
		r = w / 2
	}
	if r > h/2 {
		r = h / 2
	}
	gx0, gy0 := int(math.Round(x)), int(math.Round(y))
	x0, y0, x1, y1 := roundedRectBounds(img.Bounds(), x, y, w, h)
	ix0, iy0, ix1, iy1 := roundRectCore(x0, y0, x1, y1, x, y, w, h, r)
	for py := y0; py < y1; py++ {
		gy := py - gy0
		if gy < 0 || gy >= gh {
			continue
		}
		coreRow := py >= iy0 && py < iy1
		for px := x0; px < x1; px++ {
			gx := px - gx0
			if gx < 0 || gx >= gw {
				continue
			}
			col := grad.RGBAAt(gx, gy)
			if coreRow && px >= ix0 && px < ix1 {
				if col.A == 255 {
					off := img.PixOffset(px, py)
					img.Pix[off] = col.R
					img.Pix[off+1] = col.G
					img.Pix[off+2] = col.B
					img.Pix[off+3] = 255
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
