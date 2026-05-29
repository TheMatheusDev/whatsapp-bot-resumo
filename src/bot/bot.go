package bot

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"go.mau.fi/whatsmeow/store/sqlstore"
	waLog "go.mau.fi/whatsmeow/util/log"

	"whatsapp-summarizer/src/ai"
	"whatsapp-summarizer/src/cmd"
	"whatsapp-summarizer/src/config"
	"whatsapp-summarizer/src/database"
	"whatsapp-summarizer/src/logger"
	"whatsapp-summarizer/src/types"
	"whatsapp-summarizer/src/utils"
	"whatsapp-summarizer/src/whatsapp"
)

// Bot is the main application orchestrator
type Bot struct {
	config        *types.Config
	logger        types.Logger
	aiService     types.AIService
	dbService     types.DatabaseService
	whatsappSvc   *whatsapp.Service
	handler       *cmd.Handler
	cache         types.CacheService
	container     *sqlstore.Container
	running                    bool
	botStartTime              time.Time
	stopScheduler             chan struct{}
	stopWeeklyRankingScheduler chan struct{}
}

// New creates a new bot instance with dependency injection
func New() (*Bot, error) {
	l, err := logger.New()
	if err != nil {
		return nil, fmt.Errorf("failed to create logger: %w", err)
	}

	l.Info("Bot starting up...")
	l.Info("Logs will be saved to bot_debug.log")

	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	l.Info("Configuration loaded successfully")

	dbService, err := database.NewService(&cfg.Database, l)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize database service: %w", err)
	}

	aiService, err := ai.NewService(cfg.Gemini.APIKey, cfg.Gemini.Model, cfg.Gemini.ModelBackup, cfg.Gemini.ModelBackup2, cfg.Gemini.ApiLogs, l)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize AI service: %w", err)
	}

	cacheTTL, err := time.ParseDuration(cfg.Bot.CacheTTL)
	if err != nil {
		l.Warn("Invalid cache TTL, using default", "ttl", cfg.Bot.CacheTTL)
		cacheTTL = time.Minute * 10
	}
	cache := utils.NewCache(cacheTTL, l)

	dbLog := waLog.Stdout("Database", "WARN", true)
	container, err := sqlstore.New(context.Background(), "sqlite3",
		fmt.Sprintf("file:%s?_foreign_keys=on", cfg.Database.Path), dbLog)
	if err != nil {
		return nil, fmt.Errorf("failed to create WhatsApp store: %w", err)
	}

	b := &Bot{
		config:       cfg,
		logger:       l,
		aiService:    aiService,
		dbService:    dbService,
		cache:        cache,
		container:    container,
		botStartTime: time.Now(),
	}

	whatsappSvc, err := whatsapp.NewService(container, l, b.handleWhatsAppEvent)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize WhatsApp service: %w", err)
	}
	b.whatsappSvc = whatsappSvc

	handler, err := cmd.NewHandler(cfg, aiService, dbService, whatsappSvc, cache, l, b.botStartTime)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize handler: %w", err)
	}
	b.handler = handler

	return b, nil
}

// handleWhatsAppEvent handles WhatsApp events via the message handler
func (b *Bot) handleWhatsAppEvent(evt interface{}) {
	if b.handler != nil {
		b.handler.HandleEvent(evt)
	}
}

// Start starts the bot
func (b *Bot) Start(ctx context.Context) error {
	if b.running {
		return fmt.Errorf("bot is already running")
	}

	b.logger.Info("Starting WhatsApp Summarizer Bot...")

	if err := b.whatsappSvc.Initialize(ctx); err != nil {
		return fmt.Errorf("failed to initialize WhatsApp client: %w", err)
	}

	if err := b.whatsappSvc.Connect(ctx); err != nil {
		return fmt.Errorf("failed to connect to WhatsApp: %w", err)
	}

	b.running = true
	b.botStartTime = time.Now()
	b.logger.Info("Bot started successfully", "start_time", b.botStartTime)

	b.stopScheduler = make(chan struct{})
	go b.startDailySummaryScheduler()

	b.stopWeeklyRankingScheduler = make(chan struct{})
	go b.startWeeklyRankingScheduler()

	return nil
}

// startDailySummaryScheduler runs a goroutine that fires the automatic daily summary at 00:00 every day.
// The list of target groups is resolved dynamically at each tick by merging:
//   - Groups with daily_summary_enabled = 1 in the DB
//   - Groups listed in DailySummaryGroups / GroupWhitelist from .env (retrocompatibility)
func (b *Bot) startDailySummaryScheduler() {
	// Parse timezone from config
	loc, err := time.LoadLocation(b.config.Bot.Timezone)
	if err != nil {
		loc = time.FixedZone(b.config.Bot.Timezone, -3*60*60)
	}

	for {
		now := time.Now().In(loc)
		// Next midnight in the configured timezone
		nextMidnight := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, loc)
		waitDuration := nextMidnight.Sub(now)

		b.logger.Info("DailySummaryScheduler: next run", "in", waitDuration.Round(time.Second).String(), "at", nextMidnight.Format("2006-01-02 15:04:05"))

		select {
		case <-time.After(waitDuration):
			// Resolve groups at firing time so toggles take effect without restart.
			dbGroups, err := b.dbService.GetGroupIDsWithDailySummaryEnabled()
			if err != nil {
				b.logger.Error("DailySummaryScheduler: failed to query DB groups", "error", err)
			}
			// Merge with .env config groups (retrocompatibility for pre-DB deployments).
			configGroups := b.config.Bot.DailySummaryGroups
			if len(configGroups) == 0 {
				configGroups = b.config.WhatsApp.GroupWhitelist
			}
			groups := mergeUnique(configGroups, dbGroups)
			b.logger.Info("DailySummaryScheduler: firing daily summaries", "groups", len(groups))
			for _, jid := range groups {
				b.handler.RunAutoDailySummary(jid)
			}
		case <-b.stopScheduler:
			b.logger.Info("DailySummaryScheduler: stopped")
			return
		}
	}
}

// startWeeklyRankingScheduler fires the weekly message ranking every Monday at 12:00 local time.
// It covers messages from the previous Monday 00:00:00 through Sunday 23:59:59.
// The list of target groups is resolved dynamically at each tick by merging:
//   - Groups with weekly_ranking_enabled = 1 in the DB
//   - Groups listed in GroupWhitelist from .env (retrocompatibility)
func (b *Bot) startWeeklyRankingScheduler() {
	loc, err := time.LoadLocation(b.config.Bot.Timezone)
	if err != nil {
		loc = time.FixedZone(b.config.Bot.Timezone, -3*60*60)
	}

	for {
		now := time.Now().In(loc)

		// Calculate next Monday at 12:00 in the configured timezone.
		// time.Weekday(): Sunday=0, Monday=1, ..., Saturday=6
		daysUntilMonday := (int(time.Monday) - int(now.Weekday()) + 7) % 7

		// Use total minutes to cover the exact 12:00:00 edge case.
		nowMinutes := now.Hour()*60 + now.Minute()
		if daysUntilMonday == 0 && nowMinutes >= 12*60 {
			// Already at or past noon on Monday — wait until next Monday
			daysUntilMonday = 7
		}
		nextMonday := time.Date(now.Year(), now.Month(), now.Day()+daysUntilMonday, 12, 0, 0, 0, loc)
		waitDuration := nextMonday.Sub(now)

		b.logger.Info("WeeklyRankingScheduler: next run",
			"in", waitDuration.Round(time.Second).String(),
			"at", nextMonday.Format("2006-01-02 15:04:05"))

		select {
		case <-time.After(waitDuration):
			// Resolve groups at firing time so toggles take effect without restart.
			dbGroups, err := b.dbService.GetGroupIDsWithWeeklyRankingEnabled()
			if err != nil {
				b.logger.Error("WeeklyRankingScheduler: failed to query DB groups", "error", err)
			}
			groups := mergeUnique(b.config.WhatsApp.GroupWhitelist, dbGroups)
			b.logger.Info("WeeklyRankingScheduler: firing weekly rankings", "groups", len(groups))
			for _, jid := range groups {
				b.handler.RunAutoWeeklyRanking(jid)
			}
			// Sleep before recalculating to ensure 'now' is past noon on the next
			// iteration, acting as a second guard against the infinite-loop race.
			select {
			case <-time.After(2 * time.Minute):
			case <-b.stopWeeklyRankingScheduler:
				b.logger.Info("WeeklyRankingScheduler: stopped")
				return
			}
		case <-b.stopWeeklyRankingScheduler:
			b.logger.Info("WeeklyRankingScheduler: stopped")
			return
		}
	}
}

// mergeUnique merges two string slices into one, deduplicating entries.
// The .env config values are the base; DB values are appended if not already present.
func mergeUnique(base, extra []string) []string {
	seen := make(map[string]bool, len(base)+len(extra))
	result := make([]string, 0, len(base)+len(extra))
	for _, v := range base {
		v = strings.TrimSpace(v)
		if v != "" && !seen[v] {
			seen[v] = true
			result = append(result, v)
		}
	}
	for _, v := range extra {
		v = strings.TrimSpace(v)
		if v != "" && !seen[v] {
			seen[v] = true
			result = append(result, v)
		}
	}
	return result
}

// Stop stops the bot
func (b *Bot) Stop() error {
	if !b.running {
		return nil
	}

	b.logger.Info("Stopping bot...")

	if b.stopScheduler != nil {
		close(b.stopScheduler)
	}

	if b.stopWeeklyRankingScheduler != nil {
		close(b.stopWeeklyRankingScheduler)
	}

	if b.whatsappSvc != nil {
		b.whatsappSvc.Disconnect()
	}

	if b.aiService != nil {
		if err := b.aiService.Close(); err != nil {
			b.logger.Error("Failed to close AI service", "error", err)
		}
	}

	if b.dbService != nil {
		if err := b.dbService.Close(); err != nil {
			b.logger.Error("Failed to close database service", "error", err)
		}
	}

	if b.cache != nil {
		b.cache.Clear()
	}

	if simpleLogger, ok := b.logger.(*logger.SimpleLogger); ok {
		if err := simpleLogger.Close(); err != nil {
			log.Printf("Failed to close logger: %v", err)
		}
	}

	b.running = false
	b.logger.Info("Bot stopped successfully")
	return nil
}

// IsRunning returns whether the bot is running
func (b *Bot) IsRunning() bool {
	return b.running
}
