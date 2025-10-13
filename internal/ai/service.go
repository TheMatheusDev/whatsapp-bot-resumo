package ai

import (
	"context"
	"fmt"
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
Compreenda gírias e abreviações da internet brasileira.
`

	CLTPersonality = `
Assuma a personalidade de um trabalhador Clt.
Sua missão é resumir conversas de WhatsApp no estilo de um trabalhador cansado da vida. Sempre responda de forma breve e direta, demonstrando cansaço e desinteresse com uma pitada de deboche.
Ignore mensagens que são comandos (iniciadas com - ou --).
Compreenda gírias e abreviações da internet brasileira.
`
)

// Service implements the AIService interface
type Service struct {
	client *genai.Client
	model  string
	logger types.Logger
}

// NewService creates a new AI service
func NewService(apiKey string, model string, logger types.Logger) (*Service, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("API key is required")
	}

	if model == "" {
		model = "gemini-2.5-flash" // Default fallback
	}

	ctx := context.Background()
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey: apiKey,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create Gemini client: %w", err)
	}

	return &Service{
		client: client,
		model:  model,
		logger: logger,
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
	userPrompt := fmt.Sprintf("Resuma as seguintes mensagens:\n%s", messagesStr)

	// Combine prompts
	fullPrompt := fmt.Sprintf("%s\n\n%s", systemPrompt, userPrompt)

	// Log the full prompt for debugging (optional)
	s.logger.Debug("Generated full prompt", "prompt", fullPrompt)

	// Generate content using Gemini
	return s.generateContent(ctx, fullPrompt)
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

	return fmt.Sprintf("%s\n%s", personality, lengthPrompt)
}

// generateContent calls the Gemini API to generate content
func (s *Service) generateContent(ctx context.Context, prompt string) (string, error) {
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
	resp, err := s.client.Models.GenerateContent(ctx, s.model, contents, config)
	if err != nil {
		s.logger.Error("Failed to generate content", "error", err)
		return "", fmt.Errorf("failed to generate content: %w", err)
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

// Close closes the Gemini client
func (s *Service) Close() error {
	// The new genai client doesn't require explicit closing
	return nil
}
