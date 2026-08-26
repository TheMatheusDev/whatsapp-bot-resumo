package cmd

import (
	"fmt"
	"strconv"
	"strings"

	waE2E "go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	wstypes "whatsapp-summarizer/src/types"
)

// DispatchToolCall routes and executes a tool call requested by the Gemini model.
// Returns true if the tool call was recognized and executed, false otherwise.
func (h *Handler) DispatchToolCall(call wstypes.ToolCall, msgTrigger types.MessageInfo, msg *waE2E.Message) bool {
	if h.logger != nil {
		h.logger.Info("DispatchToolCall: executing tool call",
			"tool", call.Name,
			"args", call.Args,
			"chat", msgTrigger.Chat.User,
			"sender", msgTrigger.Sender.User,
		)
	}

	switch call.Name {
	case "summarize_messages":
		return h.executeSummarizeMessagesTool(call, msgTrigger)

	case "get_daily_summary":
		return h.executeDailySummaryTool(call, msgTrigger)

	case "ask_chat_history":
		return h.executeAskChatHistoryTool(call, msgTrigger)

	case "get_weekly_ranking":
		return h.executeWeeklyRankingTool(msgTrigger)

	case "get_monthly_ranking":
		return h.executeMonthlyRankingTool(msgTrigger)

	case "get_group_rules":
		return h.executeGroupRulesTool(msgTrigger)

	case "create_sticker":
		return h.executeCreateStickerTool(msgTrigger, msg)

	case "list_personalities":
		h.handleListPersonalitiesCommand(msgTrigger)
		return true

	case "get_bot_status":
		h.handlePingCommand(msgTrigger)
		return true

	default:
		if h.logger != nil {
			h.logger.Warn("DispatchToolCall: unknown tool call name", "tool", call.Name)
		}
		return false
	}
}

func (h *Handler) executeSummarizeMessagesTool(call wstypes.ToolCall, msgTrigger types.MessageInfo) bool {
	if !msgTrigger.IsGroup {
		h.reactToCommand(msgTrigger, "❌")
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID,
			"❌ Esse comando só é permitido em grupos.\nUse-o em um grupo onde o bot esteja presente ou adicione o bot ao grupo.")
		return true
	}

	if wait := h.checkSummarizeRateLimit(msgTrigger); wait > 0 {
		h.reactToCommand(msgTrigger, "⏳")
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID,
			fmt.Sprintf("⏳ Aguarde *%.0fs* antes de pedir outro resumo.", wait.Seconds()))
		return true
	}

	count := 700
	if cVal, ok := call.Args["count"]; ok {
		switch v := cVal.(type) {
		case float64:
			count = int(v)
		case int:
			count = v
		case string:
			if parsed, err := strconv.Atoi(v); err == nil {
				count = parsed
			}
		}
	}
	if count < 5 {
		count = 5
	}
	if count > 9000 {
		count = 9000
	}

	style := "medium"
	if sVal, ok := call.Args["style"].(string); ok && sVal != "" {
		sLower := strings.ToLower(sVal)
		if sLower == "curto" || sLower == "short" {
			style = "short"
		} else if sLower == "longo" || sLower == "long" {
			style = "long"
		}
	}

	personality := h.getGroupDefaultPersonality(msgTrigger.Chat.User)
	if pVal, ok := call.Args["personality"].(string); ok && pVal != "" {
		personality = strings.ToLower(pVal)
	}

	opts := wstypes.SummarizeOptions{
		Count:       count,
		Style:       style,
		Personality: personality,
	}

	go h.performSummarization(opts, msgTrigger)
	return true
}

func (h *Handler) executeDailySummaryTool(call wstypes.ToolCall, msgTrigger types.MessageInfo) bool {
	if !msgTrigger.IsGroup {
		h.reactToCommand(msgTrigger, "❌")
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID,
			"❌ Esse comando só é permitido em grupos.\nUse-o em um grupo onde o bot esteja presente ou adicione o bot ao grupo.")
		return true
	}

	if wait := h.checkSummarizeRateLimit(msgTrigger); wait > 0 {
		h.reactToCommand(msgTrigger, "⏳")
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID,
			fmt.Sprintf("⏳ Aguarde *%.0fs* antes de pedir outro resumo.", wait.Seconds()))
		return true
	}

	style := "medium"
	if sVal, ok := call.Args["style"].(string); ok && sVal != "" {
		sLower := strings.ToLower(sVal)
		if sLower == "curto" || sLower == "short" {
			style = "short"
		} else if sLower == "longo" || sLower == "long" {
			style = "long"
		}
	}

	personality := h.getGroupDefaultPersonality(msgTrigger.Chat.User)
	if pVal, ok := call.Args["personality"].(string); ok && pVal != "" {
		personality = strings.ToLower(pVal)
	}

	opts := wstypes.SummarizeOptions{
		Style:       style,
		Personality: personality,
	}

	h.wg.Add(1)
	go func() {
		defer h.wg.Done()
		h.performDailySummarization(opts, msgTrigger)
	}()
	return true
}

func (h *Handler) executeAskChatHistoryTool(call wstypes.ToolCall, msgTrigger types.MessageInfo) bool {
	if !msgTrigger.IsGroup {
		h.reactToCommand(msgTrigger, "❌")
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID,
			"❌ Esse comando só é permitido em grupos.\nUse-o em um grupo onde o bot esteja presente ou adicione o bot ao grupo.")
		return true
	}

	question, _ := call.Args["question"].(string)
	question = strings.TrimSpace(question)
	if question == "" {
		h.reactToCommand(msgTrigger, "❌")
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID, "❌ Você precisa fazer uma pergunta!")
		return true
	}

	if wait := h.checkSummarizeRateLimit(msgTrigger); wait > 0 {
		h.reactToCommand(msgTrigger, "⏳")
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID,
			fmt.Sprintf("⏳ Aguarde *%.0fs* antes de pedir outro resumo.", wait.Seconds()))
		return true
	}

	count := 700
	if cVal, ok := call.Args["message_count"]; ok {
		switch v := cVal.(type) {
		case float64:
			count = int(v)
		case int:
			count = v
		case string:
			if parsed, err := strconv.Atoi(v); err == nil {
				count = parsed
			}
		}
	}
	if count < 5 {
		count = 5
	}
	if count > 9000 {
		count = 9000
	}

	opts := wstypes.SummarizeOptions{
		Count:       count,
		Style:       "medium",
		Personality: h.getGroupDefaultPersonality(msgTrigger.Chat.User),
		Question:    question,
	}

	go h.performSummarization(opts, msgTrigger)
	return true
}

func (h *Handler) executeWeeklyRankingTool(msgTrigger types.MessageInfo) bool {
	if !msgTrigger.IsGroup {
		h.reactToCommand(msgTrigger, "❌")
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID,
			"❌ Esse comando só é permitido em grupos.\nUse-o em um grupo onde o bot esteja presente ou adicione o bot ao grupo.")
		return true
	}

	h.wg.Add(1)
	go func() {
		defer h.wg.Done()
		h.performRollingDaysRanking(msgTrigger, 7, "🏆 *Ranking de mensagens dos últimos 7 dias*")
	}()
	return true
}

func (h *Handler) executeMonthlyRankingTool(msgTrigger types.MessageInfo) bool {
	if !msgTrigger.IsGroup {
		h.reactToCommand(msgTrigger, "❌")
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID,
			"❌ Esse comando só é permitido em grupos.\nUse-o em um grupo onde o bot esteja presente ou adicione o bot ao grupo.")
		return true
	}

	h.wg.Add(1)
	go func() {
		defer h.wg.Done()
		h.performRollingDaysRanking(msgTrigger, 30, "🏆 *Ranking de mensagens dos últimos 30 dias*")
	}()
	return true
}

func (h *Handler) executeGroupRulesTool(msgTrigger types.MessageInfo) bool {
	if !msgTrigger.IsGroup {
		h.reactToCommand(msgTrigger, "❌")
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID,
			"❌ Esse comando só é permitido em grupos.\nUse-o em um grupo onde o bot esteja presente ou adicione o bot ao grupo.")
		return true
	}

	h.handleRulesCommand(msgTrigger)
	return true
}

func (h *Handler) executeCreateStickerTool(msgTrigger types.MessageInfo, msg *waE2E.Message) bool {
	syntheticEvt := &events.Message{Info: msgTrigger, Message: msg}
	h.handleStickerCommand(syntheticEvt)
	return true
}
