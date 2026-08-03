package quoter

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"

	"github.com/fogleman/gg"
)

const (
	// bubbleMarginSame is the vertical gap between same-sender bubbles.
	bubbleMarginSame = 2
	// bubbleMarginDiff is the vertical gap between different-sender bubbles.
	bubbleMarginDiff = 10
	// canvasPadTop/Bottom are the wallpaper insets around the bubble stack.
	canvasPadTop    = 14
	canvasPadBottom = 18
	canvasPadSide   = 6
)

// wallpaperTop and wallpaperBottom define the gradient colours for the background.
var (
	wallpaperTop    = color.RGBA{R: 10, G: 20, B: 26, A: 255}
	wallpaperBottom = color.RGBA{R: 18, G: 33, B: 40, A: 255}
)

// ComposePNG renders all messages into a single PNG image and returns the
// raw PNG bytes. The image is rendered at renderScale * canvasWidth pixels
// wide for quality; callers (e.g. quote.go) are responsible for resizing to
// 512 and converting to WebP via cwebp.
func ComposePNG(messages []BubbleMessage) ([]byte, error) {
	if err := loadFonts(); err != nil {
		return nil, fmt.Errorf("quoter: fonts not loaded: %w", err)
	}
	if len(messages) == 0 {
		return nil, fmt.Errorf("quoter: no messages to render")
	}

	s := float64(renderScale)

	// ── Pass 1: measure total height ──────────────────────────────────────────
	// We need a dummy canvas to measure text; width must match final canvas.
	totalCanvasW := float64(canvasWidth)*s + float64(canvasPadSide)*s*2
	dummy := gg.NewContext(int(totalCanvasW), 1)
	totalH := float64(canvasPadTop) * s

	type bubbleHeight struct {
		h      float64
		margin float64
	}
	heights := make([]bubbleHeight, len(messages))

	for i, msg := range messages {
		h := measureBubbleHeight(dummy, msg, s)
		margin := 0.0
		if i < len(messages)-1 {
			if messages[i].SenderJID == messages[i+1].SenderJID {
				margin = float64(bubbleMarginSame) * s
			} else {
				margin = float64(bubbleMarginDiff) * s
			}
		}
		heights[i] = bubbleHeight{h: h, margin: margin}
		totalH += h + margin
	}
	totalH += float64(canvasPadBottom) * s

	// ── Pass 2: create canvas and draw ────────────────────────────────────────
	dc := gg.NewContext(int(totalCanvasW), int(totalH))

	// Wallpaper gradient background.
	drawWallpaper(dc, int(totalCanvasW), int(totalH))

	// Draw each bubble.
	yOffset := float64(canvasPadTop) * s
	for i, msg := range messages {
		renderBubble(dc, msg, yOffset, s)
		yOffset += heights[i].h + heights[i].margin
	}

	// ── Encode to PNG ─────────────────────────────────────────────────────────
	var buf bytes.Buffer
	if err := png.Encode(&buf, dc.Image()); err != nil {
		return nil, fmt.Errorf("quoter: png encode: %w", err)
	}
	return buf.Bytes(), nil
}

// measureBubbleHeight returns the pixel height of a bubble without drawing it.
func measureBubbleHeight(dc *gg.Context, msg BubbleMessage, s float64) float64 {
	maxBubbleW := float64(canvasWidth)*s - (float64(avatarSize)+float64(avatarGap))*s - float64(shadowPad)*s
	innerW := maxBubbleW - float64(bubblePadX)*s*2

	dc.SetFontFace(regularFace(fontSizeText * s))
	lines := wrapText(dc, msg.Text, innerW)

	lineH := fontSizeText * s * 1.4
	textH := float64(len(lines)) * lineH

	dc.SetFontFace(boldFace(fontSizeName * s))
	_, nameH := dc.MeasureString(msg.SenderName)

	gapAfterName := fontSizeName * s * 0.3
	gapBeforeTime := fontSizeText * s * 0.2
	timeRowH := fontSizeTime * s * 1.6

	return float64(bubblePadY)*s + nameH + gapAfterName + textH + gapBeforeTime + timeRowH + float64(bubblePadY)*s
}

// drawWallpaper fills the canvas with a vertical dark gradient, mimicking the
// WhatsApp dark-mode chat background.
func drawWallpaper(dc *gg.Context, w, h int) {
	grad := gg.NewLinearGradient(0, 0, 0, float64(h))
	grad.AddColorStop(0, wallpaperTop)
	grad.AddColorStop(1, wallpaperBottom)
	dc.SetFillStyle(grad)
	dc.DrawRectangle(0, 0, float64(w), float64(h))
	dc.Fill()

	// Subtle dot pattern overlay (every 12px grid).
	dc.SetColor(color.RGBA{255, 255, 255, 8})
	for y := 0; y < h; y += 12 {
		for x := 0; x < w; x += 12 {
			dc.DrawCircle(float64(x), float64(y), 0.8)
			dc.Fill()
		}
	}
}

// gg.Image is satisfied by image.Image with an At method — check here.
var _ image.Image = (*gg.Context)(nil).Image()
