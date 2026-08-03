package quoter

import (
	"embed"
	"fmt"
	"sync"

	"github.com/golang/freetype/truetype"
	"golang.org/x/image/font"
)

//go:embed assets/fonts/Roboto-Regular.ttf
//go:embed assets/fonts/Roboto-Bold.ttf
//go:embed assets/fonts/Roboto-Medium.ttf
var fontFS embed.FS

var (
	fontOnce    sync.Once
	regularFont *truetype.Font
	boldFont    *truetype.Font
	mediumFont  *truetype.Font
	fontLoadErr error
)

// loadFonts loads all bundled fonts once. Subsequent calls are no-ops.
func loadFonts() error {
	fontOnce.Do(func() {
		var data []byte

		data, fontLoadErr = fontFS.ReadFile("assets/fonts/Roboto-Regular.ttf")
		if fontLoadErr != nil {
			fontLoadErr = fmt.Errorf("quoter: load regular font: %w", fontLoadErr)
			return
		}
		regularFont, fontLoadErr = truetype.Parse(data)
		if fontLoadErr != nil {
			fontLoadErr = fmt.Errorf("quoter: parse regular font: %w", fontLoadErr)
			return
		}

		data, fontLoadErr = fontFS.ReadFile("assets/fonts/Roboto-Bold.ttf")
		if fontLoadErr != nil {
			fontLoadErr = fmt.Errorf("quoter: load bold font: %w", fontLoadErr)
			return
		}
		boldFont, fontLoadErr = truetype.Parse(data)
		if fontLoadErr != nil {
			fontLoadErr = fmt.Errorf("quoter: parse bold font: %w", fontLoadErr)
			return
		}

		data, fontLoadErr = fontFS.ReadFile("assets/fonts/Roboto-Medium.ttf")
		if fontLoadErr != nil {
			fontLoadErr = fmt.Errorf("quoter: load medium font: %w", fontLoadErr)
			return
		}
		mediumFont, fontLoadErr = truetype.Parse(data)
		if fontLoadErr != nil {
			fontLoadErr = fmt.Errorf("quoter: parse medium font: %w", fontLoadErr)
			return
		}
	})
	return fontLoadErr
}

// faceFor returns a font.Face for the given TTF and DPI-scaled size.
func faceFor(ttf *truetype.Font, size float64) font.Face {
	return truetype.NewFace(ttf, &truetype.Options{
		Size:    size,
		DPI:     72,
		Hinting: font.HintingFull,
	})
}

// regularFace returns a Roboto Regular face at the given point size.
func regularFace(size float64) font.Face { return faceFor(regularFont, size) }

// boldFace returns a Roboto Bold face at the given point size.
func boldFace(size float64) font.Face { return faceFor(boldFont, size) }

// mediumFace returns a Roboto Medium face at the given point size.
func mediumFace(size float64) font.Face { return faceFor(mediumFont, size) }
