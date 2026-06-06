package cmd

import (
	"fmt"
	"time"

	"go.mau.fi/whatsmeow/types"
)

func (h *Handler) handlePingCommand(msgTrigger types.MessageInfo) {
	now := time.Now().In(h.timezone)
	uptime := time.Since(h.botStartTime)
	hours := int(uptime.Hours())
	minutes := int(uptime.Minutes()) % 60
	uptimeStr := fmt.Sprintf("%dh %02dm", hours, minutes)
	response := fmt.Sprintf("Pong! 🏓 (%02d:%02d %02d/%02d) | ⏱️ uptime: %s",
		now.Hour(), now.Minute(), now.Day(), int(now.Month()), uptimeStr)
	h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID, response)
}
