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
- Clang para build Android ARM64 (opcional)
- Android NDK para build Android ARM64 (opcional)

## ⚙️ Configuração

1. Clone o repositório
2. Use `.env.example` como base para criar um arquivo `.env` com suas configurações:
3. Instale as dependências:

```bash
go mod download
```

4. Compile o bot:

```bash
go build -o ./out/whatsapp-summarizer ./src/
```

ou use o `build.bat` em Windows para compilar para Android ARM64 (Termux) e Windows x86_64 (necessita do NDK/Clang para o target Android).

5. Escaneie o QR code no primeiro uso para conectar ao WhatsApp. Aproveite!

## 📝 Comandos

São aceitos os prefixos: `--`, `-`, `/` e `!`.

Ademais, quase todos os comandos possuem uma versão curta com a primeira letra, ex: `!r` para `!resuma`, `!p` para `!pergunte`. Opções de personalidade possuem comandos curtos como: `!fl` para `--farialimer`, `!z` para `--zoomer`.

**Comandos:**

- `!resuma <número>` (versão curta: `!r`) → Resume mensagens do chat atual
- `!clt <número>` → Atalho para resumo CLT
- `!farialimer <número>` (versão curta: `!fl`) → Atalho para resumo Faria Limer
- `!zoomer <número>` (versão curta: `!z`) → Atalho para resumo Zoomer
- `!pergunte <número> <pergunta>` (versão curta: `!p`) → Resume e responde uma pergunta
- `!dia` (versão curta: `!d`) → Resumo diário (desde 4h)
- `!help` (versão curta: `!h`) → Ajuda
- `!version` (versão curta: `!v`) → Versão do bot
- `!ping` → Teste de conectividade

**Opções de Resumo:**

Argumentos para comando de resumo (`!resuma`/`!pergunte`) também possuem versões curtas.

- `--curto` (versão curta: `-c`) → Resumo curto
- `--medio` (versão curta: `-m`) → Resumo médio
- `--longo` (versão curta: `-l`) → Resumo longo
- `--clt` → Personalidade CLT (funciona com -r e -p)
- `--farialimer` (versão curta: `-fl`) → Personalidade Faria Limer
- `--zoomer` (versão curta: `-z`) → Personalidade Zoomer

**Exemplos:**

- `!r 15` → Resumo de 15 mensagens com estilo padrão (CLT)
- `!r 5000 --zoomer` → Resumo de 5000 mensagens com estilo Zoomer
- `!p 50 Como está o humor do grupo?` → Responde a pergunta com base nas últimas 50 mensagens
- `!p 100 -fl Teve alguma treta?` → Responde a pergunta com estilo Faria Limer
- `!d --farialimer --longo` → Resumo longo do dia em estilo Faria Limer
- `!p 200 -l -z Carlos surtou?` → Responde a pergunta com um resumo longo e estilo Zoomer

## 📦 Estrutura

```
src/
  ├── main.go     - Ponto de entrada
  ├── ai/         - Integração Gemini
  ├── bot/        - Orquestração da aplicação
  ├── cmd/        - Processamento de comandos e eventos
  ├── config/     - Configurações
  ├── database/   - SQLite
  ├── logger/     - Logging
  ├── types/      - Interfaces e tipos compartilhados
  ├── utils/      - Cache e utilitários
  └── whatsapp/   - Cliente WhatsApp
```

## 📄 Inspirado por:

- [Whatsapp-Summarizer-Bot-Go-Edition por Civermau](https://github.com/Civermau/Whatsapp-Summarizer-Bot-Go-Edition)
