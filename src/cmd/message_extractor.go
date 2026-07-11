package cmd

import (
	waE2E "go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
)

// extractMessageContent extracts text content from a message
func (h *Handler) extractMessageContent(msg *waE2E.Message) string {
	if msg.GetConversation() != "" {
		return msg.GetConversation()
	}

	if extMsg := msg.GetExtendedTextMessage(); extMsg != nil {
		baseText := extMsg.GetText()

		// Check if this message is replying to another message
		if contextInfo := extMsg.GetContextInfo(); contextInfo != nil {
			if quotedMsg := contextInfo.GetQuotedMessage(); quotedMsg != nil {
				quotedText := h.extractQuotedMessageText(quotedMsg)
				if baseText != "" && quotedText != "" {
					return baseText + " [respondendo a] '" + quotedText + "'"
				}
				if baseText != "" {
					return baseText
				}
				if quotedText != "" {
					return "[respondendo a] '" + quotedText + "'"
				}
			}
		}

		return baseText
	}

	// Handle audio messages - return placeholder for transcription
	if msg.GetAudioMessage() != nil {
		return "[Áudio Não Transcrito]"
	}

	if imgMsg := msg.GetImageMessage(); imgMsg != nil {
		if caption := imgMsg.GetCaption(); caption != "" {
			return "[Image] " + caption
		}
		return "[Image]"
	}

	if videoMsg := msg.GetVideoMessage(); videoMsg != nil {
		if caption := videoMsg.GetCaption(); caption != "" {
			return "[Video] " + caption
		}
		return "[Video]"
	}

	if docMsg := msg.GetDocumentMessage(); docMsg != nil {
		if title := docMsg.GetTitle(); title != "" {
			return "[Document] " + title
		}
		return "[Document]"
	}

	// Stickers are intentionally excluded: they carry no text value for summarisation
	// and attempting to save them triggers FOREIGN KEY / CHECK constraint violations.
	if msg.GetStickerMessage() != nil {
		return ""
	}

	// Handle other message types as needed
	return ""
}

// extractQuotedMessageText extracts text from a quoted message
func (h *Handler) extractQuotedMessageText(msg *waE2E.Message) string {
	if msg.GetConversation() != "" {
		return msg.GetConversation()
	}

	if extMsg := msg.GetExtendedTextMessage(); extMsg != nil {
		return extMsg.GetText()
	}

	if imgMsg := msg.GetImageMessage(); imgMsg != nil {
		if caption := imgMsg.GetCaption(); caption != "" {
			return "[Image] " + caption
		}
		return "[Image]"
	}

	if videoMsg := msg.GetVideoMessage(); videoMsg != nil {
		if caption := videoMsg.GetCaption(); caption != "" {
			return "[Video] " + caption
		}
		return "[Video]"
	}

	if audioMsg := msg.GetAudioMessage(); audioMsg != nil {
		_ = audioMsg
		return "[Áudio Não Transcrito]"
	}

	if docMsg := msg.GetDocumentMessage(); docMsg != nil {
		if title := docMsg.GetTitle(); title != "" {
			return "[Document] " + title
		}
		return "[Document]"
	}

	// Stickers are excluded from quoted context too — no useful text to extract.
	if msg.GetStickerMessage() != nil {
		return ""
	}

	return "[Unknown message type]"
}



// getSenderName gets the sender name for a message
func (h *Handler) getSenderName(info types.MessageInfo) string {
	if info.IsFromMe {
		return "ResumoBOT [VOCÊ]"
	}

	// For group messages, use the sender's push name or phone number
	if !info.IsGroup {
		if info.PushName != "" {
			return info.PushName
		}
		return info.Sender.User
	}

	// For group messages
	if info.PushName != "" {
		return info.PushName
	}
	return info.Sender.User
}

// getMessageType determines the message type for storage.
// Returned values must be in the CHECK constraint set defined in the messages table:
//
//	'Conversation', 'ExtendedText', 'Audio', 'Summary', 'Image', 'Video', 'Document'
//
// "Unknown" is intentionally kept as the default so that any future WhatsApp
// message type that is not yet handled here is silently discarded by the
// isCheckViolation guard in SaveGroupMessageReturningID — rather than
// being stored with misleading metadata.
// Note: StickerMessage is excluded upstream in extractMessageContent (returns ""),
// so it never reaches this function.
func (h *Handler) getMessageType(msg *waE2E.Message) string {
	switch {
	case msg.GetConversation() != "":
		return "Conversation"
	case msg.GetExtendedTextMessage() != nil:
		return "ExtendedText"
	case msg.GetImageMessage() != nil:
		return "Image"
	case msg.GetVideoMessage() != nil:
		return "Video"
	case msg.GetAudioMessage() != nil:
		return "Audio"
	case msg.GetDocumentMessage() != nil:
		return "Document"
	default:
		return "Unknown"
	}
}
