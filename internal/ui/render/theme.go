package render

import "image/color"

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
type ThemePreset struct {
	ID        string
	Name      string
	Hex       string
	DarkPrim  color.RGBA
	LightPrim color.RGBA
}

// Presets contains the 11 curated M3 presets from the Android app (DynamicTheme.kt).
var Presets = []ThemePreset{
	{ID: "cyan", Name: "Cyan", Hex: "#00B4D8", DarkPrim: premul(0x00, 0xD2, 0xF8, 0xFF), LightPrim: premul(0x00, 0x84, 0xA1, 0xFF)},
	{ID: "sky", Name: "Sky", Hex: "#38BDF8", DarkPrim: premul(0x38, 0xBD, 0xF8, 0xFF), LightPrim: premul(0x02, 0x84, 0xC7, 0xFF)},
	{ID: "slate", Name: "Slate", Hex: "#64748B", DarkPrim: premul(0x94, 0xA3, 0xB8, 0xFF), LightPrim: premul(0x47, 0x55, 0x69, 0xFF)},
	{ID: "teal", Name: "Teal", Hex: "#0D9488", DarkPrim: premul(0x2D, 0xD4, 0xBF, 0xFF), LightPrim: premul(0x0F, 0x76, 0x6E, 0xFF)},
	{ID: "amber", Name: "Amber", Hex: "#F59E0B", DarkPrim: premul(0xFB, 0xBF, 0x24, 0xFF), LightPrim: premul(0xD9, 0x77, 0x06, 0xFF)},
	{ID: "orange", Name: "Orange", Hex: "#FF9248", DarkPrim: premul(0xFF, 0x92, 0x48, 0xFF), LightPrim: premul(0xEA, 0x58, 0x0C, 0xFF)},
	{ID: "blue", Name: "Blue", Hex: "#1A73E8", DarkPrim: premul(0x60, 0xA5, 0xFA, 0xFF), LightPrim: premul(0x1D, 0x4E, 0xD8, 0xFF)},
	{ID: "green", Name: "Green", Hex: "#34A853", DarkPrim: premul(0x4A, 0xDE, 0x80, 0xFF), LightPrim: premul(0x16, 0xA3, 0x4A, 0xFF)},
	{ID: "coral", Name: "Coral", Hex: "#FA5B52", DarkPrim: premul(0xFB, 0x71, 0x85, 0xFF), LightPrim: premul(0xE1, 0x1D, 0x48, 0xFF)},
	{ID: "lilac", Name: "Lilac", Hex: "#D0BCFF", DarkPrim: premul(0xD0, 0xBC, 0xFF, 0xFF), LightPrim: premul(0x6D, 0x4F, 0xA8, 0xFF)},
	{ID: "mint", Name: "Mint", Hex: "#26A69A", DarkPrim: premul(0x4D, 0xD0, 0xE1, 0xFF), LightPrim: premul(0x00, 0x89, 0x7B, 0xFF)},
}

// ThemeForPreset returns the theme for the chosen preset and light/dark mode.
func ThemeForPreset(id string, dark bool) Theme {
	base := ThemeFor(dark)
	for _, p := range Presets {
		if p.ID == id {
			if dark {
				base.Primary = p.DarkPrim
				base.OnPrimary = premul(0x00, 0x1A, 0x24, 0xFF)
			} else {
				base.Primary = p.LightPrim
				base.OnPrimary = premul(0xFF, 0xFF, 0xFF, 0xFF)
			}
			break
		}
	}
	return base
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
