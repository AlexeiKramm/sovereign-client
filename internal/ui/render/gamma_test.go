package render

import (
	"image"
	"image/color"
	"math"
	"testing"

	"golang.org/x/image/math/fixed"
)

func TestGammaTableEndpoints(t *testing.T) {
	if got := sRGBToLinear(0); got != 0 {
		t.Errorf("sRGBToLinear(0) = %v, want 0", got)
	}
	if got := sRGBToLinear(255); math.Abs(float64(got-1.0)) > 1e-4 {
		t.Errorf("sRGBToLinear(255) = %v, want ~1.0", got)
	}
	if got := linearToSRGB(0); got != 0 {
		t.Errorf("linearToSRGB(0) = %v, want 0", got)
	}
	if got := linearToSRGB(1.0); got != 255 {
		t.Errorf("linearToSRGB(1.0) = %v, want 255", got)
	}
	// Midpoint roundtrip check: 128 sRGB should be around ~0.216 linear
	lin128 := sRGBToLinear(128)
	if lin128 < 0.20 || lin128 > 0.23 {
		t.Errorf("sRGBToLinear(128) = %v, want ~0.216", lin128)
	}
	rt128 := linearToSRGB(lin128)
	if rt128 != 128 {
		t.Errorf("linearToSRGB(sRGBToLinear(128)) = %v, want 128", rt128)
	}
}

func TestDrawTextGammaRendersPixels(t *testing.T) {
	face, err := newFace(16)
	if err != nil {
		t.Fatalf("newFace: %v", err)
	}
	defer face.Close()

	img := image.NewRGBA(image.Rect(0, 0, 100, 30))
	// Draw white text onto empty (transparent) target
	drawTextGamma(img, face, color.RGBA{R: 255, G: 255, B: 255, A: 255}, fixed.Point26_6{X: 10 * 64, Y: 20 * 64}, "Test")

	hasOpaque := false
	for _, b := range img.Pix {
		if b > 0 {
			hasOpaque = true
			break
		}
	}
	if !hasOpaque {
		t.Error("drawTextGamma wrote no non-zero pixels")
	}
}

func TestDrawTextGammaBlendsOverSolidBackground(t *testing.T) {
	face, err := newFace(16)
	if err != nil {
		t.Fatalf("newFace: %v", err)
	}
	defer face.Close()

	img := image.NewRGBA(image.Rect(0, 0, 100, 30))
	// Fill with dark background: #1C232E
	bg := color.RGBA{R: 0x1C, G: 0x23, B: 0x2E, A: 0xFF}
	for y := 0; y < 30; y++ {
		for x := 0; x < 100; x++ {
			off := img.PixOffset(x, y)
			img.Pix[off] = bg.R
			img.Pix[off+1] = bg.G
			img.Pix[off+2] = bg.B
			img.Pix[off+3] = bg.A
		}
	}

	// Draw white text
	drawTextGamma(img, face, color.RGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}, fixed.Point26_6{X: 10 * 64, Y: 20 * 64}, "Sovereign")

	hasBrightened := false
	for y := 0; y < 30; y++ {
		for x := 0; x < 100; x++ {
			off := img.PixOffset(x, y)
			if img.Pix[off] > bg.R+50 {
				hasBrightened = true
				break
			}
		}
	}
	if !hasBrightened {
		t.Error("drawTextGamma did not brighten pixels over dark background")
	}
}
