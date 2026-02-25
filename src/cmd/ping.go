package cmd

import (
	"fmt"
	"time"

	"go.mau.fi/whatsmeow/types"
)

func (h *Handler) handlePingCommand(msgTrigger types.MessageInfo) {
	now := time.Now().In(h.timezone)
	response := fmt.Sprintf("Pong! 🏓 (%02d:%02d %02d/%02d)", now.Hour(), now.Minute(), now.Day(), int(now.Month()))
	h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.ID, response)
}
