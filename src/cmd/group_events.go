package cmd

import (
	"context"
	"math/rand"
	"strings"
	"time"

	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

// handleGroupInfoEvent handles participant join/leave events for all groups
// the bot belongs to.
func (h *Handler) handleGroupInfoEvent(evt *events.GroupInfo) {
	if evt == nil {
		return
	}

	// Skip join/leave notifications for historical events sent before the bot started.
	if evt.Timestamp.Before(h.botStartTime) {
		h.logger.Debug("Group event received but skipping welcome/farewell (event before bot started)",
			"event_timestamp", evt.Timestamp,
			"bot_start_time", h.botStartTime,
			"chat_id", evt.JID.String())
		return
	}

	chatID := evt.JID.User

	if len(evt.Join) > 0 {
		pool := h.resolveWelcomePool(chatID)
		if msg := pickRandom(pool); msg != "" {
			if err := h.sendParticipantStatusMessage(evt.JID, evt.Join, msg); err != nil {
				h.logger.Error("Failed to send welcome message", "error", err, "chat_id", evt.JID.String())
			}
		}
	}

	if len(evt.Leave) > 0 {
		pool := h.resolveFarewellPool(chatID)
		if msg := pickRandom(pool); msg != "" {
			if err := h.sendParticipantStatusMessage(evt.JID, evt.Leave, msg); err != nil {
				h.logger.Error("Failed to send farewell message", "error", err, "chat_id", evt.JID.String())
			}
		}
	}

	// Invalidate the GroupInfo cache whenever admin roles change so that the
	// next isGroupAdmin call fetches a fresh participant list from the API.
	if len(evt.Promote) > 0 || len(evt.Demote) > 0 {
		h.invalidateGroupInfoCache(evt.JID.String())
		h.logger.Info("GroupInfo cache invalidated due to admin role change",
			"chat_id", evt.JID.String(),
			"promoted", len(evt.Promote),
			"demoted", len(evt.Demote))
	}
}

// resolveWelcomePool returns the welcome message pool for a group.
// Per-group messages from the DB take priority; falls back to the global config.
func (h *Handler) resolveWelcomePool(chatID string) []string {
	dbMsgs, err := h.dbService.GetWelcomeMessages(chatID)
	if err != nil {
		h.logger.Error("resolveWelcomePool: DB error", "chat_id", chatID, "error", err)
	} else if len(dbMsgs) > 0 {
		pool := make([]string, 0, len(dbMsgs))
		for _, m := range dbMsgs {
			pool = append(pool, m.Message)
		}
		return pool
	}
	return h.config.Bot.WelcomeMessages
}

// resolveFarewellPool returns the farewell message pool for a group.
// Per-group messages from the DB take priority; falls back to the global config.
func (h *Handler) resolveFarewellPool(chatID string) []string {
	dbMsgs, err := h.dbService.GetFarewellMessages(chatID)
	if err != nil {
		h.logger.Error("resolveFarewellPool: DB error", "chat_id", chatID, "error", err)
	} else if len(dbMsgs) > 0 {
		pool := make([]string, 0, len(dbMsgs))
		for _, m := range dbMsgs {
			pool = append(pool, m.Message)
		}
		return pool
	}
	return h.config.Bot.FarewellMessages
}

// sendParticipantStatusMessage sends one consolidated message for all participants.
// The {numero} placeholder is replaced by participant mentions when present in the template.
func (h *Handler) sendParticipantStatusMessage(chatID types.JID, participants []types.JID, template string) error {
	template = strings.TrimSpace(template)
	if template == "" {
		return nil
	}

	if strings.Contains(template, "{regras}") {
		rules := h.config.Bot.Rules
		if s := h.getGroupSettings(chatID.User); s != nil && s.Rules != "" {
			rules = s.Rules
		}
		template = strings.ReplaceAll(template, "{regras}", rules)
	}

	if !strings.Contains(template, "{numero}") {
		return h.whatsappService.SendMessage(chatID, template)
	}

	mentionTexts := make([]string, 0, len(participants))
	mentionJIDs := make([]string, 0, len(participants))

	for _, participant := range participants {
		if participant.User == "" {
			continue
		}
		mentionTexts = append(mentionTexts, "@"+participant.User)
		mentionJIDs = append(mentionJIDs, participant.String())
	}

	if len(mentionTexts) == 0 {
		messageText := strings.ReplaceAll(template, "{numero}", "")
		return h.whatsappService.SendMessage(chatID, strings.TrimSpace(messageText))
	}

	messageText := strings.ReplaceAll(template, "{numero}", strings.Join(mentionTexts, ", "))

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	return h.whatsappService.SendMentionMessage(ctx, chatID, messageText, mentionJIDs)
}

// pickRandom returns a random non-empty element from the pool.
// Returns an empty string if the pool is nil or empty.
func pickRandom(pool []string) string {
	if len(pool) == 0 {
		return ""
	}
	return pool[rand.Intn(len(pool))]
}

