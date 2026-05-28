package cmd

import (
	"context"
	"strings"

	"go.mau.fi/whatsmeow/types"
)

// isWhitelistedGroup checks if a group JID is whitelisted for bot operations.
func (h *Handler) isWhitelistedGroup(chat types.JID) bool {
	if chat.Server != types.GroupServer {
		return false
	}

	return h.whitelistMap[chat.User]
}

// isAuthorized checks if the user is authorized to use bot commands
func (h *Handler) isAuthorized(info types.MessageInfo) bool {
	return info.IsGroup
}

// isGroupAdmin checks if senderJIDUser is a native WhatsApp admin or superadmin
// of the given group. Calls GetGroupInfo from the WhatsApp API; returns false on
// any error (fail-safe — deny rather than grant on uncertainty).
func (h *Handler) isGroupAdmin(ctx context.Context, chat types.JID, senderJIDUser string) bool {
	groupInfo, err := h.whatsappService.GetGroupInfo(ctx, chat)
	if err != nil {
		h.logger.Warn("isGroupAdmin: could not fetch group info",
			"error", err, "chat_id", chat.String())
		return false
	}
	for _, p := range groupInfo.Participants {
		if p.JID.User == senderJIDUser && (p.IsAdmin || p.IsSuperAdmin) {
			return true
		}
	}
	return false
}

// isCommand checks if a message is a bot command
func (h *Handler) isCommand(content string) bool {
	return strings.HasPrefix(content, "--") || strings.HasPrefix(content, "-") || strings.HasPrefix(content, "!") || strings.HasPrefix(content, "/")
}
