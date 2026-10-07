package render

import "image/color"

// Theme is the pill's palette, mirroring the phone's Material 3 tokens in
// theme/Color.kt so the desktop pill and the phone card read as the same object.
type Theme struct {
	Surface          color.RGBA // the card background
	Inset            color.RGBA // a recessed area (the panel's live-text box)
	Border           color.RGBA // 1px hairline around the card
	OnSurface        color.RGBA // settled text — the phone's near-white
	OnSurfaceVariant color.RGBA // live tail + status label — the phone's grey
	Primary          color.RGBA // status dot / accent / selected control
	OnPrimary        color.RGBA // text on a filled (selected) control
	Error            color.RGBA // status dot + label on failure
	Shadow           color.RGBA // drop shadow under the card
}

// DarkTheme is the phone's dark scheme: violet-tinted near-black with a lilac accent.
var DarkTheme = Theme{
	Surface:          premul(0x2E, 0x29, 0x37, 0xF2), // SurfaceContainerHigh
	Inset:            premul(0x23, 0x1F, 0x2A, 0xFF), // SurfaceContainer
	Border:           premul(0x4A, 0x44, 0x53, 0xC0), // OutlineVariant
	OnSurface:        premul(0xEA, 0xE0, 0xE7, 0xFF),
	OnSurfaceVariant: premul(0xCC, 0xC2, 0xCE, 0xFF),
	Primary:          premul(0xD0, 0xBC, 0xFF, 0xFF),
	OnPrimary:        premul(0x3A, 0x2A, 0x5C, 0xFF),
	Error:            premul(0xFF, 0xB4, 0xAB, 0xFF),
	Shadow:           premul(0x00, 0x00, 0x00, 0x50),
}

// LightTheme is the phone's warm-cream light scheme with a deep lilac accent.
var LightTheme = Theme{
	Surface:          premul(0xEC, 0xE4, 0xD5, 0xF2), // SurfaceContainerHigh
	Inset:            premul(0xF2, 0xEB, 0xDD, 0xFF), // SurfaceContainer
	Border:           premul(0xD8, 0xCC, 0xB9, 0xC0), // OutlineVariant
	OnSurface:        premul(0x2B, 0x26, 0x22, 0xFF),
	OnSurfaceVariant: premul(0x4E, 0x45, 0x3C, 0xFF),
	Primary:          premul(0x6D, 0x4F, 0xA8, 0xFF),
	OnPrimary:        premul(0xFF, 0xFF, 0xFF, 0xFF),
	Error:            premul(0xBA, 0x1A, 0x1A, 0xFF),
	Shadow:           premul(0x00, 0x00, 0x00, 0x40),
}

// ThemeFor returns the palette for the requested appearance.
func ThemeFor(dark bool) Theme {
	if dark {
		return DarkTheme
	}
	return LightTheme
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
