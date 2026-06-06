package cmd

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	waE2E "go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"

	wstypes "whatsapp-summarizer/src/types"
	"whatsapp-summarizer/src/utils"
)

// RunAutoDailySummary triggers the daily summary automatically for a given chat JID string.
// It is called by the scheduler at 00:00 and covers messages from the previous day.
// Accepts both bare numbers ("120363XXX") and full JIDs ("120363XXX@g.us"), trimming spaces.
func (h *Handler) RunAutoDailySummary(chatJIDStr string) {
	chatJIDStr = strings.TrimSpace(chatJIDStr)
	if chatJIDStr == "" {
		h.logger.Error("AutoDailySummary: empty JID, skipping")
		return
	}

	// Extract the user part (before @), regardless of input format
	userPart := chatJIDStr
	if idx := strings.Index(chatJIDStr, "@"); idx >= 0 {
		userPart = chatJIDStr[:idx]
	}

	if userPart == "" {
		h.logger.Error("AutoDailySummary: could not extract user part from JID", "jid", chatJIDStr)
		return
	}

	chatJID := types.JID{User: userPart, Server: "g.us"}
	h.logger.Info("AutoDailySummary: queuing", "chat", chatJID.User)
	h.wg.Add(1)
	go func() {
		defer h.wg.Done()
		h.performAutoDailySummarization(chatJID)
	}()
}

// performAutoDailySummarization performs the automatic daily summarization without a message trigger.
func (h *Handler) performAutoDailySummarization(chatJID types.JID) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*5)
	defer cancel()

	opts := wstypes.SummarizeOptions{
		Style:       "medium",
		Personality: "clt",
	}

	// Calculate 4 AM of the previous day in the bot's timezone (since we run at 00:00)
	now := time.Now().In(h.timezone)
	fourAMYesterday := time.Date(now.Year(), now.Month(), now.Day()-1, 4, 0, 0, 0, h.timezone)

	messages, err := h.dbService.GetMessagesSinceTime(chatJID.User, fourAMYesterday)
	if err != nil {
		h.logger.Error("AutoDailySummary: failed to get messages", "error", err)
		return
	}

	if len(messages) < 10 {
		h.logger.Info("AutoDailySummary: not enough messages, skipping", "chat", chatJID.User, "count", len(messages))
		return
	}

	loadingMessage := fmt.Sprintf("ℹ️ Resumindo o dia (%d mensagens)...", len(messages))
	msgResp, err := h.whatsappService.SendRawMessage(context.Background(), chatJID, &waE2E.Message{
		Conversation: proto.String(loadingMessage),
	})
	if err != nil {
		h.logger.Error("AutoDailySummary: failed to send loading message", "error", err)
		return
	}

	// Generate summary. Auto runs have no user-visible loading message to update
	// between retries, so onRetry is nil — the log warning from the AI service is sufficient.
	summary, err := h.aiService.SummarizeMessages(ctx, messages, opts, nil)
	if err != nil {
		h.logger.Error("AutoDailySummary: all models failed", "error", err)
		h.whatsappService.EditMessage(chatJID, msgResp.ID, "❌ Erro ao gerar resumo automático")
		return
	}

	// Build final message
	messageCount := len(messages)
	var msgsPerHour int
	if messageCount > 0 && messages[0].Timestamp.After(fourAMYesterday) {
		// Use first message timestamp as start time
		duration := now.Sub(messages[0].Timestamp)
		if hours := duration.Hours(); hours > 0 {
			msgsPerHour = int(float64(messageCount) / hours)
		}
	} else {
		// Fallback to 4 AM yesterday if no messages or invalid timestamp
		duration := now.Sub(fourAMYesterday)
		if hours := duration.Hours(); hours > 0 {
			msgsPerHour = int(float64(messageCount) / hours)
		}
	}

	header := fmt.Sprintf("🌙 *Resumo do dia %s:*\n", fourAMYesterday.Format("02/01"))
	footer := fmt.Sprintf("\n\n---\n📊 %d mensagens | ⏱️ %d msgs/h\n%s", messageCount, msgsPerHour, topTalkersFooter(messages))
	h.whatsappService.EditMessage(chatJID, msgResp.ID, header+summary+footer)

	h.logger.Info("AutoDailySummary completed",
		"chat_id", chatJID.User,
		"message_count", messageCount,
		"since", fourAMYesterday.Format("2006-01-02 15:04:05"),
	)
}

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
	h.wg.Add(1)
	go func() {
		defer h.wg.Done()
		h.performDailySummarization(opts, msgTrigger)
	}()
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
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID, "❌ Erro ao buscar mensagens do banco de dados")
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
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID, "❌ Erro ao enviar mensagem")
		return
	}

	// Check if there are enough messages
	if len(messages) < 10 {
		h.whatsappService.EditMessage(msgTrigger.Chat, msgResp.ID, fmt.Sprintf("ℹ️ Apenas %d mensagens hoje. Muito pouco para resumir...", len(messages)))
		return
	}

	// onRetry edits the loading message so the user sees each fallback attempt.
	retrySpinners := []string{"🔄", "🔁"}
	onRetry := func(attempt int, _ string) {
		spinner := retrySpinners[min(attempt-2, len(retrySpinners)-1)]
		h.whatsappService.EditMessage(msgTrigger.Chat, msgResp.ID,
			fmt.Sprintf("%s Resumindo o dia (%d mensagens)...", spinner, len(messages)))
	}

	// Generate summary (retries backup models internally)
	summary, err := h.aiService.SummarizeMessages(ctx, messages, opts, onRetry)
	if err != nil {
		h.logger.Error("Failed to generate summary", "error", err)
		errorMsg := "❌ Erro ao gerar resumo"
		if ctx.Err() == context.DeadlineExceeded {
			errorMsg = "⏱️ Timeout ao gerar resumo - tente com menos mensagens"
		}
		h.whatsappService.EditMessage(msgTrigger.Chat, msgResp.ID, errorMsg)
		return
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

	footer := fmt.Sprintf("\n\n---\n📊 %d mensagens | ⏱️ %d msgs/h\n%s", messageCount, msgsPerHour, topTalkersFooter(messages))
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

// topTalkersFooter builds the "🗣️ Mais ativos: ..." line from a message slice.
// It returns the top 3 senders by message count, formatted as:
//
//	🗣️ Mais ativos: João (45) | Maria (30) | Pedro (15)
func topTalkersFooter(messages []wstypes.Message) string {
	counts := make(map[string]int)
	for _, m := range messages {
		name := m.Sender
		// Strip the @s.whatsapp.net / @g.us suffix if present
		if idx := strings.Index(name, "@"); idx >= 0 {
			name = name[:idx]
		}
		if name != "" {
			counts[name]++
		}
	}

	type kv struct {
		Name  string
		Count int
	}
	var sorted []kv
	for name, count := range counts {
		sorted = append(sorted, kv{name, count})
	}
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Count != sorted[j].Count {
			return sorted[i].Count > sorted[j].Count
		}
		return sorted[i].Name < sorted[j].Name
	})

	const topN = 3
	if len(sorted) > topN {
		sorted = sorted[:topN]
	}

	parts := make([]string, 0, len(sorted))
	for _, kv := range sorted {
		parts = append(parts, fmt.Sprintf("%s (%d)", kv.Name, kv.Count))
	}
	return "🗣️ Mais ativos: " + strings.Join(parts, " | ")
}
