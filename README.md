# BOT de Resumos para WhatsApp

> DISCLAIMER: Projeto 90% vibe coded, feito por diversão.

Bot do WhatsApp que resume conversas usando IA Gemini.

## 🚀 Funcionalidades

- Resume mensagens individuais e em grupo
- Processamento inteligente com Google Gemini AI
- Cache de resumos para otimização
- Whitelist de usuários e grupos
- Armazenamento em SQLite

## 📋 Pré-requisitos

- Go 1.25.2+
- Conta Google Cloud com API Gemini habilitada

## ⚙️ Configuração

1. Clone o repositório
2. Use `.env.example` como base para criar um arquivo `.env` com suas configurações:
3. Instale as dependências:

```bash
go mod download
```

4. Compile o bot:

```bash
go build -o ./out/whatsapp-bot cmd/bot/main.go
```

ou use o build-android.bat para compilar para Android (execute em Termux)

Escaneie o QR code no primeiro uso para conectar ao WhatsApp.

## 📝 Comandos

São aceitos os prefixos: `--`, `-`, `/` e `!`.
**Comandos:**

- --resuma <número> → Resume mensagens do chat atual
- -r <número> → Forma abreviada
- -clt <número> → Atalho para resumo CLT
- -p <número> <pergunta> → Resume e responde uma pergunta
- --info → Informações do bot
- --version → Versão do bot

**Opções de Resumo:**

- --curto ou -c → Resumo curto
- --medio ou -m → Resumo médio
- --longo ou -l → Resumo longo
- --clt → Personalidade CLT (funciona com -r e -p)

**Exemplos:**

- -r 15 → Resumo curto de 15 mensagens
- -r 5000 --clt → Resumo com personalidade CLT de 5000 mensagens
- -p 50 Como está o humor do grupo? → Resume 50 msgs e responde a pergunta
- -p 100 --clt Teve alguma treta? → Resume com CLT e responde pergunta
- -p 200 --longo --clt Carlos surtou? → Resumo longo CLT + resposta

## 📦 Estrutura

```
cmd/bot/          - Ponto de entrada
internal/
  ├── ai/         - Integração Gemini
  ├── config/     - Configurações
  ├── database/   - SQLite
  ├── handlers/   - Processamento de mensagens
  ├── utils/      - Cache e utilitários
  └── whatsapp/   - Cliente WhatsApp
```

## 📄 Inspirado por:

- [Whatsapp-Summarizer-Bot-Go-Edition por Civermau](https://github.com/Civermau/Whatsapp-Summarizer-Bot-Go-Edition)
