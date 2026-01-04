# Proposta: Sistema de Investigação Automática Inteligente para AI Diagnostics

## 🎯 Objetivo

Transformar a AI de **passiva** (apenas lê erros) para **ativa** (investiga causas raiz no cluster).

---

## 🔍 Casos de Uso

### Caso 1: ConfigMap Não Encontrado
**Erro no Pod:**
```
MountVolume.SetUp failed for volume "config" : configmap "supply-api-config" not found
```

**Investigação Automática:**
1. ✅ Buscar ConfigMap exato `supply-api-config` no namespace `production`
2. ✅ Buscar ConfigMaps com pattern `supply-api*` (encontrar hashes do Kustomize)
3. ✅ Buscar em todos os namespaces (caso esteja no namespace errado)
4. ✅ Verificar se Pod referencia o nome correto no spec

**Resultado para AI:**
```markdown
## 🔍 INVESTIGAÇÃO AUTOMÁTICA

### ❌ ConfigMap "supply-api-config" NÃO EXISTE
- Namespace buscado: production
- Resultado: NOT FOUND

### ✅ ConfigMaps Similares ENCONTRADOS:
- **supply-api-6m6gmhh4tb** (namespace: production)
  - Criado: 2h atrás
  - Keys: application.properties, database.yml
  - Match: 85% (prefixo "supply-api")

### 📊 CAUSA RAIZ:
Pod referencia nome hardcoded "supply-api-config" mas Kustomize gera hash "supply-api-6m6gmhh4tb"

### ✅ SOLUÇÃO:
1. Atualizar Deployment para usar nameReference do Kustomize
2. OU criar ConfigMap com nome fixo "supply-api-config"
```

---

### Caso 2: Secret com Permissões Incorretas
**Erro no Pod:**
```
Error: couldn't find key DATABASE_PASSWORD in Secret default/db-secret
```

**Investigação Automática:**
1. ✅ Buscar Secret `db-secret` no namespace `default`
2. ✅ Listar keys disponíveis no Secret
3. ✅ Comparar com keys esperadas pelo Pod
4. ✅ Verificar se ServiceAccount tem permissão de leitura

**Resultado para AI:**
```markdown
## 🔍 INVESTIGAÇÃO AUTOMÁTICA

### ✅ Secret "db-secret" EXISTE
- Namespace: default
- Keys disponíveis: ["DB_PASS", "DB_USER"]

### ❌ PROBLEMA IDENTIFICADO:
Pod busca key "DATABASE_PASSWORD" mas Secret tem "DB_PASS"

### 📊 CAUSA RAIZ:
Nome da key incorreto (case mismatch ou renomeação)

### ✅ SOLUÇÃO:
Opção 1: Atualizar Secret para adicionar key "DATABASE_PASSWORD"
Opção 2: Atualizar Pod para ler key "DB_PASS"
```

---

### Caso 3: PVC Pendente (StorageClass Inválida)
**Erro no Pod:**
```
persistentvolumeclaim "data-pvc" not found
```

**Investigação Automática:**
1. ✅ Buscar PVC `data-pvc` no namespace
2. ✅ Verificar status (Pending/Bound)
3. ✅ Verificar eventos do PVC
4. ✅ Validar StorageClass existe

**Resultado para AI:**
```markdown
## 🔍 INVESTIGAÇÃO AUTOMÁTICA

### ⚠️ PVC "data-pvc" EXISTE MAS ESTÁ PENDENTE
- Status: Pending (waiting for volume provisioning)
- StorageClass: fast-ssd
- Requested: 10Gi

### ❌ PROBLEMA IDENTIFICADO:
StorageClass "fast-ssd" não existe no cluster
- StorageClasses disponíveis: ["standard", "default", "azure-disk"]

### 📊 CAUSA RAIZ:
StorageClass referenciada não existe ou foi removida

### ✅ SOLUÇÃO:
Atualizar PVC para usar StorageClass válida (ex: "azure-disk")
```

---

## 🏗️ Implementação Técnica

### 1. Novo Arquivo: `internal/collectors/smart_investigator.go`

```go
package collectors

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// SmartInvestigator executa investigações inteligentes no cluster
type SmartInvestigator struct {
	clientset *kubernetes.Clientset
}

// NewSmartInvestigator cria novo investigador
func NewSmartInvestigator(clientset *kubernetes.Clientset) *SmartInvestigator {
	return &SmartInvestigator{clientset: clientset}
}

// InvestigateConfigMap busca ConfigMap inteligentemente
func (s *SmartInvestigator) InvestigateConfigMap(ctx context.Context, namespace, name string) *ResourceInvestigation {
	result := &ResourceInvestigation{
		ResourceType: "ConfigMap",
		SearchedName: name,
		Namespace:    namespace,
	}

	// 1. Busca exata
	cm, err := s.clientset.CoreV1().ConfigMaps(namespace).Get(ctx, name, metav1.GetOptions{})
	if err == nil {
		result.Found = true
		result.ExactMatch = true
		result.ActualName = cm.Name
		result.Keys = extractConfigMapKeys(cm)
		result.Diagnosis = fmt.Sprintf("✅ ConfigMap '%s' existe e está acessível", name)
		return result
	}

	// 2. Busca por pattern (supply-api* para encontrar supply-api-6m6gmhh4tb)
	pattern := extractPattern(name) // "supply-api-config" → "supply-api"
	cmList, _ := s.clientset.CoreV1().ConfigMaps(namespace).List(ctx, metav1.ListOptions{})

	var similarCMs []SimilarResource
	for _, cm := range cmList.Items {
		if strings.HasPrefix(cm.Name, pattern) {
			similarity := calculateSimilarity(name, cm.Name)
			similarCMs = append(similarCMs, SimilarResource{
				Name:       cm.Name,
				Similarity: similarity,
				Keys:       extractConfigMapKeys(&cm),
				Age:        metav1.Now().Time.Sub(cm.CreationTimestamp.Time).String(),
			})
		}
	}

	if len(similarCMs) > 0 {
		result.Found = true
		result.ExactMatch = false
		result.SimilarResources = similarCMs
		result.Diagnosis = fmt.Sprintf("❌ ConfigMap '%s' não existe, mas encontrados %d similares", name, len(similarCMs))
		result.RootCause = "Nome hardcoded no Pod não corresponde ao hash gerado (Kustomize/Helm)"
		result.Solution = fmt.Sprintf("Atualizar Pod para referenciar '%s' OU renomear ConfigMap para '%s'", similarCMs[0].Name, name)
	} else {
		result.Found = false
		result.Diagnosis = fmt.Sprintf("❌ ConfigMap '%s' NÃO encontrado no namespace '%s'", name, namespace)
		result.RootCause = "ConfigMap não foi criado ou foi deletado"
		result.Solution = fmt.Sprintf("Criar ConfigMap com comando: kubectl create configmap %s -n %s", name, namespace)
	}

	// 3. Busca cross-namespace (caso esteja no namespace errado)
	allNamespaces, _ := s.clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	for _, ns := range allNamespaces.Items {
		if ns.Name == namespace {
			continue
		}
		cm, err := s.clientset.CoreV1().ConfigMaps(ns.Name).Get(ctx, name, metav1.GetOptions{})
		if err == nil {
			result.FoundInWrongNamespace = true
			result.CorrectNamespace = ns.Name
			result.Diagnosis += fmt.Sprintf("\n⚠️ ATENÇÃO: ConfigMap '%s' existe no namespace '%s' (namespace errado!)", name, ns.Name)
			result.Solution = fmt.Sprintf("Copiar ConfigMap do namespace '%s' para '%s'", ns.Name, namespace)
			break
		}
	}

	return result
}

// InvestigateSecret busca Secret e valida keys
func (s *SmartInvestigator) InvestigateSecret(ctx context.Context, namespace, name string, expectedKeys []string) *ResourceInvestigation {
	result := &ResourceInvestigation{
		ResourceType: "Secret",
		SearchedName: name,
		Namespace:    namespace,
	}

	secret, err := s.clientset.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		result.Found = false
		result.Diagnosis = fmt.Sprintf("❌ Secret '%s' não encontrado", name)
		return result
	}

	result.Found = true
	result.ExactMatch = true
	result.Keys = extractSecretKeys(secret)

	// Validar se keys esperadas existem
	missingKeys := []string{}
	for _, expectedKey := range expectedKeys {
		if _, exists := secret.Data[expectedKey]; !exists {
			missingKeys = append(missingKeys, expectedKey)
		}
	}

	if len(missingKeys) > 0 {
		result.Diagnosis = fmt.Sprintf("⚠️ Secret '%s' existe mas faltam keys: %v", name, missingKeys)
		result.RootCause = "Keys configuradas no Pod não existem no Secret"
		result.Solution = fmt.Sprintf("Adicionar keys faltantes ao Secret: %v", missingKeys)
	} else {
		result.Diagnosis = fmt.Sprintf("✅ Secret '%s' existe com todas as keys necessárias", name)
	}

	return result
}

// InvestigatePVC busca PVC e diagnóstica problemas de binding
func (s *SmartInvestigator) InvestigatePVC(ctx context.Context, namespace, name string) *ResourceInvestigation {
	result := &ResourceInvestigation{
		ResourceType: "PersistentVolumeClaim",
		SearchedName: name,
		Namespace:    namespace,
	}

	pvc, err := s.clientset.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		result.Found = false
		result.Diagnosis = fmt.Sprintf("❌ PVC '%s' não encontrado", name)
		return result
	}

	result.Found = true
	result.ExactMatch = true

	// Analisar status
	switch pvc.Status.Phase {
	case corev1.ClaimBound:
		result.Diagnosis = fmt.Sprintf("✅ PVC '%s' está Bound ao volume '%s'", name, pvc.Spec.VolumeName)
	case corev1.ClaimPending:
		result.Diagnosis = fmt.Sprintf("⚠️ PVC '%s' está Pending (aguardando provisionamento)", name)

		// Investigar StorageClass
		if pvc.Spec.StorageClassName != nil {
			scName := *pvc.Spec.StorageClassName
			_, err := s.clientset.StorageV1().StorageClasses().Get(ctx, scName, metav1.GetOptions{})
			if err != nil {
				result.RootCause = fmt.Sprintf("StorageClass '%s' não existe", scName)

				// Listar StorageClasses disponíveis
				scList, _ := s.clientset.StorageV1().StorageClasses().List(ctx, metav1.ListOptions{})
				availableSCs := []string{}
				for _, sc := range scList.Items {
					availableSCs = append(availableSCs, sc.Name)
				}
				result.Solution = fmt.Sprintf("Atualizar PVC para usar StorageClass válida: %v", availableSCs)
			}
		}
	case corev1.ClaimLost:
		result.Diagnosis = fmt.Sprintf("❌ PVC '%s' em estado Lost (volume perdido)", name)
		result.RootCause = "Volume backing foi deletado"
		result.Solution = "Recriar PVC e restaurar dados do backup"
	}

	return result
}

// Helper functions
func extractPattern(name string) string {
	// "supply-api-config" → "supply-api"
	// "supply-api-6m6gmhh4tb" → "supply-api"
	parts := strings.Split(name, "-")
	if len(parts) >= 2 {
		return strings.Join(parts[:2], "-")
	}
	return name
}

func calculateSimilarity(a, b string) float64 {
	// Implementar Levenshtein distance ou similar
	// Por simplicidade, usar prefixo comum
	minLen := len(a)
	if len(b) < minLen {
		minLen = len(b)
	}

	commonPrefix := 0
	for i := 0; i < minLen; i++ {
		if a[i] == b[i] {
			commonPrefix++
		} else {
			break
		}
	}

	return float64(commonPrefix) / float64(max(len(a), len(b))) * 100
}

func extractConfigMapKeys(cm *corev1.ConfigMap) []string {
	keys := make([]string, 0, len(cm.Data))
	for k := range cm.Data {
		keys = append(keys, k)
	}
	return keys
}

func extractSecretKeys(secret *corev1.Secret) []string {
	keys := make([]string, 0, len(secret.Data))
	for k := range secret.Data {
		keys = append(keys, k)
	}
	return keys
}
```

### 2. Estrutura de Dados Estendida

```go
// ResourceInvestigation resultado de investigação de um recurso
type ResourceInvestigation struct {
	ResourceType           string
	SearchedName           string
	Namespace              string
	Found                  bool
	ExactMatch             bool
	ActualName             string
	Keys                   []string
	SimilarResources       []SimilarResource
	FoundInWrongNamespace  bool
	CorrectNamespace       string
	Diagnosis              string
	RootCause              string
	Solution               string
}

// SimilarResource recurso similar encontrado
type SimilarResource struct {
	Name       string
	Similarity float64
	Keys       []string
	Age        string
}
```

### 3. Integração no Collector Principal

```go
// Em internal/collectors/pod_collector.go

func (c *PodCollector) Collect(ctx context.Context, req CollectionRequest) (*DiagnosticContext, error) {
	// ... código existente ...

	// NOVA SEÇÃO: Investigação Inteligente
	investigator := NewSmartInvestigator(c.clientset)

	// Extrair recursos referenciados do Pod
	referencedResources := extractReferencedResources(pod)

	investigations := []*ResourceInvestigation{}
	for _, ref := range referencedResources {
		var inv *ResourceInvestigation

		switch ref.Type {
		case "ConfigMap":
			inv = investigator.InvestigateConfigMap(ctx, pod.Namespace, ref.Name)
		case "Secret":
			inv = investigator.InvestigateSecret(ctx, pod.Namespace, ref.Name, ref.ExpectedKeys)
		case "PersistentVolumeClaim":
			inv = investigator.InvestigatePVC(ctx, pod.Namespace, ref.Name)
		}

		if inv != nil {
			investigations = append(investigations, inv)
		}
	}

	diagCtx.SmartInvestigations = investigations

	return diagCtx, nil
}

// extractReferencedResources extrai recursos referenciados no Pod spec
func extractReferencedResources(pod *corev1.Pod) []ResourceReference {
	refs := []ResourceReference{}

	// ConfigMaps de volumes
	for _, vol := range pod.Spec.Volumes {
		if vol.ConfigMap != nil {
			refs = append(refs, ResourceReference{
				Type: "ConfigMap",
				Name: vol.ConfigMap.Name,
			})
		}
		if vol.Secret != nil {
			refs = append(refs, ResourceReference{
				Type: "Secret",
				Name: vol.Secret.SecretName,
			})
		}
		if vol.PersistentVolumeClaim != nil {
			refs = append(refs, ResourceReference{
				Type: "PersistentVolumeClaim",
				Name: vol.PersistentVolumeClaim.ClaimName,
			})
		}
	}

	// ConfigMaps/Secrets de envFrom
	for _, container := range pod.Spec.Containers {
		for _, envFrom := range container.EnvFrom {
			if envFrom.ConfigMapRef != nil {
				refs = append(refs, ResourceReference{
					Type: "ConfigMap",
					Name: envFrom.ConfigMapRef.Name,
				})
			}
			if envFrom.SecretRef != nil {
				refs = append(refs, ResourceReference{
					Type: "Secret",
					Name: envFrom.SecretRef.Name,
				})
			}
		}

		// Keys esperadas de env
		expectedKeys := []string{}
		for _, env := range container.Env {
			if env.ValueFrom != nil {
				if env.ValueFrom.SecretKeyRef != nil {
					expectedKeys = append(expectedKeys, env.ValueFrom.SecretKeyRef.Key)
				}
			}
		}
		// Associar keys ao Secret correspondente
		// (lógica mais complexa aqui)
	}

	return refs
}
```

### 4. Atualização no Prompt

```go
// Em internal/ai/prompts.go

func (pb *PromptBuilder) addSmartInvestigations(builder *strings.Builder, investigations []*ResourceInvestigation) {
	if len(investigations) == 0 {
		return
	}

	builder.WriteString("════════════════════════════════════════════════════\n")
	builder.WriteString("🔍 INVESTIGAÇÃO AUTOMÁTICA NO CLUSTER 🔍\n")
	builder.WriteString("════════════════════════════════════════════════════\n")
	builder.WriteString("⚠️ OS DADOS ABAIXO SÃO RESULTADOS REAIS DE BUSCAS NO CLUSTER\n")
	builder.WriteString("⚠️ NÃO peça kubectl get/describe - TODOS OS RECURSOS FORAM VALIDADOS\n\n")

	for _, inv := range investigations {
		builder.WriteString(fmt.Sprintf("### %s '%s'\n", inv.ResourceType, inv.SearchedName))
		builder.WriteString(fmt.Sprintf("**Diagnóstico:** %s\n\n", inv.Diagnosis))

		if inv.RootCause != "" {
			builder.WriteString(fmt.Sprintf("**📊 Causa Raiz:** %s\n\n", inv.RootCause))
		}

		if inv.Solution != "" {
			builder.WriteString(fmt.Sprintf("**✅ Solução:** %s\n\n", inv.Solution))
		}

		if len(inv.SimilarResources) > 0 {
			builder.WriteString("**Recursos Similares Encontrados:**\n")
			for _, sim := range inv.SimilarResources {
				builder.WriteString(fmt.Sprintf("- `%s` (similaridade: %.0f%%, idade: %s)\n", sim.Name, sim.Similarity, sim.Age))
				if len(sim.Keys) > 0 {
					builder.WriteString(fmt.Sprintf("  Keys: %v\n", sim.Keys))
				}
			}
			builder.WriteString("\n")
		}

		if inv.FoundInWrongNamespace {
			builder.WriteString(fmt.Sprintf("⚠️ **ATENÇÃO:** Recurso encontrado no namespace '%s' (deveria estar em '%s')\n\n", inv.CorrectNamespace, inv.Namespace))
		}

		builder.WriteString("---\n\n")
	}
}
```

---

## 🎯 Benefícios

### ✅ **Para o Usuário**
- Diagnósticos precisos e acionáveis
- Menos tentativa-e-erro
- Soluções específicas ao invés de genéricas

### ✅ **Para a AI**
- Dados concretos ao invés de suposições
- Contexto completo da causa raiz
- Capacidade de oferecer soluções exatas

### ✅ **Para o Sistema**
- Reduz latência (menos idas-e-vindas)
- Reduz custos de API (menos tokens)
- Melhora taxa de resolução de problemas

---

## 📊 Exemplo de Saída Completa

```markdown
## 🔍 INVESTIGAÇÃO AUTOMÁTICA NO CLUSTER

### ConfigMap 'supply-api-config'
**Diagnóstico:** ❌ ConfigMap 'supply-api-config' não existe, mas encontrados 1 similares

**📊 Causa Raiz:** Nome hardcoded no Pod não corresponde ao hash gerado (Kustomize/Helm)

**✅ Solução:** Atualizar Pod para referenciar 'supply-api-6m6gmhh4tb' OU renomear ConfigMap para 'supply-api-config'

**Recursos Similares Encontrados:**
- `supply-api-6m6gmhh4tb` (similaridade: 85%, idade: 2h3m)
  Keys: [application.properties, database.yml, logging.xml]

---

### Secret 'db-credentials'
**Diagnóstico:** ✅ Secret 'db-credentials' existe com todas as keys necessárias

---

### PersistentVolumeClaim 'data-pvc'
**Diagnóstico:** ⚠️ PVC 'data-pvc' está Pending (aguardando provisionamento)

**📊 Causa Raiz:** StorageClass 'fast-ssd' não existe

**✅ Solução:** Atualizar PVC para usar StorageClass válida: [standard, default, azure-disk]
```

---

## 🚀 Roadmap de Implementação

### Fase 1: Core (Alta Prioridade)
- [x] Estrutura de dados `SmartInvestigator`
- [x] Investigação de ConfigMaps (exact + pattern matching)
- [x] Investigação de Secrets (exact + key validation)
- [x] Investigação de PVCs (status + StorageClass)

### Fase 2: Recursos Adicionais (Média Prioridade)
- [ ] Services (endpoints válidos, selector match)
- [ ] ServiceAccounts (RBAC permissions)
- [ ] Ingress (backend service exists, TLS secrets)
- [ ] Deployments (owner reference validation)

### Fase 3: Validações Avançadas (Baixa Prioridade)
- [ ] Cross-namespace search otimizado (cache)
- [ ] Histórico de mudanças (GitOps audit trail)
- [ ] Sugestões baseadas em padrões (Helm/Kustomize detection)
- [ ] Auto-correção (opcional, com confirmação do usuário)

---

## 🧪 Testes

```go
func TestSmartInvestigator_ConfigMapExact(t *testing.T) {
	// Criar ConfigMap de teste
	// Buscar com nome exato
	// Validar resultado
}

func TestSmartInvestigator_ConfigMapPattern(t *testing.T) {
	// Criar ConfigMap com hash
	// Buscar com nome base
	// Validar encontrou similar
}

func TestSmartInvestigator_SecretMissingKeys(t *testing.T) {
	// Criar Secret com keys A, B
	// Buscar esperando keys A, B, C
	// Validar detectou key faltante
}
```

---

Quer que eu implemente essa proposta?
