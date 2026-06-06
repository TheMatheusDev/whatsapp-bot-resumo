package bot

import (
	"context"
	"fmt"
	"log"
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
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	l, err := logger.New(cfg.Gemini.ApiLogs)
	if err != nil {
		return nil, fmt.Errorf("failed to create logger: %w", err)
	}

	l.Info("Bot starting up...")
	if cfg.Gemini.ApiLogs {
		l.Info("Logs will be saved to bot_debug.log")
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
// The list of target groups is resolved dynamically at each tick by querying groups with daily_summary_enabled = 1 in the DB.
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
			b.logger.Info("DailySummaryScheduler: firing daily summaries", "groups", len(dbGroups))
			for _, jid := range dbGroups {
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
// The list of target groups is resolved dynamically at each tick by querying groups with weekly_ranking_enabled = 1 in the DB.
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
			b.logger.Info("WeeklyRankingScheduler: firing weekly rankings", "groups", len(dbGroups))
			for _, jid := range dbGroups {
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



// Stop performs a phased graceful shutdown:
//  1. Drain handler — stop accepting new WhatsApp events and wait for inflight goroutines.
//  2. Disconnect WhatsApp — send a clean presence-unavailable to the server.
//  3. Stop schedulers — signal daily and weekly goroutines to exit.
//  4. Close resources — DB, sqlstore container, AI service, cache.
//  5. Flush logger — log the final "stopped" line, then close the log file.
//
// An overall 30-second timeout guards against a stuck goroutine hanging the process.
func (b *Bot) Stop() error {
	if !b.running {
		return nil
	}

	b.logger.Info("Stopping bot — initiating graceful shutdown...")

	// Overall shutdown timeout.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Phase 1: stop accepting new events and drain inflight handler goroutines.
	if b.handler != nil {
		b.logger.Info("Waiting for inflight goroutines to finish...")
		done := make(chan struct{})
		go func() {
			b.handler.Shutdown()
			close(done)
		}()
		select {
		case <-done:
			b.logger.Info("Handler drained")
		case <-shutdownCtx.Done():
			b.logger.Warn("Timed out waiting for handler to drain — proceeding")
		}
	}

	// Phase 2: disconnect from WhatsApp gracefully.
	if b.whatsappSvc != nil {
		b.whatsappSvc.Disconnect()
	}

	// Phase 3: stop scheduler goroutines.
	if b.stopScheduler != nil {
		close(b.stopScheduler)
	}
	if b.stopWeeklyRankingScheduler != nil {
		close(b.stopWeeklyRankingScheduler)
	}

	// Phase 4: close database, sqlstore container, AI service, and cache.
	if b.dbService != nil {
		if err := b.dbService.Close(); err != nil {
			b.logger.Error("Failed to close database service", "error", err)
		}
	}

	if b.container != nil {
		if err := b.container.Close(); err != nil {
			b.logger.Error("Failed to close WhatsApp store container", "error", err)
		}
	}

	if b.aiService != nil {
		if err := b.aiService.Close(); err != nil {
			b.logger.Error("Failed to close AI service", "error", err)
		}
	}

	if b.cache != nil {
		b.cache.Clear()
	}

	// Phase 5: flush and close the logger — must be the very last step.
	b.running = false
	b.logger.Info("Bot stopped successfully")
	if simpleLogger, ok := b.logger.(*logger.SimpleLogger); ok {
		if err := simpleLogger.Close(); err != nil {
			log.Printf("Failed to close logger: %v", err)
		}
	}

	return nil
}

// IsRunning returns whether the bot is running
func (b *Bot) IsRunning() bool {
	return b.running
}
