package types

import (
	"context"
	"time"

	"go.mau.fi/whatsmeow/types"
)

// Message represents a WhatsApp message
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
	Count int    `json:"count"` // number of messages to summarize
	Style string `json:"style"` // short, medium, long
	Media bool   `json:"media"` // include media in the summary
	Clt   bool   `json:"clt"`   // use clt personality instead of ProfetaBOT
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
	Close() error
}

// DatabaseService defines the interface for database operations
type DatabaseService interface {
	// Message operations
	SaveGroupMessage(msg Message, groupName string) error
	SaveDirectMessage(msg Message, groupName string) error
	GetGroupMessages(chatID string, count int) ([]Message, error)
	GetDirectMessages(chatID string, count int) ([]Message, error)
	GetAllGroups() ([]GroupSummary, error)

	// Connection management
	Close() error
	Ping() error
}

// WhatsAppService defines the interface for WhatsApp operations
type WhatsAppService interface {
	SendMessage(ctx context.Context, chatID types.JID, message string) error
	SendEditMessage(ctx context.Context, chatID types.JID, messageID types.MessageID, newContent string) error
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
