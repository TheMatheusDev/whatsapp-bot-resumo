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
			ApiLogs:      getEnvBool("API_LOGS", false),
		},
		Database: types.DatabaseConfig{
			Path:            getEnv("DB_PATH", "work.db"),
			MaxOpenConns:    getEnvInt("DB_MAX_OPEN_CONNS", 10),
			MaxIdleConns:    getEnvInt("DB_MAX_IDLE_CONNS", 5),
			ConnMaxLifetime: getEnv("DB_CONN_MAX_LIFETIME", "1h"),
		},
		WhatsApp: types.WhatsAppConfig{
			OwnerJID:       getEnv("OWNER_JID", ""),
			EveryoneAdmins: getEnvSlice("EVERYONE_ADMINS", []string{}),
		},
		Bot: types.BotConfig{
			Timezone:           getEnv("TIMEZONE", "GMT-3"),
			CacheTTL:           getEnv("CACHE_TTL", "10m"),
			LogLevel:           getEnv("LOG_LEVEL", "INFO"),
			EnableMetrics:      getEnvBool("ENABLE_METRICS", false),
			WelcomeMessages:    getEnvSlicePipeAllowEmpty("WELCOME_MESSAGES", []string{"Seja bem-vindo(a), {numero}!"}),
			FarewellMessages:   getEnvSlicePipeAllowEmpty("FAREWELL_MESSAGES", []string{"{numero} saiu do grupo."}),
			Rules:              strings.ReplaceAll(getEnvAllowEmpty("GROUP_RULES", ""), `\n`, "\n"),
			OnboardingMessage:  strings.ReplaceAll(getEnvAllowEmpty("BOT_ONBOARDING_MESSAGE", ""), `\n`, "\n"),
			MediaDownload: types.MediaDownloadConfig{
				Image:    getEnvBool("DOWNLOAD_IMAGE", true),
				Video:    getEnvBool("DOWNLOAD_VIDEO", true),
				Audio:    getEnvBool("DOWNLOAD_AUDIO", true),
				Document: getEnvBool("DOWNLOAD_DOCUMENT", true),
				Sticker:  getEnvBool("DOWNLOAD_STICKER", true),
			},
		},
	}

	// Resolve {regras} placeholder in welcome/farewell messages
	rules := config.Bot.Rules
	for i, msg := range config.Bot.WelcomeMessages {
		config.Bot.WelcomeMessages[i] = strings.ReplaceAll(msg, "{regras}", rules)
	}
	for i, msg := range config.Bot.FarewellMessages {
		config.Bot.FarewellMessages[i] = strings.ReplaceAll(msg, "{regras}", rules)
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

// getEnvAllowEmpty returns empty when the variable is explicitly set to empty.
// It falls back only when the variable is not defined.
func getEnvAllowEmpty(key, defaultValue string) string {
	if value, ok := os.LookupEnv(key); ok {
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

// getEnvSlicePipeAllowEmpty parses a pipe-separated ("|") env variable as a slice of strings,
// trimming whitespace from each element and filtering empty entries and replacing literal \n with actual newlines.
// Falls back to defaultValue only when the variable is not defined at all.
func getEnvSlicePipeAllowEmpty(key string, defaultValue []string) []string {
	value, ok := os.LookupEnv(key)
	if !ok {
		return defaultValue
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return []string{}
	}
	parts := strings.Split(value, "|")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			t = strings.ReplaceAll(t, `\n`, "\n")
			result = append(result, t)
		}
	}
	return result
}

// getEnvSlice gets environment variable as slice with default value
func getEnvSlice(key string, defaultValue []string) []string {
	if value := os.Getenv(key); value != "" {
		return strings.Split(value, ",")
	}
	return defaultValue
}
