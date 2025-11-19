package cmd

import (
	"strconv"
	"strings"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"

	wstypes "whatsapp-summarizer/pkg/types"
)

// handleAskQuestionCommand handles the --pergunte/-p command
func (h *Handler) handleAskQuestionCommand(args []string, info types.MessageInfo, client *whatsmeow.Client) {
	if len(args) < 2 {
		h.sendErrorMessage(client, info.Chat, "Uso: -p <número> [opções] <pergunta>\n\nOpções: --clt, --curto, --medio, --longo\n\nExemplos:\n• -p 50 Quem foi o usuário mais ativo?\n• -p 100 --clt Qual foi o assunto principal?\n• -p 200 --longo --clt Houve conflitos?")
		return
	}

	// Parse message count
	count, err := strconv.Atoi(args[0])
	if err != nil || count <= 0 {
		h.sendErrorMessage(client, info.Chat, "Número de mensagens inválido")
		return
	}

	// Validate count limits
	if count <= 3 {
		h.sendErrorMessage(client, info.Chat, "ℹ️ Se acha o engraçadinho, hein?")
		return
	}

	if count <= 10 {
		h.sendErrorMessage(client, info.Chat, "ℹ️ Não faz sentido resumir tão poucas mensagens...")
		return
	}

	if count > 9000 {
		h.sendErrorMessage(client, info.Chat, "ℹ️ Você só pode ta de brincadeira, né?! Escolha um número menor!")
		return
	}

	// Parse options and extract question
	// Check for flags in the remaining args
	var questionParts []string
	useClt := false
	style := "short" // default to medium for questions since they need more context

	for _, arg := range args[1:] {
		argLower := strings.ToLower(arg)
		switch argLower {
		case "--clt", "-clt":
			useClt = true
		case "--curto", "-c":
			style = "short"
		case "--medio", "-m":
			style = "medium"
		case "--longo", "-l":
			style = "long"
		default:
			// Not a flag, it's part of the question
			questionParts = append(questionParts, arg)
		}
	}

	// Join the remaining args as the question
	question := strings.Join(questionParts, " ")

	if question == "" {
		h.sendErrorMessage(client, info.Chat, "ℹ️ Você precisa fazer uma pergunta!")
		return
	}

	// Parse options - start with defaults
	opts := wstypes.SummarizeOptions{
		Count:    count,
		Style:    style,
		Clt:      useClt,
		Question: question,
	}

	// Start summarization in goroutine
	go h.performSummarization(opts, info, client)
}
