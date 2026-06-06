package cmd

import (
	"fmt"
	"strings"

	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	wstypes "whatsapp-summarizer/src/types"
)



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

	msg := h.buildOnboardingMessage(settings)
	if strings.TrimSpace(msg) != "" {
		if err := h.whatsappService.SendMessage(evt.JID, msg); err != nil {
			h.logger.Error("JoinedGroup: failed to send onboarding message",
				"error", err, "chat", chatID)
		}
	}

	// Notify the owner via DM about the new group.
	h.notifyOwnerNewGroup(evt.JID)

	h.logger.Info("JoinedGroup: registered new group with default settings and sent onboarding",
		"chat", chatID)
}

// buildOnboardingMessage returns the onboarding message for a newly joined group.
// It uses BOT_ONBOARDING_MESSAGE from the environment and replaces status placeholders.
func (h *Handler) buildOnboardingMessage(settings wstypes.GroupSettings) string {
	msg := h.config.Bot.OnboardingMessage
	if strings.TrimSpace(msg) == "" {
		return ""
	}

	dailyStatus := "✅ ligado"
	if !settings.DailySummaryEnabled {
		dailyStatus = "⛔ desligado"
	}
	weeklyStatus := "✅ ligado"
	if !settings.WeeklyRankingEnabled {
		weeklyStatus = "⛔ desligado"
	}
	
	msg = strings.ReplaceAll(msg, "{daily_status}", dailyStatus)
	msg = strings.ReplaceAll(msg, "{weekly_status}", weeklyStatus)
	
	return msg
}

// notifyOwnerNewGroup sends a DM to OWNER_JID informing that the bot was added
// to a new group. Failures are logged but do not affect the onboarding flow.
func (h *Handler) notifyOwnerNewGroup(groupJID types.JID) {
	if h.config.WhatsApp.OwnerJID == "" {
		return
	}

	groupName := h.getGroupName(groupJID)
	ownerJID := types.NewJID(h.config.WhatsApp.OwnerJID, types.DefaultUserServer)
	msg := fmt.Sprintf("🤖 Bot adicionado ao grupo *%s* (`%s`)", groupName, groupJID.User)
	if err := h.whatsappService.SendMessage(ownerJID, msg); err != nil {
		h.logger.Warn("JoinedGroup: failed to notify owner", "error", err, "chat", groupJID.User)
	}
}
