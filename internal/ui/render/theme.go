package render

import (
	"image/color"
	"math"
)

// Theme is the pill's palette, mirroring the phone's Material 3 tokens in
// theme/Color.kt so the desktop pill and the phone card read as the same object.
type Theme struct {
	Surface          color.RGBA // the window/card canvas background
	Card             color.RGBA // elevated card background (lighter than Surface in dark mode, pure white in light mode)
	CardBorder       color.RGBA // border around elevated cards
	Inset            color.RGBA // recessed area (e.g. live-text box)
	Border           color.RGBA // 1px hairline around the card
	OnSurface        color.RGBA // settled text — high contrast
	OnSurfaceVariant color.RGBA // live tail + status label — slate muted
	Primary          color.RGBA // status dot / accent / selected control
	OnPrimary        color.RGBA // text on a filled (selected) control
	Success          color.RGBA // inserted feedback state (green)
	OnSuccess        color.RGBA // text on success
	Error            color.RGBA // status dot + label on failure
	Shadow           color.RGBA // drop shadow under the card
}

// DarkTheme is the Sovereign Cyan dark scheme: obsidian canvas with elevated cards,
// mirroring Android's DynamicTheme.kt elevatedSovereignPop tokens.
var DarkTheme = Theme{
	Surface:          premul(0x11, 0x16, 0x1F, 0xFA), // Tone 8 dark obsidian canvas
	Card:             premul(0x1B, 0x24, 0x30, 0xFF), // Tone 15 elevated card surface (lighter than canvas)
	CardBorder:       premul(0x2C, 0x3B, 0x4E, 0x90), // OutlineVariant
	Inset:            premul(0x14, 0x1B, 0x24, 0xFF), // SurfaceContainerLow
	Border:           premul(0x26, 0x34, 0x44, 0xC0), // Outer window hairline
	OnSurface:        premul(0xF8, 0xFA, 0xFC, 0xFF), // High-contrast crisp text
	OnSurfaceVariant: premul(0x94, 0xA3, 0xB8, 0xFF), // Slate muted secondary
	Primary:          premul(0x00, 0xD2, 0xF8, 0xFF), // Sovereign Cyan (#00B4D8 / tone 80)
	OnPrimary:        premul(0x00, 0x1A, 0x24, 0xFF), // Deep obsidian-slate tone 10 on cyan
	Success:          premul(0x4A, 0xDE, 0x80, 0xFF), // Emerald green tone 80
	OnSuccess:        premul(0x05, 0x2E, 0x16, 0xFF), // Deep green tone 10
	Error:            premul(0xFF, 0x71, 0x6E, 0xFF), // Alert coral
	Shadow:           premul(0x00, 0x00, 0x00, 0x60),
}

// LightTheme is the Sovereign Cyan light scheme: soft paper canvas with elevated white cards.
var LightTheme = Theme{
	Surface:          premul(0xEE, 0xF2, 0xF6, 0xF8), // Tone 92 soft paper canvas
	Card:             premul(0xFF, 0xFF, 0xFF, 0xFF), // Pure elevated white cards
	CardBorder:       premul(0xCB, 0xD5, 0xE1, 0xB0), // OutlineVariant
	Inset:            premul(0xF8, 0xFA, 0xFC, 0xFF), // SurfaceContainerLow
	Border:           premul(0xCB, 0xD5, 0xE1, 0xD0), // Outer window hairline
	OnSurface:        premul(0x0F, 0x17, 0x2A, 0xFF), // Deep slate text
	OnSurfaceVariant: premul(0x64, 0x74, 0x8B, 0xFF), // Slate secondary
	Primary:          premul(0x00, 0x84, 0xA1, 0xFF), // Sovereign Cyan (tone 40)
	OnPrimary:        premul(0xFF, 0xFF, 0xFF, 0xFF),
	Success:          premul(0x16, 0xA3, 0x4A, 0xFF), // Green tone 40
	OnSuccess:        premul(0xFF, 0xFF, 0xFF, 0xFF),
	Error:            premul(0xBA, 0x1A, 0x1A, 0xFF),
	Shadow:           premul(0x00, 0x00, 0x00, 0x30),
}

// ThemeFor returns the palette for the requested appearance.
func ThemeFor(dark bool) Theme {
	if dark {
		return DarkTheme
	}
	return LightTheme
}

// ThemePreset defines one curated Material 3 seed color preset, mirroring Android ThemePresets.
// Seed is the swatch shown in the picker; DarkPrim/LightPrim and DarkOnPrim are the resolved
// Material 3 TonalSpot "primary"/"onPrimary" roles for that seed (see ThemeForPreset).
type ThemePreset struct {
	ID         string
	Name       string
	Hex        string
	Seed       color.RGBA
	DarkPrim   color.RGBA
	DarkOnPrim color.RGBA
	LightPrim  color.RGBA
}

// Presets contains the 11 curated M3 presets from the Android app (DynamicTheme.kt). The accent
// values are the exact TonalSpot primary/onPrimary materialkolor generates from each seed — i.e.
// TonalPalette.fromHueAndChroma(seed.hue, 36.0) at tones 80/20 (dark) and 40/100 (light), which is
// what Android's ThemePaletteStyle.TONAL_SPOT produces. The previous hand-picked values were closer
// to the raw seeds, which is why the desktop accents read as Vibrant.
var Presets = []ThemePreset{
	{ID: "cyan", Name: "Cyan", Hex: "#00B4D8", Seed: premul(0x00, 0xB4, 0xD8, 0xFF),
		DarkPrim: premul(0x86, 0xD1, 0xEA, 0xFF), DarkOnPrim: premul(0x00, 0x36, 0x42, 0xFF), LightPrim: premul(0x01, 0x67, 0x7D, 0xFF)},
	{ID: "sky", Name: "Sky", Hex: "#38BDF8", Seed: premul(0x38, 0xBD, 0xF8, 0xFF),
		DarkPrim: premul(0x90, 0xCE, 0xF3, 0xFF), DarkOnPrim: premul(0x00, 0x35, 0x4A, 0xFF), LightPrim: premul(0x1C, 0x65, 0x86, 0xFF)},
	{ID: "slate", Name: "Slate", Hex: "#64748B", Seed: premul(0x64, 0x74, 0x8B, 0xFF),
		DarkPrim: premul(0xA2, 0xC9, 0xFE, 0xFF), DarkOnPrim: premul(0x00, 0x32, 0x5B, 0xFF), LightPrim: premul(0x38, 0x60, 0x8F, 0xFF)},
	{ID: "teal", Name: "Teal", Hex: "#0D9488", Seed: premul(0x0D, 0x94, 0x88, 0xFF),
		DarkPrim: premul(0x82, 0xD5, 0xCA, 0xFF), DarkOnPrim: premul(0x00, 0x37, 0x32, 0xFF), LightPrim: premul(0x00, 0x6A, 0x61, 0xFF)},
	{ID: "amber", Name: "Amber", Hex: "#F59E0B", Seed: premul(0xF5, 0x9E, 0x0B, 0xFF),
		DarkPrim: premul(0xF8, 0xBB, 0x71, 0xFF), DarkOnPrim: premul(0x47, 0x2A, 0x00, 0xFF), LightPrim: premul(0x82, 0x55, 0x13, 0xFF)},
	{ID: "orange", Name: "Orange", Hex: "#FF9248", Seed: premul(0xFF, 0x92, 0x48, 0xFF),
		DarkPrim: premul(0xFF, 0xB6, 0x8A, 0xFF), DarkOnPrim: premul(0x52, 0x23, 0x00, 0xFF), LightPrim: premul(0x8C, 0x4F, 0x26, 0xFF)},
	{ID: "blue", Name: "Blue", Hex: "#1A73E8", Seed: premul(0x1A, 0x73, 0xE8, 0xFF),
		DarkPrim: premul(0xAD, 0xC7, 0xFF, 0xFF), DarkOnPrim: premul(0x0F, 0x2F, 0x60, 0xFF), LightPrim: premul(0x43, 0x5E, 0x91, 0xFF)},
	{ID: "green", Name: "Green", Hex: "#34A853", Seed: premul(0x34, 0xA8, 0x53, 0xFF),
		DarkPrim: premul(0x9C, 0xD4, 0x9F, 0xFF), DarkOnPrim: premul(0x00, 0x39, 0x14, 0xFF), LightPrim: premul(0x36, 0x69, 0x3E, 0xFF)},
	{ID: "coral", Name: "Coral", Hex: "#FA5B52", Seed: premul(0xFA, 0x5B, 0x52, 0xFF),
		DarkPrim: premul(0xFF, 0xB4, 0xAC, 0xFF), DarkOnPrim: premul(0x56, 0x1E, 0x1A, 0xFF), LightPrim: premul(0x90, 0x4A, 0x44, 0xFF)},
	{ID: "lilac", Name: "Lilac", Hex: "#D0BCFF", Seed: premul(0xD0, 0xBC, 0xFF, 0xFF),
		DarkPrim: premul(0xD0, 0xBC, 0xFE, 0xFF), DarkOnPrim: premul(0x36, 0x26, 0x5D, 0xFF), LightPrim: premul(0x66, 0x55, 0x8F, 0xFF)},
	{ID: "mint", Name: "Mint", Hex: "#26A69A", Seed: premul(0x26, 0xA6, 0x9A, 0xFF),
		DarkPrim: premul(0x81, 0xD5, 0xCA, 0xFF), DarkOnPrim: premul(0x00, 0x37, 0x32, 0xFF), LightPrim: premul(0x00, 0x6A, 0x62, 0xFF)},
}

// ThemeForPreset returns the theme for the chosen preset and light/dark mode. It mirrors the
// phone's dynamic palette: the seed only sets the accent, and the neutral canvas/card/border tones
// borrow the accent's hue at a low chroma — so switching preset retints the whole background, not
// just the accent (Android DynamicThemeEngine.elevatedSovereignPop).
func ThemeForPreset(id string, dark bool) Theme {
	base := ThemeFor(dark)
	p, ok := presetFor(id)
	if !ok {
		p = Presets[0]
	}
	if dark {
		base.Primary = p.DarkPrim
		base.OnPrimary = p.DarkOnPrim
	} else {
		base.Primary = p.LightPrim
		base.OnPrimary = premul(0xFF, 0xFF, 0xFF, 0xFF)
	}
	accent := base.Primary

	// Match the accent's chroma to the low level of a Material 3 neutral palette while keeping each
	// surface's own lightness, so the elevation ladder is preserved and text contrast is untouched.
	// TonalSpot is deliberately restrained: a subtle tint, not a vibrant wash.
	chroma := 3.0
	if !dark {
		chroma = 2.0
	}
	base.Surface = tintHue(base.Surface, accent, chroma)
	base.Card = tintHue(base.Card, accent, chroma)
	base.Inset = tintHue(base.Inset, accent, chroma)
	base.Border = tintHue(base.Border, accent, chroma)
	base.CardBorder = tintHue(base.CardBorder, accent, chroma)
	return base
}

// presetFor resolves a preset id to its full definition.
func presetFor(id string) (ThemePreset, bool) {
	for _, p := range Presets {
		if p.ID == id {
			return p, true
		}
	}
	return ThemePreset{}, false
}

// tintHue returns base with its lightness preserved but its hue/chroma replaced by the accent's hue
// at the given chroma. It is the desktop equivalent of Android's LAB tone retint: a near-neutral
// surface takes on the seed's hue without losing its place in the elevation ladder.
func tintHue(base, accent color.RGBA, chroma float64) color.RGBA {
	br, bg, bb := unpremulRGB(base)
	ar, ag, ab := unpremulRGB(accent)
	bl, _, _ := rgbToLAB(br, bg, bb)
	_, al, abl := rgbToLAB(ar, ag, ab)
	c := math.Hypot(al, abl)
	if c < 1e-3 {
		return base
	}
	s := chroma / c
	r, g, b := labToRGB(bl, al*s, abl*s)
	return premul(r, g, b, base.A)
}

// unpremulRGB separates a premultiplied color.RGBA into straight 8-bit components.
func unpremulRGB(c color.RGBA) (uint8, uint8, uint8) {
	switch c.A {
	case 0:
		return 0, 0, 0
	case 0xFF:
		return c.R, c.G, c.B
	}
	f := func(v uint8) uint8 { return uint8((uint32(v)*255 + uint32(c.A)/2) / uint32(c.A)) }
	return f(c.R), f(c.G), f(c.B)
}

// rgbToLAB converts a straight sRGB triple to CIE L*a*b* (D65).
func rgbToLAB(r, g, b uint8) (l, a, bb float64) {
	sr := float64(sRGBToLinear(r))
	sg := float64(sRGBToLinear(g))
	sb := float64(sRGBToLinear(b))
	x := (sr*0.4124 + sg*0.3576 + sb*0.1805) / 0.95047
	y := sr*0.2126 + sg*0.7152 + sb*0.0722
	z := (sr*0.0193 + sg*0.1192 + sb*0.9505) / 1.08883
	x, y, z = labF(x), labF(y), labF(z)
	return 116*y - 16, 500 * (x - y), 200 * (y - z)
}

// labToRGB converts CIE L*a*b* (D65) back to straight sRGB.
func labToRGB(l, a, b float64) (uint8, uint8, uint8) {
	fy := (l + 16) / 116
	fx := fy + a/500
	fz := fy - b/200
	x := labFInv(fx) * 0.95047
	y := labFInv(fy)
	z := labFInv(fz) * 1.08883
	r := x*3.2406 + y*-1.5372 + z*-0.4986
	g := x*-0.9689 + y*1.8758 + z*0.0415
	bl := x*0.0557 + y*-0.2040 + z*1.0570
	return linearToSRGB(float32(r)), linearToSRGB(float32(g)), linearToSRGB(float32(bl))
}

func labF(t float64) float64 {
	if t > 0.008856 {
		return math.Cbrt(t)
	}
	return 7.787*t + 16.0/116.0
}

func labFInv(t float64) float64 {
	c := t * t * t
	if c > 0.008856 {
		return c
	}
	return (t - 16.0/116.0) / 7.787
}

// premul returns an alpha-premultiplied color.RGBA, which is what image.RGBA stores and what the
// layered-window DIB needs (AC_SRC_ALPHA expects premultiplied bytes). Passing non-premultiplied
// components here would brighten translucent fills.
func premul(r, g, b, a uint8) color.RGBA {
	if a == 0xFF {
		return color.RGBA{R: r, G: g, B: b, A: a}
	}
	m := func(c uint8) uint8 { return uint8((uint32(c)*uint32(a) + 127) / 255) }
	return color.RGBA{R: m(r), G: m(g), B: m(b), A: a}
}
