package types

import (
	"context"
	"os"
	"time"

	"go.mau.fi/whatsmeow"
	waE2E "go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	watypes "go.mau.fi/whatsmeow/types"
)

// Message represents a WhatsApp message (type used for database operations)
type Message struct {
	ID          int       `json:"id" db:"id"`
	ChatID      string    `json:"chat_id" db:"chat_id"`
	Sender      string    `json:"sender" db:"sender"`
	Content     string    `json:"content" db:"message"`
	MessageType string    `json:"message_type" db:"message_type"`
	Timestamp   time.Time `json:"timestamp" db:"timestamp"`
}

// SummarizeOptions represents options for message summarization
type SummarizeOptions struct {
	Count       int    `json:"count"`       // number of messages to summarize
	Style       string `json:"style"`       // short, medium, long
	Media       bool   `json:"media"`       // include media in the summary
	Personality string `json:"personality"` // personality: clt (default), profeta, farialimer, zoomer
	Question    string `json:"question"`    // optional question to answer along with the summary
}

// GroupInfo represents cached group information
type GroupInfo struct {
	Name     string    `json:"name"`
	CachedAt time.Time `json:"cached_at"`
}

// GroupSummary represents a summary of a group's message count
type GroupSummary struct {
	ChatID       string `json:"chat_id"`
	Name         string `json:"name"`
	MessageCount int    `json:"message_count"`
}

// AIService defines the interface for AI operations
type AIService interface {
	SummarizeMessages(ctx context.Context, messages []Message, opts SummarizeOptions) (string, error)
	SummarizeMessagesWithBackup(ctx context.Context, messages []Message, opts SummarizeOptions) (string, error)
	SummarizeMessagesWithBackup2(ctx context.Context, messages []Message, opts SummarizeOptions) (string, error)
	TranscribeAudio(ctx context.Context, audioData []byte, mimeType string) (string, error)
	Close() error
}

// DatabaseService defines the interface for database operations
type DatabaseService interface {
	// Message operations
	SaveGroupMessage(msg Message, groupName string) error
	SaveGroupMessageReturningID(msg Message, groupName string) (int64, error)
	UpdateMessageContent(id int64, newContent string) error
	GetGroupMessages(chatID string, count int) ([]Message, error)
	GetMessagesSinceTime(chatID string, sinceTime time.Time) ([]Message, error)
	GetMessagesBetween(chatID string, from, to time.Time) ([]Message, error)
	GetAllGroups() ([]GroupSummary, error)

	// Connection management
	Close() error
	Ping() error
}

// WhatsAppService defines the interface for WhatsApp operations
type WhatsAppService interface {
	SendMessage(chatID types.JID, message string) error
	SendMessageReply(chatID types.JID, replyTo types.MessageID, message string) error
	EditMessage(chatID types.JID, messageID types.MessageID, newContent string) error
	SendRawMessage(ctx context.Context, chatID types.JID, msg *waE2E.Message) (whatsmeow.SendResponse, error)
	GetGroupInfo(ctx context.Context, chatID types.JID) (*watypes.GroupInfo, error)
	DownloadToFile(ctx context.Context, msg whatsmeow.DownloadableMessage, file *os.File) error
	DownloadToMemory(ctx context.Context, msg whatsmeow.DownloadableMessage) ([]byte, error)
	GetBotJID() types.JID
	SendMentionMessage(ctx context.Context, chatID types.JID, text string, mentionedJIDs []string) error
	Connect(ctx context.Context) error
	Disconnect()
	IsConnected() bool
}

// CacheService defines the interface for caching operations
type CacheService interface {
	GetGroupName(chatID string) (string, bool)
	SetGroupName(chatID, name string)
	Clear()
}

// Logger defines the interface for logging
type Logger interface {
	Debug(msg string, fields ...interface{})
	Info(msg string, fields ...interface{})
	Warn(msg string, fields ...interface{})
	Error(msg string, fields ...interface{})
	Fatal(msg string, fields ...interface{})
}

// Bot represents the main bot structure
type Bot interface {
	Start(ctx context.Context) error
	Stop() error
	IsRunning() bool
}

// Config holds all configuration for the bot
type Config struct {
	Gemini   GeminiConfig   `json:"gemini"`
	Database DatabaseConfig `json:"database"`
	WhatsApp WhatsAppConfig `json:"whatsapp"`
	Bot      BotConfig      `json:"bot"`
}

// GeminiConfig holds Gemini AI configuration
type GeminiConfig struct {
	APIKey       string `json:"api_key"`
	Model        string `json:"model"`
	ModelBackup  string `json:"model_backup"`
	ModelBackup2 string `json:"model_backup2"`
}

// DatabaseConfig holds database configuration
type DatabaseConfig struct {
	Path            string `json:"path"`
	MaxOpenConns    int    `json:"max_open_conns"`
	MaxIdleConns    int    `json:"max_idle_conns"`
	ConnMaxLifetime string `json:"conn_max_lifetime"`
}

// WhatsAppConfig holds WhatsApp configuration
type WhatsAppConfig struct {
	OwnerJID       string   `json:"owner_jid"`
	GroupWhitelist []string `json:"group_whitelist"`
	EveryoneAdmins []string `json:"everyone_admins"`
}

// MediaDownloadConfig holds per-type media download toggles
type MediaDownloadConfig struct {
	Image    bool `json:"image"`
	Video    bool `json:"video"`
	Audio    bool `json:"audio"`
	Document bool `json:"document"`
	Sticker  bool `json:"sticker"`
}

// BotConfig holds bot behavior configuration
type BotConfig struct {
	Timezone           string              `json:"timezone"`
	CacheTTL           string              `json:"cache_ttl"`
	LogLevel           string              `json:"log_level"`
	EnableMetrics      bool                `json:"enable_metrics"`
	WelcomeMessages    []string            `json:"welcome_messages"`
	FarewellMessages   []string            `json:"farewell_messages"`
	DailySummaryGroups []string            `json:"daily_summary_groups"`
	Rules              string              `json:"rules"`
	MediaDownload      MediaDownloadConfig `json:"media_download"`
}
