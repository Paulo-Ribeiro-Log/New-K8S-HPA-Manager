# 🔍 AI Investigativa - Sistema de Investigação Automática de Recursos

## Visão Geral

Transformamos a AI de um **"relator de erros"** em um **"investigador ativo"** que BUSCA informações no Kubernetes antes de fazer o diagnóstico.

## Problema Anterior

**Antes:**
```
Evento: "configmap 'supply-api-6m6gmhh4tb' not found"
AI: "ConfigMap não encontrado" ❌ (não sabia se era verdade)
```

**Agora:**
```
Evento: "configmap 'supply-api-6m6gmhh4tb' not found"
Sistema busca no K8s: kubectl get configmaps -n namespace
Encontra: supply-api-abc123 (similar)
AI: "ConfigMap 'supply-api-6m6gmhh4tb' NÃO EXISTE no cluster, mas encontrado 'supply-api-abc123' com mesmo prefixo. Provável erro de referência no manifesto." ✅
```

## Como Funciona

### 1. Detecção de Recursos Problemáticos

O sistema analisa:
- **Eventos do Kubernetes**: Parse de mensagens de erro
- **Status de Containers**: Waiting/Terminated states
- **Logs**: Erros de recursos faltantes

**Detecta:**
- ❌ ConfigMaps não encontrados
- ❌ Secrets não encontrados
- ❌ PVCs não encontrados
- ❌ Images que falharam pull
- ❌ Volumes não montados

### 2. Busca Ativa no Cluster

Para cada recurso problemático:

```go
// Exemplo: ConfigMap 'supply-api-6m6gmhh4tb' não encontrado
baseName := "supply-api" // Extrai prefixo

// Busca TODOS configmaps do namespace
kubectl get configmaps -n abastecimento-hlg

// Compara com padrão:
- supply-api-6m6gmhh4tb    ❌ Não existe
- supply-api-abc123         ✅ Encontrado! (exact_prefix match)
- api-config               ❌ Diferente
```

### 3. Análise de Similaridade

O sistema calcula similaridade entre nomes:

| Tipo | Exemplo | Descrição |
|------|---------|-----------|
| **exact_prefix** | `supply-api-6m6gmhh4tb` vs `supply-api-abc123` | Mesmo prefixo base, hash diferente |
| **prefix_match** | `app-config` vs `app-config-prod` | Um é prefixo do outro |
| **contains** | `myapp` contém em `prod-myapp-svc` | Nome contido no outro |
| **similar** | `supply-api` e `supply-backend` | Palavras em comum |

### 4. Recomendações Inteligentes

Baseado nos achados:

**Caso 1: Recurso NÃO existe**
```
❌ ConfigMap 'supply-api-6m6gmhh4tb' NÃO EXISTE no namespace 'abastecimento-hlg'

Recomendação:
$ kubectl create configmap supply-api-6m6gmhh4tb --from-literal=key=value -n abastecimento-hlg
```

**Caso 2: Recurso similar encontrado**
```
⚠️ ConfigMap 'supply-api-6m6gmhh4tb' não existe, mas encontrados:
   • supply-api-abc123 (exact_prefix)
   • supply-api-old (exact_prefix)

Recomendação:
💡 Possível erro de referência. Atualizar manifesto para usar 'supply-api-abc123'
```

## Arquitetura

### Novos Componentes

#### 1. `investigator.go` - Investigador Principal
```go
type Investigator struct {
    clientset *kubernetes.Clientset
}

// Método principal
func (i *Investigator) InvestigateMissingResources(
    ctx context.Context, 
    diagCtx *DiagnosticContext
) (*InvestigationResult, error)
```

**Funções:**
- `detectMissingResourcesFromEvents()` - Parse de eventos
- `detectMissingResourcesFromPodStatus()` - Parse de container status
- `searchAlternatives()` - Busca recursos similares
- `calculateSimilarity()` - Compara nomes
- `generateRecommendations()` - Gera sugestões

#### 2. `models.go` - Novos Tipos

```go
type InvestigationResult struct {
    MissingResources  []MissingResource
    FoundAlternatives []AlternativeResource
    Recommendations   []string
}

type MissingResource struct {
    Type      string // ConfigMap, Secret, PVC, Image
    Name      string // Nome exato buscado
    Namespace string
    Reason    string // Evento/log que mencionou
}

type AlternativeResource struct {
    Type       string
    SearchName string // Nome buscado (com wildcard)
    FoundName  string // Nome real encontrado
    Namespace  string
    Similarity string // exact_prefix, prefix_match, contains, similar
}
```

#### 3. `context_builder.go` - Integração

```go
// Após coletar eventos e status
investigator := NewInvestigator(clientset)
investigation, err := investigator.InvestigateMissingResources(ctx, diagCtx)
if err == nil && len(investigation.MissingResources) > 0 {
    diagCtx.Investigation = investigation // ✅ Adiciona ao contexto
}
```

#### 4. `prompts.go` - Formato para AI

```go
func (pb *PromptBuilder) addInvestigationResults(
    builder *strings.Builder, 
    investigation *InvestigationResult
)
```

**Saída formatada:**
```markdown
## 🔍 INVESTIGAÇÃO AUTOMÁTICA DE RECURSOS

### Recursos Faltantes (confirmado via busca no cluster):
- **ConfigMap 'supply-api-6m6gmhh4tb'** (namespace: abastecimento-hlg)
  Motivo: Event: Failed - Error: configmap 'supply-api-6m6gmhh4tb' not found

### Recursos Similares Encontrados no Cluster:
- Buscado: **ConfigMap 'supply-api-6m6gmhh4tb'**
  Encontrado: **'supply-api-abc123'** (similaridade: exact_prefix)
  ⚠️ Mesmo prefixo base - provavelmente versão/hash diferente

### Recomendações Baseadas na Investigação:
⚠️ ConfigMap 'supply-api-6m6gmhh4tb' não existe, mas encontrados 1 similares:
   • supply-api-abc123 (exact_prefix)
💡 Possível erro de referência. Atualizar manifesto para usar 'supply-api-abc123'

**USE ESTAS INFORMAÇÕES PARA DAR UM DIAGNÓSTICO PRECISO.**
```

## Fluxo Completo

```mermaid
graph TD
    A[Usuário clica "Analisar com AI"] --> B[ContextBuilder coleta dados]
    B --> C[Coletar Eventos]
    B --> D[Coletar Pod Status]
    B --> E[Coletar Logs]
    
    C --> F[Investigator detecta recursos faltantes]
    D --> F
    E --> F
    
    F --> G{Recurso faltante encontrado?}
    G -->|Sim| H[Buscar no K8s por wildcards]
    G -->|Não| I[Prosseguir sem investigação]
    
    H --> J[Comparar similaridade]
    J --> K[Gerar recomendações]
    K --> L[Adicionar ao contexto]
    
    L --> M[PromptBuilder formata contexto]
    I --> M
    
    M --> N[AI analisa com informações precisas]
    N --> O[Resposta específica e acionável]
```

## Exemplos de Detecção

### ConfigMap Faltante

**Evento:**
```
Type: Warning
Reason: Failed
Message: Error: configmap "supply-api-6m6gmhh4tb" not found
```

**Regex Match:**
```go
regexp.MustCompile(`configmap\s+"([^"]+)"\s+not found`)
// Captura: supply-api-6m6gmhh4tb
```

**Busca:**
```go
configMaps, _ := clientset.CoreV1().ConfigMaps(namespace).List()
// Encontra: supply-api-abc123
```

**Resultado:**
- Recurso não existe: ✅ Confirmado
- Alternativa encontrada: ✅ supply-api-abc123
- Similaridade: exact_prefix (mesmo prefixo base)

### Secret com Nome Diferente

**Container Status:**
```
State: Waiting
Reason: CreateContainerConfigError
Message: secret "db-password-old" not found
```

**Busca:**
```
secrets:
  - db-password      ✅
  - db-credentials   ✅
  - app-secrets      ❌
```

**Resultado:**
- Recurso não existe: ✅ Confirmado
- Alternativas: db-password, db-credentials
- Similaridade: contains ("db" em comum)

### Image Pull Error

**Container Status:**
```
State: Waiting
Reason: ErrImagePull
Message: Failed to pull image "myregistry.io/app:v1.2.3"
```

**Investigação:**
- Tipo: Image
- Não busca alternativas (problema de registry/tag)
- Recomendação: Verificar registry, credenciais, tag

## Configuração

### Habilitado por Padrão

A investigação automática é executada automaticamente durante a coleta de contexto.

### Desabilitar (se necessário)

Para debugging ou desenvolvimento:

```go
// Em context_builder.go, comentar:
// investigator := NewInvestigator(clientset)
// investigation, _ := investigator.InvestigateMissingResources(ctx, diagCtx)
// diagCtx.Investigation = investigation
```

### Limites e Performance

- **Eventos analisados**: Até 50 mais recentes
- **Recursos buscados**: Todos do namespace (via List)
- **Similaridade**: Até 10 alternativas por recurso
- **Timeout**: Usa timeout do contexto pai (120s padrão)

**Performance:**
- ConfigMaps: ~10-50ms (depende de quantidade no namespace)
- Secrets: ~10-50ms
- PVCs: ~10-30ms
- **Total**: +50-200ms no tempo de análise

## Benefícios

### Antes (Análise Passiva)
```
❌ "ConfigMap não encontrado"
❌ "Pode haver problema com Secret"
❌ "Verifique se recursos existem"
```
**Resultado:** Usuário precisa investigar manualmente

### Agora (Análise Ativa)
```
✅ "ConfigMap 'xyz' NÃO EXISTE (confirmado)"
✅ "Secret 'abc' existe mas referenciado 'def'"
✅ "kubectl create configmap xyz -n namespace"
```
**Resultado:** Diagnóstico preciso e acionável

## Casos de Uso

### 1. Pod com ConfigMap Inexistente
- Detecta ConfigMap faltante
- Confirma que NÃO existe
- Sugere comando para criar
- AI: "Criar ConfigMap com: kubectl create..."

### 2. Pod Referenciando Nome Antigo
- Detecta ConfigMap faltante
- Encontra ConfigMap com prefixo similar
- Sugere atualizar referência no manifesto
- AI: "ConfigMap 'old-name' não existe. Use 'new-name'."

### 3. Secret com Typo
- Detecta Secret faltante
- Encontra Secret com nome similar (1 letra diferente)
- Sugere corrigir typo
- AI: "Secret 'db-pasword' não existe. Typo? Existe 'db-password'."

### 4. Image com Tag Errada
- Detecta image pull error
- Não busca alternativas (problema de registry)
- Sugere verificar tag e registry
- AI: "Image v1.2.3 não encontrada. Verificar tag no registry."

## Próximos Passos

### Melhorias Futuras

1. **Cache de Recursos**
   - Cachear lista de ConfigMaps/Secrets por namespace
   - Reduzir chamadas à API do K8s
   - Invalidar cache após 5 minutos

2. **Levenshtein Distance**
   - Algoritmo de distância de edição
   - Detectar typos com precisão
   - "db-pasword" vs "db-password" = 1 caractere diferente

3. **Investigação de Nodes**
   - Verificar se node tem recursos suficientes
   - Comparar resource requests com capacity
   - Detectar node pressure (disk, memory, CPU)

4. **Investigação de Images**
   - Tentar listar tags disponíveis no registry
   - Sugerir tag mais recente
   - Verificar se registry está acessível

5. **Investigação de Network Policies**
   - Verificar se políticas bloqueiam comunicação
   - Sugerir correções em NetworkPolicies
   - Detectar conflitos de egress/ingress

## Troubleshooting

### Investigação não executada

**Verificar:**
```bash
# Logs do backend
./build/new-k8s-hpa web --debug

# Procurar por:
# "🔍 INVESTIGAÇÃO AUTOMÁTICA" no contexto da análise
```

**Possíveis causas:**
- Nenhum recurso faltante detectado (normal)
- Erro ao fazer type assertion do clientset
- Timeout do contexto excedido

### Alternativas não encontradas

**Normal se:**
- Recurso realmente não existe no namespace
- Nome é muito diferente (sem similaridade)
- Namespace vazio

**Verificar manualmente:**
```bash
kubectl get configmaps -n <namespace>
kubectl get secrets -n <namespace>
kubectl get pvc -n <namespace>
```

### Performance lenta

**Otimizar:**
1. Reduzir número de recursos no namespace
2. Usar labels para filtrar buscas
3. Implementar cache (próxima versão)

## Conclusão

O sistema de **Investigação Automática** transforma a AI de um observador passivo em um investigador ativo, fornecendo diagnósticos precisos baseados em **fatos verificados** diretamente no cluster Kubernetes.

**Antes:** "Pode haver um problema com ConfigMap"
**Agora:** "ConfigMap 'xyz' NÃO EXISTE (verificado via kubectl get)"

Isso resulta em análises muito mais úteis e acionáveis para o usuário! 🎯
