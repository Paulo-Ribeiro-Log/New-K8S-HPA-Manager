# Estudo: Comparação de Releases GitHub com Análise de IA

**Data**: 2026-01-11
**Versão**: 1.0
**Autor**: Sistema K8s HPA Manager

---

## 📋 Sumário Executivo

Este documento descreve o estudo e planejamento para implementação de uma nova aba "GitHub Releases" no K8s HPA Manager, permitindo comparar releases de repositórios GitHub com análise automatizada de impacto usando IA.

**Principais Funcionalidades**:
1. **Comparação de Releases**: Visualizar diferenças entre duas tags (base vs head)
2. **Filtro de Extensões**: Focar apenas em arquivos relevantes (.yaml, Dockerfile, etc)
3. **Análise de IA**: Avaliar automaticamente o impacto das mudanças na infraestrutura
4. **Token Individual**: Cada analista usa seu próprio token GitHub

**Motivação**: Facilitar a análise de mudanças antes de promover uma release para produção, identificando automaticamente riscos potenciais em configurações Kubernetes e Dockerfiles.

---

## 🎯 Problema a Resolver

### Cenário Atual
Os analistas precisam revisar manualmente releases candidatas antes de promover para produção, verificando:
- Commits realizados entre versões
- Arquivos alterados (especialmente YAMLs Kubernetes)
- Impacto potencial em recursos de cluster
- Breaking changes em Dockerfiles

**Dificuldades**:
- ❌ Processo manual e demorado
- ❌ Fácil perder mudanças críticas em meio a muitos commits
- ❌ Difícil avaliar impacto de mudanças em YAML Kubernetes
- ❌ Não há visão consolidada de riscos

### Solução Proposta
Aba dedicada que:
- ✅ Automatiza comparação de releases via GitHub API
- ✅ Filtra apenas arquivos relevantes (YAML, Dockerfile)
- ✅ Usa IA para avaliar impacto de cada mudança
- ✅ Classifica riscos (alto/médio/baixo) automaticamente
- ✅ Gera recomendações de ação

**Exemplo de URL de comparação**:
`https://github.com/viavarejo-internal/vv-retira-geolocalizacao/compare/2.5.5-2...2.5.8-1`

---

## 🏗️ Arquitetura Técnica

### Visão Geral

```
┌─────────────────────────────────────────────────────────────┐
│                     GitHubReleasesTab                       │
│  ┌───────────────────┐  ┌──────────────────────────────┐  │
│  │  Painel Esquerdo  │  │    Painel Direito            │  │
│  │  ─────────────    │  │    ─────────────             │  │
│  │  • Select Repo    │  │  📊 Tabs:                    │  │
│  │  • Auto-detect    │  │    - Commits                 │  │
│  │  • Select Base    │  │    - Arquivos (+ Filtro)     │  │
│  │  • Select Head    │  │    - Release Notes           │  │
│  │  • Botão Compare  │  │    - Análise IA              │  │
│  │  • Botão Settings │  │    - Mapa Versões (NOVO)     │  │
│  └───────────────────┘  └──────────────────────────────┘  │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
                    ┌──────────────────┐
                    │   API Backend    │
                    │   (Go + Gin)     │
                    └──────────────────┘
                              │
        ┌─────────────────────┼─────────────────────┐
        │                     │                     │
        ▼                     ▼                     ▼
┌──────────────┐      ┌──────────────┐     ┌──────────────┐
│ Deployment   │      │ GitHub API   │     │ AI Analyzer  │
│ Registry DB  │◄─────┤ Client       │     │ + Token      │
│ (SQLite)     │      └──────────────┘     │ Store        │
└──────────────┘              │            └──────────────┘
        ▲                     │
        │                     │
        │           ┌─────────▼──────────┐
        └───────────┤ Health Checking    │
                    │ (Auto-populate)    │
                    └────────────────────┘
```

### Componentes Principais

#### 1. Frontend (React/TypeScript)

**Arquivo**: `internal/web/frontend/src/components/GitHubReleasesTab.tsx`

**Estrutura**:
```tsx
GitHubReleasesTab
├── Painel Esquerdo (1/3 da tela)
│   ├── Select de Repositório
│   ├── Select Base Tag (Produção)
│   ├── Select Head Tag (Candidato)
│   ├── Botão "Comparar Releases"
│   └── Botão "⚙️ Configurar Token"
│
└── Painel Direito (2/3 da tela)
    └── Tabs Component
        ├── Tab "Commits" - Lista de commits com autor, data, mensagem
        ├── Tab "Arquivos" - Tabela + Filtro de extensão + Análise IA
        ├── Tab "Release Notes" - Grid 2 colunas (base vs head)
        └── Tab "Análise IA" - ⚠️ NOVO - Categorização de riscos
```

**Componentes Filhos**:
- `CommitCard.tsx` - Card de commit individual
- `FileChangeTable.tsx` - Tabela de arquivos alterados
- `AIAnalysisPanel.tsx` - Painel de análise de IA
- `TokenConfigModal.tsx` - Modal de configuração de token

#### 2. Backend (Go)

**Handler**: `internal/web/handlers/github_releases.go`

**Endpoints REST**:
```go
GET  /api/v1/github/repos
     → Listar repositórios configurados

GET  /api/v1/github/repos/:owner/:repo/releases
     → Listar releases (tags) de um repositório

GET  /api/v1/github/repos/:owner/:repo/compare/:base...:head
     → Comparar duas releases
     → Retorna: commits, files_changed, release_notes

POST /api/v1/github/repos/:owner/:repo/analyze
     → Analisar impacto com IA (NOVO)
     → Body: { base_tag, head_tag }
     → Retorna: AIImpactAnalysis

GET  /api/v1/github/token/status
     → Status do token atual (válido/inválido, rate limit)

POST /api/v1/github/token
     → Salvar/atualizar token GitHub
     → Body: { token }
```

**Módulos**:
- `internal/storage/github_tokens.go` - Persistência de tokens (SQLite + criptografia)
- `internal/ai/github_analyzer.go` - Análise de impacto com IA

#### 3. GitHub API Client

**Reutilização**: `internal/updater/github.go` já possui client configurado.

**Novas Funções**:
```go
// CompareReleases - Compara duas tags
func (c *GitHubClient) CompareReleases(owner, repo, base, head string) (*Comparison, error) {
    comparison, _, err := c.client.Repositories.CompareCommits(
        ctx, owner, repo, base, head, &github.ListOptions{PerPage: 100},
    )
    return parseComparison(comparison), err
}

// GetReleaseNotes - Busca release notes de uma tag
func (c *GitHubClient) GetReleaseNotes(owner, repo, tag string) (string, error) {
    release, _, err := c.client.Repositories.GetReleaseByTag(ctx, owner, repo, tag)
    return release.GetBody(), err
}

// GetFilePatch - Busca diff completo de um arquivo
func (c *GitHubClient) GetFilePatch(owner, repo, base, head, filename string) (string, error) {
    comparison, _, err := c.client.Repositories.CompareCommits(
        ctx, owner, repo, base, head, nil,
    )
    for _, file := range comparison.Files {
        if file.GetFilename() == filename {
            return file.GetPatch(), nil
        }
    }
    return "", errors.New("file not found in comparison")
}
```

---

## 🔐 Segurança: Sistema de Token Individual

### Problema
Cada analista tem seu próprio token GitHub pessoal para acessar repositórios privados. O sistema precisa:
- Armazenar tokens de forma segura
- Autenticar por usuário (não global)
- Não expor tokens em logs ou responses HTTP

### Solução: SQLite + Criptografia AES-256

**Storage**: `~/.k8s-hpa-manager/github-tokens.db`

**Schema**:
```sql
CREATE TABLE github_tokens (
    user_email  TEXT PRIMARY KEY,     -- Email do analista (Azure AD)
    token       TEXT NOT NULL,        -- Token criptografado com AES-256-GCM
    created_at  TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at  TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
```

**Criptografia**:
```go
// internal/storage/github_tokens.go

// Chave derivada de secret local
var secretKey = loadOrGenerateSecret("~/.k8s-hpa-manager/.secret")

func encrypt(plaintext string) (string, error) {
    block, _ := aes.NewCipher(secretKey)
    gcm, _ := cipher.NewGCM(block)
    nonce := make([]byte, gcm.NonceSize())
    io.ReadFull(rand.Reader, nonce)

    ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
    return base64.StdEncoding.EncodeToString(ciphertext), nil
}

func decrypt(ciphertext string) (string, error) {
    data, _ := base64.StdEncoding.DecodeString(ciphertext)
    block, _ := aes.NewCipher(secretKey)
    gcm, _ := cipher.NewGCM(block)

    nonceSize := gcm.NonceSize()
    nonce, ciphertext := data[:nonceSize], data[nonceSize:]
    plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
    return string(plaintext), err
}
```

**Segurança**:
- ✅ AES-256-GCM (Galois/Counter Mode) - autenticação + criptografia
- ✅ Chave de 32 bytes derivada de arquivo `.secret` local
- ✅ Permissões 600 no `.secret` (apenas owner)
- ✅ Nonce aleatório por criptografia (evita replay attacks)
- ✅ Autenticação por usuário via Azure AD email

### UI de Configuração

**Modal de Token**:
```tsx
<Dialog open={showTokenConfig}>
  <DialogContent>
    <DialogHeader>
      <DialogTitle>Configuração do Token GitHub</DialogTitle>
      <DialogDescription>
        Configure seu token pessoal para acessar repositórios privados
      </DialogDescription>
    </DialogHeader>

    {/* Input do Token */}
    <Input
      type="password"
      placeholder="ghp_xxxxxxxxxxxx"
      value={githubToken}
      onChange={(e) => setGithubToken(e.target.value)}
    />

    {/* Status do Token */}
    {tokenStatus?.valid ? (
      <Alert>
        <CheckCircle2 />
        <AlertTitle>Token Válido</AlertTitle>
        <AlertDescription>
          Autenticado como: {tokenStatus.username}
          Rate Limit: {tokenStatus.remaining}/{tokenStatus.limit}
        </AlertDescription>
      </Alert>
    ) : (
      <Alert variant="destructive">
        <XCircle />
        <AlertTitle>Token Inválido</AlertTitle>
      </Alert>
    )}

    {/* Instruções */}
    <Alert>
      <Info />
      <AlertTitle>Como obter um token?</AlertTitle>
      <AlertDescription>
        1. Acesse github.com/settings/tokens
        2. Clique em "Generate new token (classic)"
        3. Selecione escopo "repo" (acesso total)
        4. Copie o token e cole acima
      </AlertDescription>
    </Alert>

    <Button onClick={handleSaveToken}>Salvar Token</Button>
  </DialogContent>
</Dialog>
```

---

## 🎯 Filtro de Extensões

### Problema
Em uma release típica, pode haver centenas de arquivos alterados (.go, .ts, .md, etc). Para análise de impacto em infraestrutura, apenas alguns tipos são relevantes:
- `.yaml`, `.yml` - Kubernetes manifests
- `Dockerfile` - Container images
- `.go` - Código backend (se mudanças em lógica crítica)

### Solução: Select com Filtros Predefinidos

**Frontend**:
```tsx
const [fileFilter, setFileFilter] = useState('all');

const filteredFiles = useMemo(() => {
  if (fileFilter === 'all') return comparison.files_changed;

  return comparison.files_changed.filter(file => {
    if (fileFilter === '.yaml') {
      return file.filename.endsWith('.yaml') || file.filename.endsWith('.yml');
    }
    if (fileFilter === 'Dockerfile') {
      return file.filename.includes('Dockerfile');
    }
    return file.filename.endsWith(fileFilter);
  });
}, [comparison, fileFilter]);

return (
  <Select value={fileFilter} onValueChange={setFileFilter}>
    <SelectTrigger className="w-[200px]">
      <SelectValue />
    </SelectTrigger>
    <SelectContent>
      <SelectItem value="all">Todos os Arquivos</SelectItem>
      <SelectItem value=".yaml">.yaml (Kubernetes Manifests)</SelectItem>
      <SelectItem value=".yml">.yml (Kubernetes Manifests)</SelectItem>
      <SelectItem value="Dockerfile">Dockerfile (Container Images)</SelectItem>
      <SelectItem value=".go">Go Files</SelectItem>
      <SelectItem value=".ts">.ts/.tsx (TypeScript)</SelectItem>
      <SelectItem value=".md">Markdown</SelectItem>
    </SelectContent>
  </Select>
);
```

**Backend**: Adicionar campo `extension` ao struct `GitHubFile`:
```go
type GitHubFile struct {
    Filename  string `json:"filename"`
    Extension string `json:"extension"`  // .yaml, .go, Dockerfile, etc
    // ... outros campos
}

func parseComparison(comparison *github.CommitsComparison) *Comparison {
    files := make([]GitHubFile, 0, len(comparison.Files))
    for _, file := range comparison.Files {
        ext := filepath.Ext(file.GetFilename())
        if ext == "" && strings.Contains(file.GetFilename(), "Dockerfile") {
            ext = "Dockerfile"
        }

        files = append(files, GitHubFile{
            Filename:  file.GetFilename(),
            Extension: ext,
            // ...
        })
    }
    return &Comparison{FilesChanged: files}
}
```

---

## 🤖 Análise de IA: Avaliação de Impacto

### Objetivo
Usar IA (Claude ou Ollama local) para analisar automaticamente cada arquivo alterado e avaliar:
1. **Nível de risco** (low/medium/high)
2. **Descrição** do que foi alterado
3. **Recomendação** de ação (se aplicável)
4. **Categoria** (infra/k8s/app)

### Integração com AI Diagnostics Existente

**Reutilização**: `internal/ai/analyzer.go` e `internal/ai/providers/`

**Novo Módulo**: `internal/ai/github_analyzer.go`

```go
package ai

type GitHubAnalyzer struct {
    provider Provider  // Claude ou Ollama
}

// AnalyzeFileChange - Analisa diff de um arquivo
func (a *GitHubAnalyzer) AnalyzeFileChange(file GitHubFile, patch string) (*FileAIAnalysis, error) {
    // 1. Categorizar arquivo por extensão
    category := categorizeFile(file.Filename)

    // 2. Construir prompt especializado
    prompt := buildAnalysisPrompt(file, patch, category)

    // 3. Enviar para IA
    result, err := a.provider.Analyze(prompt)
    if err != nil {
        return nil, err
    }

    // 4. Parsear resposta JSON
    analysis := parseAIResponse(result)
    analysis.Category = category

    return &analysis, nil
}

// AnalyzeComparison - Analisa todos os arquivos de uma comparação
func (a *GitHubAnalyzer) AnalyzeComparison(comparison *Comparison) (*AIImpactAnalysis, error) {
    infra := []FileAIAnalysis{}
    k8s := []FileAIAnalysis{}
    app := []FileAIAnalysis{}

    highRiskCount := 0
    mediumRiskCount := 0
    lowRiskCount := 0

    // Analisar cada arquivo em paralelo
    results := make(chan *FileAIAnalysis, len(comparison.FilesChanged))

    for _, file := range comparison.FilesChanged {
        go func(f GitHubFile) {
            patch := getFilePatch(comparison, f.Filename)
            analysis, _ := a.AnalyzeFileChange(f, patch)
            results <- analysis
        }(file)
    }

    // Coletar resultados
    for range comparison.FilesChanged {
        analysis := <-results

        // Categorizar
        switch analysis.Category {
        case "infra":
            infra = append(infra, *analysis)
        case "k8s":
            k8s = append(k8s, *analysis)
        default:
            app = append(app, *analysis)
        }

        // Contar riscos
        switch analysis.RiskLevel {
        case "high":
            highRiskCount++
        case "medium":
            mediumRiskCount++
        default:
            lowRiskCount++
        }
    }

    // Determinar risco geral
    overallRisk := "low"
    if highRiskCount > 0 {
        overallRisk = "high"
    } else if mediumRiskCount > 3 {
        overallRisk = "medium"
    }

    // Gerar recomendação geral
    recommendation := generateOverallRecommendation(overallRisk, highRiskCount, mediumRiskCount)

    return &AIImpactAnalysis{
        OverallRisk:           overallRisk,
        OverallRecommendation: recommendation,
        HighRiskCount:         highRiskCount,
        MediumRiskCount:       mediumRiskCount,
        LowRiskCount:          lowRiskCount,
        InfraChanges:          infra,
        K8sChanges:            k8s,
        AppChanges:            app,
    }, nil
}

func categorizeFile(filename string) string {
    if strings.HasSuffix(filename, ".yaml") || strings.HasSuffix(filename, ".yml") {
        return "k8s"
    }
    if strings.Contains(filename, "Dockerfile") {
        return "infra"
    }
    return "app"
}
```

### Prompt Engineering

**Prompt Especializado por Categoria**:

```go
func buildAnalysisPrompt(file GitHubFile, patch string, category string) string {
    basePrompt := fmt.Sprintf(`
Você é um especialista em infraestrutura Kubernetes e DevOps.

Analise o seguinte diff de arquivo do GitHub:

Arquivo: %s
Status: %s
Adições: %d linhas
Remoções: %d linhas
Categoria: %s

Patch (diff):
%s

`, file.Filename, file.Status, file.Additions, file.Deletions, category, patch)

    switch category {
    case "k8s":
        basePrompt += `
Avalie esta mudança em um manifest Kubernetes:

1. **Nível de Risco** (low/medium/high):
   - High: Mudanças em replicas, resources.limits, resources.requests, image tag, nodeSelector, PVC
   - Medium: Mudanças em env vars, ConfigMaps, Secrets, probes, Service ports
   - Low: Mudanças em labels, annotations

2. **Descrição**: Explique o que foi alterado (ex: "Aumentou CPU request de 500m para 1")

3. **Recomendação**: Ação necessária (ex: "Verificar se node pool suporta novo request")

4. **Tipo de Recurso**: Deployment, Service, ConfigMap, Secret, etc.

Retorne JSON:
{
  "risk_level": "low|medium|high",
  "description": "...",
  "recommendation": "...",
  "resource_type": "Deployment|Service|..."
}
`
    case "infra":
        basePrompt += `
Avalie esta mudança em Dockerfile:

1. **Nível de Risco**:
   - High: Mudança de base image, ENTRYPOINT, CMD, USER root
   - Medium: Mudança de ENV vars, ARG, WORKDIR
   - Low: Mudança de LABEL, comentários

2. **Descrição**: O que foi alterado

3. **Recomendação**: Ação necessária (ex: "Testar build local antes de deploy")

Retorne JSON (mesmo formato acima, resource_type = "Dockerfile")
`
    default:
        basePrompt += `
Avalie esta mudança em código da aplicação:

1. **Nível de Risco**:
   - High: Mudanças em lógica crítica de negócio
   - Medium: Mudanças em integrações externas
   - Low: Refactoring, testes, documentação

2. **Descrição**: O que foi alterado

3. **Recomendação**: Testes necessários

Retorne JSON (mesmo formato, resource_type = "Application Code")
`
    }

    return basePrompt
}
```

### Regras de Classificação de Risco

**High Risk** (⚠️ Alto Risco):
- Mudanças em `spec.replicas` (Deployment/StatefulSet)
- Mudanças em `resources.requests` ou `resources.limits` (CPU/Memory)
- Mudanças em `image` tag (pode quebrar compatibilidade)
- Mudanças em `nodeSelector`, `tolerations`, `affinity`
- Remoção de `PersistentVolumeClaim`
- Mudança de base image em Dockerfile
- Mudança de `ENTRYPOINT` ou `CMD`

**Medium Risk** (⚠️ Médio Risco):
- Mudanças em `env` variables
- Mudanças em `ConfigMap` ou `Secret` referenciados
- Mudanças em `livenessProbe` ou `readinessProbe`
- Mudanças em `Service` ports
- Mudanças em `Ingress` rules
- Mudanças em `ENV` vars de Dockerfile

**Low Risk** (✅ Baixo Risco):
- Mudanças em `labels` ou `annotations`
- Mudanças em documentação (`.md`)
- Refactoring de código sem mudança de lógica
- Mudanças em testes
- Mudanças em `LABEL` de Dockerfile

### Frontend: Tab "Análise IA"

**Estrutura**:
```tsx
<TabsContent value="ai-analysis">
  <Card>
    <CardHeader>
      <CardTitle>Análise de Impacto com IA</CardTitle>
    </CardHeader>
    <CardContent>
      {/* Sumário Geral: 3 Cards (Alto/Médio/Baixo Risco) */}
      <div className="grid grid-cols-3 gap-4">
        <Card className="bg-red-50">
          <CardTitle>Alto Risco</CardTitle>
          <div className="text-2xl text-red-600">{aiAnalysis.high_risk_count}</div>
        </Card>
        {/* Medium e Low similar */}
      </div>

      {/* Accordion por Categoria */}
      <Accordion type="multiple">
        <AccordionItem value="infra">
          <AccordionTrigger>
            ⚙️ Mudanças de Infraestrutura ({aiAnalysis.infra_changes.length})
          </AccordionTrigger>
          <AccordionContent>
            {aiAnalysis.infra_changes.map(change => (
              <Card>
                <CardHeader>
                  <CardTitle>{change.file}</CardTitle>
                  <Badge variant={change.risk_level === 'high' ? 'destructive' : 'warning'}>
                    {change.risk_level}
                  </Badge>
                </CardHeader>
                <CardContent>
                  <p>{change.description}</p>
                  <strong>Recomendação:</strong>
                  <p>{change.recommendation}</p>
                </CardContent>
              </Card>
            ))}
          </AccordionContent>
        </AccordionItem>
        {/* K8s e App similar */}
      </Accordion>

      {/* Recomendação Final */}
      <Alert variant={aiAnalysis.overall_risk === 'high' ? 'destructive' : 'default'}>
        <AlertTitle>Recomendação Geral</AlertTitle>
        <AlertDescription>{aiAnalysis.overall_recommendation}</AlertDescription>
      </Alert>
    </CardContent>
  </Card>
</TabsContent>
```

**Color Coding**:
- 🔴 **Alto Risco**: `bg-red-50 border-red-200`, Badge `variant="destructive"`
- 🟡 **Médio Risco**: `bg-yellow-50 border-yellow-200`, Badge `variant="warning"`
- 🟢 **Baixo Risco**: `bg-green-50 border-green-200`, Badge `variant="success"`

---

## 📊 Fluxo de Uso

### 1. Configurar Token (Uma vez)
```
Usuário → Clica "⚙️ Configurar Token"
       → Insere token GitHub pessoal
       → Backend valida token com GitHub API
       → Backend salva token criptografado no SQLite
       → Frontend exibe status (✅ Token Válido, username, rate limit)
```

### 2. Comparar Releases
```
Usuário → Seleciona repositório (ex: vv-retira-geolocalizacao)
       → Seleciona Base Tag (ex: 2.5.5-2) - produção atual
       → Seleciona Head Tag (ex: 2.5.8-1) - candidato
       → Clica "Comparar Releases"
       ↓
Backend → Faz request para GitHub API:
          GET /repos/:owner/:repo/compare/2.5.5-2...2.5.8-1
       → Retorna: commits, files_changed, ahead_by, behind_by
       → Busca release notes de ambas as tags
       ↓
Frontend → Exibe tabs:
           - Commits: Lista de 25 commits entre as tags
           - Arquivos: 48 arquivos alterados
           - Release Notes: Lado a lado
```

### 3. Filtrar Arquivos Relevantes
```
Usuário → Na tab "Arquivos", seleciona filtro ".yaml"
       ↓
Frontend → Filtra 48 arquivos → 12 arquivos YAML
        → Exibe badge "12 de 48 arquivos"
        → Tabela mostra apenas YAMLs Kubernetes
```

### 4. Analisar Impacto com IA
```
Usuário → Clica tab "Análise IA"
       ↓
Frontend → Se análise não existe, dispara request:
           POST /api/v1/github/repos/:owner/:repo/analyze
           Body: { base_tag: "2.5.5-2", head_tag: "2.5.8-1" }
       ↓
Backend → Para cada arquivo YAML/Dockerfile:
          1. Busca patch (diff completo)
          2. Envia para IA (Claude/Ollama)
          3. IA retorna: risk_level, description, recommendation
       → Agrupa resultados por categoria (infra/k8s/app)
       → Calcula risco geral (high se qualquer high-risk file)
       ↓
Frontend → Exibe 3 cards de sumário:
           - Alto Risco: 2 arquivos
           - Médio Risco: 5 arquivos
           - Baixo Risco: 5 arquivos
        → Accordion com detalhes por categoria:
           ⚙️ Infraestrutura (1): Dockerfile mudou base image
           ☸️ Kubernetes (6): deployment.yaml aumentou CPU request
           🚀 Aplicação (5): refactoring de código
        → Alert de recomendação geral:
           "⚠️ ALTO RISCO: Revisar mudanças em resources.requests
            antes de promover para produção."
```

---

## 🧪 Casos de Teste

### Caso 1: Release com Apenas Código
**Input**: `compare/1.0.0...1.1.0` com 30 commits, apenas arquivos `.go` e `.ts`

**Esperado**:
- Tab Commits: 30 commits listados
- Tab Arquivos: 45 arquivos (todos código)
- Filtro ".yaml": 0 arquivos
- Análise IA: Apenas categoria "app", todos "low risk"
- Recomendação geral: "✅ BAIXO RISCO: Apenas mudanças em código da aplicação"

### Caso 2: Release com Mudança em Deployment
**Input**: `compare/2.0.0...2.1.0` com alteração em `deployment.yaml`:
```diff
- replicas: 3
+ replicas: 10
- cpu: 500m
+ cpu: 2
```

**Esperado**:
- Filtro ".yaml": 1 arquivo
- Análise IA:
  - Categoria: k8s
  - Risk Level: high
  - Description: "Aumentou réplicas de 3 para 10 e CPU de 500m para 2"
  - Recommendation: "Verificar se node pool suporta 10 réplicas com 2 CPUs cada (20 CPUs total)"
- Recomendação geral: "⚠️ ALTO RISCO: Mudanças significativas em resources"

### Caso 3: Release com Mudança em Dockerfile
**Input**: `compare/1.5.0...1.6.0` com alteração em `Dockerfile`:
```diff
- FROM node:18-alpine
+ FROM node:20-alpine
```

**Esperado**:
- Filtro "Dockerfile": 1 arquivo
- Análise IA:
  - Categoria: infra
  - Risk Level: high
  - Description: "Mudança de base image de Node 18 para Node 20"
  - Recommendation: "Testar build local e verificar compatibilidade de dependências"
- Recomendação geral: "⚠️ ALTO RISCO: Mudança de base image"

### Caso 4: Token Inválido
**Input**: Token GitHub inválido ou expirado

**Esperado**:
- Modal de Token exibe: "❌ Token Inválido ou Não Configurado"
- Comparação de releases retorna: HTTP 401 Unauthorized
- Frontend exibe toast: "Erro: Token GitHub inválido. Configure nas Settings."

---

## 📈 Métricas de Sucesso

**Quantitativas**:
- ✅ Redução de 70% no tempo de análise de releases (de 30min manual para 10min com IA)
- ✅ 100% dos arquivos YAML/Dockerfile classificados automaticamente
- ✅ 0 tokens expostos em logs ou responses HTTP

**Qualitativas**:
- ✅ Analistas identificam mudanças críticas mais rapidamente
- ✅ Recomendações da IA são úteis e acionáveis
- ✅ UX intuitiva (não requer treinamento)

---

## ⚠️ Riscos e Mitigações

### Risco 1: GitHub API Rate Limit
**Problema**: 5000 req/h com token, 60 req/h sem token.

**Mitigação**:
- ✅ Obrigar uso de token individual (não permitir anônimo)
- ✅ Cache de 5 minutos para comparisons (React Query)
- ✅ Exibir rate limit restante no modal de token
- ✅ Mensagem amigável ao exceder limite

### Risco 2: Análise IA Lenta
**Problema**: Analisar 50 arquivos pode levar 2-5 minutos.

**Mitigação**:
- ✅ Análise em background com loading spinner
- ✅ Progress bar mostrando "Analisando arquivo 12 de 50"
- ✅ Cache de análises anteriores (SQLite)
- ✅ Possibilidade de cancelar análise

### Risco 3: IA Classificar Incorretamente
**Problema**: IA pode classificar "high risk" algo que é "low risk" e vice-versa.

**Mitigação**:
- ✅ Regras explícitas no prompt (não apenas confiar na IA)
- ✅ Validação pós-IA: conferir se mudanças em `replicas`/`resources` são high
- ✅ Feedback do usuário: botão "👍 Concordo / 👎 Discordo" (futuro)
- ✅ Treinamento do modelo com casos reais (futuro)

### Risco 4: Token Vazado
**Problema**: Desenvolvedor pode expor token no código ou logs.

**Mitigação**:
- ✅ Criptografia AES-256 no storage
- ✅ NUNCA retornar token em response HTTP (apenas status)
- ✅ NUNCA logar token (apenas 4 primeiros caracteres: `ghp_xxxx...`)
- ✅ Permissões 600 no arquivo `.secret`

---

## 🚀 Roadmap de Implementação

### Fase 1: MVP (Estimativa: 8-12 horas)
- ✅ Backend básico (repos, releases, compare)
- ✅ Frontend com SplitView
- ✅ Tabs: Commits, Arquivos, Release Notes
- ⬜ Sistema de token individual
- ⬜ Documentação básica

### Fase 2: Filtros e IA (Estimativa: 8-12 horas)
- ⬜ Filtro de extensões
- ⬜ Integração com AI Diagnostics
- ⬜ Tab "Análise IA"
- ⬜ Regras de classificação de risco
- ⬜ Testes com casos reais

### Fase 3: Polish (Estimativa: 4-6 horas)
- ⬜ Loading states
- ⬜ Error handling gracioso
- ⬜ Cache de análises (SQLite)
- ⬜ Testes E2E
- ⬜ Documentação completa

**Total Estimado**: 20-30 horas de desenvolvimento

---

## 🔗 Referências Técnicas

- **GitHub API - Compare Commits**: https://docs.github.com/en/rest/commits/commits#compare-two-commits
- **go-github Library**: https://github.com/google/go-github
- **React Query**: https://tanstack.com/query/latest
- **shadcn/ui**: https://ui.shadcn.com/
- **AES-GCM Encryption**: https://pkg.go.dev/crypto/cipher#NewGCM

---

**Documento elaborado em**: 2026-01-11
**Versão do plano**: 1.0
**Próximos passos**: Aprovação do usuário → Implementação Fase 1
