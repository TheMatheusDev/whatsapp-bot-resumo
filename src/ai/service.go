package ai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"google.golang.org/genai"

	"whatsapp-summarizer/src/types"
)

// Service implements the AIService interface
type Service struct {
	client            *genai.Client
	apiKey            string
	model             string
	modelBackup       string
	modelBackup2      string
	modelTranscribe   string
	apiLogs           bool
	logger            types.Logger
	timezone          *time.Location // used to format message timestamps sent to the AI
	personalityLoader *PersonalityLoader
	httpClient        *http.Client
}

// NewService creates a new AI service.
// timezone is an IANA timezone name (e.g. "America/Sao_Paulo") used to
// convert message timestamps to local time before sending them to the API.
// An empty or invalid value falls back to UTC.
// personalityLoader is used to resolve personality prompts at runtime from TOML files.
func NewService(apiKey string, model string, modelBackup string, modelBackup2 string, modelTranscribe string, apiLogs bool, logger types.Logger, timezone string, personalityLoader *PersonalityLoader) (*Service, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("API key is required")
	}

	if model == "" {
		model = "gemini-3.1-pro-preview" // Default fallback
	}

	if modelBackup == "" {
		modelBackup = "gemini-3-flash-preview" // Default backup
	}

	if modelBackup2 == "" {
		modelBackup2 = "gemini-2.5-flash" // Default second backup
	}

	if modelTranscribe == "" {
		modelTranscribe = "gemini-3.5-transcribe" // Default transcription model
	}

	loc, err := time.LoadLocation(timezone)
	if err != nil {
		// "GMT-3" and similar non-IANA names are common in .env files but are
		// not recognised by time.LoadLocation. Fall back to a fixed UTC-3 zone
		// (matching the bot's default region) rather than silently using UTC.
		logger.Warn("AI service: invalid IANA timezone name, falling back to UTC-3",
			"timezone", timezone, "error", err)
		loc = time.FixedZone("UTC-3", -3*60*60)
	}

	ctx := context.Background()
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey: apiKey,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create Gemini client: %w", err)
	}

	return &Service{
		client:            client,
		apiKey:            apiKey,
		model:             model,
		modelBackup:       modelBackup,
		modelBackup2:      modelBackup2,
		modelTranscribe:   modelTranscribe,
		apiLogs:           apiLogs,
		logger:            logger,
		timezone:          loc,
		personalityLoader: personalityLoader,
		httpClient:        &http.Client{Timeout: 2 * time.Minute},
	}, nil
}

// SummarizeMessages summarizes a list of messages, retrying with the backup
// models if the primary one fails. Fallback order: primary → backup → backup2.
//
// onRetry is called before each fallback attempt (attempt is 1-based, so the
// first retry fires with attempt=2). Pass nil when no progress feedback is needed.
func (s *Service) SummarizeMessages(ctx context.Context, messages []types.Message, opts types.SummarizeOptions, onRetry types.OnRetryFunc) (string, error) {
	if len(messages) == 0 {
		return "", fmt.Errorf("no messages to summarize")
	}

	systemPrompt, err := s.buildSystemPrompt(opts)
	if err != nil {
		return "", err
	}

	messagesStr := s.buildMessagesString(messages)
	var userPrompt string
	if opts.Question != "" {
		userPrompt = fmt.Sprintf("Responda a pergunta a seguir baseado nas msgs: \"%s\"\n\nMensagens:\n%s", opts.Question, messagesStr)
	} else {
		userPrompt = fmt.Sprintf("Resuma as seguintes mensagens:\n%s", messagesStr)
	}

	fullPrompt := fmt.Sprintf("%s\n\n%s", systemPrompt, userPrompt)
	s.logger.Debug("Generated full prompt", "prompt", fullPrompt)

	models := []string{s.model, s.modelBackup, s.modelBackup2}
	var lastErr error
	for i, model := range models {
		if i > 0 {
			s.logger.Warn("SummarizeMessages: retrying with fallback model",
				"model", model, "attempt", i+1, "prev_error", lastErr)
			if onRetry != nil {
				onRetry(i+1, model)
			}
		}
		result, err := s.generateContent(ctx, fullPrompt, model)
		if err == nil {
			return result, nil
		}
		lastErr = err
	}
	return "", fmt.Errorf("all models failed to summarize: %w", lastErr)
}

// ChatResponse generates a conversational reply when the bot is mentioned or
// receives a reply in a group. It uses the recent group message history as
// context (including bot messages) and responds in the group's configured
// personality. Falls back through models the same way as SummarizeMessages.
func (s *Service) ChatResponse(ctx context.Context, messages []types.Message, triggerMsg string, triggerSender string, opts types.ChatOptions) (string, error) {
	if len(messages) == 0 {
		return "", fmt.Errorf("no messages for chat context")
	}

	messagesStr := s.buildMessagesString(messages)
	systemPrompt, err := s.buildChatSystemPrompt(opts)
	if err != nil {
		return "", err
	}

	userPrompt := fmt.Sprintf(
		"Histórico recente da conversa:\n%s\n\n%s escreveu para você: \"%s\"\n\nResponda diretamente a essa mensagem.",
		messagesStr, triggerSender, triggerMsg,
	)

	fullPrompt := fmt.Sprintf("%s\n\n%s", systemPrompt, userPrompt)
	s.logger.Debug("Generated chat prompt", "prompt", fullPrompt)

	models := []string{s.model, s.modelBackup, s.modelBackup2}
	var lastErr error
	for i, model := range models {
		if i > 0 {
			s.logger.Warn("ChatResponse: retrying with fallback model",
				"model", model, "attempt", i+1, "prev_error", lastErr)
		}
		result, err := s.generateContent(ctx, fullPrompt, model)
		if err == nil {
			return result, nil
		}
		lastErr = err
	}
	return "", fmt.Errorf("all models failed to generate chat response: %w", lastErr)
}

// buildChatSystemPrompt returns the chat system prompt for the configured personality
// by reading it from the PersonalityLoader.
func (s *Service) buildChatSystemPrompt(opts types.ChatOptions) (string, error) {
	name := opts.Personality
	if name == "" {
		name = "resumobot"
	}
	return s.personalityLoader.GetChatPersonality(name)
}

// buildMessagesString converts messages to a formatted string.
// Timestamps are converted to the service's configured timezone before
// formatting so the AI model sees local times rather than UTC.
func (s *Service) buildMessagesString(messages []types.Message) string {
	var builder strings.Builder

	for _, msg := range messages {
		if msg.Content != "" {
			// Format: [timestamp] Sender: Message
			timestamp := msg.Timestamp.In(s.timezone).Format("15:04:05")
			builder.WriteString(fmt.Sprintf("[%s] %s: %s\n", timestamp, msg.Sender, msg.Content))
		}
	}

	return builder.String()
}

// buildSystemPrompt creates the system prompt based on options by reading
// personality and length prompts from the PersonalityLoader.
func (s *Service) buildSystemPrompt(opts types.SummarizeOptions) (string, error) {
	name := opts.Personality
	if name == "" {
		name = "resumobot"
	}

	personality, err := s.personalityLoader.GetSummarizePersonality(name)
	if err != nil {
		return "", err
	}

	lengthPrompt, err := s.personalityLoader.GetLengthPrompt(name, opts.Style)
	if err != nil {
		return "", err
	}

	// Add question-specific instructions if a question is provided
	var questionPrompt string
	if opts.Question != "" {
		questionPrompt = "\n\nVocê deve responder SOMENTE à pergunta fornecida pelo usuário. NÃO faça um resumo das mensagens. A resposta deve ser baseada nas mensagens fornecidas e ser direta e objetiva, respondendo especificamente o que foi perguntado."
	}

	return fmt.Sprintf("%s\n%s%s", personality, lengthPrompt, questionPrompt), nil
}

// generateContent calls the Gemini API to generate content
func (s *Service) generateContent(ctx context.Context, prompt string, model string) (string, error) {
	// Add timeout if not already set
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Minute*2)
		defer cancel()
	}

	// Create content from prompt
	contents := []*genai.Content{
		genai.NewContentFromText(prompt, genai.RoleUser),
	}

	// Create generation config
	config := &genai.GenerateContentConfig{
		Temperature:     genai.Ptr(float32(0.7)),
		MaxOutputTokens: 65536,
	}

	// Generate content
	resp, err := s.client.Models.GenerateContent(ctx, model, contents, config)
	if err != nil {
		s.logger.Error("Failed to generate content", "error", err)
		return "", fmt.Errorf("failed to generate content: %w", err)
	}

	// Save the entire API response to a file for debugging
	if s.apiLogs {
		s.saveAPIResponse(resp)
	}

	// Extract response content
	if len(resp.Candidates) == 0 || len(resp.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("empty response from Gemini API")
	}

	// Extract text from the response part
	part := resp.Candidates[0].Content.Parts[0]

	// Access the text field directly from the part
	content := part.Text

	if content == "" {
		return "", fmt.Errorf("empty content in Gemini response")
	}

	s.logger.Info("Successfully generated summary", "length", len(content))
	return content, nil
}

// saveAPIResponse saves the Gemini API response to a file for debugging
func (s *Service) saveAPIResponse(resp *genai.GenerateContentResponse) {
	// Open file in append mode, create if doesn't exist
	file, err := os.OpenFile("APIresponse.txt", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		s.logger.Error("Failed to open APIresponse.txt", "error", err)
		return
	}
	defer file.Close()

	// Write timestamp
	fmt.Fprintf(file, "=== Gemini API Response - %s ===\n\n", time.Now().Format(time.RFC3339))

	// Write the full response as JSON for complete inspection
	responseJSON, err := json.MarshalIndent(resp, "", "  ")
	if err != nil {
		s.logger.Error("Failed to marshal response to JSON", "error", err)
		fmt.Fprintf(file, "Error marshaling response: %v\n", err)
		return
	}

	fmt.Fprintf(file, "Full Response (JSON):\n%s\n\n", string(responseJSON))

	// Write detailed breakdown
	fmt.Fprintf(file, "=== Detailed Breakdown ===\n")
	fmt.Fprintf(file, "Number of Candidates: %d\n", len(resp.Candidates))

	for i, candidate := range resp.Candidates {
		fmt.Fprintf(file, "\n--- Candidate %d ---\n", i)
		fmt.Fprintf(file, "Number of Parts: %d\n", len(candidate.Content.Parts))

		for j, part := range candidate.Content.Parts {
			fmt.Fprintf(file, "\nPart %d:\n", j)
			fmt.Fprintf(file, "Text: %s\n", part.Text)
		}
	}

	s.logger.Info("API response saved to APIresponse.txt")
}

// saveRawAPIResponse saves raw API responses to APIresponse.txt for debugging
func (s *Service) saveRawAPIResponse(title string, data []byte) {
	file, err := os.OpenFile("APIresponse.txt", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		s.logger.Error("Failed to open APIresponse.txt", "error", err)
		return
	}
	defer file.Close()

	fmt.Fprintf(file, "=== %s - %s ===\n\n", title, time.Now().Format(time.RFC3339))
	fmt.Fprintf(file, "%s\n\n", string(data))
	s.logger.Info("API response saved to APIresponse.txt")
}

type interactionAudioInput struct {
	Type     string `json:"type"`
	Data     string `json:"data,omitempty"`
	URI      string `json:"uri,omitempty"`
	MIMEType string `json:"mime_type"`
}

type interactionTranscriptionConfig struct {
	Mode          string   `json:"mode,omitempty"`
	LanguageCodes []string `json:"language_codes,omitempty"`
}

type interactionGenerationConfig struct {
	TranscriptionConfig *interactionTranscriptionConfig `json:"transcription_config,omitempty"`
}

type interactionRequest struct {
	Model            string                       `json:"model"`
	Input            []interactionAudioInput      `json:"input"`
	GenerationConfig *interactionGenerationConfig `json:"generation_config,omitempty"`
}

type interactionContentPart struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type interactionStep struct {
	ID      string                   `json:"id"`
	Type    string                   `json:"type"`
	Content []interactionContentPart `json:"content,omitempty"`
}

type interactionResponse struct {
	ID         string            `json:"id"`
	Status     string            `json:"status"`
	OutputText string            `json:"output_text,omitempty"`
	Steps      []interactionStep `json:"steps,omitempty"`
	Error      interface{}       `json:"error,omitempty"`
}

// transcribeWithInteractions sends an audio transcription request to the Gemini Interactions API
func (s *Service) transcribeWithInteractions(ctx context.Context, audioData []byte, mimeType string, model string) (string, error) {
	reqBody := interactionRequest{
		Model: model,
		Input: []interactionAudioInput{
			{
				Type:     "audio",
				Data:     base64.StdEncoding.EncodeToString(audioData),
				MIMEType: mimeType,
			},
		},
		GenerationConfig: &interactionGenerationConfig{
			TranscriptionConfig: &interactionTranscriptionConfig{
				Mode: "smart",
			},
		},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal interaction request: %w", err)
	}

	url := "https://generativelanguage.googleapis.com/v1beta/interactions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create http request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-goog-api-key", s.apiKey)

	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("interactions api request failed: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	if s.apiLogs {
		s.saveRawAPIResponse("Interactions API Response", bodyBytes)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("interactions api returned status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var interactionResp interactionResponse
	if err := json.Unmarshal(bodyBytes, &interactionResp); err != nil {
		return "", fmt.Errorf("failed to unmarshal interactions response: %w", err)
	}

	if interactionResp.OutputText != "" {
		return strings.TrimSpace(interactionResp.OutputText), nil
	}

	// Extract text from steps if output_text is not directly populated
	var textBuilder strings.Builder
	for _, step := range interactionResp.Steps {
		if step.Type == "model_output" {
			for _, content := range step.Content {
				if content.Text != "" {
					textBuilder.WriteString(content.Text)
				}
			}
		}
	}

	result := strings.TrimSpace(textBuilder.String())
	if result == "" {
		return "", fmt.Errorf("empty transcription text in interactions response")
	}

	return result, nil
}

// TranscribeAudio transcribes audio data using gemini-3.5-transcribe via the Interactions API,
// with model fallback to GenerateContent if necessary.
func (s *Service) TranscribeAudio(ctx context.Context, audioData []byte, mimeType string) (string, error) {
	// Add timeout if not already set
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
	}

	// Try primary transcription model first (gemini-3.5-transcribe)
	s.logger.Info("Transcribing audio with dedicated model", "model", s.modelTranscribe)
	transcription, err := s.transcribeWithInteractions(ctx, audioData, mimeType, s.modelTranscribe)
	if err == nil && transcription != "" {
		s.logger.Info("Audio transcribed successfully with dedicated model", "model", s.modelTranscribe, "length", len(transcription))
		return transcription, nil
	}

	s.logger.Warn("Dedicated transcription failed, attempting fallback models",
		"model", s.modelTranscribe,
		"error", err)

	// Fallback path: generateContent with general-purpose models
	prompt := "Transcreva de forma precisa o áudio em anexo. Seja direto, não adicione nenhum comentário ou narração. Apenas o texto falado"

	contents := []*genai.Content{
		{
			Role: genai.RoleUser,
			Parts: []*genai.Part{
				{InlineData: &genai.Blob{MIMEType: mimeType, Data: audioData}},
				{Text: prompt},
			},
		},
	}

	config := &genai.GenerateContentConfig{
		Temperature:     genai.Ptr(float32(0.2)),
		MaxOutputTokens: 8192,
	}

	models := []string{s.model, s.modelBackup, s.modelBackup2}
	var lastErr error = err

	for i, model := range models {
		s.logger.Warn("Retrying transcription with fallback model", "model", model, "attempt", i+1)

		resp, err := s.client.Models.GenerateContent(ctx, model, contents, config)
		if err != nil {
			lastErr = fmt.Errorf("model %s failed: %w", model, err)
			s.logger.Error("Transcription failed with fallback model", "model", model, "error", err)
			continue
		}

		if len(resp.Candidates) == 0 || len(resp.Candidates[0].Content.Parts) == 0 {
			lastErr = fmt.Errorf("model %s returned empty response", model)
			s.logger.Error("Empty transcription response", "model", model)
			continue
		}

		text := strings.TrimSpace(resp.Candidates[0].Content.Parts[0].Text)
		if text == "" {
			lastErr = fmt.Errorf("model %s returned empty text", model)
			continue
		}

		s.logger.Info("Audio transcribed successfully with fallback model", "model", model, "length", len(text))
		return text, nil
	}

	return "", fmt.Errorf("all models failed to transcribe audio: %w", lastErr)
}

// Close closes the Gemini client
func (s *Service) Close() error {
	// The new genai client doesn't require explicit closing
	return nil
}
