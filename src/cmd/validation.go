package cmd

import (
	"strconv"

	"go.mau.fi/whatsmeow/types"
)

// CountValidationMessages holds the personalized error messages for count validation
type CountValidationMessages struct {
	TooFewJoke string // message for count <= 3
	TooFew     string // message for count <= 10
	TooMany    string // message for count > 9000
}

// DefaultCountMessages provides the default validation messages
var DefaultCountMessages = CountValidationMessages{
	TooFewJoke: "❌ Se acha o engraçadinho, hein?",
	TooFew:     "❌ Não faz sentido resumir tão poucas mensagens...",
	TooMany:    "❌ Você só pode ta de brincadeira, né?! Escolha um número menor!",
}

// parseAndValidateCount parses the count from args[0] and validates it.
// Returns the parsed count and true if valid, or 0 and false if invalid (error message already sent).
func (h *Handler) parseAndValidateCount(msgTrigger types.MessageInfo, countStr string, msgs CountValidationMessages) (int, bool) {
	count, err := strconv.Atoi(countStr)
	if err != nil || count <= 0 {
		h.reactToCommand(msgTrigger, "❌")
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID, "❌ Número de mensagens inválido")
		return 0, false
	}

	if count <= 3 {
		h.reactToCommand(msgTrigger, "❌")
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID, msgs.TooFewJoke)
		return 0, false
	}

	if count <= 10 {
		h.reactToCommand(msgTrigger, "❌")
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID, msgs.TooFew)
		return 0, false
	}

	if count > 9000 {
		h.reactToCommand(msgTrigger, "❌")
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID, msgs.TooMany)
		return 0, false
	}

	return count, true
}
