package cmd

import (
	"context"
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

	leaveParticipants := evt.Leave
	joinParticipants := filterParticipantsNotIn(evt.Join, leaveParticipants)

	if len(leaveParticipants) > 0 && strings.TrimSpace(h.config.Bot.FarewellMessage) != "" {
		if err := h.sendParticipantStatusMessage(evt.JID, leaveParticipants, h.config.Bot.FarewellMessage); err != nil {
			h.logger.Error("Failed to send farewell message", "error", err, "chat_id", evt.JID.String())
		}
	}

	if len(joinParticipants) > 0 && strings.TrimSpace(h.config.Bot.WelcomeMessage) != "" {
		if err := h.sendParticipantStatusMessage(evt.JID, joinParticipants, h.config.Bot.WelcomeMessage); err != nil {
			h.logger.Error("Failed to send welcome message", "error", err, "chat_id", evt.JID.String())
		}
	}
}

// filterParticipantsNotIn removes participants from source that also appear in excluded.
// This avoids sending conflicting welcome/farewell messages for the same user when
// WhatsApp emits add/remove entries in the same metadata update.
func filterParticipantsNotIn(source, excluded []types.JID) []types.JID {
	if len(source) == 0 || len(excluded) == 0 {
		return source
	}

	excludedJIDs := make(map[string]struct{}, len(excluded))
	excludedUsers := make(map[string]struct{}, len(excluded))
	for _, participant := range excluded {
		excludedJIDs[participant.String()] = struct{}{}
		if participant.User != "" {
			excludedUsers[participant.User] = struct{}{}
		}
	}

	filtered := make([]types.JID, 0, len(source))
	for _, participant := range source {
		if _, found := excludedJIDs[participant.String()]; found {
			continue
		}
		if participant.User != "" {
			if _, found := excludedUsers[participant.User]; found {
				continue
			}
		}
		filtered = append(filtered, participant)
	}

	return filtered
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
