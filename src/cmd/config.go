package cmd

import (
	"fmt"
	"strings"

	"go.mau.fi/whatsmeow/types"
)

// handleConfigCommand displays the current configuration for this group.
// It shows: rules (yes/no), number of welcome/farewell messages, daily
// summary and weekly ranking status. Admin-only.
//
// Usage: !config
func (h *Handler) handleConfigCommand(msgTrigger types.MessageInfo) {
	if !h.requireGroupAdmin(msgTrigger) {
		return
	}

	settings := h.loadOrDefaultSettings(msgTrigger.Chat.User)

	// Rules status
	rulesStatus := "❌ não definidas"
	if strings.TrimSpace(settings.Rules) != "" {
		rulesStatus = "✅ definidas"
	}

	// Welcome/farewell pools
	welcomePool := h.resolveWelcomePool(msgTrigger.Chat.User)
	farewellPool := h.resolveFarewellPool(msgTrigger.Chat.User)
	welcomeCount := len(welcomePool)
	farewellCount := len(farewellPool)

	// Daily summary status
	dailyStatus := "✅ ligado"
	if !settings.DailySummaryEnabled {
		dailyStatus = "⛔ desligado"
	}

	// Weekly ranking status
	weeklyStatus := "✅ ligado"
	if !settings.WeeklyRankingEnabled {
		weeklyStatus = "⛔ desligado"
	}

	// Chatbot mentions status
	chatbotMentionsStatus := "✅ ligado"
	if !settings.ChatbotMentionsEnabled {
		chatbotMentionsStatus = "⛔ desligado"
	}

	// Chatbot replies status
	chatbotRepliesStatus := "✅ ligado"
	if !settings.ChatbotRepliesEnabled {
		chatbotRepliesStatus = "⛔ desligado"
	}

	// Default personality
	defaultPersonality := h.getGroupDefaultPersonality(msgTrigger.Chat.User)

	msg := fmt.Sprintf(`⚙️ *Configurações do grupo*

📜 *Regras:* %s
👋 *Boas-vindas:* %d mensagem(ns) configurada(s)
👋 *Despedidas:* %d mensagem(ns) configurada(s)
🌙 *Resumo diário:* %s
🏆 *Ranking semanal:* %s
🤖 *Chatbot (menções):* %s
💬 *Chatbot (replies):* %s
🎭 *Personalidade atual:* %s`,
		rulesStatus,
		welcomeCount,
		farewellCount,
		dailyStatus,
		weeklyStatus,
		chatbotMentionsStatus,
		chatbotRepliesStatus,
		defaultPersonality,
	)

	h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID, msg)
}

