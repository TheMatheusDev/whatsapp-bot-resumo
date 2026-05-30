package cmd

import (
	"context"
	"time"

	"go.mau.fi/whatsmeow/types"

	wstypes "whatsapp-summarizer/src/types"
)

// getGroupName gets the group name, using the shared groupInfoCache when possible
func (h *Handler) getGroupName(chat types.JID) string {
	if chat.Server != types.GroupServer {
		return "Direct Chat"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if info := h.cachedGetGroupInfo(ctx, chat); info != nil {
		return info.Name
	}
	return chat.User
}

// saveMessage saves a message to the database and returns the inserted row ID.
// All group messages are persisted regardless of whitelist status.
func (h *Handler) saveMessage(message wstypes.Message, chat types.JID) (int64, error) {
	if chat.Server != types.GroupServer {
		return 0, nil
	}
	groupName := h.getGroupName(chat)
	return h.dbService.SaveGroupMessageReturningID(message, groupName)
}
