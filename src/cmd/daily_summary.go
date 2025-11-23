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
func (h *Handler) handleDailySummaryCommand(args []string, info types.MessageInfo, client *whatsmeow.Client) {
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
	go h.performDailySummarization(opts, info, client)
}

// performDailySummarization performs the daily summarization (since 4 AM)
func (h *Handler) performDailySummarization(opts wstypes.SummarizeOptions, info types.MessageInfo, client *whatsmeow.Client) {
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
	messages, err := h.dbService.GetMessagesSinceTime(info.Chat.User, fourAMToday)
	if err != nil {
		h.logger.Error("Failed to get messages since time", "error", err)
		h.sendErrorMessageReply(client, info, "Erro ao buscar mensagens do banco de dados")
		return
	}

	// Send initial "reading messages..." message as reply
	loadingMessage := fmt.Sprintf("ℹ️ Resumindo o dia (%d mensagens)...", len(messages))
	msgResp, err := client.SendMessage(context.Background(), info.Chat, &waE2E.Message{
		ExtendedTextMessage: &waE2E.ExtendedTextMessage{
			Text: proto.String(loadingMessage),
			ContextInfo: &waE2E.ContextInfo{
				StanzaID:    proto.String(info.ID),
				Participant: proto.String(info.Sender.String()),
			},
		},
	})
	if err != nil {
		h.logger.Error("Failed to send loading message", "error", err)
		h.sendErrorMessageReply(client, info, "Erro ao enviar mensagem")
		return
	}

	// Check if there are enough messages
	if len(messages) < 10 {
		h.editMessage(client, info.Chat, msgResp.ID, fmt.Sprintf("ℹ️ Apenas %d mensagens hoje. Muito pouco para resumir...", len(messages)))
		return
	}

	// Generate summary using AI - try primary model first
	summary, err := h.aiService.SummarizeMessages(ctx, messages, opts)
	if err != nil {
		h.logger.Error("Failed to generate summary", "error", err)
		if ctx.Err() == context.DeadlineExceeded {
			h.sendErrorMessageReply(client, info, "Timeout ao gerar resumo - tente com menos mensagens")
		} else {
			h.sendErrorMessageReply(client, info, fmt.Sprintf("Erro ao gerar resumo\n\n%s", err.Error()))
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
	h.editMessage(client, info.Chat, msgResp.ID, fullSummary)

	h.logger.Info("Daily summary completed",
		"chat_id", info.Chat.User,
		"message_count", messageCount,
		"style", opts.Style,
		"clt", opts.Clt,
		"since", fourAMToday.Format("2006-01-02 15:04:05"),
	)
}

// editMessage edits a previously sent message
func (h *Handler) editMessage(client *whatsmeow.Client, chat types.JID, messageID types.MessageID, newContent string) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*30)
	defer cancel()

	_, err := client.SendMessage(ctx, chat, client.BuildEdit(chat, messageID, &waE2E.Message{
		Conversation: proto.String(newContent),
	}))
	if err != nil {
		h.logger.Error("Failed to edit message", "error", err)
	}
}
