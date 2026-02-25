package cmd

import (
	"strings"

	"go.mau.fi/whatsmeow/types"
)

// isAuthorized checks if the user is authorized to use bot commands
func (h *Handler) isAuthorized(info types.MessageInfo) bool {
	// Direct messages (DMs) are NOT authorized
	if !info.IsGroup {
		return false
	}

	// O(1) whitelist lookup
	return h.whitelistMap[info.Chat.User]
}

// isCommand checks if a message is a bot command
func (h *Handler) isCommand(content string) bool {
	return strings.HasPrefix(content, "--") || strings.HasPrefix(content, "-") || strings.HasPrefix(content, "!") || strings.HasPrefix(content, "/")
}
