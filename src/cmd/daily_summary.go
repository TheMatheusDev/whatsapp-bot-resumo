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

// handleDailySummaryCommand handles the daily summary command (summarization since 4 AM)
func (h *Handler) handleDailySummaryCommand(args []string, msgTrigger types.MessageInfo) {
	// Parse options using utility function
	style, personality, _ := utils.ParseSummarizeOptions(args, false)

	// Override default style for daily summary
	if style == "short" {
		style = "medium"
	}

	opts := wstypes.SummarizeOptions{
		Style:       style,
		Personality: personality,
	}

	// Start summarization in goroutine
	go h.performDailySummarization(opts, msgTrigger)
}

// performDailySummarization performs the daily summarization (since 4 AM)
func (h *Handler) performDailySummarization(opts wstypes.SummarizeOptions, msgTrigger types.MessageInfo) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*5)
	defer cancel()

	// Calculate 4 AM today in the bot's timezone (GMT-3)
	now := time.Now().In(h.timezone)
	fourAMToday := time.Date(now.Year(), now.Month(), now.Day(), 4, 0, 0, 0, h.timezone)

	// If current time is before 4 AM, use yesterday's 4 AM
	if now.Hour() < 4 {
		fourAMToday = fourAMToday.Add(-24 * time.Hour)
	}

	// Get messages since 4 AM
	messages, err := h.dbService.GetMessagesSinceTime(msgTrigger.Chat.User, fourAMToday)
	if err != nil {
		h.logger.Error("Failed to get messages since time", "error", err)
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.ID, "❌ Erro ao buscar mensagens do banco de dados")
		return
	}

	// Send initial "reading messages..." message as reply
	loadingMessage := fmt.Sprintf("ℹ️ Resumindo o dia (%d mensagens)...", len(messages))
	msgResp, err := h.whatsappService.SendRawMessage(context.Background(), msgTrigger.Chat, &waE2E.Message{
		ExtendedTextMessage: &waE2E.ExtendedTextMessage{
			Text: proto.String(loadingMessage),
			ContextInfo: &waE2E.ContextInfo{
				StanzaID:    proto.String(msgTrigger.ID),
				Participant: proto.String(msgTrigger.Sender.String()),
			},
		},
	})
	if err != nil {
		h.logger.Error("Failed to send loading message", "error", err)
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.ID, "❌ Erro ao enviar mensagem")
		return
	}

	// Check if there are enough messages
	if len(messages) < 10 {
		h.whatsappService.EditMessage(msgTrigger.Chat, msgResp.ID, fmt.Sprintf("ℹ️ Apenas %d mensagens hoje. Muito pouco para resumir...", len(messages)))
		return
	}

	// Generate summary using AI - try primary model first
	summary, err := h.aiService.SummarizeMessages(ctx, messages, opts)
	if err != nil {
		h.logger.Error("Failed to generate summary", "error", err)

		// Try with backup model
		h.logger.Info("Retrying with backup model")

		// Edit the loading message to show we're trying backup
		h.whatsappService.EditMessage(msgTrigger.Chat, msgResp.ID, fmt.Sprintf("🔄 Lendo %d mensagens...", len(messages)))

		// Try again with backup model
		summary, err = h.aiService.SummarizeMessagesWithBackup(ctx, messages, opts)
		if err != nil {
			h.logger.Error("Failed to generate summary with backup model", "error", err)

			// Try with second backup model
			h.logger.Info("Retrying with second backup model")
			h.whatsappService.EditMessage(msgTrigger.Chat, msgResp.ID, fmt.Sprintf("🔄 Lendo %d mensagens...", len(messages)))

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

	// Add header
	header := "ℹ️ *Resumo do dia:*\n"

	// Add metadata footer
	messageCount := len(messages)

	// Calculate duration from first message timestamp instead of 4 AM
	var duration time.Duration
	var msgsPerHour int
	if messageCount > 0 && messages[0].Timestamp.After(fourAMToday) {
		// Use first message timestamp as start time
		duration = now.Sub(messages[0].Timestamp)
		hours := duration.Hours()
		if hours > 0 {
			msgsPerHour = int(float64(messageCount) / hours)
		}
	} else {
		// Fallback to 4 AM if no messages or invalid timestamp
		duration = now.Sub(fourAMToday)
		hours := duration.Hours()
		msgsPerHour = int(float64(messageCount) / hours)
	}

	footer := fmt.Sprintf("\n\n---\n📊 %d mensagens | ⏱️ %d msgs/h", messageCount, msgsPerHour)
	fullSummary := header + summary + footer

	// Edit the loading message with the final summary
	h.whatsappService.EditMessage(msgTrigger.Chat, msgResp.ID, fullSummary)

	h.logger.Info("Daily summary completed",
		"chat_id", msgTrigger.Chat.User,
		"message_count", messageCount,
		"style", opts.Style,
		"personality", opts.Personality,
		"since", fourAMToday.Format("2006-01-02 15:04:05"),
	)
}
