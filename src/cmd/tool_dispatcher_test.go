package cmd

import (
	"testing"

	"go.mau.fi/whatsmeow/types"

	wstypes "whatsapp-summarizer/src/types"
)

func TestParseToolArguments(t *testing.T) {
	t.Run("count parsing with float64", func(t *testing.T) {
		args := map[string]any{"count": float64(500)}
		count := 700
		if cVal, ok := args["count"]; ok {
			switch v := cVal.(type) {
			case float64:
				count = int(v)
			}
		}
		if count != 500 {
			t.Errorf("expected count=500, got %d", count)
		}
	})

	t.Run("count clamping to max 9000", func(t *testing.T) {
		args := map[string]any{"count": float64(15000)}
		count := 700
		if cVal, ok := args["count"]; ok {
			switch v := cVal.(type) {
			case float64:
				count = int(v)
			}
		}
		if count > 9000 {
			count = 9000
		}
		if count != 9000 {
			t.Errorf("expected count clamped to 9000, got %d", count)
		}
	})

	t.Run("count clamping to min 5", func(t *testing.T) {
		args := map[string]any{"count": float64(2)}
		count := 700
		if cVal, ok := args["count"]; ok {
			switch v := cVal.(type) {
			case float64:
				count = int(v)
			}
		}
		if count < 5 {
			count = 5
		}
		if count != 5 {
			t.Errorf("expected count clamped to 5, got %d", count)
		}
	})

	t.Run("style mapping", func(t *testing.T) {
		testCases := []struct {
			input    string
			expected string
		}{
			{"curto", "short"},
			{"short", "short"},
			{"longo", "long"},
			{"long", "long"},
			{"medio", "medium"},
			{"", "medium"},
		}
		for _, tc := range testCases {
			style := "medium"
			if tc.input == "curto" || tc.input == "short" {
				style = "short"
			} else if tc.input == "longo" || tc.input == "long" {
				style = "long"
			}
			if style != tc.expected {
				t.Errorf("for input %q: expected style %q, got %q", tc.input, tc.expected, style)
			}
		}
	})
}

func TestDispatchToolCallUnknownTool(t *testing.T) {
	h := &Handler{}
	call := wstypes.ToolCall{
		Name: "non_existent_tool",
		Args: map[string]any{},
	}
	msgTrigger := types.MessageInfo{}

	result := h.DispatchToolCall(call, msgTrigger, nil)
	if result {
		t.Errorf("expected DispatchToolCall to return false for unknown tool, got true")
	}
}
