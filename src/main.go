package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.mau.fi/whatsmeow/store/sqlstore"
	waLog "go.mau.fi/whatsmeow/util/log"

	"whatsapp-summarizer/src/ai"
	"whatsapp-summarizer/src/cmd"
	"whatsapp-summarizer/src/config"
	"whatsapp-summarizer/src/database"
	"whatsapp-summarizer/src/types"
	"whatsapp-summarizer/src/utils"
	"whatsapp-summarizer/src/whatsapp"
)

// SimpleLogger implements types.Logger interface with file and console output
type SimpleLogger struct {
	logger  *log.Logger
	logFile *os.File
}

// NewSimpleLogger creates a new logger that writes to both console and file
func NewSimpleLogger() (*SimpleLogger, error) {
	// Create or open log file
	logFile, err := os.OpenFile("bot_debug.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file: %w", err)
	}

	// Create multi-writer for both console and file
	multiWriter := io.MultiWriter(os.Stdout, logFile)

	logger := log.New(multiWriter, "", log.LstdFlags)

	return &SimpleLogger{
		logger:  logger,
		logFile: logFile,
	}, nil
}

func (l *SimpleLogger) Debug(msg string, fields ...interface{}) {
	l.logger.Printf("[DEBUG] %s %v", msg, fields)
}

func (l *SimpleLogger) Info(msg string, fields ...interface{}) {
	l.logger.Printf("[INFO] %s %v", msg, fields)
}

func (l *SimpleLogger) Warn(msg string, fields ...interface{}) {
	l.logger.Printf("[WARN] %s %v", msg, fields)
}

func (l *SimpleLogger) Error(msg string, fields ...interface{}) {
	l.logger.Printf("[ERROR] %s %v", msg, fields)
}

func (l *SimpleLogger) Fatal(msg string, fields ...interface{}) {
	l.logger.Fatalf("[FATAL] %s %v", msg, fields)
}

func (l *SimpleLogger) Close() error {
	if l.logFile != nil {
		return l.logFile.Close()
	}
	return nil
}

// Bot implements the types.Bot interface
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

// NewBot creates a new bot instance with dependency injection
func NewBot() (*Bot, error) {
	// Create logger with file output
	logger, err := NewSimpleLogger()
	if err != nil {
		return nil, fmt.Errorf("failed to create logger: %w", err)
	}

	logger.Info("Bot starting up...")
	logger.Info("Logs will be saved to bot_debug.log")

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	logger.Info("Configuration loaded successfully")

	// Initialize database service
	dbService, err := database.NewService(&cfg.Database, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize database service: %w", err)
	}

	// Initialize AI service
	aiService, err := ai.NewService(cfg.Gemini.APIKey, cfg.Gemini.Model, cfg.Gemini.ModelBackup, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize AI service: %w", err)
	}

	// Initialize cache service
	cacheTTL, err := time.ParseDuration(cfg.Bot.CacheTTL)
	if err != nil {
		logger.Warn("Invalid cache TTL, using default", "ttl", cfg.Bot.CacheTTL)
		cacheTTL = time.Minute * 10
	}
	cache := utils.NewCache(cacheTTL, logger)

	// Initialize WhatsApp container
	dbLog := waLog.Stdout("Database", "WARN", true)
	container, err := sqlstore.New(context.Background(), "sqlite3",
		fmt.Sprintf("file:%s?_foreign_keys=on", cfg.Database.Path), dbLog)
	if err != nil {
		return nil, fmt.Errorf("failed to create WhatsApp store: %w", err)
	}

	bot := &Bot{
		config:       cfg,
		logger:       logger,
		aiService:    aiService,
		dbService:    dbService,
		cache:        cache,
		container:    container,
		botStartTime: time.Now(),
	}

	// Initialize WhatsApp service first (before handler)
	whatsappSvc, err := whatsapp.NewService(container, logger, bot.handleWhatsAppEvent)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize WhatsApp service: %w", err)
	}
	bot.whatsappSvc = whatsappSvc

	// Initialize handler with WhatsApp service
	handler, err := cmd.NewHandler(cfg, aiService, dbService, whatsappSvc, cache, logger, bot.botStartTime)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize handler: %w", err)
	}
	bot.handler = handler

	return bot, nil
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

	// Initialize WhatsApp client
	if err := b.whatsappSvc.Initialize(ctx); err != nil {
		return fmt.Errorf("failed to initialize WhatsApp client: %w", err)
	}

	// Connect to WhatsApp
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

	// Disconnect WhatsApp
	if b.whatsappSvc != nil {
		b.whatsappSvc.Disconnect()
	}

	// Close AI service
	if b.aiService != nil {
		if err := b.aiService.Close(); err != nil {
			b.logger.Error("Failed to close AI service", "error", err)
		}
	}

	// Close database service
	if b.dbService != nil {
		if err := b.dbService.Close(); err != nil {
			b.logger.Error("Failed to close database service", "error", err)
		}
	}

	// Clear cache
	if b.cache != nil {
		b.cache.Clear()
	}

	// Close logger file
	if simpleLogger, ok := b.logger.(*SimpleLogger); ok {
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

func main() {
	// Create bot instance
	bot, err := NewBot()
	if err != nil {
		log.Fatalf("Failed to create bot: %v", err)
	}

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Setup signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start bot
	if err := bot.Start(ctx); err != nil {
		log.Fatalf("Failed to start bot: %v", err)
	}

	// Wait for shutdown signal
	sig := <-sigChan
	log.Printf("Received signal: %v, shutting down...", sig)

	// Stop bot
	if err := bot.Stop(); err != nil {
		log.Printf("Error stopping bot: %v", err)
	}

	log.Println("Bot shutdown complete")
}
