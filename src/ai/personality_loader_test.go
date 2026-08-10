package ai

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPersonalityLoader_FallbackLengths(t *testing.T) {
	// Create a temp directory with a test personality file omitting [lengths]
	tmpDir, err := os.MkdirTemp("", "personalities_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tomlContent := `
[summarize]
prompt = "Test summarize prompt"

[chat]
prompt = "Test chat prompt"
`
	err = os.WriteFile(filepath.Join(tmpDir, "testbot.toml"), []byte(tomlContent), 0644)
	if err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	loader, err := NewPersonalityLoader(tmpDir)
	if err != nil {
		t.Fatalf("failed to create loader: %v", err)
	}

	// Test short fallback
	shortPrompt, err := loader.GetLengthPrompt("testbot", "short")
	if err != nil {
		t.Errorf("unexpected error for short: %v", err)
	}
	if shortPrompt != defaultShortPrompt {
		t.Errorf("expected defaultShortPrompt, got %q", shortPrompt)
	}

	// Test medium fallback
	mediumPrompt, err := loader.GetLengthPrompt("testbot", "medium")
	if err != nil {
		t.Errorf("unexpected error for medium: %v", err)
	}
	if mediumPrompt != defaultMediumPrompt {
		t.Errorf("expected defaultMediumPrompt, got %q", mediumPrompt)
	}

	// Test long fallback
	longPrompt, err := loader.GetLengthPrompt("testbot", "long")
	if err != nil {
		t.Errorf("unexpected error for long: %v", err)
	}
	if longPrompt != defaultLongPrompt {
		t.Errorf("expected defaultLongPrompt, got %q", longPrompt)
	}
}

func TestPersonalityLoader_CustomLengthsOverride(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "personalities_test_custom")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tomlContent := `
[summarize]
prompt = "Test summarize prompt"

[chat]
prompt = "Test chat prompt"

[lengths]
short = "Custom short prompt"
`
	err = os.WriteFile(filepath.Join(tmpDir, "custombot.toml"), []byte(tomlContent), 0644)
	if err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	loader, err := NewPersonalityLoader(tmpDir)
	if err != nil {
		t.Fatalf("failed to create loader: %v", err)
	}

	shortPrompt, err := loader.GetLengthPrompt("custombot", "short")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if shortPrompt != "Custom short prompt" {
		t.Errorf("expected custom prompt, got %q", shortPrompt)
	}

	// Medium omitted in file -> should fallback to defaultMediumPrompt
	mediumPrompt, err := loader.GetLengthPrompt("custombot", "medium")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if mediumPrompt != defaultMediumPrompt {
		t.Errorf("expected defaultMediumPrompt, got %q", mediumPrompt)
	}
}
