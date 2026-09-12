package utils

import (
	"testing"
)

func TestParseSummarizeOptions_Defaults(t *testing.T) {
	style, personality, nonFlags := ParseSummarizeOptions([]string{}, false)
	if style != "short" {
		t.Errorf("expected default style 'short', got '%s'", style)
	}
	if personality != "resumobot" {
		t.Errorf("expected default personality 'resumobot', got '%s'", personality)
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
		{"farialimer", []string{"--farialimer"}, "farialimer"},
		{"farialimer alias", []string{"-fl"}, "farialimer"},
		{"zoomer", []string{"--zoomer"}, "zoomer"},
		{"zoomer alias", []string{"-z"}, "zoomer"},
		{"profeta", []string{"--profeta"}, "profeta"},
		{"profeta alias", []string{"-pft"}, "profeta"},
		{"resumobot", []string{"--resumobot"}, "resumobot"},
		{"resumobot alias", []string{"-rb"}, "resumobot"},
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
	style, personality, _ := ParseSummarizeOptions([]string{"--longo", "--clt"}, false)
	if style != "long" {
		t.Errorf("expected style 'long', got '%s'", style)
	}
	if personality != "clt" {
		t.Errorf("expected personality 'clt', got '%s'", personality)
	}
}

func TestParseSummarizeOptionsToStruct(t *testing.T) {
	opts := ParseSummarizeOptionsToStruct([]string{"--medio", "--clt", "qual", "o", "assunto?"}, 50)
	if opts.Count != 50 {
		t.Errorf("expected count 50, got %d", opts.Count)
	}
	if opts.Style != "medium" {
		t.Errorf("expected style 'medium', got '%s'", opts.Style)
	}
	if opts.Personality != "clt" {
		t.Errorf("expected personality 'clt', got '%s'", opts.Personality)
	}
	if opts.Question != "qual o assunto?" {
		t.Errorf("expected question 'qual o assunto?', got '%s'", opts.Question)
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

func TestParseSummarizeArgs(t *testing.T) {
	tests := []struct {
		name                 string
		args                 []string
		defaultCount         int
		expectedCount        int
		expectedStyle        string
		expectedPersonality  string
		expectedQuestion     string
		expectedExplicitFlag bool
	}{
		{
			name:                 "empty args uses defaultCount and empty question",
			args:                 []string{},
			defaultCount:         300,
			expectedCount:        300,
			expectedStyle:        "short",
			expectedPersonality:  "resumobot",
			expectedQuestion:     "",
			expectedExplicitFlag: false,
		},
		{
			name:                 "flags only uses defaultCount",
			args:                 []string{"--clt", "--longo"},
			defaultCount:         300,
			expectedCount:        300,
			expectedStyle:        "long",
			expectedPersonality:  "clt",
			expectedQuestion:     "",
			expectedExplicitFlag: false,
		},
		{
			name:                 "explicit count only",
			args:                 []string{"50"},
			defaultCount:         300,
			expectedCount:        50,
			expectedStyle:        "short",
			expectedPersonality:  "resumobot",
			expectedQuestion:     "",
			expectedExplicitFlag: true,
		},
		{
			name:                 "explicit count with question",
			args:                 []string{"50", "Quem", "falou", "de", "férias?"},
			defaultCount:         300,
			expectedCount:        50,
			expectedStyle:        "short",
			expectedPersonality:  "resumobot",
			expectedQuestion:     "Quem falou de férias?",
			expectedExplicitFlag: true,
		},
		{
			name:                 "explicit count, question, and trailing flags",
			args:                 []string{"50", "Quem", "falou", "de", "férias?", "--clt", "--longo"},
			defaultCount:         300,
			expectedCount:        50,
			expectedStyle:        "long",
			expectedPersonality:  "clt",
			expectedQuestion:     "Quem falou de férias?",
			expectedExplicitFlag: true,
		},
		{
			name:                 "leading flags, explicit count, and question",
			args:                 []string{"--clt", "50", "Quem", "falou", "de", "férias?"},
			defaultCount:         300,
			expectedCount:        50,
			expectedStyle:        "short",
			expectedPersonality:  "clt",
			expectedQuestion:     "Quem falou de férias?",
			expectedExplicitFlag: true,
		},
		{
			name:                 "interspersed flags, count, and question",
			args:                 []string{"--medio", "100", "Qual", "--fl", "foi", "o", "lucro?"},
			defaultCount:         300,
			expectedCount:        100,
			expectedStyle:        "medium",
			expectedPersonality:  "farialimer",
			expectedQuestion:     "Qual foi o lucro?",
			expectedExplicitFlag: true,
		},
		{
			name:                 "no explicit count uses defaultCount and treats all non-flags as question",
			args:                 []string{"Qual", "foi", "o", "assunto", "principal?"},
			defaultCount:         300,
			expectedCount:        300,
			expectedStyle:        "short",
			expectedPersonality:  "resumobot",
			expectedQuestion:     "Qual foi o assunto principal?",
			expectedExplicitFlag: false,
		},
		{
			name:                 "no explicit count with trailing flags",
			args:                 []string{"Qual", "o", "assunto?", "--clt"},
			defaultCount:         300,
			expectedCount:        300,
			expectedStyle:        "short",
			expectedPersonality:  "clt",
			expectedQuestion:     "Qual o assunto?",
			expectedExplicitFlag: false,
		},
		{
			name:                 "question containing numbers does not treat inner number as count",
			args:                 []string{"Quem", "fez", "2", "gols", "ontem?"},
			defaultCount:         300,
			expectedCount:        300,
			expectedStyle:        "short",
			expectedPersonality:  "resumobot",
			expectedQuestion:     "Quem fez 2 gols ontem?",
			expectedExplicitFlag: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			count, style, personality, question, hasExplicit := ParseSummarizeArgs(tt.args, tt.defaultCount)
			if count != tt.expectedCount {
				t.Errorf("expected count %d, got %d", tt.expectedCount, count)
			}
			if style != tt.expectedStyle {
				t.Errorf("expected style '%s', got '%s'", tt.expectedStyle, style)
			}
			if personality != tt.expectedPersonality {
				t.Errorf("expected personality '%s', got '%s'", tt.expectedPersonality, personality)
			}
			if question != tt.expectedQuestion {
				t.Errorf("expected question '%s', got '%s'", tt.expectedQuestion, question)
			}
			if hasExplicit != tt.expectedExplicitFlag {
				t.Errorf("expected hasExplicitCount %v, got %v", tt.expectedExplicitFlag, hasExplicit)
			}
		})
	}
}
