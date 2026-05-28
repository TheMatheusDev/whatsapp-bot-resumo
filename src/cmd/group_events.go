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
}

// resolveWelcomePool returns the welcome message pool for a group.
// Per-group messages from the DB take priority; falls back to the global config.
func (h *Handler) resolveWelcomePool(chatID string) []string {
	if s := h.getGroupSettings(chatID); s != nil && len(s.WelcomeMessages) > 0 {
		return s.WelcomeMessages
	}
	return h.config.Bot.WelcomeMessages
}

// resolveFarewellPool returns the farewell message pool for a group.
// Per-group messages from the DB take priority; falls back to the global config.
func (h *Handler) resolveFarewellPool(chatID string) []string {
	if s := h.getGroupSettings(chatID); s != nil && len(s.FarewellMessages) > 0 {
		return s.FarewellMessages
	}
	return h.config.Bot.FarewellMessages
}

// sendParticipantStatusMessage sends one consolidated message for all participants.
// Both {numero} and @numero placeholders are treated as equivalent and replaced
// by participant mentions when present in the template.
func (h *Handler) sendParticipantStatusMessage(chatID types.JID, participants []types.JID, template string) error {
	template = strings.TrimSpace(template)
	if template == "" {
		return nil
	}

	// Normalise both placeholder styles to @numero for internal processing.
	template = strings.ReplaceAll(template, "{numero}", "@numero")

	if !strings.Contains(template, "@numero") {
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
		messageText := strings.ReplaceAll(template, "@numero", "")
		return h.whatsappService.SendMessage(chatID, strings.TrimSpace(messageText))
	}

	messageText := strings.ReplaceAll(template, "@numero", strings.Join(mentionTexts, ", "))

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

