package cmd

import (
	"fmt"
	"strings"
	"time"

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
		everyoneHandler: NewEveryoneHandler(config, logger, loc, whatsappService),
		whitelistMap:    whitelistMap,
	}, nil
}

// HandleEvent handles WhatsApp events
func (h *Handler) HandleEvent(evt interface{}) {
	switch v := evt.(type) {
	case *events.Message:
		h.handleMessage(v)
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
func (h *Handler) handleMessage(evt *events.Message) {
	msg := evt.Message
	if msg == nil {
		return
	}

	// Download media if present (images, videos, etc.)
	h.downloadMediaIfPresent(msg)

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
	if err := h.saveMessage(message, evt.Info.Chat); err != nil {
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
			h.everyoneHandler.HandleEveryoneCommand(evt.Info.Chat, h.dbService, h.cache, currentMessageText)
		} else {
			h.logger.Info("Unauthorized @everyone attempt", "sender", senderName)
		}
	}

	// Process commands if from authorized users (only for new messages)
	if h.isAuthorized(evt.Info) && h.isCommand(content) {
		h.handleCommand(content, evt.Info)
	}
}

// handleCommand processes bot commands
func (h *Handler) handleCommand(content string, msgTrigger types.MessageInfo) {
	parts := strings.Fields(content)
	if len(parts) == 0 {
		return
	}

	command := strings.ToLower(parts[0])

	switch command {
	case "--resuma", "-r", "!resumo", "!resuma", "!r", "/resuma", "/resumo", "/r":
		h.handleSummarizeCommand(parts[1:], msgTrigger)
	case "-clt", "!clt", "--clt", "/clt":
		h.handleSummarizeCltCommand(parts[1:], msgTrigger)
	case "--farialimer", "-fl", "!farialimer", "!fl", "/farialimer", "/fl":
		h.handleSummarizeFariaLimerCommand(parts[1:], msgTrigger)
	case "--zoomer", "-z", "!zoomer", "!z", "/zoomer", "/z":
		h.handleSummarizeZoomerCommand(parts[1:], msgTrigger)
	case "--pergunte", "-p", "!pergunte", "!p", "/pergunte", "/p":
		h.handleAskQuestionCommand(parts[1:], msgTrigger)
	case "--dia", "-d", "!dia", "!d", "/dia", "/d", "--daily", "/daily":
		h.handleDailySummaryCommand(parts[1:], msgTrigger)
	case "--help", "-h", "!help", "!h", "/help", "/h":
		h.handleHelpCommand(msgTrigger)
	case "--version", "-v", "!version", "!v", "/version", "/v":
		h.handleVersionCommand(msgTrigger)
	case "!ping", "--ping", "/ping":
		h.handlePingCommand(msgTrigger)
	default:
		h.logger.Debug("Unknown command", "command", command)
	}
}
