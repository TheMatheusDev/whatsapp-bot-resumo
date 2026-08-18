package cmd

import (
	"strconv"
	"strings"
	"testing"
)

func TestNumericCommandAliasParsing(t *testing.T) {
	tests := []struct {
		input        string
		isNumeric    bool
		expectedArgs []string
	}{
		{
			input:        "!50",
			isNumeric:    true,
			expectedArgs: []string{"50"},
		},
		{
			input:        "!100 --clt",
			isNumeric:    true,
			expectedArgs: []string{"100", "--clt"},
		},
		{
			input:        "!5000 -l --farialimer",
			isNumeric:    true,
			expectedArgs: []string{"5000", "-l", "--farialimer"},
		},
		{
			input:     "!resuma 50",
			isNumeric: false,
		},
		{
			input:     "!r 50",
			isNumeric: false,
		},
		{
			input:     "!p 50 pergunta",
			isNumeric: false,
		},
		{
			input:     "!abc",
			isNumeric: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			parts := strings.Fields(tt.input)
			if len(parts) == 0 {
				t.Fatalf("empty parts for input %s", tt.input)
			}
			command := strings.ToLower(parts[0])

			isNumCmd := strings.HasPrefix(command, "!") && len(command) > 1
			if isNumCmd {
				_, err := strconv.Atoi(command[1:])
				isNumCmd = (err == nil)
			}

			if isNumCmd != tt.isNumeric {
				t.Errorf("expected isNumeric=%v, got %v", tt.isNumeric, isNumCmd)
			}

			if isNumCmd {
				args := append([]string{command[1:]}, parts[1:]...)
				if len(args) != len(tt.expectedArgs) {
					t.Fatalf("expected args len %d, got %d (%v)", len(tt.expectedArgs), len(args), args)
				}
				for i := range args {
					if args[i] != tt.expectedArgs[i] {
						t.Errorf("expected arg[%d]=%s, got %s", i, tt.expectedArgs[i], args[i])
					}
				}
			}
		})
	}
}
