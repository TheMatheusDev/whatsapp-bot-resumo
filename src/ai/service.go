package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"google.golang.org/genai"

	"whatsapp-summarizer/src/types"
)

// Service implements the AIService interface
type Service struct {
	client       *genai.Client
	model        string
	modelBackup  string
	modelBackup2 string
	apiLogs      bool
	logger       types.Logger
	timezone     *time.Location // used to format message timestamps sent to the AI
}

// NewService creates a new AI service.
// timezone is an IANA timezone name (e.g. "America/Sao_Paulo") used to
// convert message timestamps to local time before sending them to the API.
// An empty or invalid value falls back to UTC.
func NewService(apiKey string, model string, modelBackup string, modelBackup2 string, apiLogs bool, logger types.Logger, timezone string) (*Service, error) {
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

	loc, err := time.LoadLocation(timezone)
	if err != nil {
		logger.Warn("AI service: invalid timezone, falling back to UTC", "timezone", timezone, "error", err)
		loc = time.UTC
	}

	ctx := context.Background()
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey: apiKey,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create Gemini client: %w", err)
	}

	return &Service{
		client:       client,
		model:        model,
		modelBackup:  modelBackup,
		modelBackup2: modelBackup2,
		apiLogs:      apiLogs,
		logger:       logger,
		timezone:     loc,
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
		result, err := s.summarize(ctx, messages, opts, model)
		if err == nil {
			return result, nil
		}
		lastErr = err
	}
	return "", fmt.Errorf("all models failed to summarize: %w", lastErr)
}

// summarize builds the prompt from messages+opts and calls the Gemini API with
// the given model name. It is the single shared implementation used by
// SummarizeMessages for all retry attempts.
func (s *Service) summarize(ctx context.Context, messages []types.Message, opts types.SummarizeOptions, model string) (string, error) {
	messagesStr := s.buildMessagesString(messages)
	systemPrompt := s.buildSystemPrompt(opts)

	var userPrompt string
	if opts.Question != "" {
		userPrompt = fmt.Sprintf("Responda a pergunta a seguir baseado nas msgs: \"%s\"\n\nMensagens:\n%s", opts.Question, messagesStr)
	} else {
		userPrompt = fmt.Sprintf("Resuma as seguintes mensagens:\n%s", messagesStr)
	}

	fullPrompt := fmt.Sprintf("%s\n\n%s", systemPrompt, userPrompt)
	s.logger.Debug("Generated full prompt", "prompt", fullPrompt)

	return s.generateContent(ctx, fullPrompt, model)
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

// buildSystemPrompt creates the system prompt based on options
func (s *Service) buildSystemPrompt(opts types.SummarizeOptions) string {
	// Choose personality
	var personality string
	switch opts.Personality {
	case "profeta":
		personality = ProfetaBOTPersonality
	case "farialimer":
		personality = FariaLimerPersonality
	case "zoomer":
		personality = ZoomerPersonality
	case "clt":
		fallthrough
	default:
		personality = CLTPersonality
	}

	// Choose length prompt
	var lengthPrompt string
	switch opts.Style {
	case "short":
		lengthPrompt = ShortLengthPrompt
	case "medium":
		lengthPrompt = MediumLengthPrompt
	case "long":
		lengthPrompt = LongLengthPrompt
	default:
		lengthPrompt = ShortLengthPrompt
	}

	// Add question-specific instructions if a question is provided
	var questionPrompt string
	if opts.Question != "" {
		questionPrompt = "\n\nVocê deve responder SOMENTE à pergunta fornecida pelo usuário. NÃO faça um resumo das mensagens. A resposta deve ser baseada nas mensagens fornecidas e ser direta e objetiva, respondendo especificamente o que foi perguntado."
	}

	return fmt.Sprintf("%s\n%s%s", personality, lengthPrompt, questionPrompt)
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

// TranscribeAudio transcribes audio data using the Gemini API with model fallback
func (s *Service) TranscribeAudio(ctx context.Context, audioData []byte, mimeType string) (string, error) {
	prompt := "Transcreva de forma precisa o áudio em anexo. Seja direto, não adicione nenhum comentário ou narração. Apenas o texto falado"

	// Build content with audio inline data
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

	// Try primary model
	models := []string{s.model, s.modelBackup, s.modelBackup2}
	var lastErr error

	for i, model := range models {
		if i == 0 {
			s.logger.Info("Transcribing audio with primary model", "model", model)
		} else {
			s.logger.Warn("Retrying transcription with fallback model", "model", model, "attempt", i+1)
		}

		resp, err := s.client.Models.GenerateContent(ctx, model, contents, config)
		if err != nil {
			lastErr = fmt.Errorf("model %s failed: %w", model, err)
			s.logger.Error("Transcription failed with model", "model", model, "error", err)
			continue
		}

		if len(resp.Candidates) == 0 || len(resp.Candidates[0].Content.Parts) == 0 {
			lastErr = fmt.Errorf("model %s returned empty response", model)
			s.logger.Error("Empty transcription response", "model", model)
			continue
		}

		text := resp.Candidates[0].Content.Parts[0].Text
		if text == "" {
			lastErr = fmt.Errorf("model %s returned empty text", model)
			continue
		}

		s.logger.Info("Audio transcribed successfully", "model", model, "length", len(text))
		return text, nil
	}

	return "", fmt.Errorf("all models failed to transcribe audio: %w", lastErr)
}

// Close closes the Gemini client
func (s *Service) Close() error {
	// The new genai client doesn't require explicit closing
	return nil
}
