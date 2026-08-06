package database

import (
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"whatsapp-summarizer/src/types"
)

// Service implements the DatabaseService interface
type Service struct {
	db            *sql.DB
	logger        types.Logger
	insertMsgStmt *sql.Stmt
	getMsgStmt    *sql.Stmt
	stmtMutex     sync.RWMutex
	botLID        string // sender_lid of the bot itself; used to exclude its own messages from queries
	batchWriter   *MessageBatchWriter
}

// NewService creates a new database service connected to bot.db.
// The whatsmeow database (whatsmeow.db) is managed separately by the library.
func NewService(cfg *types.DatabaseConfig, logger types.Logger) (*Service, error) {
	db, err := sql.Open("sqlite3", fmt.Sprintf("file:%s?_foreign_keys=on&_journal_mode=WAL", cfg.Path))
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Configure connection pool
	db.SetMaxOpenConns(cfg.MaxOpenConns)
	db.SetMaxIdleConns(cfg.MaxIdleConns)

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

	if err := service.initSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	if err := service.migrateMessagesCheckConstraint(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to migrate schema: %w", err)
	}

	if err := service.migrateChatbotEnabled(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to migrate chatbot_enabled column: %w", err)
	}

	if err := service.prepareStatements(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to prepare statements: %w", err)
	}

	// Start the batch writer with production defaults (50 msgs / 30 s).
	service.batchWriter = NewMessageBatchWriter(db, logger, 0, 0)

	logger.Info("Database service initialized successfully", "path", cfg.Path)
	return service, nil
}

// initSchema creates all tables and indexes as specified in database_spec.md.
// Statements are executed in dependency order (no FKs before referenced tables).
func (s *Service) initSchema() error {
	queries := []string{
		// ── 4.1 Enable foreign keys ──────────────────────────────────────────
		`PRAGMA foreign_keys = ON`,

		// ── 4.2 contacts ─────────────────────────────────────────────────────
		`CREATE TABLE IF NOT EXISTS contacts (
			lid        TEXT PRIMARY KEY,
			name       TEXT NOT NULL,
			updated_at INTEGER NOT NULL
			           DEFAULT (CAST(strftime('%s', 'now') AS INTEGER))
		)`,

		// ── 4.3 chats ────────────────────────────────────────────────────────
		`CREATE TABLE IF NOT EXISTS chats (
			chat_id    TEXT PRIMARY KEY,
			chat_type  TEXT NOT NULL DEFAULT 'group'
			           CHECK (chat_type IN ('group', 'direct')),
			created_at INTEGER NOT NULL
			           DEFAULT (CAST(strftime('%s', 'now') AS INTEGER))
		)`,

		// ── 4.4 messages ─────────────────────────────────────────────────────
		`CREATE TABLE IF NOT EXISTS messages (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			chat_id      TEXT    NOT NULL,
			sender_lid   TEXT    NOT NULL,
			message      TEXT,
			message_type TEXT    NOT NULL
			             CHECK (message_type IN ('Conversation','ExtendedText','Audio','Summary','Image','Video','Document')),
			timestamp    INTEGER NOT NULL,
			FOREIGN KEY (chat_id)    REFERENCES chats(chat_id) ON DELETE CASCADE,
			FOREIGN KEY (sender_lid) REFERENCES contacts(lid)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_messages_chat_ts
		    ON messages (chat_id, timestamp)`,
		`CREATE INDEX IF NOT EXISTS idx_messages_sender
		    ON messages (sender_lid)`,

		// ── 4.5 group_configs ────────────────────────────────────────────────────
		// weekly_ranking_enabled and chatbot_enabled are extensions beyond the spec.
		`CREATE TABLE IF NOT EXISTS group_configs (
			chat_id                TEXT    PRIMARY KEY,
			rules                  TEXT,
			daily_summary_enabled  INTEGER NOT NULL DEFAULT 0
			                       CHECK (daily_summary_enabled  IN (0, 1)),
			weekly_ranking_enabled INTEGER NOT NULL DEFAULT 0
			                       CHECK (weekly_ranking_enabled IN (0, 1)),
			chatbot_enabled        INTEGER NOT NULL DEFAULT 1
			                       CHECK (chatbot_enabled        IN (0, 1)),
			updated_at             INTEGER NOT NULL
			                       DEFAULT (CAST(strftime('%s', 'now') AS INTEGER)),
			updated_by             TEXT    NOT NULL DEFAULT '',
			FOREIGN KEY (chat_id) REFERENCES chats(chat_id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_gc_daily_enabled
		    ON group_configs (daily_summary_enabled)  WHERE daily_summary_enabled  = 1`,
		`CREATE INDEX IF NOT EXISTS idx_gc_weekly_enabled
		    ON group_configs (weekly_ranking_enabled) WHERE weekly_ranking_enabled = 1`,

		// ── 4.6 welcome_messages ───────────────────────────────────────────────
		`CREATE TABLE IF NOT EXISTS welcome_messages (
			id        INTEGER PRIMARY KEY AUTOINCREMENT,
			chat_id   TEXT    NOT NULL,
			message   TEXT    NOT NULL,
			FOREIGN KEY (chat_id) REFERENCES group_configs(chat_id) ON DELETE CASCADE
		)`,

		// ── 4.7 farewell_messages ───────────────────────────────────────────────
		`CREATE TABLE IF NOT EXISTS farewell_messages (
			id        INTEGER PRIMARY KEY AUTOINCREMENT,
			chat_id   TEXT    NOT NULL,
			message   TEXT    NOT NULL,
			FOREIGN KEY (chat_id) REFERENCES group_configs(chat_id) ON DELETE CASCADE
		)`,
	}

	for _, q := range queries {
		if _, err := s.db.Exec(q); err != nil {
			return fmt.Errorf("schema init failed — query: %s | error: %w", q, err)
		}
	}

	return nil
}

// migrateMessagesCheckConstraint expands the message_type CHECK constraint on an
// existing messages table to include 'Image', 'Video', and 'Document'.
// New databases are created with the correct constraint by initSchema; this
// function is a no-op for them. For existing databases it uses SQLite's
// 12-step table-recreation procedure since ALTER TABLE cannot modify CHECK constraints.
func (s *Service) migrateMessagesCheckConstraint() error {
	// Inspect the current CREATE TABLE statement stored in sqlite_master.
	var tableSql string
	err := s.db.QueryRow(
		`SELECT sql FROM sqlite_master WHERE type='table' AND name='messages'`,
	).Scan(&tableSql)
	if err == sql.ErrNoRows {
		return nil // table not yet created; initSchema will use the correct definition
	}
	if err != nil {
		return fmt.Errorf("migrateMessagesCheckConstraint: read schema: %w", err)
	}

	// Already contains the expanded set — nothing to do.
	if strings.Contains(tableSql, "'Image'") {
		return nil
	}

	s.logger.Info("Migrating messages table: expanding message_type CHECK constraint to include Image, Video, Document")

	// Step 1: disable FK enforcement for the duration of the migration.
	if _, err := s.db.Exec(`PRAGMA foreign_keys = OFF`); err != nil {
		return fmt.Errorf("migrateMessagesCheckConstraint: disable FKs: %w", err)
	}

	tx, err := s.db.Begin()
	if err != nil {
		s.db.Exec(`PRAGMA foreign_keys = ON`) //nolint:errcheck
		return fmt.Errorf("migrateMessagesCheckConstraint: begin tx: %w", err)
	}

	steps := []string{
		// Step 2: create the replacement table with the updated CHECK constraint.
		`CREATE TABLE messages_v2 (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			chat_id      TEXT    NOT NULL,
			sender_lid   TEXT    NOT NULL,
			message      TEXT,
			message_type TEXT    NOT NULL
			             CHECK (message_type IN ('Conversation','ExtendedText','Audio','Summary','Image','Video','Document')),
			timestamp    INTEGER NOT NULL,
			FOREIGN KEY (chat_id)    REFERENCES chats(chat_id) ON DELETE CASCADE,
			FOREIGN KEY (sender_lid) REFERENCES contacts(lid)
		)`,
		// Step 3: copy all existing data.
		`INSERT INTO messages_v2 SELECT * FROM messages`,
		// Step 4: drop the old table (cascades to its indexes).
		`DROP TABLE messages`,
		// Step 5: rename the replacement into place.
		`ALTER TABLE messages_v2 RENAME TO messages`,
		// Step 6: recreate indexes.
		`CREATE INDEX IF NOT EXISTS idx_messages_chat_ts ON messages (chat_id, timestamp)`,
		`CREATE INDEX IF NOT EXISTS idx_messages_sender  ON messages (sender_lid)`,
	}

	for _, step := range steps {
		if _, err := tx.Exec(step); err != nil {
			tx.Rollback()                      //nolint:errcheck
			s.db.Exec(`PRAGMA foreign_keys = ON`) //nolint:errcheck
			return fmt.Errorf("migrateMessagesCheckConstraint: step failed — %s | error: %w", step, err)
		}
	}

	if err := tx.Commit(); err != nil {
		s.db.Exec(`PRAGMA foreign_keys = ON`) //nolint:errcheck
		return fmt.Errorf("migrateMessagesCheckConstraint: commit: %w", err)
	}

	if _, err := s.db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		return fmt.Errorf("migrateMessagesCheckConstraint: re-enable FKs: %w", err)
	}

	s.logger.Info("Messages table migration completed: message_type now accepts Image, Video, Document")
	return nil
}

// migrateChatbotEnabled adds the chatbot_enabled column to an existing
// group_configs table. New databases already have this column via initSchema;
// this function is a no-op for them. For existing databases it uses
// ALTER TABLE ADD COLUMN which is safe and atomic in SQLite.
func (s *Service) migrateChatbotEnabled() error {
	// Attempt to add the column. SQLite returns an error if it already exists;
	// we check for that specific message and treat it as success.
	_, err := s.db.Exec(
		`ALTER TABLE group_configs ADD COLUMN chatbot_enabled INTEGER NOT NULL DEFAULT 1
		 CHECK (chatbot_enabled IN (0, 1))`)
	if err != nil {
		// "duplicate column name" means the column is already present — no action needed.
		if strings.Contains(err.Error(), "duplicate column name") {
			return nil
		}
		// "no such table" means group_configs doesn't exist yet; initSchema will create it correctly.
		if strings.Contains(err.Error(), "no such table") {
			return nil
		}
		return fmt.Errorf("migrateChatbotEnabled: %w", err)
	}
	s.logger.Info("group_configs migration completed: chatbot_enabled column added (default 1)")
	return nil
}

// prepareStatements prepares hot-path SQL statements.

func (s *Service) prepareStatements() error {
	s.stmtMutex.Lock()
	defer s.stmtMutex.Unlock()
	return s.prepareGetMsgStmtLocked()
}

// prepareGetMsgStmtLocked (re)prepares getMsgStmt using the current botLID.
// Must be called with stmtMutex held for writing.
func (s *Service) prepareGetMsgStmtLocked() error {
	var err error

	s.insertMsgStmt, err = s.db.Prepare(
		`INSERT INTO messages (chat_id, sender_lid, message, message_type, timestamp)
		 VALUES (?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("failed to prepare insertMsgStmt: %w", err)
	}

	// JOIN contacts to surface the display name.
	// Bot messages are excluded by comparing sender_lid to the bot's own LID (integer equality,
	// not LIKE), so the B-Tree index on sender_lid can be used without a full-table LIKE scan.
	s.getMsgStmt, err = s.db.Prepare(
		`SELECT m.id, m.chat_id, m.sender_lid, c.name, m.message, m.message_type, m.timestamp
		 FROM messages m
		 JOIN contacts c ON c.lid = m.sender_lid
		 WHERE m.chat_id = ?
		   AND m.sender_lid != ?
		 ORDER BY m.timestamp DESC
		 LIMIT ?`)
	if err != nil {
		return fmt.Errorf("failed to prepare getMsgStmt: %w", err)
	}

	return nil
}

// SetBotLID stores the bot's own sender LID so queries can exclude its own messages
// via sender_lid != botLID instead of a LIKE scan on contacts.name.
// Must be called once, after the WhatsApp client is connected and the LID is known.
func (s *Service) SetBotLID(lid string) {
	s.stmtMutex.Lock()
	defer s.stmtMutex.Unlock()
	s.botLID = lid
	s.logger.Info("Database: bot LID registered for query filtering", "lid", lid)
}

// ── Contact / Chat upserts ───────────────────────────────────────────────────

// UpsertContact inserts or updates a contact record.
// The name and updated_at are refreshed on every call.
func (s *Service) UpsertContact(contact types.Contact) error {
	updatedAt := contact.UpdatedAt.Unix()
	_, err := s.db.Exec(
		`INSERT INTO contacts (lid, name, updated_at) VALUES (?, ?, ?)
		 ON CONFLICT(lid) DO UPDATE SET
		     name       = excluded.name,
		     updated_at = excluded.updated_at`,
		contact.LID, contact.Name, updatedAt,
	)
	if err != nil {
		return fmt.Errorf("UpsertContact: %w", err)
	}
	return nil
}

// UpsertChat records a chat the first time it is seen; subsequent calls are no-ops.
// For group chats, a group_configs row is also created on first sight with both
// daily_summary_enabled and weekly_ranking_enabled set to 1 (opt-out model).
// Admins can toggle these off at any time via !resumodia / !ranking commands.
func (s *Service) UpsertChat(chat types.Chat) error {
	_, err := s.db.Exec(
		`INSERT OR IGNORE INTO chats (chat_id, chat_type) VALUES (?, ?)`,
		chat.ChatID, chat.ChatType,
	)
	if err != nil {
		return fmt.Errorf("UpsertChat (chats): %w", err)
	}

	// For group chats: ensure a group_configs row exists with both toggles ON.
	// INSERT OR IGNORE means existing rows (and their current toggle values) are never overwritten.
	if chat.ChatType == "group" {
		updatedAt := time.Now().Unix()
		_, err = s.db.Exec(
			`INSERT OR IGNORE INTO group_configs
			    (chat_id, daily_summary_enabled, weekly_ranking_enabled, updated_at, updated_by)
			 VALUES (?, 1, 1, ?, '')`,
			chat.ChatID, updatedAt,
		)
		if err != nil {
			return fmt.Errorf("UpsertChat (group_configs): %w", err)
		}
	}

	return nil
}

// ── Message operations ───────────────────────────────────────────────────────

// SaveGroupMessageReturningID saves a message and returns the inserted row ID.
// msg.SenderLID must already exist in the contacts table (call UpsertContact first).
// msg.ChatID must already exist in the chats table (call UpsertChat first).
// Returns 0 without error when the message_type is not in the controlled vocabulary
// (unsupported types are silently discarded per spec §4.4).
func (s *Service) SaveGroupMessageReturningID(msg types.Message, groupName string) (int64, error) {
	s.stmtMutex.RLock()
	stmt := s.insertMsgStmt
	s.stmtMutex.RUnlock()

	if stmt == nil {
		return 0, fmt.Errorf("insertMsgStmt not initialized")
	}

	timestamp := msg.Timestamp.Unix() // int64 Unix epoch, always UTC — no string alloc

	result, err := stmt.Exec(msg.ChatID, msg.SenderLID, msg.Content, msg.MessageType, timestamp)
	if err != nil {
		// CHECK constraint failure on message_type means unsupported type — discard silently.
		if isCheckViolation(err) {
			s.logger.Debug("Discarding message with unsupported type",
				"message_type", msg.MessageType, "group", groupName)
			return 0, nil
		}
		s.logger.Error("Failed to save message", "error", err, "group", groupName)
		return 0, fmt.Errorf("failed to save message: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("failed to get last insert ID: %w", err)
	}

	s.logger.Debug(fmt.Sprintf("Message saved: [ID: %d | Sender: %s | Group: %s]",
		id, msg.SenderLID, groupName))
	return id, nil
}

// EnqueueMessage submits contact, chat, and message writes for the next batch
// flush. resultCh receives exactly one MessageResult once the batch is committed.
// This is the hot path for all incoming WhatsApp messages.
func (s *Service) EnqueueMessage(
	contact types.Contact,
	chat types.Chat,
	msg types.Message,
	groupName string,
	resultCh chan<- types.MessageResult,
) {
	// Bridge types: database.MessageResult → types.MessageResult via a wrapper channel.
	internalCh := make(chan MessageResult, 1)
	s.batchWriter.Enqueue(contact, chat, msg, groupName, internalCh)
	go func() {
		r := <-internalCh
		resultCh <- types.MessageResult{ID: r.ID, Err: r.Err}
	}()
}

// FlushPendingMessages blocks until all buffered message entries have been
// persisted to the database. Call this before any read-heavy command to ensure
// the latest messages are visible to queries.
func (s *Service) FlushPendingMessages() {
	s.batchWriter.FlushSync()
}

// isCheckViolation returns true when the SQLite error is a CHECK constraint failure.
// Used to silently discard messages whose message_type is not in the allowed set.
func isCheckViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "CHECK constraint failed")
}

// UpdateMessageContent updates the text content for a given message ID.
func (s *Service) UpdateMessageContent(id int64, newContent string) error {
	_, err := s.db.Exec("UPDATE messages SET message = ? WHERE id = ?", newContent, id)
	if err != nil {
		s.logger.Error("Failed to update message content", "error", err, "id", id)
		return fmt.Errorf("failed to update message content: %w", err)
	}
	s.logger.Debug("Message content updated", "id", id)
	return nil
}

// GetGroupMessages retrieves the last `count` messages for a chat (bot excluded),
// returned in chronological order (oldest first).
func (s *Service) GetGroupMessages(chatID string, count int) ([]types.Message, error) {
	s.stmtMutex.RLock()
	stmt := s.getMsgStmt
	botLID := s.botLID
	s.stmtMutex.RUnlock()

	if stmt == nil {
		return nil, fmt.Errorf("getMsgStmt not initialized")
	}

	rows, err := stmt.Query(chatID, botLID, count)
	if err != nil {
		s.logger.Error("Failed to query group messages", "error", err, "chat_id", chatID)
		return nil, fmt.Errorf("failed to query group messages: %w", err)
	}
	defer rows.Close()

	messages := make([]types.Message, 0, count)
	for rows.Next() {
		var msg types.Message
		var tsEpoch int64
		if err := rows.Scan(&msg.ID, &msg.ChatID, &msg.SenderLID, &msg.Sender,
			&msg.Content, &msg.MessageType, &tsEpoch); err != nil {
			return nil, fmt.Errorf("failed to scan message row: %w", err)
		}
		msg.Timestamp = time.Unix(tsEpoch, 0).UTC()
		messages = append(messages, msg)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	// Query returns DESC (newest first); reverse to ASC (oldest first) for AI context.
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}

	s.logger.Debug("Retrieved group messages", "chat_id", chatID, "count", len(messages))
	return messages, nil
}

// GetGroupMessagesWithBot retrieves the last `count` messages for a chat,
// including messages sent by the bot itself. This is used to provide full
// conversation context for chatbot responses, so the model can see its own
// previous replies. Messages are returned in chronological order (oldest first).
func (s *Service) GetGroupMessagesWithBot(chatID string, count int) ([]types.Message, error) {
	query := `SELECT m.id, m.chat_id, m.sender_lid, c.name, m.message, m.message_type, m.timestamp
	          FROM messages m
	          JOIN contacts c ON c.lid = m.sender_lid
	          WHERE m.chat_id = ?
	          ORDER BY m.timestamp DESC
	          LIMIT ?`

	rows, err := s.db.Query(query, chatID, count)
	if err != nil {
		s.logger.Error("Failed to query group messages with bot", "error", err, "chat_id", chatID)
		return nil, fmt.Errorf("failed to query group messages with bot: %w", err)
	}
	defer rows.Close()

	messages := make([]types.Message, 0, count)
	for rows.Next() {
		var msg types.Message
		var tsEpoch int64
		if err := rows.Scan(&msg.ID, &msg.ChatID, &msg.SenderLID, &msg.Sender,
			&msg.Content, &msg.MessageType, &tsEpoch); err != nil {
			return nil, fmt.Errorf("failed to scan message row: %w", err)
		}
		msg.Timestamp = time.Unix(tsEpoch, 0).UTC()
		messages = append(messages, msg)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	// Query returns DESC (newest first); reverse to ASC (oldest first) for AI context.
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}

	s.logger.Debug("Retrieved group messages with bot", "chat_id", chatID, "count", len(messages))
	return messages, nil
}



// GetMessagesBetween retrieves non-bot messages within [from, to] for the given chat.
func (s *Service) GetMessagesBetween(chatID string, from, to time.Time) ([]types.Message, error) {
	fromEpoch := from.Unix()
	toEpoch := to.Unix()

	s.stmtMutex.RLock()
	botLID := s.botLID
	s.stmtMutex.RUnlock()

	query := `SELECT m.id, m.chat_id, m.sender_lid, c.name, m.message, m.message_type, m.timestamp
	          FROM messages m
	          JOIN contacts c ON c.lid = m.sender_lid
	          WHERE m.chat_id = ?
	            AND m.sender_lid != ?
	            AND m.timestamp >= ?
	            AND m.timestamp <= ?
	          ORDER BY m.timestamp ASC`

	rows, err := s.db.Query(query, chatID, botLID, fromEpoch, toEpoch)
	if err != nil {
		s.logger.Error("Failed to query messages between times", "error", err,
			"chat_id", chatID, "from", fromEpoch, "to", toEpoch)
		return nil, fmt.Errorf("failed to query messages between times: %w", err)
	}
	defer rows.Close()

	return s.scanMessages(rows)
}

// scanMessages is a helper that scans a result set into a slice of Message.
func (s *Service) scanMessages(rows *sql.Rows) ([]types.Message, error) {
	var messages []types.Message
	for rows.Next() {
		var msg types.Message
		var tsEpoch int64
		if err := rows.Scan(&msg.ID, &msg.ChatID, &msg.SenderLID, &msg.Sender,
			&msg.Content, &msg.MessageType, &tsEpoch); err != nil {
			s.logger.Error("Failed to scan message row", "error", err)
			return nil, fmt.Errorf("failed to scan message row: %w", err)
		}
		msg.Timestamp = time.Unix(tsEpoch, 0).UTC()
		messages = append(messages, msg)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}
	return messages, nil
}

// GetAllGroups returns all chats with a message count (bot messages excluded),
// ordered by message count descending.
func (s *Service) GetAllGroups() ([]types.GroupSummary, error) {
	s.stmtMutex.RLock()
	botLID := s.botLID
	s.stmtMutex.RUnlock()

	query := `
		SELECT ch.chat_id, COUNT(m.id) as message_count
		FROM chats ch
		LEFT JOIN messages m ON m.chat_id = ch.chat_id AND m.sender_lid != ?
		GROUP BY ch.chat_id
		ORDER BY message_count DESC
	`
	rows, err := s.db.Query(query, botLID)
	if err != nil {
		s.logger.Error("Failed to query groups", "error", err)
		return nil, fmt.Errorf("failed to query groups: %w", err)
	}
	defer rows.Close()

	var groups []types.GroupSummary
	for rows.Next() {
		var g types.GroupSummary
		if err := rows.Scan(&g.ChatID, &g.MessageCount); err != nil {
			return nil, fmt.Errorf("failed to scan group row: %w", err)
		}
		groups = append(groups, g)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	s.logger.Debug("Retrieved groups", "count", len(groups))
	return groups, nil
}

// ── Group settings ───────────────────────────────────────────────────────────

// GetGroupSettings fetches the per-group configuration from group_configs.
// Returns (nil, nil) when no record exists — callers should fall back to global defaults.
// WelcomeMessages and FarewellMessages in the returned struct are populated from
// their respective tables.
func (s *Service) GetGroupSettings(chatID string) (*types.GroupSettings, error) {
	row := s.db.QueryRow(
		`SELECT chat_id, rules, daily_summary_enabled, weekly_ranking_enabled,
		        chatbot_enabled, updated_at, updated_by
		 FROM group_configs WHERE chat_id = ?`, chatID)

	var gs types.GroupSettings
	var dailyEnabled, weeklyEnabled, chatbotEnabled int
	var rules sql.NullString

	err := row.Scan(&gs.ChatID, &rules, &dailyEnabled, &weeklyEnabled,
		&chatbotEnabled, &gs.UpdatedAt, &gs.UpdatedBy)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to scan group_configs row: %w", err)
	}

	gs.Rules = rules.String
	gs.DailySummaryEnabled = dailyEnabled != 0
	gs.WeeklyRankingEnabled = weeklyEnabled != 0
	gs.ChatbotEnabled = chatbotEnabled != 0

	// Populate welcome messages
	wmsgs, err := s.GetWelcomeMessages(chatID)
	if err != nil {
		return nil, err
	}
	gs.WelcomeMessages = make([]string, 0, len(wmsgs))
	for _, wm := range wmsgs {
		gs.WelcomeMessages = append(gs.WelcomeMessages, wm.Message)
	}

	// Populate farewell messages
	fmsgs, err := s.GetFarewellMessages(chatID)
	if err != nil {
		return nil, err
	}
	gs.FarewellMessages = make([]string, 0, len(fmsgs))
	for _, fm := range fmsgs {
		gs.FarewellMessages = append(gs.FarewellMessages, fm.Message)
	}

	return &gs, nil
}

// UpsertGroupSettings inserts or updates the per-group configuration row.
// It does NOT touch welcome_messages or farewell_messages — use the dedicated methods.
// A group_configs row requires the chat_id to already exist in chats (FK).
// updated_at is always refreshed to the current UTC time.
func (s *Service) UpsertGroupSettings(settings types.GroupSettings) error {
	dailyEnabled := 0
	if settings.DailySummaryEnabled {
		dailyEnabled = 1
	}
	weeklyEnabled := 0
	if settings.WeeklyRankingEnabled {
		weeklyEnabled = 1
	}
	chatbotEnabled := 1 // default on
	if !settings.ChatbotEnabled {
		chatbotEnabled = 0
	}

	updatedAt := time.Now().Unix()
	updatedBy := settings.UpdatedBy

	_, err := s.db.Exec(
		`INSERT INTO group_configs
		    (chat_id, rules, daily_summary_enabled, weekly_ranking_enabled,
		     chatbot_enabled, updated_at, updated_by)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(chat_id) DO UPDATE SET
		     rules                  = excluded.rules,
		     daily_summary_enabled  = excluded.daily_summary_enabled,
		     weekly_ranking_enabled = excluded.weekly_ranking_enabled,
		     chatbot_enabled        = excluded.chatbot_enabled,
		     updated_at             = excluded.updated_at,
		     updated_by             = excluded.updated_by`,
		settings.ChatID, settings.Rules, dailyEnabled, weeklyEnabled,
		chatbotEnabled, updatedAt, updatedBy,
	)
	if err != nil {
		return fmt.Errorf("failed to upsert group_configs: %w", err)
	}
	return nil
}

// GetGroupIDsWithDailySummaryEnabled returns chat_ids where daily_summary_enabled = 1.
func (s *Service) GetGroupIDsWithDailySummaryEnabled() ([]string, error) {
	return s.queryGroupIDsByFlag("daily_summary_enabled")
}

// GetGroupIDsWithWeeklyRankingEnabled returns chat_ids where weekly_ranking_enabled = 1.
func (s *Service) GetGroupIDsWithWeeklyRankingEnabled() ([]string, error) {
	return s.queryGroupIDsByFlag("weekly_ranking_enabled")
}

func (s *Service) queryGroupIDsByFlag(column string) ([]string, error) {
	var query string
	switch column {
	case "daily_summary_enabled":
		query = `SELECT chat_id FROM group_configs WHERE daily_summary_enabled = 1`
	case "weekly_ranking_enabled":
		query = `SELECT chat_id FROM group_configs WHERE weekly_ranking_enabled = 1`
	default:
		return nil, fmt.Errorf("queryGroupIDsByFlag: unknown column %q", column)
	}

	rows, err := s.db.Query(query)
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

// ── Welcome messages ─────────────────────────────────────────────────────────

// AddWelcomeMessage appends a new welcome message template for a group.
// The group_configs row must exist before calling this (UpsertGroupSettings first).
func (s *Service) AddWelcomeMessage(chatID, message string) error {
	_, err := s.db.Exec(
		`INSERT INTO welcome_messages (chat_id, message) VALUES (?, ?)`,
		chatID, message,
	)
	if err != nil {
		return fmt.Errorf("AddWelcomeMessage: %w", err)
	}
	return nil
}

// DeleteWelcomeMessage hard-deletes the welcome message with the given row ID.
func (s *Service) DeleteWelcomeMessage(id int64) error {
	_, err := s.db.Exec(`DELETE FROM welcome_messages WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("DeleteWelcomeMessage: %w", err)
	}
	return nil
}

// GetWelcomeMessages returns all welcome messages for a group.
func (s *Service) GetWelcomeMessages(chatID string) ([]types.WelcomeMessage, error) {
	rows, err := s.db.Query(
		`SELECT id, chat_id, message
		 FROM welcome_messages
		 WHERE chat_id = ?
		 ORDER BY id ASC`,
		chatID,
	)
	if err != nil {
		return nil, fmt.Errorf("GetWelcomeMessages: %w", err)
	}
	defer rows.Close()

	var msgs []types.WelcomeMessage
	for rows.Next() {
		var m types.WelcomeMessage
		if err := rows.Scan(&m.ID, &m.ChatID, &m.Message); err != nil {
			return nil, fmt.Errorf("GetWelcomeMessages scan: %w", err)
		}
		msgs = append(msgs, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}
	return msgs, nil
}

// ── Farewell messages ─────────────────────────────────────────────────────────

// AddFarewellMessage appends a new farewell message template for a group.
func (s *Service) AddFarewellMessage(chatID, message string) error {
	_, err := s.db.Exec(
		`INSERT INTO farewell_messages (chat_id, message) VALUES (?, ?)`,
		chatID, message,
	)
	if err != nil {
		return fmt.Errorf("AddFarewellMessage: %w", err)
	}
	return nil
}

// DeleteFarewellMessage hard-deletes the farewell message with the given row ID.
func (s *Service) DeleteFarewellMessage(id int64) error {
	_, err := s.db.Exec(`DELETE FROM farewell_messages WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("DeleteFarewellMessage: %w", err)
	}
	return nil
}

// GetFarewellMessages returns all farewell messages for a group.
func (s *Service) GetFarewellMessages(chatID string) ([]types.FarewellMessage, error) {
	rows, err := s.db.Query(
		`SELECT id, chat_id, message
		 FROM farewell_messages
		 WHERE chat_id = ?
		 ORDER BY id ASC`,
		chatID,
	)
	if err != nil {
		return nil, fmt.Errorf("GetFarewellMessages: %w", err)
	}
	defer rows.Close()

	var msgs []types.FarewellMessage
	for rows.Next() {
		var m types.FarewellMessage
		if err := rows.Scan(&m.ID, &m.ChatID, &m.Message); err != nil {
			return nil, fmt.Errorf("GetFarewellMessages scan: %w", err)
		}
		msgs = append(msgs, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}
	return msgs, nil
}

// ── Connection management ────────────────────────────────────────────────────

// Ping checks if the database connection is alive.
func (s *Service) Ping() error {
	return s.db.Ping()
}

// Close closes prepared statements and the database connection.
func (s *Service) Close() error {
	// Stop the batch writer first — drains the buffer and waits for the
	// background goroutine to exit before closing the underlying *sql.DB.
	if s.batchWriter != nil {
		s.batchWriter.Stop()
	}

	s.stmtMutex.Lock()
	defer s.stmtMutex.Unlock()

	if s.insertMsgStmt != nil {
		s.insertMsgStmt.Close()
	}
	if s.getMsgStmt != nil {
		s.getMsgStmt.Close()
	}

	if s.db != nil {
		if err := s.db.Close(); err != nil {
			s.logger.Error("Failed to close database", "error", err)
			return fmt.Errorf("failed to close database: %w", err)
		}
	}

	s.logger.Info("Database service closed successfully")
	return nil
}
