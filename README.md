# WhatsApp Summarizer Bot - Refactored Edition

Um bot WhatsApp que resume conversas usando Google Gemini AI, completamente reestruturado seguindo as melhores práticas do Go.

## 🏗️ Arquitetura

O projeto foi reestruturado seguindo padrões clean architecture e dependency injection:

```
whatsapp-summarizer/
├── cmd/bot/                    # Entry point da aplicação
├── internal/                   # Código interno da aplicação
│   ├── ai/                    # Serviço de IA (Gemini)
│   ├── config/                # Configuração
│   ├── database/              # Operações de banco de dados
│   ├── handlers/              # Handlers de eventos WhatsApp
│   ├── utils/                 # Utilitários (cache, helpers)
│   └── whatsapp/             # Serviço WhatsApp
├── pkg/types/                 # Tipos e interfaces públicas
├── configs/                   # Arquivos de configuração
└── code/                     # Código legacy (será removido)
```

## 🚀 Melhorias Implementadas

### 1. **Separação de Responsabilidades**

- Cada package tem uma responsabilidade específica
- Interfaces bem definidas para testabilidade
- Dependency injection para desacoplamento

### 2. **Configuração Externa**

- Variáveis de ambiente para configuração
- Arquivo `.env` exemplo incluído
- Validação de configurações

### 3. **Error Handling Robusto**

- Tratamento de erro consistente
- Logs estruturados
- Evitado uso de `panic()`

### 4. **Performance e Concorrência**

- Connection pooling adequado no banco
- Cache com TTL para informações de grupo
- Operações assíncronas onde apropriado

### 5. **Manutenibilidade**

- Código modular e testável
- Interfaces para mocking
- Estrutura padronizada

## ⚙️ Configuração

1. **Copie o arquivo de exemplo:**

```bash
cp configs/.env.example .env
```

2. **Configure as variáveis:**

```env
GEMINI_API_KEY=sua_chave_aqui
OWNER_JID=seu_numero_aqui
USER_WHITELIST=num1,num2,num3
GROUP_WHITELIST=group1,group2
```

## 🔧 Compilação e Execução

```bash
# Compilar
go build -o bot ./cmd/bot

# Executar
./bot

# Ou diretamente
go run ./cmd/bot
```

## 📁 Estrutura dos Packages

### `pkg/types`

Define interfaces e tipos compartilhados:

- `AIService`: Interface para serviços de IA
- `DatabaseService`: Interface para operações de BD
- `WhatsAppService`: Interface para WhatsApp
- `Bot`: Interface principal do bot

### `internal/config`

Gerenciamento de configuração:

- Carregamento de variáveis de ambiente
- Validação de configurações
- Estruturas de config tipadas

### `internal/ai`

Serviço de IA usando Gemini:

- Geração de resumos
- Múltiplas personalidades (ProfetaBOT/CLT)
- Controle de tamanho de resumo

### `internal/database`

Operações de banco de dados:

- Prepared statements para performance
- Connection pooling
- Separação grupo/mensagem direta

### `internal/whatsapp`

Integração WhatsApp:

- Autenticação via QR code
- Envio de mensagens
- Gerenciamento de conexão

### `internal/handlers`

Processamento de eventos:

- Parsing de comandos
- Autorização de usuários
- Geração de resumos
- Funcionalidade @everyone para grupos

## 🔄 Migração do Código Legacy

O código original está em `/code` e será gradualmente removido. A nova estrutura mantém compatibilidade funcional.

## 🧪 Testing

A nova estrutura permite testing robusto:

```bash
# Rodar todos os testes
go test ./...

# Com coverage
go test -cover ./...
```

## 📈 Próximos Passos

1. **Testes Unitários**: Implementar testes para cada package
2. **Métricas**: Adicionar Prometheus metrics
3. **CI/CD**: Setup pipeline de integração
4. **Docker**: Containerização da aplicação
5. **Graceful Shutdown**: Implementação completa

## 🤝 Contribuição

Com a nova estrutura, contribuições são mais fáceis:

1. Fork o projeto
2. Crie uma branch para sua feature
3. Siga as interfaces definidas
4. Adicione testes
5. Submeta um PR

## 📝 Comandos do Bot

### Comandos de Resumo:

- `--resuma <num>` ou `-r <num>`: Resume últimas N mensagens
- `--info` ou `-i`: Informações do bot
- `--help` ou `-h`: Ajuda
- `--version` ou `-v`: Versão

### Opções de Resumo:

- `--curto` ou `-c`: Resumo curto
- `--medio` ou `-m`: Resumo médio (padrão)
- `--longo` ou `-l`: Resumo longo
- `--clt`: Usa personalidade CLT

### Funcionalidades Especiais:

- `@everyone`, `@todos` ou `@here`: Menciona todos os membros do grupo
  - Funciona automaticamente quando detectado em qualquer mensagem
  - Disponível apenas em grupos
  - Registra a ação no banco de dados

## 🔒 Segurança

- API keys via variáveis de ambiente
- Whitelist de usuários/grupos
- Validação de entrada
- Rate limiting (planejado)

---

**Mantido por:** Matheus Araújo  
**Versão:** 2.0 - Refactored Edition
