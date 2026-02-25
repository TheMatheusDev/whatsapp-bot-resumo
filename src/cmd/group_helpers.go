package cmd

import (
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"

	wstypes "whatsapp-summarizer/src/types"
)

// getGroupName gets the group name, using cache when possible
func (h *Handler) getGroupName(client *whatsmeow.Client, chat types.JID) string {
	if chat.Server != types.GroupServer {
		return "Direct Chat"
	}

	chatID := chat.User

	// Try to get from cache first
	if name, exists := h.cache.GetGroupName(chatID); exists {
		return name
	}

	// Cache miss, fetch from WhatsApp API
	groupInfo, err := client.GetGroupInfo(chat)
	groupName := chatID // fallback to chat ID
	if err == nil && groupInfo != nil {
		groupName = groupInfo.Name
	}

	// Update cache
	h.cache.SetGroupName(chatID, groupName)

	return groupName
}

// saveMessage saves a message to the database
func (h *Handler) saveMessage(message wstypes.Message, chat types.JID, client *whatsmeow.Client) error {
	// Only save group messages
	if chat.Server != types.GroupServer {
		return nil
	}

	groupName := h.getGroupName(client, chat)
	return h.dbService.SaveGroupMessage(message, groupName)
}
