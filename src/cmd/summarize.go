package cmd

import (
	"context"
	"fmt"
	"time"

	waE2E "go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"

	wstypes "whatsapp-summarizer/src/types"
	"whatsapp-summarizer/src/ai"
	"whatsapp-summarizer/src/utils"
)

// summarizeCooldown is the minimum interval between summarize requests per user.
const summarizeCooldown = 5 * time.Second

// handleSummarizeCommand handles the summarize command
func (h *Handler) handleSummarizeCommand(args []string, msgTrigger types.MessageInfo) {
	if len(args) == 0 {
		h.reactToCommand(msgTrigger, "❌")
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID, "❌ Número de mensagens não especificado")
		return
	}

	// Enforce per-user rate limit to prevent Gemini API flooding.
	if wait := h.checkSummarizeRateLimit(msgTrigger); wait > 0 {
		h.reactToCommand(msgTrigger, "⏳")
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID,
			fmt.Sprintf("⏳ Aguarde *%.0fs* antes de pedir outro resumo.", wait.Seconds()))
		return
	}

	count, ok := h.parseAndValidateCount(msgTrigger, args[0], DefaultCountMessages)
	if !ok {
		return
	}

	// Parse options using utility function
	opts := utils.ParseSummarizeOptionsToStruct(args[1:], count)

	// Start summarization in goroutine
	go h.performSummarization(opts, msgTrigger)
}

// checkSummarizeRateLimit returns the remaining cooldown duration for the sender,
// or 0 if the user may proceed. When the user may proceed, it also records the
// current time so the next call sees the cooldown.
func (h *Handler) checkSummarizeRateLimit(msgTrigger types.MessageInfo) time.Duration {
	key := msgTrigger.Chat.User + ":" + msgTrigger.Sender.User
	now := time.Now()
	if v, loaded := h.sumRateLimitCache.Load(key); loaded {
		if last, ok := v.(time.Time); ok {
			if remaining := summarizeCooldown - now.Sub(last); remaining > 0 {
				return remaining
			}
		}
	}
	h.sumRateLimitCache.Store(key, now)
	return 0
}

// performSummarization performs the actual summarization
func (h *Handler) performSummarization(opts wstypes.SummarizeOptions, msgTrigger types.MessageInfo) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*10)
	defer cancel()

	// Send initial "reading messages..." message as reply
	loadingMessage := fmt.Sprintf("ℹ️ Lendo %d mensagens...", opts.Count)
	msgResp, err := h.whatsappService.SendRawMessage(context.Background(), msgTrigger.Chat, &waE2E.Message{
		ExtendedTextMessage: &waE2E.ExtendedTextMessage{
			Text: proto.String(loadingMessage),
			ContextInfo: &waE2E.ContextInfo{
				StanzaID:    proto.String(msgTrigger.ID),
				Participant: proto.String(msgTrigger.Sender.ToNonAD().String()),
			},
		},
	})
	if err != nil {
		h.logger.Error("Failed to send loading message", "error", err)
		h.reactToCommand(msgTrigger, "❌")
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID, "❌ Erro ao enviar mensagem")
		return
	}

	// Notify owner about the request (similar to legacy code)
	if h.config.WhatsApp.OwnerJID != "" && h.config.WhatsApp.OwnerJID != msgTrigger.Sender.User {
		groupName := h.getGroupName(msgTrigger.Chat)
		senderName := msgTrigger.PushName
		if senderName == "" {
			senderName = msgTrigger.Sender.User
		}

		var ownerMessage string
		if opts.Question != "" {
			ownerMessage = fmt.Sprintf("ℹ️ %s requisitou um %s resumo de %d mensagens em %s\n🎭 Personalidade: %s\n❓ Pergunta: %s",
				senderName, opts.Style, opts.Count, groupName, opts.Personality, opts.Question)
		} else {
			ownerMessage = fmt.Sprintf("ℹ️ %s requisitou um %s resumo de %d mensagens em %s\n🎭 Personalidade: %s",
				senderName, opts.Style, opts.Count, groupName, opts.Personality)
		}

		ownerJID := types.NewJID(h.config.WhatsApp.OwnerJID, types.DefaultUserServer)
		go func() {
			h.whatsappService.SendMessage(ownerJID, ownerMessage)
		}()
	}

	// Get messages from database (only groups are supported).
	// Flush the batch writer first to ensure recently-received messages
	// that have not yet been persisted are visible to this query.
	var messages []wstypes.Message

	h.dbService.FlushPendingMessages()
	messages, err = h.dbService.GetGroupMessages(msgTrigger.Chat.User, opts.Count)

	if err != nil {
		h.logger.Error("Failed to get messages", "error", err)
		h.reactToCommand(msgTrigger, "❌")
		h.whatsappService.EditMessage(msgTrigger.Chat, msgResp.ID, "❌ Erro ao buscar mensagens")
		return
	}

	if len(messages) == 0 {
		h.reactToCommand(msgTrigger, "❌")
		h.whatsappService.EditMessage(msgTrigger.Chat, msgResp.ID, "ℹ❌ Nenhuma mensagem encontrada")
		return
	}

	// onRetry edits the loading message so the user sees each internal model fallback.
	retrySpinners := []string{"🔄", "🔁"}
	onRetry := func(attempt int, _ string) {
		spinner := retrySpinners[min(attempt-2, len(retrySpinners)-1)]
		h.whatsappService.EditMessage(msgTrigger.Chat, msgResp.ID,
			fmt.Sprintf("%s Lendo %d mensagens...", spinner, opts.Count))
	}

	// Outer retry loop: up to 7 attempts, each exhausting all 3 AI models internally.
	// Between attempts, display a live per-second countdown so the user knows what's happening.
	retryDelays := []int{10, 15, 30, 45, 60, 80} // seconds to wait before attempt 2–7
	var summary string
	var lastErr error
	for attempt := 1; attempt <= 7; attempt++ {
		summary, lastErr = h.aiService.SummarizeMessages(ctx, messages, opts, onRetry)
		if lastErr == nil {
			break
		}

		if ai.IsPersonalityError(lastErr) {
			h.logger.Error("performSummarization: personality error", "error", lastErr, "personality", opts.Personality)
			h.reactToCommand(msgTrigger, "❌")
			h.whatsappService.EditMessage(msgTrigger.Chat, msgResp.ID,
				fmt.Sprintf("❌ A personalidade *%s* não está disponível ou está mal configurada no servidor (arquivo .toml ausente ou inválido).", opts.Personality))
			return
		}

		h.logger.Warn("SummarizeMessages: all models failed on attempt",
			"attempt", attempt, "error", lastErr)

		// No more retries after the 7th attempt.
		if attempt == 7 {
			break
		}

		delay := retryDelays[attempt-1]

		// Countdown: edit the loading message every second.
		for remaining := delay; remaining > 0; remaining-- {
			unit := "segundos"
			if remaining == 1 {
				unit = "segundo"
			}
			h.whatsappService.EditMessage(msgTrigger.Chat, msgResp.ID,
				fmt.Sprintf("⏳ Gemini indisponível no momento.\nTentando novamente em %d %s...", remaining, unit))

			select {
			case <-ctx.Done():
				h.logger.Warn("performSummarization: context cancelled during retry countdown")
				h.reactToCommand(msgTrigger, "❌")
				h.whatsappService.EditMessage(msgTrigger.Chat, msgResp.ID, "❌ Erro ao gerar resumo.\nGemini indisponível no momento, tente mais tarde.")
				return
			case <-time.After(1 * time.Second):
			}
		}
	}

	if lastErr != nil {
		h.logger.Error("Failed to generate summary after all attempts", "error", lastErr)
		h.reactToCommand(msgTrigger, "❌")
		h.whatsappService.EditMessage(msgTrigger.Chat, msgResp.ID,
			"❌ Erro ao gerar resumo.\nGemini indisponível no momento, tente mais tarde.")
		return
	}

	// Edit the loading message with the final summary
	finalSummary := fmt.Sprintf("ℹ️ Resumo por IA:\n%s", summary)
	err = h.whatsappService.EditMessage(msgTrigger.Chat, msgResp.ID, finalSummary)
	if err != nil {
		h.logger.Error("Failed to edit message with summary", "error", err)
		// Fallback: send summary as new message
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID, finalSummary)
	}
	// React ✅ to the user's original command message to signal completion.
	h.reactToCommand(msgTrigger, "✅")

	// Save summary as a message
	summaryMsg := wstypes.Message{
		ChatID:      msgTrigger.Chat.User,
		SenderLID:   h.botLID, // bare numeric LID, same format as human senders
		Sender:      "ResumoBOT [VOCÊ]",
		Content:     finalSummary,
		MessageType: "Summary",
		Timestamp:   time.Now().In(h.timezone),
	}
	h.saveMessage(summaryMsg, msgTrigger.Chat) //nolint:errcheck
}
