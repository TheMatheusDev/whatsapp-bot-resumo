package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	waE2E "go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types/events"

	wstypes "whatsapp-summarizer/src/types"
)

// chatCooldown is the minimum interval between chatbot responses per user.
// Prevents a single user from flooding the bot with mentions/replies.
const chatCooldown = 5 * time.Second

// chatContextMessages is the number of recent messages (including bot messages)
// sent to the AI model as conversation context.
const chatContextMessages = 100

// extractContextInfo returns the ContextInfo embedded in any supported message
// type, or nil if none is present. Used to detect mentions and replies.
func extractContextInfo(msg *waE2E.Message) *waE2E.ContextInfo {
	switch {
	case msg.GetExtendedTextMessage() != nil:
		return msg.GetExtendedTextMessage().GetContextInfo()
	case msg.GetImageMessage() != nil:
		return msg.GetImageMessage().GetContextInfo()
	case msg.GetVideoMessage() != nil:
		return msg.GetVideoMessage().GetContextInfo()
	case msg.GetAudioMessage() != nil:
		return msg.GetAudioMessage().GetContextInfo()
	case msg.GetDocumentMessage() != nil:
		return msg.GetDocumentMessage().GetContextInfo()
	case msg.GetStickerMessage() != nil:
		return msg.GetStickerMessage().GetContextInfo()
	default:
		return nil
	}
}

// isBotMentioned reports whether the bot's own JID appears in the MentionedJID
// list of the given message. Returns false when there is no ContextInfo.
//
// JID comparison uses the bare User part (no server suffix) to avoid mismatches
// between @s.whatsapp.net and @lid representations.
func (h *Handler) isBotMentioned(msg *waE2E.Message) bool {
	ctx := extractContextInfo(msg)
	if ctx == nil {
		return false
	}

	for _, jid := range ctx.GetMentionedJID() {
		// MentionedJID entries are "user@server"; split on '@' and compare only
		// the user part to be resilient against PN→LID migration differences.
		user := jid
		if idx := strings.Index(jid, "@"); idx >= 0 {
			user = jid[:idx]
		}
		if user == h.botLID {
			return true
		}
	}
	return false
}

// isReplyToBot reports whether the message is a reply to a message originally
// sent by the bot. It does this by comparing ContextInfo.Participant (the author
// of the quoted message) against the bot's own LID.
func (h *Handler) isReplyToBot(msg *waE2E.Message) bool {
	ctx := extractContextInfo(msg)
	if ctx == nil || ctx.GetStanzaID() == "" {
		return false
	}

	participant := ctx.GetParticipant()
	// Participant is "user@server"; extract the user part for comparison.
	if idx := strings.Index(participant, "@"); idx >= 0 {
		participant = participant[:idx]
	}
	return participant == h.botLID
}

// checkChatRateLimit returns the remaining cooldown duration for the sender
// in the chatbot rate limiter, or 0 if the user may proceed.
// When the user may proceed, it also records the current timestamp so the next
// call sees the cooldown.
func (h *Handler) checkChatRateLimit(chatUser, senderUser string) time.Duration {
	key := chatUser + ":" + senderUser
	now := time.Now()
	if v, loaded := h.chatRateLimitCache.Load(key); loaded {
		if last, ok := v.(time.Time); ok {
			if remaining := chatCooldown - now.Sub(last); remaining > 0 {
				return remaining
			}
		}
	}
	h.chatRateLimitCache.Store(key, now)
	return 0
}

// handleChatResponse is the entry point for the chatbot feature.
// It is called (in a dedicated goroutine) whenever the bot is mentioned or
// receives a reply in a group message.
//
// Flow:
//  1. Enforce per-user rate limit (5 s cooldown).
//  2. Flush the batch writer so the triggering message is already in the DB.
//  3. Fetch the last 100 messages including bot messages for context.
//  4. Call AIService.ChatResponse with the group's personality.
//  5. Send the response as a reply to the user's triggering message.
func (h *Handler) handleChatResponse(evt *events.Message) {
	msgTrigger := evt.Info

	// Enforce rate limit before doing any expensive work.
	if wait := h.checkChatRateLimit(msgTrigger.Chat.User, msgTrigger.Sender.User); wait > 0 {
		h.logger.Debug("handleChatResponse: rate limited",
			"chat", msgTrigger.Chat.User,
			"sender", msgTrigger.Sender.User,
			"wait", wait)
		return
	}

	h.logger.Info("handleChatResponse: triggered",
		"chat", msgTrigger.Chat.User,
		"sender", msgTrigger.Sender.User,
		"msg_id", msgTrigger.ID)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Flush so the triggering message is visible to the context query.
	h.dbService.FlushPendingMessages()

	messages, err := h.dbService.GetGroupMessagesWithBot(msgTrigger.Chat.User, chatContextMessages)
	if err != nil {
		h.logger.Error("handleChatResponse: failed to fetch context messages", "error", err)
		return
	}

	if len(messages) == 0 {
		h.logger.Warn("handleChatResponse: no messages in context, skipping response",
			"chat", msgTrigger.Chat.User)
		return
	}

	// Extract the text of the triggering message.
	triggerMsg := h.extractMessageContent(evt.Message)
	if triggerMsg == "" {
		triggerMsg = "[mensagem sem texto]"
	}

	// Use the sender's push name when available.
	triggerSender := msgTrigger.PushName
	if triggerSender == "" {
		triggerSender = msgTrigger.Sender.User
	}

	// Personality defaults to CLT; future group-level personality config can be
	// plumbed through here by reading GroupSettings.
	opts := wstypes.ChatOptions{
		Personality: "clt",
	}

	response, err := h.aiService.ChatResponse(ctx, messages, triggerMsg, triggerSender, opts)
	if err != nil {
		h.logger.Error("handleChatResponse: AI service failed", "error", err)
		return
	}

	if err := h.whatsappService.SendMessageReply(
		msgTrigger.Chat,
		msgTrigger.Sender,
		msgTrigger.ID,
		response,
	); err != nil {
		h.logger.Error("handleChatResponse: failed to send reply", "error", err)
		return
	}

	// Persist the bot's reply so it appears in future context windows.
	botMsg := wstypes.Message{
		ChatID:      msgTrigger.Chat.User,
		SenderLID:   h.botLID,
		Sender:      "ResumoBOT [VOCÊ]",
		Content:     fmt.Sprintf("[Chat] %s", response),
		MessageType: "Summary", // reuse the Summary slot — no schema change needed
		Timestamp:   time.Now().In(h.timezone),
	}
	h.saveMessage(botMsg, msgTrigger.Chat) //nolint:errcheck

	h.logger.Info("handleChatResponse: response sent",
		"chat", msgTrigger.Chat.User,
		"response_len", len(response))
}
