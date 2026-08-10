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
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID, errNotAdmin)
		return false
	}
	return true
}

// loadOrDefaultSettings fetches the GroupSettings for a group, creating a
// default struct (not yet persisted) if no record exists in the DB.
// WelcomeMessages and FarewellMessages are NOT populated here — they are
// managed separately via Add/Delete/Get DB methods.
func (h *Handler) loadOrDefaultSettings(chatID string) wstypes.GroupSettings {
	if gs := h.getGroupSettings(chatID); gs != nil {
		return *gs
	}
	return wstypes.GroupSettings{
		ChatID:               chatID,
		DailySummaryEnabled:  true,
		WeeklyRankingEnabled: true,
		ChatbotEnabled:       true,
		DefaultPersonality:   "resumobot",
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
// Personality helpers
// ---------------------------------------------------------------------------

// defaultPersonalityFallback is the global fallback used when a group has no
// default_personality configured in the database.
const defaultPersonalityFallback = "resumobot"

// getGroupDefaultPersonality returns the default personality for a group.
// If the group has no record in the DB, or the field is empty, it returns
// defaultPersonalityFallback ("resumobot").
func (h *Handler) getGroupDefaultPersonality(chatID string) string {
	settings := h.getGroupSettings(chatID)
	if settings != nil && settings.DefaultPersonality != "" {
		return settings.DefaultPersonality
	}
	return defaultPersonalityFallback
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
func (h *Handler) handleSetRulesCommand(rawArgs string, msgTrigger types.MessageInfo) {
	// Display current rules when called with no arguments.
	if strings.TrimSpace(rawArgs) == "" {
		h.handleRulesCommand(msgTrigger)
		return
	}

	if !h.requireGroupAdmin(msgTrigger) {
		return
	}

	// TrimSpace only removes whitespace from the edges; internal \n characters are preserved.
	rules := strings.TrimSpace(rawArgs)
	settings := h.loadOrDefaultSettings(msgTrigger.Chat.User)
	settings.Rules = rules

	if err := h.saveAndInvalidate(settings); err != nil {
		h.logger.Error("handleSetRulesCommand: failed to save settings", "error", err)
		h.reactToCommand(msgTrigger, "❌")
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID,
			"❌ Erro ao salvar regras. Tente novamente.")
		return
	}

	// React with ✅ to confirm the rules were saved.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := h.whatsappService.ReactToMessage(ctx, msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID, "✅"); err != nil {
		h.logger.Warn("handleSetRulesCommand: failed to send reaction, falling back to reply", "error", err)
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID,
			"✅ Regras do grupo atualizadas! Use !regras para visualizá-las.")
	}
	h.logger.Info("Group rules updated", "chat", msgTrigger.Chat.User)
}

// ---------------------------------------------------------------------------
// !addwelcome / !delwelcome
// ---------------------------------------------------------------------------

// handleAddWelcomeCommand appends one or more welcome message templates to the
// group's pool in the welcome_messages table.
// Separate multiple messages with | to add them all at once.
// Use {numero} in templates to mention the joining participant(s).
// Use {regras} in templates to insert the group rules.
//
// Usage:
//
//	!addwelcome Olá {numero}! Seja bem-vindo(a)! Leia as regras: {regras}
//	!addwelcome Oi {numero}!|Bem-vindo(a), {numero}! 🎉
func (h *Handler) handleAddWelcomeCommand(rawArgs string, msgTrigger types.MessageInfo) {
	if !h.requireGroupAdmin(msgTrigger) {
		return
	}

	if strings.TrimSpace(rawArgs) == "" {
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID,
			"❌ Informe o texto da mensagem de boas-vindas.\n"+
				"Exemplo: !addwelcome Olá {numero}! Leia: {regras}\n"+
				"Separe múltiplas mensagens com |")
		return
	}

	newMessages := splitPipe(rawArgs)
	if len(newMessages) == 0 {
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID,
			"❌ Nenhuma mensagem válida encontrada.")
		return
	}

	chatID := msgTrigger.Chat.User

	// Ensure group_configs row exists before inserting into welcome_messages (FK).
	settings := h.loadOrDefaultSettings(chatID)
	if err := h.saveAndInvalidate(settings); err != nil {
		h.logger.Error("handleAddWelcomeCommand: failed to ensure group_configs row", "error", err)
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID,
			"❌ Erro ao salvar mensagem. Tente novamente.")
		return
	}

	for _, msg := range newMessages {
		if err := h.dbService.AddWelcomeMessage(chatID, msg); err != nil {
			h.logger.Error("handleAddWelcomeCommand: failed to add welcome message", "error", err)
			h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID,
				"❌ Erro ao salvar mensagem. Tente novamente.")
			return
		}
	}

	allMsgs, _ := h.dbService.GetWelcomeMessages(chatID)
	h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID,
		formatAddedMessages("boas-vindas", newMessages, len(allMsgs)))
}

// handleDelWelcomeCommand removes a welcome message by its database ID.
// The ID is shown by !welcome (e.g. "[3] Olá!") and remains stable across deletions.
//
// Usage:  !delwelcome <id>  — removes the message whose DB id equals <id>
func (h *Handler) handleDelWelcomeCommand(args []string, msgTrigger types.MessageInfo) {
	if !h.requireGroupAdmin(msgTrigger) {
		return
	}

	if len(args) == 0 {
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID,
			"❌ Informe o ID da mensagem a remover. Use !welcome para ver a lista.")
		return
	}

	targetID, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil || targetID < 1 {
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID,
			"❌ ID inválido. Use !welcome para ver os IDs disponíveis.")
		return
	}

	// Fetch the group's messages to validate that targetID belongs to this group.
	msgs, err := h.dbService.GetWelcomeMessages(msgTrigger.Chat.User)
	if err != nil {
		h.logger.Error("handleDelWelcomeCommand: failed to fetch messages", "error", err)
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID,
			"❌ Erro ao buscar mensagens. Tente novamente.")
		return
	}

	var target *wstypes.WelcomeMessage
	for i := range msgs {
		if msgs[i].ID == targetID {
			target = &msgs[i]
			break
		}
	}
	if target == nil {
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID,
			fmt.Sprintf("❌ Nenhuma mensagem de boas-vindas com ID %d. Use !welcome para ver a lista.", targetID))
		return
	}

	if err := h.dbService.DeleteWelcomeMessage(target.ID); err != nil {
		h.logger.Error("handleDelWelcomeCommand: failed to delete", "error", err, "id", target.ID)
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID,
			"❌ Erro ao remover. Tente novamente.")
		return
	}

	h.invalidateGroupSettings(msgTrigger.Chat.User)
	h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID,
		fmt.Sprintf("✅ Mensagem de boas-vindas [%d] removida:\n\n_%s_\n\nTotal restante: %d",
			target.ID, target.Message, len(msgs)-1))
}

// ---------------------------------------------------------------------------
// !addfarewell / !delfarewell
// ---------------------------------------------------------------------------

// handleAddFarewellCommand appends one or more farewell message templates to the
// group's pool in the farewell_messages table.
// Separate multiple messages with | to add them all at once.
// Use {numero} in templates to mention the leaving participant(s).
// Use {regras} in templates to insert the group rules.
//
// Usage:
//
//	!addfarewell Até mais, {numero}! 👋 Leia as regras: {regras}
//	!addfarewell Tchau {numero}!|Até mais, {numero}! 👋
func (h *Handler) handleAddFarewellCommand(rawArgs string, msgTrigger types.MessageInfo) {
	if !h.requireGroupAdmin(msgTrigger) {
		return
	}

	if strings.TrimSpace(rawArgs) == "" {
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID,
			"❌ Informe o texto da mensagem de despedida.\n"+
				"Exemplo: !addfarewell Até mais, {numero}! 👋 Leia: {regras}\n"+
				"Separe múltiplas mensagens com |")
		return
	}

	newMessages := splitPipe(rawArgs)
	if len(newMessages) == 0 {
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID,
			"❌ Nenhuma mensagem válida encontrada.")
		return
	}

	chatID := msgTrigger.Chat.User

	// Ensure group_configs row exists before inserting into farewell_messages (FK).
	settings := h.loadOrDefaultSettings(chatID)
	if err := h.saveAndInvalidate(settings); err != nil {
		h.logger.Error("handleAddFarewellCommand: failed to ensure group_configs row", "error", err)
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID,
			"❌ Erro ao salvar mensagem. Tente novamente.")
		return
	}

	for _, msg := range newMessages {
		if err := h.dbService.AddFarewellMessage(chatID, msg); err != nil {
			h.logger.Error("handleAddFarewellCommand: failed to add farewell message", "error", err)
			h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID,
				"❌ Erro ao salvar mensagem. Tente novamente.")
			return
		}
	}

	allMsgs, _ := h.dbService.GetFarewellMessages(chatID)
	h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID,
		formatAddedMessages("despedida", newMessages, len(allMsgs)))
}

// handleDelFarewellCommand removes a farewell message by its database ID.
// The ID is shown by !farewell (e.g. "[3] Até mais!") and remains stable across deletions.
//
// Usage:  !delfarewell <id>  — removes the message whose DB id equals <id>
func (h *Handler) handleDelFarewellCommand(args []string, msgTrigger types.MessageInfo) {
	if !h.requireGroupAdmin(msgTrigger) {
		return
	}

	if len(args) == 0 {
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID,
			"❌ Informe o ID da mensagem a remover. Use !farewell para ver a lista.")
		return
	}

	targetID, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil || targetID < 1 {
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID,
			"❌ ID inválido. Use !farewell para ver os IDs disponíveis.")
		return
	}

	// Fetch the group's messages to validate that targetID belongs to this group.
	msgs, err := h.dbService.GetFarewellMessages(msgTrigger.Chat.User)
	if err != nil {
		h.logger.Error("handleDelFarewellCommand: failed to fetch messages", "error", err)
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID,
			"❌ Erro ao buscar mensagens. Tente novamente.")
		return
	}

	var target *wstypes.FarewellMessage
	for i := range msgs {
		if msgs[i].ID == targetID {
			target = &msgs[i]
			break
		}
	}
	if target == nil {
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID,
			fmt.Sprintf("❌ Nenhuma mensagem de despedida com ID %d. Use !farewell para ver a lista.", targetID))
		return
	}

	if err := h.dbService.DeleteFarewellMessage(target.ID); err != nil {
		h.logger.Error("handleDelFarewellCommand: failed to delete", "error", err, "id", target.ID)
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID,
			"❌ Erro ao remover. Tente novamente.")
		return
	}

	h.invalidateGroupSettings(msgTrigger.Chat.User)
	h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID,
		fmt.Sprintf("✅ Mensagem de despedida [%d] removida:\n\n_%s_\n\nTotal restante: %d",
			target.ID, target.Message, len(msgs)-1))
}

// ---------------------------------------------------------------------------
// !resumodia  (daily summary toggle)
// ---------------------------------------------------------------------------

// handleDailySummaryToggle flips the automatic daily summary on/off for this
// group. Only group admins can use this command.
//
// Usage: !resumodia
func (h *Handler) handleDailySummaryToggle(args []string, msgTrigger types.MessageInfo) {
	if !h.requireGroupAdmin(msgTrigger) {
		return
	}

	settings := h.loadOrDefaultSettings(msgTrigger.Chat.User)
	prevEnabled := settings.DailySummaryEnabled
	settings.DailySummaryEnabled = !settings.DailySummaryEnabled

	if err := h.saveAndInvalidate(settings); err != nil {
		h.logger.Error("handleDailySummaryToggle: failed to save settings", "error", err)
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID,
			"❌ Erro ao salvar configuração. Tente novamente.")
		return
	}

	prevStatus := "✅ ligado"
	if !prevEnabled {
		prevStatus = "⛔ desligado"
	}
	newStatus := "✅ ligado"
	if !settings.DailySummaryEnabled {
		newStatus = "⛔ desligado"
	}
	h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID,
		fmt.Sprintf("🌙 Resumo diário automático: %s → *%s*", prevStatus, newStatus))
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
	if !h.requireGroupAdmin(msgTrigger) {
		return
	}

	settings := h.loadOrDefaultSettings(msgTrigger.Chat.User)
	prevEnabled := settings.WeeklyRankingEnabled
	settings.WeeklyRankingEnabled = !settings.WeeklyRankingEnabled

	if err := h.saveAndInvalidate(settings); err != nil {
		h.logger.Error("handleWeeklyRankingToggle: failed to save settings", "error", err)
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID,
			"❌ Erro ao salvar configuração. Tente novamente.")
		return
	}

	prevStatus := "✅ ligado"
	if !prevEnabled {
		prevStatus = "⛔ desligado"
	}
	newStatus := "✅ ligado"
	if !settings.WeeklyRankingEnabled {
		newStatus = "⛔ desligado"
	}
	h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID,
		fmt.Sprintf("🏆 Ranking semanal: %s → *%s*", prevStatus, newStatus))
	h.logger.Info("Weekly ranking toggled", "chat", msgTrigger.Chat.User, "enabled", settings.WeeklyRankingEnabled)
}

// ---------------------------------------------------------------------------
// !chatbot  (chatbot mention/reply toggle)
// ---------------------------------------------------------------------------

// handleChatbotToggle enables or disables the chatbot feature (mention/reply
// responses) for the current group. Only group admins can use this command.
//
// Usage:
//
//	!chatbot        → toggles current state
//	!chatbot on     → enables explicitly
//	!chatbot off    → disables explicitly
func (h *Handler) handleChatbotToggle(args []string, msgTrigger types.MessageInfo) {
	if !h.requireGroupAdmin(msgTrigger) {
		return
	}

	settings := h.loadOrDefaultSettings(msgTrigger.Chat.User)
	prevEnabled := settings.ChatbotEnabled

	// Support explicit on/off arguments; fall back to toggle when absent.
	if len(args) > 0 {
		switch strings.ToLower(args[0]) {
		case "on", "ativar", "ligar":
			settings.ChatbotEnabled = true
		case "off", "desativar", "desligar":
			settings.ChatbotEnabled = false
		default:
			h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID,
				"❌ Uso: !chatbot [on|off]\n\n• !chatbot on  → ativa respostas a menções e replies\n• !chatbot off → desativa\n• !chatbot     → alterna o estado atual")
			return
		}
	} else {
		settings.ChatbotEnabled = !settings.ChatbotEnabled
	}

	if err := h.saveAndInvalidate(settings); err != nil {
		h.logger.Error("handleChatbotToggle: failed to save settings", "error", err)
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID,
			"❌ Erro ao salvar configuração. Tente novamente.")
		return
	}

	prevStatus := "✅ ligado"
	if !prevEnabled {
		prevStatus = "⛔ desligado"
	}
	newStatus := "✅ ligado"
	if !settings.ChatbotEnabled {
		newStatus = "⛔ desligado"
	}
	h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID,
		fmt.Sprintf("🤖 Chatbot (menções/replies): %s → *%s*", prevStatus, newStatus))
	h.logger.Info("Chatbot toggled", "chat", msgTrigger.Chat.User, "enabled", settings.ChatbotEnabled)
}

// ---------------------------------------------------------------------------
// !personalidade  (set default personality — admin only)
// !personalidades (list all personalities — everyone)
// ---------------------------------------------------------------------------

// handleSetPersonalityCommand sets (or displays) the default personality for this
// group. Only admins can change it; any member can call with no argument to view it.
//
// Usage:
//
//	!personalidade           → displays current default personality
//	!personalidade <nome>    → sets the default personality (admin only)
func (h *Handler) handleSetPersonalityCommand(args []string, msgTrigger types.MessageInfo) {
	chatID := msgTrigger.Chat.User

	// No argument → show current personality (everyone can do this).
	if len(args) == 0 || strings.TrimSpace(args[0]) == "" {
		current := h.getGroupDefaultPersonality(chatID)
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID,
			fmt.Sprintf("🎭 *Personalidade padrão do grupo:* %s\n\nUse !personalidades para ver as disponíveis.", current))
		return
	}

	// Changing requires admin.
	if !h.requireGroupAdmin(msgTrigger) {
		return
	}

	name := strings.ToLower(strings.TrimSpace(args[0]))

	// Validate that the personality exists.
	available := h.personalityLoader.ListAvailable()
	healthy, exists := available[name]
	if !exists {
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID,
			fmt.Sprintf("❌ Personalidade *%s* não encontrada.\n\nUse !personalidades para ver as disponíveis.", name))
		return
	}
	if !healthy {
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID,
			fmt.Sprintf("❌ Personalidade *%s* está com erro de configuração (arquivo .toml inválido).\n\nUse !personalidades para ver as disponíveis.", name))
		return
	}

	prev := h.getGroupDefaultPersonality(chatID)
	settings := h.loadOrDefaultSettings(chatID)
	settings.DefaultPersonality = name

	if err := h.saveAndInvalidate(settings); err != nil {
		h.logger.Error("handleSetPersonalityCommand: failed to save settings", "error", err)
		h.reactToCommand(msgTrigger, "❌")
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID,
			"❌ Erro ao salvar personalidade. Tente novamente.")
		return
	}

	h.reactToCommand(msgTrigger, "✅")
	h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID,
		fmt.Sprintf("🎭 Personalidade padrão: *%s* → *%s*\n\nEssa personalidade será usada no resumo diário, chatbot e em todos os resumos sem personalidade explícita.", prev, name))
	h.logger.Info("Default personality changed", "chat", chatID, "from", prev, "to", name)
}

// handleListPersonalitiesCommand lists all available personalities.
// Available to every member of the group (no admin required).
//
// Usage: !personalidades
func (h *Handler) handleListPersonalitiesCommand(msgTrigger types.MessageInfo) {
	available := h.personalityLoader.ListAvailable()
	if len(available) == 0 {
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID,
			"❌ Nenhuma personalidade disponível. Verifique os arquivos .toml no servidor.")
		return
	}

	current := h.getGroupDefaultPersonality(msgTrigger.Chat.User)

	var sb strings.Builder
	sb.WriteString("🎭 *Personalidades disponíveis:*\n\n")

	// Sort names for deterministic output.
	names := make([]string, 0, len(available))
	for n := range available {
		names = append(names, n)
	}
	sortStrings(names)

	for _, n := range names {
		healthy := available[n]
		status := "✅"
		if !healthy {
			status = "⚠️ (erro)"
		}
		marker := ""
		if n == current {
			marker = " ← *padrão do grupo*"
		}
		sb.WriteString(fmt.Sprintf("%s *%s*%s\n", status, n, marker))
	}

	sb.WriteString("\nPara mudar: !personalidade <nome> (apenas admins).")
	h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID, sb.String())
}

// sortStrings sorts a string slice in-place (ascending).
func sortStrings(ss []string) {
	for i := 1; i < len(ss); i++ {
		for j := i; j > 0 && ss[j] < ss[j-1]; j-- {
			ss[j], ss[j-1] = ss[j-1], ss[j]
		}
	}
}

// ---------------------------------------------------------------------------
// !cache  (flush GroupInfo cache for this group)
// ---------------------------------------------------------------------------

// handleAdminCacheCommand evicts the cached GroupInfo for the current group,
// forcing the next admin check to fetch fresh data from the WhatsApp API.
// Useful after promoting or demoting participants when you don't want to wait
// for the 5-minute TTL to expire.
//
// Usage: !cache
func (h *Handler) handleAdminCacheCommand(msgTrigger types.MessageInfo) {
	if !h.requireGroupAdmin(msgTrigger) {
		return
	}

	h.invalidateGroupInfoCache(msgTrigger.Chat.String())
	h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID,
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
	if !h.requireGroupAdmin(msgTrigger) {
		return
	}
	dbMsgs, err := h.dbService.GetWelcomeMessages(msgTrigger.Chat.User)
	if err != nil {
		h.logger.Error("handleListWelcomeCommand: DB error", "error", err)
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID,
			"❌ Erro ao buscar mensagens.")
		return
	}
	h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID,
		formatWelcomeMessageList(dbMsgs))
}

// ---------------------------------------------------------------------------
// !farewell  (list farewell messages — admin only)
// ---------------------------------------------------------------------------

// handleListFarewellCommand lists the farewell message templates configured for
// this group. Only group admins can use this command.
//
// Usage: !farewell
func (h *Handler) handleListFarewellCommand(msgTrigger types.MessageInfo) {
	if !h.requireGroupAdmin(msgTrigger) {
		return
	}
	dbMsgs, err := h.dbService.GetFarewellMessages(msgTrigger.Chat.User)
	if err != nil {
		h.logger.Error("handleListFarewellCommand: DB error", "error", err)
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID,
			"❌ Erro ao buscar mensagens.")
		return
	}
	h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID,
		formatFarewellMessageList(dbMsgs))
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

// formatWelcomeMessageList builds the reply for !welcome showing each message
// with its stable database ID in square brackets, e.g.:
//
//	📋 *Mensagens de boas-vindas* (2):
//	[1] _Bem-vindo ao grupo!_
//	[3] _Olá, seja bem-vindo!_
//
// Use !delwelcome <id> to remove a message.
func formatWelcomeMessageList(msgs []wstypes.WelcomeMessage) string {
	if len(msgs) == 0 {
		return "ℹ️ Mensagens de boas-vindas: nenhuma mensagem configurada."
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("📋 *Mensagens de boas-vindas* (%d):\n\n", len(msgs)))
	for _, m := range msgs {
		sb.WriteString(fmt.Sprintf("[%d] _%s_\n", m.ID, m.Message))
	}
	sb.WriteString("\nPara remover: !delwelcome <id>")
	return sb.String()
}

// formatFarewellMessageList builds the reply for !farewell showing each message
// with its stable database ID in square brackets, e.g.:
//
//	📋 *Mensagens de despedida* (2):
//	[2] _Até mais!_
//	[4] _Tchau, {numero}!_
//
// Use !delfarewell <id> to remove a message.
func formatFarewellMessageList(msgs []wstypes.FarewellMessage) string {
	if len(msgs) == 0 {
		return "ℹ️ Mensagens de despedida: nenhuma mensagem configurada."
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("📋 *Mensagens de despedida* (%d):\n\n", len(msgs)))
	for _, m := range msgs {
		sb.WriteString(fmt.Sprintf("[%d] _%s_\n", m.ID, m.Message))
	}
	sb.WriteString("\nPara remover: !delfarewell <id>")
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
