package cmd

import (
	"context"
	"fmt"
	"time"

	waE2E "go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"

	wstypes "whatsapp-summarizer/src/types"
	"whatsapp-summarizer/src/utils"
)

// summarizeCooldown is the minimum interval between summarize requests per user.
const summarizeCooldown = 30 * time.Second

// handleSummarizeCommand handles the summarize command
func (h *Handler) handleSummarizeCommand(args []string, msgTrigger types.MessageInfo) {
	if len(args) == 0 {
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID, "❌ Número de mensagens não especificado")
		return
	}

	// Enforce per-user rate limit to prevent Gemini API flooding.
	if wait := h.checkSummarizeRateLimit(msgTrigger); wait > 0 {
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
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*3)
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

	// Get messages from database (only groups are supported)
	var messages []wstypes.Message

	if !msgTrigger.IsGroup {
		h.logger.Error("Direct messages are not supported for summarization")
		h.whatsappService.EditMessage(msgTrigger.Chat, msgResp.ID, "❌ Resumos não são suportados em mensagens diretas")
		return
	}

	messages, err = h.dbService.GetGroupMessages(msgTrigger.Chat.User, opts.Count)

	if err != nil {
		h.logger.Error("Failed to get messages", "error", err)
		// Edit the loading message to show error
		h.whatsappService.EditMessage(msgTrigger.Chat, msgResp.ID, "❌ Erro ao buscar mensagens")
		return
	}

	if len(messages) == 0 {
		// Edit the loading message to show no messages found
		h.whatsappService.EditMessage(msgTrigger.Chat, msgResp.ID, "ℹ❌ Nenhuma mensagem encontrada")
		return
	}

	// Generate summary
	summary, err := h.aiService.SummarizeMessages(ctx, messages, opts)
	if err != nil {
		h.logger.Error("Failed to generate summary", "error", err)

		// Try with backup model
		h.logger.Info("Retrying with backup model")

		// Edit the loading message to show we're trying backup
		h.whatsappService.EditMessage(msgTrigger.Chat, msgResp.ID, fmt.Sprintf("🔄 Lendo %d mensagens...", opts.Count))

		// Try again with backup model
		summary, err = h.aiService.SummarizeMessagesWithBackup(ctx, messages, opts)
		if err != nil {
			h.logger.Error("Failed to generate summary with backup model", "error", err)

			// Try with second backup model
			h.logger.Info("Retrying with second backup model")
			h.whatsappService.EditMessage(msgTrigger.Chat, msgResp.ID, fmt.Sprintf("� Lendo %d mensagens...", opts.Count))

			summary, err = h.aiService.SummarizeMessagesWithBackup2(ctx, messages, opts)
			if err != nil {
				h.logger.Error("Failed to generate summary with second backup model", "error", err)
				// Edit the loading message to show error
				errorMsg := ""
				if ctx.Err() == context.DeadlineExceeded {
					errorMsg = "⏱️ Timeout ao gerar resumo - tente com menos mensagens"
				} else {
					errorMsg = fmt.Sprintf("❌ Erro ao gerar resumo\n\n%s", err.Error())
				}
				h.whatsappService.EditMessage(msgTrigger.Chat, msgResp.ID, errorMsg)
				return
			}
		}
	}

	// Edit the loading message with the final summary
	finalSummary := fmt.Sprintf("ℹ️ Resumo por IA:\n%s", summary)
	err = h.whatsappService.EditMessage(msgTrigger.Chat, msgResp.ID, finalSummary)
	if err != nil {
		h.logger.Error("Failed to edit message with summary", "error", err)
		// Fallback: send summary as new message
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID, finalSummary)
	}

	// Save summary as a message
	summaryMsg := wstypes.Message{
		ChatID:      msgTrigger.Chat.User,
		Sender:      "ResumoBOT [VOCÊ]",
		Content:     finalSummary,
		MessageType: "Summary",
		Timestamp:   time.Now().In(h.timezone),
	}
	h.saveMessage(summaryMsg, msgTrigger.Chat) //nolint:errcheck
}
