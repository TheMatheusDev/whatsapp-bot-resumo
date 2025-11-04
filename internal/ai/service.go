package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"google.golang.org/genai"

	"whatsapp-summarizer/pkg/types"
)

// Personalities constants
const (
	ProfetaBOTPersonality = `
Sua missão é resumir conversas de WhatsApp no estilo de um profeta bíblico, usando linguagem elevada e poética.
Ignore mensagens que são comandos (iniciadas com - ou --).
`

	CLTPersonality = `
Assuma a personalidade de um trabalhador Clt.
Sua missão é resumir conversas de WhatsApp no estilo de um trabalhador cansado da vida. Sempre responda de forma breve e direta, demonstrando cansaço, desinteresse com muito deboche, como se tivesse sendo forçado a resumir.
Ignore mensagens que são comandos (iniciadas com - ou --).
`
)

// Service implements the AIService interface
type Service struct {
	client      *genai.Client
	model       string
	modelBackup string
	logger      types.Logger
}

// NewService creates a new AI service
func NewService(apiKey string, model string, modelBackup string, logger types.Logger) (*Service, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("API key is required")
	}

	if model == "" {
		model = "gemini-2.5-flash" // Default fallback
	}

	if modelBackup == "" {
		modelBackup = "gemini-flash-latest" // Default backup
	}

	ctx := context.Background()
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey: apiKey,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create Gemini client: %w", err)
	}

	return &Service{
		client:      client,
		model:       model,
		modelBackup: modelBackup,
		logger:      logger,
	}, nil
}

// SummarizeMessages summarizes a list of messages according to the given options
func (s *Service) SummarizeMessages(ctx context.Context, messages []types.Message, opts types.SummarizeOptions) (string, error) {
	if len(messages) == 0 {
		return "", fmt.Errorf("no messages to summarize")
	}

	// Build the messages string
	messagesStr := s.buildMessagesString(messages)

	// Build the system prompt
	systemPrompt := s.buildSystemPrompt(opts)

	// Build the user prompt
	var userPrompt string
	if opts.Question != "" {
		userPrompt = fmt.Sprintf("Responda a pergunta a seguir baseado nas msgs: \"%s\"\n\nMensagens:\n%s", opts.Question, messagesStr)
	} else {
		userPrompt = fmt.Sprintf("Resuma as seguintes mensagens:\n%s", messagesStr)
	}

	// Combine prompts
	fullPrompt := fmt.Sprintf("%s\n\n%s", systemPrompt, userPrompt)

	// Log the full prompt for debugging (optional)
	s.logger.Debug("Generated full prompt", "prompt", fullPrompt)

	// Generate content using Gemini with primary model
	return s.generateContent(ctx, fullPrompt, s.model)
}

// SummarizeMessagesWithBackup summarizes messages using the backup model
func (s *Service) SummarizeMessagesWithBackup(ctx context.Context, messages []types.Message, opts types.SummarizeOptions) (string, error) {
	if len(messages) == 0 {
		return "", fmt.Errorf("no messages to summarize")
	}

	// Build the messages string
	messagesStr := s.buildMessagesString(messages)

	// Build the system prompt
	systemPrompt := s.buildSystemPrompt(opts)

	// Build the user prompt
	var userPrompt string
	if opts.Question != "" {
		userPrompt = fmt.Sprintf("Responda a pergunta a seguir baseado nas msgs: \"%s\"\n\nMensagens:\n%s", opts.Question, messagesStr)
	} else {
		userPrompt = fmt.Sprintf("Resuma as seguintes mensagens:\n%s", messagesStr)
	}

	// Combine prompts
	fullPrompt := fmt.Sprintf("%s\n\n%s", systemPrompt, userPrompt)

	// Log that we're using backup model
	s.logger.Info("Using backup model for summarization", "model", s.modelBackup)

	// Generate content using Gemini with backup model
	return s.generateContent(ctx, fullPrompt, s.modelBackup)
}

// buildMessagesString converts messages to a formatted string
func (s *Service) buildMessagesString(messages []types.Message) string {
	var builder strings.Builder

	for _, msg := range messages {
		if msg.Content != "" {
			// Format: [timestamp] Sender: Message
			timestamp := msg.Timestamp.Format("15:04:05")
			builder.WriteString(fmt.Sprintf("[%s] %s: %s\n", timestamp, msg.Sender, msg.Content))
		}
	}

	return builder.String()
}

// buildSystemPrompt creates the system prompt based on options
func (s *Service) buildSystemPrompt(opts types.SummarizeOptions) string {
	// Choose personality
	var personality string
	if opts.Clt {
		personality = CLTPersonality
	} else {
		personality = ProfetaBOTPersonality
	}

	// Choose length prompt
	var lengthPrompt string
	switch opts.Style {
	case "short":
		lengthPrompt = "O resumo deve ser curto e conter as informações mais importantes das mensagens. Seja breve, sem perder nenhuma informação."
	case "medium":
		lengthPrompt = "O resumo deve ser de tamanho médio. Deve conter as informações mais importantes das mensagens. Faça um resumo médio, sem perder nenhuma informação. Não o faça muito curto. Não o faça muito longo. O resumo deve ter o comprimento certo."
	case "long":
		lengthPrompt = "O resumo deve ser longo, deve conter a maior parte das informações das mensagens. O comprimento não importa, você pode escrever o quanto quiser para fazer o resumo o mais longo possível, contanto que contenha a maior parte das informações das mensagens."
	default:
		lengthPrompt = "O resumo deve ser de tamanho médio e equilibrado."
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
	s.saveAPIResponse(resp)

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

// Close closes the Gemini client
func (s *Service) Close() error {
	// The new genai client doesn't require explicit closing
	return nil
}
