package ai

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/BurntSushi/toml"
)

// personalityFile is the structure expected in each <name>.toml file.
type personalityFile struct {
	Summarize struct {
		Prompt string `toml:"prompt"`
	} `toml:"summarize"`
	Chat struct {
		Prompt string `toml:"prompt"`
	} `toml:"chat"`
	Lengths struct {
		Short  string `toml:"short"`
		Medium string `toml:"medium"`
		Long   string `toml:"long"`
	} `toml:"lengths"`
}

// personalityEntry holds the parsed prompts for a single personality.
type personalityEntry struct {
	summarize string
	chat      string
	short     string
	medium    string
	long      string
}

// PersonalityLoader loads personality prompts from TOML files and provides
// thread-safe hot-swap via Reload. Each personality lives in its own file
// (<name>.toml). If a file is missing or invalid the personality is marked
// as unavailable and callers receive an explicit error — the bot continues
// serving the other personalities normally.
type PersonalityLoader struct {
	mu          sync.RWMutex
	personalities map[string]*personalityEntry // nil entry means unavailable
	dir         string
}

// NewPersonalityLoader creates and initialises a PersonalityLoader by loading
// all TOML files from dir. It returns an error only when no files are found
// at all; individual missing/broken files are logged to stderr but do not
// prevent the loader from being returned.
func NewPersonalityLoader(dir string) (*PersonalityLoader, error) {
	pl := &PersonalityLoader{dir: dir}
	if err := pl.load(dir); err != nil {
		return nil, err
	}
	return pl, nil
}

// Reload re-reads all TOML files from the configured directory atomically.
// It returns an error only when no files are found; individual file errors
// are collected and returned as a combined message so the caller can report
// them without aborting the whole reload.
func (pl *PersonalityLoader) Reload() error {
	return pl.load(pl.dir)
}

// load does the actual file I/O. It scans dir for *.toml files, parses each
// one, and replaces the in-memory map under the write lock.
func (pl *PersonalityLoader) load(dir string) error {
	pattern := filepath.Join(dir, "*.toml")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return fmt.Errorf("personality_loader: failed to scan directory %q: %w", dir, err)
	}
	if len(files) == 0 {
		return fmt.Errorf("personality_loader: no .toml files found in %q — check PERSONALITIES_DIR", dir)
	}

	newMap := make(map[string]*personalityEntry, len(files))
	var loadErrs []string

	for _, path := range files {
		name := fileBaseName(path)
		entry, err := parsePersonalityFile(path)
		if err != nil {
			msg := fmt.Sprintf("personality %q: %v", name, err)
			loadErrs = append(loadErrs, msg)
			fmt.Fprintf(os.Stderr, "[personality_loader] WARNING: %s — personality unavailable, check file: %s\n", msg, path)
			newMap[name] = nil // mark as unavailable
			continue
		}
		newMap[name] = entry
	}

	pl.mu.Lock()
	pl.personalities = newMap
	pl.mu.Unlock()

	if len(loadErrs) > 0 {
		return fmt.Errorf("personality_loader: %d file(s) had errors:\n  • %s",
			len(loadErrs), joinStrings(loadErrs, "\n  • "))
	}
	return nil
}

// GetSummarizePersonality returns the summarize prompt for the named
// personality. Returns ("", error) when the personality is unavailable or not found.
func (pl *PersonalityLoader) GetSummarizePersonality(name string) (string, error) {
	pl.mu.RLock()
	defer pl.mu.RUnlock()

	entry, ok := pl.personalities[name]
	if !ok {
		return "", fmt.Errorf("personality %q not found — check file %s.toml", name, name)
	}
	if entry == nil {
		return "", fmt.Errorf("personality %q is misconfigured — check file %s.toml", name, name)
	}
	if entry.summarize == "" {
		return "", fmt.Errorf("personality %q has no summarize prompt — check file %s.toml", name, name)
	}
	return entry.summarize, nil
}

// GetChatPersonality returns the chat prompt for the named personality.
// Returns ("", error) when the personality is unavailable or not found.
func (pl *PersonalityLoader) GetChatPersonality(name string) (string, error) {
	pl.mu.RLock()
	defer pl.mu.RUnlock()

	entry, ok := pl.personalities[name]
	if !ok {
		return "", fmt.Errorf("personality %q not found — check file %s.toml", name, name)
	}
	if entry == nil {
		return "", fmt.Errorf("personality %q is misconfigured — check file %s.toml", name, name)
	}
	if entry.chat == "" {
		return "", fmt.Errorf("personality %q has no chat prompt — check file %s.toml", name, name)
	}
	return entry.chat, nil
}

const (
	defaultShortPrompt  = "O resumo deve ser curto e conter as informações mais importantes das mensagens. Seja breve, sem perder nenhuma informação. Sempre cite quem disse o quê."
	defaultMediumPrompt = "O resumo deve ser de tamanho médio. Deve conter as informações mais importantes das mensagens. Faça um resumo médio, sem perder nenhuma informação. Não o faça muito curto. Não o faça muito longo. O resumo deve ter o comprimento certo. Sempre cite quem disse o quê."
	defaultLongPrompt   = "O resumo deve ser longo, deve conter a maior parte das informações das mensagens. O comprimento não importa, você pode escrever o quanto quiser para fazer o resumo o mais longo possível, contanto que contenha a maior parte das informações das mensagens. Sempre cite quem disse o quê."
)

// GetLengthPrompt returns the length prompt for the given style ("short",
// "medium", "long") from the named personality, falling back to global defaults
// if omitted in the personality file.
func (pl *PersonalityLoader) GetLengthPrompt(name, style string) (string, error) {
	pl.mu.RLock()
	defer pl.mu.RUnlock()

	entry, ok := pl.personalities[name]
	if !ok || entry == nil {
		return "", fmt.Errorf("personality %q is not available for length prompt", name)
	}

	switch style {
	case "short":
		if entry.short != "" {
			return entry.short, nil
		}
		return defaultShortPrompt, nil
	case "medium":
		if entry.medium != "" {
			return entry.medium, nil
		}
		return defaultMediumPrompt, nil
	case "long":
		if entry.long != "" {
			return entry.long, nil
		}
		return defaultLongPrompt, nil
	default:
		if entry.short != "" {
			return entry.short, nil
		}
		return defaultShortPrompt, nil
	}
}

// ListAvailable returns the names of all loaded personalities and whether
// each one is healthy (true) or unavailable due to a load error (false).
func (pl *PersonalityLoader) ListAvailable() map[string]bool {
	pl.mu.RLock()
	defer pl.mu.RUnlock()

	result := make(map[string]bool, len(pl.personalities))
	for name, entry := range pl.personalities {
		result[name] = entry != nil
	}
	return result
}

// parsePersonalityFile reads a TOML file and returns the parsed entry.
func parsePersonalityFile(path string) (*personalityEntry, error) {
	var pf personalityFile
	if _, err := toml.DecodeFile(path, &pf); err != nil {
		return nil, fmt.Errorf("failed to parse TOML file %q: %w", path, err)
	}
	return &personalityEntry{
		summarize: pf.Summarize.Prompt,
		chat:      pf.Chat.Prompt,
		short:     pf.Lengths.Short,
		medium:    pf.Lengths.Medium,
		long:      pf.Lengths.Long,
	}, nil
}

// fileBaseName returns the filename without directory and extension.
func fileBaseName(path string) string {
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	return base[:len(base)-len(ext)]
}

// joinStrings joins a slice of strings with a separator.
func joinStrings(ss []string, sep string) string {
	result := ""
	for i, s := range ss {
		if i > 0 {
			result += sep
		}
		result += s
	}
	return result
}
