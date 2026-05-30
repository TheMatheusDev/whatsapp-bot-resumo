package cmd

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"go.mau.fi/whatsmeow/types"

	wstypes "whatsapp-summarizer/src/types"
)

// errNotAdmin is the reply sent when a non-admin tries to use an admin command.
const errNotAdmin = "❌ Apenas admins do grupo podem usar este comando."

// requireGroupAdmin checks if the sender is a native group admin.
// OWNER_JID is treated as a universal admin across all groups — the check
// short-circuits before any WhatsApp API call, so there is no network cost.
// If the sender is not the owner and not a native admin, sends errNotAdmin
// and returns false.
func (h *Handler) requireGroupAdmin(msgTrigger types.MessageInfo) bool {
	// OWNER_JID is always allowed, regardless of native group admin status.
	if h.config.WhatsApp.OwnerJID != "" && msgTrigger.Sender.User == h.config.WhatsApp.OwnerJID {
		return true
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if !h.isGroupAdmin(ctx, msgTrigger.Chat, msgTrigger.Sender.User) {
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.ID, errNotAdmin)
		return false
	}
	return true
}

// loadOrDefaultSettings fetches the GroupSettings for a group, creating a
// default struct (not yet persisted) if no record exists in the DB.
func (h *Handler) loadOrDefaultSettings(chatID string) wstypes.GroupSettings {
	if gs := h.getGroupSettings(chatID); gs != nil {
		return *gs
	}
	return wstypes.GroupSettings{
		ChatID:               chatID,
		DailySummaryEnabled:  true,
		WeeklyRankingEnabled: true,
		WelcomeMessages:      []string{},
		FarewellMessages:     []string{},
	}
}

// saveAndInvalidate persists updated settings and clears the in-memory cache
// for that group so the next read fetches fresh data.
func (h *Handler) saveAndInvalidate(settings wstypes.GroupSettings) error {
	if err := h.dbService.UpsertGroupSettings(settings); err != nil {
		return err
	}
	h.invalidateGroupSettings(settings.ChatID)
	return nil
}

// ---------------------------------------------------------------------------
// !setregras
// ---------------------------------------------------------------------------

// handleSetRulesCommand sets (or displays) the group rules stored in the DB.
//
// Usage:
//
//	!setregras <texto>   → stores the rules and confirms
//	!setregras           → displays the current rules (or "none defined")
func (h *Handler) handleSetRulesCommand(args []string, msgTrigger types.MessageInfo) {
	if !msgTrigger.IsGroup {
		return
	}

	// Display current rules when called with no arguments.
	if len(args) == 0 {
		h.handleRulesCommand(msgTrigger)
		return
	}

	if !h.requireGroupAdmin(msgTrigger) {
		return
	}

	rules := strings.Join(args, " ")
	settings := h.loadOrDefaultSettings(msgTrigger.Chat.User)
	settings.Rules = rules

	if err := h.saveAndInvalidate(settings); err != nil {
		h.logger.Error("handleSetRulesCommand: failed to save settings", "error", err)
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.ID,
			"❌ Erro ao salvar regras. Tente novamente.")
		return
	}

	h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.ID,
		"✅ Regras do grupo atualizadas!\n\nUse !regras para visualizá-las.")
	h.logger.Info("Group rules updated", "chat", msgTrigger.Chat.User)
}

// ---------------------------------------------------------------------------
// !addwelcome / !delwelcome
// ---------------------------------------------------------------------------

// handleAddWelcomeCommand appends one or more welcome message templates to the
// group's pool. Separate multiple messages with | to add them all at once.
// Use {numero} in templates to mention the joining participant(s).
//
// Usage:
//
//	!addwelcome Olá {numero}! Seja bem-vindo(a)!
//	!addwelcome Oi {numero}!|Bem-vindo(a), {numero}! 🎉
func (h *Handler) handleAddWelcomeCommand(args []string, msgTrigger types.MessageInfo) {
	if !msgTrigger.IsGroup {
		return
	}
	if !h.requireGroupAdmin(msgTrigger) {
		return
	}

	if len(args) == 0 {
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.ID,
			"❌ Informe o texto da mensagem de boas-vindas.\n"+
				"Exemplo: !addwelcome Olá {numero}! Seja bem-vindo(a)!\n"+
				"Separe múltiplas mensagens com |")
		return
	}

	newMessages := splitPipe(strings.Join(args, " "))
	if len(newMessages) == 0 {
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.ID,
			"❌ Nenhuma mensagem válida encontrada.")
		return
	}

	settings := h.loadOrDefaultSettings(msgTrigger.Chat.User)
	settings.WelcomeMessages = append(settings.WelcomeMessages, newMessages...)

	if err := h.saveAndInvalidate(settings); err != nil {
		h.logger.Error("handleAddWelcomeCommand: failed to save settings", "error", err)
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.ID,
			"❌ Erro ao salvar mensagem. Tente novamente.")
		return
	}

	h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.ID,
		formatAddedMessages("boas-vindas", newMessages, len(settings.WelcomeMessages)))
}

// handleDelWelcomeCommand removes a welcome message by its 1-based index.
// When called without an index, it lists the current messages.
//
// Usage:
//
//	!delwelcome      → lists current messages with indices
//	!delwelcome <n>  → removes message at index n
func (h *Handler) handleDelWelcomeCommand(args []string, msgTrigger types.MessageInfo) {
	if !msgTrigger.IsGroup {
		return
	}
	if !h.requireGroupAdmin(msgTrigger) {
		return
	}

	settings := h.loadOrDefaultSettings(msgTrigger.Chat.User)

	// No argument: list current messages.
	if len(args) == 0 {
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.ID,
			formatMessageList("Mensagens de boas-vindas atuais", settings.WelcomeMessages,
				"!delwelcome"))
		return
	}

	idx, err := strconv.Atoi(args[0])
	if err != nil || idx < 1 || idx > len(settings.WelcomeMessages) {
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.ID,
			fmt.Sprintf("❌ Índice inválido. Use !delwelcome (sem número) para ver a lista."))
		return
	}

	removed := settings.WelcomeMessages[idx-1]
	settings.WelcomeMessages = append(
		settings.WelcomeMessages[:idx-1],
		settings.WelcomeMessages[idx:]...,
	)

	if err := h.saveAndInvalidate(settings); err != nil {
		h.logger.Error("handleDelWelcomeCommand: failed to save settings", "error", err)
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.ID,
			"❌ Erro ao salvar. Tente novamente.")
		return
	}

	h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.ID,
		fmt.Sprintf("✅ Mensagem de boas-vindas #%d removida:\n\n_%s_\n\n"+
			"Total restante: %d", idx, removed, len(settings.WelcomeMessages)))
}

// ---------------------------------------------------------------------------
// !addfarewell / !delfarewell
// ---------------------------------------------------------------------------

// handleAddFarewellCommand appends one or more farewell message templates to the
// group's pool. Separate multiple messages with | to add them all at once.
//
// Usage:
//
//	!addfarewell Até mais, {numero}! 👋
//	!addfarewell Tchau {numero}!|Até mais, {numero}! 👋
func (h *Handler) handleAddFarewellCommand(args []string, msgTrigger types.MessageInfo) {
	if !msgTrigger.IsGroup {
		return
	}
	if !h.requireGroupAdmin(msgTrigger) {
		return
	}

	if len(args) == 0 {
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.ID,
			"❌ Informe o texto da mensagem de despedida.\n"+
				"Exemplo: !addfarewell Até mais, {numero}! 👋\n"+
				"Separe múltiplas mensagens com |")
		return
	}

	newMessages := splitPipe(strings.Join(args, " "))
	if len(newMessages) == 0 {
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.ID,
			"❌ Nenhuma mensagem válida encontrada.")
		return
	}

	settings := h.loadOrDefaultSettings(msgTrigger.Chat.User)
	settings.FarewellMessages = append(settings.FarewellMessages, newMessages...)

	if err := h.saveAndInvalidate(settings); err != nil {
		h.logger.Error("handleAddFarewellCommand: failed to save settings", "error", err)
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.ID,
			"❌ Erro ao salvar mensagem. Tente novamente.")
		return
	}

	h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.ID,
		formatAddedMessages("despedida", newMessages, len(settings.FarewellMessages)))
}

// handleDelFarewellCommand removes a farewell message by its 1-based index.
// When called without an index, it lists the current messages.
//
// Usage:
//
//	!delfarewell      → lists current messages with indices
//	!delfarewell <n>  → removes message at index n
func (h *Handler) handleDelFarewellCommand(args []string, msgTrigger types.MessageInfo) {
	if !msgTrigger.IsGroup {
		return
	}
	if !h.requireGroupAdmin(msgTrigger) {
		return
	}

	settings := h.loadOrDefaultSettings(msgTrigger.Chat.User)

	if len(args) == 0 {
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.ID,
			formatMessageList("Mensagens de despedida atuais", settings.FarewellMessages,
				"!delfarewell"))
		return
	}

	idx, err := strconv.Atoi(args[0])
	if err != nil || idx < 1 || idx > len(settings.FarewellMessages) {
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.ID,
			"❌ Índice inválido. Use !delfarewell (sem número) para ver a lista.")
		return
	}

	removed := settings.FarewellMessages[idx-1]
	settings.FarewellMessages = append(
		settings.FarewellMessages[:idx-1],
		settings.FarewellMessages[idx:]...,
	)

	if err := h.saveAndInvalidate(settings); err != nil {
		h.logger.Error("handleDelFarewellCommand: failed to save settings", "error", err)
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.ID,
			"❌ Erro ao salvar. Tente novamente.")
		return
	}

	h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.ID,
		fmt.Sprintf("✅ Mensagem de despedida #%d removida:\n\n_%s_\n\n"+
			"Total restante: %d", idx, removed, len(settings.FarewellMessages)))
}

// ---------------------------------------------------------------------------
// !resumo  (daily summary toggle)
// ---------------------------------------------------------------------------

// handleDailySummaryToggle flips the automatic daily summary on/off for this
// group. Only group admins can use this command.
//
// Usage: !resumo
func (h *Handler) handleDailySummaryToggle(args []string, msgTrigger types.MessageInfo) {
	if !msgTrigger.IsGroup {
		return
	}
	if !h.requireGroupAdmin(msgTrigger) {
		return
	}

	settings := h.loadOrDefaultSettings(msgTrigger.Chat.User)
	settings.DailySummaryEnabled = !settings.DailySummaryEnabled

	if err := h.saveAndInvalidate(settings); err != nil {
		h.logger.Error("handleDailySummaryToggle: failed to save settings", "error", err)
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.ID,
			"❌ Erro ao salvar configuração. Tente novamente.")
		return
	}

	status := "✅ ligado"
	if !settings.DailySummaryEnabled {
		status = "⛔ desligado"
	}
	h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.ID,
		fmt.Sprintf("🌙 Resumo diário automático: *%s*", status))
	h.logger.Info("Daily summary toggled", "chat", msgTrigger.Chat.User, "enabled", settings.DailySummaryEnabled)
}

// ---------------------------------------------------------------------------
// !ranking  (weekly ranking toggle)
// ---------------------------------------------------------------------------

// handleWeeklyRankingToggle flips the automatic weekly ranking on/off for this
// group. Only group admins can use this command.
//
// Usage: !ranking
func (h *Handler) handleWeeklyRankingToggle(args []string, msgTrigger types.MessageInfo) {
	if !msgTrigger.IsGroup {
		return
	}
	if !h.requireGroupAdmin(msgTrigger) {
		return
	}

	settings := h.loadOrDefaultSettings(msgTrigger.Chat.User)
	settings.WeeklyRankingEnabled = !settings.WeeklyRankingEnabled

	if err := h.saveAndInvalidate(settings); err != nil {
		h.logger.Error("handleWeeklyRankingToggle: failed to save settings", "error", err)
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.ID,
			"❌ Erro ao salvar configuração. Tente novamente.")
		return
	}

	status := "✅ ligado"
	if !settings.WeeklyRankingEnabled {
		status = "⛔ desligado"
	}
	h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.ID,
		fmt.Sprintf("🏆 Ranking semanal: *%s*", status))
	h.logger.Info("Weekly ranking toggled", "chat", msgTrigger.Chat.User, "enabled", settings.WeeklyRankingEnabled)
}

// ---------------------------------------------------------------------------
// !admincache  (flush GroupInfo cache for this group)
// ---------------------------------------------------------------------------

// handleAdminCacheCommand evicts the cached GroupInfo for the current group,
// forcing the next admin check to fetch fresh data from the WhatsApp API.
// Useful after promoting or demoting participants when you don't want to wait
// for the 5-minute TTL to expire.
//
// Usage: !admincache
func (h *Handler) handleAdminCacheCommand(msgTrigger types.MessageInfo) {
	if !msgTrigger.IsGroup {
		return
	}
	if !h.requireGroupAdmin(msgTrigger) {
		return
	}

	h.invalidateGroupInfoCache(msgTrigger.Chat.String())
	h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.ID,
		"🔄 Cache de admins atualizado! A próxima validação buscará dados frescos do WhatsApp.")
	h.logger.Info("Admin cache flushed manually", "chat", msgTrigger.Chat.User)
}

// ---------------------------------------------------------------------------
// !welcome  (list welcome messages — admin only)
// ---------------------------------------------------------------------------

// handleListWelcomeCommand lists the welcome message templates configured for
// this group. Only group admins can use this command.
//
// Usage: !welcome
func (h *Handler) handleListWelcomeCommand(msgTrigger types.MessageInfo) {
	if !msgTrigger.IsGroup {
		return
	}
	if !h.requireGroupAdmin(msgTrigger) {
		return
	}
	pool := h.resolveWelcomePool(msgTrigger.Chat.User)
	h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.ID,
		formatMessageList("Mensagens de boas-vindas", pool, ""))
}

// ---------------------------------------------------------------------------
// !farewell  (list farewell messages — admin only)
// ---------------------------------------------------------------------------

// handleListFarewellCommand lists the farewell message templates configured for
// this group. Only group admins can use this command.
//
// Usage: !farewell
func (h *Handler) handleListFarewellCommand(msgTrigger types.MessageInfo) {
	if !msgTrigger.IsGroup {
		return
	}
	if !h.requireGroupAdmin(msgTrigger) {
		return
	}
	pool := h.resolveFarewellPool(msgTrigger.Chat.User)
	h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.ID,
		formatMessageList("Mensagens de despedida", pool, ""))
}

// ---------------------------------------------------------------------------
// !configs  (settings panel — admin only)
// ---------------------------------------------------------------------------

// handleConfigsCommand replies with a summary panel of all current group
// settings. Only group admins can use this command.
//
// Usage: !configs
func (h *Handler) handleConfigsCommand(msgTrigger types.MessageInfo) {
	if !msgTrigger.IsGroup {
		return
	}
	if !h.requireGroupAdmin(msgTrigger) {
		return
	}

	settings := h.loadOrDefaultSettings(msgTrigger.Chat.User)

	// Booleans formatted as emoji labels.
	boolLabel := func(enabled bool) string {
		if enabled {
			return "✅ ligado"
		}
		return "⛔ desligado"
	}

	// Rules display: show first 120 chars to keep the panel compact.
	rulesDisplay := "_nenhuma definida_"
	if settings.Rules != "" {
		if len(settings.Rules) > 120 {
			rulesDisplay = settings.Rules[:120] + "…"
		} else {
			rulesDisplay = settings.Rules
		}
	}

	welcomeCount := len(settings.WelcomeMessages)
	farewellCount := len(settings.FarewellMessages)

	msg := fmt.Sprintf(
		"⚙️ *Configurações do grupo*\n\n"+
			"👋 Boas-vindas: *%d* mensagem(s)\n"+
			"👋 Despedidas: *%d* mensagem(s)\n"+
			"🌙 Resumo diário: *%s*\n"+
			"🏆 Ranking semanal: *%s*\n"+
			"📜 Regras: %s",
		welcomeCount,
		farewellCount,
		boolLabel(settings.DailySummaryEnabled),
		boolLabel(settings.WeeklyRankingEnabled),
		rulesDisplay,
	)

	h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.ID, msg)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// splitPipe splits a string by the | separator, trims whitespace from each
// part, and discards empty parts. Used to support bulk-add via pipe syntax.
func splitPipe(s string) []string {
	parts := strings.Split(s, "|")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			result = append(result, t)
		}
	}
	return result
}

// formatAddedMessages builds a confirmation reply after adding one or more
// message templates to a pool.
func formatAddedMessages(kind string, added []string, total int) string {
	var sb strings.Builder
	if len(added) == 1 {
		sb.WriteString(fmt.Sprintf("✅ Mensagem de %s adicionada (total: %d):\n\n", kind, total))
	} else {
		sb.WriteString(fmt.Sprintf("✅ %d mensagens de %s adicionadas (total: %d):\n\n", len(added), kind, total))
	}
	for i, m := range added {
		sb.WriteString(fmt.Sprintf("%d. _%s_\n", i+1, m))
	}
	return sb.String()
}

// formatMessageList builds a numbered list of message templates for display.
// When deleteCmd is empty, the "Para remover:" hint is omitted.
func formatMessageList(title string, messages []string, deleteCmd string) string {
	if len(messages) == 0 {
		return fmt.Sprintf("ℹ️ %s: nenhuma mensagem configurada.", title)
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("📋 *%s* (%d):\n\n", title, len(messages)))
	for i, msg := range messages {
		sb.WriteString(fmt.Sprintf("%d. _%s_\n", i+1, msg))
	}
	if deleteCmd != "" {
		sb.WriteString(fmt.Sprintf("\nPara remover: %s <número>", deleteCmd))
	}
	return sb.String()
}
