package quoter

import "image/color"

// nameColors is a palette of 7 accent colors used for sender names.
// Dark-background version (same hues as Quotly's NAME_COLORS_DARK).
var nameColors = []color.RGBA{
	{R: 0xFF, G: 0x6B, B: 0x6B, A: 0xFF}, // coral-red
	{R: 0xFF, G: 0xA5, B: 0x00, A: 0xFF}, // amber
	{R: 0xFF, G: 0xD7, B: 0x00, A: 0xFF}, // gold
	{R: 0x7C, G: 0xE3, B: 0x8A, A: 0xFF}, // mint-green
	{R: 0x4D, G: 0xC0, B: 0xFF, A: 0xFF}, // sky-blue
	{R: 0xBB, G: 0x86, B: 0xFC, A: 0xFF}, // lavender
	{R: 0xFF, G: 0x79, B: 0xC6, A: 0xFF}, // pink
}

// nameColorFor returns a deterministic accent color for a JID user string.
// It hashes the string and picks from the 7-color palette.
func nameColorFor(jidUser string) color.RGBA {
	h := uint64(5381)
	for _, c := range jidUser {
		h = (h << 5) + h + uint64(c)
	}
	return nameColors[h%uint64(len(nameColors))]
}

// luminance returns the relative luminance of a colour (0–255 scale).
func luminance(c color.RGBA) float64 {
	toLinear := func(ch uint8) float64 {
		v := float64(ch) / 255.0
		if v <= 0.03928 {
			return v / 12.92
		}
		return ((v + 0.055) / 1.055) * ((v + 0.055) / 1.055)
	}
	return 0.2126*toLinear(c.R) + 0.7152*toLinear(c.G) + 0.0722*toLinear(c.B)
}

// colorWithAlpha returns the RGBA colour with an explicit alpha value (0–255).
func colorWithAlpha(c color.RGBA, a uint8) color.RGBA {
	return color.RGBA{R: c.R, G: c.G, B: c.B, A: a}
}
