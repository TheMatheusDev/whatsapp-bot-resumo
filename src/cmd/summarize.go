package cmd

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	waE2E "go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"

	wstypes "whatsapp-summarizer/src/types"
)

// handleSummarizeCommand handles the summarize command
func (h *Handler) handleSummarizeCommand(args []string, info types.MessageInfo, client *whatsmeow.Client) {
	if len(args) == 0 {
		h.sendErrorMessageReply(client, info, "Número de mensagens não especificado")
		return
	}

	// Parse message count
	count, err := strconv.Atoi(args[0])
	if err != nil || count <= 0 {
		h.sendErrorMessageReply(client, info, "Número de mensagens inválido")
		return
	}

	// Validate count limits (same as legacy code)
	if count <= 3 {
		h.sendErrorMessageReply(client, info, "ℹ️ Se acha o engraçadinho, hein?")
		return
	}

	if count <= 10 {
		h.sendErrorMessageReply(client, info, "ℹ️ Não faz sentido resumir tão poucas mensagens...")
		return
	}

	if count > 9000 {
		h.sendErrorMessageReply(client, info, "ℹ️ Você só pode ta de brincadeira, né?! Escolha um número menor!")
		return
	}

	// Parse options
	opts := wstypes.SummarizeOptions{
		Count: count,
		Style: "short", // default
		Clt:   false,   // default
	}

	for _, arg := range args[1:] {
		switch strings.ToLower(arg) {
		case "--curto", "-c":
			opts.Style = "short"
		case "--medio", "-m":
			opts.Style = "medium"
		case "--longo", "-l":
			opts.Style = "long"
		case "--clt", "-clt":
			opts.Clt = true
		}
	}

	// Start summarization in goroutine
	go h.performSummarization(opts, info, client)
}

// performSummarization performs the actual summarization
func (h *Handler) performSummarization(opts wstypes.SummarizeOptions, info types.MessageInfo, client *whatsmeow.Client) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*3)
	defer cancel()

	// Send initial "reading messages..." message as reply
	loadingMessage := fmt.Sprintf("ℹ️ Lendo %d mensagens...", opts.Count)
	msgResp, err := client.SendMessage(context.Background(), info.Chat, &waE2E.Message{
		ExtendedTextMessage: &waE2E.ExtendedTextMessage{
			Text: proto.String(loadingMessage),
			ContextInfo: &waE2E.ContextInfo{
				StanzaID:    proto.String(info.ID),
				Participant: proto.String(info.Sender.String()),
			},
		},
	})
	if err != nil {
		h.logger.Error("Failed to send loading message", "error", err)
		h.sendErrorMessage(client, info.Chat, "Erro ao enviar mensagem")
		return
	}

	// Notify owner about the request (similar to legacy code)
	if h.config.WhatsApp.OwnerJID != "" && h.config.WhatsApp.OwnerJID != info.Sender.User {
		groupName := h.getGroupName(client, info.Chat)
		senderName := info.PushName
		if senderName == "" {
			senderName = info.Sender.User
		}

		var ownerMessage string
		if opts.Question != "" {
			ownerMessage = fmt.Sprintf("ℹ️ %s requisitou um %s resumo de %d mensagens em %s\n❓ Pergunta: %s",
				senderName, opts.Style, opts.Count, groupName, opts.Question)
		} else {
			ownerMessage = fmt.Sprintf("ℹ️ %s requisitou um %s resumo de %d mensagens em %s",
				senderName, opts.Style, opts.Count, groupName)
		}

		ownerJID, err := types.ParseJID(h.config.WhatsApp.OwnerJID)
		if err == nil {
			go func() {
				client.SendMessage(context.Background(), ownerJID, &waE2E.Message{
					Conversation: proto.String(ownerMessage),
				})
			}()
		}
	}

	// Get messages from database (only groups are supported)
	var messages []wstypes.Message

	if !info.IsGroup {
		h.logger.Error("Direct messages are not supported for summarization")
		editMsg := client.BuildEdit(info.Chat, msgResp.ID, &waE2E.Message{
			Conversation: proto.String("❌ Resumos não são suportados em mensagens diretas"),
		})
		client.SendMessage(context.Background(), info.Chat, editMsg)
		return
	}

	messages, err = h.dbService.GetGroupMessages(info.Chat.User, opts.Count)

	if err != nil {
		h.logger.Error("Failed to get messages", "error", err)
		// Edit the loading message to show error
		editMsg := client.BuildEdit(info.Chat, msgResp.ID, &waE2E.Message{
			Conversation: proto.String("❌ Erro ao buscar mensagens"),
		})
		client.SendMessage(context.Background(), info.Chat, editMsg)
		return
	}

	if len(messages) == 0 {
		// Edit the loading message to show no messages found
		editMsg := client.BuildEdit(info.Chat, msgResp.ID, &waE2E.Message{
			Conversation: proto.String("ℹ️ Nenhuma mensagem encontrada"),
		})
		client.SendMessage(context.Background(), info.Chat, editMsg)
		return
	}

	// Generate summary
	summary, err := h.aiService.SummarizeMessages(ctx, messages, opts)
	if err != nil {
		h.logger.Error("Failed to generate summary", "error", err)

		// Try with backup model
		h.logger.Info("Retrying with backup model")

		// Edit the loading message to show we're trying backup
		editMsg := client.BuildEdit(info.Chat, msgResp.ID, &waE2E.Message{
			Conversation: proto.String("ℹ️ Tentando resumir com modelo de backup..."),
		})
		client.SendMessage(context.Background(), info.Chat, editMsg)

		// Try again with backup model
		summary, err = h.aiService.SummarizeMessagesWithBackup(ctx, messages, opts)
		if err != nil {
			h.logger.Error("Failed to generate summary with backup model", "error", err)
			// Edit the loading message to show error
			errorMsg := ""
			if ctx.Err() == context.DeadlineExceeded {
				errorMsg = "⏱️ Timeout ao gerar resumo - tente com menos mensagens"
			} else {
				errorMsg = fmt.Sprintf("❌ Erro ao gerar resumo\n\n%s", err.Error())
			}
			editMsg := client.BuildEdit(info.Chat, msgResp.ID, &waE2E.Message{
				Conversation: proto.String(errorMsg),
			})
			client.SendMessage(context.Background(), info.Chat, editMsg)
			return
		}
	}

	// Edit the loading message with the final summary
	finalSummary := fmt.Sprintf("ℹ️ Resumo por IA:\n%s", summary)
	editMsg := client.BuildEdit(info.Chat, msgResp.ID, &waE2E.Message{
		Conversation: proto.String(finalSummary),
	})

	_, err = client.SendMessage(context.Background(), info.Chat, editMsg)
	if err != nil {
		h.logger.Error("Failed to edit message with summary", "error", err)
		// Fallback: send summary as new message
		h.sendMessageReply(client, info, finalSummary)
	}

	// Save summary as a message
	summaryMsg := wstypes.Message{
		ChatID:      info.Chat.User,
		Sender:      "ProfetaBOT [VOCÊ]",
		Content:     finalSummary,
		MessageType: "Summary",
		Timestamp:   time.Now().In(h.timezone),
	}
	h.saveMessage(summaryMsg, info.Chat, client)
}
