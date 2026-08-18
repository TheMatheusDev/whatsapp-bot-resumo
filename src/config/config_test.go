package config

import (
	"os"
	"testing"
)

func TestConfigPreservesPlaceholders(t *testing.T) {
	os.Setenv("GEMINI_API_KEY", "test_key")
	os.Setenv("OWNER_JID", "123456")
	os.Setenv("WELCOME_MESSAGES", "Bem-vindo {numero}! Regras:\n{regras}")
	os.Setenv("FAREWELL_MESSAGES", "Tchau {numero}! Regras eram:\n{regras}")
	os.Setenv("GROUP_RULES", "Regra global 1")
	defer func() {
		os.Unsetenv("GEMINI_API_KEY")
		os.Unsetenv("OWNER_JID")
		os.Unsetenv("WELCOME_MESSAGES")
		os.Unsetenv("FAREWELL_MESSAGES")
		os.Unsetenv("GROUP_RULES")
	}()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if len(cfg.Bot.WelcomeMessages) != 1 || cfg.Bot.WelcomeMessages[0] != "Bem-vindo {numero}! Regras:\n{regras}" {
		t.Errorf("Expected WelcomeMessages[0] to retain '{regras}', got: %q", cfg.Bot.WelcomeMessages[0])
	}

	if len(cfg.Bot.FarewellMessages) != 1 || cfg.Bot.FarewellMessages[0] != "Tchau {numero}! Regras eram:\n{regras}" {
		t.Errorf("Expected FarewellMessages[0] to retain '{regras}', got: %q", cfg.Bot.FarewellMessages[0])
	}
}
