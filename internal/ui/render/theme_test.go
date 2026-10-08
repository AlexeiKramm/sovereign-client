package render

import (
	"math"
	"testing"
)

// TestPresetRetintsBackground pins the fix for the background not following the theme: switching
// preset must change the neutral canvas and card tones, not just the accent.
func TestPresetRetintsBackground(t *testing.T) {
	cyan := ThemeForPreset("cyan", true)
	amber := ThemeForPreset("amber", true)
	if cyan.Surface == amber.Surface {
		t.Fatal("dark surface is identical across presets; the background does not follow the theme")
	}
	if cyan.Card == amber.Card {
		t.Fatal("dark card is identical across presets")
	}
	if cyan.Border == amber.Border {
		t.Fatal("dark border is identical across presets")
	}
	if cyan.Primary == amber.Primary {
		t.Fatal("accent did not follow the preset")
	}
}

// TestPresetTintPreservesLightness checks the retint only borrows hue: a dark canvas must stay dark
// and keep its place in the elevation ladder, and the on-surface text must stay high contrast.
func TestPresetTintPreservesLightness(t *testing.T) {
	for _, id := range []string{"cyan", "amber", "coral", "green", "lilac"} {
		th := ThemeForPreset(id, true)
		if th.Surface.R > 0x40 {
			t.Fatalf("%s dark surface too bright: %v", id, th.Surface)
		}
		if th.Card.R <= th.Surface.R {
			t.Fatalf("%s card is not lighter than the surface", id)
		}
		if th.OnSurface.R <= th.Surface.R+0x60 {
			t.Fatalf("%s on-surface contrast too low: %v on %v", id, th.OnSurface, th.Surface)
		}
	}
}

// TestUnknownPresetFallsBackToDefault checks an empty/unknown preset yields the default cyan preset
// rather than a zero colour.
func TestUnknownPresetFallsBackToDefault(t *testing.T) {
	got := ThemeForPreset("", true)
	if got.Primary != Presets[0].DarkPrim {
		t.Fatalf("empty preset primary = %v, want the cyan default %v", got.Primary, Presets[0].DarkPrim)
	}
}

// TestAccentsAreTonalSpot pins the fix for accents reading as Vibrant: the accent must be the
// Material 3 TonalSpot primary (tone 80 dark / 40 light) generated from the seed, not the raw seed.
func TestAccentsAreTonalSpot(t *testing.T) {
	if got, want := ThemeForPreset("cyan", true).Primary, premul(0x86, 0xD1, 0xEA, 0xFF); got != want {
		t.Fatalf("cyan dark accent = %v, want TonalSpot %v", got, want)
	}
	if got, want := ThemeForPreset("cyan", false).Primary, premul(0x01, 0x67, 0x7D, 0xFF); got != want {
		t.Fatalf("cyan light accent = %v, want TonalSpot %v", got, want)
	}
	// And the accent is measurably less chromatic than the seed it was derived from.
	seed := Presets[0].Seed
	sr, sg, sb := unpremulRGB(seed)
	_, sa, sbb := rgbToLAB(sr, sg, sb)
	got := ThemeForPreset("cyan", true).Primary
	ar, ag, ab := unpremulRGB(got)
	_, aa, abb := rgbToLAB(ar, ag, ab)
	if math.Hypot(aa, abb) >= math.Hypot(sa, sbb) {
		t.Fatalf("TonalSpot accent chroma %.1f is not below the seed chroma %.1f",
			math.Hypot(aa, abb), math.Hypot(sa, sbb))
	}
}
