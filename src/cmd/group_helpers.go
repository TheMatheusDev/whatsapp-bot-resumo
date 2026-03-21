package cmd

import (
	"context"

	"go.mau.fi/whatsmeow/types"

	wstypes "whatsapp-summarizer/src/types"
)

// getGroupName gets the group name, using cache when possible
func (h *Handler) getGroupName(chat types.JID) string {
	if chat.Server != types.GroupServer {
		return "Direct Chat"
	}

	chatID := chat.User

	// Try to get from cache first
	if name, exists := h.cache.GetGroupName(chatID); exists {
		return name
	}

	// Cache miss, fetch from WhatsApp API
	groupInfo, err := h.whatsappService.GetGroupInfo(context.Background(), chat)
	groupName := chatID // fallback to chat ID
	if err == nil && groupInfo != nil {
		groupName = groupInfo.Name
	}

	// Update cache
	h.cache.SetGroupName(chatID, groupName)

	return groupName
}

// saveMessage saves a message to the database and returns the inserted row ID
func (h *Handler) saveMessage(message wstypes.Message, chat types.JID) (int64, error) {
	// Only save group messages
	if chat.Server != types.GroupServer {
		return 0, nil
	}

	groupName := h.getGroupName(chat)
	return h.dbService.SaveGroupMessageReturningID(message, groupName)
}
