package cmd

import (
	"context"
	"strings"
	"time"

	"go.mau.fi/whatsmeow/types"

	wstypes "whatsapp-summarizer/src/types"
)

// EveryoneHandler handles @everyone mentions
type EveryoneHandler struct {
	config          *wstypes.Config
	logger          wstypes.Logger
	timezone        *time.Location
	whatsappService wstypes.WhatsAppService
}

// NewEveryoneHandler creates a new everyone handler
func NewEveryoneHandler(config *wstypes.Config, logger wstypes.Logger, timezone *time.Location, whatsappService wstypes.WhatsAppService) *EveryoneHandler {
	return &EveryoneHandler{
		config:          config,
		logger:          logger,
		timezone:        timezone,
		whatsappService: whatsappService,
	}
}

// ContainsEveryoneMention checks if message contains @everyone mentions
func (e *EveryoneHandler) ContainsEveryoneMention(content string) bool {
	return strings.Contains(content, "@everyone") ||
		strings.Contains(content, "@todos") ||
		strings.Contains(content, "@here")
}

// IsEveryoneAdmin checks if the user is authorized to use @everyone
func (e *EveryoneHandler) IsEveryoneAdmin(senderName string) bool {
	// If no admins configured, allow everyone (backward compatibility)
	if len(e.config.WhatsApp.EveryoneAdmins) == 0 {
		return true
	}

	// Normalize sender name for comparison (lowercase and trim)
	normalizedSender := strings.ToLower(strings.TrimSpace(senderName))

	// Check if sender is in the admin list
	for _, admin := range e.config.WhatsApp.EveryoneAdmins {
		normalizedAdmin := strings.ToLower(strings.TrimSpace(admin))
		if normalizedSender == normalizedAdmin {
			return true
		}
	}

	return false
}

// HandleEveryoneCommand mentions all group members when @everyone is detected
func (e *EveryoneHandler) HandleEveryoneCommand(chat types.JID, dbService wstypes.DatabaseService, cache wstypes.CacheService, messageContent string) {
	go func() {
		// Check if it's a group chat
		if chat.Server != types.GroupServer {
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), time.Second*30)
		defer cancel()

		// Get group info
		groupInfo, err := e.whatsappService.GetGroupInfo(ctx, chat)
		if err != nil {
			e.logger.Error("Failed to get group info for @everyone", "error", err, "chat_id", chat.String())
			return
		}

		members := groupInfo.Participants
		if len(members) == 0 {
			e.logger.Warn("No members found in group", "chat_id", chat.String())
			return
		}

		// Get bot JID to exclude from mentions
		botJID := e.whatsappService.GetBotJID()

		// Create mentions string with all group members
		var mentionTexts []string
		var mentionJIDs []string

		for _, member := range members {
			// Skip if it's the bot itself
			if member.JID.User == botJID.User {
				continue
			}

			mentionTexts = append(mentionTexts, "@"+member.JID.User)
			mentionJIDs = append(mentionJIDs, member.JID.String())
		}

		if len(mentionJIDs) == 0 {
			e.logger.Warn("No members to mention", "chat_id", chat.String())
			return
		}

		// Create the message with mentions
		messageText := "ℹ️ " + messageContent + "\n" + strings.Join(mentionTexts, " ")

		// Send message with mentions
		err = e.whatsappService.SendMentionMessage(ctx, chat, messageText, mentionJIDs)
		if err != nil {
			e.logger.Error("Failed to send @everyone message", "error", err, "chat_id", chat.String())
			return
		}

		// Log the message to database
		everyoneMsg := wstypes.Message{
			ChatID:      chat.User,
			Sender:      "ProfetaBOT [VOCÊ]",
			Content:     "[MENTIONED EVERYONE]",
			MessageType: "EveryoneMention",
			Timestamp:   time.Now().In(e.timezone),
		}

		// Get group name and save message
		groupName := e.getGroupName(chat, cache)
		if err := dbService.SaveGroupMessage(everyoneMsg, groupName); err != nil {
			e.logger.Error("Failed to save @everyone message to database", "error", err)
		}

		e.logger.Info("@everyone command executed successfully",
			"chat_id", chat.String(),
			"mentioned_count", len(mentionJIDs))
	}()
}

// getGroupName gets the group name, using cache when possible
func (e *EveryoneHandler) getGroupName(chat types.JID, cache wstypes.CacheService) string {
	if chat.Server != types.GroupServer {
		return "Direct Chat"
	}

	chatID := chat.User

	// Try to get from cache first
	if name, exists := cache.GetGroupName(chatID); exists {
		return name
	}

	// Cache miss, fetch from WhatsApp API
	groupInfo, err := e.whatsappService.GetGroupInfo(context.Background(), chat)
	groupName := chatID // fallback to chat ID
	if err == nil && groupInfo != nil {
		groupName = groupInfo.Name
	}

	// Update cache
	cache.SetGroupName(chatID, groupName)

	return groupName
}
