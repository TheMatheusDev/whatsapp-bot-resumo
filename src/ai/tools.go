package ai

import "google.golang.org/genai"

// GetChatbotTools returns the list of tools (function declarations)
// available to the Gemini model during conversational interactions.
func GetChatbotTools() []*genai.Tool {
	emptySchema := &genai.Schema{
		Type:       genai.TypeObject,
		Properties: map[string]*genai.Schema{},
	}

	return []*genai.Tool{
		{
			FunctionDeclarations: []*genai.FunctionDeclaration{
				{
					Name:        "summarize_messages",
					Description: "Resume as mensagens recentes da conversa. Use quando o usuário pedir expressamente para resumir mensagens recentes, conversas anteriores ou saber o que aconteceu no chat.",
					Parameters: &genai.Schema{
						Type: genai.TypeObject,
						Properties: map[string]*genai.Schema{
							"count": {
								Type:        genai.TypeInteger,
								Description: "Número de mensagens a serem resumidas. Padrão: 700. Mínimo: 5, Máximo: 9000.",
							},
							"style": {
								Type:        genai.TypeString,
								Description: "Tamanho e estilo do resumo: 'curto', 'medio' ou 'longo'. Padrão: 'medio'.",
								Enum:        []string{"curto", "medio", "longo"},
							},
							"personality": {
								Type:        genai.TypeString,
								Description: "Personalidade/estilo de escrita: 'clt', 'farialimer', 'zoomer', 'profeta' ou 'resumobot'.",
								Enum:        []string{"clt", "farialimer", "zoomer", "profeta", "resumobot"},
							},
						},
					},
				},
				{
					Name:        "get_daily_summary",
					Description: "Gera o resumo de tudo o que foi conversado no dia de hoje (desde as 4h da manhã). Use quando o usuário pedir o resumo do dia de hoje ou perguntar o que rolou hoje.",
					Parameters: &genai.Schema{
						Type: genai.TypeObject,
						Properties: map[string]*genai.Schema{
							"style": {
								Type:        genai.TypeString,
								Description: "Tamanho e estilo do resumo: 'curto', 'medio' ou 'longo'. Padrão: 'medio'.",
								Enum:        []string{"curto", "medio", "longo"},
							},
							"personality": {
								Type:        genai.TypeString,
								Description: "Personalidade/estilo de escrita: 'clt', 'farialimer', 'zoomer', 'profeta' ou 'resumobot'.",
								Enum:        []string{"clt", "farialimer", "zoomer", "profeta", "resumobot"},
							},
						},
					},
				},
				{
					Name:        "ask_chat_history",
					Description: "Responde a uma pergunta específica buscando e analisando o histórico de mensagens da conversa. Use quando o usuário fizer uma pergunta sobre algo que foi falado no grupo/chat que exige busca em mensagens anteriores.",
					Parameters: &genai.Schema{
						Type:     genai.TypeObject,
						Required: []string{"question"},
						Properties: map[string]*genai.Schema{
							"question": {
								Type:        genai.TypeString,
								Description: "A pergunta a ser respondida com base no histórico de mensagens.",
							},
							"message_count": {
								Type:        genai.TypeInteger,
								Description: "Quantidade de mensagens anteriores para analisar (padrão: 700, máximo: 9000).",
							},
						},
					},
				},
				{
					Name:        "get_weekly_ranking",
					Description: "Gera o ranking de mensagens dos participantes mais ativos nos últimos 7 dias. Use quando o usuário perguntar quem mais falou na semana ou pedir o ranking semanal.",
					Parameters:  emptySchema,
				},
				{
					Name:        "get_monthly_ranking",
					Description: "Gera o ranking de mensagens dos participantes mais ativos nos últimos 30 dias. Use quando o usuário perguntar quem mais falou no mês ou pedir o ranking mensal.",
					Parameters:  emptySchema,
				},
				{
					Name:        "get_group_rules",
					Description: "Consulta e exibe as regras oficiais do grupo atual. Use quando perguntarem sobre regras, condutas, normas ou proibições do grupo.",
					Parameters:  emptySchema,
				},
				{
					Name:        "create_sticker",
					Description: "Cria uma figurinha (sticker) do WhatsApp a partir da imagem, vídeo ou GIF que foi enviado ou citado pelo usuário. Use quando o usuário pedir para criar figurinha ou sticker.",
					Parameters:  emptySchema,
				},
				{
					Name:        "list_personalities",
					Description: "Lista todas as personalidades e estilos disponíveis no bot (ex: CLT, Faria Limer, Zoomer, Profeta, ResumoBOT).",
					Parameters:  emptySchema,
				},
				{
					Name:        "get_bot_status",
					Description: "Verifica a conectividade, latência e versão do bot (ping/status). Use quando o usuário testar se o bot está vivo/online ou perguntar sua versão.",
					Parameters:  emptySchema,
				},
			},
		},
	}
}
