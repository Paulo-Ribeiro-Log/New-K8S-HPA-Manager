package collectors

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Investigator investiga recursos problemáticos no Kubernetes
type Investigator struct {
	clientset *kubernetes.Clientset
	validator *ResourceValidator
}

// NewInvestigator cria um novo investigador
func NewInvestigator(clientset *kubernetes.Clientset) *Investigator {
	return &Investigator{
		clientset: clientset,
		validator: NewResourceValidator(clientset),
	}
}

// InvestigateMissingResources investiga recursos faltantes mencionados em eventos
func (i *Investigator) InvestigateMissingResources(ctx context.Context, diagCtx *DiagnosticContext) (*InvestigationResult, error) {
	fmt.Printf("\n🔍 [INVESTIGATOR] Iniciando investigação automática...\n")
	fmt.Printf("📋 [INVESTIGATOR] Namespace: %s\n", diagCtx.Namespace)

	result := &InvestigationResult{
		MissingResources:  []MissingResource{},
		FoundAlternatives: []AlternativeResource{},
		Recommendations:   []string{},
	}

	// 1. Detectar recursos faltantes nos eventos
	fmt.Printf("📰 [INVESTIGATOR] Analisando %d eventos...\n", len(diagCtx.Events))
	missingResources := i.detectMissingResourcesFromEvents(diagCtx.Events)
	fmt.Printf("❓ [INVESTIGATOR] Recursos faltantes detectados nos eventos: %d\n", len(missingResources))

	// 2. Detectar recursos faltantes nos status de containers
	if diagCtx.Pod != nil && diagCtx.Pod.Manifest != nil {
		fmt.Printf("🐳 [INVESTIGATOR] Analisando status dos containers do Pod...\n")
		podMissing := i.detectMissingResourcesFromPodStatus(diagCtx.Pod.Manifest)
		fmt.Printf("❓ [INVESTIGATOR] Recursos faltantes detectados no Pod: %d\n", len(podMissing))
		missingResources = append(missingResources, podMissing...)
	}

	// 3. Para cada recurso faltante, buscar alternativas
	fmt.Printf("\n🔎 [INVESTIGATOR] Total de recursos faltantes a investigar: %d\n", len(missingResources))
	for idx, missing := range missingResources {
		fmt.Printf("\n[%d/%d] Investigando %s '%s' no namespace '%s'\n", idx+1, len(missingResources), missing.Type, missing.Name, diagCtx.Namespace)
		fmt.Printf("  Motivo: %s\n", missing.Reason)

		result.MissingResources = append(result.MissingResources, missing)

		// Buscar alternativas com wildcard
		fmt.Printf("  🔍 Buscando recursos similares no cluster...\n")
		alternatives := i.searchAlternatives(ctx, missing, diagCtx.Namespace)
		fmt.Printf("  ✅ Encontradas %d alternativas\n", len(alternatives))
		result.FoundAlternatives = append(result.FoundAlternatives, alternatives...)

		// VALIDAÇÃO AUTOMÁTICA: Se encontrou recurso exato, validar referencias
		if len(alternatives) > 0 && diagCtx.Pod != nil && diagCtx.Pod.Manifest != nil {
			for _, alt := range alternatives {
				if alt.Similarity == "exact_match" {
					fmt.Printf("  🔍 [VALIDATOR] Validando %s '%s' automaticamente...\n", missing.Type, alt.FoundName)

					var validation *ValidationResult
					switch missing.Type {
					case "ConfigMap":
						validation = i.validator.ValidateConfigMapReferences(ctx, diagCtx.Pod.Manifest, alt.FoundName, diagCtx.Namespace)
					case "Secret":
						validation = i.validator.ValidateSecretReferences(ctx, diagCtx.Pod.Manifest, alt.FoundName, diagCtx.Namespace)
					case "Service":
						validation = i.validator.ValidateServiceReference(ctx, diagCtx.Pod.Manifest, alt.FoundName, diagCtx.Namespace)
					case "PVC":
						validation = i.validator.ValidatePVCReference(ctx, alt.FoundName, diagCtx.Namespace)
					}

					if validation != nil {
						result.Validations = append(result.Validations, *validation)
						fmt.Printf("  ✅ [VALIDATOR] Validação concluída: %d problemas encontrados\n", len(validation.Issues))
					}
				}
			}
		}

		// Gerar recomendações
		recommendations := i.generateRecommendations(missing, alternatives)
		result.Recommendations = append(result.Recommendations, recommendations...)
	}

	return result, nil
}

// detectMissingResourcesFromEvents detecta recursos faltantes nos eventos
func (i *Investigator) detectMissingResourcesFromEvents(events []corev1.Event) []MissingResource {
	missing := []MissingResource{}

	// Padrões de erro comuns
	patterns := map[string]*regexp.Regexp{
		"ConfigMap": regexp.MustCompile(`configmap\s+"([^"]+)"\s+not found|configmap\s+['"']([^'"']+)['"']\s+not found`),
		"Secret":    regexp.MustCompile(`secret\s+"([^"]+)"\s+not found|secret\s+['"']([^'"']+)['"']\s+not found`),
		"PVC":       regexp.MustCompile(`persistentvolumeclaim\s+"([^"]+)"\s+not found|claim\s+['"']([^'"']+)['"']\s+not found`),
		"Service":   regexp.MustCompile(`service\s+"([^"]+)"\s+not found|no endpoints available for service\s+['"']([^'"']+)['"']`),
		"Image":     regexp.MustCompile(`Failed to pull image\s+"([^"]+)"|image pull failed for\s+"([^"]+)"`),
	}

	for _, event := range events {
		message := event.Message

		for resourceType, pattern := range patterns {
			matches := pattern.FindStringSubmatch(message)
			if len(matches) > 1 {
				// Pegar o primeiro grupo não vazio
				resourceName := ""
				for j := 1; j < len(matches); j++ {
					if matches[j] != "" {
						resourceName = matches[j]
						break
					}
				}

				if resourceName != "" {
					missing = append(missing, MissingResource{
						Type:      resourceType,
						Name:      resourceName,
						Namespace: event.InvolvedObject.Namespace,
						Reason:    fmt.Sprintf("Event: %s - %s", event.Reason, event.Message),
					})
				}
			}
		}
	}

	return missing
}

// detectMissingResourcesFromPodStatus detecta recursos faltantes no status dos containers
func (i *Investigator) detectMissingResourcesFromPodStatus(pod *corev1.Pod) []MissingResource {
	missing := []MissingResource{}

	// Verificar container statuses
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.State.Waiting != nil {
			reason := cs.State.Waiting.Reason
			message := cs.State.Waiting.Message

			// CreateContainerConfigError geralmente indica ConfigMap/Secret faltando
			if reason == "CreateContainerConfigError" {
				// Extrair nome do ConfigMap/Secret da mensagem
				if strings.Contains(message, "configmap") {
					re := regexp.MustCompile(`configmap\s+"([^"]+)"|configmap\s+['"']([^'"']+)['"']`)
					matches := re.FindStringSubmatch(message)
					if len(matches) > 1 {
						name := matches[1]
						if name == "" && len(matches) > 2 {
							name = matches[2]
						}
						if name != "" {
							missing = append(missing, MissingResource{
								Type:      "ConfigMap",
								Name:      name,
								Namespace: pod.Namespace,
								Reason:    fmt.Sprintf("Container %s: %s", cs.Name, message),
							})
						}
					}
				}

				if strings.Contains(message, "secret") {
					re := regexp.MustCompile(`secret\s+"([^"]+)"|secret\s+['"']([^'"']+)['"']`)
					matches := re.FindStringSubmatch(message)
					if len(matches) > 1 {
						name := matches[1]
						if name == "" && len(matches) > 2 {
							name = matches[2]
						}
						if name != "" {
							missing = append(missing, MissingResource{
								Type:      "Secret",
								Name:      name,
								Namespace: pod.Namespace,
								Reason:    fmt.Sprintf("Container %s: %s", cs.Name, message),
							})
						}
					}
				}
			}
		}

		// ErrImagePull/ImagePullBackOff
		if cs.State.Waiting != nil && (cs.State.Waiting.Reason == "ErrImagePull" || cs.State.Waiting.Reason == "ImagePullBackOff") {
			// Extrair nome da imagem
			missing = append(missing, MissingResource{
				Type:      "Image",
				Name:      cs.Image,
				Namespace: pod.Namespace,
				Reason:    fmt.Sprintf("Container %s: %s - %s", cs.Name, cs.State.Waiting.Reason, cs.State.Waiting.Message),
			})
		}
	}

	return missing
}

// searchAlternatives busca recursos similares no namespace
func (i *Investigator) searchAlternatives(ctx context.Context, missing MissingResource, namespace string) []AlternativeResource {
	alternatives := []AlternativeResource{}

	// Extrair padrão base do nome (remover hash/sufixos)
	baseName := extractBaseName(missing.Name)

	switch missing.Type {
	case "ConfigMap":
		alternatives = i.searchConfigMaps(ctx, namespace, baseName, missing.Name)
	case "Secret":
		alternatives = i.searchSecrets(ctx, namespace, baseName, missing.Name)
	case "PVC":
		alternatives = i.searchPVCs(ctx, namespace, baseName, missing.Name)
		// Image não precisa buscar (problema de registry/tag)
	}

	return alternatives
}

// searchConfigMaps busca ConfigMaps similares
func (i *Investigator) searchConfigMaps(ctx context.Context, namespace, baseName, originalName string) []AlternativeResource {
	alternatives := []AlternativeResource{}

	fmt.Printf("    🗂️  Listando ConfigMaps no namespace '%s'...\n", namespace)

	// Primeiro, verificar se o ConfigMap EXISTE exatamente
	cm, err := i.clientset.CoreV1().ConfigMaps(namespace).Get(ctx, originalName, metav1.GetOptions{})
	if err == nil {
		// ConfigMap EXISTE! Isso significa que o erro não é "not found"
		fmt.Printf("    ✅ ConfigMap '%s' EXISTE no cluster!\n", originalName)

		// Coletar conteúdo para análise
		keys := getConfigMapKeys(cm)
		fmt.Printf("    📊 Keys no ConfigMap: %v\n", keys)

		// Preparar dados para análise com mascaramento parcial inteligente
		data := make(map[string]string)
		for k, v := range cm.Data {
			masked := maskConfigMapValue(k, v)
			data[k] = masked
		}

		alternatives = append(alternatives, AlternativeResource{
			Type:       "ConfigMap",
			SearchName: originalName,
			FoundName:  originalName,
			Namespace:  namespace,
			Similarity: "exact_match",
			Content: &ResourceContent{
				Type: "ConfigMap",
				Name: originalName,
				Keys: keys,
				Data: data,
				Size: len(keys),
			},
		})
		return alternatives
	}

	fmt.Printf("    ❌ ConfigMap '%s' NÃO existe. Buscando similares...\n", originalName)

	// Listar todos ConfigMaps do namespace para encontrar similares
	configMaps, err := i.clientset.CoreV1().ConfigMaps(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		fmt.Printf("    ⚠️ Erro ao listar ConfigMaps: %v\n", err)
		return alternatives
	}

	fmt.Printf("    📋 Total de ConfigMaps no namespace: %d\n", len(configMaps.Items))

	for _, cm := range configMaps.Items {

		similarity := calculateSimilarity(originalName, cm.Name, baseName)
		if similarity != "" {
			alternatives = append(alternatives, AlternativeResource{
				Type:       "ConfigMap",
				SearchName: originalName,
				FoundName:  cm.Name,
				Namespace:  namespace,
				Similarity: similarity,
			})
		}
	}

	return alternatives
}

// searchSecrets busca Secrets similares
func (i *Investigator) searchSecrets(ctx context.Context, namespace, baseName, originalName string) []AlternativeResource {
	alternatives := []AlternativeResource{}

	fmt.Printf("    🔐 Listando Secrets no namespace '%s'...\n", namespace)

	// Primeiro, verificar se o Secret EXISTE exatamente
	secret, err := i.clientset.CoreV1().Secrets(namespace).Get(ctx, originalName, metav1.GetOptions{})
	if err == nil {
		// Secret EXISTE!
		fmt.Printf("    ✅ Secret '%s' EXISTE no cluster!\n", originalName)

		// Coletar apenas keys (não valores sensíveis)
		keys := make([]string, 0, len(secret.Data))
		for k := range secret.Data {
			keys = append(keys, k)
		}
		fmt.Printf("    📊 Keys no Secret: %v\n", keys)

		alternatives = append(alternatives, AlternativeResource{
			Type:       "Secret",
			SearchName: originalName,
			FoundName:  originalName,
			Namespace:  namespace,
			Similarity: "exact_match",
			Content: &ResourceContent{
				Type: "Secret",
				Name: originalName,
				Keys: keys,
				Size: len(keys),
				// Data não incluído por segurança
			},
		})
		return alternatives
	}

	fmt.Printf("    ❌ Secret '%s' NÃO existe. Buscando similares...\n", originalName)

	secrets, err := i.clientset.CoreV1().Secrets(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		fmt.Printf("    ⚠️ Erro ao listar Secrets: %v\n", err)
		return alternatives
	}

	fmt.Printf("    📋 Total de Secrets no namespace: %d\n", len(secrets.Items))

	for _, secret := range secrets.Items {

		similarity := calculateSimilarity(originalName, secret.Name, baseName)
		if similarity != "" {
			alternatives = append(alternatives, AlternativeResource{
				Type:       "Secret",
				SearchName: originalName,
				FoundName:  secret.Name,
				Namespace:  namespace,
				Similarity: similarity,
			})
		}
	}

	return alternatives
}

// searchPVCs busca PVCs similares
func (i *Investigator) searchPVCs(ctx context.Context, namespace, baseName, originalName string) []AlternativeResource {
	alternatives := []AlternativeResource{}

	pvcs, err := i.clientset.CoreV1().PersistentVolumeClaims(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return alternatives
	}

	for _, pvc := range pvcs.Items {
		if pvc.Name == originalName {
			continue
		}

		similarity := calculateSimilarity(originalName, pvc.Name, baseName)
		if similarity != "" {
			alternatives = append(alternatives, AlternativeResource{
				Type:       "PVC",
				SearchName: originalName,
				FoundName:  pvc.Name,
				Namespace:  namespace,
				Similarity: similarity,
			})
		}
	}

	return alternatives
}

// extractBaseName extrai nome base removendo hash/sufixos gerados
func extractBaseName(name string) string {
	// Remover sufixos comuns gerados por Kubernetes/Kustomize
	// Exemplos:
	// - supply-api-6m6gmhh4tb → supply-api
	// - app-config-abc123 → app-config
	// - myapp-1234567890 → myapp

	// Padrão: nome-hash (hash = 8-10 caracteres alfanuméricos)
	re := regexp.MustCompile(`^(.+?)[-_]([a-z0-9]{8,10})$`)
	matches := re.FindStringSubmatch(name)
	if len(matches) > 1 {
		return matches[1]
	}

	return name
}

// calculateSimilarity calcula similaridade entre nomes
func calculateSimilarity(original, candidate, baseName string) string {
	// Verificar se compartilham o mesmo prefixo base
	candidateBase := extractBaseName(candidate)

	if candidateBase == baseName {
		return "exact_prefix" // Mesmo prefixo, provavelmente versão diferente
	}

	if strings.HasPrefix(candidate, baseName) || strings.HasPrefix(original, candidateBase) {
		return "prefix_match"
	}

	if strings.Contains(candidate, baseName) || strings.Contains(original, candidateBase) {
		return "contains"
	}

	// Levenshtein distance seria ideal aqui, mas vamos simplificar
	// Verificar se tem palavras em comum
	originalWords := strings.Split(strings.ReplaceAll(original, "-", " "), " ")
	candidateWords := strings.Split(strings.ReplaceAll(candidate, "-", " "), " ")

	commonWords := 0
	for _, ow := range originalWords {
		for _, cw := range candidateWords {
			if ow == cw && len(ow) > 3 { // Palavras com mais de 3 caracteres
				commonWords++
			}
		}
	}

	if commonWords >= 2 {
		return "similar"
	}

	return "" // Não similar
}

// getConfigMapKeys retorna as keys de um ConfigMap
func getConfigMapKeys(cm *corev1.ConfigMap) []string {
	keys := make([]string, 0, len(cm.Data))
	for k := range cm.Data {
		keys = append(keys, k)
	}
	return keys
}

// generateRecommendations gera recomendações baseadas na investigação
func (i *Investigator) generateRecommendations(missing MissingResource, alternatives []AlternativeResource) []string {
	recommendations := []string{}

	if len(alternatives) == 0 {
		// Recurso realmente não existe
		recommendations = append(recommendations, fmt.Sprintf(
			"❌ %s '%s' NÃO EXISTE no namespace '%s'. Criar o recurso:",
			missing.Type, missing.Name, missing.Namespace,
		))

		switch missing.Type {
		case "ConfigMap":
			recommendations = append(recommendations, fmt.Sprintf(
				"kubectl create configmap %s --from-literal=key=value -n %s",
				missing.Name, missing.Namespace,
			))
		case "Secret":
			recommendations = append(recommendations, fmt.Sprintf(
				"kubectl create secret generic %s --from-literal=key=value -n %s",
				missing.Name, missing.Namespace,
			))
		}
	} else {
		// Recursos similares encontrados
		recommendations = append(recommendations, fmt.Sprintf(
			"⚠️ %s '%s' não existe, mas encontrados %d similares:",
			missing.Type, missing.Name, len(alternatives),
		))

		for _, alt := range alternatives {
			recommendations = append(recommendations, fmt.Sprintf(
				"   • %s (similaridade: %s)",
				alt.FoundName, alt.Similarity,
			))
		}

		if len(alternatives) > 0 && alternatives[0].Similarity == "exact_prefix" {
			recommendations = append(recommendations, fmt.Sprintf(
				"💡 Possível erro de referência. Atualizar manifesto para usar '%s'",
				alternatives[0].FoundName,
			))
		}
	}

	return recommendations
}

// maskConfigMapValue mascara parcialmente valores sensíveis
func maskConfigMapValue(key, value string) string {
	// Se key é sensível, mascara mais agressivamente
	isSensitive := isSensitiveKey(key)

	// Connection strings: NÃO MASCARAR - preserva sintaxe completa para análise
	// A AI precisa ver todos os caracteres especiais (@, :, /, etc)
	if isConnectionString(value) {
		// Retorna completo - a sintaxe é crítica para diagnóstico
		return value
	}

	// Valores muito longos (>200 chars) - mascara mas NÃO trunca
	if len(value) > 200 {
		if isSensitive {
			// Mascara parcialmente mas mostra tudo
			return maskPartial(value, 20)
		}
		// Valores não sensíveis longos: mostra completo
		return value
	}

	// Valores médios (>50 chars)
	if len(value) > 50 {
		if isSensitive {
			return maskPartial(value, 4)
		}
		// Valores não sensíveis: mostra completo se <200 chars
		return value
	}

	// Valores curtos
	if isSensitive && len(value) > 10 {
		return maskPartial(value, 3)
	}

	return value
}

// isSensitiveKey verifica se uma key é sensível
func isSensitiveKey(key string) bool {
	sensitivePatterns := []string{
		"password", "passwd", "pwd",
		"secret", "token", "key",
		"credential", "auth",
		"private", "sensitive",
	}

	keyLower := strings.ToLower(key)
	for _, pattern := range sensitivePatterns {
		if strings.Contains(keyLower, pattern) {
			return true
		}
	}

	return false
}

// maskPartial mascara parcialmente um valor
func maskPartial(value string, showChars int) string {
	if len(value) <= showChars*2 {
		return "***REDACTED***"
	}

	prefix := value[:showChars]
	suffix := value[len(value)-showChars:]
	middle := len(value) - (showChars * 2)
	mask := strings.Repeat("*", middle)

	return prefix + mask + suffix
}

// isConnectionString detecta connection strings
func isConnectionString(value string) bool {
	patterns := []string{
		"mongodb://", "postgres://", "mysql://",
		"redis://", "amqp://", "://",
	}

	for _, pattern := range patterns {
		if strings.Contains(value, pattern) {
			return true
		}
	}

	// Detecta pattern user:password@host
	if strings.Contains(value, "@") && strings.Contains(value, ":") {
		parts := strings.Split(value, "@")
		if len(parts) == 2 && strings.Contains(parts[0], ":") {
			return true
		}
	}

	return false
}
