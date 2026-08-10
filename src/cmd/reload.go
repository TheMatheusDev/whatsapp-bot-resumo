package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.mau.fi/whatsmeow/types"

	"whatsapp-summarizer/src/ai"
)

// handleReloadPersonalitiesCommand reloads all personality TOML files from disk
// without requiring a bot restart. Only bot admins and group admins can invoke it.
//
// On success: reacts ✅ and sends a confirmation listing loaded personalities.
// On partial error (some files broken): reacts ✅ but warns about unavailable personalities.
// On total failure (no files found): reacts ❌ and sends the error.
func (h *Handler) handleReloadPersonalitiesCommand(msgTrigger types.MessageInfo) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if !h.isGroupAdmin(ctx, msgTrigger.Chat, msgTrigger.Sender.User) {
		h.reactToCommand(msgTrigger, "❌")
		h.whatsappService.SendMessageReply(
			msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID,
			"❌ Apenas admins podem recarregar as personalidades.",
		)
		return
	}

	loader, ok := h.getPersonalityLoader()
	if !ok {
		h.reactToCommand(msgTrigger, "❌")
		h.whatsappService.SendMessageReply(
			msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID,
			"❌ Não foi possível carregar as personalidades — verifique os logs no servidor.",
		)
		return
	}

	reloadErr := loader.Reload()

	// Build status message regardless of partial errors
	available := loader.ListAvailable()
	var okList, errList []string
	for name, healthy := range available {
		if healthy {
			okList = append(okList, "✅ "+name)
		} else {
			errList = append(errList, "❌ "+name+" (mal configurada — verifique o arquivo)")
		}
	}

	if reloadErr != nil && len(okList) == 0 {
		// Total failure: no personality loaded at all
		h.reactToCommand(msgTrigger, "❌")
		h.whatsappService.SendMessageReply(
			msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID,
			fmt.Sprintf("❌ Falha ao recarregar personalidades:\n%s", reloadErr.Error()),
		)
		return
	}

	// At least some personalities loaded — react success
	h.reactToCommand(msgTrigger, "✅")

	var sb strings.Builder
	sb.WriteString("✅ *Personalidades recarregadas!*\n\n")

	if len(okList) > 0 {
		sb.WriteString("*Disponíveis:*\n")
		for _, s := range okList {
			sb.WriteString("  " + s + "\n")
		}
	}

	if len(errList) > 0 {
		sb.WriteString("\n⚠️ *Com problemas (indisponíveis):*\n")
		for _, s := range errList {
			sb.WriteString("  " + s + "\n")
		}
		sb.WriteString("\nVerifique os arquivos .toml e envie !reload novamente.")
	}

	h.whatsappService.SendMessageReply(
		msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID,
		strings.TrimSpace(sb.String()),
	)
}

// getPersonalityLoader returns the handler's PersonalityLoader reference.
// Returns (nil, false) when the handler was created without one.
func (h *Handler) getPersonalityLoader() (*ai.PersonalityLoader, bool) {
	if h.personalityLoader == nil {
		return nil, false
	}
	return h.personalityLoader, true
}
