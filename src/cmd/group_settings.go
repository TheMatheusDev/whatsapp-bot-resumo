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
// If not, sends errNotAdmin and returns false.
func (h *Handler) requireGroupAdmin(msgTrigger types.MessageInfo) bool {
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

// handleAddWelcomeCommand appends a welcome message template to the group's pool.
// Use {numero} in the template to mention the joining participant(s).
//
// Usage:  !addwelcome Olá {numero}! Seja bem-vindo(a)!
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
				"Exemplo: !addwelcome Olá {numero}! Seja bem-vindo(a)!")
		return
	}

	template := strings.Join(args, " ")
	settings := h.loadOrDefaultSettings(msgTrigger.Chat.User)
	settings.WelcomeMessages = append(settings.WelcomeMessages, template)

	if err := h.saveAndInvalidate(settings); err != nil {
		h.logger.Error("handleAddWelcomeCommand: failed to save settings", "error", err)
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.ID,
			"❌ Erro ao salvar mensagem. Tente novamente.")
		return
	}

	reply := fmt.Sprintf("✅ Mensagem de boas-vindas adicionada (total: %d):\n\n_%s_",
		len(settings.WelcomeMessages), template)
	h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.ID, reply)
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
// !addfarewall / !delfarewall
// ---------------------------------------------------------------------------

// handleAddFarewallCommand appends a farewell message template to the group's pool.
//
// Usage:  !addfarewall Até mais, {numero}! 👋
func (h *Handler) handleAddFarewallCommand(args []string, msgTrigger types.MessageInfo) {
	if !msgTrigger.IsGroup {
		return
	}
	if !h.requireGroupAdmin(msgTrigger) {
		return
	}

	if len(args) == 0 {
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.ID,
			"❌ Informe o texto da mensagem de despedida.\n"+
				"Exemplo: !addfarewall Até mais, {numero}! 👋")
		return
	}

	template := strings.Join(args, " ")
	settings := h.loadOrDefaultSettings(msgTrigger.Chat.User)
	settings.FarewellMessages = append(settings.FarewellMessages, template)

	if err := h.saveAndInvalidate(settings); err != nil {
		h.logger.Error("handleAddFarewallCommand: failed to save settings", "error", err)
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.ID,
			"❌ Erro ao salvar mensagem. Tente novamente.")
		return
	}

	reply := fmt.Sprintf("✅ Mensagem de despedida adicionada (total: %d):\n\n_%s_",
		len(settings.FarewellMessages), template)
	h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.ID, reply)
}

// handleDelFarewallCommand removes a farewell message by its 1-based index.
// When called without an index, it lists the current messages.
//
// Usage:
//
//	!delfarewall      → lists current messages with indices
//	!delfarewall <n>  → removes message at index n
func (h *Handler) handleDelFarewallCommand(args []string, msgTrigger types.MessageInfo) {
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
				"!delfarewall"))
		return
	}

	idx, err := strconv.Atoi(args[0])
	if err != nil || idx < 1 || idx > len(settings.FarewellMessages) {
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.ID,
			"❌ Índice inválido. Use !delfarewall (sem número) para ver a lista.")
		return
	}

	removed := settings.FarewellMessages[idx-1]
	settings.FarewellMessages = append(
		settings.FarewellMessages[:idx-1],
		settings.FarewellMessages[idx:]...,
	)

	if err := h.saveAndInvalidate(settings); err != nil {
		h.logger.Error("handleDelFarewallCommand: failed to save settings", "error", err)
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.ID,
			"❌ Erro ao salvar. Tente novamente.")
		return
	}

	h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.ID,
		fmt.Sprintf("✅ Mensagem de despedida #%d removida:\n\n_%s_\n\n"+
			"Total restante: %d", idx, removed, len(settings.FarewellMessages)))
}

// ---------------------------------------------------------------------------
// !resumo on|off  (daily summary toggle)
// ---------------------------------------------------------------------------

// handleDailySummaryToggle enables or disables the automatic daily summary
// for this group. Only group admins can use this command.
//
// Usage:  !resumo on | !resumo off
func (h *Handler) handleDailySummaryToggle(state string, msgTrigger types.MessageInfo) {
	if !msgTrigger.IsGroup {
		return
	}
	if !h.requireGroupAdmin(msgTrigger) {
		return
	}

	enabled := state == "on"
	settings := h.loadOrDefaultSettings(msgTrigger.Chat.User)
	settings.DailySummaryEnabled = enabled

	if err := h.saveAndInvalidate(settings); err != nil {
		h.logger.Error("handleDailySummaryToggle: failed to save settings", "error", err)
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.ID,
			"❌ Erro ao salvar configuração. Tente novamente.")
		return
	}

	status := "✅ ligado"
	if !enabled {
		status = "⛔ desligado"
	}
	h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.ID,
		fmt.Sprintf("🌙 Resumo diário automático: *%s*", status))
	h.logger.Info("Daily summary toggled", "chat", msgTrigger.Chat.User, "enabled", enabled)
}

// ---------------------------------------------------------------------------
// !ranking on|off  (weekly ranking toggle)
// ---------------------------------------------------------------------------

// handleWeeklyRankingToggle enables or disables the automatic weekly ranking
// for this group. Only group admins can use this command.
//
// Usage:  !ranking on | !ranking off
func (h *Handler) handleWeeklyRankingToggle(args []string, msgTrigger types.MessageInfo) {
	if !msgTrigger.IsGroup {
		return
	}

	if len(args) == 0 || (strings.ToLower(args[0]) != "on" && strings.ToLower(args[0]) != "off") {
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.ID,
			"ℹ️ Use: !ranking on ou !ranking off")
		return
	}

	if !h.requireGroupAdmin(msgTrigger) {
		return
	}

	enabled := strings.ToLower(args[0]) == "on"
	settings := h.loadOrDefaultSettings(msgTrigger.Chat.User)
	settings.WeeklyRankingEnabled = enabled

	if err := h.saveAndInvalidate(settings); err != nil {
		h.logger.Error("handleWeeklyRankingToggle: failed to save settings", "error", err)
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.ID,
			"❌ Erro ao salvar configuração. Tente novamente.")
		return
	}

	status := "✅ ligado"
	if !enabled {
		status = "⛔ desligado"
	}
	h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.ID,
		fmt.Sprintf("🏆 Ranking semanal: *%s*", status))
	h.logger.Info("Weekly ranking toggled", "chat", msgTrigger.Chat.User, "enabled", enabled)
}

// ---------------------------------------------------------------------------
// !welcome  (list welcome messages — open to all members)
// ---------------------------------------------------------------------------

// handleListWelcomeCommand lists the welcome message templates configured for
// this group. Any member can use this command.
//
// Usage: !welcome
func (h *Handler) handleListWelcomeCommand(msgTrigger types.MessageInfo) {
	if !msgTrigger.IsGroup {
		return
	}
	pool := h.resolveWelcomePool(msgTrigger.Chat.User)
	h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.ID,
		formatMessageList("Mensagens de boas-vindas", pool, ""))
}

// ---------------------------------------------------------------------------
// !farewall  (list farewell messages — open to all members)
// ---------------------------------------------------------------------------

// handleListFarewallCommand lists the farewell message templates configured for
// this group. Any member can use this command.
//
// Usage: !farewall
func (h *Handler) handleListFarewallCommand(msgTrigger types.MessageInfo) {
	if !msgTrigger.IsGroup {
		return
	}
	pool := h.resolveFarewellPool(msgTrigger.Chat.User)
	h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.ID,
		formatMessageList("Mensagens de despedida", pool, ""))
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

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
