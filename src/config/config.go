package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/joho/godotenv"

	"whatsapp-summarizer/src/types"
)

// Load loads configuration from environment variables
func Load() (*types.Config, error) {
	// Try to load .env file from current directory
	if err := godotenv.Load(".env"); err != nil {
		// If .env doesn't exist in current dir, try executable directory
		if execPath, err := os.Executable(); err == nil {
			execDir := filepath.Dir(execPath)
			envPath := filepath.Join(execDir, ".env")
			godotenv.Load(envPath) // Ignore error if file doesn't exist
		}
	}

	config := &types.Config{
		Gemini: types.GeminiConfig{
			APIKey:       getEnv("GEMINI_API_KEY", ""),
			Model:        getEnv("GEMINI_MODEL", "gemini-3.1-pro-preview"),
			ModelBackup:  getEnv("GEMINI_MODEL_BACKUP", "gemini-3-flash-preview"),
			ModelBackup2: getEnv("GEMINI_MODEL_BACKUP2", "gemini-2.5-flash"),
		},
		Database: types.DatabaseConfig{
			Path:            getEnv("DB_PATH", "work.db"),
			MaxOpenConns:    getEnvInt("DB_MAX_OPEN_CONNS", 10),
			MaxIdleConns:    getEnvInt("DB_MAX_IDLE_CONNS", 5),
			ConnMaxLifetime: getEnv("DB_CONN_MAX_LIFETIME", "1h"),
		},
		WhatsApp: types.WhatsAppConfig{
			OwnerJID:       getEnv("OWNER_JID", ""),
			GroupWhitelist: getEnvSlice("GROUP_WHITELIST", []string{}),
			EveryoneAdmins: getEnvSlice("EVERYONE_ADMINS", []string{}),
		},
		Bot: types.BotConfig{
			Timezone:           getEnv("TIMEZONE", "GMT-3"),
			CacheTTL:           getEnv("CACHE_TTL", "10m"),
			LogLevel:           getEnv("LOG_LEVEL", "INFO"),
			EnableMetrics:      getEnvBool("ENABLE_METRICS", false),
			WelcomeMessage:     getEnv("WELCOME_MESSAGE", "Seja bem-vindo(a), @numero!"),
			FarewellMessage:    getEnv("FAREWELL_MESSAGE", "@numero saiu do grupo."),
			DailySummaryGroups: getEnvSlice("DAILY_SUMMARY_GROUPS", []string{}),
			MediaDownload: types.MediaDownloadConfig{
				Image:    getEnvBool("DOWNLOAD_IMAGE", true),
				Video:    getEnvBool("DOWNLOAD_VIDEO", true),
				Audio:    getEnvBool("DOWNLOAD_AUDIO", true),
				Document: getEnvBool("DOWNLOAD_DOCUMENT", true),
				Sticker:  getEnvBool("DOWNLOAD_STICKER", true),
			},
		},
	}

	if err := Validate(config); err != nil {
		return nil, err
	}

	return config, nil
}

// Validate validates the configuration
func Validate(c *types.Config) error {
	if c.Gemini.APIKey == "" {
		return fmt.Errorf("GEMINI_API_KEY is required")
	}

	if c.WhatsApp.OwnerJID == "" {
		return fmt.Errorf("OWNER_JID is required")
	}

	return nil
}

// getEnv gets environment variable with default value
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getEnvInt gets environment variable as int with default value
func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intVal, err := strconv.Atoi(value); err == nil {
			return intVal
		}
	}
	return defaultValue
}

// getEnvBool gets environment variable as bool with default value
func getEnvBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		if boolVal, err := strconv.ParseBool(value); err == nil {
			return boolVal
		}
	}
	return defaultValue
}

// getEnvSlice gets environment variable as slice with default value
func getEnvSlice(key string, defaultValue []string) []string {
	if value := os.Getenv(key); value != "" {
		return strings.Split(value, ",")
	}
	return defaultValue
}
