package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	waE2E "go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"

	wstypes "whatsapp-summarizer/src/types"
)

// handleDailySummaryCommand handles the daily summary command (summarization since 4 AM)
func (h *Handler) handleDailySummaryCommand(args []string, msgTrigger types.MessageInfo, client *whatsmeow.Client) {
	// Parse options
	opts := wstypes.SummarizeOptions{
		Style: "medium", // default para resumo diário
		Clt:   false,
	}

	for _, arg := range args {
		switch strings.ToLower(arg) {
		case "--curto", "-c":
			opts.Style = "short"
		case "--medio", "-m":
			opts.Style = "medium"
		case "--longo", "-l":
			opts.Style = "long"
		case "--clt", "-clt":
			opts.Clt = true
		}
	}

	// Start summarization in goroutine
	go h.performDailySummarization(opts, msgTrigger, client)
}

// performDailySummarization performs the daily summarization (since 4 AM)
func (h *Handler) performDailySummarization(opts wstypes.SummarizeOptions, msgTrigger types.MessageInfo, client *whatsmeow.Client) {
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
	msgResp, err := client.SendMessage(context.Background(), msgTrigger.Chat, &waE2E.Message{
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
		if ctx.Err() == context.DeadlineExceeded {
			h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.ID, "❌ Timeout ao gerar resumo - tente com menos mensagens")
		} else {
			h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.ID, fmt.Sprintf("❌ Erro ao gerar resumo\n\n%s", err.Error()))
		}
		return
	}

	// Add metadata footer
	messageCount := len(messages)
	duration := now.Sub(fourAMToday)
	hours := duration.Hours()
	msgsPerHour := int(float64(messageCount) / hours)

	footer := fmt.Sprintf("\n\n---\n📊 %d mensagens | ⏱️ %d msgs/h", messageCount, msgsPerHour)
	fullSummary := summary + footer

	// Edit the loading message with the final summary
	h.whatsappService.EditMessage(msgTrigger.Chat, msgResp.ID, fullSummary)

	h.logger.Info("Daily summary completed",
		"chat_id", msgTrigger.Chat.User,
		"message_count", messageCount,
		"style", opts.Style,
		"clt", opts.Clt,
		"since", fourAMToday.Format("2006-01-02 15:04:05"),
	)
}
