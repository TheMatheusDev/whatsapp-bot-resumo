package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	waE2E "go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	wstypes "whatsapp-summarizer/src/types"
)

// Handler manages WhatsApp message events
type Handler struct {
	config          *wstypes.Config
	aiService       wstypes.AIService
	dbService       wstypes.DatabaseService
	cache           wstypes.CacheService
	logger          wstypes.Logger
	botStartTime    time.Time
	timezone        *time.Location
	everyoneHandler *EveryoneHandler
}

// NewHandler creates a new message handler
func NewHandler(
	config *wstypes.Config,
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
		config:          config,
		aiService:       aiService,
		dbService:       dbService,
		cache:           cache,
		logger:          logger,
		botStartTime:    botStartTime,
		timezone:        loc,
		everyoneHandler: NewEveryoneHandler(config, logger, loc),
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

// downloadMediaIfPresent checks if message contains media and downloads it
func (h *Handler) downloadMediaIfPresent(msg *waE2E.Message, client *whatsmeow.Client) {
	// Check for image
	if imgMsg := msg.GetImageMessage(); imgMsg != nil {
		if err := h.downloadImage(imgMsg, client); err != nil {
			h.logger.Error("Failed to download image", "error", err)
		}
		return
	}

	// Check for video
	if videoMsg := msg.GetVideoMessage(); videoMsg != nil {
		if err := h.downloadVideo(videoMsg, client); err != nil {
			h.logger.Error("Failed to download video", "error", err)
		}
		return
	}

	// Check for audio
	if audioMsg := msg.GetAudioMessage(); audioMsg != nil {
		if err := h.downloadAudio(audioMsg, client); err != nil {
			h.logger.Error("Failed to download audio", "error", err)
		}
		return
	}

	// Check for document
	if docMsg := msg.GetDocumentMessage(); docMsg != nil {
		if err := h.downloadDocument(docMsg, client); err != nil {
			h.logger.Error("Failed to download document", "error", err)
		}
		return
	}

	// Check for sticker
	if stickerMsg := msg.GetStickerMessage(); stickerMsg != nil {
		if err := h.downloadSticker(stickerMsg, client); err != nil {
			h.logger.Error("Failed to download sticker", "error", err)
		}
		return
	}
}

// downloadImage downloads and saves a full resolution image to /tmp/
func (h *Handler) downloadImage(imgMsg *waE2E.ImageMessage, client *whatsmeow.Client) error {
	// Create tmp directory if it doesn't exist
	if err := os.MkdirAll("tmp", 0755); err != nil {
		return fmt.Errorf("failed to create tmp directory: %w", err)
	}

	// Create filename with timestamp
	timestamp := time.Now().Format("20060102_150405")
	filename := fmt.Sprintf("image_%s.jpg", timestamp)
	filePath := filepath.Join("tmp", filename)

	// Create file
	file, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	// Download full image using whatsmeow's DownloadToFile
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	err = client.DownloadToFile(ctx, imgMsg, file)
	if err != nil {
		// If download fails, try to save thumbnail as fallback
		h.logger.Warn("Failed to download full image, saving thumbnail instead", "error", err)
		thumbnailData := imgMsg.GetJPEGThumbnail()
		if len(thumbnailData) > 0 {
			if err := os.WriteFile(filePath, thumbnailData, 0644); err != nil {
				return fmt.Errorf("failed to write thumbnail: %w", err)
			}
			h.logger.Info("Image thumbnail saved", "path", filePath)
			return nil
		}
		return fmt.Errorf("failed to download image and no thumbnail available: %w", err)
	}

	h.logger.Info("Image downloaded", "path", filePath, "size", getFileSize(file))
	return nil
}

// downloadVideo downloads and saves a video to /tmp/
func (h *Handler) downloadVideo(videoMsg *waE2E.VideoMessage, client *whatsmeow.Client) error {
	if err := os.MkdirAll("tmp", 0755); err != nil {
		return fmt.Errorf("failed to create tmp directory: %w", err)
	}

	timestamp := time.Now().Format("20060102_150405")
	filename := fmt.Sprintf("video_%s.mp4", timestamp)
	filePath := filepath.Join("tmp", filename)

	file, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	if err := client.DownloadToFile(ctx, videoMsg, file); err != nil {
		return fmt.Errorf("failed to download video: %w", err)
	}

	h.logger.Info("Video downloaded", "path", filePath, "size", getFileSize(file))
	return nil
}

// downloadAudio downloads and saves an audio to /tmp/
func (h *Handler) downloadAudio(audioMsg *waE2E.AudioMessage, client *whatsmeow.Client) error {
	if err := os.MkdirAll("tmp", 0755); err != nil {
		return fmt.Errorf("failed to create tmp directory: %w", err)
	}

	timestamp := time.Now().Format("20060102_150405")
	ext := "ogg" // Default extension for WhatsApp audio
	if mimetype := audioMsg.GetMimetype(); mimetype != "" {
		if strings.Contains(mimetype, "mp3") {
			ext = "mp3"
		} else if strings.Contains(mimetype, "mp4") {
			ext = "m4a"
		}
	}
	filename := fmt.Sprintf("audio_%s.%s", timestamp, ext)
	filePath := filepath.Join("tmp", filename)

	file, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if err := client.DownloadToFile(ctx, audioMsg, file); err != nil {
		return fmt.Errorf("failed to download audio: %w", err)
	}

	h.logger.Info("Audio downloaded", "path", filePath, "size", getFileSize(file))
	return nil
}

// downloadDocument downloads and saves a document to /tmp/
func (h *Handler) downloadDocument(docMsg *waE2E.DocumentMessage, client *whatsmeow.Client) error {
	if err := os.MkdirAll("tmp", 0755); err != nil {
		return fmt.Errorf("failed to create tmp directory: %w", err)
	}

	timestamp := time.Now().Format("20060102_150405")
	// Use original filename if available
	filename := docMsg.GetTitle()
	if filename == "" {
		filename = fmt.Sprintf("document_%s", timestamp)
	} else {
		// Sanitize filename and add timestamp to avoid conflicts
		filename = fmt.Sprintf("%s_%s", timestamp, sanitizeFilename(filename))
	}
	filePath := filepath.Join("tmp", filename)

	file, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	if err := client.DownloadToFile(ctx, docMsg, file); err != nil {
		return fmt.Errorf("failed to download document: %w", err)
	}

	h.logger.Info("Document downloaded", "path", filePath, "size", getFileSize(file))
	return nil
}

// downloadSticker downloads and saves a sticker to /tmp/
func (h *Handler) downloadSticker(stickerMsg *waE2E.StickerMessage, client *whatsmeow.Client) error {
	if err := os.MkdirAll("tmp", 0755); err != nil {
		return fmt.Errorf("failed to create tmp directory: %w", err)
	}
	timestamp := time.Now().Format("20060102_150405")
	filename := fmt.Sprintf("sticker_%s.webp", timestamp)
	filePath := filepath.Join("tmp", filename)
	file, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := client.DownloadToFile(ctx, stickerMsg, file); err != nil {
		return fmt.Errorf("failed to download sticker: %w", err)
	}
	h.logger.Info("Sticker downloaded", "path", filePath, "size", getFileSize(file))
	return nil
}

// sanitizeFilename removes potentially dangerous characters from filename
func sanitizeFilename(filename string) string {
	// Remove path separators and other dangerous characters
	dangerous := []string{"/", "\\", "..", ":", "*", "?", "\"", "<", ">", "|"}
	for _, char := range dangerous {
		filename = strings.ReplaceAll(filename, char, "_")
	}
	return filename
}

// getFileSize returns the size of a file in a human-readable format
func getFileSize(file *os.File) string {
	info, err := file.Stat()
	if err != nil {
		return "unknown"
	}
	size := info.Size()
	if size < 1024 {
		return fmt.Sprintf("%d B", size)
	} else if size < 1024*1024 {
		return fmt.Sprintf("%.2f KB", float64(size)/1024)
	} else if size < 1024*1024*1024 {
		return fmt.Sprintf("%.2f MB", float64(size)/(1024*1024))
	}
	return fmt.Sprintf("%.2f GB", float64(size)/(1024*1024*1024))
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

	// For groups, check if the group is whitelisted
	chatID := info.Chat.User
	for _, allowedGroup := range h.config.WhatsApp.GroupWhitelist {
		if chatID == allowedGroup {
			return true
		}
	}

	// Group not whitelisted
	return false
}

// isCommand checks if a message is a bot command
func (h *Handler) isCommand(content string) bool {
	return strings.HasPrefix(content, "--") || strings.HasPrefix(content, "-") || strings.HasPrefix(content, "!") || strings.HasPrefix(content, "/")
}

// handleCommand processes bot commands
func (h *Handler) handleCommand(content string, info types.MessageInfo, client *whatsmeow.Client) {
	parts := strings.Fields(content)
	if len(parts) == 0 {
		return
	}

	command := strings.ToLower(parts[0])

	switch command {
	case "--resuma", "-r", "!resumo", "!resuma", "!r", "/resuma", "/resumo", "/r":
		h.handleSummarizeCommand(parts[1:], info, client)
	case "-clt", "!clt", "--clt", "/clt":
		h.handleSummarizeCltCommand(parts[1:], info, client)
	case "--pergunte", "-p", "!pergunte", "!p", "/pergunte", "/p":
		h.handleAskQuestionCommand(parts[1:], info, client)
	case "--help", "-h", "!help", "!h", "/help", "/h":
		h.handleHelpCommand(info, client)
	case "--version", "-v", "!version", "!v", "/version", "/v":
		h.handleVersionCommand(info, client)
	default:
		h.logger.Debug("Unknown command", "command", command)
	}
}

// handleSummarizeCltCommand handles the -clt command (shortcut for -r with --clt flag)
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

func (h *Handler) sendMessageReply(client *whatsmeow.Client, info types.MessageInfo, message string) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*30)
	defer cancel()

	_, err := client.SendMessage(ctx, info.Chat, &waE2E.Message{
		ExtendedTextMessage: &waE2E.ExtendedTextMessage{
			Text: proto.String(message),
			ContextInfo: &waE2E.ContextInfo{
				StanzaID:    proto.String(info.ID),
				Participant: proto.String(info.Sender.String()),
			},
		},
	})
	if err != nil {
		h.logger.Error("Failed to send message", "error", err)
	}
}

func (h *Handler) sendErrorMessage(client *whatsmeow.Client, chat types.JID, message string) {
	errorMsg := fmt.Sprintf("❌ %s", message)
	h.sendMessage(client, chat, errorMsg)
}

func (h *Handler) sendErrorMessageReply(client *whatsmeow.Client, info types.MessageInfo, message string) {
	errorMsg := fmt.Sprintf("❌ %s", message)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*30)
	defer cancel()

	_, err := client.SendMessage(ctx, info.Chat, &waE2E.Message{
		ExtendedTextMessage: &waE2E.ExtendedTextMessage{
			Text: proto.String(errorMsg),
			ContextInfo: &waE2E.ContextInfo{
				StanzaID:    proto.String(info.ID),
				Participant: proto.String(info.Sender.String()),
			},
		},
	})
	if err != nil {
		h.logger.Error("Failed to send error message", "error", err)
	}
}
