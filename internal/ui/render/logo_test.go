package render

import (
	"testing"
)

func TestRenderSovereignLogo(t *testing.T) {
	sizes := []int{16, 24, 32, 48, 64}
	for _, sz := range sizes {
		img := RenderSovereignLogo(sz)
		if img == nil {
			t.Fatalf("RenderSovereignLogo(%d) returned nil", sz)
		}
		if b := img.Bounds(); b.Dx() != sz || b.Dy() != sz {
			t.Fatalf("size = %dx%d, want %dx%d", b.Dx(), b.Dy(), sz, sz)
		}

		// Centre pixel or mid-upper pixel should be part of the white Dreadnought stencil
		hasWhite := false
		for y := 0; y < sz; y++ {
			for x := 0; x < sz; x++ {
				off := img.PixOffset(x, y)
				if img.Pix[off] > 200 && img.Pix[off+1] > 200 && img.Pix[off+2] > 200 {
					hasWhite = true
					break
				}
			}
			if hasWhite {
				break
			}
		}
		if !hasWhite {
			t.Errorf("size %d: did not find white stencil pixels", sz)
		}
	}
}
