package handlers

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	waE2E "go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	"whatsapp-summarizer/internal/config"
	wstypes "whatsapp-summarizer/pkg/types"
)

// Handler manages WhatsApp message events
type Handler struct {
	config       *config.Config
	aiService    wstypes.AIService
	dbService    wstypes.DatabaseService
	cache        wstypes.CacheService
	logger       wstypes.Logger
	botStartTime time.Time
	timezone     *time.Location
}

// NewHandler creates a new message handler
func NewHandler(
	config *config.Config,
	aiService wstypes.AIService,
	dbService wstypes.DatabaseService,
	cache wstypes.CacheService,
	logger wstypes.Logger,
	botStartTime time.Time,
) (*Handler, error) {
	// Parse timezone
	loc, err := time.LoadLocation("GMT-3")
	if err != nil {
		loc = time.FixedZone("GMT-3", -3*60*60)
	}

	return &Handler{
		config:       config,
		aiService:    aiService,
		dbService:    dbService,
		cache:        cache,
		logger:       logger,
		botStartTime: botStartTime,
		timezone:     loc,
	}, nil
}

// HandleEvent handles WhatsApp events
func (h *Handler) HandleEvent(evt interface{}, client *whatsmeow.Client) {
	switch v := evt.(type) {
	case *events.Message:
		h.handleMessage(v, client)
	case *events.Receipt:
		// Handle message receipts if needed
	case *events.Presence:
		// Handle presence updates if needed
	case *events.HistorySync:
		h.logger.Debug("History sync received", "type", v.Data.GetSyncType().String())
	default:
		h.logger.Debug("Unhandled event", "type", fmt.Sprintf("%T", v))
	}
}

// handleMessage processes incoming messages
func (h *Handler) handleMessage(evt *events.Message, client *whatsmeow.Client) {
	// Skip messages sent before bot started
	if evt.Info.Timestamp.Before(h.botStartTime) {
		return
	}

	msg := evt.Message
	if msg == nil {
		return
	}

	// Extract message content
	content := h.extractMessageContent(msg)
	if content == "" {
		return
	}

	// Create message object
	message := wstypes.Message{
		ChatID:      evt.Info.Chat.User,
		Sender:      h.getSenderName(evt.Info),
		Content:     content,
		MessageType: h.getMessageType(msg),
		Timestamp:   evt.Info.Timestamp.In(h.timezone),
	}

	// Save to database
	if err := h.saveMessage(message, evt.Info.Chat, client); err != nil {
		h.logger.Error("Failed to save message", "error", err)
	}

	// Check for @everyone mentions (before command processing)
	if h.containsEveryoneMention(content) && evt.Info.IsGroup {
		h.handleEveryoneCommand(evt.Info.Chat, client)
	}

	// Process commands if from authorized users
	if h.isAuthorized(evt.Info) && h.isCommand(content) {
		h.handleCommand(content, evt.Info, client)
	}
}

// extractMessageContent extracts text content from a message
func (h *Handler) extractMessageContent(msg *waE2E.Message) string {
	if msg.GetConversation() != "" {
		return msg.GetConversation()
	}

	if extMsg := msg.GetExtendedTextMessage(); extMsg != nil {
		return extMsg.GetText()
	}

	// Handle other message types as needed
	return ""
}

// getSenderName gets the sender name for a message
func (h *Handler) getSenderName(info types.MessageInfo) string {
	if info.IsFromMe {
		return "ProfetaBOT [VOCÊ]"
	}

	// For group messages, use the sender's push name or phone number
	if !info.IsGroup {
		if info.PushName != "" {
			return info.PushName
		}
		return info.Sender.User
	}

	// For group messages
	if info.PushName != "" {
		return info.PushName
	}
	return info.Sender.User
}

// getMessageType determines the message type
func (h *Handler) getMessageType(msg *waE2E.Message) string {
	switch {
	case msg.GetConversation() != "":
		return "Conversation"
	case msg.GetExtendedTextMessage() != nil:
		return "ExtendedText"
	case msg.GetImageMessage() != nil:
		return "Image"
	case msg.GetVideoMessage() != nil:
		return "Video"
	case msg.GetAudioMessage() != nil:
		return "Audio"
	case msg.GetDocumentMessage() != nil:
		return "Document"
	default:
		return "Unknown"
	}
}

// getGroupName gets the group name, using cache when possible
func (h *Handler) getGroupName(client *whatsmeow.Client, chat types.JID) string {
	if chat.Server != types.GroupServer {
		return "Direct Chat"
	}

	chatID := chat.User

	// Try to get from cache first
	if name, exists := h.cache.GetGroupName(chatID); exists {
		return name
	}

	// Cache miss, fetch from WhatsApp API
	groupInfo, err := client.GetGroupInfo(chat)
	groupName := chatID // fallback to chat ID
	if err == nil && groupInfo != nil {
		groupName = groupInfo.Name
	}

	// Update cache
	h.cache.SetGroupName(chatID, groupName)

	return groupName
}

// saveMessage saves a message to the database
func (h *Handler) saveMessage(message wstypes.Message, chat types.JID, client *whatsmeow.Client) error {
	groupName := h.getGroupName(client, chat)

	if chat.Server == types.GroupServer {
		return h.dbService.SaveGroupMessage(message, groupName)
	}
	return h.dbService.SaveDirectMessage(message, groupName)
}

// isAuthorized checks if the user is authorized to use bot commands
func (h *Handler) isAuthorized(info types.MessageInfo) bool {
	senderJID := info.Sender.User

	// Owner is always authorized
	if senderJID == h.config.WhatsApp.OwnerJID {
		return true
	}

	// Check user whitelist
	for _, allowedUser := range h.config.WhatsApp.UserWhitelist {
		if senderJID == allowedUser {
			return true
		}
	}

	// Check if in whitelisted group
	if info.IsGroup {
		chatID := info.Chat.User
		for _, allowedGroup := range h.config.WhatsApp.GroupWhitelist {
			if chatID == allowedGroup {
				return true
			}
		}
	}

	return false
}

// isCommand checks if a message is a bot command
func (h *Handler) isCommand(content string) bool {
	return strings.HasPrefix(content, "--") || strings.HasPrefix(content, "-")
}

// handleCommand processes bot commands
func (h *Handler) handleCommand(content string, info types.MessageInfo, client *whatsmeow.Client) {
	parts := strings.Fields(content)
	if len(parts) == 0 {
		return
	}

	command := strings.ToLower(parts[0])

	switch command {
	case "--resuma", "-r":
		h.handleSummarizeCommand(parts[1:], info, client)
	case "--info", "-i":
		h.handleInfoCommand(info, client)
	case "--help", "-h":
		h.handleHelpCommand(info, client)
	case "--version", "-v":
		h.handleVersionCommand(info, client)
	default:
		h.logger.Debug("Unknown command", "command", command)
	}
}

// handleSummarizeCommand handles the summarize command
func (h *Handler) handleSummarizeCommand(args []string, info types.MessageInfo, client *whatsmeow.Client) {
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
		case "--clt":
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

	// Send initial "reading messages..." message
	loadingMessage := fmt.Sprintf("ℹ️ Lendo %d mensagens...", opts.Count)
	msgResp, err := client.SendMessage(context.Background(), info.Chat, &waE2E.Message{
		Conversation: proto.String(loadingMessage),
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
		ownerMessage := fmt.Sprintf("ℹ️ %s requisitou um %s resumo de %d mensagens em %s",
			senderName, opts.Style, opts.Count, groupName)

		ownerJID, err := types.ParseJID(h.config.WhatsApp.OwnerJID)
		if err == nil {
			go func() {
				client.SendMessage(context.Background(), ownerJID, &waE2E.Message{
					Conversation: proto.String(ownerMessage),
				})
			}()
		}
	}

	// Get messages from database
	var messages []wstypes.Message

	if info.IsGroup {
		messages, err = h.dbService.GetGroupMessages(info.Chat.User, opts.Count)
	} else {
		messages, err = h.dbService.GetDirectMessages(info.Chat.User, opts.Count)
	}

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
		// Edit the loading message to show error
		errorMsg := "❌ Erro ao gerar resumo"
		if ctx.Err() == context.DeadlineExceeded {
			errorMsg = "⏱️ Timeout ao gerar resumo - tente com menos mensagens"
		}
		editMsg := client.BuildEdit(info.Chat, msgResp.ID, &waE2E.Message{
			Conversation: proto.String(errorMsg),
		})
		client.SendMessage(context.Background(), info.Chat, editMsg)
		return
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
		h.sendMessage(client, info.Chat, finalSummary)
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

// Helper methods for sending messages
func (h *Handler) handleInfoCommand(info types.MessageInfo, client *whatsmeow.Client) {
	infoText := `Bot mantido por *Matheus Araújo*!

Resume mensagens via Google Gemini 2.5 Flash

*Comandos:*
- --resuma <número> → Resume mensagens
- -r <número> → Forma abreviada
- --info → Informações do bot
- --version → Versão do bot

*Opções de Resumo:*
- --curto ou -c → Resumo curto
- --medio ou -m → Resumo médio (padrão)
- --longo ou -l → Resumo longo
- --clt → Personalidade CLT

*Exemplo:*
- --resuma 50 --longo → Resumo longo 50 mensagens
- -r 20 -c → Resumo curto 20 mensagens
- -r 5000 --clt → Resumo longo 5000 mensagens com personalidade CLT
`

	h.sendMessage(client, info.Chat, infoText)
}

func (h *Handler) handleHelpCommand(info types.MessageInfo, client *whatsmeow.Client) {
	h.handleInfoCommand(info, client) // Same as info for now
}

func (h *Handler) handleVersionCommand(info types.MessageInfo, client *whatsmeow.Client) {
	h.sendMessage(client, info.Chat, "🤖 WhatsApp Summarizer Bot v2.0 - Refactored Edition")
}

func (h *Handler) sendMessage(client *whatsmeow.Client, chat types.JID, message string) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*30)
	defer cancel()

	_, err := client.SendMessage(ctx, chat, &waE2E.Message{
		Conversation: proto.String(message),
	})
	if err != nil {
		h.logger.Error("Failed to send message", "error", err)
	}
}

func (h *Handler) sendErrorMessage(client *whatsmeow.Client, chat types.JID, message string) {
	errorMsg := fmt.Sprintf("❌ %s", message)
	h.sendMessage(client, chat, errorMsg)
}

// containsEveryoneMention checks if message contains @everyone mentions
func (h *Handler) containsEveryoneMention(content string) bool {
	return strings.Contains(content, "@everyone") ||
		strings.Contains(content, "@todos") ||
		strings.Contains(content, "@here")
}

// handleEveryoneCommand mentions all group members when @everyone is detected
func (h *Handler) handleEveryoneCommand(chat types.JID, client *whatsmeow.Client) {
	go func() {
		// Check if it's a group chat
		if chat.Server != types.GroupServer {
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), time.Second*30)
		defer cancel()

		// Get group info
		groupInfo, err := client.GetGroupInfo(chat)
		if err != nil {
			h.logger.Error("Failed to get group info for @everyone", "error", err, "chat_id", chat.String())
			return
		}

		members := groupInfo.Participants
		if len(members) == 0 {
			h.logger.Warn("No members found in group", "chat_id", chat.String())
			return
		}

		// Create mentions string with all group members
		var mentionTexts []string
		var mentionJIDs []string

		for _, member := range members {
			// Skip if it's the bot itself
			if member.JID.User == client.Store.LID.ToNonAD().User {
				continue
			}

			mentionTexts = append(mentionTexts, "@"+member.JID.User)
			mentionJIDs = append(mentionJIDs, member.JID.String())
		}

		if len(mentionJIDs) == 0 {
			h.logger.Warn("No members to mention", "chat_id", chat.String())
			return
		}

		// Create the message with mentions
		messageText := "ℹ️ @everyone: \n" + strings.Join(mentionTexts, " ")

		// Send message with mentions
		_, err = client.SendMessage(ctx, chat, &waE2E.Message{
			ExtendedTextMessage: &waE2E.ExtendedTextMessage{
				Text: proto.String(messageText),
				ContextInfo: &waE2E.ContextInfo{
					MentionedJID: mentionJIDs,
				},
			},
		})

		if err != nil {
			h.logger.Error("Failed to send @everyone message", "error", err, "chat_id", chat.String())
			return
		}

		// Log the message to database
		everyoneMsg := wstypes.Message{
			ChatID:      chat.User,
			Sender:      "ProfetaBOT [VOCÊ]",
			Content:     "[MENTIONED EVERYONE]",
			MessageType: "EveryoneMention",
			Timestamp:   time.Now().In(h.timezone),
		}

		if err := h.saveMessage(everyoneMsg, chat, client); err != nil {
			h.logger.Error("Failed to save @everyone message to database", "error", err)
		}

		h.logger.Info("@everyone command executed successfully",
			"chat_id", chat.String(),
			"mentioned_count", len(mentionJIDs))
	}()
}
