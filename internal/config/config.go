package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

// Config holds all configuration for the bot
type Config struct {
	Gemini   GeminiConfig   `json:"gemini"`
	Database DatabaseConfig `json:"database"`
	WhatsApp WhatsAppConfig `json:"whatsapp"`
	Bot      BotConfig      `json:"bot"`
}

// GeminiConfig holds Gemini AI configuration
type GeminiConfig struct {
	APIKey string `json:"api_key"`
	Model  string `json:"model"`
}

// DatabaseConfig holds database configuration
type DatabaseConfig struct {
	Path            string `json:"path"`
	MaxOpenConns    int    `json:"max_open_conns"`
	MaxIdleConns    int    `json:"max_idle_conns"`
	ConnMaxLifetime string `json:"conn_max_lifetime"`
}

// WhatsAppConfig holds WhatsApp configuration
type WhatsAppConfig struct {
	OwnerJID       string   `json:"owner_jid"`
	UserWhitelist  []string `json:"user_whitelist"`
	GroupWhitelist []string `json:"group_whitelist"`
	UserBlacklist  []string `json:"user_blacklist"`
	GroupBlacklist []string `json:"group_blacklist"`
}

// BotConfig holds bot behavior configuration
type BotConfig struct {
	Timezone      string `json:"timezone"`
	CacheTTL      string `json:"cache_ttl"`
	LogLevel      string `json:"log_level"`
	EnableMetrics bool   `json:"enable_metrics"`
}

// Load loads configuration from environment variables
func Load() (*Config, error) {
	// Try to load .env file from current directory
	if err := godotenv.Load(".env"); err != nil {
		// If .env doesn't exist in current dir, try executable directory
		if execPath, err := os.Executable(); err == nil {
			execDir := filepath.Dir(execPath)
			envPath := filepath.Join(execDir, ".env")
			godotenv.Load(envPath) // Ignore error if file doesn't exist
		}
	}

	config := &Config{
		Gemini: GeminiConfig{
			APIKey: getEnv("GEMINI_API_KEY", ""),
			Model:  getEnv("GEMINI_MODEL", "gemini-2.5-flash"),
		},
		Database: DatabaseConfig{
			Path:            getEnv("DB_PATH", "work.db"),
			MaxOpenConns:    getEnvInt("DB_MAX_OPEN_CONNS", 10),
			MaxIdleConns:    getEnvInt("DB_MAX_IDLE_CONNS", 5),
			ConnMaxLifetime: getEnv("DB_CONN_MAX_LIFETIME", "1h"),
		},
		WhatsApp: WhatsAppConfig{
			OwnerJID:       getEnv("OWNER_JID", ""),
			UserWhitelist:  getEnvSlice("USER_WHITELIST", []string{}),
			GroupWhitelist: getEnvSlice("GROUP_WHITELIST", []string{}),
			UserBlacklist:  getEnvSlice("USER_BLACKLIST", []string{}),
			GroupBlacklist: getEnvSlice("GROUP_BLACKLIST", []string{}),
		},
		Bot: BotConfig{
			Timezone:      getEnv("TIMEZONE", "GMT-3"),
			CacheTTL:      getEnv("CACHE_TTL", "10m"),
			LogLevel:      getEnv("LOG_LEVEL", "INFO"),
			EnableMetrics: getEnvBool("ENABLE_METRICS", false),
		},
	}

	if err := config.Validate(); err != nil {
		return nil, err
	}

	return config, nil
}

// Validate validates the configuration
func (c *Config) Validate() error {
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
