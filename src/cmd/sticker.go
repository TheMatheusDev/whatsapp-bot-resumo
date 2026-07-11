package cmd

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	waE2E "go.mau.fi/whatsmeow/proto/waE2E"
	watypes "go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

const (
	stickerMaxImageBytes = 2_097_000 // ~2 MB for static stickers
	stickerMaxVideoBytes = 1_024_000 // 1 MB for animated stickers
	stickerTargetBytes   = 512_000   // 500 KB — WhatsApp animated sticker limit
	stickerSize          = "512:512"
	stickerFPS           = "13"
	stickerInitialQValue = 85
	stickerQStep         = 5

	// stickerExifFile is the filename of the pre-generated EXIF blob that is
	// injected into every sticker via webpmux. The file must live next to the
	// bot executable. It contains the pack name, publisher and pack ID that
	// WhatsApp displays under "Saved Stickers".
	stickerExifFile = "raw.exif"
)

// StickerDeps holds the names of the external binaries required for sticker creation.
var StickerDeps = struct {
	FFmpeg  string
	CWebP   string
	Convert string
	WebPMux string
}{
	FFmpeg:  "ffmpeg",
	CWebP:   "cwebp",
	Convert: "convert",
	WebPMux: "webpmux",
}

// CheckStickerDependencies verifies that ffmpeg, cwebp, convert, and webpmux are
// available in PATH. Should be called once at bot startup.
// Returns an error listing all missing binaries.
func CheckStickerDependencies() error {
	missing := []string{}
	for _, bin := range []string{StickerDeps.FFmpeg, StickerDeps.CWebP, StickerDeps.Convert, StickerDeps.WebPMux} {
		if _, err := exec.LookPath(bin); err != nil {
			missing = append(missing, bin)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("sticker dependencies not found in PATH: %s", strings.Join(missing, ", "))
	}
	return nil
}

// stickerExifPath returns the absolute path to raw.exif, resolved relative to
// the bot executable so the file is found regardless of working directory.
func stickerExifPath() string {
	exe, err := os.Executable()
	if err != nil {
		return stickerExifFile // fallback: relative to working dir
	}
	return filepath.Join(filepath.Dir(exe), stickerExifFile)
}

// handleStickerCommand is the entry point for !figurinha / !sticker.
// It expects the user to have replied to a message containing an image, video, or animated GIF.
func (h *Handler) handleStickerCommand(evt *events.Message) {
	msgTrigger := evt.Info

	// Extract the target message (either the media in the current message or the replied one)
	targetMsg := extractTargetMedia(evt.Message)
	if targetMsg == nil {
		h.whatsappService.ReactToMessage(
			context.Background(), msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID, "❌",
		)
		h.whatsappService.SendMessageReply(
			msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID,
			"Responda a uma foto, vídeo ou GIF com !figurinha para criar o sticker.",
		)
		return
	}

	// Dispatch based on media type
	switch m := targetMsg.(type) {
	case *waE2E.ImageMessage:
		if fileLen := m.GetFileLength(); fileLen > stickerMaxImageBytes {
			h.whatsappService.ReactToMessage(
				context.Background(), msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID, "❌",
			)
			h.whatsappService.SendMessageReply(
				msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID,
				fmt.Sprintf("Imagem muito grande (%.1f MB). Limite: 2 MB.", float64(fileLen)/(1024*1024)),
			)
			return
		}
		h.createStickerAsync(evt, m, "image", "jpg")

	case *waE2E.VideoMessage:
		if fileLen := m.GetFileLength(); fileLen > stickerMaxVideoBytes {
			h.whatsappService.ReactToMessage(
				context.Background(), msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID, "❌",
			)
			h.whatsappService.SendMessageReply(
				msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID,
				fmt.Sprintf("Vídeo/GIF muito grande (%.1f MB). Limite: 1 MB.", float64(fileLen)/(1024*1024)),
			)
			return
		}
		// Warn the user upfront that video processing takes longer
		h.whatsappService.ReactToMessage(
			context.Background(), msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID, "⏳",
		)
		h.createStickerAsync(evt, m, "video", resolveVideoExt(m.GetMimetype()))

	default:
		h.whatsappService.ReactToMessage(
			context.Background(), msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID, "❌",
		)
		h.whatsappService.SendMessageReply(
			msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID,
			"Tipo de mídia não suportado. Responda a uma *foto*, *vídeo* ou *GIF*.",
		)
	}
}

// createStickerAsync runs the full download → convert → upload → send pipeline
// in a background goroutine, tracking it via h.wg for graceful shutdown.
func (h *Handler) createStickerAsync(evt *events.Message, media whatsmeow.DownloadableMessage, prefix, ext string) {
	msgTrigger := evt.Info

	h.wg.Add(1)
	go func() {
		defer h.wg.Done()

		// --- 1. Download the source media ---
		inputPath, err := h.downloadMedia(media, "sticker_src_"+prefix, ext, 90*time.Second)
		if err != nil {
			h.logger.Error("Sticker: failed to download source media", "error", err)
			h.whatsappService.ReactToMessage(
				context.Background(), msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID, "❌",
			)
			h.whatsappService.SendMessageReply(
				msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID,
				"Falha ao baixar a mídia. Tente novamente.",
			)
			return
		}
		defer os.Remove(inputPath)

		// --- 2. Convert to .webp ---
		var webpPath string
		if ext == "jpg" {
			webpPath, err = h.convertImageToSticker(inputPath)
		} else {
			webpPath, err = h.convertVideoToSticker(inputPath)
		}
		if err != nil {
			h.logger.Error("Sticker: conversion failed", "error", err, "input", inputPath)
			h.whatsappService.ReactToMessage(
				context.Background(), msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID, "❌",
			)
			h.whatsappService.SendMessageReply(
				msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID,
				err.Error(),
			)
			return
		}
		defer os.Remove(webpPath)

		// --- 3. Inject WhatsApp sticker pack metadata via webpmux ---
		// webpmux embeds the raw.exif blob (pack name, publisher, pack ID) into
		// the WebP file so WhatsApp displays them under "Saved Stickers".
		// Non-fatal: if webpmux fails the sticker is still sent without metadata.
		if out, err := exec.Command(
			StickerDeps.WebPMux,
			"-set", "exif", stickerExifPath(),
			webpPath,
			"-o", webpPath,
		).CombinedOutput(); err != nil {
			h.logger.Warn("Sticker: webpmux metadata injection failed (continuing anyway)",
				"error", err, "output", string(out))
		}

		// --- 4. Upload & send the sticker ---
		if err := h.uploadAndSendSticker(evt.Info, webpPath, ext != "jpg"); err != nil {
			h.logger.Error("Sticker: upload/send failed", "error", err)
			h.whatsappService.ReactToMessage(
				context.Background(), msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID, "❌",
			)
			h.whatsappService.SendMessageReply(
				msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID,
				"Falha ao enviar o sticker. Tente novamente.",
			)
			return
		}

		// React with ✅ to confirm the sticker was created successfully
		h.whatsappService.ReactToMessage(
			context.Background(), msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID, "✅",
		)
		h.logger.Info("Sticker: created and sent successfully", "chat", msgTrigger.Chat.String())
	}()
}

// convertImageToSticker converts a static image (JPEG) to a 512x512 WebP sticker.
// Pipeline: convert (ImageMagick) -> cwebp
func (h *Handler) convertImageToSticker(inputPath string) (string, error) {
	// Stage 1: normalize canvas to 512x512 with ImageMagick
	normalizedPath := strings.TrimSuffix(inputPath, filepath.Ext(inputPath)) + "_norm.png"
	defer os.Remove(normalizedPath)

	cmd := exec.Command(StickerDeps.Convert,
		inputPath,
		"-gravity", "center",
		"-background", "none",
		"-resize", "512x512",
		"-extent", "512x512",
		normalizedPath,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("falha no redimensionamento (ImageMagick): %s", string(out))
	}

	// Stage 2: encode to WebP with cwebp
	outputPath := strings.TrimSuffix(inputPath, filepath.Ext(inputPath)) + ".webp"
	cmd = exec.Command(StickerDeps.CWebP,
		normalizedPath,
		"-resize", "512", "512",
		"-o", outputPath,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("falha na compressão WebP (cwebp): %s", string(out))
	}

	h.logger.Info("Sticker: static image converted", "output", outputPath)
	return outputPath, nil
}

// convertVideoToSticker converts a video or animated GIF to an animated WebP sticker.
// Uses an adaptive quality loop: starts at qValue=60, decrements by 10 until the
// output is <= 500 KB (stickerTargetBytes), as required by WhatsApp's animated sticker spec.
func (h *Handler) convertVideoToSticker(inputPath string) (string, error) {
	outputPath := strings.TrimSuffix(inputPath, filepath.Ext(inputPath)) + ".webp"

	for qValue := stickerInitialQValue; qValue >= 0; qValue -= stickerQStep {
		// Remove any previous failed attempt
		os.Remove(outputPath)

		cmd := exec.Command(StickerDeps.FFmpeg,
			"-i", inputPath,
			"-fs", fmt.Sprintf("%d", stickerTargetBytes),
			"-filter:v", fmt.Sprintf("fps=fps=%s", stickerFPS),
			"-compression_level", "0",
			"-q:v", fmt.Sprintf("%d", qValue),
			"-loop", "0",
			"-preset", "picture",
			"-an",
			"-vsync", "0",
			"-s", stickerSize,
			outputPath,
		)

		h.logger.Info("Sticker: running ffmpeg", "q_value", qValue, "output", outputPath)
		if out, err := cmd.CombinedOutput(); err != nil {
			return "", fmt.Errorf("falha na conversão de vídeo (ffmpeg, q=%d): %s", qValue, string(out))
		}

		// Check resulting file size
		info, err := os.Stat(outputPath)
		if err != nil {
			return "", fmt.Errorf("falha ao verificar tamanho do arquivo gerado: %w", err)
		}

		if info.Size() <= stickerTargetBytes {
			h.logger.Info("Sticker: animated webp within size limit",
				"size_bytes", info.Size(), "q_value", qValue)
			return outputPath, nil
		}

		h.logger.Warn("Sticker: output exceeds limit, retrying with lower quality",
			"size_bytes", info.Size(), "q_value", qValue, "next_q", qValue-stickerQStep)
	}

	return "", fmt.Errorf("não foi possível comprimir o vídeo para menos de 500 KB — tente um clipe mais curto")
}

// uploadAndSendSticker reads the generated .webp file, uploads it to WhatsApp's
// media servers, and sends it as a StickerMessage to the chat.
func (h *Handler) uploadAndSendSticker(msgTrigger watypes.MessageInfo, webpPath string, animated bool) error {
	data, err := os.ReadFile(webpPath)
	if err != nil {
		return fmt.Errorf("failed to read webp file: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	resp, err := h.whatsappService.UploadMedia(ctx, data, whatsmeow.MediaImage)
	if err != nil {
		return fmt.Errorf("upload failed: %w", err)
	}

	// Compute SHA256 of the plaintext data (required by the StickerMessage proto)
	hash := sha256.Sum256(data)

	stickerMsg := &waE2E.Message{
		StickerMessage: &waE2E.StickerMessage{
			URL:           proto.String(resp.URL),
			DirectPath:    proto.String(resp.DirectPath),
			MediaKey:      resp.MediaKey,
			Mimetype:      proto.String("image/webp"),
			FileEncSHA256: resp.FileEncSHA256,
			FileSHA256:    hash[:],
			FileLength:    proto.Uint64(uint64(len(data))),
			IsAnimated:    proto.Bool(animated),
			ContextInfo: &waE2E.ContextInfo{
				StanzaID:    proto.String(string(msgTrigger.ID)),
				Participant: proto.String(msgTrigger.Sender.ToNonAD().String()),
			},
		},
	}

	sendCtx, sendCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer sendCancel()

	if _, err := h.whatsappService.SendRawMessage(sendCtx, msgTrigger.Chat, stickerMsg); err != nil {
		return fmt.Errorf("send failed: %w", err)
	}
	return nil
}

// extractTargetMedia returns the downloadable media message.
// It first checks if the current message contains the media (e.g. command in the caption).
// If not, it checks if it's a reply to a media message.
func extractTargetMedia(msg *waE2E.Message) whatsmeow.DownloadableMessage {
	// 1. Check if the message itself is a media message
	if img := msg.GetImageMessage(); img != nil {
		return img
	}
	if vid := msg.GetVideoMessage(); vid != nil {
		return vid
	}

	// 2. Fallback to check if it's a reply (quoted message)
	var contextInfo *waE2E.ContextInfo

	if ext := msg.GetExtendedTextMessage(); ext != nil {
		contextInfo = ext.GetContextInfo()
	} else if img := msg.GetImageMessage(); img != nil {
		contextInfo = img.GetContextInfo()
	} else if vid := msg.GetVideoMessage(); vid != nil {
		contextInfo = vid.GetContextInfo()
	}

	if contextInfo == nil {
		return nil
	}

	quoted := contextInfo.GetQuotedMessage()
	if quoted == nil {
		return nil
	}

	if img := quoted.GetImageMessage(); img != nil {
		return img
	}
	if vid := quoted.GetVideoMessage(); vid != nil {
		return vid
	}

	return nil
}

// resolveVideoExt infers a file extension from the video MIME type.
func resolveVideoExt(mimetype string) string {
	switch {
	case strings.Contains(mimetype, "gif"):
		return "gif"
	case strings.Contains(mimetype, "mp4"):
		return "mp4"
	case strings.Contains(mimetype, "3gpp"):
		return "3gp"
	case strings.Contains(mimetype, "webm"):
		return "webm"
	default:
		return "mp4"
	}
}
