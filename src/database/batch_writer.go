package database

import (
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"

	"whatsapp-summarizer/src/types"
)

const (
	// defaultBatchSize is the maximum number of pending entries before an
	// automatic flush is triggered, regardless of the timer.
	defaultBatchSize = 50

	// defaultFlushInterval controls how often the background goroutine flushes
	// whatever has accumulated in the buffer since the last flush.
	defaultFlushInterval = 30 * time.Second
)

// MessageResult is the outcome of persisting a single entry inside a batch.
// Callers that need the auto-incremented message ID (e.g. audio transcription)
// receive it through the resultCh channel they supplied when enqueuing.
type MessageResult struct {
	ID  int64
	Err error
}

// pendingEntry groups the three writes that belong to a single incoming message:
// contact upsert, chat upsert, and message insert. resultCh is closed (never nil
// but may be a discard channel) after the entry is persisted.
type pendingEntry struct {
	contact   types.Contact
	chat      types.Chat
	msg       types.Message
	groupName string
	resultCh  chan<- MessageResult
}

// flushRequest is sent on the flushNow channel to request a synchronous flush.
// doneCh is closed by the writer goroutine once the flush is complete.
type flushRequest struct {
	doneCh chan struct{}
}

// MessageBatchWriter accumulates incoming message entries and persists them to
// SQLite in batches, dramatically reducing the number of individual transactions
// (and therefore fsync/WAL overhead) under high message throughput.
//
// Flush is triggered by whichever of the following happens first:
//   - The internal buffer reaches batchSize entries.
//   - The flush interval elapses.
//   - An explicit FlushSync() call is made (e.g. before read-heavy commands).
//   - Stop() is called (drains the buffer before returning).
type MessageBatchWriter struct {
	db     *sql.DB
	logger types.Logger

	inCh     chan pendingEntry
	flushNow chan flushRequest
	stopCh   chan struct{}

	batchSize int
	interval  time.Duration

	// wg tracks the single background goroutine.
	wg sync.WaitGroup
}

// NewMessageBatchWriter creates and starts a MessageBatchWriter.
// batchSize and interval use package-level defaults when ≤ 0 / zero.
func NewMessageBatchWriter(db *sql.DB, logger types.Logger, batchSize int, interval time.Duration) *MessageBatchWriter {
	if batchSize <= 0 {
		batchSize = defaultBatchSize
	}
	if interval <= 0 {
		interval = defaultFlushInterval
	}

	bw := &MessageBatchWriter{
		db:        db,
		logger:    logger,
		inCh:      make(chan pendingEntry, batchSize*2), // generous buffer so Enqueue never blocks
		flushNow:  make(chan flushRequest),
		stopCh:    make(chan struct{}),
		batchSize: batchSize,
		interval:  interval,
	}

	bw.wg.Add(1)
	go bw.loop()

	return bw
}

// Enqueue submits an entry for deferred persistence.
// resultCh receives exactly one MessageResult once the entry's batch is flushed.
// Callers that do not need the result should pass a discardResultCh (a buffered
// channel of size 1 that is never read) to avoid blocking the writer.
// Enqueue is non-blocking as long as the internal channel is not full.
func (bw *MessageBatchWriter) Enqueue(
	contact types.Contact,
	chat types.Chat,
	msg types.Message,
	groupName string,
	resultCh chan<- MessageResult,
) {
	bw.inCh <- pendingEntry{
		contact:   contact,
		chat:      chat,
		msg:       msg,
		groupName: groupName,
		resultCh:  resultCh,
	}
}

// FlushSync blocks until all entries currently in the buffer (and any that
// arrive while the flush is being set up) have been written to the database.
// Use this before any read command that needs up-to-date message data.
func (bw *MessageBatchWriter) FlushSync() {
	req := flushRequest{doneCh: make(chan struct{})}
	bw.flushNow <- req
	<-req.doneCh
}

// Stop signals the background goroutine to flush the remaining buffer and exit.
// It blocks until the drain is complete. Safe to call once.
func (bw *MessageBatchWriter) Stop() {
	close(bw.stopCh)
	bw.wg.Wait()
}

// ── Background goroutine ──────────────────────────────────────────────────────

func (bw *MessageBatchWriter) loop() {
	defer bw.wg.Done()

	ticker := time.NewTicker(bw.interval)
	defer ticker.Stop()

	buffer := make([]pendingEntry, 0, bw.batchSize)

	for {
		select {
		// New entry arrived.
		case entry := <-bw.inCh:
			buffer = append(buffer, entry)
			if len(buffer) >= bw.batchSize {
				bw.flush(buffer)
				buffer = buffer[:0]
			}

		// Timer elapsed — flush whatever we have.
		case <-ticker.C:
			if len(buffer) > 0 {
				bw.flush(buffer)
				buffer = buffer[:0]
			}

		// Explicit synchronous flush requested (e.g. before a read command).
		case req := <-bw.flushNow:
			// Drain any entries that are already waiting in the channel before
			// flushing so the caller sees a fully up-to-date database.
		drain:
			for {
				select {
				case entry := <-bw.inCh:
					buffer = append(buffer, entry)
				default:
					break drain
				}
			}
			if len(buffer) > 0 {
				bw.flush(buffer)
				buffer = buffer[:0]
			}
			close(req.doneCh)

		// Shutdown: drain remaining entries and exit.
		case <-bw.stopCh:
		stopDrain:
			for {
				select {
				case entry := <-bw.inCh:
					buffer = append(buffer, entry)
				default:
					break stopDrain
				}
			}
			if len(buffer) > 0 {
				bw.flush(buffer)
			}
			return
		}
	}
}

// ── Flush implementation ──────────────────────────────────────────────────────

// flush persists all entries in a single SQLite transaction and delivers each
// MessageResult to the caller's resultCh.
func (bw *MessageBatchWriter) flush(entries []pendingEntry) {
	if len(entries) == 0 {
		return
	}

	bw.logger.Debug("MessageBatchWriter: flushing batch", "count", len(entries))

	tx, err := bw.db.Begin()
	if err != nil {
		bw.logger.Error("MessageBatchWriter: failed to begin transaction", "error", err)
		bw.deliverErrors(entries, fmt.Errorf("begin tx: %w", err))
		return
	}

	// ── 1. Upsert contacts ────────────────────────────────────────────────────
	contactStmt, err := tx.Prepare(
		`INSERT INTO contacts (lid, name, updated_at) VALUES (?, ?, ?)
		 ON CONFLICT(lid) DO UPDATE SET
		     name       = excluded.name,
		     updated_at = excluded.updated_at`)
	if err != nil {
		tx.Rollback() //nolint:errcheck
		bw.logger.Error("MessageBatchWriter: failed to prepare contact upsert", "error", err)
		bw.deliverErrors(entries, fmt.Errorf("prepare contact stmt: %w", err))
		return
	}
	defer contactStmt.Close()

	for _, e := range entries {
		if _, err := contactStmt.Exec(e.contact.LID, e.contact.Name, e.contact.UpdatedAt.Unix()); err != nil {
			bw.logger.Warn("MessageBatchWriter: contact upsert failed",
				"lid", e.contact.LID, "error", err)
			// Non-fatal: continue — the message insert will fail on FK if truly missing.
		}
	}

	// ── 2. Upsert chats ───────────────────────────────────────────────────────
	chatStmt, err := tx.Prepare(
		`INSERT OR IGNORE INTO chats (chat_id, chat_type) VALUES (?, ?)`)
	if err != nil {
		tx.Rollback() //nolint:errcheck
		bw.logger.Error("MessageBatchWriter: failed to prepare chat upsert", "error", err)
		bw.deliverErrors(entries, fmt.Errorf("prepare chat stmt: %w", err))
		return
	}
	defer chatStmt.Close()

	gcStmt, err := tx.Prepare(
		`INSERT OR IGNORE INTO group_configs
		    (chat_id, daily_summary_enabled, weekly_ranking_enabled, updated_at, updated_by)
		 VALUES (?, 1, 1, ?, '')`)
	if err != nil {
		tx.Rollback() //nolint:errcheck
		bw.logger.Error("MessageBatchWriter: failed to prepare group_configs upsert", "error", err)
		bw.deliverErrors(entries, fmt.Errorf("prepare group_configs stmt: %w", err))
		return
	}
	defer gcStmt.Close()

	seen := make(map[string]struct{}, len(entries))
	for _, e := range entries {
		if _, already := seen[e.chat.ChatID]; already {
			continue
		}
		seen[e.chat.ChatID] = struct{}{}

		if _, err := chatStmt.Exec(e.chat.ChatID, e.chat.ChatType); err != nil {
			bw.logger.Warn("MessageBatchWriter: chat upsert failed",
				"chat_id", e.chat.ChatID, "error", err)
		}
		if e.chat.ChatType == "group" {
			if _, err := gcStmt.Exec(e.chat.ChatID, time.Now().Unix()); err != nil {
				bw.logger.Warn("MessageBatchWriter: group_configs upsert failed",
					"chat_id", e.chat.ChatID, "error", err)
			}
		}
	}

	// ── 3. Insert messages (batched VALUES clause) ────────────────────────────
	// Build a single INSERT … VALUES (?,?,?,?,?), (?,?,?,?,?), …
	// Entries whose message_type is not in the allowed set are filtered out here
	// (equivalent to the CHECK-constraint discard in the original SaveGroupMessageReturningID).
	validTypes := map[string]bool{
		"Conversation": true, "ExtendedText": true, "Audio": true,
		"Summary": true, "Image": true, "Video": true, "Document": true,
	}

	type insertRow struct {
		idx int // original index in entries[]
		e   pendingEntry
	}
	valid := make([]insertRow, 0, len(entries))
	for i, e := range entries {
		if !validTypes[e.msg.MessageType] {
			bw.logger.Debug("MessageBatchWriter: discarding unsupported message type",
				"type", e.msg.MessageType, "group", e.groupName)
			e.resultCh <- MessageResult{ID: 0, Err: nil}
			continue
		}
		valid = append(valid, insertRow{i, e})
	}

	if len(valid) == 0 {
		if err := tx.Commit(); err != nil {
			bw.logger.Error("MessageBatchWriter: commit failed (no messages)", "error", err)
		}
		return
	}

	// SQLite does not return multiple last-insert IDs from a multi-row INSERT, so
	// we use a prepared single-row INSERT inside a loop — still inside the same
	// transaction, so the overhead is one fsync total for the whole batch.
	msgStmt, err := tx.Prepare(
		`INSERT INTO messages (chat_id, sender_lid, message, message_type, timestamp)
		 VALUES (?, ?, ?, ?, ?)`)
	if err != nil {
		tx.Rollback() //nolint:errcheck
		bw.logger.Error("MessageBatchWriter: failed to prepare message insert", "error", err)
		bw.deliverErrors(entries, fmt.Errorf("prepare message stmt: %w", err))
		return
	}
	defer msgStmt.Close()

	results := make([]MessageResult, len(entries))
	for _, row := range valid {
		e := row.e
		ts := e.msg.Timestamp.Unix()
		res, err := msgStmt.Exec(e.msg.ChatID, e.msg.SenderLID, e.msg.Content, e.msg.MessageType, ts)
		if err != nil {
			if isCheckViolation(err) {
				bw.logger.Debug("MessageBatchWriter: discarding unsupported message type (CHECK)",
					"type", e.msg.MessageType)
				results[row.idx] = MessageResult{ID: 0, Err: nil}
				continue
			}
			bw.logger.Error("MessageBatchWriter: message insert failed",
				"error", err, "group", e.groupName)
			results[row.idx] = MessageResult{ID: 0, Err: fmt.Errorf("insert message: %w", err)}
			continue
		}
		id, _ := res.LastInsertId()
		results[row.idx] = MessageResult{ID: id, Err: nil}
		bw.logger.Debug("MessageBatchWriter: message inserted",
			"id", id, "sender", e.msg.SenderLID, "group", e.groupName)
	}

	if err := tx.Commit(); err != nil {
		bw.logger.Error("MessageBatchWriter: commit failed", "error", err)
		bw.deliverErrors(entries, fmt.Errorf("commit: %w", err))
		return
	}

	bw.logger.Debug("MessageBatchWriter: batch committed", "count", len(valid))

	// Deliver results to callers.
	for i, e := range entries {
		e.resultCh <- results[i]
	}
}

// deliverErrors sends an error result to every entry's resultCh.
// Used when the whole transaction fails so callers are not blocked forever.
func (bw *MessageBatchWriter) deliverErrors(entries []pendingEntry, err error) {
	for _, e := range entries {
		e.resultCh <- MessageResult{ID: 0, Err: err}
	}
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// buildPlaceholders returns n repetitions of "(?,?,?,?,?)" joined by commas.
// Kept for potential future use with a true multi-row INSERT.
func buildPlaceholders(n int) string {
	if n == 0 {
		return ""
	}
	placeholder := "(?,?,?,?,?)"
	placeholders := make([]string, n)
	for i := range placeholders {
		placeholders[i] = placeholder
	}
	return strings.Join(placeholders, ", ")
}
