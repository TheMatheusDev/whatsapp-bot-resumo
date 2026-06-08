package types

import (
	"context"
	"os"
	"time"

	"go.mau.fi/whatsmeow"
	waE2E "go.mau.fi/whatsmeow/proto/waE2E"
	watypes "go.mau.fi/whatsmeow/types"
)

// Contact represents a known WhatsApp contact stored in the contacts table.
type Contact struct {
	LID       string    `json:"lid" db:"lid"`
	Name      string    `json:"name" db:"name"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}

// Chat represents a WhatsApp conversation (group or DM) stored in the chats table.
type Chat struct {
	ChatID    string    `json:"chat_id" db:"chat_id"`
	ChatType  string    `json:"chat_type" db:"chat_type"` // "group" or "direct"
	CreatedAt time.Time `json:"created_at" db:"created_at"`
}

// WelcomeMessage represents a single welcome message template for a group.
type WelcomeMessage struct {
	ID      int64  `json:"id" db:"id"`
	ChatID  string `json:"chat_id" db:"chat_id"`
	Message string `json:"message" db:"message"`
}

// FarewellMessage represents a single farewell message template for a group.
type FarewellMessage struct {
	ID      int64  `json:"id" db:"id"`
	ChatID  string `json:"chat_id" db:"chat_id"`
	Message string `json:"message" db:"message"`
}

// Message represents a WhatsApp message (type used for database operations)
type Message struct {
	ID          int       `json:"id" db:"id"`
	ChatID      string    `json:"chat_id" db:"chat_id"`
	SenderLID   string    `json:"sender_lid" db:"sender_lid"`
	Sender      string    `json:"sender" db:"sender"` // display name, populated via JOIN
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

// GroupSettings holds per-group dynamic configuration stored in the database.
// When a field is zero/empty, callers should fall back to the global config defaults.
// WelcomeMessages and FarewellMessages are populated from their dedicated tables
// and are read-only within GroupSettings — use Add/Delete methods to modify them.
type GroupSettings struct {
	ChatID               string   `json:"chat_id"`
	Rules                string   `json:"rules"`
	WelcomeMessages      []string `json:"welcome_messages"`  // from welcome_messages table
	FarewellMessages     []string `json:"farewell_messages"` // from farewell_messages table
	DailySummaryEnabled  bool     `json:"daily_summary_enabled"`
	WeeklyRankingEnabled bool     `json:"weekly_ranking_enabled"`
	UpdatedAt            int64    `json:"updated_at"`
	UpdatedBy            string   `json:"updated_by"`
}

// OnRetryFunc is called by SummarizeMessages before each fallback attempt
// (attempt is 1-based, so the first retry is attempt=2). Callers can use
// this to update a loading message in WhatsApp. Passing nil is safe.
type OnRetryFunc func(attempt int, model string)

// AIService defines the interface for AI operations
type AIService interface {
	SummarizeMessages(ctx context.Context, messages []Message, opts SummarizeOptions, onRetry OnRetryFunc) (string, error)
	TranscribeAudio(ctx context.Context, audioData []byte, mimeType string) (string, error)
	Close() error
}


// DatabaseService defines the interface for database operations
type DatabaseService interface {
	// Contact / chat upserts (called on every incoming message)
	UpsertContact(contact Contact) error
	UpsertChat(chat Chat) error

	// Message operations
	SaveGroupMessageReturningID(msg Message, groupName string) (int64, error)
	UpdateMessageContent(id int64, newContent string) error
	GetGroupMessages(chatID string, count int) ([]Message, error)
	GetMessagesBetween(chatID string, from, to time.Time) ([]Message, error)
	GetAllGroups() ([]GroupSummary, error)
	// SetBotLID registers the bot's own sender LID so query filters can use
	// sender_lid != botLID instead of a LIKE scan on contacts.name.
	SetBotLID(lid string)

	// Group settings operations
	GetGroupSettings(chatID string) (*GroupSettings, error)
	UpsertGroupSettings(settings GroupSettings) error
	GetGroupIDsWithDailySummaryEnabled() ([]string, error)
	GetGroupIDsWithWeeklyRankingEnabled() ([]string, error)

	// Welcome message operations
	AddWelcomeMessage(chatID, message string) error
	DeleteWelcomeMessage(id int64) error
	GetWelcomeMessages(chatID string) ([]WelcomeMessage, error)

	// Farewell message operations
	AddFarewellMessage(chatID, message string) error
	DeleteFarewellMessage(id int64) error
	GetFarewellMessages(chatID string) ([]FarewellMessage, error)

	// Connection management
	Close() error
	Ping() error
}

// WhatsAppService defines the interface for WhatsApp operations
type WhatsAppService interface {
	SendMessage(chatID watypes.JID, message string) error
	SendMessageReply(chatID watypes.JID, senderJID watypes.JID, replyTo watypes.MessageID, message string) error
	EditMessage(chatID watypes.JID, messageID watypes.MessageID, newContent string) error
	SendRawMessage(ctx context.Context, chatID watypes.JID, msg *waE2E.Message) (whatsmeow.SendResponse, error)
	ReactToMessage(ctx context.Context, chatID watypes.JID, senderJID watypes.JID, msgID watypes.MessageID, emoji string) error
	GetGroupInfo(ctx context.Context, chatID watypes.JID) (*watypes.GroupInfo, error)
	DownloadToFile(ctx context.Context, msg whatsmeow.DownloadableMessage, file *os.File) error
	DownloadToMemory(ctx context.Context, msg whatsmeow.DownloadableMessage) ([]byte, error)
	GetBotJID() watypes.JID
	SendMentionMessage(ctx context.Context, chatID watypes.JID, text string, mentionedJIDs []string) error
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
	ApiLogs      bool   `json:"api_logs"`
}

// DatabaseConfig holds database configuration
type DatabaseConfig struct {
	Path            string `json:"path"`           // bot.db — application data
	WhatsmeowPath   string `json:"whatsmeow_path"`  // whatsmeow.db — managed by the library
	MaxOpenConns    int    `json:"max_open_conns"`
	MaxIdleConns    int    `json:"max_idle_conns"`
	ConnMaxLifetime string `json:"conn_max_lifetime"`
}

// WhatsAppConfig holds WhatsApp configuration
type WhatsAppConfig struct {
	OwnerJID       string   `json:"owner_jid"`
	BotAdmins []string `json:"bot_admins"`
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
	Rules              string              `json:"rules"`
	OnboardingMessage  string              `json:"onboarding_message"`
	MediaDownload      MediaDownloadConfig `json:"media_download"`
}
