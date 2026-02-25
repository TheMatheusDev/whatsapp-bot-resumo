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
	config       *types.Config
	logger       types.Logger
	aiService    types.AIService
	dbService    types.DatabaseService
	whatsappSvc  *whatsapp.Service
	handler      *cmd.Handler
	cache        types.CacheService
	container    *sqlstore.Container
	running      bool
	botStartTime time.Time
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

	aiService, err := ai.NewService(cfg.Gemini.APIKey, cfg.Gemini.Model, cfg.Gemini.ModelBackup, l)
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
	if b.handler != nil && b.whatsappSvc != nil {
		b.handler.HandleEvent(evt, b.whatsappSvc.GetClient())
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

	return nil
}

// Stop stops the bot
func (b *Bot) Stop() error {
	if !b.running {
		return nil
	}

	b.logger.Info("Stopping bot...")

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
