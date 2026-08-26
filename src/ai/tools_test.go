package ai

import (
	"testing"

	"google.golang.org/genai"
)

func TestGetChatbotTools(t *testing.T) {
	tools := GetChatbotTools()
	if len(tools) == 0 {
		t.Fatal("expected at least one Tool, got 0")
	}

	decls := tools[0].FunctionDeclarations
	if len(decls) == 0 {
		t.Fatal("expected FunctionDeclarations to be non-empty")
	}

	expectedTools := map[string]bool{
		"summarize_messages": false,
		"get_daily_summary":  false,
		"ask_chat_history":   false,
		"get_weekly_ranking": false,
		"get_monthly_ranking": false,
		"get_group_rules":    false,
		"create_sticker":     false,
		"list_personalities": false,
		"get_bot_status":     false,
	}

	for _, decl := range decls {
		if _, exists := expectedTools[decl.Name]; exists {
			expectedTools[decl.Name] = true
		}
		if decl.Description == "" {
			t.Errorf("tool %q has empty description", decl.Name)
		}
		if decl.Parameters == nil {
			t.Errorf("tool %q has nil Parameters (Gemini API requires TypeObject schema)", decl.Name)
		} else if decl.Parameters.Type != genai.TypeObject {
			t.Errorf("tool %q Parameters.Type is %v, expected %v", decl.Name, decl.Parameters.Type, genai.TypeObject)
		}
	}

	for name, found := range expectedTools {
		if !found {
			t.Errorf("expected tool %q not found in FunctionDeclarations", name)
		}
	}

	// Verify summarize_messages parameters
	var sumDecl *genai.FunctionDeclaration
	for _, decl := range decls {
		if decl.Name == "summarize_messages" {
			sumDecl = decl
			break
		}
	}
	if sumDecl == nil || sumDecl.Parameters == nil {
		t.Fatal("summarize_messages has nil parameters")
	}
	if _, ok := sumDecl.Parameters.Properties["count"]; !ok {
		t.Error("summarize_messages missing 'count' property")
	}
	if _, ok := sumDecl.Parameters.Properties["style"]; !ok {
		t.Error("summarize_messages missing 'style' property")
	}
	if _, ok := sumDecl.Parameters.Properties["personality"]; !ok {
		t.Error("summarize_messages missing 'personality' property")
	}
}
