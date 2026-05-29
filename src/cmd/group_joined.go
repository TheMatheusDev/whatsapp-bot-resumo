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
• !addfarewell <msg> — Adiciona mensagem de despedida (use {numero} para mencionar quem saiu)
• !delwelcome <n> — Remove boas-vindas pelo índice (sem índice lista as atuais)
• !delfarewell <n> — Remove despedida pelo índice (sem índice lista as atuais)

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
//
// Thread-safety: rapid reconnects can fire multiple JoinedGroup events for the
// same group in quick succession. The joiningGroups sync.Map is used as an
// atomic set — only the first goroutine to acquire the slot for a given chatID
// proceeds with onboarding; all others return immediately. The slot is released
// via defer so that future legitimate re-joins (bot removed then re-added) are
// handled correctly. The DB upsert is inherently idempotent (ON CONFLICT DO
// UPDATE), so even if two goroutines race through, the data remains consistent
// and only the message send is guarded by the in-process lock.
func (h *Handler) handleJoinedGroupEvent(evt *events.JoinedGroup) {
	if evt == nil {
		return
	}

	chatID := evt.JID.User

	// Atomic guard: only the first goroutine for this chatID proceeds.
	// LoadOrStore returns (existingValue, true) if the key was already present,
	// or (storedValue, false) if this goroutine just stored it.
	if _, alreadyProcessing := h.joiningGroups.LoadOrStore(chatID, struct{}{}); alreadyProcessing {
		h.logger.Debug("JoinedGroup: onboarding already in progress, skipping duplicate event",
			"chat", chatID)
		return
	}
	// Release the slot when done so future re-joins are processed correctly.
	defer h.joiningGroups.Delete(chatID)

	// Secondary idempotency check against the DB: handles the case where the
	// bot reconnects after a clean shutdown and the group is already registered.
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
