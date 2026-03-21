package cmd

import (
	"context"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	waE2E "go.mau.fi/whatsmeow/proto/waE2E"
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

// transcribeAudioAsync runs the audio transcription in a goroutine and updates the database
func (h *Handler) transcribeAudioAsync(msgID int64, audioData []byte, mimeType string) {
	go func() {
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
	}()
}
