package database

import (
	"database/sql"
	"fmt"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"whatsapp-summarizer/pkg/types"
	"whatsapp-summarizer/src/config"
)

// Service implements the DatabaseService interface
type Service struct {
	db              *sql.DB
	logger          types.Logger
	insertGroupStmt *sql.Stmt
	getGroupStmt    *sql.Stmt
	stmtMutex       sync.RWMutex
}

// NewService creates a new database service
func NewService(cfg *config.DatabaseConfig, logger types.Logger) (*Service, error) {
	db, err := sql.Open("sqlite3", fmt.Sprintf("%s?cache=shared&mode=rwc&_foreign_keys=on", cfg.Path))
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Configure connection pool
	db.SetMaxOpenConns(cfg.MaxOpenConns)
	db.SetMaxIdleConns(cfg.MaxIdleConns)

	// Parse lifetime duration
	if cfg.ConnMaxLifetime != "" {
		if lifetime, err := time.ParseDuration(cfg.ConnMaxLifetime); err == nil {
			db.SetConnMaxLifetime(lifetime)
		} else {
			logger.Warn("Invalid ConnMaxLifetime, using default", "value", cfg.ConnMaxLifetime)
			db.SetConnMaxLifetime(time.Hour)
		}
	}

	db.SetConnMaxIdleTime(time.Minute * 10)

	service := &Service{
		db:     db,
		logger: logger,
	}

	// Initialize database schema
	if err := service.initSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	// Prepare statements
	if err := service.prepareStatements(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to prepare statements: %w", err)
	}

	logger.Info("Database service initialized successfully", "path", cfg.Path)
	return service, nil
}

// initSchema creates the necessary tables and indexes
func (s *Service) initSchema() error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS group_messages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			chat_id TEXT NOT NULL,
			sender TEXT NOT NULL,
			message TEXT,
			message_type TEXT,
			timestamp DATETIME
		)`,
		`CREATE INDEX IF NOT EXISTS idx_group_messages_chat_timestamp ON group_messages(chat_id, timestamp ASC)`,
	}

	for _, query := range queries {
		if _, err := s.db.Exec(query); err != nil {
			return fmt.Errorf("failed to execute query: %s, error: %w", query, err)
		}
	}

	return nil
}

// prepareStatements prepares all SQL statements
func (s *Service) prepareStatements() error {
	s.stmtMutex.Lock()
	defer s.stmtMutex.Unlock()

	var err error

	s.insertGroupStmt, err = s.db.Prepare(`INSERT INTO group_messages (chat_id, sender, message, message_type, timestamp) VALUES (?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("failed to prepare insertGroupStmt: %w", err)
	}

	// Filter out bot messages when retrieving messages for summarization
	// This excludes all messages sent by the bot itself (ProfetaBOT [VOCÊ])
	// Order by DESC to get the most recent messages first
	s.getGroupStmt, err = s.db.Prepare(`SELECT id, chat_id, sender, message, message_type, timestamp FROM group_messages WHERE chat_id = ? AND sender NOT LIKE 'ProfetaBOT [VOCÊ]%' ORDER BY timestamp DESC LIMIT ?`)
	if err != nil {
		return fmt.Errorf("failed to prepare getGroupStmt: %w", err)
	}

	return nil
}

// SaveGroupMessage saves a group message to the database
func (s *Service) SaveGroupMessage(msg types.Message, groupName string) error {
	s.stmtMutex.RLock()
	stmt := s.insertGroupStmt
	s.stmtMutex.RUnlock()

	if stmt == nil {
		return fmt.Errorf("insertGroupStmt not initialized")
	}

	_, err := stmt.Exec(msg.ChatID, msg.Sender, msg.Content, msg.MessageType, msg.Timestamp)
	if err != nil {
		s.logger.Error("Failed to save group message", "error", err, "group", groupName)
		return fmt.Errorf("failed to save group message: %w", err)
	}

	s.logger.Debug("Message saved", msg.Sender, "|", groupName)
	return nil
}

// GetGroupMessages retrieves group messages from the database
func (s *Service) GetGroupMessages(chatID string, count int) ([]types.Message, error) {
	s.stmtMutex.RLock()
	stmt := s.getGroupStmt
	s.stmtMutex.RUnlock()

	if stmt == nil {
		return nil, fmt.Errorf("getGroupStmt not initialized")
	}

	rows, err := stmt.Query(chatID, count)
	if err != nil {
		s.logger.Error("Failed to query group messages", "error", err, "chat_id", chatID)
		return nil, fmt.Errorf("failed to query group messages: %w", err)
	}
	defer rows.Close()

	messages := make([]types.Message, 0, count)
	for rows.Next() {
		var msg types.Message
		err := rows.Scan(&msg.ID, &msg.ChatID, &msg.Sender, &msg.Content, &msg.MessageType, &msg.Timestamp)
		if err != nil {
			s.logger.Error("Failed to scan message row", "error", err)
			return nil, fmt.Errorf("failed to scan message row: %w", err)
		}
		messages = append(messages, msg)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	// Reverse messages to be in chronological order (oldest to newest)
	// Query returns DESC (newest first), but we want ASC (oldest first) for AI context
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}

	s.logger.Debug("Retrieved group messages (bot messages filtered out)", "chat_id", chatID, "count", len(messages))
	return messages, nil
}

// GetAllGroups retrieves a list of all groups with their message counts
func (s *Service) GetAllGroups() ([]types.GroupSummary, error) {
	query := `
		SELECT chat_id, COUNT(*) as message_count
		FROM group_messages
		WHERE sender NOT LIKE 'ProfetaBOT [VOCÊ]%'
		GROUP BY chat_id
		ORDER BY message_count DESC
	`

	rows, err := s.db.Query(query)
	if err != nil {
		s.logger.Error("Failed to query groups", "error", err)
		return nil, fmt.Errorf("failed to query groups: %w", err)
	}
	defer rows.Close()

	var groups []types.GroupSummary
	for rows.Next() {
		var group types.GroupSummary
		err := rows.Scan(&group.ChatID, &group.MessageCount)
		if err != nil {
			s.logger.Error("Failed to scan group row", "error", err)
			return nil, fmt.Errorf("failed to scan group row: %w", err)
		}
		groups = append(groups, group)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	s.logger.Debug("Retrieved groups", "count", len(groups))
	return groups, nil
}

// Ping checks if the database connection is alive
func (s *Service) Ping() error {
	return s.db.Ping()
}

// Close closes the database connection and prepared statements
func (s *Service) Close() error {
	s.stmtMutex.Lock()
	defer s.stmtMutex.Unlock()

	// Close prepared statements
	if s.insertGroupStmt != nil {
		s.insertGroupStmt.Close()
	}
	if s.getGroupStmt != nil {
		s.getGroupStmt.Close()
	}

	// Close database connection
	if s.db != nil {
		err := s.db.Close()
		if err != nil {
			s.logger.Error("Failed to close database", "error", err)
			return fmt.Errorf("failed to close database: %w", err)
		}
	}

	s.logger.Info("Database service closed successfully")
	return nil
}
