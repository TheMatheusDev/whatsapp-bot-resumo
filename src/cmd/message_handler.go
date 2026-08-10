package cmd

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	waE2E "go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"whatsapp-summarizer/src/ai"
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
	// botLID is the bare numeric User part of the bot's own LID JID.
	// Used for comparing against Participant/MentionedJID in LID format.
	botLID string
	// botPhoneUser is the bare phone-number User part of the bot's own JID
	// (e.g. "5521999999999"). WhatsApp still uses the phone number in
	// MentionedJID and Participant for many clients, so we check both.
	botPhoneUser string

	// personalityLoader holds the hot-swappable personality prompts loaded
	// from TOML files. Commands that need personality prompts use this loader
	// instead of hardcoded constants.
	personalityLoader *ai.PersonalityLoader

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

	// sumRateLimitCache enforces a per-user cooldown on summarize commands.
	// Keys are "chatID:senderUser" strings; values are time.Time (last execution).
	// Prevents a single user from flooding the Gemini API.
	sumRateLimitCache sync.Map

	// chatRateLimitCache enforces a per-user cooldown on chatbot triggers
	// (mentions and replies). Separate from sumRateLimitCache so the two
	// features do not share rate limit state. Cooldown: chatCooldown (5 s).
	chatRateLimitCache sync.Map

	// chatLastInteraction tracks the last successful chatbot response time per
	// group (keys are chatID strings, values are time.Time). Used to switch
	// between cold (100 msgs) and warm (30 msgs) context windows.
	chatLastInteraction sync.Map

	// shutdownCh is closed when the handler is shutting down. All goroutines
	// spawned by the handler should select on this channel to detect shutdown.
	shutdownCh chan struct{}

	// wg tracks all inflight goroutines spawned by the handler. Shutdown() waits
	// on wg so that every in-progress operation finishes before resources are torn down.
	wg sync.WaitGroup
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
	personalityLoader *ai.PersonalityLoader,
) (*Handler, error) {
	// Parse timezone from config
	loc, err := time.LoadLocation(config.Bot.Timezone)
	if err != nil {
		loc = time.FixedZone(config.Bot.Timezone, -3*60*60)
	}

	return &Handler{
		config:            config,
		aiService:         aiService,
		dbService:         dbService,
		whatsappService:   whatsappService,
		cache:             cache,
		logger:            logger,
		botStartTime:      botStartTime,
		timezone:          loc,
		botLID:            whatsappService.GetBotJID().User,
		botPhoneUser:      whatsappService.GetBotPhoneUser(),
		shutdownCh:        make(chan struct{}),
		personalityLoader: personalityLoader,
	}, nil
}

// SetBotIdentity stores the bot's own JID identifiers on the handler.
// Must be called after the WhatsApp client connects, since the JID values
// (LID and phone number) are only available once the session is established.
// Both lid (LID user part) and phoneUser (phone number user part) are stored
// so that isBotMentioned and isReplyToBot can match against either format.
func (h *Handler) SetBotIdentity(lid, phoneUser string) {
	h.botLID = lid
	h.botPhoneUser = phoneUser
	h.logger.Info("Bot identity set on handler",
		"bot_lid", lid,
		"bot_phone", phoneUser)
}

// Shutdown signals the handler to stop accepting new events and waits for all
// inflight goroutines to finish. It is safe to call Shutdown more than once.
func (h *Handler) Shutdown() {
	select {
	case <-h.shutdownCh:
		// already closed
	default:
		close(h.shutdownCh)
	}
	h.wg.Wait()
}

// HandleEvent handles WhatsApp events
func (h *Handler) HandleEvent(evt interface{}) {
	// Reject new events when the handler is shutting down.
	select {
	case <-h.shutdownCh:
		h.logger.Debug("HandleEvent: handler is shutting down, dropping event", "type", fmt.Sprintf("%T", evt))
		return
	default:
	}

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

	// Build contact + chat objects for the batch writer.
	contact := wstypes.Contact{
		LID:       evt.Info.Sender.User,
		Name:      h.getSenderName(evt.Info),
		UpdatedAt: time.Now().UTC(),
	}

	chatType := "group"
	if !evt.Info.IsGroup {
		chatType = "direct"
	}
	chat := wstypes.Chat{
		ChatID:   evt.Info.Chat.User,
		ChatType: chatType,
	}

	// Create message object
	message := wstypes.Message{
		ChatID:      evt.Info.Chat.User,
		SenderLID:   evt.Info.Sender.User, // bare numeric User, matches contacts.lid format
		Sender:      contact.Name,
		Content:     content,
		MessageType: h.getMessageType(msg),
		Timestamp:   evt.Info.Timestamp.In(h.timezone),
	}

	groupName := h.getGroupName(evt.Info.Chat)

	// Enqueue the three writes (contact+chat+message) into the batch writer.
	// For voice messages we need the real row ID, so we pass a real resultCh.
	// For all other messages we use a discard channel (fire-and-forget).
	if audioMsg := msg.GetAudioMessage(); audioMsg != nil && isVoiceMessage(audioMsg) {
		resultCh := make(chan wstypes.MessageResult, 1)
		h.dbService.EnqueueMessage(contact, chat, message, groupName, resultCh)

		audioData, mimeType, err := h.tryDownloadAudioToMemory(audioMsg)
		if err != nil {
			h.logger.Error("Failed to download audio to memory for transcription", "error", err)
			// Drain the result channel to prevent the batch writer's goroutine from blocking.
			go func() { <-resultCh }()
		} else {
			// Wait for the batch flush to get the row ID, then transcribe.
			h.wg.Add(1)
			go func() {
				defer h.wg.Done()
				result := <-resultCh
				if result.Err != nil {
					h.logger.Error("Failed to save audio message before transcription", "error", result.Err)
					return
				}
				if result.ID > 0 {
					h.transcribeAudioAsync(result.ID, audioData, mimeType)
				}
			}()
		}
	} else {
		// No result needed — use a discard channel.
		discardCh := make(chan wstypes.MessageResult, 1)
		h.dbService.EnqueueMessage(contact, chat, message, groupName, discardCh)
		go func() { <-discardCh }() // drain so the batch writer goroutine never blocks
	}

	// Extract the actual command content by stripping media prefixes if present
	cmdContent := content
	if strings.HasPrefix(cmdContent, "[Image] ") {
		cmdContent = strings.TrimPrefix(cmdContent, "[Image] ")
	} else if strings.HasPrefix(cmdContent, "[Video] ") {
		cmdContent = strings.TrimPrefix(cmdContent, "[Video] ")
	} else if strings.HasPrefix(cmdContent, "[Document] ") {
		cmdContent = strings.TrimPrefix(cmdContent, "[Document] ")
	}

	// Skip command processing for messages sent before bot started
	if evt.Info.Timestamp.Before(h.botStartTime) {
		h.logger.Debug("Message saved but skipping command processing (sent before bot started)",
			"timestamp", evt.Info.Timestamp,
			"bot_start_time", h.botStartTime)
		return
	}

	// Process commands (only for new messages)
	if h.isCommand(cmdContent) {
		h.handleCommand(cmdContent, evt.Info, evt.Message)
		return
	}

	// Detect mention or reply to the bot (groups only, post-boot messages only).
	// Messages that start with '!' are already handled as commands above and
	// must never double-trigger the chatbot — the HasPrefix guard below is a
	// safety net for any edge case where isCommand returned false but the
	// content still begins with a command prefix.
	if evt.Info.IsGroup && !strings.HasPrefix(cmdContent, "!") {
		if h.isBotMentioned(msg) || h.isReplyToBot(msg) {
			h.wg.Add(1)
			go func() {
				defer h.wg.Done()
				h.handleChatResponse(evt)
			}()
		}
	}
}

// handleCommand processes bot commands.
// msg is the full raw protobuf message — required by commands that need to inspect
// ContextInfo (e.g. quoted media for !figurinha).
func (h *Handler) handleCommand(content string, msgTrigger types.MessageInfo, msg *waE2E.Message) {
	parts := strings.Fields(content)
	if len(parts) == 0 {
		return
	}

	command := strings.ToLower(parts[0])

	// rawArgs is the text after the command with newlines preserved.
	// Finds the first whitespace character (space, \t, \n or \r) and takes everything after it.
	// Commands with no arguments result in an empty string — handlers should use
	// strings.TrimSpace(rawArgs) == "" to detect missing args.
	rawArgs := ""
	if idx := strings.IndexAny(content, " \t\n\r"); idx >= 0 {
		rawArgs = content[idx+1:]
	}

	// Build a synthetic events.Message for commands that need the full event context
	// (e.g. !figurinha which reads ContextInfo from the quoted message).
	syntheticEvt := &events.Message{Info: msgTrigger, Message: msg}

	// Commands allowed in Direct Messages (DMs).
	// Every other command requires a group context.
	dmAllowed := map[string]bool{
		"!figurinha": true,
		"!sticker":   true,
		"!help":      true,
		"!h":         true,
		"!version":   true,
		"!v":         true,
		"!ping":      true,
	}

	if !msgTrigger.IsGroup && !dmAllowed[command] {
		h.reactToCommand(msgTrigger, "❌")
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID,
			"❌ Esse comando só é permitido em grupos.\nUse-o em um grupo onde o bot esteja presente ou adicione o bot ao grupo.")
		return
	}

	switch command {
	case "!figurinha", "!sticker":
		h.handleStickerCommand(syntheticEvt)
	case "!resuma", "!resumo", "!r":
		h.handleSummarizeCommand(parts[1:], msgTrigger)
	case "!clt":
		h.handleSummarizeCltCommand(parts[1:], msgTrigger)
	case "!farialimer", "!fl":
		h.handleSummarizeFariaLimerCommand(parts[1:], msgTrigger)
	case "!zoomer", "!z":
		h.handleSummarizeZoomerCommand(parts[1:], msgTrigger)
	case "!profeta", "!pft":
		h.handleSummarizeProfetaCommand(parts[1:], msgTrigger)
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
	// These three receive rawArgs to preserve newlines in free-form text:
	case "!setregras":
		h.handleSetRulesCommand(rawArgs, msgTrigger)
	case "!addwelcome":
		h.handleAddWelcomeCommand(rawArgs, msgTrigger)
	case "!delwelcome":
		h.handleDelWelcomeCommand(parts[1:], msgTrigger)
	case "!addfarewell":
		h.handleAddFarewellCommand(rawArgs, msgTrigger)
	case "!delfarewell":
		h.handleDelFarewellCommand(parts[1:], msgTrigger)
	case "!resumodia":
		h.handleDailySummaryToggle(parts[1:], msgTrigger)
	case "!ranking":
		h.handleWeeklyRankingToggle(parts[1:], msgTrigger)
	case "!chatbot":
		h.handleChatbotToggle(parts[1:], msgTrigger)
	case "!cache":
		h.handleAdminCacheCommand(msgTrigger)
	// --- per-group read-only commands (admin only) ---
	case "!welcome":
		h.handleListWelcomeCommand(msgTrigger)
	case "!farewell":
		h.handleListFarewellCommand(msgTrigger)
	case "!config":
		h.handleConfigCommand(msgTrigger)
	case "!reload":
		h.handleReloadPersonalitiesCommand(msgTrigger)
	case "!personalidade":
		h.handleSetPersonalityCommand(parts[1:], msgTrigger)
	case "!personalidades":
		h.handleListPersonalitiesCommand(msgTrigger)
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
