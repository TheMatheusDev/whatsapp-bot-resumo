package utils

import (
	"testing"
)

func TestParseSummarizeOptions_Defaults(t *testing.T) {
	style, personality, nonFlags := ParseSummarizeOptions([]string{}, false)
	if style != "short" {
		t.Errorf("expected default style 'short', got '%s'", style)
	}
	if personality != "profeta" {
		t.Errorf("expected default personality 'profeta', got '%s'", personality)
	}
	if len(nonFlags) != 0 {
		t.Errorf("expected no non-flag args, got %v", nonFlags)
	}
}

func TestParseSummarizeOptions_StyleFlags(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		expected string
	}{
		{"short explicit", []string{"--curto"}, "short"},
		{"short alias", []string{"-c"}, "short"},
		{"medium", []string{"--medio"}, "medium"},
		{"medium alias", []string{"-m"}, "medium"},
		{"long", []string{"--longo"}, "long"},
		{"long alias", []string{"-l"}, "long"},
		{"last wins", []string{"--curto", "--longo"}, "long"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			style, _, _ := ParseSummarizeOptions(tt.args, false)
			if style != tt.expected {
				t.Errorf("expected style '%s', got '%s'", tt.expected, style)
			}
		})
	}
}

func TestParseSummarizeOptions_PersonalityFlags(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		expected string
	}{
		{"clt", []string{"--clt"}, "clt"},
		{"narrador", []string{"--narrador"}, "narrador"},
		{"narrador alias", []string{"-n"}, "narrador"},
		{"farialimer", []string{"--farialimer"}, "farialimer"},
		{"farialimer alias", []string{"-fl"}, "farialimer"},
		{"noir", []string{"--noir"}, "noir"},
		{"noir detetive alias", []string{"--detetive"}, "noir"},
		{"zoomer", []string{"--zoomer"}, "zoomer"},
		{"zoomer alias", []string{"-z"}, "zoomer"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, personality, _ := ParseSummarizeOptions(tt.args, false)
			if personality != tt.expected {
				t.Errorf("expected personality '%s', got '%s'", tt.expected, personality)
			}
		})
	}
}

func TestParseSummarizeOptions_NonFlagArgs(t *testing.T) {
	_, _, nonFlags := ParseSummarizeOptions([]string{"--clt", "hello", "world"}, true)
	if len(nonFlags) != 2 {
		t.Fatalf("expected 2 non-flag args, got %d", len(nonFlags))
	}
	if nonFlags[0] != "hello" || nonFlags[1] != "world" {
		t.Errorf("expected [hello world], got %v", nonFlags)
	}
}

func TestParseSummarizeOptions_NonFlagArgsDisabled(t *testing.T) {
	_, _, nonFlags := ParseSummarizeOptions([]string{"--clt", "hello", "world"}, false)
	if len(nonFlags) != 0 {
		t.Errorf("expected no non-flag args when disabled, got %v", nonFlags)
	}
}

func TestParseSummarizeOptions_CombinedFlags(t *testing.T) {
	style, personality, _ := ParseSummarizeOptions([]string{"--longo", "--noir"}, false)
	if style != "long" {
		t.Errorf("expected style 'long', got '%s'", style)
	}
	if personality != "noir" {
		t.Errorf("expected personality 'noir', got '%s'", personality)
	}
}

func TestParseSummarizeOptionsToStruct(t *testing.T) {
	opts := ParseSummarizeOptionsToStruct([]string{"--medio", "--clt"}, 50)
	if opts.Count != 50 {
		t.Errorf("expected count 50, got %d", opts.Count)
	}
	if opts.Style != "medium" {
		t.Errorf("expected style 'medium', got '%s'", opts.Style)
	}
	if opts.Personality != "clt" {
		t.Errorf("expected personality 'clt', got '%s'", opts.Personality)
	}
}

func TestParseSummarizeOptions_CaseInsensitive(t *testing.T) {
	style, personality, _ := ParseSummarizeOptions([]string{"--LONGO", "--CLT"}, false)
	if style != "long" {
		t.Errorf("expected style 'long', got '%s'", style)
	}
	if personality != "clt" {
		t.Errorf("expected personality 'clt', got '%s'", personality)
	}
}
