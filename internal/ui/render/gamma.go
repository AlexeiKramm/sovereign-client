package render

import (
	"image"
	"image/color"
	"math"
	"unicode/utf8"

	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

// Gamma tables for fast, optically correct sRGB <-> linear conversion.
var (
	srgbToLinearTab [256]float32
	linearToSrgbTab [4096]uint8
)

func init() {
	for i := 0; i < 256; i++ {
		v := float64(i) / 255.0
		if v <= 0.04045 {
			srgbToLinearTab[i] = float32(v / 12.92)
		} else {
			srgbToLinearTab[i] = float32(math.Pow((v+0.055)/1.055, 2.4))
		}
	}
	for i := 0; i < 4096; i++ {
		v := float64(i) / 4095.0
		var c float64
		if v <= 0.0031308 {
			c = v * 12.92
		} else {
			c = 1.055*math.Pow(v, 1.0/2.4) - 0.055
		}
		n := int(math.Round(c * 255.0))
		if n < 0 {
			n = 0
		} else if n > 255 {
			n = 255
		}
		linearToSrgbTab[i] = uint8(n)
	}
}

func sRGBToLinear(c uint8) float32 {
	return srgbToLinearTab[c]
}

func linearToSRGB(v float32) uint8 {
	if v <= 0 {
		return 0
	}
	if v >= 1 {
		return 255
	}
	idx := int(v * 4095.0)
	if idx >= 4096 {
		return 255
	}
	return linearToSrgbTab[idx]
}

// drawTextGamma renders string s with high-precision grayscale anti-aliasing, gamma-corrected
// linear blending, and stem contrast enhancement. It avoids subpixel color shifting (which causes
// chromatic fringing on transparent/layered windows) while keeping typography crisp and solid.
func drawTextGamma(img *image.RGBA, face font.Face, col color.RGBA, dot fixed.Point26_6, s string) {
	drawTextOptions(img, face, col, dot, s, false)
}

// drawTextBold renders string s with synthesized Medium/SemiBold stroke dilation and crisp anti-aliasing.
func drawTextBold(img *image.RGBA, face font.Face, col color.RGBA, dot fixed.Point26_6, s string) {
	drawTextOptions(img, face, col, dot, s, true)
}

func drawTextOptions(img *image.RGBA, face font.Face, col color.RGBA, dot fixed.Point26_6, s string, bold bool) {
	if col.A == 0 || s == "" {
		return
	}

	var srcUnpremulR, srcUnpremulG, srcUnpremulB uint8
	if col.A == 255 {
		srcUnpremulR, srcUnpremulG, srcUnpremulB = col.R, col.G, col.B
	} else {
		srcUnpremulR = uint8((uint32(col.R)*255 + uint32(col.A)/2) / uint32(col.A))
		srcUnpremulG = uint8((uint32(col.G)*255 + uint32(col.A)/2) / uint32(col.A))
		srcUnpremulB = uint8((uint32(col.B)*255 + uint32(col.A)/2) / uint32(col.A))
	}
	srcLinR := sRGBToLinear(srcUnpremulR)
	srcLinG := sRGBToLinear(srcUnpremulG)
	srcLinB := sRGBToLinear(srcUnpremulB)
	srcAlphaF := float32(col.A) / 255.0

	bounds := img.Bounds()
	prevC := rune(-1)
	// Round dot to integer pixel boundary for crisp vertical stem placement
	curDot := fixed.Point26_6{
		X: (dot.X + 32) &^ 63,
		Y: (dot.Y + 32) &^ 63,
	}

	for len(s) > 0 {
		c, sz := utf8.DecodeRuneInString(s)
		s = s[sz:]
		if prevC >= 0 {
			curDot.X += face.Kern(prevC, c)
		}

		dr, mask, maskp, advance, ok := face.Glyph(curDot, c)
		if !ok || dr.Empty() {
			curDot.X += advance
			prevC = c
			continue
		}

		clip := dr
		if bold {
			clip.Max.X += 1
		}
		clip = clip.Intersect(bounds)
		if !clip.Empty() {
			alphaMask, isAlpha := mask.(*image.Alpha)

			getAlpha := func(x, y int) float32 {
				if x < dr.Min.X || x >= dr.Max.X || y < dr.Min.Y || y >= dr.Max.Y {
					return 0
				}
				if isAlpha {
					off := (y-dr.Min.Y+maskp.Y)*alphaMask.Stride + (x - dr.Min.X + maskp.X)
					if off >= 0 && off < len(alphaMask.Pix) {
						return float32(alphaMask.Pix[off]) / 255.0
					}
					return 0
				}
				_, _, _, a := mask.At(x-dr.Min.X+maskp.X, y-dr.Min.Y+maskp.Y).RGBA()
				return float32(a>>8) / 255.0
			}

			for y := clip.Min.Y; y < clip.Max.Y; y++ {
				for x := clip.Min.X; x < clip.Max.X; x++ {
					raw := getAlpha(x, y)
					if bold {
						rawShift := getAlpha(x-1, y)
						if rawShift > raw {
							raw = rawShift
						}
					}

					if raw <= 0.02 {
						continue
					}

					off := img.PixOffset(x, y)
					dstA := img.Pix[off+3]

					if dstA == 255 {
						// Opaque surface (card / buttons / chips) - linear light blending
						dstLinR := sRGBToLinear(img.Pix[off])
						dstLinG := sRGBToLinear(img.Pix[off+1])
						dstLinB := sRGBToLinear(img.Pix[off+2])

						srcLum := 0.2126*srcLinR + 0.7152*srcLinG + 0.0722*srcLinB
						dstLum := 0.2126*dstLinR + 0.7152*dstLinG + 0.0722*dstLinB

						// Polarity-aware coverage:
						// - Light text on darker background: apply gentle optical compensation (stem boost)
						//   to counteract optical thinning caused by background halation.
						// - Dark text on light/bright background (e.g. cyan chip, light theme): do NOT
						//   boost coverage, as ink spreading bloats stems and closes small counters.
						cov := raw * srcAlphaF
						if srcLum >= dstLum {
							cov = float32(math.Pow(float64(raw), 0.82)) * srcAlphaF
						}
						if cov > 1.0 {
							cov = 1.0
						}

						outLinR := srcLinR*cov + dstLinR*(1.0-cov)
						outLinG := srcLinG*cov + dstLinG*(1.0-cov)
						outLinB := srcLinB*cov + dstLinB*(1.0-cov)

						img.Pix[off] = linearToSRGB(outLinR)
						img.Pix[off+1] = linearToSRGB(outLinG)
						img.Pix[off+2] = linearToSRGB(outLinB)
						img.Pix[off+3] = 255
					} else if dstA == 0 {
						// Fully transparent background
						srcLum := 0.2126*srcLinR + 0.7152*srcLinG + 0.0722*srcLinB
						cov := raw * srcAlphaF
						if srcLum >= 0.5 {
							cov = float32(math.Pow(float64(raw), 0.82)) * srcAlphaF
						}
						if cov > 1.0 {
							cov = 1.0
						}

						outA := uint8(cov*255.0 + 0.5)
						img.Pix[off] = uint8(float32(col.R)*cov + 0.5)
						img.Pix[off+1] = uint8(float32(col.G)*cov + 0.5)
						img.Pix[off+2] = uint8(float32(col.B)*cov + 0.5)
						img.Pix[off+3] = outA
					} else {
						// Partially translucent background
						srcLum := 0.2126*srcLinR + 0.7152*srcLinG + 0.0722*srcLinB
						dstUnpreR := uint8((uint32(img.Pix[off])*255 + uint32(dstA)/2) / uint32(dstA))
						dstUnpreG := uint8((uint32(img.Pix[off+1])*255 + uint32(dstA)/2) / uint32(dstA))
						dstUnpreB := uint8((uint32(img.Pix[off+2])*255 + uint32(dstA)/2) / uint32(dstA))

						dstLinR := sRGBToLinear(dstUnpreR)
						dstLinG := sRGBToLinear(dstUnpreG)
						dstLinB := sRGBToLinear(dstUnpreB)
						dstLum := 0.2126*dstLinR + 0.7152*dstLinG + 0.0722*dstLinB

						cov := raw * srcAlphaF
						if srcLum >= dstLum {
							cov = float32(math.Pow(float64(raw), 0.82)) * srcAlphaF
						}
						if cov > 1.0 {
							cov = 1.0
						}

						outLinR := srcLinR*cov + dstLinR*(1.0-cov)
						outLinG := srcLinG*cov + dstLinG*(1.0-cov)
						outLinB := srcLinB*cov + dstLinB*(1.0-cov)

						outUnpreR := linearToSRGB(outLinR)
						outUnpreG := linearToSRGB(outLinG)
						outUnpreB := linearToSRGB(outLinB)

						invA := float32(255-dstA) / 255.0
						outA := uint8(float32(dstA) + cov*invA*255.0 + 0.5)

						img.Pix[off] = uint8((uint32(outUnpreR)*uint32(outA) + 127) / 255)
						img.Pix[off+1] = uint8((uint32(outUnpreG)*uint32(outA) + 127) / 255)
						img.Pix[off+2] = uint8((uint32(outUnpreB)*uint32(outA) + 127) / 255)
						img.Pix[off+3] = outA
					}
				}
			}
		}

		curDot.X += advance
		prevC = c
	}
}
