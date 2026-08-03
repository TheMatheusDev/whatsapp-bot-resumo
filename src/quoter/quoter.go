// Package quoter provides the WhatsApp quote-bubble image generator.
// It renders one or more chat messages into a PNG image that mimics the
// look of a WhatsApp dark-mode conversation, then exposes the raw PNG bytes
// so the caller can convert them to a 512×512 WebP sticker.
//
// Usage:
//
//	msgs := []quoter.BubbleMessage{
//	    {SenderJID: "5521999...", SenderName: "João", Text: "olá!", Timestamp: time.Now(), ShowAvatar: true},
//	}
//	pngBytes, err := quoter.ComposePNG(msgs)
package quoter
