# Claude AI Setup - AI Diagnostics

Este guia mostra como configurar e usar o **Claude AI (Anthropic)** como provider para AI Diagnostics.

## Por que usar Claude?

- **Excelente para análises técnicas**: Claude é especialmente bom em entender contextos complexos
- **Respostas detalhadas**: Oferece análises mais profundas que outros modelos
- **Sem limitações locais**: Não precisa de GPU ou recursos locais (ao contrário do Ollama)
- **Quota generosa**: API com limites diários adequados para uso profissional
- **Português nativo**: Excelente suporte para português brasileiro

## Modelos Disponíveis

### Claude 3.5 Sonnet (Recomendado)
```bash
claude-3-5-sonnet-20241022
```
- **Melhor custo-benefício**
- Excelente para análises técnicas de Kubernetes
- Respostas rápidas e precisas
- **Preço**: ~$3/milhão tokens input, ~$15/milhão tokens output

### Claude 3 Opus
```bash
claude-3-opus-20240229
```
- Modelo mais poderoso
- Para análises muito complexas
- Mais lento e mais caro
- **Preço**: ~$15/milhão tokens input, ~$75/milhão tokens output

### Claude 3 Haiku
```bash
claude-3-haiku-20240307
```
- Mais rápido e barato
- Para análises simples
- **Preço**: ~$0.25/milhão tokens input, ~$1.25/milhão tokens output

## Setup

### 1. Obter API Key da Anthropic

1. Acesse: https://console.anthropic.com/
2. Faça login ou crie uma conta
3. Vá em **API Keys** → **Create Key**
4. Copie a chave (começa com `sk-ant-api...`)

### 2. Configurar Variável de Ambiente (Recomendado)

```bash
# Adicionar ao ~/.bashrc ou ~/.zshrc
export CLAUDE_API_KEY="sk-ant-api03-..."
```

Ou diretamente na linha de comando:
```bash
export CLAUDE_API_KEY="sk-ant-api03-..."
```

### 3. Iniciar Servidor com Claude

#### Opção 1: Via variável de ambiente (Recomendado)
```bash
export CLAUDE_API_KEY="sk-ant-api03-..."
./build/new-k8s-hpa web --ai-provider claude
```

#### Opção 2: Via flag --claude-api-key
```bash
./build/new-k8s-hpa web --ai-provider claude --claude-api-key "sk-ant-api03-..."
```

#### Opção 3: Com modelo customizado
```bash
export CLAUDE_API_KEY="sk-ant-api03-..."
./build/new-k8s-hpa web \
  --ai-provider claude \
  --claude-model claude-3-opus-20240229
```

## Exemplo de Uso Completo

```bash
# 1. Definir API key
export CLAUDE_API_KEY="sk-ant-api03-..."

# 2. Iniciar servidor
./build/new-k8s-hpa web --ai-provider claude

# Saída esperada:
# 🤖 Inicializando AI Diagnostics System...
# ✅ AI Tokens Handler criado
# ✅ AI Diagnostics habilitado (Provider: claude)
# 🧠 Claude AI disponível (modelo: claude-3-5-sonnet-20241022)
```

## Comparação com Outros Providers

### Gemini (Google)
- ✅ **Grátis** (com quota limitada)
- ⚠️ Quota pode esgotar rápido
- ✅ Bom para testes
- ❌ Português menos natural

### Ollama (Local)
- ✅ **Grátis e ilimitado**
- ❌ Requer recursos locais (RAM, CPU)
- ⚠️ Modelos pequenos (llama3.2:3b) = análises menos detalhadas
- ✅ Privacidade total (tudo local)

### Claude (Anthropic)
- ✅ **Análises mais detalhadas**
- ✅ Excelente português
- ✅ Sem necessidade de recursos locais
- ⚠️ Pago (mas quota generosa no free tier)
- ✅ Contexto longo (200k tokens)

## Análise de Custos

### Exemplo: Pod com erro de ConfigMap

**Contexto enviado**: ~1000 tokens
**Análise retornada**: ~500 tokens

**Total**: 1500 tokens = $0.0045 com Claude 3.5 Sonnet

### Estimativa Mensal

- 100 análises/dia × 30 dias = 3000 análises/mês
- 3000 × 1500 tokens = 4.5M tokens/mês
- **Custo estimado**: ~$13.50/mês (Sonnet)

## Troubleshooting

### Erro: "claude API key not provided"
```bash
# Verificar se variável está definida
echo $CLAUDE_API_KEY

# Se vazio, definir novamente
export CLAUDE_API_KEY="sk-ant-api03-..."
```

### Erro: 401 Unauthorized
- API key inválida ou expirada
- Gerar nova chave em: https://console.anthropic.com/

### Erro: 429 Rate Limit
- Quota de requisições por minuto excedida
- Aguardar 60 segundos
- Considerar fazer upgrade do plano

### Erro: 400 Bad Request
- Verificar nome do modelo
- Usar um dos modelos listados acima

## Switching Between Providers

```bash
# Usar Claude
export CLAUDE_API_KEY="sk-ant-api03-..."
./build/new-k8s-hpa web --ai-provider claude

# Voltar para Ollama (local)
./build/new-k8s-hpa web --ai-provider ollama --ollama-model llama3.2:3b

# Usar Gemini (Google)
export GEMINI_API_KEY="AI..."
./build/new-k8s-hpa web --ai-provider gemini
```

## Multi-User Support

Cada usuário pode ter seu próprio token Claude:

1. Acesse **AI Diagnostics** → **Configurações de AI**
2. Selecione **Claude** como provider
3. Cole sua **Claude API Key**
4. Clique em **Salvar Configuração**

Agora suas análises usarão sua chave pessoal, não consumindo quota compartilhada.

## Best Practices

1. **Use variáveis de ambiente**: Mais seguro que --flag
2. **Não commite API keys**: Adicione ao .gitignore
3. **Monitore custos**: Check em https://console.anthropic.com/
4. **Use Sonnet**: Melhor custo-benefício para análises K8s
5. **Cache resultados**: Evite re-análises do mesmo problema

## Links Úteis

- **Console Anthropic**: https://console.anthropic.com/
- **Documentação API**: https://docs.anthropic.com/
- **Preços**: https://www.anthropic.com/pricing
- **Status da API**: https://status.anthropic.com/
