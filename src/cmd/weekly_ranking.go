package cmd

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"go.mau.fi/whatsmeow/types"

	wstypes "whatsapp-summarizer/src/types"
)

// RunAutoWeeklyRanking triggers the weekly ranking broadcast for a given group JID string.
// It is called by the scheduler every Monday at 12:00 and covers messages from the
// previous Monday 00:00:00 through Sunday 23:59:59.
// Accepts both bare numbers ("120363XXX") and full JIDs ("120363XXX@g.us"), trimming spaces.
//
// Only one execution is allowed at a time. If the ranking is already running when this
// function is called (e.g. due to a scheduler race), the call is silently skipped.
func (h *Handler) RunAutoWeeklyRanking(chatJIDStr string) {
	chatJIDStr = strings.TrimSpace(chatJIDStr)
	if chatJIDStr == "" {
		h.logger.Error("WeeklyRanking: empty JID, skipping")
		return
	}

	userPart := chatJIDStr
	if idx := strings.Index(chatJIDStr, "@"); idx >= 0 {
		userPart = chatJIDStr[:idx]
	}

	if userPart == "" {
		h.logger.Error("WeeklyRanking: could not extract user part from JID", "jid", chatJIDStr)
		return
	}

	// Prevent concurrent or back-to-back executions triggered by scheduler races.
	if !h.weeklyRankingRunning.CompareAndSwap(false, true) {
		h.logger.Warn("WeeklyRanking: already running, skipping duplicate dispatch", "jid", chatJIDStr)
		return
	}

	chatJID := types.JID{User: userPart, Server: "g.us"}
	h.logger.Info("WeeklyRanking: queuing", "chat", chatJID.User)
	go func() {
		defer h.weeklyRankingRunning.Store(false)
		h.performWeeklyRanking(chatJID)
	}()
}

// performWeeklyRanking queries messages from the previous Mon 00:00 – Sun 23:59:59,
// builds the ranking, and sends it to the group.
func (h *Handler) performWeeklyRanking(chatJID types.JID) {
	now := time.Now().In(h.timezone)

	// We fire on Monday at 12:00, so "last week" is the 7 days that just ended.
	// Monday of the previous week = today (Monday) minus 7 days, at 00:00:00.
	// Sunday end = today (Monday) at 00:00:00 (exclusive upper bound is fine; we
	// fetch everything >= mondayStart, and the DB stores timestamps up to Sunday 23:59:59).
	mondayStart := time.Date(now.Year(), now.Month(), now.Day()-7, 0, 0, 0, 0, h.timezone)
	// Sunday 23:59:59 = mondayStart + 7 days - 1 second
	sundayEnd := mondayStart.Add(7*24*time.Hour - time.Second)

	messages, err := h.dbService.GetMessagesBetween(chatJID.User, mondayStart, sundayEnd)
	if err != nil {
		h.logger.Error("WeeklyRanking: failed to get messages", "error", err, "chat", chatJID.User)
		return
	}

	if len(messages) == 0 {
		h.logger.Info("WeeklyRanking: no messages this week, skipping", "chat", chatJID.User)
		return
	}

	rankingMsg := buildWeeklyRankingMessage(messages, mondayStart, sundayEnd)

	if err := h.whatsappService.SendMessage(chatJID, rankingMsg); err != nil {
		h.logger.Error("WeeklyRanking: failed to send ranking", "error", err, "chat", chatJID.User)
		return
	}

	h.logger.Info("WeeklyRanking completed",
		"chat_id", chatJID.User,
		"message_count", len(messages),
		"week_start", mondayStart.Format("2006-01-02"),
		"week_end", sundayEnd.Format("2006-01-02"),
	)
}

// buildWeeklyRankingMessage constructs the formatted ranking message string.
func buildWeeklyRankingMessage(messages []wstypes.Message, weekStart, weekEnd time.Time) string {
	// Count messages per sender
	counts := make(map[string]int)
	for _, m := range messages {
		name := m.Sender
		if idx := strings.Index(name, "@"); idx >= 0 {
			name = name[:idx]
		}
		if name != "" {
			counts[name]++
		}
	}

	type kv struct {
		Name  string
		Count int
	}
	sorted := make([]kv, 0, len(counts))
	for name, count := range counts {
		sorted = append(sorted, kv{name, count})
	}
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Count != sorted[j].Count {
			return sorted[i].Count > sorted[j].Count
		}
		return sorted[i].Name < sorted[j].Name
	})

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("🏆 *Ranking semanal de mensagens* (%s a %s):\n",
		weekStart.Format("02/01"), weekEnd.Format("02/01")))

	for i, entry := range sorted {
		sb.WriteString(fmt.Sprintf("   %d. %s — %d msgs\n", i+1, entry.Name, entry.Count))
	}

	sb.WriteString(fmt.Sprintf("\n📊 *Total:* %d mensagens no grupo esta semana.", len(messages)))

	return sb.String()
}
