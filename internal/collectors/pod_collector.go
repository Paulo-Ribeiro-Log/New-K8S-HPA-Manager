package collectors

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// PodCollector coleta contexto de diagnóstico de Pods
type PodCollector struct {
	clientset kubernetes.Interface
}

// NewPodCollector cria um novo PodCollector
func NewPodCollector(clientset kubernetes.Interface) *PodCollector {
	return &PodCollector{
		clientset: clientset,
	}
}

// Collect coleta contexto completo de um Pod
func (c *PodCollector) Collect(ctx context.Context, namespace, podName string, req *ContextRequest) (*PodContext, error) {
	podContext := &PodContext{
		Logs:         make(map[string]string),
		PreviousLogs: make(map[string]string),
	}

	// 1. Obter manifesto do Pod
	pod, err := c.clientset.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get pod: %w", err)
	}
	podContext.Manifest = pod

	// 2. Coletar logs dos containers (se habilitado)
	if req.IncludeLogs {
		fmt.Printf("📋 [POD_COLLECTOR] Coletando logs de %d containers...\n", len(pod.Spec.Containers))
		for _, container := range pod.Spec.Containers {
			// Logs atuais
			fmt.Printf("  📄 Coletando logs do container '%s'...\n", container.Name)
			logs, err := c.getContainerLogs(ctx, namespace, podName, container.Name, false, req.LogTailLines)
			if err == nil && logs != "" {
				podContext.Logs[container.Name] = logs
				lineCount := len(strings.Split(logs, "\n"))
				fmt.Printf("  ✅ Logs coletados: %d linhas\n", lineCount)
			} else if err != nil {
				fmt.Printf("  ⚠️ Erro ao coletar logs: %v\n", err)
			} else {
				fmt.Printf("  ⚠️ Logs vazios\n")
			}

			// Logs anteriores (se container crashou)
			previousLogs, err := c.getContainerLogs(ctx, namespace, podName, container.Name, true, req.LogTailLines)
			if err == nil && previousLogs != "" {
				podContext.PreviousLogs[container.Name] = previousLogs
				lineCount := len(strings.Split(previousLogs, "\n"))
				fmt.Printf("  ✅ Logs anteriores coletados: %d linhas\n", lineCount)
			}
		}
	} else {
		fmt.Println("⚠️ [POD_COLLECTOR] IncludeLogs=false - logs NÃO serão coletados")
	}

	// 3. Identificar recursos relacionados
	podContext.RelatedDeployment = c.findRelatedDeployment(pod)
	podContext.RelatedConfigMaps = c.findRelatedConfigMaps(pod)
	podContext.RelatedSecrets = c.findRelatedSecrets(pod)

	// 4. Informações do node
	if pod.Spec.NodeName != "" {
		nodeInfo, err := c.getNodeSummary(ctx, pod.Spec.NodeName)
		if err == nil {
			podContext.NodeInfo = nodeInfo
		}
	}

	return podContext, nil
}

// InvestigateResources executa investigação inteligente de recursos referenciados pelo Pod
func (c *PodCollector) InvestigateResources(ctx context.Context, pod *corev1.Pod, podContext *PodContext) *InvestigationResult {
	investigator := NewSmartInvestigator(c.clientset, pod.Namespace)
	result := &InvestigationResult{
		MissingResources:  []MissingResource{},
		FoundAlternatives: []AlternativeResource{},
		Validations:       []ValidationResult{},
		Recommendations:   []string{},
	}

	// 1. Investigar ConfigMaps
	fmt.Printf("🔍 [SMART_INVESTIGATOR] Investigando %d ConfigMaps...\n", len(podContext.RelatedConfigMaps))
	for _, cmName := range podContext.RelatedConfigMaps {
		investigation := investigator.InvestigateConfigMap(ctx, pod.Namespace, cmName)
		c.processConfigMapInvestigation(investigation, result)
	}

	// 2. Investigar Secrets (extrair expected keys)
	fmt.Printf("🔍 [SMART_INVESTIGATOR] Investigando %d Secrets...\n", len(podContext.RelatedSecrets))
	for _, secretName := range podContext.RelatedSecrets {
		expectedKeys := c.extractExpectedSecretKeys(pod, secretName)
		investigation := investigator.InvestigateSecret(ctx, pod.Namespace, secretName, expectedKeys)
		c.processSecretInvestigation(investigation, result)
	}

	// 3. Investigar PVCs
	pvcs := c.findRelatedPVCs(pod)
	fmt.Printf("🔍 [SMART_INVESTIGATOR] Investigando %d PVCs...\n", len(pvcs))
	for _, pvcName := range pvcs {
		investigation := investigator.InvestigatePVC(ctx, pod.Namespace, pvcName)
		c.processPVCInvestigation(investigation, result)
	}

	// 4. Gerar recomendações finais
	if len(result.MissingResources) > 0 {
		result.Recommendations = append(result.Recommendations,
			fmt.Sprintf("🔴 Encontrados %d recursos faltantes que impedem o Pod de iniciar", len(result.MissingResources)))
	}

	if len(result.FoundAlternatives) > 0 {
		result.Recommendations = append(result.Recommendations,
			fmt.Sprintf("⚠️ Encontrados %d recursos similares com nomes diferentes (possível problema de hash Kustomize/Helm)", len(result.FoundAlternatives)))
	}

	return result
}

// processConfigMapInvestigation converte resultado de investigação de ConfigMap
func (c *PodCollector) processConfigMapInvestigation(inv *ResourceInvestigation, result *InvestigationResult) {
	validation := ValidationResult{
		ResourceType: "ConfigMap",
		ResourceName: inv.SearchedName,
		Exists:       inv.Found,
		Issues:       []string{},
		Suggestions:  []string{},
	}

	if !inv.Found {
		// ConfigMap não existe
		result.MissingResources = append(result.MissingResources, MissingResource{
			Type:      "ConfigMap",
			Name:      inv.SearchedName,
			Namespace: inv.Namespace,
			Reason:    inv.Diagnosis,
		})
		validation.Issues = append(validation.Issues, inv.RootCause)
		validation.Suggestions = append(validation.Suggestions, inv.Solution)
	} else if !inv.ExactMatch && len(inv.SimilarResources) > 0 {
		// ConfigMap similar encontrado (nome diferente)
		for _, similar := range inv.SimilarResources {
			result.FoundAlternatives = append(result.FoundAlternatives, AlternativeResource{
				Type:       "ConfigMap",
				SearchName: inv.SearchedName,
				FoundName:  similar.Name,
				Namespace:  inv.Namespace,
				Similarity: fmt.Sprintf("%.0f%% similar", similar.Similarity),
				Content: &ResourceContent{
					Type: "ConfigMap",
					Name: similar.Name,
					Keys: similar.Keys,
					Size: len(similar.Keys),
				},
			})
		}
		validation.Issues = append(validation.Issues, inv.RootCause)
		validation.Suggestions = append(validation.Suggestions, inv.Solution)
	} else if inv.FoundInWrongNamespace {
		// ConfigMap existe em outro namespace
		validation.Issues = append(validation.Issues, fmt.Sprintf("ConfigMap existe no namespace '%s' ao invés de '%s'", inv.CorrectNamespace, inv.Namespace))
		validation.Suggestions = append(validation.Suggestions, inv.Solution)
	} else {
		// ConfigMap OK
		fmt.Printf("  ✅ ConfigMap '%s' verificado\n", inv.SearchedName)
	}

	result.Validations = append(result.Validations, validation)
}

// processSecretInvestigation converte resultado de investigação de Secret
func (c *PodCollector) processSecretInvestigation(inv *ResourceInvestigation, result *InvestigationResult) {
	validation := ValidationResult{
		ResourceType: "Secret",
		ResourceName: inv.SearchedName,
		Exists:       inv.Found,
		Issues:       []string{},
		Suggestions:  []string{},
	}

	if !inv.Found {
		// Secret não existe
		result.MissingResources = append(result.MissingResources, MissingResource{
			Type:      "Secret",
			Name:      inv.SearchedName,
			Namespace: inv.Namespace,
			Reason:    inv.Diagnosis,
		})
		validation.Issues = append(validation.Issues, inv.RootCause)
		validation.Suggestions = append(validation.Suggestions, inv.Solution)
	} else {
		// Secret existe - verificar se root cause indica keys faltantes
		if strings.Contains(inv.RootCause, "Keys configuradas no Pod não existem") {
			validation.Issues = append(validation.Issues, inv.RootCause)
			validation.Suggestions = append(validation.Suggestions, inv.Solution)
		} else {
			fmt.Printf("  ✅ Secret '%s' verificado\n", inv.SearchedName)
		}
	}

	result.Validations = append(result.Validations, validation)
}

// processPVCInvestigation converte resultado de investigação de PVC
func (c *PodCollector) processPVCInvestigation(inv *ResourceInvestigation, result *InvestigationResult) {
	validation := ValidationResult{
		ResourceType: "PersistentVolumeClaim",
		ResourceName: inv.SearchedName,
		Exists:       inv.Found,
		Issues:       []string{},
		Suggestions:  []string{},
	}

	if !inv.Found {
		// PVC não existe
		result.MissingResources = append(result.MissingResources, MissingResource{
			Type:      "PersistentVolumeClaim",
			Name:      inv.SearchedName,
			Namespace: inv.Namespace,
			Reason:    inv.Diagnosis,
		})
		validation.Issues = append(validation.Issues, inv.RootCause)
		validation.Suggestions = append(validation.Suggestions, inv.Solution)
	} else if inv.RootCause != "" {
		// PVC existe mas tem problemas (Pending, Lost, StorageClass inválida)
		validation.Issues = append(validation.Issues, inv.RootCause)
		validation.Suggestions = append(validation.Suggestions, inv.Solution)
	} else {
		fmt.Printf("  ✅ PVC '%s' verificado\n", inv.SearchedName)
	}

	result.Validations = append(result.Validations, validation)
}

// extractExpectedSecretKeys extrai keys esperadas de um Secret baseado no Pod spec
func (c *PodCollector) extractExpectedSecretKeys(pod *corev1.Pod, secretName string) []string {
	keys := make(map[string]bool)

	for _, container := range pod.Spec.Containers {
		// Procura em env individuais
		for _, env := range container.Env {
			if env.ValueFrom != nil && env.ValueFrom.SecretKeyRef != nil {
				if env.ValueFrom.SecretKeyRef.Name == secretName {
					keys[env.ValueFrom.SecretKeyRef.Key] = true
				}
			}
		}
	}

	// Converte map para slice
	result := make([]string, 0, len(keys))
	for key := range keys {
		result = append(result, key)
	}

	return result
}

// findRelatedPVCs encontra PVCs referenciados pelo pod
func (c *PodCollector) findRelatedPVCs(pod *corev1.Pod) []string {
	pvcs := make(map[string]bool)

	// Procura em volumes
	for _, volume := range pod.Spec.Volumes {
		if volume.PersistentVolumeClaim != nil {
			pvcs[volume.PersistentVolumeClaim.ClaimName] = true
		}
	}

	// Converte map para slice
	result := make([]string, 0, len(pvcs))
	for pvc := range pvcs {
		result = append(result, pvc)
	}

	return result
}

// getContainerLogs obtém logs de um container
func (c *PodCollector) getContainerLogs(ctx context.Context, namespace, podName, containerName string, previous bool, tailLines int64) (string, error) {
	if tailLines == 0 {
		tailLines = 500 // Padrão: 500 linhas
	}

	logOptions := &corev1.PodLogOptions{
		Container: containerName,
		TailLines: &tailLines,
		Previous:  previous,
	}

	// Timeout de 10 segundos para logs
	logCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req := c.clientset.CoreV1().Pods(namespace).GetLogs(podName, logOptions)
	stream, err := req.Stream(logCtx)
	if err != nil {
		return "", err
	}
	defer stream.Close()

	// Limitar leitura a 1MB
	limitedReader := io.LimitReader(stream, 1024*1024)
	logs, err := io.ReadAll(limitedReader)
	if err != nil {
		return "", err
	}

	return string(logs), nil
}

// findRelatedDeployment encontra deployment relacionado ao pod
func (c *PodCollector) findRelatedDeployment(pod *corev1.Pod) string {
	// Procura em ownerReferences
	for _, owner := range pod.OwnerReferences {
		if owner.Kind == "ReplicaSet" {
			// ReplicaSet geralmente tem nome no formato: deployment-name-hash
			// Extraímos o nome do deployment removendo o hash
			rsName := owner.Name
			parts := strings.Split(rsName, "-")
			if len(parts) > 1 {
				// Remove último segmento (hash)
				return strings.Join(parts[:len(parts)-1], "-")
			}
		}
	}

	// Fallback: procura em labels
	if deployName, ok := pod.Labels["app"]; ok {
		return deployName
	}

	return ""
}

// findRelatedConfigMaps encontra configmaps referenciados pelo pod
func (c *PodCollector) findRelatedConfigMaps(pod *corev1.Pod) []string {
	configMaps := make(map[string]bool)

	// Procura em volumes
	for _, volume := range pod.Spec.Volumes {
		if volume.ConfigMap != nil {
			configMaps[volume.ConfigMap.Name] = true
		}
	}

	// Procura em envFrom
	for _, container := range pod.Spec.Containers {
		for _, envFrom := range container.EnvFrom {
			if envFrom.ConfigMapRef != nil {
				configMaps[envFrom.ConfigMapRef.Name] = true
			}
		}

		// Procura em env individuais
		for _, env := range container.Env {
			if env.ValueFrom != nil && env.ValueFrom.ConfigMapKeyRef != nil {
				configMaps[env.ValueFrom.ConfigMapKeyRef.Name] = true
			}
		}
	}

	// Converte map para slice
	result := make([]string, 0, len(configMaps))
	for cm := range configMaps {
		result = append(result, cm)
	}

	return result
}

// findRelatedSecrets encontra secrets referenciados pelo pod
func (c *PodCollector) findRelatedSecrets(pod *corev1.Pod) []string {
	secrets := make(map[string]bool)

	// Procura em volumes
	for _, volume := range pod.Spec.Volumes {
		if volume.Secret != nil {
			secrets[volume.Secret.SecretName] = true
		}
	}

	// Procura em imagePullSecrets
	for _, imagePullSecret := range pod.Spec.ImagePullSecrets {
		secrets[imagePullSecret.Name] = true
	}

	// Procura em envFrom
	for _, container := range pod.Spec.Containers {
		for _, envFrom := range container.EnvFrom {
			if envFrom.SecretRef != nil {
				secrets[envFrom.SecretRef.Name] = true
			}
		}

		// Procura em env individuais
		for _, env := range container.Env {
			if env.ValueFrom != nil && env.ValueFrom.SecretKeyRef != nil {
				secrets[env.ValueFrom.SecretKeyRef.Name] = true
			}
		}
	}

	// Converte map para slice
	result := make([]string, 0, len(secrets))
	for secret := range secrets {
		result = append(result, secret)
	}

	return result
}

// getNodeSummary obtém sumário de informações de um node
func (c *PodCollector) getNodeSummary(ctx context.Context, nodeName string) (*NodeSummary, error) {
	node, err := c.clientset.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	// Determina se node está ready
	ready := false
	for _, condition := range node.Status.Conditions {
		if condition.Type == corev1.NodeReady {
			ready = condition.Status == corev1.ConditionTrue
			break
		}
	}

	return &NodeSummary{
		Name:       node.Name,
		Ready:      ready,
		Conditions: node.Status.Conditions,
	}, nil
}
