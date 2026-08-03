package quoter

import (
	"image/color"
	"math"
	"strings"
	"time"
	"unicode"

	"github.com/fogleman/gg"
)

// Layout constants (all in canvas pixels at 1x; the canvas is 512px wide).
const (
	canvasWidth = 512 // sticker width (target after resize)
	renderScale = 2   // render at 2x for quality, then downscale via cwebp

	// Bubble geometry
	avatarSize   = 50  // avatar circle diameter
	avatarGap    = 8   // gap between avatar and bubble
	bubblePadX   = 14  // horizontal inner padding of the bubble
	bubblePadY   = 10  // vertical inner padding of the bubble
	bubbleRadius = 18  // bubble corner radius
	tailSize     = 10  // bubble tail size
	shadowPad    = 10  // extra canvas margin for shadow
	bubbleMinW   = 140 // minimum bubble width

	// Typography (point sizes at 1x scale)
	fontSizeName   = 14.0
	fontSizeText   = 15.0
	fontSizeTime   = 11.0
	fontSizeReply  = 11.0

	// Bubble bubble background — dark WhatsApp-style
	bgR, bgG, bgB = 18, 18, 18 // canvas/wallpaper background
)

// bubbleColor is the bubble background colour.
var bubbleColor = color.RGBA{R: 31, G: 47, B: 59, A: 255} // WhatsApp dark teal-grey

// textColor is the default message text colour.
var textColor = color.RGBA{R: 228, G: 228, B: 228, A: 255}

// timeColor is the timestamp + ticks colour.
var timeColor = color.RGBA{R: 145, G: 167, B: 179, A: 255}

// replyBarColor is the accent bar shown inside the reply preview.
var replyBarColor = color.RGBA{R: 0, G: 167, B: 131, A: 255} // WA green

// BubbleAvatar carries the data needed to render an avatar circle.
// It is populated by the command handler (quote.go) and consumed during rendering.
type BubbleAvatar struct {
	JIDUser     string // bare numeric JID, used for colour selection and cache key
	DisplayName string // used as initials fallback
	PhotoURL    string // CDN URL from GetProfilePictureURL; empty = use initials
}

// BubbleMessage holds all the data needed to render one chat bubble.
type BubbleMessage struct {
	// SenderJID is the bare numeric JID (e.g. "5521999999999") used for
	// avatar caching and name colour selection.
	SenderJID string

	// SenderName is the display name shown at the top of the bubble.
	SenderName string

	// Text is the plain-text message content.
	Text string

	// Timestamp is used to render the HH:MM in the bottom-right corner.
	Timestamp time.Time

	// IsFromMe controls whether the bubble sits on the right (true) or left (false).
	// v1: always false (all quotes render as "received" bubbles).
	IsFromMe bool

	// GroupPos indicates this bubble's position within a run of same-sender
	// messages: "single" | "first" | "middle" | "last".
	GroupPos string

	// ShowAvatar controls whether the avatar circle is rendered.
	// Only the last bubble in a same-sender run should show the avatar.
	ShowAvatar bool

	// Avatar carries the data for rendering the avatar circle.
	// If nil, no avatar is drawn.
	Avatar *BubbleAvatar
}

// renderBubble draws a single chat bubble and returns its height.
// dc is the destination canvas; yOffset is where to start drawing.
func renderBubble(dc *gg.Context, msg BubbleMessage, yOffset float64, s float64) float64 {
	nameCol := nameColorFor(msg.SenderJID)

	// ── Measure text ──────────────────────────────────────────────────────────
	maxBubbleW := float64(canvasWidth)*s - (float64(avatarSize)+float64(avatarGap))*s - float64(shadowPad)*s
	innerW := maxBubbleW - float64(bubblePadX)*s*2

	dc.SetFontFace(regularFace(fontSizeText * s))
	lines := wrapText(dc, msg.Text, innerW)

	// Measure text block height
	lineH := fontSizeText * s * 1.4
	textH := float64(len(lines)) * lineH

	// Measure name row
	dc.SetFontFace(boldFace(fontSizeName * s))
	nameW, nameH := dc.MeasureString(msg.SenderName)
	_ = nameH

	// Measure time string
	timeStr := formatTime(msg.Timestamp)
	dc.SetFontFace(regularFace(fontSizeTime * s))
	timeW, timeH := dc.MeasureString(timeStr + " ✓✓")
	_ = timeH

	// Measure actual text width for bubble width calculation
	dc.SetFontFace(regularFace(fontSizeText * s))
	maxLineW := 0.0
	for _, l := range lines {
		w, _ := dc.MeasureString(l)
		if w > maxLineW {
			maxLineW = w
		}
	}

	// Bubble width: max of (name, text, min)
	contentW := math.Max(nameW, math.Max(maxLineW, timeW+float64(bubblePadX)*s))
	contentW = math.Max(contentW, float64(bubbleMinW)*s)
	contentW = math.Min(contentW, maxBubbleW-float64(bubblePadX)*s)
	bubbleW := contentW + float64(bubblePadX)*s*2

	// Bubble height: name + gap + text + gap + time row
	gapAfterName := fontSizeName * s * 0.3
	gapBeforeTime := fontSizeText * s * 0.2
	timeRowH := fontSizeTime * s * 1.6
	bubbleH := float64(bubblePadY)*s + nameH + gapAfterName + textH + gapBeforeTime + timeRowH + float64(bubblePadY)*s

	// ── Position ─────────────────────────────────────────────────────────────
	// Left column is reserved for the avatar.
	bubbleX := (float64(avatarSize) + float64(avatarGap)) * s
	bubbleY := yOffset

	// ── Draw bubble background ────────────────────────────────────────────────
	drawBubble(dc, bubbleX, bubbleY, bubbleW, bubbleH, msg.GroupPos, s, bubbleColor)

	// ── Draw sender name ─────────────────────────────────────────────────────
	dc.SetFontFace(boldFace(fontSizeName * s))
	dc.SetColor(nameCol)
	nameX := bubbleX + float64(bubblePadX)*s
	nameY := bubbleY + float64(bubblePadY)*s + nameH // baseline
	dc.DrawString(msg.SenderName, nameX, nameY)

	// ── Draw message text ─────────────────────────────────────────────────────
	dc.SetFontFace(regularFace(fontSizeText * s))
	dc.SetColor(textColor)
	textX := nameX
	curY := nameY + gapAfterName + lineH
	for _, line := range lines {
		dc.DrawString(line, textX, curY)
		curY += lineH
	}

	// ── Draw timestamp + ticks ────────────────────────────────────────────────
	dc.SetFontFace(regularFace(fontSizeTime * s))
	dc.SetColor(timeColor)
	fullTime := timeStr + " ✓✓"
	tw, _ := dc.MeasureString(fullTime)
	timeX := bubbleX + bubbleW - float64(bubblePadX)*s - tw
	timeY := bubbleY + bubbleH - float64(bubblePadY)*s
	dc.DrawString(fullTime, timeX, timeY)

	// ── Draw avatar ───────────────────────────────────────────────────────────
	if msg.ShowAvatar && msg.Avatar != nil {
		avatarY := bubbleY + bubbleH - float64(avatarSize)*s
		avatarCx := float64(avatarSize) * s / 2
		avatarCy := avatarY + float64(avatarSize)*s/2

		// Fetch (or generate) the avatar image on the fly.
		avatarImg := fetchAvatar(
			nil, // context — nil is OK; http download skipped for nil ctx
			msg.Avatar.JIDUser,
			msg.Avatar.DisplayName,
			nil, // no whatsmeow client at render time; PhotoURL used instead
		)
		// If a PhotoURL was provided, prefer it. The caller already fetched it.
		if msg.Avatar.PhotoURL != "" {
			if img := downloadImageSync(msg.Avatar.PhotoURL); img != nil {
				avatarImg = clipCircle(img, avatarSize)
			}
		}
		dc.DrawImageAnchored(avatarImg, int(avatarCx), int(avatarCy), 0.5, 0.5)
	}

	return bubbleH
}

// drawBubble draws a rounded rectangle with a small tail at the bottom-left.
// groupPos affects which corners are sharp vs. rounded:
//
//	"single" → all corners rounded + tail
//	"first"  → top-left sharp (sits below a previous bubble)
//	"middle" → top-left and bottom-left sharp
//	"last"   → bottom-left sharp + tail
func drawBubble(dc *gg.Context, x, y, w, h float64, groupPos string, s float64, bg color.RGBA) {
	r := float64(bubbleRadius) * s
	rSmall := r * 0.25 // nearly square corner facing group neighbour
	tail := float64(tailSize) * s

	topLeft := r
	bottomLeft := r

	switch groupPos {
	case "first":
		topLeft = rSmall
	case "middle":
		topLeft = rSmall
		bottomLeft = rSmall
	case "last":
		bottomLeft = rSmall
	}

	// Soft drop shadow
	dc.SetColor(color.RGBA{0, 0, 0, 40})
	dc.DrawRoundedRectangle(x+2, y+3, w, h, r)
	dc.Fill()

	// Bubble body
	dc.SetColor(bg)
	drawRoundedRectWithVariableRadii(dc, x, y, w, h, topLeft, r, r, bottomLeft)
	dc.Fill()

	// Bubble tail (bottom-left triangle) — only for "single" and "last"
	if groupPos == "single" || groupPos == "last" {
		dc.SetColor(bg)
		dc.MoveTo(x, y+h-tail)
		dc.LineTo(x, y+h)
		dc.LineTo(x-tail*0.8, y+h+tail*0.5)
		dc.ClosePath()
		dc.Fill()
	}
}

// drawRoundedRectWithVariableRadii draws a rounded rect where each corner
// can have a different radius (tl = top-left, tr = top-right, br, bl).
func drawRoundedRectWithVariableRadii(dc *gg.Context, x, y, w, h, tl, tr, br, bl float64) {
	dc.NewSubPath()
	// Top-left corner
	dc.DrawArc(x+tl, y+tl, tl, math.Pi, 3*math.Pi/2)
	// Top-right corner
	dc.DrawArc(x+w-tr, y+tr, tr, 3*math.Pi/2, 2*math.Pi)
	// Bottom-right corner
	dc.DrawArc(x+w-br, y+h-br, br, 0, math.Pi/2)
	// Bottom-left corner
	dc.DrawArc(x+bl, y+h-bl, bl, math.Pi/2, math.Pi)
	dc.ClosePath()
}

// wrapText breaks text into lines that fit within maxWidth using the
// font face currently set on dc.
func wrapText(dc *gg.Context, text string, maxWidth float64) []string {
	if text == "" {
		return []string{""}
	}
	var lines []string
	for _, paragraph := range strings.Split(text, "\n") {
		words := strings.Fields(paragraph)
		if len(words) == 0 {
			lines = append(lines, "")
			continue
		}
		current := words[0]
		for _, word := range words[1:] {
			candidate := current + " " + word
			w, _ := dc.MeasureString(candidate)
			if w > maxWidth {
				lines = append(lines, current)
				current = word
			} else {
				current = candidate
			}
		}
		lines = append(lines, current)
	}
	return lines
}

// formatTime returns "HH:MM" for the given time, in local timezone.
func formatTime(t time.Time) string {
	return t.Format("15:04")
}

// isRTL returns true if the first significant rune in s is a right-to-left character.
func isRTL(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Arabic, r) || unicode.Is(unicode.Hebrew, r) {
			return true
		}
	}
	return false
}
