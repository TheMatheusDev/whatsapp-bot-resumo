package database

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"whatsapp-summarizer/src/types"
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
func NewService(cfg *types.DatabaseConfig, logger types.Logger) (*Service, error) {
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
		`CREATE TABLE IF NOT EXISTS group_settings (
			chat_id                TEXT    PRIMARY KEY,
			rules                  TEXT    NOT NULL DEFAULT '',
			welcome_messages       TEXT    NOT NULL DEFAULT '[]',
			farewell_messages      TEXT    NOT NULL DEFAULT '[]',
			daily_summary_enabled  INTEGER NOT NULL DEFAULT 1,
			weekly_ranking_enabled INTEGER NOT NULL DEFAULT 1,
			updated_at             DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
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
	// This excludes all messages sent by the bot itself (ResumoBOT [VOCÊ])
	// Order by DESC to get the most recent messages first
	s.getGroupStmt, err = s.db.Prepare(`SELECT id, chat_id, sender, message, message_type, timestamp FROM group_messages WHERE chat_id = ? AND sender NOT LIKE 'ResumoBOT [VOCÊ]%' ORDER BY timestamp DESC LIMIT ?`)
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

// SaveGroupMessageReturningID saves a group message and returns the inserted row ID
func (s *Service) SaveGroupMessageReturningID(msg types.Message, groupName string) (int64, error) {
	s.stmtMutex.RLock()
	stmt := s.insertGroupStmt
	s.stmtMutex.RUnlock()

	if stmt == nil {
		return 0, fmt.Errorf("insertGroupStmt not initialized")
	}

	result, err := stmt.Exec(msg.ChatID, msg.Sender, msg.Content, msg.MessageType, msg.Timestamp)
	if err != nil {
		s.logger.Error("Failed to save group message", "error", err, "group", groupName)
		return 0, fmt.Errorf("failed to save group message: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		s.logger.Error("Failed to get last insert ID", "error", err)
		return 0, fmt.Errorf("failed to get last insert ID: %w", err)
	}

	s.logger.Debug(fmt.Sprintf("Message saved: [ID: %d | Sender: %s | Group: %s]", id, msg.Sender, groupName))
	return id, nil
}

// UpdateMessageContent updates the message content for a given message ID
func (s *Service) UpdateMessageContent(id int64, newContent string) error {
	_, err := s.db.Exec("UPDATE group_messages SET message = ? WHERE id = ?", newContent, id)
	if err != nil {
		s.logger.Error("Failed to update message content", "error", err, "id", id)
		return fmt.Errorf("failed to update message content: %w", err)
	}

	s.logger.Debug("Message content updated", "id", id)
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

// GetMessagesSinceTime retrieves messages from a specific chat since a given time
func (s *Service) GetMessagesSinceTime(chatID string, sinceTime time.Time) ([]types.Message, error) {
	// Format time to match the database format: 2025-11-23 20:34:35-03:00
	sinceTimeStr := sinceTime.Format("2006-01-02 15:04:05-07:00")

	query := `SELECT id, chat_id, sender, message, message_type, timestamp 
			  FROM group_messages 
			  WHERE chat_id = ? 
			  AND sender NOT LIKE 'ResumoBOT [VOCÊ]%' 
			  AND timestamp >= ? 
			  ORDER BY timestamp ASC`

	rows, err := s.db.Query(query, chatID, sinceTimeStr)
	if err != nil {
		s.logger.Error("Failed to query messages since time", "error", err, "chat_id", chatID, "since_time", sinceTimeStr)
		return nil, fmt.Errorf("failed to query messages since time: %w", err)
	}
	defer rows.Close()

	var messages []types.Message
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

	s.logger.Debug("Retrieved messages since time", "chat_id", chatID, "since_time", sinceTimeStr, "count", len(messages))
	return messages, nil
}

// GetMessagesBetween retrieves messages from a chat within an inclusive [from, to] time window.
// Bot messages (ResumoBOT) are excluded and results are ordered chronologically.
func (s *Service) GetMessagesBetween(chatID string, from, to time.Time) ([]types.Message, error) {
	fromStr := from.Format("2006-01-02 15:04:05-07:00")
	toStr := to.Format("2006-01-02 15:04:05-07:00")

	query := `SELECT id, chat_id, sender, message, message_type, timestamp
			  FROM group_messages
			  WHERE chat_id = ?
			  AND sender NOT LIKE 'ResumoBOT [VOCÊ]%'
			  AND timestamp >= ?
			  AND timestamp <= ?
			  ORDER BY timestamp ASC`

	rows, err := s.db.Query(query, chatID, fromStr, toStr)
	if err != nil {
		s.logger.Error("Failed to query messages between times", "error", err, "chat_id", chatID, "from", fromStr, "to", toStr)
		return nil, fmt.Errorf("failed to query messages between times: %w", err)
	}
	defer rows.Close()

	var messages []types.Message
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

	s.logger.Debug("Retrieved messages between times", "chat_id", chatID, "from", fromStr, "to", toStr, "count", len(messages))
	return messages, nil
}

// GetAllGroups retrieves a list of all groups with their message counts
func (s *Service) GetAllGroups() ([]types.GroupSummary, error) {
	query := `
		SELECT chat_id, COUNT(*) as message_count
		FROM group_messages
		WHERE sender NOT LIKE 'ResumoBOT [VOCÊ]%'
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

// GetGroupSettings retrieves the per-group dynamic configuration from the database.
// Returns (nil, nil) when no record exists for the given chatID — callers should
// fall back to the global config defaults in that case.
func (s *Service) GetGroupSettings(chatID string) (*types.GroupSettings, error) {
	row := s.db.QueryRow(
		`SELECT chat_id, rules, welcome_messages, farewell_messages,
		        daily_summary_enabled, weekly_ranking_enabled
		 FROM group_settings WHERE chat_id = ?`, chatID)

	var gs types.GroupSettings
	var welcomeJSON, farewellJSON string
	var dailyEnabled, weeklyEnabled int

	err := row.Scan(&gs.ChatID, &gs.Rules, &welcomeJSON, &farewellJSON,
		&dailyEnabled, &weeklyEnabled)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to scan group_settings row: %w", err)
	}

	if err := json.Unmarshal([]byte(welcomeJSON), &gs.WelcomeMessages); err != nil {
		gs.WelcomeMessages = []string{}
	}
	if err := json.Unmarshal([]byte(farewellJSON), &gs.FarewellMessages); err != nil {
		gs.FarewellMessages = []string{}
	}
	gs.DailySummaryEnabled = dailyEnabled != 0
	gs.WeeklyRankingEnabled = weeklyEnabled != 0

	return &gs, nil
}

// UpsertGroupSettings inserts or replaces the per-group configuration.
// The updated_at timestamp is refreshed on every upsert.
func (s *Service) UpsertGroupSettings(settings types.GroupSettings) error {
	welcomeJSON, err := json.Marshal(settings.WelcomeMessages)
	if err != nil {
		return fmt.Errorf("failed to marshal welcome_messages: %w", err)
	}
	farewellJSON, err := json.Marshal(settings.FarewellMessages)
	if err != nil {
		return fmt.Errorf("failed to marshal farewell_messages: %w", err)
	}

	dailyEnabled := 0
	if settings.DailySummaryEnabled {
		dailyEnabled = 1
	}
	weeklyEnabled := 0
	if settings.WeeklyRankingEnabled {
		weeklyEnabled = 1
	}

	_, err = s.db.Exec(
		`INSERT INTO group_settings
			(chat_id, rules, welcome_messages, farewell_messages,
			 daily_summary_enabled, weekly_ranking_enabled, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT(chat_id) DO UPDATE SET
			rules                  = excluded.rules,
			welcome_messages       = excluded.welcome_messages,
			farewell_messages      = excluded.farewell_messages,
			daily_summary_enabled  = excluded.daily_summary_enabled,
			weekly_ranking_enabled = excluded.weekly_ranking_enabled,
			updated_at             = excluded.updated_at`,
		settings.ChatID, settings.Rules, string(welcomeJSON), string(farewellJSON),
		dailyEnabled, weeklyEnabled,
	)
	if err != nil {
		return fmt.Errorf("failed to upsert group_settings: %w", err)
	}
	return nil
}

// GetGroupIDsWithDailySummaryEnabled returns chat_ids of all groups that
// have the daily summary feature enabled in the database.
func (s *Service) GetGroupIDsWithDailySummaryEnabled() ([]string, error) {
	return s.queryGroupIDsByFlag("daily_summary_enabled")
}

// GetGroupIDsWithWeeklyRankingEnabled returns chat_ids of all groups that
// have the weekly ranking feature enabled in the database.
func (s *Service) GetGroupIDsWithWeeklyRankingEnabled() ([]string, error) {
	return s.queryGroupIDsByFlag("weekly_ranking_enabled")
}

// queryGroupIDsByFlag is a helper that fetches chat_ids where the given
// boolean column equals 1.
func (s *Service) queryGroupIDsByFlag(column string) ([]string, error) {
	// column is an internal constant — no risk of SQL injection here.
	rows, err := s.db.Query(
		`SELECT chat_id FROM group_settings WHERE `+column+` = 1`)
	if err != nil {
		return nil, fmt.Errorf("failed to query group IDs by flag %q: %w", column, err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("failed to scan chat_id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}
	return ids, nil
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
