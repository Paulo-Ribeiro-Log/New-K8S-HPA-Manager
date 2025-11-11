package kubernetes

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/yaml"

	"k8s-hpa-manager/internal/models"
)

// systemNamespaces lista os namespaces de sistema que devem ser filtrados
var systemNamespaces = map[string]bool{
	"default":           true,
	"kube-system":       true,
	"kube-public":       true,
	"kube-node-lease":   true,
	"keycloak":          true,
	"gatekeeper-system": true,
	"istio-system":      true,
	"istio-injection":   true,
	"cert-manager":      true,
	// Remover namespaces de monitoramento para permitir Prometheus
	// "monitoring":                    true,  // ✅ REMOVIDO - Permitir Prometheus
	// "prometheus":                    true,  // ✅ REMOVIDO - Permitir Prometheus
	// "grafana":                       true,  // ✅ REMOVIDO - Permitir Grafana
	"elastic-system":                true,
	"logging":                       true,
	"dynatrace":                     true,
	"flux-system":                   true,
	"argocd":                        true,
	"guardicore":                    true,
	"guardicore-orch":               true,
	"cattle-system":                 true,
	"longhorn-system":               true,
	"metallb-system":                true,
	"calico-system":                 true,
	"tigera-operator":               true,
	"azure-arc":                     true,
	"cluster-baseline-pod-security": true,
	"dsv":                           true,
	"velero":                        true,
	"calico-apiserver":              true,
	"rbac-manager":                  true,
	"spinnaker":                     true,
	"aks-command":                   true,
	"dsv-system":                    true,
}

// isSystemNamespace verifica se um namespace é de sistema e deve ser filtrado
func isSystemNamespace(namespace string) bool {
	return systemNamespaces[namespace]
}

// Client encapsula as operações do Kubernetes
type Client struct {
	clientset kubernetes.Interface
	cluster   string
}

// NewClient cria um novo cliente Kubernetes
func NewClient(clientset kubernetes.Interface, clusterName string) *Client {
	return &Client{
		clientset: clientset,
		cluster:   clusterName,
	}
}

// ListNamespaces lista todos os namespaces do cluster
func (c *Client) ListNamespaces(ctx context.Context, showSystemNamespaces bool) ([]models.Namespace, error) {
	namespaces, err := c.clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list namespaces in cluster %s: %w", c.cluster, err)
	}

	var result []models.Namespace
	for _, ns := range namespaces.Items {
		// Filtrar namespaces de sistema se showSystemNamespaces for false
		if !showSystemNamespaces && isSystemNamespace(ns.Name) {
			continue
		}

		namespace := models.Namespace{
			Name:     ns.Name,
			Cluster:  c.cluster,
			HPACount: -1, // -1 indica "carregando", será contado assincronamente depois
		}
		result = append(result, namespace)
	}

	return result, nil
}

// ListConfigMaps retorna todos os ConfigMaps considerando filtros simples
func (c *Client) ListConfigMaps(ctx context.Context, namespaces []string, search string, showSystemNamespaces bool) ([]models.ConfigMapSummary, error) {
	var result []models.ConfigMapSummary
	search = strings.ToLower(strings.TrimSpace(search))

	listAllNamespaces := len(namespaces) == 0
	uniqueNamespaces := make(map[string]struct{})
	for _, ns := range namespaces {
		ns = strings.TrimSpace(ns)
		if ns == "" {
			continue
		}
		uniqueNamespaces[ns] = struct{}{}
	}

	appendSummaries := func(items []corev1.ConfigMap) {
		for _, cm := range items {
			if !showSystemNamespaces && isSystemNamespace(cm.Namespace) {
				continue
			}
			if search != "" && !matchesConfigMapSearch(&cm, search) {
				continue
			}
			result = append(result, buildConfigMapSummary(c.cluster, &cm))
		}
	}

	if listAllNamespaces {
		cms, err := c.clientset.CoreV1().ConfigMaps("").List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to list configmaps in cluster %s: %w", c.cluster, err)
		}
		appendSummaries(cms.Items)
		return result, nil
	}

	for ns := range uniqueNamespaces {
		cms, err := c.clientset.CoreV1().ConfigMaps(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to list configmaps in %s/%s: %w", c.cluster, ns, err)
		}
		appendSummaries(cms.Items)
	}

	return result, nil
}

// GetConfigMap retorna o manifesto YAML completo do ConfigMap
func (c *Client) GetConfigMap(ctx context.Context, namespace, name string) (*models.ConfigMapManifest, error) {
	cm, err := c.clientset.CoreV1().ConfigMaps(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get configmap %s/%s in cluster %s: %w", namespace, name, c.cluster, err)
	}

	yamlBytes, err := yaml.Marshal(cm)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal configmap %s/%s: %w", namespace, name, err)
	}

	manifest := &models.ConfigMapManifest{
		Cluster:   c.cluster,
		Namespace: namespace,
		Name:      name,
		YAML:      string(yamlBytes),
		Metadata: models.ConfigMapMetadata{
			UID:             string(cm.UID),
			ResourceVersion: cm.ResourceVersion,
			Labels:          copyStringMap(cm.Labels),
			Annotations:     copyStringMap(cm.Annotations),
		},
	}

	return manifest, nil
}

func matchesConfigMapSearch(cm *corev1.ConfigMap, search string) bool {
	name := strings.ToLower(cm.Name)
	if strings.Contains(name, search) {
		return true
	}
	for k, v := range cm.Labels {
		candidate := strings.ToLower(fmt.Sprintf("%s=%s", k, v))
		if strings.Contains(candidate, search) {
			return true
		}
	}
	return false
}

func buildConfigMapSummary(cluster string, cm *corev1.ConfigMap) models.ConfigMapSummary {
	dataKeys := make([]string, 0, len(cm.Data))
	for key := range cm.Data {
		dataKeys = append(dataKeys, key)
	}
	sort.Strings(dataKeys)

	binaryKeys := make([]string, 0, len(cm.BinaryData))
	for key := range cm.BinaryData {
		binaryKeys = append(binaryKeys, key)
	}
	sort.Strings(binaryKeys)

	updatedAt := cm.CreationTimestamp.Time
	return models.ConfigMapSummary{
		Cluster:         cluster,
		Namespace:       cm.Namespace,
		Name:            cm.Name,
		Labels:          copyStringMap(cm.Labels),
		DataKeys:        dataKeys,
		BinaryKeys:      binaryKeys,
		ResourceVersion: cm.ResourceVersion,
		UpdatedAt:       updatedAt,
	}
}

func copyStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// ValidateConfigMap executa um server-side apply com dry-run
func (c *Client) ValidateConfigMap(ctx context.Context, yamlContent, fieldManager, enforceNamespace string) (*corev1.ConfigMap, error) {
	return c.applyConfigMap(ctx, yamlContent, fieldManager, enforceNamespace, "", true)
}

// ApplyConfigMap aplica (ou dry-run opcionalmente) o ConfigMap no cluster
func (c *Client) ApplyConfigMap(ctx context.Context, yamlContent, fieldManager, enforceNamespace, enforceName string, dryRun bool) (*corev1.ConfigMap, error) {
	return c.applyConfigMap(ctx, yamlContent, fieldManager, enforceNamespace, enforceName, dryRun)
}

func (c *Client) applyConfigMap(ctx context.Context, yamlContent, fieldManager, enforceNamespace, enforceName string, dryRun bool) (*corev1.ConfigMap, error) {
	if strings.TrimSpace(yamlContent) == "" {
		return nil, fmt.Errorf("configmap yaml content cannot be empty")
	}
	if fieldManager == "" {
		fieldManager = "web-configmap-editor"
	}

	payload, namespace, name, err := prepareConfigMapApplyPayload(yamlContent, enforceNamespace, enforceName)
	if err != nil {
		return nil, err
	}

	options := metav1.PatchOptions{FieldManager: fieldManager}
	if dryRun {
		options.DryRun = []string{metav1.DryRunAll}
	}

	result, err := c.clientset.CoreV1().ConfigMaps(namespace).Patch(ctx, name, types.ApplyPatchType, payload, options)
	if err != nil {
		return nil, fmt.Errorf("failed to apply configmap %s/%s in cluster %s: %w", namespace, name, c.cluster, err)
	}

	return result, nil
}

func prepareConfigMapApplyPayload(yamlContent, enforceNamespace, enforceName string) ([]byte, string, string, error) {
	var cm map[string]interface{}
	if err := yaml.Unmarshal([]byte(yamlContent), &cm); err != nil {
		return nil, "", "", fmt.Errorf("invalid configmap yaml: %w", err)
	}

	if len(cm) == 0 {
		return nil, "", "", fmt.Errorf("configmap yaml cannot be empty")
	}

	apiVersion, _ := cm["apiVersion"].(string)
	if strings.TrimSpace(apiVersion) == "" {
		cm["apiVersion"] = "v1"
	}
	kind, _ := cm["kind"].(string)
	if strings.TrimSpace(kind) == "" {
		cm["kind"] = "ConfigMap"
	} else if !strings.EqualFold(kind, "ConfigMap") {
		return nil, "", "", fmt.Errorf("expected kind ConfigMap, got %s", kind)
	}

	metadata, _ := cm["metadata"].(map[string]interface{})
	if metadata == nil {
		metadata = make(map[string]interface{})
	}

	name, _ := metadata["name"].(string)
	name = strings.TrimSpace(name)
	if enforceName != "" {
		enforceName = strings.TrimSpace(enforceName)
		if name == "" {
			name = enforceName
		}
		if name != enforceName {
			return nil, "", "", fmt.Errorf("configmap name mismatch: expected %s, got %s", enforceName, name)
		}
	}
	if name == "" {
		return nil, "", "", fmt.Errorf("configmap metadata.name is required")
	}
	metadata["name"] = name

	namespace := strings.TrimSpace(enforceNamespace)
	if nsRaw, ok := metadata["namespace"].(string); ok {
		ns := strings.TrimSpace(nsRaw)
		if namespace == "" {
			namespace = ns
		} else if ns != "" && ns != namespace {
			return nil, "", "", fmt.Errorf("configmap namespace mismatch: expected %s, got %s", namespace, ns)
		}
	}
	if namespace == "" {
		return nil, "", "", fmt.Errorf("configmap metadata.namespace is required")
	}
	metadata["namespace"] = namespace
	cm["metadata"] = metadata

	jsonPayload, err := json.Marshal(cm)
	if err != nil {
		return nil, "", "", fmt.Errorf("failed to marshal configmap payload: %w", err)
	}

	return jsonPayload, namespace, name, nil
}

// CountHPAs conta o número de HPAs em um namespace
func (c *Client) CountHPAs(ctx context.Context, namespace string) (int, error) {
	hpas, err := c.clientset.AutoscalingV2().HorizontalPodAutoscalers(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return 0, fmt.Errorf("failed to count HPAs in namespace %s/%s: %w", c.cluster, namespace, err)
	}
	return len(hpas.Items), nil
}

// UpdateHPA aplica mudanças em um HPA específico
func (c *Client) UpdateHPA(ctx context.Context, hpa models.HPA) error {
	// Obter o HPA atual do cluster
	currentHPA, err := c.clientset.AutoscalingV2().HorizontalPodAutoscalers(hpa.Namespace).Get(ctx, hpa.Name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get HPA %s/%s in cluster %s: %w", hpa.Namespace, hpa.Name, c.cluster, err)
	}

	// Aplicar mudanças
	if hpa.MinReplicas != nil {
		currentHPA.Spec.MinReplicas = hpa.MinReplicas
	}
	currentHPA.Spec.MaxReplicas = hpa.MaxReplicas

	// Aplicar mudanças de CPU target se especificado
	if hpa.TargetCPU != nil {
		// Encontrar ou criar métrica de CPU
		found := false
		for i, metric := range currentHPA.Spec.Metrics {
			if metric.Type == autoscalingv2.ResourceMetricSourceType &&
				metric.Resource != nil &&
				metric.Resource.Name == "cpu" {
				currentHPA.Spec.Metrics[i].Resource.Target.AverageUtilization = hpa.TargetCPU
				found = true
				break
			}
		}
		if !found {
			// Adicionar métrica de CPU se não existir
			cpuMetric := autoscalingv2.MetricSpec{
				Type: autoscalingv2.ResourceMetricSourceType,
				Resource: &autoscalingv2.ResourceMetricSource{
					Name: "cpu",
					Target: autoscalingv2.MetricTarget{
						Type:               autoscalingv2.UtilizationMetricType,
						AverageUtilization: hpa.TargetCPU,
					},
				},
			}
			currentHPA.Spec.Metrics = append(currentHPA.Spec.Metrics, cpuMetric)
		}
	}

	// Aplicar mudanças de Memory target se especificado
	if hpa.TargetMemory != nil {
		// Encontrar ou criar métrica de Memory
		found := false
		for i, metric := range currentHPA.Spec.Metrics {
			if metric.Type == autoscalingv2.ResourceMetricSourceType &&
				metric.Resource != nil &&
				metric.Resource.Name == "memory" {
				currentHPA.Spec.Metrics[i].Resource.Target.AverageUtilization = hpa.TargetMemory
				found = true
				break
			}
		}
		if !found {
			// Adicionar métrica de Memory se não existir
			memoryMetric := autoscalingv2.MetricSpec{
				Type: autoscalingv2.ResourceMetricSourceType,
				Resource: &autoscalingv2.ResourceMetricSource{
					Name: "memory",
					Target: autoscalingv2.MetricTarget{
						Type:               autoscalingv2.UtilizationMetricType,
						AverageUtilization: hpa.TargetMemory,
					},
				},
			}
			currentHPA.Spec.Metrics = append(currentHPA.Spec.Metrics, memoryMetric)
		}
	}

	// Aplicar as mudanças no cluster
	_, err = c.clientset.AutoscalingV2().HorizontalPodAutoscalers(hpa.Namespace).Update(ctx, currentHPA, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update HPA %s/%s in cluster %s: %w", hpa.Namespace, hpa.Name, c.cluster, err)
	}

	// Atualizar resources do deployment se fornecidos
	if hpa.TargetCPURequest != "" || hpa.TargetCPULimit != "" ||
		hpa.TargetMemoryRequest != "" || hpa.TargetMemoryLimit != "" {
		// Obter o deployment target do HPA
		if currentHPA.Spec.ScaleTargetRef.Kind == "Deployment" {
			deploymentName := currentHPA.Spec.ScaleTargetRef.Name
			deployment, err := c.clientset.AppsV1().Deployments(hpa.Namespace).Get(ctx, deploymentName, metav1.GetOptions{})
			if err != nil {
				return fmt.Errorf("failed to get deployment %s/%s: %w", hpa.Namespace, deploymentName, err)
			}

			// Atualizar resources do primeiro container (assume que é o principal)
			if len(deployment.Spec.Template.Spec.Containers) > 0 {
				container := &deployment.Spec.Template.Spec.Containers[0]

				if container.Resources.Requests == nil {
					container.Resources.Requests = corev1.ResourceList{}
				}
				if container.Resources.Limits == nil {
					container.Resources.Limits = corev1.ResourceList{}
				}

				// CPU Request
				if hpa.TargetCPURequest != "" {
					cpuRequest, err := resource.ParseQuantity(hpa.TargetCPURequest)
					if err != nil {
						return fmt.Errorf("invalid CPU request value %s: %w", hpa.TargetCPURequest, err)
					}
					container.Resources.Requests["cpu"] = cpuRequest
				}

				// CPU Limit
				if hpa.TargetCPULimit != "" {
					cpuLimit, err := resource.ParseQuantity(hpa.TargetCPULimit)
					if err != nil {
						return fmt.Errorf("invalid CPU limit value %s: %w", hpa.TargetCPULimit, err)
					}
					container.Resources.Limits["cpu"] = cpuLimit
				}

				// Memory Request
				if hpa.TargetMemoryRequest != "" {
					memRequest, err := resource.ParseQuantity(hpa.TargetMemoryRequest)
					if err != nil {
						return fmt.Errorf("invalid memory request value %s: %w", hpa.TargetMemoryRequest, err)
					}
					container.Resources.Requests["memory"] = memRequest
				}

				// Memory Limit
				if hpa.TargetMemoryLimit != "" {
					memLimit, err := resource.ParseQuantity(hpa.TargetMemoryLimit)
					if err != nil {
						return fmt.Errorf("invalid memory limit value %s: %w", hpa.TargetMemoryLimit, err)
					}
					container.Resources.Limits["memory"] = memLimit
				}

				// Atualizar deployment
				_, err = c.clientset.AppsV1().Deployments(hpa.Namespace).Update(ctx, deployment, metav1.UpdateOptions{})
				if err != nil {
					return fmt.Errorf("failed to update deployment resources %s/%s: %w", hpa.Namespace, deploymentName, err)
				}
			}
		}
	}

	// Executar rollout de Deployment se solicitado
	if err := c.TriggerRollout(ctx, hpa); err != nil {
		// Log warning but don't fail the update
		fmt.Printf("⚠️  Warning: failed to trigger deployment rollout for %s/%s: %v\n", hpa.Namespace, hpa.Name, err)
	}

	// Executar rollout de DaemonSet se solicitado
	if err := c.TriggerDaemonSetRollout(ctx, hpa); err != nil {
		fmt.Printf("⚠️  Warning: failed to trigger daemonset rollout for %s/%s: %v\n", hpa.Namespace, hpa.Name, err)
	}

	// Executar rollout de StatefulSet se solicitado
	if err := c.TriggerStatefulSetRollout(ctx, hpa); err != nil {
		fmt.Printf("⚠️  Warning: failed to trigger statefulset rollout for %s/%s: %v\n", hpa.Namespace, hpa.Name, err)
	}

	return nil
}

// GetHPA retorna um HPA específico com dados enriquecidos
func (c *Client) GetHPA(ctx context.Context, namespace, name string) (models.HPA, error) {
	hpa, err := c.clientset.AutoscalingV2().HorizontalPodAutoscalers(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return models.HPA{}, fmt.Errorf("failed to get HPA %s/%s in cluster %s: %w", namespace, name, c.cluster, err)
	}

	model := c.convertHPAToModel(hpa)

	if err := c.EnrichHPAWithDeploymentResources(ctx, &model); err != nil {
		fmt.Printf("Warning: failed to load deployment resources for HPA %s/%s: %v\n", model.Namespace, model.Name, err)
	}

	return model, nil
}

// TriggerRollout executa rollout de um deployment (se PerformRollout for true)
func (c *Client) TriggerRollout(ctx context.Context, hpa models.HPA) error {
	if !hpa.PerformRollout {
		return nil // Não executar rollout se não solicitado
	}

	// Obter o target do HPA para encontrar o deployment
	hpaObj, err := c.clientset.AutoscalingV2().HorizontalPodAutoscalers(hpa.Namespace).Get(ctx, hpa.Name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get HPA %s/%s: %w", hpa.Namespace, hpa.Name, err)
	}

	// Verificar se o target é um Deployment
	if hpaObj.Spec.ScaleTargetRef.Kind != "Deployment" {
		return fmt.Errorf("rollout only supported for Deployment targets, found %s", hpaObj.Spec.ScaleTargetRef.Kind)
	}

	deploymentName := hpaObj.Spec.ScaleTargetRef.Name

	// Obter o deployment atual
	deployment, err := c.clientset.AppsV1().Deployments(hpa.Namespace).Get(ctx, deploymentName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get deployment %s/%s: %w", hpa.Namespace, deploymentName, err)
	}

	// Forçar rollout adicionando/atualizando annotation
	if deployment.Spec.Template.Annotations == nil {
		deployment.Spec.Template.Annotations = make(map[string]string)
	}
	deployment.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"] = metav1.Now().Format("2006-01-02T15:04:05Z")

	// Aplicar o rollout
	_, err = c.clientset.AppsV1().Deployments(hpa.Namespace).Update(ctx, deployment, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to trigger rollout for deployment %s/%s: %w", hpa.Namespace, deploymentName, err)
	}

	return nil
}

// TriggerDaemonSetRollout executa rollout de um DaemonSet
func (c *Client) TriggerDaemonSetRollout(ctx context.Context, hpa models.HPA) error {
	if !hpa.PerformDaemonSetRollout {
		return nil // Não executar rollout se não solicitado
	}

	// Para DaemonSets, precisamos identificar qual DaemonSet está relacionado
	// Como HPAs normalmente targetam Deployments, vamos buscar DaemonSets no mesmo namespace
	// que tenham labels similares ou mesmo nome

	// Obter o target do HPA
	hpaObj, err := c.clientset.AutoscalingV2().HorizontalPodAutoscalers(hpa.Namespace).Get(ctx, hpa.Name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get HPA %s/%s: %w", hpa.Namespace, hpa.Name, err)
	}

	targetName := hpaObj.Spec.ScaleTargetRef.Name

	// Tentar encontrar DaemonSet com nome similar
	daemonSet, err := c.clientset.AppsV1().DaemonSets(hpa.Namespace).Get(ctx, targetName, metav1.GetOptions{})
	if err != nil {
		// Se não encontrou pelo nome exato, pode não existir DaemonSet para este HPA
		fmt.Printf("ℹ️  No DaemonSet found with name %s in namespace %s, skipping rollout\n", targetName, hpa.Namespace)
		return nil
	}

	// Forçar rollout adicionando/atualizando annotation
	if daemonSet.Spec.Template.Annotations == nil {
		daemonSet.Spec.Template.Annotations = make(map[string]string)
	}
	daemonSet.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"] = metav1.Now().Format("2006-01-02T15:04:05Z")

	// Aplicar o rollout
	_, err = c.clientset.AppsV1().DaemonSets(hpa.Namespace).Update(ctx, daemonSet, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to trigger rollout for daemonset %s/%s: %w", hpa.Namespace, targetName, err)
	}

	fmt.Printf("✅ DaemonSet rollout triggered for %s/%s\n", hpa.Namespace, targetName)
	return nil
}

// TriggerStatefulSetRollout executa rollout de um StatefulSet
func (c *Client) TriggerStatefulSetRollout(ctx context.Context, hpa models.HPA) error {
	if !hpa.PerformStatefulSetRollout {
		return nil // Não executar rollout se não solicitado
	}

	// Obter o target do HPA
	hpaObj, err := c.clientset.AutoscalingV2().HorizontalPodAutoscalers(hpa.Namespace).Get(ctx, hpa.Name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get HPA %s/%s: %w", hpa.Namespace, hpa.Name, err)
	}

	targetName := hpaObj.Spec.ScaleTargetRef.Name

	// Verificar se o target é um StatefulSet ou buscar por nome
	var statefulSetName string
	if hpaObj.Spec.ScaleTargetRef.Kind == "StatefulSet" {
		statefulSetName = targetName
	} else {
		// Tentar encontrar StatefulSet com nome similar
		statefulSetName = targetName
	}

	statefulSet, err := c.clientset.AppsV1().StatefulSets(hpa.Namespace).Get(ctx, statefulSetName, metav1.GetOptions{})
	if err != nil {
		fmt.Printf("ℹ️  No StatefulSet found with name %s in namespace %s, skipping rollout\n", statefulSetName, hpa.Namespace)
		return nil
	}

	// Forçar rollout adicionando/atualizando annotation
	if statefulSet.Spec.Template.Annotations == nil {
		statefulSet.Spec.Template.Annotations = make(map[string]string)
	}
	statefulSet.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"] = metav1.Now().Format("2006-01-02T15:04:05Z")

	// Aplicar o rollout
	_, err = c.clientset.AppsV1().StatefulSets(hpa.Namespace).Update(ctx, statefulSet, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to trigger rollout for statefulset %s/%s: %w", hpa.Namespace, statefulSetName, err)
	}

	fmt.Printf("✅ StatefulSet rollout triggered for %s/%s\n", hpa.Namespace, statefulSetName)
	return nil
}

// ListHPAs lista todos os HPAs em um namespace
func (c *Client) ListHPAs(ctx context.Context, namespace string) ([]models.HPA, error) {
	hpas, err := c.clientset.AutoscalingV2().HorizontalPodAutoscalers(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list HPAs in namespace %s/%s: %w", c.cluster, namespace, err)
	}

	var result []models.HPA
	for _, hpa := range hpas.Items {
		modelHPA := c.convertHPAToModel(&hpa)

		// Enriquecer com dados de recursos do deployment
		if err := c.EnrichHPAWithDeploymentResources(ctx, &modelHPA); err != nil {
			// Log do erro mas continue processando outros HPAs
			fmt.Printf("Warning: failed to load deployment resources for HPA %s/%s: %v\n", modelHPA.Namespace, modelHPA.Name, err)
		}

		result = append(result, modelHPA)
	}

	return result, nil
}

// UpdateHPA atualiza um HPA

// RolloutDeployment executa rollout restart em um deployment
func (c *Client) RolloutDeployment(ctx context.Context, namespace, deploymentName string) error {
	// Obter deployment
	deployment, err := c.clientset.AppsV1().Deployments(namespace).Get(ctx, deploymentName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get deployment %s/%s/%s: %w", c.cluster, namespace, deploymentName, err)
	}

	// Adicionar annotation para forçar rollout
	if deployment.Spec.Template.Annotations == nil {
		deployment.Spec.Template.Annotations = make(map[string]string)
	}
	deployment.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"] = time.Now().Format(time.RFC3339)

	// Atualizar deployment
	_, err = c.clientset.AppsV1().Deployments(namespace).Update(ctx, deployment, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to rollout deployment %s/%s/%s: %w", c.cluster, namespace, deploymentName, err)
	}

	return nil
}

// GetDeploymentFromHPA obtém o nome do deployment associado ao HPA
func (c *Client) GetDeploymentFromHPA(ctx context.Context, namespace, hpaName string) (string, error) {
	hpa, err := c.clientset.AutoscalingV2().HorizontalPodAutoscalers(namespace).Get(ctx, hpaName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to get HPA %s/%s/%s: %w", c.cluster, namespace, hpaName, err)
	}

	// Verificar se o target é um Deployment
	if hpa.Spec.ScaleTargetRef.Kind == "Deployment" {
		return hpa.Spec.ScaleTargetRef.Name, nil
	}

	return "", fmt.Errorf("HPA %s does not target a Deployment (targets %s)", hpaName, hpa.Spec.ScaleTargetRef.Kind)
}

// TestConnection testa a conectividade com o cluster
func (c *Client) TestConnection(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	_, err := c.clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{Limit: 1})
	return err
}

// CountHPAs conta o número de HPAs em um namespace
// convertHPAToModel converte um HPA do Kubernetes para o modelo interno
func (c *Client) convertHPAToModel(hpa *autoscalingv2.HorizontalPodAutoscaler) models.HPA {
	modelHPA := models.HPA{
		Name:            hpa.Name,
		Namespace:       hpa.Namespace,
		Cluster:         c.cluster,
		MinReplicas:     hpa.Spec.MinReplicas,
		MaxReplicas:     hpa.Spec.MaxReplicas,
		CurrentReplicas: hpa.Status.CurrentReplicas,
		LastUpdated:     time.Now(), // HPA doesn't have LastUpdateTime field
	}

	// Extrair métricas de CPU e Memory
	for _, metric := range hpa.Spec.Metrics {
		if metric.Type == autoscalingv2.ResourceMetricSourceType && metric.Resource != nil {
			switch metric.Resource.Name {
			case corev1.ResourceCPU:
				if metric.Resource.Target.AverageUtilization != nil {
					modelHPA.TargetCPU = metric.Resource.Target.AverageUtilization
				}
			case corev1.ResourceMemory:
				if metric.Resource.Target.AverageUtilization != nil {
					modelHPA.TargetMemory = metric.Resource.Target.AverageUtilization
				}
			}
		}
	}

	// Salvar valores originais
	modelHPA.OriginalValues = &models.HPAValues{
		MinReplicas:  modelHPA.MinReplicas,
		MaxReplicas:  modelHPA.MaxReplicas,
		TargetCPU:    modelHPA.TargetCPU,
		TargetMemory: modelHPA.TargetMemory,
	}

	return modelHPA
}

// updateHPAMetrics atualiza as métricas de um HPA
func (c *Client) updateHPAMetrics(hpa *autoscalingv2.HorizontalPodAutoscaler, model *models.HPA) {
	// Atualizar ou criar métricas
	for i, metric := range hpa.Spec.Metrics {
		if metric.Type == autoscalingv2.ResourceMetricSourceType && metric.Resource != nil {
			switch metric.Resource.Name {
			case corev1.ResourceCPU:
				if model.TargetCPU != nil {
					hpa.Spec.Metrics[i].Resource.Target.AverageUtilization = model.TargetCPU
				}
			case corev1.ResourceMemory:
				if model.TargetMemory != nil {
					hpa.Spec.Metrics[i].Resource.Target.AverageUtilization = model.TargetMemory
				}
			}
		}
	}

	// Se não existem métricas, criar novas
	if len(hpa.Spec.Metrics) == 0 {
		if model.TargetCPU != nil {
			cpuMetric := autoscalingv2.MetricSpec{
				Type: autoscalingv2.ResourceMetricSourceType,
				Resource: &autoscalingv2.ResourceMetricSource{
					Name: corev1.ResourceCPU,
					Target: autoscalingv2.MetricTarget{
						Type:               autoscalingv2.UtilizationMetricType,
						AverageUtilization: model.TargetCPU,
					},
				},
			}
			hpa.Spec.Metrics = append(hpa.Spec.Metrics, cpuMetric)
		}

		if model.TargetMemory != nil {
			memoryMetric := autoscalingv2.MetricSpec{
				Type: autoscalingv2.ResourceMetricSourceType,
				Resource: &autoscalingv2.ResourceMetricSource{
					Name: corev1.ResourceMemory,
					Target: autoscalingv2.MetricTarget{
						Type:               autoscalingv2.UtilizationMetricType,
						AverageUtilization: model.TargetMemory,
					},
				},
			}
			hpa.Spec.Metrics = append(hpa.Spec.Metrics, memoryMetric)
		}
	}
}

// DiscoverClusterResources descobre recursos do cluster em todos os namespaces
func (c *Client) DiscoverClusterResources(showSystemResources bool, prometheusOnly bool, logFunc func(string, ...interface{})) ([]models.ClusterResource, error) {
	var resources []models.ClusterResource

	// Default logger se não for fornecido
	if logFunc == nil {
		logFunc = func(format string, args ...interface{}) {}
	}

	// Listar todos os namespaces
	namespaces, err := c.clientset.CoreV1().Namespaces().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list namespaces: %w", err)
	}

	logFunc("📊 Total de namespaces encontrados: %d", len(namespaces.Items))
	logFunc("⚙️  showSystemResources=%v, prometheusOnly=%v", showSystemResources, prometheusOnly)

	for _, ns := range namespaces.Items {
		// Filtrar namespaces de sistema se necessário
		if !showSystemResources && isSystemNamespace(ns.Name) {
			logFunc("❌ Namespace %s filtrado (sistema)", ns.Name)
			continue
		}
		logFunc("✅ Processando namespace: %s", ns.Name)

		// Descobrir Deployments
		deployments, err := c.clientset.AppsV1().Deployments(ns.Name).List(context.Background(), metav1.ListOptions{})
		if err != nil {
			continue // Continue mesmo com erro em um namespace
		}

		for _, deployment := range deployments.Items {
			resource := c.createResourceFromDeployment(&deployment)

			// Se prometheusOnly, filtrar apenas recursos relacionados ao Prometheus
			if prometheusOnly {
				if isPrometheusRelated(resource.Name, resource.Namespace) {
					logFunc("✅ Deployment Prometheus encontrado: %s/%s", resource.Namespace, resource.Name)
					resources = append(resources, resource)
				} else {
					logFunc("⏭️  Deployment ignorado (não é Prometheus): %s/%s", resource.Namespace, resource.Name)
				}
			} else {
				resources = append(resources, resource)
			}
		}

		// Descobrir StatefulSets
		statefulSets, err := c.clientset.AppsV1().StatefulSets(ns.Name).List(context.Background(), metav1.ListOptions{})
		if err != nil {
			continue
		}

		for _, sts := range statefulSets.Items {
			resource := c.createResourceFromStatefulSet(&sts)

			if prometheusOnly {
				if isPrometheusRelated(resource.Name, resource.Namespace) {
					logFunc("✅ StatefulSet Prometheus encontrado: %s/%s", resource.Namespace, resource.Name)
					resources = append(resources, resource)
				} else {
					logFunc("⏭️  StatefulSet ignorado (não é Prometheus): %s/%s", resource.Namespace, resource.Name)
				}
			} else {
				resources = append(resources, resource)
			}
		}

		// Descobrir DaemonSets
		daemonSets, err := c.clientset.AppsV1().DaemonSets(ns.Name).List(context.Background(), metav1.ListOptions{})
		if err != nil {
			continue
		}

		for _, ds := range daemonSets.Items {
			resource := c.createResourceFromDaemonSet(&ds)

			if prometheusOnly {
				if isPrometheusRelated(resource.Name, resource.Namespace) {
					logFunc("✅ DaemonSet Prometheus encontrado: %s/%s", resource.Namespace, resource.Name)
					resources = append(resources, resource)
				} else {
					logFunc("⏭️  DaemonSet ignorado (não é Prometheus): %s/%s", resource.Namespace, resource.Name)
				}
			} else {
				resources = append(resources, resource)
			}
		}
	}

	logFunc("📊 Total de recursos Prometheus descobertos: %d", len(resources))
	return resources, nil
}

// createResourceFromDeployment cria um ClusterResource a partir de um Deployment
func (c *Client) createResourceFromDeployment(deployment *appsv1.Deployment) models.ClusterResource {
	resource := models.ClusterResource{
		Name:         deployment.Name,
		Namespace:    deployment.Namespace,
		WorkloadType: "Deployment",
		Cluster:      c.cluster,
		Type:         determineResourceType(deployment.Name, deployment.Namespace),
		Component:    extractComponent(deployment.Name),
		Status:       models.ResourceHealthy,
		Replicas:     *deployment.Spec.Replicas,
		Modified:     false,
		Selected:     false,
		LastUpdated:  time.Now(),
	}

	// Extrair recursos dos containers
	if len(deployment.Spec.Template.Spec.Containers) > 0 {
		container := deployment.Spec.Template.Spec.Containers[0] // Pegar o primeiro container

		// Extrair requests
		if container.Resources.Requests != nil {
			if cpu := container.Resources.Requests[corev1.ResourceCPU]; !cpu.IsZero() {
				resource.CurrentCPURequest = cpu.String()
			}
			if memory := container.Resources.Requests[corev1.ResourceMemory]; !memory.IsZero() {
				resource.CurrentMemoryRequest = memory.String()
			}
		}

		// Extrair limits
		if container.Resources.Limits != nil {
			if cpu := container.Resources.Limits[corev1.ResourceCPU]; !cpu.IsZero() {
				resource.CurrentCPULimit = cpu.String()
			}
			if memory := container.Resources.Limits[corev1.ResourceMemory]; !memory.IsZero() {
				resource.CurrentMemoryLimit = memory.String()
			}
		}

		// Definir valores padrão "-" se não houver recursos definidos (mais limpo que N/A)
		if resource.CurrentCPURequest == "" {
			resource.CurrentCPURequest = "-"
		}
		if resource.CurrentMemoryRequest == "" {
			resource.CurrentMemoryRequest = "-"
		}
		if resource.CurrentCPULimit == "" {
			resource.CurrentCPULimit = "-"
		}
		if resource.CurrentMemoryLimit == "" {
			resource.CurrentMemoryLimit = "-"
		}

		// NÃO buscar métricas aqui - será feito de forma assíncrona depois
		// Marcar apenas que precisa de métricas
		if resource.CurrentCPURequest == "-" || resource.CurrentMemoryRequest == "-" {
			// Métricas serão buscadas de forma assíncrona
		}

		// Armazenar valores originais
		resource.OriginalValues = &models.ResourceValues{
			CPURequest:    resource.CurrentCPURequest,
			MemoryRequest: resource.CurrentMemoryRequest,
			CPULimit:      resource.CurrentCPULimit,
			MemoryLimit:   resource.CurrentMemoryLimit,
			Replicas:      resource.Replicas,
		}
	}

	return resource
}

// createResourceFromStatefulSet cria um ClusterResource a partir de um StatefulSet
func (c *Client) createResourceFromStatefulSet(sts *appsv1.StatefulSet) models.ClusterResource {
	resource := models.ClusterResource{
		Name:         sts.Name,
		Namespace:    sts.Namespace,
		WorkloadType: "StatefulSet",
		Cluster:      c.cluster,
		Type:         determineResourceType(sts.Name, sts.Namespace),
		Component:    extractComponent(sts.Name),
		Status:       models.ResourceHealthy,
		Replicas:     *sts.Spec.Replicas,
		Modified:     false,
		Selected:     false,
		LastUpdated:  time.Now(),
	}

	// Extrair recursos dos containers
	if len(sts.Spec.Template.Spec.Containers) > 0 {
		container := sts.Spec.Template.Spec.Containers[0]

		// Extrair requests
		if container.Resources.Requests != nil {
			if cpu := container.Resources.Requests[corev1.ResourceCPU]; !cpu.IsZero() {
				resource.CurrentCPURequest = cpu.String()
			}
			if memory := container.Resources.Requests[corev1.ResourceMemory]; !memory.IsZero() {
				resource.CurrentMemoryRequest = memory.String()
			}
		}

		// Extrair limits
		if container.Resources.Limits != nil {
			if cpu := container.Resources.Limits[corev1.ResourceCPU]; !cpu.IsZero() {
				resource.CurrentCPULimit = cpu.String()
			}
			if memory := container.Resources.Limits[corev1.ResourceMemory]; !memory.IsZero() {
				resource.CurrentMemoryLimit = memory.String()
			}
		}

		// Definir valores padrão "-" se não houver recursos definidos (mais limpo que N/A)
		if resource.CurrentCPURequest == "" {
			resource.CurrentCPURequest = "-"
		}
		if resource.CurrentMemoryRequest == "" {
			resource.CurrentMemoryRequest = "-"
		}
		if resource.CurrentCPULimit == "" {
			resource.CurrentCPULimit = "-"
		}
		if resource.CurrentMemoryLimit == "" {
			resource.CurrentMemoryLimit = "-"
		}

		// NÃO buscar métricas aqui - será feito de forma assíncrona depois
		if resource.CurrentCPURequest == "-" || resource.CurrentMemoryRequest == "-" {
			// Métricas serão buscadas de forma assíncrona
		}

		// Armazenar valores originais
		resource.OriginalValues = &models.ResourceValues{
			CPURequest:    resource.CurrentCPURequest,
			MemoryRequest: resource.CurrentMemoryRequest,
			CPULimit:      resource.CurrentCPULimit,
			MemoryLimit:   resource.CurrentMemoryLimit,
			Replicas:      resource.Replicas,
		}
	}

	// Para StatefulSets, verificar se tem storage
	if len(sts.Spec.VolumeClaimTemplates) > 0 {
		pvc := sts.Spec.VolumeClaimTemplates[0]
		if storage := pvc.Spec.Resources.Requests[corev1.ResourceStorage]; !storage.IsZero() {
			resource.StorageSize = storage.String()
			resource.OriginalValues.StorageSize = storage.String()
		}
	}

	return resource
}

// createResourceFromDaemonSet cria um ClusterResource a partir de um DaemonSet
func (c *Client) createResourceFromDaemonSet(ds *appsv1.DaemonSet) models.ClusterResource {
	resource := models.ClusterResource{
		Name:         ds.Name,
		Namespace:    ds.Namespace,
		WorkloadType: "DaemonSet",
		Cluster:      c.cluster,
		Type:         determineResourceType(ds.Name, ds.Namespace),
		Component:    extractComponent(ds.Name),
		Status:       models.ResourceHealthy,
		Replicas:     1, // DaemonSets não têm replicas fixas, mas indicar 1 para UI
		Modified:     false,
		Selected:     false,
		LastUpdated:  time.Now(),
	}

	// Extrair recursos dos containers
	if len(ds.Spec.Template.Spec.Containers) > 0 {
		container := ds.Spec.Template.Spec.Containers[0]

		// Extrair requests
		if container.Resources.Requests != nil {
			if cpu := container.Resources.Requests[corev1.ResourceCPU]; !cpu.IsZero() {
				resource.CurrentCPURequest = cpu.String()
			}
			if memory := container.Resources.Requests[corev1.ResourceMemory]; !memory.IsZero() {
				resource.CurrentMemoryRequest = memory.String()
			}
		}

		// Extrair limits
		if container.Resources.Limits != nil {
			if cpu := container.Resources.Limits[corev1.ResourceCPU]; !cpu.IsZero() {
				resource.CurrentCPULimit = cpu.String()
			}
			if memory := container.Resources.Limits[corev1.ResourceMemory]; !memory.IsZero() {
				resource.CurrentMemoryLimit = memory.String()
			}
		}

		// Definir valores padrão "-" se não houver recursos definidos (mais limpo que N/A)
		if resource.CurrentCPURequest == "" {
			resource.CurrentCPURequest = "-"
		}
		if resource.CurrentMemoryRequest == "" {
			resource.CurrentMemoryRequest = "-"
		}
		if resource.CurrentCPULimit == "" {
			resource.CurrentCPULimit = "-"
		}
		if resource.CurrentMemoryLimit == "" {
			resource.CurrentMemoryLimit = "-"
		}

		// NÃO buscar métricas aqui - será feito de forma assíncrona depois
		if resource.CurrentCPURequest == "-" || resource.CurrentMemoryRequest == "-" {
			// Métricas serão buscadas de forma assíncrona
		}

		// Armazenar valores originais
		resource.OriginalValues = &models.ResourceValues{
			CPURequest:    resource.CurrentCPURequest,
			MemoryRequest: resource.CurrentMemoryRequest,
			CPULimit:      resource.CurrentCPULimit,
			MemoryLimit:   resource.CurrentMemoryLimit,
			Replicas:      resource.Replicas,
		}
	}

	return resource
}

// determineResourceType determina o tipo do recurso baseado no nome e namespace
func determineResourceType(name, namespace string) models.ResourceType {
	name = strings.ToLower(name)
	namespace = strings.ToLower(namespace)

	// Monitoring
	if strings.Contains(name, "prometheus") || strings.Contains(name, "grafana") ||
		strings.Contains(name, "alertmanager") || namespace == "monitoring" {
		return models.ResourceMonitoring
	}

	// Ingress
	if strings.Contains(name, "nginx") || strings.Contains(name, "ingress") ||
		strings.Contains(name, "istio") || strings.Contains(namespace, "ingress") {
		return models.ResourceIngress
	}

	// Security
	if strings.Contains(name, "cert-manager") || strings.Contains(name, "gatekeeper") ||
		namespace == "cert-manager" || namespace == "gatekeeper-system" {
		return models.ResourceSecurity
	}

	// Storage
	if strings.Contains(name, "longhorn") || strings.Contains(name, "storage") ||
		namespace == "longhorn-system" {
		return models.ResourceStorage
	}

	// Networking
	if strings.Contains(name, "calico") || strings.Contains(name, "metallb") ||
		strings.Contains(name, "cilium") || namespace == "calico-system" ||
		namespace == "metallb-system" {
		return models.ResourceNetworking
	}

	// Logging
	if strings.Contains(name, "elastic") || strings.Contains(name, "fluentd") ||
		strings.Contains(name, "logstash") || namespace == "logging" ||
		namespace == "elastic-system" {
		return models.ResourceLogging
	}

	return models.ResourceCustom
}

// extractComponent extrai o componente principal do nome do recurso
func extractComponent(name string) string {
	name = strings.ToLower(name)

	if strings.Contains(name, "prometheus-server") {
		return "prometheus-server"
	} else if strings.Contains(name, "prometheus") {
		return "prometheus"
	} else if strings.Contains(name, "grafana") {
		return "grafana"
	} else if strings.Contains(name, "alertmanager") {
		return "alertmanager"
	} else if strings.Contains(name, "node-exporter") {
		return "node-exporter"
	}

	return name
}

// isPrometheusRelated verifica se um recurso está relacionado ao Prometheus
func isPrometheusRelated(name, namespace string) bool {
	name = strings.ToLower(name)
	namespace = strings.ToLower(namespace)

	prometheusKeywords := []string{
		"prometheus", "grafana", "alertmanager", "pushgateway",
		"blackbox", "node-exporter", "kube-state-metrics",
	}

	for _, keyword := range prometheusKeywords {
		if strings.Contains(name, keyword) {
			return true
		}
	}

	return namespace == "monitoring" || namespace == "prometheus"
}

// ApplyResourceChanges aplica mudanças nos recursos do cluster
func (c *Client) ApplyResourceChanges(resource *models.ClusterResource) error {
	switch resource.WorkloadType {
	case "Deployment":
		return c.updateDeploymentResources(resource)
	case "StatefulSet":
		return c.updateStatefulSetResources(resource)
	case "DaemonSet":
		return c.updateDaemonSetResources(resource)
	default:
		return fmt.Errorf("unsupported workload type: %s", resource.WorkloadType)
	}
}

// updateDeploymentResources atualiza recursos de um Deployment
func (c *Client) updateDeploymentResources(clusterResource *models.ClusterResource) error {
	deployment, err := c.clientset.AppsV1().Deployments(clusterResource.Namespace).Get(
		context.Background(), clusterResource.Name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get deployment %s: %w", clusterResource.Name, err)
	}

	// Atualizar replicas se especificado
	if clusterResource.TargetReplicas != nil {
		deployment.Spec.Replicas = clusterResource.TargetReplicas
	}

	// Atualizar recursos do container principal
	if len(deployment.Spec.Template.Spec.Containers) > 0 {
		container := &deployment.Spec.Template.Spec.Containers[0]

		if container.Resources.Requests == nil {
			container.Resources.Requests = make(corev1.ResourceList)
		}
		if container.Resources.Limits == nil {
			container.Resources.Limits = make(corev1.ResourceList)
		}

		// Atualizar CPU Request
		if clusterResource.TargetCPURequest != "" {
			if cpu, err := resource.ParseQuantity(clusterResource.TargetCPURequest); err == nil {
				container.Resources.Requests[corev1.ResourceCPU] = cpu
			}
		}

		// Atualizar Memory Request
		if clusterResource.TargetMemoryRequest != "" {
			if memory, err := resource.ParseQuantity(clusterResource.TargetMemoryRequest); err == nil {
				container.Resources.Requests[corev1.ResourceMemory] = memory
			}
		}

		// Atualizar CPU Limit
		if clusterResource.TargetCPULimit != "" {
			if cpu, err := resource.ParseQuantity(clusterResource.TargetCPULimit); err == nil {
				container.Resources.Limits[corev1.ResourceCPU] = cpu
			}
		}

		// Atualizar Memory Limit
		if clusterResource.TargetMemoryLimit != "" {
			if memory, err := resource.ParseQuantity(clusterResource.TargetMemoryLimit); err == nil {
				container.Resources.Limits[corev1.ResourceMemory] = memory
			}
		}
	}

	_, err = c.clientset.AppsV1().Deployments(clusterResource.Namespace).Update(
		context.Background(), deployment, metav1.UpdateOptions{})
	return err
}

// updateStatefulSetResources atualiza recursos de um StatefulSet
func (c *Client) updateStatefulSetResources(clusterResource *models.ClusterResource) error {
	sts, err := c.clientset.AppsV1().StatefulSets(clusterResource.Namespace).Get(
		context.Background(), clusterResource.Name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get statefulset %s: %w", clusterResource.Name, err)
	}

	// Atualizar replicas se especificado
	if clusterResource.TargetReplicas != nil {
		sts.Spec.Replicas = clusterResource.TargetReplicas
	}

	// Atualizar recursos do container principal
	if len(sts.Spec.Template.Spec.Containers) > 0 {
		container := &sts.Spec.Template.Spec.Containers[0]

		if container.Resources.Requests == nil {
			container.Resources.Requests = make(corev1.ResourceList)
		}
		if container.Resources.Limits == nil {
			container.Resources.Limits = make(corev1.ResourceList)
		}

		// Atualizar CPU Request
		if clusterResource.TargetCPURequest != "" {
			if cpu, err := resource.ParseQuantity(clusterResource.TargetCPURequest); err == nil {
				container.Resources.Requests[corev1.ResourceCPU] = cpu
			}
		}

		// Atualizar Memory Request
		if clusterResource.TargetMemoryRequest != "" {
			if memory, err := resource.ParseQuantity(clusterResource.TargetMemoryRequest); err == nil {
				container.Resources.Requests[corev1.ResourceMemory] = memory
			}
		}

		// Atualizar CPU Limit
		if clusterResource.TargetCPULimit != "" {
			if cpu, err := resource.ParseQuantity(clusterResource.TargetCPULimit); err == nil {
				container.Resources.Limits[corev1.ResourceCPU] = cpu
			}
		}

		// Atualizar Memory Limit
		if clusterResource.TargetMemoryLimit != "" {
			if memory, err := resource.ParseQuantity(clusterResource.TargetMemoryLimit); err == nil {
				container.Resources.Limits[corev1.ResourceMemory] = memory
			}
		}
	}

	_, err = c.clientset.AppsV1().StatefulSets(clusterResource.Namespace).Update(
		context.Background(), sts, metav1.UpdateOptions{})
	return err
}

// updateDaemonSetResources atualiza recursos de um DaemonSet
func (c *Client) updateDaemonSetResources(clusterResource *models.ClusterResource) error {
	ds, err := c.clientset.AppsV1().DaemonSets(clusterResource.Namespace).Get(
		context.Background(), clusterResource.Name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get daemonset %s: %w", clusterResource.Name, err)
	}

	// Atualizar recursos do container principal
	if len(ds.Spec.Template.Spec.Containers) > 0 {
		container := &ds.Spec.Template.Spec.Containers[0]

		if container.Resources.Requests == nil {
			container.Resources.Requests = make(corev1.ResourceList)
		}
		if container.Resources.Limits == nil {
			container.Resources.Limits = make(corev1.ResourceList)
		}

		// Atualizar CPU Request
		if clusterResource.TargetCPURequest != "" {
			if cpu, err := resource.ParseQuantity(clusterResource.TargetCPURequest); err == nil {
				container.Resources.Requests[corev1.ResourceCPU] = cpu
			}
		}

		// Atualizar Memory Request
		if clusterResource.TargetMemoryRequest != "" {
			if memory, err := resource.ParseQuantity(clusterResource.TargetMemoryRequest); err == nil {
				container.Resources.Requests[corev1.ResourceMemory] = memory
			}
		}

		// Atualizar CPU Limit
		if clusterResource.TargetCPULimit != "" {
			if cpu, err := resource.ParseQuantity(clusterResource.TargetCPULimit); err == nil {
				container.Resources.Limits[corev1.ResourceCPU] = cpu
			}
		}

		// Atualizar Memory Limit
		if clusterResource.TargetMemoryLimit != "" {
			if memory, err := resource.ParseQuantity(clusterResource.TargetMemoryLimit); err == nil {
				container.Resources.Limits[corev1.ResourceMemory] = memory
			}
		}
	}

	_, err = c.clientset.AppsV1().DaemonSets(clusterResource.Namespace).Update(
		context.Background(), ds, metav1.UpdateOptions{})
	return err
}

// EnrichHPAWithDeploymentResources enriquece o HPA com informações de recursos do deployment
func (c *Client) EnrichHPAWithDeploymentResources(ctx context.Context, hpa *models.HPA) error {
	// Obter o deployment associado ao HPA
	deploymentName, err := c.GetDeploymentFromHPA(ctx, hpa.Namespace, hpa.Name)
	if err != nil {
		return fmt.Errorf("failed to get deployment for HPA %s: %w", hpa.Name, err)
	}

	// Obter informações do deployment
	deployment, err := c.clientset.AppsV1().Deployments(hpa.Namespace).Get(ctx, deploymentName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get deployment %s: %w", deploymentName, err)
	}

	hpa.DeploymentName = deploymentName

	// Extrair recursos CONFIGURADOS do primeiro container (Target* = configuração)
	if len(deployment.Spec.Template.Spec.Containers) > 0 {
		container := deployment.Spec.Template.Spec.Containers[0]

		// CPU Request (configurado no deployment)
		if cpuReq, exists := container.Resources.Requests[corev1.ResourceCPU]; exists {
			hpa.TargetCPURequest = cpuReq.String()
		}

		// CPU Limit (configurado no deployment)
		if cpuLimit, exists := container.Resources.Limits[corev1.ResourceCPU]; exists {
			hpa.TargetCPULimit = cpuLimit.String()
		}

		// Memory Request (configurado no deployment)
		if memReq, exists := container.Resources.Requests[corev1.ResourceMemory]; exists {
			hpa.TargetMemoryRequest = memReq.String()
		}

		// Memory Limit (configurado no deployment)
		if memLimit, exists := container.Resources.Limits[corev1.ResourceMemory]; exists {
			hpa.TargetMemoryLimit = memLimit.String()
		}
	}

	// Obter métricas de USO REAL do Metrics Server (Current* = uso corrente)
	// TODO: Implementar coleta de métricas reais via Metrics Server API
	// Por enquanto, Current* ficam vazios (serão preenchidos via metrics server)

	// Atualizar valores originais para incluir recursos do deployment
	if hpa.OriginalValues != nil {
		hpa.OriginalValues.DeploymentName = hpa.DeploymentName
		hpa.OriginalValues.CPURequest = hpa.TargetCPURequest
		hpa.OriginalValues.CPULimit = hpa.TargetCPULimit
		hpa.OriginalValues.MemoryRequest = hpa.TargetMemoryRequest
		hpa.OriginalValues.MemoryLimit = hpa.TargetMemoryLimit
	}

	return nil
}

// ApplyHPADeploymentResourceChanges aplica mudanças de recursos no deployment do HPA
func (c *Client) ApplyHPADeploymentResourceChanges(ctx context.Context, hpa *models.HPA) error {
	if hpa.DeploymentName == "" {
		return fmt.Errorf("deployment name not set for HPA %s", hpa.Name)
	}

	// Obter o deployment
	deployment, err := c.clientset.AppsV1().Deployments(hpa.Namespace).Get(ctx, hpa.DeploymentName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get deployment %s: %w", hpa.DeploymentName, err)
	}

	// Atualizar recursos do primeiro container
	if len(deployment.Spec.Template.Spec.Containers) > 0 {
		container := &deployment.Spec.Template.Spec.Containers[0]

		if container.Resources.Requests == nil {
			container.Resources.Requests = make(corev1.ResourceList)
		}
		if container.Resources.Limits == nil {
			container.Resources.Limits = make(corev1.ResourceList)
		}

		// Aplicar CPU Request
		if hpa.TargetCPURequest != "" {
			if cpu, err := resource.ParseQuantity(hpa.TargetCPURequest); err == nil {
				container.Resources.Requests[corev1.ResourceCPU] = cpu
			}
		}

		// Aplicar CPU Limit
		if hpa.TargetCPULimit != "" {
			if cpu, err := resource.ParseQuantity(hpa.TargetCPULimit); err == nil {
				container.Resources.Limits[corev1.ResourceCPU] = cpu
			}
		}

		// Aplicar Memory Request
		if hpa.TargetMemoryRequest != "" {
			if memory, err := resource.ParseQuantity(hpa.TargetMemoryRequest); err == nil {
				container.Resources.Requests[corev1.ResourceMemory] = memory
			}
		}

		// Aplicar Memory Limit
		if hpa.TargetMemoryLimit != "" {
			if memory, err := resource.ParseQuantity(hpa.TargetMemoryLimit); err == nil {
				container.Resources.Limits[corev1.ResourceMemory] = memory
			}
		}
	}

	// Aplicar mudanças
	_, err = c.clientset.AppsV1().Deployments(hpa.Namespace).Update(ctx, deployment, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update deployment %s: %w", hpa.DeploymentName, err)
	}

	// Marcar como não modificado
	hpa.ResourcesModified = false

	return nil
}

// GetPodMetrics busca métricas de uso real de CPU e memória dos pods via kubectl top
func (c *Client) GetPodMetrics(namespace, resourceName, workloadType string) (cpuUsage, memUsage string) {
	contextName := c.cluster

	// Tentar múltiplas estratégias de label selector
	labelSelectors := []string{
		fmt.Sprintf("app=%s", resourceName),
		fmt.Sprintf("app.kubernetes.io/name=%s", resourceName),
		fmt.Sprintf("app.kubernetes.io/instance=%s", resourceName),
		fmt.Sprintf("app.kubernetes.io/component=%s", resourceName),
		"", // Buscar todos os pods e filtrar por nome depois
	}

	var output []byte
	var err error

	// Tentar cada label selector
	for _, selector := range labelSelectors {
		var cmd *exec.Cmd
		if selector == "" {
			cmd = exec.Command("kubectl", "--context", contextName, "top", "pods", "-n", namespace, "--no-headers")
		} else {
			cmd = exec.Command("kubectl", "--context", contextName, "top", "pods", "-n", namespace, "-l", selector, "--no-headers")
		}

		output, err = cmd.CombinedOutput()
		outputStr := string(output)

		// Verificar se a saída contém "No resources found"
		if strings.Contains(outputStr, "No resources found") {
			continue
		}

		if err == nil && len(output) > 0 {
			break
		}
	}

	if err != nil || len(output) == 0 {
		return "-", "-"
	}

	// Parse da saída (formato: POD_NAME CPU MEMORY)
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) == 0 || lines[0] == "" {
		return "-", "-"
	}

	// Filtrar pelo nome do recurso
	var targetLine string
	for _, line := range lines {
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 3 {
			podName := fields[0]
			if strings.Contains(podName, resourceName) {
				targetLine = line
				break
			}
		}
	}

	// Se não encontrou match, usar primeira linha
	if targetLine == "" {
		targetLine = lines[0]
	}

	// Parse da linha selecionada
	fields := strings.Fields(targetLine)
	if len(fields) >= 3 {
		cpuUsage = fields[1]
		memUsage = fields[2]
	}

	return cpuUsage, memUsage
}

// RolloutStatefulSet executa rollout de um StatefulSet genérico
func (c *Client) RolloutStatefulSet(ctx context.Context, namespace, statefulSetName string) error {
	// Obter statefulset
	statefulSet, err := c.clientset.AppsV1().StatefulSets(namespace).Get(ctx, statefulSetName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get statefulset %s/%s/%s: %w", c.cluster, namespace, statefulSetName, err)
	}

	// Adicionar annotation para forçar rollout
	if statefulSet.Spec.Template.Annotations == nil {
		statefulSet.Spec.Template.Annotations = make(map[string]string)
	}
	statefulSet.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"] = time.Now().Format(time.RFC3339)

	// Atualizar statefulset
	_, err = c.clientset.AppsV1().StatefulSets(namespace).Update(ctx, statefulSet, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to rollout statefulset %s/%s/%s: %w", c.cluster, namespace, statefulSetName, err)
	}

	return nil
}

// RolloutDaemonSet executa rollout de um DaemonSet genérico
func (c *Client) RolloutDaemonSet(ctx context.Context, namespace, daemonSetName string) error {
	// Obter daemonset
	daemonSet, err := c.clientset.AppsV1().DaemonSets(namespace).Get(ctx, daemonSetName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get daemonset %s/%s/%s: %w", c.cluster, namespace, daemonSetName, err)
	}

	// Adicionar annotation para forçar rollout
	if daemonSet.Spec.Template.Annotations == nil {
		daemonSet.Spec.Template.Annotations = make(map[string]string)
	}
	daemonSet.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"] = time.Now().Format(time.RFC3339)

	// Atualizar daemonset
	_, err = c.clientset.AppsV1().DaemonSets(namespace).Update(ctx, daemonSet, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to rollout daemonset %s/%s/%s: %w", c.cluster, namespace, daemonSetName, err)
	}

	return nil
}
