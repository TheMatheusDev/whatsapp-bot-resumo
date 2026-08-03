package quoter

import (
	"context"
	"image"
	"image/color"
	"image/draw"
	"net/http"
	"sync"
	"time"

	"github.com/fogleman/gg"
	"go.mau.fi/whatsmeow"
	watypes "go.mau.fi/whatsmeow/types"
)

// avatarCacheEntry holds a cached avatar image and its expiry time.
type avatarCacheEntry struct {
	img       image.Image
	expiresAt time.Time
}

var (
	avatarCache   sync.Map          // map[string]avatarCacheEntry
	avatarCacheTTL = 5 * time.Minute
)

// avatarGetter is the interface needed to fetch profile picture info.
// Satisfied by *whatsmeow.Client.
type avatarGetter interface {
	GetProfilePictureInfo(jid watypes.JID, params *whatsmeow.GetProfilePictureParams) (*watypes.ProfilePictureInfo, error)
}

// fetchAvatar returns a circular avatar image for the given user.
// It tries to download the profile picture; on failure it falls back to an
// initials-over-gradient avatar. The result is cached for 5 minutes.
// ctx and client may be nil — in that case only the cache and initials are used.
func fetchAvatar(ctx context.Context, jidUser, displayName string, client avatarGetter) image.Image {
	// Check cache first.
	if v, ok := avatarCache.Load(jidUser); ok {
		entry := v.(avatarCacheEntry)
		if time.Now().Before(entry.expiresAt) {
			return entry.img
		}
	}

	var img image.Image

	// Try to fetch the profile picture.
	if client != nil && ctx != nil {
		jid, err := watypes.ParseJID(jidUser + "@s.whatsapp.net")
		if err == nil {
			info, err := client.GetProfilePictureInfo(jid, &whatsmeow.GetProfilePictureParams{
				Preview: false,
			})
			if err == nil && info != nil && info.URL != "" {
				img = downloadImage(ctx, info.URL)
			}
		}
	}

	// Fallback: initials avatar.
	if img == nil {
		img = initialsAvatar(displayName, nameColorFor(jidUser))
	}

	// Clip to circle and cache.
	circled := clipCircle(img, avatarSize)
	avatarCache.Store(jidUser, avatarCacheEntry{img: circled, expiresAt: time.Now().Add(avatarCacheTTL)})
	return circled
}

// downloadImage fetches an image from a URL and decodes it.
func downloadImage(ctx context.Context, url string) image.Image {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	img, _, err := image.Decode(resp.Body)
	if err != nil {
		return nil
	}
	return img
}

// downloadImageSync downloads an image without requiring a caller-supplied context.
// Used during rendering when the caller already resolved the URL.
func downloadImageSync(url string) image.Image {
	resp, err := http.Get(url) //nolint:noctx // intentional: called from render path
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	img, _, err := image.Decode(resp.Body)
	if err != nil {
		return nil
	}
	return img
}

// initialsAvatar creates a square image with a gradient background and the
// first one or two initials of the display name centred on it.
func initialsAvatar(name string, bg color.RGBA) image.Image {
	const size = 256
	dc := gg.NewContext(size, size)

	// Gradient background: darker at bottom-right.
	grad := gg.NewLinearGradient(0, 0, size, size)
	grad.AddColorStop(0, colorBrighter(bg, 1.25))
	grad.AddColorStop(1, bg)
	dc.SetFillStyle(grad)
	dc.DrawRectangle(0, 0, size, size)
	dc.Fill()

	// Initials text.
	initials := extractInitials(name)
	fontSize := 96.0
	if len([]rune(initials)) > 1 {
		fontSize = 80.0
	}
	dc.SetFontFace(boldFace(fontSize))
	dc.SetColor(color.White)
	dc.DrawStringAnchored(initials, size/2, size/2, 0.5, 0.5)

	return dc.Image()
}

// extractInitials returns up to two uppercase initials from a display name.
func extractInitials(name string) string {
	runes := []rune(name)
	if len(runes) == 0 {
		return "?"
	}
	words := splitWords(name)
	if len(words) == 0 {
		return string([]rune(name)[:1])
	}
	if len(words) == 1 {
		return string([]rune(words[0])[:1])
	}
	first := []rune(words[0])
	last := []rune(words[len(words)-1])
	return string(first[:1]) + string(last[:1])
}

// splitWords splits a name into words, ignoring empty strings.
func splitWords(s string) []string {
	var words []string
	current := []rune{}
	for _, r := range s {
		if r == ' ' || r == '\t' {
			if len(current) > 0 {
				words = append(words, string(current))
				current = current[:0]
			}
		} else {
			current = append(current, r)
		}
	}
	if len(current) > 0 {
		words = append(words, string(current))
	}
	return words
}

// colorBrighter returns a brighter version of a colour by multiplying the
// RGB channels by the given factor (clamped to 255).
func colorBrighter(c color.RGBA, factor float64) color.RGBA {
	clamp := func(v float64) uint8 {
		if v > 255 {
			return 255
		}
		return uint8(v)
	}
	return color.RGBA{
		R: clamp(float64(c.R) * factor),
		G: clamp(float64(c.G) * factor),
		B: clamp(float64(c.B) * factor),
		A: c.A,
	}
}

// clipCircle scales src to side×side and clips it to a circle.
func clipCircle(src image.Image, side int) image.Image {
	// Scale the source into a gg context of the target size.
	dc2 := gg.NewContext(side, side)
	dc2.DrawImage(src, 0, 0)
	scaled := dc2.Image()

	// Build a circular mask.
	maskDC := gg.NewContext(side, side)
	maskDC.DrawCircle(float64(side)/2, float64(side)/2, float64(side)/2)
	maskDC.Fill()

	out := image.NewRGBA(image.Rect(0, 0, side, side))
	draw.DrawMask(out, out.Bounds(), scaled, image.Point{}, &circularMask{maskDC.Image(), side}, image.Point{}, draw.Over)
	return out
}

// circularMask implements image.Image as a grayscale mask derived from a gg canvas.
type circularMask struct {
	src  image.Image
	side int
}

func (m *circularMask) ColorModel() color.Model { return color.AlphaModel }
func (m *circularMask) Bounds() image.Rectangle { return image.Rect(0, 0, m.side, m.side) }
func (m *circularMask) At(x, y int) color.Color {
	_, _, _, a := m.src.At(x, y).RGBA()
	return color.Alpha{A: uint8(a >> 8)}
}
