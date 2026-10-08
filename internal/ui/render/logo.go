package render

import (
	"image"
	"image/color"
	"math"

	"golang.org/x/image/vector"
)

// Point2D is a 2D float point.
type Point2D struct {
	X, Y float32
}

// Dreadnought stencil polygons, extracted directly from the canonical
// Android app vector asset (app/src/main/res/drawable/ic_sovereign_logo.xml).
// Viewport is 108x108.
var dreadnoughtPolygons = [][]Point2D{
	// Tier 1: Apex Needles
	{
		{X: 52.08, Y: 24.88},
		{X: 52.08, Y: 51.12},
		{X: 42.48, Y: 51.12},
	},
	{
		{X: 55.92, Y: 24.88},
		{X: 65.52, Y: 51.12},
		{X: 55.92, Y: 51.12},
	},

	// Tier 2: Mid Flared Ribs
	{
		{X: 52.08, Y: 53.04},
		{X: 41.52, Y: 53.04},
		{X: 38.64, Y: 60.08},
		{X: 52.08, Y: 67.12},
	},
	{
		{X: 55.92, Y: 53.04},
		{X: 55.92, Y: 67.12},
		{X: 69.36, Y: 60.08},
		{X: 66.48, Y: 53.04},
	},

	// Tier 3: Lower Keel Plates
	{
		{X: 52.08, Y: 69.04},
		{X: 39.92, Y: 62.64},
		{X: 52.08, Y: 77.36},
	},
	{
		{X: 55.92, Y: 69.04},
		{X: 55.92, Y: 77.36},
		{X: 68.08, Y: 62.64},
	},

	// Bottom Keel Diamond Tip
	{
		{X: 52.08, Y: 78.96},
		{X: 54.00, Y: 83.12},
		{X: 55.92, Y: 78.96},
		{X: 54.00, Y: 77.36},
	},
}

// DrawSovereignLogo draws the authentic Sovereign brand mark: the obsidian gradient disc
// with the white 3-Tier Dreadnought stencil, scaled to fit the destination bounds [x, y, x+size, y+size].
func DrawSovereignLogo(dst *image.RGBA, x, y, size float64) {
	if size <= 0 {
		return
	}

	w := int(math.Ceil(x + size))
	h := int(math.Ceil(y + size))
	clip := dst.Bounds().Intersect(image.Rect(int(math.Floor(x)), int(math.Floor(y)), w, h))
	if clip.Empty() {
		return
	}

	cx := x + size/2.0
	cy := y + size/2.0
	r := size / 2.0

	// 1. Draw the obsidian gradient disc with anti-aliased edge.
	// Gradient stops from Android:
	// 0.0: #1C232E
	// 0.6: #10141C
	// 1.0: #07090C
	for py := clip.Min.Y; py < clip.Max.Y; py++ {
		fy := float64(py) + 0.5
		t := (fy - y) / size
		if t < 0 {
			t = 0
		} else if t > 1 {
			t = 1
		}

		var gradR, gradG, gradB float64
		if t <= 0.6 {
			f := t / 0.6
			gradR = 0x1C*(1.0-f) + 0x10*f
			gradG = 0x23*(1.0-f) + 0x14*f
			gradB = 0x2E*(1.0-f) + 0x1C*f
		} else {
			f := (t - 0.6) / 0.4
			gradR = 0x10*(1.0-f) + 0x07*f
			gradG = 0x14*(1.0-f) + 0x09*f
			gradB = 0x1C*(1.0-f) + 0x0C*f
		}

		for px := clip.Min.X; px < clip.Max.X; px++ {
			fx := float64(px) + 0.5
			dx := fx - cx
			dy := fy - cy
			dist := math.Sqrt(dx*dx + dy*dy)

			if dist > r+0.5 {
				continue
			}

			// Subpixel anti-aliasing on circle perimeter
			cov := 1.0
			if dist > r-0.5 {
				cov = (r + 0.5) - dist
			}

			// Subtle inner rim bevel
			rCol, gCol, bCol := gradR, gradG, gradB
			if dist >= r-1.5 && dist <= r-0.5 {
				bevelA := 0.12 * cov
				rCol = rCol*(1.0-bevelA) + 255.0*bevelA
				gCol = gCol*(1.0-bevelA) + 255.0*bevelA
				bCol = bCol*(1.0-bevelA) + 255.0*bevelA
			}

			off := dst.PixOffset(px, py)
			dstA := float64(dst.Pix[off+3]) / 255.0
			srcA := cov

			outA := srcA + dstA*(1.0-srcA)
			if outA <= 0 {
				continue
			}

			// Premultiplied alpha composite
			pSrcR := rCol * srcA
			pSrcG := gCol * srcA
			pSrcB := bCol * srcA

			pDstR := float64(dst.Pix[off])
			pDstG := float64(dst.Pix[off+1])
			pDstB := float64(dst.Pix[off+2])

			dst.Pix[off] = uint8(math.Round(pSrcR + pDstR*(1.0-srcA)))
			dst.Pix[off+1] = uint8(math.Round(pSrcG + pDstG*(1.0-srcA)))
			dst.Pix[off+2] = uint8(math.Round(pSrcB + pDstB*(1.0-srcA)))
			dst.Pix[off+3] = uint8(math.Round(outA * 255.0))
		}
	}

	// 2. Rasterize the 3-Tier Dreadnought stencil in white, bounded to the logo box.
	stencilCol := color.RGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}
	fillPathUniform(dst, clip, stencilCol, func(z *vector.Rasterizer, ox, oy float64) {
		s := float32(size) / 108.0
		lx, ly := float32(x-ox), float32(y-oy)
		for _, poly := range dreadnoughtPolygons {
			if len(poly) < 3 {
				continue
			}
			z.MoveTo(lx+poly[0].X*s, ly+poly[0].Y*s)
			for _, pt := range poly[1:] {
				z.LineTo(lx+pt.X*s, ly+pt.Y*s)
			}
			z.ClosePath()
		}
	})
}

// RenderSovereignLogo returns a standalone square image of the Sovereign brand mark.
func RenderSovereignLogo(size int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	DrawSovereignLogo(img, 0, 0, float64(size))
	return img
}
