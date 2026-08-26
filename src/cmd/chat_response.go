package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	waE2E "go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types/events"

	wstypes "whatsapp-summarizer/src/types"
	"whatsapp-summarizer/src/ai"
)

// chatCooldown is the minimum interval between chatbot responses per user.
// Prevents a single user from flooding the bot with mentions/replies.
const chatCooldown = 5 * time.Second

// chatContextMessages is the number of recent messages (including bot messages)
// sent to the AI model as conversation context when no recent interaction exists (cold).
const chatContextMessages = 100

// chatContextMessagesWarm is the reduced context size used when the group already
// had a chatbot interaction within the last chatWarmWindow. The model already has
// recent context from the previous call, so fewer messages suffice.
const chatContextMessagesWarm = 30

// chatWarmWindow is the duration after a chatbot response during which
// subsequent interactions in the same group use the smaller context window.
const chatWarmWindow = 1 * time.Minute

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
// WhatsApp encodes MentionedJID as "user@server". We strip the server suffix
// and compare the user part against BOTH the bot's LID and its phone number,
// because different client versions and the PN→LID migration mean either format
// can appear in the field.
func (h *Handler) isBotMentioned(msg *waE2E.Message) bool {
	ctx := extractContextInfo(msg)
	if ctx == nil {
		return false
	}

	mentioned := ctx.GetMentionedJID()
	if len(mentioned) == 0 {
		return false
	}

	for _, jid := range mentioned {
		user := jid
		if idx := strings.Index(jid, "@"); idx >= 0 {
			user = jid[:idx]
		}
		if user == h.botLID || (h.botPhoneUser != "" && user == h.botPhoneUser) {
			h.logger.Info("isBotMentioned: match found", "matched_jid", jid)
			return true
		}
	}
	return false
}

// isReplyToBot reports whether the message is a reply to a message originally
// sent by the bot. It does this by comparing ContextInfo.Participant (the author
// of the quoted message) against the bot's own LID and phone number.
func (h *Handler) isReplyToBot(msg *waE2E.Message) bool {
	ctx := extractContextInfo(msg)
	if ctx == nil || ctx.GetStanzaID() == "" {
		return false
	}

	raw := ctx.GetParticipant()
	if raw == "" {
		return false
	}

	participant := raw
	if idx := strings.Index(raw, "@"); idx >= 0 {
		participant = raw[:idx]
	}

	matched := participant == h.botLID || (h.botPhoneUser != "" && participant == h.botPhoneUser)
	if matched {
		h.logger.Info("isReplyToBot: match found", "participant", raw)
	}
	return matched
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

		// Notify the user and react with ❌ so they know they must wait.
		replyMsg := fmt.Sprintf("❌ Você deve aguardar %ds entre interações!", int(chatCooldown.Seconds()))
		_ = h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID, replyMsg)

		reactCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = h.whatsappService.ReactToMessage(reactCtx, msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID, "❌")
		return
	}

	// Check whether the chatbot feature is enabled for this group and trigger type.
	if msgTrigger.IsGroup {
		settings := h.loadOrDefaultSettings(msgTrigger.Chat.User)
		isMention := h.isBotMentioned(evt.Message)
		isReply := h.isReplyToBot(evt.Message)
		if !((isMention && settings.ChatbotMentionsEnabled) || (isReply && settings.ChatbotRepliesEnabled)) {
			h.logger.Debug("handleChatResponse: chatbot disabled for this trigger in group",
				"chat", msgTrigger.Chat.User,
				"is_mention", isMention,
				"is_reply", isReply,
				"mentions_enabled", settings.ChatbotMentionsEnabled,
				"replies_enabled", settings.ChatbotRepliesEnabled)
			return
		}
	}

	h.logger.Info("handleChatResponse: triggered",
		"chat", msgTrigger.Chat.User,
		"sender", msgTrigger.Sender.User,
		"is_group", msgTrigger.IsGroup,
		"msg_id", msgTrigger.ID)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	// Flush so the triggering message is visible to the context query.
	h.dbService.FlushPendingMessages()

	// Use a smaller context window when the group had a recent chatbot interaction
	// (warm conversation) to save API tokens. Cold conversations get the full 100.
	contextSize := chatContextMessages
	if v, ok := h.chatLastInteraction.Load(msgTrigger.Chat.User); ok {
		if last, ok := v.(time.Time); ok && time.Since(last) < chatWarmWindow {
			contextSize = chatContextMessagesWarm
		}
	}

	messages, err := h.dbService.GetGroupMessagesWithBot(msgTrigger.Chat.User, contextSize)
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

	// Use the group's configured default personality (falls back to "resumobot" if not set).
	opts := wstypes.ChatOptions{
		Personality: h.getGroupDefaultPersonality(msgTrigger.Chat.User),
	}

	chatResult, err := h.aiService.ChatResponse(ctx, messages, triggerMsg, triggerSender, opts)
	if err != nil {
		h.logger.Error("handleChatResponse: AI service failed", "error", err)
		if ai.IsPersonalityError(err) {
			replyMsg := fmt.Sprintf("❌ A personalidade *%s* não está disponível ou está mal configurada no servidor (arquivo .toml ausente ou inválido).", opts.Personality)
			_ = h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID, replyMsg)
			reactCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_ = h.whatsappService.ReactToMessage(reactCtx, msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID, "❌")
		}
		return
	}

	// If the model requested tool calls, dispatch the tool call.
	if len(chatResult.ToolCalls) > 0 {
		toolCall := chatResult.ToolCalls[0]
		h.logger.Info("handleChatResponse: executing tool call",
			"tool", toolCall.Name,
			"args", toolCall.Args)

		if executed := h.DispatchToolCall(toolCall, msgTrigger, evt.Message); executed {
			// Record the interaction timestamp so subsequent triggers use the warm window.
			h.chatLastInteraction.Store(msgTrigger.Chat.User, time.Now())
			return
		}
	}

	response := chatResult.Text
	if strings.TrimSpace(response) == "" {
		h.logger.Warn("handleChatResponse: empty response text and no tool executed")
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

	// Record the interaction timestamp so subsequent triggers within
	// chatWarmWindow use the smaller context window.
	h.chatLastInteraction.Store(msgTrigger.Chat.User, time.Now())

	h.logger.Info("handleChatResponse: response sent",
		"chat", msgTrigger.Chat.User,
		"response_len", len(response),
		"context_size", contextSize)
}
