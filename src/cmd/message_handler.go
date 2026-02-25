package cmd

import (
	"fmt"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	waE2E "go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	wstypes "whatsapp-summarizer/src/types"
)

// Handler manages WhatsApp message events
type Handler struct {
	config          *wstypes.Config
	aiService       wstypes.AIService
	dbService       wstypes.DatabaseService
	whatsappService wstypes.WhatsAppService
	cache           wstypes.CacheService
	logger          wstypes.Logger
	botStartTime    time.Time
	timezone        *time.Location
	everyoneHandler *EveryoneHandler
	whitelistMap    map[string]bool
}

// NewHandler creates a new message handler
func NewHandler(
	config *wstypes.Config,
	aiService wstypes.AIService,
	dbService wstypes.DatabaseService,
	whatsappService wstypes.WhatsAppService,
	cache wstypes.CacheService,
	logger wstypes.Logger,
	botStartTime time.Time,
) (*Handler, error) {
	// Parse timezone from config
	loc, err := time.LoadLocation(config.Bot.Timezone)
	if err != nil {
		loc = time.FixedZone(config.Bot.Timezone, -3*60*60)
	}

	// Build whitelist map for O(1) lookups
	whitelistMap := make(map[string]bool, len(config.WhatsApp.GroupWhitelist))
	for _, gid := range config.WhatsApp.GroupWhitelist {
		whitelistMap[gid] = true
	}

	return &Handler{
		config:          config,
		aiService:       aiService,
		dbService:       dbService,
		whatsappService: whatsappService,
		cache:           cache,
		logger:          logger,
		botStartTime:    botStartTime,
		timezone:        loc,
		everyoneHandler: NewEveryoneHandler(config, logger, loc),
		whitelistMap:    whitelistMap,
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
	msg := evt.Message
	if msg == nil {
		return
	}

	// Download media if present (images, videos, etc.)
	h.downloadMediaIfPresent(msg, client)

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

	// Save to database (ALWAYS save, even if message is from before bot started)
	if err := h.saveMessage(message, evt.Info.Chat, client); err != nil {
		h.logger.Error("Failed to save message", "error", err)
	}

	// Skip command processing for messages sent before bot started
	if evt.Info.Timestamp.Before(h.botStartTime) {
		h.logger.Debug("Message saved but skipping command processing (sent before bot started)",
			"timestamp", evt.Info.Timestamp,
			"bot_start_time", h.botStartTime)
		return
	}

	// Check for @everyone mentions (only for new messages)
	// Extract only the current message text (without quoted message) for @everyone check
	currentMessageText := h.extractCurrentMessageText(msg)
	if h.everyoneHandler.ContainsEveryoneMention(currentMessageText) && evt.Info.IsGroup {
		// Check if user is authorized to use @everyone
		senderName := h.getSenderName(evt.Info)
		if h.everyoneHandler.IsEveryoneAdmin(senderName) {
			h.everyoneHandler.HandleEveryoneCommand(evt.Info.Chat, client, h.dbService, h.cache, currentMessageText)
		} else {
			h.logger.Info("Unauthorized @everyone attempt", "sender", senderName)
		}
	}

	// Process commands if from authorized users (only for new messages)
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
		baseText := extMsg.GetText()

		// Check if this message is replying to another message
		if contextInfo := extMsg.GetContextInfo(); contextInfo != nil {
			if quotedMsg := contextInfo.GetQuotedMessage(); quotedMsg != nil {
				quotedText := h.extractQuotedMessageText(quotedMsg)
				if baseText != "" && quotedText != "" {
					return baseText + " [respondendo a] '" + quotedText + "'"
				}
				if baseText != "" {
					return baseText
				}
				if quotedText != "" {
					return "[respondendo a] '" + quotedText + "'"
				}
			}
		}

		return baseText
	}

	// Handle other message types as needed
	return ""
}

// extractQuotedMessageText extracts text from a quoted message
func (h *Handler) extractQuotedMessageText(msg *waE2E.Message) string {
	if msg.GetConversation() != "" {
		return msg.GetConversation()
	}

	if extMsg := msg.GetExtendedTextMessage(); extMsg != nil {
		return extMsg.GetText()
	}

	if imgMsg := msg.GetImageMessage(); imgMsg != nil {
		if caption := imgMsg.GetCaption(); caption != "" {
			return "[Image] " + caption
		}
		return "[Image]"
	}

	if videoMsg := msg.GetVideoMessage(); videoMsg != nil {
		if caption := videoMsg.GetCaption(); caption != "" {
			return "[Video] " + caption
		}
		return "[Video]"
	}

	if audioMsg := msg.GetAudioMessage(); audioMsg != nil {
		return "[Audio]"
	}

	if docMsg := msg.GetDocumentMessage(); docMsg != nil {
		if title := docMsg.GetTitle(); title != "" {
			return "[Document] " + title
		}
		return "[Document]"
	}

	if stickerMsg := msg.GetStickerMessage(); stickerMsg != nil {
		return "[Sticker]"
	}

	return "[Unknown message type]"
}

// extractCurrentMessageText extracts only the current message text without quoted message info
// This is used for command processing and mention detection
func (h *Handler) extractCurrentMessageText(msg *waE2E.Message) string {
	if msg.GetConversation() != "" {
		return msg.GetConversation()
	}

	if extMsg := msg.GetExtendedTextMessage(); extMsg != nil {
		return extMsg.GetText()
	}

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
	// Only save group messages
	if chat.Server != types.GroupServer {
		return nil
	}

	groupName := h.getGroupName(client, chat)
	return h.dbService.SaveGroupMessage(message, groupName)
}

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

// handleCommand processes bot commands
func (h *Handler) handleCommand(content string, msgTrigger types.MessageInfo, client *whatsmeow.Client) {
	parts := strings.Fields(content)
	if len(parts) == 0 {
		return
	}

	command := strings.ToLower(parts[0])

	switch command {
	case "--resuma", "-r", "!resumo", "!resuma", "!r", "/resuma", "/resumo", "/r":
		h.handleSummarizeCommand(parts[1:], msgTrigger, client)
	case "-clt", "!clt", "--clt", "/clt":
		h.handleSummarizeCltCommand(parts[1:], msgTrigger, client)
	case "--narrador", "-n", "!narrador", "!n", "/narrador", "/n":
		h.handleSummarizeNarradorCommand(parts[1:], msgTrigger, client)
	case "--farialimer", "-fl", "!farialimer", "!fl", "/farialimer", "/fl":
		h.handleSummarizeFariaLimerCommand(parts[1:], msgTrigger, client)
	case "--noir", "--detetive", "-noir", "-detetive", "!noir", "!detetive", "/noir", "/detetive":
		h.handleSummarizeNoirCommand(parts[1:], msgTrigger, client)
	case "--zoomer", "-z", "!zoomer", "!z", "/zoomer", "/z":
		h.handleSummarizeZoomerCommand(parts[1:], msgTrigger, client)
	case "--pergunte", "-p", "!pergunte", "!p", "/pergunte", "/p":
		h.handleAskQuestionCommand(parts[1:], msgTrigger, client)
	case "--dia", "-d", "!dia", "!d", "/dia", "/d", "--daily", "/daily":
		h.handleDailySummaryCommand(parts[1:], msgTrigger, client)
	case "--help", "-h", "!help", "!h", "/help", "/h":
		h.handleHelpCommand(msgTrigger, client)
	case "--version", "-v", "!version", "!v", "/version", "/v":
		h.handleVersionCommand(msgTrigger, client)
	default:
		h.logger.Debug("Unknown command", "command", command)
	}
}
