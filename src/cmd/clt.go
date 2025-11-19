package cmd

import (
	"strconv"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"

	wstypes "whatsapp-summarizer/pkg/types"
)

// handleSummarizeCltCommand handles the -clt command (shortcut for -r with --clt flag)
func (h *Handler) handleSummarizeCltCommand(args []string, info types.MessageInfo, client *whatsmeow.Client) {
	if len(args) == 0 {
		h.sendErrorMessage(client, info.Chat, "Número de mensagens não especificado")
		return
	}

	// Parse message count
	count, err := strconv.Atoi(args[0])
	if err != nil || count <= 0 {
		h.sendErrorMessage(client, info.Chat, "Número de mensagens inválido")
		return
	}

	// Validate count limits (same as legacy code)
	if count <= 3 {
		h.sendErrorMessage(client, info.Chat, "ℹ️ Sem tempo para brincadeiras...")
		return
	}

	if count <= 10 {
		h.sendErrorMessage(client, info.Chat, "ℹ️ 10 msgs? Sério? Resuma você mesmo...")
		return
	}

	if count > 9000 {
		h.sendErrorMessage(client, info.Chat, "ℹ️ Tá achando que eu sou seu escravo? Escolha um número menor!	")
		return
	}

	// Parse options - CLT is always enabled for this command
	opts := wstypes.SummarizeOptions{
		Count: count,
		Style: "short", // default
		Clt:   true,    // always enabled for -clt command
	}

	// Start summarization in goroutine
	go h.performSummarization(opts, info, client)
}
