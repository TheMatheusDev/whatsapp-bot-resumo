package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	waE2E "go.mau.fi/whatsmeow/proto/waE2E"

	"context"
)

// downloadMedia is a generic template for downloading media files from WhatsApp.
// It handles: directory creation, file creation, context timeout, download, and logging.
func (h *Handler) downloadMedia(msg whatsmeow.DownloadableMessage, prefix, ext string, timeout time.Duration) (string, error) {
	if err := os.MkdirAll("tmp", 0755); err != nil {
		return "", fmt.Errorf("failed to create tmp directory: %w", err)
	}

	timestamp := time.Now().Format("20060102_150405")
	filename := fmt.Sprintf("%s_%s.%s", prefix, timestamp, ext)
	filePath := filepath.Join("tmp", filename)

	file, err := os.Create(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if err := h.whatsappService.DownloadToFile(ctx, msg, file); err != nil {
		return filePath, fmt.Errorf("failed to download %s: %w", prefix, err)
	}

	label := strings.ToUpper(prefix[:1]) + prefix[1:]
	h.logger.Info(fmt.Sprintf("%s downloaded", label), "path", filePath, "size", getFileSize(file))
	return filePath, nil
}

// downloadMediaIfPresent checks if message contains media and downloads it
func (h *Handler) downloadMediaIfPresent(msg *waE2E.Message) {
	if imgMsg := msg.GetImageMessage(); imgMsg != nil {
		if !h.config.Bot.MediaDownload.Image {
			h.logger.Debug("Image download disabled, skipping")
			return
		}
		if err := h.downloadImage(imgMsg); err != nil {
			h.logger.Error("Failed to download image", "error", err)
		}
		return
	}

	if videoMsg := msg.GetVideoMessage(); videoMsg != nil {
		if !h.config.Bot.MediaDownload.Video {
			h.logger.Debug("Video download disabled, skipping")
			return
		}
		if _, err := h.downloadMedia(videoMsg, "video", "mp4", 120*time.Second); err != nil {
			h.logger.Error("Failed to download video", "error", err)
		}
		return
	}

	if audioMsg := msg.GetAudioMessage(); audioMsg != nil {
		if !h.config.Bot.MediaDownload.Audio {
			h.logger.Debug("Audio download disabled, skipping")
			return
		}
		ext := "ogg"
		if mimetype := audioMsg.GetMimetype(); mimetype != "" {
			if strings.Contains(mimetype, "mp3") {
				ext = "mp3"
			} else if strings.Contains(mimetype, "mp4") {
				ext = "m4a"
			}
		}
		if _, err := h.downloadMedia(audioMsg, "audio", ext, 60*time.Second); err != nil {
			h.logger.Error("Failed to download audio", "error", err)
		}
		return
	}

	if docMsg := msg.GetDocumentMessage(); docMsg != nil {
		if !h.config.Bot.MediaDownload.Document {
			h.logger.Debug("Document download disabled, skipping")
			return
		}
		if err := h.downloadDocument(docMsg); err != nil {
			h.logger.Error("Failed to download document", "error", err)
		}
		return
	}

	if stickerMsg := msg.GetStickerMessage(); stickerMsg != nil {
		if !h.config.Bot.MediaDownload.Sticker {
			h.logger.Debug("Sticker download disabled, skipping")
			return
		}
		if _, err := h.downloadMedia(stickerMsg, "sticker", "webp", 60*time.Second); err != nil {
			h.logger.Error("Failed to download sticker", "error", err)
		}
		return
	}
}


// downloadImage downloads an image with thumbnail fallback
func (h *Handler) downloadImage(imgMsg *waE2E.ImageMessage) error {
	filePath, err := h.downloadMedia(imgMsg, "image", "jpg", 60*time.Second)
	if err != nil {
		// If download fails, try to save thumbnail as fallback
		h.logger.Warn("Failed to download full image, saving thumbnail instead", "error", err)
		thumbnailData := imgMsg.GetJPEGThumbnail()
		if len(thumbnailData) > 0 {
			if err := os.WriteFile(filePath, thumbnailData, 0644); err != nil {
				return fmt.Errorf("failed to write thumbnail: %w", err)
			}
			h.logger.Info("Image thumbnail saved", "path", filePath)
			return nil
		}
		return fmt.Errorf("failed to download image and no thumbnail available: %w", err)
	}
	return nil
}

// downloadDocument downloads a document with original filename preservation
func (h *Handler) downloadDocument(docMsg *waE2E.DocumentMessage) error {
	if err := os.MkdirAll("tmp", 0755); err != nil {
		return fmt.Errorf("failed to create tmp directory: %w", err)
	}

	timestamp := time.Now().Format("20060102_150405")
	filename := docMsg.GetTitle()
	if filename == "" {
		filename = fmt.Sprintf("document_%s", timestamp)
	} else {
		filename = fmt.Sprintf("%s_%s", timestamp, sanitizeFilename(filename))
	}
	filePath := filepath.Join("tmp", filename)

	file, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	if err := h.whatsappService.DownloadToFile(ctx, docMsg, file); err != nil {
		return fmt.Errorf("failed to download document: %w", err)
	}

	h.logger.Info("Document downloaded", "path", filePath, "size", getFileSize(file))
	return nil
}

// sanitizeFilename removes potentially dangerous characters from filename
func sanitizeFilename(filename string) string {
	dangerous := []string{"/", "\\", "..", ":", "*", "?", "\"", "<", ">", "|"}
	for _, char := range dangerous {
		filename = strings.ReplaceAll(filename, char, "_")
	}
	return filename
}

// getFileSize returns the size of a file in a human-readable format
func getFileSize(file *os.File) string {
	info, err := file.Stat()
	if err != nil {
		return "unknown"
	}
	size := info.Size()
	if size < 1024 {
		return fmt.Sprintf("%d B", size)
	} else if size < 1024*1024 {
		return fmt.Sprintf("%.2f KB", float64(size)/1024)
	} else if size < 1024*1024*1024 {
		return fmt.Sprintf("%.2f MB", float64(size)/(1024*1024))
	}
	return fmt.Sprintf("%.2f GB", float64(size)/(1024*1024*1024))
}
