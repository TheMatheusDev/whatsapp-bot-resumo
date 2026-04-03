package cmd

import (
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
	return info.IsGroup && h.isWhitelistedGroup(info.Chat)
}

// isCommand checks if a message is a bot command
func (h *Handler) isCommand(content string) bool {
	return strings.HasPrefix(content, "--") || strings.HasPrefix(content, "-") || strings.HasPrefix(content, "!") || strings.HasPrefix(content, "/")
}
