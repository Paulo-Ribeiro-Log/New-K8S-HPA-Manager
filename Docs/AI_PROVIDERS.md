# AI Diagnostics - Providers Suportados

O sistema AI Diagnostics suporta **3 providers**:

## 1. 🟦 Claude (Anthropic) - **RECOMENDADO**

### Quando usar
- ✅ **Análises de produção**: Ambiente profissional
- ✅ **Problemas complexos**: Contextos grandes com muitos eventos
- ✅ **Português de qualidade**: Respostas naturais em PT-BR
- ✅ **Sem hardware local**: Não precisa de GPU/RAM

### Setup Rápido
```bash
# 1. Obter API key em: https://console.anthropic.com/
export CLAUDE_API_KEY="sk-ant-api03-..."

# 2. Iniciar servidor
./build/new-k8s-hpa web --ai-provider claude
```

### Modelos
- **claude-3-5-sonnet-20241022** (padrão) - Melhor custo-benefício
- **claude-3-opus-20240229** - Mais poderoso
- **claude-3-haiku-20240307** - Mais rápido e barato

### Custos
- **Sonnet**: ~$0.0045 por análise (estimativa)
- **Free tier**: Quota generosa para desenvolvimento
- **Produção**: ~$13.50/mês para 100 análises/dia

📖 **Guia completo**: [CLAUDE_AI_SETUP.md](CLAUDE_AI_SETUP.md)

---

## 2. 🟩 Ollama - **Grátis e Local**

### Quando usar
- ✅ **Desenvolvimento**: Testes ilimitados sem custos
- ✅ **Privacidade**: Dados não saem do ambiente local
- ✅ **Offline**: Funciona sem internet
- ⚠️ **Requer recursos**: 2-8GB RAM por modelo

### Setup Rápido
```bash
# 1. Instalar Ollama
curl -fsSL https://ollama.com/install.sh | sh

# 2. Baixar modelo
ollama pull llama3.2:3b  # 2GB - Básico
ollama pull llama3.2:7b  # 5.5GB - Melhor qualidade

# 3. Iniciar servidor
./build/new-k8s-hpa web \
  --ai-provider ollama \
  --ollama-model llama3.2:3b
```

### Modelos Recomendados
- **llama3.2:3b** (2GB) - Análises básicas, rápido
- **llama3.2:7b** (5.5GB) - Melhor qualidade
- **deepseek-coder:6.7b** (3.8GB) - Focado em código

### Limitações
- ⚠️ Análises menos detalhadas que Claude
- ⚠️ Português menos natural
- ⚠️ Contextos grandes podem sobrecarregar modelos pequenos

---

## 3. 🔵 Gemini (Google) - **Testes**

### Quando usar
- ✅ **Protótipos**: Desenvolvimento inicial
- ✅ **Demos**: Apresentações e testes
- ⚠️ **Quota limitada**: 50 requisições/dia (free tier)

### Setup Rápido
```bash
# 1. Obter API key em: https://aistudio.google.com/app/apikey
export GEMINI_API_KEY="AIza..."

# 2. Iniciar servidor
./build/new-k8s-hpa web --ai-provider gemini
```

### Modelos
- **gemini-2.0-flash-exp** (padrão) - Mais rápido
- **gemini-pro** - Mais robusto

### Limitações
- ⚠️ Quota diária pode esgotar rapidamente
- ⚠️ Não recomendado para produção
- ⚠️ Português aceitável mas não nativo

---

## Comparação Rápida

| Feature | Claude | Ollama | Gemini |
|---------|--------|--------|--------|
| **Qualidade** | ⭐⭐⭐⭐⭐ | ⭐⭐⭐ | ⭐⭐⭐⭐ |
| **Português** | ⭐⭐⭐⭐⭐ | ⭐⭐⭐ | ⭐⭐⭐⭐ |
| **Custo** | 💰 Pago | 🆓 Grátis | 🆓 Limited |
| **Velocidade** | ⚡⚡⚡⚡ | ⚡⚡⚡ | ⚡⚡⚡⚡⚡ |
| **Setup** | Fácil | Médio | Fácil |
| **Hardware** | Nenhum | RAM 2-8GB | Nenhum |
| **Privacidade** | Cloud | ⭐⭐⭐⭐⭐ Local | Cloud |
| **Produção** | ✅ Sim | ⚠️ Depende | ❌ Não |

---

## Recomendações por Cenário

### 🏢 Produção (Empresa)
**Use Claude 3.5 Sonnet**
- Análises detalhadas e confiáveis
- Custo previsível
- Português profissional
- Suporte empresarial disponível

### 🧪 Desenvolvimento
**Use Ollama (llama3.2:3b)**
- Testes ilimitados
- Sem custos
- Privacidade total
- Bom para iterações rápidas

### 🎓 Aprendizado/Demos
**Use Gemini**
- Setup rápido
- Grátis (com limites)
- Boa qualidade
- Ideal para apresentações

### 💼 Startup/Budget
**Use Ollama (llama3.2:7b)**
- Zero custos operacionais
- Boa qualidade com modelo maior
- Escalável (basta adicionar RAM)

---

## Multi-User Configuration

Cada usuário pode configurar seu próprio provider no frontend:

1. **Acesse**: AI Diagnostics → ⚙️ Configurações de AI
2. **Selecione**: Provider desejado (Claude/Gemini/Ollama)
3. **Configure**: API Key pessoal (se aplicável)
4. **Salve**: Configuração armazenada no SQLite

**Benefícios:**
- ✅ Quota individual (não compartilhada)
- ✅ Configuração por usuário
- ✅ Fallback automático para provider padrão do servidor

---

## Switching Providers

```bash
# Claude (Produção)
export CLAUDE_API_KEY="sk-ant-api03-..."
./build/new-k8s-hpa web --ai-provider claude

# Ollama (Desenvolvimento)
./build/new-k8s-hpa web \
  --ai-provider ollama \
  --ollama-model llama3.2:7b

# Gemini (Testes)
export GEMINI_API_KEY="AIza..."
./build/new-k8s-hpa web --ai-provider gemini
```

---

## Troubleshooting

### Claude: "API key not provided"
```bash
# Verificar variável
echo $CLAUDE_API_KEY

# Se vazio, definir
export CLAUDE_API_KEY="sk-ant-api03-..."
```

### Ollama: "connection refused"
```bash
# Verificar se Ollama está rodando
systemctl status ollama
# Ou
ollama list

# Iniciar se necessário
ollama serve
```

### Gemini: "429 Quota exceeded"
- Quota diária esgotada (50 requisições/dia)
- Aguardar 24h ou fazer upgrade do plano
- Considerar migrar para Claude ou Ollama

---

## Links Úteis

### Claude
- **Console**: https://console.anthropic.com/
- **Docs**: https://docs.anthropic.com/
- **Pricing**: https://www.anthropic.com/pricing

### Ollama
- **Site**: https://ollama.com/
- **Models**: https://ollama.com/library
- **GitHub**: https://github.com/ollama/ollama

### Gemini
- **Console**: https://aistudio.google.com/
- **Docs**: https://ai.google.dev/docs
- **Pricing**: https://ai.google.dev/pricing
