package cmd

import (
	"context"
	"time"

	"go.mau.fi/whatsmeow/types"
)

const reactionTimeout = 10 * time.Second

// reactToCommand sends an emoji reaction to the user's original command message.
// If the reaction fails (e.g. old WhatsApp client), the error is only logged —
// reactions are best-effort and never block the main flow.
func (h *Handler) reactToCommand(msgTrigger types.MessageInfo, emoji string) {
	ctx, cancel := context.WithTimeout(context.Background(), reactionTimeout)
	defer cancel()
	if err := h.whatsappService.ReactToMessage(ctx, msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID, emoji); err != nil {
		h.logger.Warn("reactToCommand: failed to send reaction",
			"emoji", emoji, "chat", msgTrigger.Chat.User, "error", err)
	}
}
