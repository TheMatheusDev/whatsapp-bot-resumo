package cmd

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
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

	// settingsCache caches per-group settings to avoid DB round-trips on every
	// message. Keys are chatID strings; values are *wstypes.GroupSettings.
	// Invalidated explicitly when a group's settings are updated via admin commands.
	settingsCache sync.Map

	// groupInfoCache caches WhatsApp GroupInfo responses to avoid a network
	// round-trip on every admin command. Keys are JID strings; values are
	// cachedGroupInfo (info + expiresAt). TTL is groupInfoTTL (6 hours).
	groupInfoCache sync.Map

	// joiningGroups is an atomic set (sync.Map used as map[string]struct{}) that
	// prevents duplicate onboarding when rapid reconnects fire multiple
	// JoinedGroup events for the same group concurrently. LoadOrStore ensures
	// only one goroutine proceeds; the entry is deleted when onboarding finishes.
	joiningGroups sync.Map

	// weeklyRankingRunning is an atomic flag that prevents concurrent executions
	// of the weekly ranking. It is set to 1 while performWeeklyRanking is running
	// and reset to 0 when it finishes.
	weeklyRankingRunning atomic.Bool
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
	case *events.GroupInfo:
		h.handleGroupInfoEvent(v)
	case *events.JoinedGroup:
		h.handleJoinedGroupEvent(v)
	case *events.HistorySync:
		h.logger.Debug("History sync received", "type", v.Data.GetSyncType().String())
	default:
		h.logger.Debug("Unhandled event", "type", fmt.Sprintf("%T", v))
	}
}

// handleMessage processes incoming messages
func (h *Handler) handleMessage(evt *events.Message) {
	ctx := context.Background()
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

	// Save to database for all groups the bot is in.
	// Messages sent before bot start are still stored for historical context.
	msgID, err := h.saveMessage(message, evt.Info.Chat)
	if err != nil {
		h.logger.Error("Failed to save message", "error", err)
	}

	// If this is a voice message, trigger async transcription
	if audioMsg := msg.GetAudioMessage(); audioMsg != nil && msgID > 0 {
		if isVoiceMessage(audioMsg) {
			audioData, mimeType, err := h.tryDownloadAudioToMemory(audioMsg)
			if err != nil {
				h.logger.Error("Failed to download audio to memory for transcription", "error", err)
			} else {
				h.transcribeAudioAsync(msgID, audioData, mimeType)
			}
		}
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
	if h.everyoneHandler.ContainsEveryoneMention(currentMessageText) && h.isAuthorized(evt.Info) {
		// Check if user is authorized to use @everyone (by JID, not display name)
		senderJID := evt.Info.Sender.User
		if h.everyoneHandler.IsEveryoneAdmin(ctx, evt.Info.Chat, senderJID) {
			h.everyoneHandler.HandleEveryoneCommand(evt.Info.Chat, h.dbService, h.cache, currentMessageText)
		} else {
			h.logger.Info("Unauthorized @everyone attempt", "sender_jid", senderJID)
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
	case "!resuma", "!r":
		h.handleSummarizeCommand(parts[1:], msgTrigger)
	case "!clt":
		h.handleSummarizeCltCommand(parts[1:], msgTrigger)
	case "!farialimer", "!fl":
		h.handleSummarizeFariaLimerCommand(parts[1:], msgTrigger)
	case "!zoomer", "!z":
		h.handleSummarizeZoomerCommand(parts[1:], msgTrigger)
	case "!pergunta", "!p":
		h.handleAskQuestionCommand(parts[1:], msgTrigger)
	case "!dia", "!d":
		h.handleDailySummaryCommand(parts[1:], msgTrigger)
	case "!help", "!h":
		h.handleHelpCommand(msgTrigger)
	case "!version", "!v":
		h.handleVersionCommand(msgTrigger)
	case "!ping":
		h.handlePingCommand(msgTrigger)
	case "!regras", "!rg":
		h.handleRulesCommand(msgTrigger)
		// --- per-group admin commands ---
	case "!setregras":
		h.handleSetRulesCommand(parts[1:], msgTrigger)
	case "!addwelcome":
		h.handleAddWelcomeCommand(parts[1:], msgTrigger)
	case "!delwelcome":
		h.handleDelWelcomeCommand(parts[1:], msgTrigger)
	case "!addfarewell":
		h.handleAddFarewellCommand(parts[1:], msgTrigger)
	case "!delfarewell":
		h.handleDelFarewellCommand(parts[1:], msgTrigger)
	case "!resumo":
		h.handleDailySummaryToggle(parts[1:], msgTrigger)
	case "!ranking":
		h.handleWeeklyRankingToggle(parts[1:], msgTrigger)
	case "!admincache":
		h.handleAdminCacheCommand(msgTrigger)
	// --- per-group read-only commands (admin only) ---
	case "!welcome":
		h.handleListWelcomeCommand(msgTrigger)
	case "!farewell":
		h.handleListFarewellCommand(msgTrigger)
	case "!configs":
		h.handleConfigsCommand(msgTrigger)
	default:
		h.logger.Debug("Unknown command", "command", command)
	}
}

// getGroupSettings returns the cached GroupSettings for a group, fetching from
// the DB on a cache miss. Returns nil when the group has no DB record yet
// (callers should fall back to global config defaults in that case).
func (h *Handler) getGroupSettings(chatID string) *wstypes.GroupSettings {
	if v, ok := h.settingsCache.Load(chatID); ok {
		if gs, ok := v.(*wstypes.GroupSettings); ok {
			return gs
		}
	}

	gs, err := h.dbService.GetGroupSettings(chatID)
	if err != nil {
		h.logger.Error("getGroupSettings: DB error", "chat_id", chatID, "error", err)
		return nil
	}
	if gs != nil {
		h.settingsCache.Store(chatID, gs)
	}
	return gs
}

// invalidateGroupSettings removes a group's settings from the in-memory cache,
// forcing the next getGroupSettings call to re-fetch from the DB.
func (h *Handler) invalidateGroupSettings(chatID string) {
	h.settingsCache.Delete(chatID)
}
