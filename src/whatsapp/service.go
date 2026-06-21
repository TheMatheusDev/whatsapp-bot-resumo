package whatsapp

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/mdp/qrterminal/v3"
	"go.mau.fi/whatsmeow"
	waE2E "go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	watypes "go.mau.fi/whatsmeow/types"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"

	wstypes "whatsapp-summarizer/src/types"
)

// Service implements the WhatsAppService interface
type Service struct {
	client       *whatsmeow.Client
	container    *sqlstore.Container
	logger       wstypes.Logger
	eventHandler func(interface{})
	connected    bool
}

// NewService creates a new WhatsApp service
func NewService(container *sqlstore.Container, logger wstypes.Logger, eventHandler func(interface{})) (*Service, error) {
	if container == nil {
		return nil, fmt.Errorf("sqlstore container is required")
	}

	return &Service{
		container:    container,
		logger:       logger,
		eventHandler: eventHandler,
	}, nil
}

// Initialize initializes the WhatsApp client
func (s *Service) Initialize(ctx context.Context) error {
	deviceStore, err := s.container.GetFirstDevice(ctx)
	if err != nil {
		return fmt.Errorf("failed to get device store: %w", err)
	}

	clientLog := waLog.Stdout("WhatsApp", "WARN", true)
	s.client = whatsmeow.NewClient(deviceStore, clientLog)

	// Add event handler
	if s.eventHandler != nil {
		s.client.AddEventHandler(s.eventHandler)
	}

	s.logger.Info("WhatsApp client initialized")
	return nil
}

// Connect connects to WhatsApp
func (s *Service) Connect(ctx context.Context) error {
	if s.client == nil {
		return fmt.Errorf("client not initialized")
	}

	if s.client.Store.ID == nil {
		// No ID stored, new login
		s.logger.Info("No stored session, starting new login")
		return s.performNewLogin(ctx)
	}

	// Already logged in, just connect
	s.logger.Info("Using existing session, connecting...")
	err := s.client.Connect()
	if err != nil {
		return fmt.Errorf("failed to connect with existing session: %w", err)
	}

	s.connected = true
	s.logger.Info("Connected to WhatsApp successfully")
	return nil
}

// performNewLogin handles the QR code login process
func (s *Service) performNewLogin(ctx context.Context) error {
	qrChan, err := s.client.GetQRChannel(ctx)
	if err != nil {
		return fmt.Errorf("failed to get QR channel: %w", err)
	}

	err = s.client.Connect()
	if err != nil {
		return fmt.Errorf("failed to connect for QR login: %w", err)
	}

	s.logger.Info("Waiting for QR code scan...")
	for evt := range qrChan {
		switch evt.Event {
		case "code":
			s.logger.Info("QR code received, please scan with WhatsApp")
			fmt.Println("Scan this QR code with WhatsApp:")
			qrterminal.GenerateHalfBlock(evt.Code, qrterminal.L, os.Stdout)
		case "success":
			s.logger.Info("QR code login successful")
			s.connected = true
			return nil
		case "timeout":
			return fmt.Errorf("QR code login timeout")
		default:
			s.logger.Debug("QR login event", "event", evt.Event)
		}
	}

	return fmt.Errorf("QR code login failed")
}

// SendMessage sends a text message to a chat
func (s *Service) SendMessage(chatID types.JID, message string) error {
	if s.client == nil {
		return fmt.Errorf("client not initialized")
	}

	if !s.connected {
		return fmt.Errorf("not connected to WhatsApp")
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*15)
	defer cancel()

	msg := &waE2E.Message{
		Conversation: proto.String(message),
	}

	_, err := s.client.SendMessage(ctx, chatID, msg)
	if err != nil {
		s.logger.Error("Failed to send message", "error", err, "chat_id", chatID.String())
		return fmt.Errorf("failed to send message: %w", err)
	}

	s.logger.Debug("Message sent successfully", "chat_id", chatID.String())
	return nil
}

func (s *Service) SendMessageReply(chatID types.JID, senderJID types.JID, replyTo types.MessageID, message string) error {
	if s.client == nil {
		return fmt.Errorf("client not initialized")
	}
	if !s.connected {
		return fmt.Errorf("not connected to WhatsApp")
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*15)
	defer cancel()

	msg := &waE2E.Message{
		ExtendedTextMessage: &waE2E.ExtendedTextMessage{
			Text: proto.String(message),
			ContextInfo: &waE2E.ContextInfo{
				StanzaID:    proto.String(replyTo),
				Participant: proto.String(senderJID.ToNonAD().String()),
			},
		},
	}

	_, err := s.client.SendMessage(ctx, chatID, msg)
	if err != nil {
		s.logger.Error("Failed to send reply message", "error", err, "chat_id", chatID.String())
		return fmt.Errorf("failed to send reply message: %w", err)
	}
	s.logger.Debug("Reply message sent successfully", "chat_id", chatID.String())
	return nil
}

// EditMessage sends an edit to an existing message
func (s *Service) EditMessage(chatID types.JID, messageID types.MessageID, newContent string) error {
	if s.client == nil {
		return fmt.Errorf("client not initialized")
	}

	if !s.connected {
		return fmt.Errorf("not connected to WhatsApp")
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*15)
	defer cancel()

	editMsg := s.client.BuildEdit(chatID, messageID, &waE2E.Message{
		Conversation: proto.String(newContent),
	})

	_, err := s.client.SendMessage(ctx, chatID, editMsg)
	if err != nil {
		s.logger.Error("Failed to send edit message", "error", err, "chat_id", chatID.String())
		return fmt.Errorf("failed to send edit message: %w", err)
	}

	s.logger.Debug("Edit message sent successfully", "chat_id", chatID.String())
	return nil
}

// Disconnect disconnects from WhatsApp
func (s *Service) Disconnect() {
	if s.client != nil {
		s.client.Disconnect()
		s.connected = false
		s.logger.Info("Disconnected from WhatsApp")
	}
}

// IsConnected returns the connection status
func (s *Service) IsConnected() bool {
	return s.connected && s.client != nil && s.client.IsConnected()
}

// SendRawMessage sends a raw protobuf message to a chat and returns the response.
// This is used for messages that need ContextInfo (e.g., replies with loading indicator).
// The returned SendResponse contains the message ID needed for subsequent EditMessage calls.
func (s *Service) SendRawMessage(ctx context.Context, chatID types.JID, msg *waE2E.Message) (whatsmeow.SendResponse, error) {
	if s.client == nil {
		return whatsmeow.SendResponse{}, fmt.Errorf("client not initialized")
	}
	if !s.connected {
		return whatsmeow.SendResponse{}, fmt.Errorf("not connected to WhatsApp")
	}

	resp, err := s.client.SendMessage(ctx, chatID, msg)
	if err != nil {
		s.logger.Error("Failed to send raw message", "error", err, "chat_id", chatID.String())
		return whatsmeow.SendResponse{}, fmt.Errorf("failed to send raw message: %w", err)
	}

	s.logger.Debug("Raw message sent successfully", "chat_id", chatID.String(), "msg_id", resp.ID)
	return resp, nil
}

// GetGroupInfo returns information about a WhatsApp group
func (s *Service) GetGroupInfo(ctx context.Context, chatID types.JID) (*watypes.GroupInfo, error) {
	if s.client == nil {
		return nil, fmt.Errorf("client not initialized")
	}

	groupInfo, err := s.client.GetGroupInfo(ctx, chatID)
	if err != nil {
		return nil, fmt.Errorf("failed to get group info: %w", err)
	}

	return groupInfo, nil
}

// DownloadToFile downloads media from a WhatsApp message to a file
func (s *Service) DownloadToFile(ctx context.Context, msg whatsmeow.DownloadableMessage, file *os.File) error {
	if s.client == nil {
		return fmt.Errorf("client not initialized")
	}

	if err := s.client.DownloadToFile(ctx, msg, file); err != nil {
		return fmt.Errorf("failed to download to file: %w", err)
	}

	return nil
}

// GetBotJID returns the JID of the bot itself
func (s *Service) GetBotJID() types.JID {
	if s.client == nil || s.client.Store == nil {
		return types.JID{}
	}
	return s.client.Store.LID.ToNonAD()
}

// ReactToMessage sends an emoji reaction to a specific message.
func (s *Service) ReactToMessage(ctx context.Context, chatID types.JID, senderJID types.JID, msgID types.MessageID, emoji string) error {
	if s.client == nil {
		return fmt.Errorf("client not initialized")
	}
	if !s.connected {
		return fmt.Errorf("not connected to WhatsApp")
	}
	reaction := s.client.BuildReaction(chatID, senderJID, msgID, emoji)
	_, err := s.client.SendMessage(ctx, chatID, reaction)
	if err != nil {
		s.logger.Error("Failed to send reaction", "error", err, "chat_id", chatID.String(), "msg_id", msgID)
		return fmt.Errorf("failed to send reaction: %w", err)
	}
	s.logger.Debug("Reaction sent", "chat_id", chatID.String(), "emoji", emoji)
	return nil
}

// SendMentionMessage sends a message that mentions specific users
func (s *Service) SendMentionMessage(ctx context.Context, chatID types.JID, text string, mentionedJIDs []string) error {
	if s.client == nil {
		return fmt.Errorf("client not initialized")
	}
	if !s.connected {
		return fmt.Errorf("not connected to WhatsApp")
	}

	_, err := s.client.SendMessage(ctx, chatID, &waE2E.Message{
		ExtendedTextMessage: &waE2E.ExtendedTextMessage{
			Text: proto.String(text),
			ContextInfo: &waE2E.ContextInfo{
				MentionedJID: mentionedJIDs,
			},
		},
	})
	if err != nil {
		s.logger.Error("Failed to send mention message", "error", err, "chat_id", chatID.String())
		return fmt.Errorf("failed to send mention message: %w", err)
	}

	s.logger.Debug("Mention message sent successfully", "chat_id", chatID.String())
	return nil
}

// DownloadToMemory downloads media from a WhatsApp message directly into memory
func (s *Service) DownloadToMemory(ctx context.Context, msg whatsmeow.DownloadableMessage) ([]byte, error) {
	if s.client == nil {
		return nil, fmt.Errorf("client not initialized")
	}

	data, err := s.client.Download(ctx, msg)
	if err != nil {
		return nil, fmt.Errorf("failed to download to memory: %w", err)
	}

	return data, nil
}

// UploadMedia uploads raw bytes to WhatsApp media servers and returns the upload response
// (which contains the URL, media key, SHA256 hash, and encrypted SHA256 hash needed to
// build a StickerMessage / ImageMessage / etc.).
func (s *Service) UploadMedia(ctx context.Context, data []byte, mediaType whatsmeow.MediaType) (whatsmeow.UploadResponse, error) {
	if s.client == nil {
		return whatsmeow.UploadResponse{}, fmt.Errorf("client not initialized")
	}
	if !s.connected {
		return whatsmeow.UploadResponse{}, fmt.Errorf("not connected to WhatsApp")
	}

	resp, err := s.client.Upload(ctx, data, mediaType)
	if err != nil {
		s.logger.Error("Failed to upload media", "error", err, "type", mediaType)
		return whatsmeow.UploadResponse{}, fmt.Errorf("failed to upload media: %w", err)
	}

	s.logger.Debug("Media uploaded successfully", "url", resp.URL, "type", mediaType)
	return resp, nil
}
