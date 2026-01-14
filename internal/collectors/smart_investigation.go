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
	clientset kubernetes.Interface
	namespace string
}

// NewSmartInvestigator cria novo investigador
func NewSmartInvestigator(clientset kubernetes.Interface, namespace string) *SmartInvestigator {
	return &SmartInvestigator{
		clientset: clientset,
		namespace: namespace,
	}
}

// ResourceInvestigation resultado de investigação de um recurso
type ResourceInvestigation struct {
	ResourceType          string            `json:"resource_type"`
	SearchedName          string            `json:"searched_name"`
	Namespace             string            `json:"namespace"`
	Found                 bool              `json:"found"`
	ExactMatch            bool              `json:"exact_match"`
	ActualName            string            `json:"actual_name,omitempty"`
	Keys                  []string          `json:"keys,omitempty"`
	SimilarResources      []SimilarResource `json:"similar_resources,omitempty"`
	FoundInWrongNamespace bool              `json:"found_in_wrong_namespace"`
	CorrectNamespace      string            `json:"correct_namespace,omitempty"`
	Diagnosis             string            `json:"diagnosis"`
	RootCause             string            `json:"root_cause,omitempty"`
	Solution              string            `json:"solution,omitempty"`
}

// SimilarResource recurso similar encontrado
type SimilarResource struct {
	Name       string   `json:"name"`
	Similarity float64  `json:"similarity"`
	Keys       []string `json:"keys,omitempty"`
	Age        string   `json:"age,omitempty"`
}

// ResourceReference referência a um recurso extraída do Pod spec
type ResourceReference struct {
	Type         string
	Name         string
	ExpectedKeys []string
}

// extractPattern extrai padrão base do nome do recurso
// Exemplos:
//   "supply-api-config" → "supply-api"
//   "supply-api-6m6gmhh4tb" → "supply-api"
//   "db-secret-prod" → "db-secret"
func extractPattern(name string) string {
	// Remover sufixos comuns de hash/ambiente
	// Estratégia: pegar primeiros 2 segmentos separados por '-'
	parts := strings.Split(name, "-")
	if len(parts) >= 2 {
		// Se último segmento parece hash (alfanumérico longo), ignorar
		lastPart := parts[len(parts)-1]
		if len(lastPart) >= 8 && isAlphanumeric(lastPart) {
			// Provavelmente hash do Kustomize/Helm
			return strings.Join(parts[:len(parts)-1], "-")
		}
		// Senão, pegar primeiros 2 segmentos
		return strings.Join(parts[:2], "-")
	}
	return name
}

// isAlphanumeric verifica se string é alfanumérica
func isAlphanumeric(s string) bool {
	for _, c := range s {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')) {
			return false
		}
	}
	return true
}

// calculateSimilarityLocal calcula similaridade entre dois nomes (0-100%)
// Usa prefixo comum como métrica simples
func calculateSimilarityLocal(a, b string) float64 {
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

	maxLen := len(a)
	if len(b) > maxLen {
		maxLen = len(b)
	}

	return float64(commonPrefix) / float64(maxLen) * 100
}

// InvestigateConfigMap busca ConfigMap inteligentemente
func (s *SmartInvestigator) InvestigateConfigMap(ctx context.Context, namespace, name string) *ResourceInvestigation {
	result := &ResourceInvestigation{
		ResourceType: "ConfigMap",
		SearchedName: name,
		Namespace:    namespace,
	}

	// 1. Busca exata no namespace especificado
	cm, err := s.clientset.CoreV1().ConfigMaps(namespace).Get(ctx, name, metav1.GetOptions{})
	if err == nil {
		result.Found = true
		result.ExactMatch = true
		result.ActualName = cm.Name
		result.Keys = extractConfigMapKeys(cm)
		result.Diagnosis = fmt.Sprintf("✅ ConfigMap '%s' existe e está acessível", name)
		return result
	}

	// 2. Busca por pattern (supply-api-6m6gmhh4tb → supply-api)
	pattern := extractPattern(name)
	isKustomizeHash := pattern != name // Se padrão é diferente do nome, provavelmente é hash Kustomize/Helm

	fmt.Printf("    🔍 Buscando ConfigMaps com prefixo '%s' (hash detectado: %v)\n", pattern, isKustomizeHash)

	cmList, err := s.clientset.CoreV1().ConfigMaps(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		result.Diagnosis = fmt.Sprintf("❌ Erro ao listar ConfigMaps: %v", err)
		return result
	}

	var similarCMs []SimilarResource
	var allWithPrefix []SimilarResource

	for _, cm := range cmList.Items {
		// Se é padrão Kustomize, listar TODOS com mesmo prefixo (sem filtro de similaridade)
		if strings.HasPrefix(cm.Name, pattern) {
			age := metav1.Now().Time.Sub(cm.CreationTimestamp.Time).String()
			sr := SimilarResource{
				Name:       cm.Name,
				Similarity: calculateSimilarityLocal(name, cm.Name),
				Keys:       extractConfigMapKeys(&cm),
				Age:        age,
			}
			allWithPrefix = append(allWithPrefix, sr)

			if sr.Similarity > 50 {
				similarCMs = append(similarCMs, sr)
			}
		}
	}

	fmt.Printf("    📋 ConfigMaps com prefixo '%s': %d encontrados\n", pattern, len(allWithPrefix))

	// Usar TODOS com prefixo se é hash Kustomize, senão usar apenas similares
	foundCMs := similarCMs
	if isKustomizeHash && len(allWithPrefix) > 0 {
		foundCMs = allWithPrefix
		fmt.Printf("    ✅ Usando todos ConfigMaps com prefixo (padrão Kustomize detectado)\n")
	}

	if len(foundCMs) > 0 {
		result.Found = true
		result.ExactMatch = false
		result.SimilarResources = foundCMs

		// Diagnóstico específico para Kustomize/Helm
		if isKustomizeHash {
			result.Diagnosis = fmt.Sprintf("❌ ConfigMap '%s' (hash Kustomize/Helm) não existe. Encontrados %d ConfigMaps com prefixo '%s'", name, len(foundCMs), pattern)
			result.RootCause = "ConfigMap com hash específico não foi criado. Possíveis causas: (1) Pipeline de deploy falhou, (2) Kustomize não gerou o ConfigMap, (3) ConfigMap foi deletado"
			result.Solution = fmt.Sprintf("OPÇÕES: (1) Re-executar pipeline de deploy para criar '%s', (2) Usar ConfigMap existente '%s' se aplicável, (3) Verificar logs do pipeline CI/CD", name, foundCMs[0].Name)
		} else {
			result.Diagnosis = fmt.Sprintf("❌ ConfigMap '%s' não existe, mas encontrados %d similares", name, len(foundCMs))
			result.RootCause = "Nome hardcoded no Pod não corresponde ao ConfigMap existente"
			result.Solution = fmt.Sprintf("Atualizar Pod para referenciar '%s' OU renomear ConfigMap para '%s'", foundCMs[0].Name, name)
		}
		return result
	}

	// 3. Busca cross-namespace
	namespaces, _ := s.clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	for _, ns := range namespaces.Items {
		if ns.Name == namespace {
			continue
		}
		cm, err := s.clientset.CoreV1().ConfigMaps(ns.Name).Get(ctx, name, metav1.GetOptions{})
		if err == nil {
			result.FoundInWrongNamespace = true
			result.CorrectNamespace = ns.Name
			result.Keys = extractConfigMapKeys(cm)
			result.Diagnosis = fmt.Sprintf("⚠️ ConfigMap '%s' existe no namespace '%s' (deveria estar em '%s')", name, ns.Name, namespace)
			result.RootCause = "ConfigMap criado no namespace errado"
			result.Solution = fmt.Sprintf("Copiar ConfigMap do namespace '%s' para '%s'", ns.Name, namespace)
			return result
		}
	}

	// Não encontrado em lugar nenhum
	result.Found = false
	result.Diagnosis = fmt.Sprintf("❌ ConfigMap '%s' NÃO encontrado no namespace '%s' nem em outros namespaces", name, namespace)
	result.RootCause = "ConfigMap não foi criado ou foi deletado"
	result.Solution = fmt.Sprintf("Criar ConfigMap com: kubectl create configmap %s -n %s --from-file=...", name, namespace)

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
		result.Diagnosis = fmt.Sprintf("❌ Secret '%s' não encontrado no namespace '%s'", name, namespace)
		result.RootCause = "Secret não foi criado ou foi deletado"
		result.Solution = fmt.Sprintf("Criar Secret com: kubectl create secret generic %s -n %s", name, namespace)
		return result
	}

	result.Found = true
	result.ExactMatch = true
	result.ActualName = secret.Name
	result.Keys = extractSecretKeys(secret)

	// Validar se keys esperadas existem
	if len(expectedKeys) > 0 {
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
			result.Diagnosis = fmt.Sprintf("✅ Secret '%s' existe com todas as keys necessárias (%v)", name, expectedKeys)
		}
	} else {
		result.Diagnosis = fmt.Sprintf("✅ Secret '%s' existe (keys: %v)", name, result.Keys)
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
		result.Diagnosis = fmt.Sprintf("❌ PVC '%s' não encontrado no namespace '%s'", name, namespace)
		result.RootCause = "PVC não foi criado ou foi deletado"
		result.Solution = fmt.Sprintf("Criar PVC com YAML apropriado")
		return result
	}

	result.Found = true
	result.ExactMatch = true
	result.ActualName = pvc.Name

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

				if len(availableSCs) > 0 {
					result.Solution = fmt.Sprintf("Atualizar PVC para usar StorageClass válida: %v", availableSCs)
				} else {
					result.Solution = "Instalar um CSI driver e criar StorageClass"
				}
			} else {
				result.RootCause = "PVC aguardando provisionamento (pode demorar alguns segundos)"
				result.Solution = "Aguardar provisionamento ou verificar eventos do PVC: kubectl describe pvc " + name
			}
		}

	case corev1.ClaimLost:
		result.Diagnosis = fmt.Sprintf("❌ PVC '%s' em estado Lost (volume perdido)", name)
		result.RootCause = "Volume backing foi deletado"
		result.Solution = "Recriar PVC e restaurar dados do backup"
	}

	return result
}

// extractConfigMapKeys extrai keys de um ConfigMap
func extractConfigMapKeys(cm *corev1.ConfigMap) []string {
	keys := make([]string, 0, len(cm.Data)+len(cm.BinaryData))
	for k := range cm.Data {
		keys = append(keys, k)
	}
	for k := range cm.BinaryData {
		keys = append(keys, k)
	}
	return keys
}

// extractSecretKeys extrai keys de um Secret
func extractSecretKeys(secret *corev1.Secret) []string {
	keys := make([]string, 0, len(secret.Data))
	for k := range secret.Data {
		keys = append(keys, k)
	}
	return keys
}
