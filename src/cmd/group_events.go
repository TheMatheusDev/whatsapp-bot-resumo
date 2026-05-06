package cmd

import (
	"context"
	"math/rand"
	"strings"
	"time"

	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

// handleGroupInfoEvent handles participant join/leave events for whitelisted groups.
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

	if !h.whitelistMap[evt.JID.User] {
		return
	}

	if len(evt.Join) > 0 {
		if msg := pickRandom(h.config.Bot.WelcomeMessages); msg != "" {
			if err := h.sendParticipantStatusMessage(evt.JID, evt.Join, msg); err != nil {
				h.logger.Error("Failed to send welcome message", "error", err, "chat_id", evt.JID.String())
			}
		}
	}

	if len(evt.Leave) > 0 {
		if msg := pickRandom(h.config.Bot.FarewellMessages); msg != "" {
			if err := h.sendParticipantStatusMessage(evt.JID, evt.Leave, msg); err != nil {
				h.logger.Error("Failed to send farewell message", "error", err, "chat_id", evt.JID.String())
			}
		}
	}
}

// sendParticipantStatusMessage sends one consolidated message for all participants.
// If the template contains @numero, it is replaced by all participant numbers and they are mentioned.
func (h *Handler) sendParticipantStatusMessage(chatID types.JID, participants []types.JID, template string) error {
	template = strings.TrimSpace(template)
	if template == "" {
		return nil
	}

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
