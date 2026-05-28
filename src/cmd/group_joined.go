package cmd

import (
	"go.mau.fi/whatsmeow/types/events"

	wstypes "whatsapp-summarizer/src/types"
)

// onboardingMessage is sent to a group the first time the bot joins it.
const onboardingMessage = `👋 *Olá! Sou o ResumoBOT.*

Fui adicionado a este grupo e já estou pronto para resumir conversas com IA!

⚙️ *Configurações do grupo (somente admins):*
• !setregras <texto> — Define as regras do grupo
• !addwelcome <msg> — Adiciona mensagem de boas-vindas (use {numero} para mencionar quem entrou)
• !addfarewall <msg> — Adiciona mensagem de despedida (use {numero} para mencionar quem saiu)
• !delwelcome <n> — Remove boas-vindas pelo índice (sem índice lista as atuais)
• !delfarewall <n> — Remove despedida pelo índice (sem índice lista as atuais)

🔔 *Status atual:*
• Resumo diário automático: ✅ ligado (desligue com !resumo off)
• Ranking semanal: ✅ ligado (desligue com !ranking off)

📖 *Comandos gerais:*
• !help — Lista todos os comandos disponíveis
• !resumo <n> ou !r <n> — Resume as últimas N mensagens
• !dia ou !d — Resume as mensagens do dia
• !regras — Exibe as regras do grupo`

// handleJoinedGroupEvent is triggered when the bot is added to a new group.
// It creates a default GroupSettings record in the DB (daily summary and weekly
// ranking enabled by default) and sends an onboarding message in the group.
// The function is idempotent: if the group already has a record in the DB (e.g.
// after a bot reconnect), it logs and returns without sending the message again.
func (h *Handler) handleJoinedGroupEvent(evt *events.JoinedGroup) {
	if evt == nil {
		return
	}

	chatID := evt.JID.User

	// Check if already registered to ensure idempotency on reconnects.
	existing, err := h.dbService.GetGroupSettings(chatID)
	if err != nil {
		h.logger.Error("JoinedGroup: failed to check existing settings",
			"error", err, "chat", chatID)
		return
	}
	if existing != nil {
		h.logger.Debug("JoinedGroup: group already registered, skipping onboarding",
			"chat", chatID)
		return
	}

	// Create default settings: both daily summary and weekly ranking enabled.
	settings := wstypes.GroupSettings{
		ChatID:               chatID,
		Rules:                "",
		WelcomeMessages:      []string{},
		FarewellMessages:     []string{},
		DailySummaryEnabled:  true,
		WeeklyRankingEnabled: true,
	}
	if err := h.dbService.UpsertGroupSettings(settings); err != nil {
		h.logger.Error("JoinedGroup: failed to save default settings",
			"error", err, "chat", chatID)
		// Don't abort — still send the onboarding message so admins know the bot is here.
	}

	if err := h.whatsappService.SendMessage(evt.JID, onboardingMessage); err != nil {
		h.logger.Error("JoinedGroup: failed to send onboarding message",
			"error", err, "chat", chatID)
	}

	h.logger.Info("JoinedGroup: registered new group with default settings and sent onboarding",
		"chat", chatID)
}
