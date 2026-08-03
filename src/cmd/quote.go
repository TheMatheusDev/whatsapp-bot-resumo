package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	waE2E "go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"whatsapp-summarizer/src/quoter"
)

const (
	quoteMaxMessages = 20  // cap on N to prevent abuse
	quoteMinMessages = 1
)

// handleQuoteCommand is the entry-point for !quote / !q.
//
// Usage:
//
//	!quote         → 1 bubble (the message being replied to)
//	!quote 5       → 5 bubbles: the replied message + 4 most-recent messages
//	                  after it in the same chat
func (h *Handler) handleQuoteCommand(args []string, msgTrigger types.MessageInfo, msg *waE2E.Message) {
	// The command must be a reply to another message.
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	contextInfo := extractContextInfo(msg)
	if contextInfo == nil || contextInfo.GetQuotedMessage() == nil {
		h.whatsappService.ReactToMessage(ctx, msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID, "❌")
		h.whatsappService.SendMessageReply(
			msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID,
			"Responda a uma mensagem com *!quote* para criar o balão.\nExemplo: *!quote 3* para incluir 3 mensagens.",
		)
		return
	}

	// Parse optional N argument.
	n := 1
	if len(args) > 0 {
		if parsed, err := strconv.Atoi(strings.TrimSpace(args[0])); err == nil {
			n = parsed
		}
	}
	if n < quoteMinMessages {
		n = quoteMinMessages
	}
	if n > quoteMaxMessages {
		n = quoteMaxMessages
	}

	h.wg.Add(1)
	go func() {
		defer h.wg.Done()
		if err := h.generateAndSendQuote(ctx, msgTrigger, contextInfo, n); err != nil {
			h.logger.Error("Quote: failed", "error", err)
			h.whatsappService.ReactToMessage(ctx, msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID, "❌")
			h.whatsappService.SendMessageReply(
				msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID,
				"❌ Falha ao gerar o quote. Tente novamente.",
			)
		}
	}()
}

// generateAndSendQuote does the heavy lifting: builds BubbleMessage list,
// renders PNG, converts to WebP sticker, uploads and sends.
func (h *Handler) generateAndSendQuote(
	ctx context.Context,
	msgTrigger types.MessageInfo,
	ctxInfo *waE2E.ContextInfo,
	n int,
) error {
	h.whatsappService.ReactToMessage(ctx, msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID, "⏳")

	messages, err := h.buildBubbleMessages(ctx, msgTrigger, ctxInfo, n)
	if err != nil {
		return fmt.Errorf("build bubbles: %w", err)
	}
	if len(messages) == 0 {
		return fmt.Errorf("no messages to render")
	}

	// Assign groupPos so consecutive same-sender bubbles share styling.
	assignGroupPos(messages)

	// Render to PNG.
	pngBytes, err := quoter.ComposePNG(messages)
	if err != nil {
		return fmt.Errorf("render PNG: %w", err)
	}

	// Write PNG to tmp file, then convert via existing sticker pipeline.
	tmpPNG, err := writeTempFile("quote_", ".png", pngBytes)
	if err != nil {
		return fmt.Errorf("write tmp PNG: %w", err)
	}
	defer os.Remove(tmpPNG)

	webpPath, err := h.convertImageToSticker(tmpPNG)
	if err != nil {
		return fmt.Errorf("convert to sticker: %w", err)
	}
	defer os.Remove(webpPath)

	// Inject EXIF metadata (non-fatal).
	if out, err := runWebPMux(webpPath); err != nil {
		h.logger.Warn("Quote: webpmux metadata injection failed", "error", err, "output", string(out))
	}

	// Upload and send.
	if err := h.uploadAndSendSticker(msgTrigger, webpPath, false); err != nil {
		return fmt.Errorf("upload sticker: %w", err)
	}

	h.whatsappService.ReactToMessage(ctx, msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID, "✅")
	h.logger.Info("Quote: sticker sent", "chat", msgTrigger.Chat.String(), "n", len(messages))
	return nil
}

// buildBubbleMessages constructs the ordered list of BubbleMessages.
//
//	messages[0] = the quoted message (from ContextInfo)
//	messages[1..n-1] = the n-1 most-recent DB messages in the chat
func (h *Handler) buildBubbleMessages(
	ctx context.Context,
	msgTrigger types.MessageInfo,
	ctxInfo *waE2E.ContextInfo,
	n int,
) ([]quoter.BubbleMessage, error) {
	var result []quoter.BubbleMessage

	// ── Bubble 0: the quoted message ─────────────────────────────────────────
	quotedMsg := ctxInfo.GetQuotedMessage()
	quotedText := extractPlainText(quotedMsg)
	quotedSenderJID := strings.TrimSuffix(ctxInfo.GetParticipant(), "@s.whatsapp.net")
	quotedSenderJID = strings.TrimSuffix(quotedSenderJID, "@lid")
	// No display name available from ContextInfo — use the bare JID number.
	// If the user is in the DB from prior messages, we could look them up, but
	// we don't have chatID here. JID is readable enough for a quote bubble.
	quotedName := quotedSenderJID
	if quotedName == "" {
		quotedName = "Unknown"
	}

	quotedAvatar := h.fetchAvatarFor(ctx, quotedSenderJID, quotedName)

	// Use the !quote command's timestamp minus a second as a proxy for the
	// quoted message's time (we don't have the original timestamp from ContextInfo).
	quotedTime := msgTrigger.Timestamp.Add(-time.Duration(n) * time.Second)

	result = append(result, quoter.BubbleMessage{
		SenderJID:  quotedSenderJID,
		SenderName: quotedName,
		Text:       quotedText,
		Timestamp:  quotedTime,
		ShowAvatar: true,
		Avatar:     quotedAvatar,
	})

	// ── Bubbles 1..n-1: most-recent DB messages ───────────────────────────────
	if n > 1 {
		dbMsgs, err := h.dbService.GetGroupMessages(msgTrigger.Chat.User, n-1)
		if err != nil {
			h.logger.Warn("Quote: failed to fetch follow-up messages from DB", "error", err)
			// Not fatal — return just the quoted bubble.
			return result, nil
		}

		// GetGroupMessages returns messages in DESC order (newest first); reverse to chrono.
		for i, j := 0, len(dbMsgs)-1; i < j; i, j = i+1, j-1 {
			dbMsgs[i], dbMsgs[j] = dbMsgs[j], dbMsgs[i]
		}

		for _, dbMsg := range dbMsgs {
			avatar := h.fetchAvatarFor(ctx, dbMsg.SenderLID, dbMsg.Sender)
			result = append(result, quoter.BubbleMessage{
				SenderJID:  dbMsg.SenderLID,
				SenderName: dbMsg.Sender,
				Text:       dbMsg.Content,
				Timestamp:  dbMsg.Timestamp,
				ShowAvatar: true,
				Avatar:     avatar,
			})
		}
	}

	return result, nil
}

// assignGroupPos fills in the GroupPos and ShowAvatar fields based on
// consecutive same-sender runs, exactly like the Quotly logic.
func assignGroupPos(msgs []quoter.BubbleMessage) {
	for i := range msgs {
		prevSame := i > 0 && msgs[i-1].SenderJID == msgs[i].SenderJID
		nextSame := i < len(msgs)-1 && msgs[i+1].SenderJID == msgs[i].SenderJID

		switch {
		case prevSame && nextSame:
			msgs[i].GroupPos = "middle"
		case prevSame:
			msgs[i].GroupPos = "last"
		case nextSame:
			msgs[i].GroupPos = "first"
		default:
			msgs[i].GroupPos = "single"
		}

		// Avatar only for the last bubble of a same-sender run.
		msgs[i].ShowAvatar = !nextSame
	}
}


// fetchAvatarFor fetches (or generates) an avatar for the given JID user.
// Returns nil if the quoter package Avatar type is not yet wired up.
func (h *Handler) fetchAvatarFor(ctx context.Context, jidUser, displayName string) *quoter.BubbleAvatar {
	url := ""
	if jidUser != "" {
		jidFull := jidUser
		if !strings.Contains(jidFull, "@") {
			jidFull = jidFull + "@s.whatsapp.net"
		}
		if jid, err := types.ParseJID(jidFull); err == nil {
			if u, err := h.whatsappService.GetProfilePictureURL(ctx, jid); err == nil {
				url = u
			}
		}
	}
	return &quoter.BubbleAvatar{
		JIDUser:     jidUser,
		DisplayName: displayName,
		PhotoURL:    url,
	}
}

// extractContextInfo extracts the ContextInfo from any supported message type.
func extractContextInfo(msg *waE2E.Message) *waE2E.ContextInfo {
	if ext := msg.GetExtendedTextMessage(); ext != nil {
		return ext.GetContextInfo()
	}
	if img := msg.GetImageMessage(); img != nil {
		return img.GetContextInfo()
	}
	if vid := msg.GetVideoMessage(); vid != nil {
		return vid.GetContextInfo()
	}
	if aud := msg.GetAudioMessage(); aud != nil {
		return aud.GetContextInfo()
	}
	return nil
}

// extractPlainText extracts the plain-text content from a quoted proto message.
func extractPlainText(msg *waE2E.Message) string {
	if msg == nil {
		return ""
	}
	if t := msg.GetConversation(); t != "" {
		return t
	}
	if ext := msg.GetExtendedTextMessage(); ext != nil && ext.GetText() != "" {
		return ext.GetText()
	}
	if img := msg.GetImageMessage(); img != nil {
		if c := img.GetCaption(); c != "" {
			return "[Imagem] " + c
		}
		return "[Imagem]"
	}
	if vid := msg.GetVideoMessage(); vid != nil {
		if c := vid.GetCaption(); c != "" {
			return "[Vídeo] " + c
		}
		return "[Vídeo]"
	}
	if msg.GetAudioMessage() != nil {
		return "[Áudio]"
	}
	if msg.GetStickerMessage() != nil {
		return "[Sticker]"
	}
	return "[Mensagem]"
}

// writeTempFile writes data to a temp file with the given prefix and suffix.
func writeTempFile(prefix, suffix string, data []byte) (string, error) {
	if err := os.MkdirAll("tmp", 0755); err != nil {
		return "", fmt.Errorf("create tmp dir: %w", err)
	}
	name := filepath.Join("tmp", fmt.Sprintf("%s%d%s", prefix, time.Now().UnixNano(), suffix))
	if err := os.WriteFile(name, data, 0644); err != nil {
		return "", fmt.Errorf("write temp file: %w", err)
	}
	return name, nil
}

// runWebPMux injects the EXIF metadata blob into a WebP file (non-fatal helper).
func runWebPMux(webpPath string) ([]byte, error) {
	out, err := exec.Command(
		StickerDeps.WebPMux,
		"-set", "exif", stickerExifPath(),
		webpPath,
		"-o", webpPath,
	).CombinedOutput()
	return out, err
}

// handleQuoteCommandSynth is kept for future use (unused import guard).
func (h *Handler) handleQuoteCommandSynth(_ *events.Message) {}
