package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	waE2E "go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

// isVoiceMessage checks if an audio message is a voice message (audio/ogg with opus codec)
func isVoiceMessage(audioMsg *waE2E.AudioMessage) bool {
	mimetype := audioMsg.GetMimetype()
	return strings.Contains(mimetype, "audio/ogg") || strings.Contains(mimetype, "opus")
}

// tryDownloadAudioToMemory downloads audio to memory for transcription
func (h *Handler) tryDownloadAudioToMemory(audioMsg *waE2E.AudioMessage) ([]byte, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	data, err := h.whatsappService.DownloadToMemory(ctx, whatsmeow.DownloadableMessage(audioMsg))
	if err != nil {
		return nil, "", err
	}

	mimeType := audioMsg.GetMimetype()
	return data, mimeType, nil
}

// transcribeAudioAsync runs the audio transcription in a goroutine, updates the database,
// and optionally replies to the message if automatic transcription is enabled for the group.
func (h *Handler) transcribeAudioAsync(msgID int64, audioData []byte, mimeType string, msgInfo types.MessageInfo) {
	h.wg.Add(1)
	go func() {
		defer h.wg.Done()

		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()

		h.logger.Info("Starting async audio transcription", "msg_id", msgID, "mime_type", mimeType, "size_bytes", len(audioData))

		transcription, err := h.aiService.TranscribeAudio(ctx, audioData, mimeType)
		if err != nil {
			h.logger.Error("Failed to transcribe audio", "msg_id", msgID, "error", err)
			return
		}

		// Update the database entry with the transcription
		newContent := "[Áudio Transcrito] " + transcription
		if err := h.dbService.UpdateMessageContent(msgID, newContent); err != nil {
			h.logger.Error("Failed to update message with transcription", "msg_id", msgID, "error", err)
			return
		}

		h.logger.Info("Audio transcription completed and saved", "msg_id", msgID, "transcription_length", len(transcription))

		// If auto-transcribe reply is enabled for this chat and message was sent after bot start, reply
		settings := h.loadOrDefaultSettings(msgInfo.Chat.User)
		if settings.AudioTranscribeEnabled && !msgInfo.Timestamp.Before(h.botStartTime) {
			replyText := fmt.Sprintf("🎙️ *Transcrição do Áudio:*\n\n%s", transcription)
			if err := h.whatsappService.SendMessageReply(msgInfo.Chat, msgInfo.Sender, msgInfo.ID, replyText); err != nil {
				h.logger.Error("Failed to send transcription reply", "chat_id", msgInfo.Chat.String(), "msg_id", msgInfo.ID, "error", err)
			} else {
				h.logger.Info("Transcription reply sent successfully", "chat_id", msgInfo.Chat.String(), "msg_id", msgInfo.ID)
			}
		}
	}()
}

// extractTargetAudio returns the AudioMessage if msg contains or quotes a voice/audio message.
func extractTargetAudio(msg *waE2E.Message) *waE2E.AudioMessage {
	if msg == nil {
		return nil
	}

	// 1. Check if the message itself is an audio message
	if aud := msg.GetAudioMessage(); aud != nil {
		return aud
	}

	// 2. Check if it is a reply (quoted message)
	var contextInfo *waE2E.ContextInfo
	if ext := msg.GetExtendedTextMessage(); ext != nil {
		contextInfo = ext.GetContextInfo()
	} else if img := msg.GetImageMessage(); img != nil {
		contextInfo = img.GetContextInfo()
	} else if vid := msg.GetVideoMessage(); vid != nil {
		contextInfo = vid.GetContextInfo()
	} else if aud := msg.GetAudioMessage(); aud != nil {
		contextInfo = aud.GetContextInfo()
	}

	if contextInfo == nil {
		return nil
	}

	quoted := contextInfo.GetQuotedMessage()
	if quoted == nil {
		return nil
	}

	if aud := quoted.GetAudioMessage(); aud != nil {
		return aud
	}

	return nil
}

// handleTranscribeCommand handles the !transcreva / !transcreve / !t command.
// It transcribes a voice message quoted by the user and replies with the transcription text.
func (h *Handler) handleTranscribeCommand(evt *events.Message) {
	msgTrigger := evt.Info

	audioMsg := extractTargetAudio(evt.Message)
	if audioMsg == nil || !isVoiceMessage(audioMsg) {
		h.reactToCommand(msgTrigger, "❌")
		h.whatsappService.SendMessageReply(
			msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID,
			"❌ Este comando só funciona respondendo a uma mensagem de voz.",
		)
		return
	}

	h.reactToCommand(msgTrigger, "⏳")

	audioData, mimeType, err := h.tryDownloadAudioToMemory(audioMsg)
	if err != nil {
		h.logger.Error("handleTranscribeCommand: failed to download audio", "error", err)
		h.reactToCommand(msgTrigger, "❌")
		h.whatsappService.SendMessageReply(
			msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID,
			"❌ Não foi possível baixar o áudio para transcrição. Tente novamente.",
		)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	transcription, err := h.aiService.TranscribeAudio(ctx, audioData, mimeType)
	if err != nil {
		h.logger.Error("handleTranscribeCommand: failed to transcribe audio", "error", err)
		h.reactToCommand(msgTrigger, "❌")
		h.whatsappService.SendMessageReply(
			msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID,
			"❌ Não foi possível transcrever o áudio. Tente novamente.",
		)
		return
	}

	h.reactToCommand(msgTrigger, "✅")
	replyText := fmt.Sprintf("🎙️ *Transcrição:*\n\n%s", transcription)
	if err := h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID, replyText); err != nil {
		h.logger.Error("handleTranscribeCommand: failed to send reply", "error", err)
	}
}
