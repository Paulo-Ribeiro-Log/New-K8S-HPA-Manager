package kubernetes

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	metricsv1beta1 "k8s.io/metrics/pkg/apis/metrics/v1beta1"
	metricsclientset "k8s.io/metrics/pkg/client/clientset/versioned"
	"sigs.k8s.io/yaml"

	"k8s-hpa-manager/internal/history"
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
	// "spinnaker":                     true,
	"aks-command": true,
	"dsv-system":  true,
}

// isSystemNamespace verifica se um namespace é de sistema e deve ser filtrado
func isSystemNamespace(namespace string) bool {
	return systemNamespaces[namespace]
}

// Client encapsula as operações do Kubernetes
type Client struct {
	clientset      kubernetes.Interface
	metricsClient  *metricsclientset.Clientset
	cluster        string
	historyTracker *history.HistoryTracker
}

// NewClient cria um novo cliente Kubernetes
func NewClient(clientset kubernetes.Interface, clusterName string) *Client {
	return &Client{
		clientset:      clientset,
		cluster:        clusterName,
		historyTracker: nil, // Será configurado via SetHistoryTracker se necessário
	}
}

// GetClientset retorna o clientset do Kubernetes
func (c *Client) GetClientset() kubernetes.Interface {
	return c.clientset
}

// SetHistoryTracker configura o historyTracker para audit logging
func (c *Client) SetHistoryTracker(tracker *history.HistoryTracker) {
	c.historyTracker = tracker
}

// ListNamespaces lista todos os namespaces do cluster
// Retorna todos os namespaces com o campo IsSystem marcado, permitindo que o frontend filtre
func (c *Client) ListNamespaces(ctx context.Context, showSystemNamespaces bool) ([]models.Namespace, error) {
	namespaces, err := c.clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list namespaces in cluster %s: %w", c.cluster, err)
	}

	var result []models.Namespace
	for _, ns := range namespaces.Items {
		isSystem := isSystemNamespace(ns.Name)

		// Filtrar namespaces de sistema se showSystemNamespaces for false
		if !showSystemNamespaces && isSystem {
			continue
		}

		namespace := models.Namespace{
			Name:     ns.Name,
			Cluster:  c.cluster,
			HPACount: -1, // -1 indica "carregando", será contado assincronamente depois
			IsSystem: isSystem,
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

// formatAge formata a idade de um recurso em formato legível (exibe as 2 unidades mais significativas)
func formatAge(t time.Time) string {
	duration := time.Since(t)

	totalSeconds := int(duration.Seconds())

	years := totalSeconds / (365 * 24 * 3600)
	remainingAfterYears := totalSeconds % (365 * 24 * 3600)

	days := remainingAfterYears / (24 * 3600)
	remainingAfterDays := remainingAfterYears % (24 * 3600)

	hours := remainingAfterDays / 3600
	remainingAfterHours := remainingAfterDays % 3600

	minutes := remainingAfterHours / 60
	seconds := remainingAfterHours % 60

	// Exibir as 2 unidades mais significativas
	if years > 0 {
		if days > 0 {
			return fmt.Sprintf("%dy%dd", years, days)
		}
		return fmt.Sprintf("%dy", years)
	}
	if days > 0 {
		if hours > 0 {
			return fmt.Sprintf("%dd%dh", days, hours)
		}
		return fmt.Sprintf("%dd", days)
	}
	if hours > 0 {
		if minutes > 0 {
			return fmt.Sprintf("%dh%dm", hours, minutes)
		}
		return fmt.Sprintf("%dh", hours)
	}
	if minutes > 0 {
		if seconds > 0 {
			return fmt.Sprintf("%dm%ds", minutes, seconds)
		}
		return fmt.Sprintf("%dm", minutes)
	}
	return fmt.Sprintf("%ds", seconds)
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

	// Force=true permite assumir ownership de campos gerenciados por outros field managers
	// Necessário quando ConfigMaps são gerenciados por kubectl, helm, terraform, etc.
	forceFlag := true
	options := metav1.PatchOptions{
		FieldManager: fieldManager,
		Force:        &forceFlag,
	}
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

// ============================================================================
// Ingress Methods
// ============================================================================

// ListIngresses retorna todos os Ingresses considerando filtros simples
func (c *Client) ListIngresses(ctx context.Context, namespaces []string, search string, showSystemNamespaces bool) ([]models.IngressSummary, error) {
	var result []models.IngressSummary
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

	appendSummaries := func(items []networkingv1.Ingress) {
		for _, ing := range items {
			if !showSystemNamespaces && isSystemNamespace(ing.Namespace) {
				continue
			}
			if search != "" && !matchesIngressSearch(&ing, search) {
				continue
			}
			result = append(result, buildIngressSummary(c.cluster, &ing))
		}
	}

	if listAllNamespaces {
		ings, err := c.clientset.NetworkingV1().Ingresses("").List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to list ingresses in cluster %s: %w", c.cluster, err)
		}
		appendSummaries(ings.Items)
		return result, nil
	}

	for ns := range uniqueNamespaces {
		ings, err := c.clientset.NetworkingV1().Ingresses(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to list ingresses in %s/%s: %w", c.cluster, ns, err)
		}
		appendSummaries(ings.Items)
	}

	return result, nil
}

// GetIngress retorna o manifesto YAML completo do Ingress
func (c *Client) GetIngress(ctx context.Context, namespace, name string) (*models.IngressManifest, error) {
	ing, err := c.clientset.NetworkingV1().Ingresses(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get ingress %s/%s in cluster %s: %w", namespace, name, c.cluster, err)
	}

	yamlBytes, err := yaml.Marshal(ing)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal ingress %s/%s: %w", namespace, name, err)
	}

	manifest := &models.IngressManifest{
		Cluster:   c.cluster,
		Namespace: namespace,
		Name:      name,
		YAML:      string(yamlBytes),
		Metadata: models.IngressMetadata{
			UID:             string(ing.UID),
			ResourceVersion: ing.ResourceVersion,
			Labels:          copyStringMap(ing.Labels),
			Annotations:     copyStringMap(ing.Annotations),
		},
	}

	return manifest, nil
}

func matchesIngressSearch(ing *networkingv1.Ingress, search string) bool {
	name := strings.ToLower(ing.Name)
	if strings.Contains(name, search) {
		return true
	}
	for k, v := range ing.Labels {
		candidate := strings.ToLower(fmt.Sprintf("%s=%s", k, v))
		if strings.Contains(candidate, search) {
			return true
		}
	}
	// Pesquisar por hosts
	if ing.Spec.Rules != nil {
		for _, rule := range ing.Spec.Rules {
			host := strings.ToLower(rule.Host)
			if strings.Contains(host, search) {
				return true
			}
		}
	}
	return false
}

func buildIngressSummary(cluster string, ing *networkingv1.Ingress) models.IngressSummary {
	hosts := make([]string, 0)
	if ing.Spec.Rules != nil {
		for _, rule := range ing.Spec.Rules {
			if rule.Host != "" {
				hosts = append(hosts, rule.Host)
			}
		}
	}

	addresses := make([]string, 0)
	if ing.Status.LoadBalancer.Ingress != nil {
		for _, lb := range ing.Status.LoadBalancer.Ingress {
			if lb.IP != "" {
				addresses = append(addresses, lb.IP)
			}
			if lb.Hostname != "" {
				addresses = append(addresses, lb.Hostname)
			}
		}
	}

	ingressClass := ""
	if ing.Spec.IngressClassName != nil {
		ingressClass = *ing.Spec.IngressClassName
	}

	updatedAt := ing.CreationTimestamp.Time
	return models.IngressSummary{
		Cluster:         cluster,
		Namespace:       ing.Namespace,
		Name:            ing.Name,
		Labels:          copyStringMap(ing.Labels),
		IngressClass:    ingressClass,
		Hosts:           hosts,
		Addresses:       addresses,
		ResourceVersion: ing.ResourceVersion,
		UpdatedAt:       updatedAt,
	}
}

// ValidateIngress executa um server-side apply com dry-run
func (c *Client) ValidateIngress(ctx context.Context, yamlContent, fieldManager, enforceNamespace string) (*networkingv1.Ingress, error) {
	return c.applyIngress(ctx, yamlContent, fieldManager, enforceNamespace, "", true)
}

// ApplyIngress aplica (ou dry-run opcionalmente) o Ingress no cluster
func (c *Client) ApplyIngress(ctx context.Context, yamlContent, fieldManager, enforceNamespace, enforceName string, dryRun bool) (*networkingv1.Ingress, error) {
	return c.applyIngress(ctx, yamlContent, fieldManager, enforceNamespace, enforceName, dryRun)
}

func (c *Client) applyIngress(ctx context.Context, yamlContent, fieldManager, enforceNamespace, enforceName string, dryRun bool) (*networkingv1.Ingress, error) {
	if strings.TrimSpace(yamlContent) == "" {
		return nil, fmt.Errorf("ingress yaml content cannot be empty")
	}
	if fieldManager == "" {
		fieldManager = "web-ingress-editor"
	}

	payload, namespace, name, err := prepareIngressApplyPayload(yamlContent, enforceNamespace, enforceName)
	if err != nil {
		return nil, err
	}

	// Force=true permite assumir ownership de campos gerenciados por outros field managers
	// Necessário quando Ingresses são gerenciados por kubectl, helm, terraform, etc.
	forceFlag := true
	options := metav1.PatchOptions{
		FieldManager: fieldManager,
		Force:        &forceFlag,
	}
	if dryRun {
		options.DryRun = []string{metav1.DryRunAll}
	}

	result, err := c.clientset.NetworkingV1().Ingresses(namespace).Patch(ctx, name, types.ApplyPatchType, payload, options)
	if err != nil {
		return nil, fmt.Errorf("failed to apply ingress %s/%s in cluster %s: %w", namespace, name, c.cluster, err)
	}

	return result, nil
}

func prepareIngressApplyPayload(yamlContent, enforceNamespace, enforceName string) ([]byte, string, string, error) {
	var ing map[string]interface{}
	if err := yaml.Unmarshal([]byte(yamlContent), &ing); err != nil {
		return nil, "", "", fmt.Errorf("invalid ingress yaml: %w", err)
	}

	if len(ing) == 0 {
		return nil, "", "", fmt.Errorf("ingress yaml cannot be empty")
	}

	apiVersion, _ := ing["apiVersion"].(string)
	if strings.TrimSpace(apiVersion) == "" {
		ing["apiVersion"] = "networking.k8s.io/v1"
	}
	kind, _ := ing["kind"].(string)
	if strings.TrimSpace(kind) == "" {
		ing["kind"] = "Ingress"
	} else if !strings.EqualFold(kind, "Ingress") {
		return nil, "", "", fmt.Errorf("expected kind Ingress, got %s", kind)
	}

	metadata, _ := ing["metadata"].(map[string]interface{})
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
			return nil, "", "", fmt.Errorf("ingress name mismatch: expected %s, got %s", enforceName, name)
		}
	}
	if name == "" {
		return nil, "", "", fmt.Errorf("ingress metadata.name is required")
	}
	metadata["name"] = name

	namespace := strings.TrimSpace(enforceNamespace)
	if nsRaw, ok := metadata["namespace"].(string); ok {
		ns := strings.TrimSpace(nsRaw)
		if namespace == "" {
			namespace = ns
		} else if ns != "" && ns != namespace {
			return nil, "", "", fmt.Errorf("ingress namespace mismatch: expected %s, got %s", namespace, ns)
		}
	}
	if namespace == "" {
		return nil, "", "", fmt.Errorf("ingress metadata.namespace is required")
	}
	metadata["namespace"] = namespace
	ing["metadata"] = metadata

	jsonPayload, err := json.Marshal(ing)
	if err != nil {
		return nil, "", "", fmt.Errorf("failed to marshal ingress payload: %w", err)
	}

	return jsonPayload, namespace, name, nil
}

// ============================================================================
// Namespaces Methods
// ============================================================================

// GetNamespace retorna o manifesto YAML completo do Namespace
func (c *Client) GetNamespace(ctx context.Context, name string) (*models.NamespaceManifest, error) {
	ns, err := c.clientset.CoreV1().Namespaces().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get namespace %s in cluster %s: %w", name, c.cluster, err)
	}

	yamlBytes, err := yaml.Marshal(ns)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal namespace %s: %w", name, err)
	}

	// Calcular status e age
	status := string(ns.Status.Phase)
	if status == "" {
		status = "Active"
	}
	age := formatAge(ns.CreationTimestamp.Time)

	manifest := &models.NamespaceManifest{
		Cluster: c.cluster,
		Name:    name,
		YAML:    string(yamlBytes),
		Status:  status,
		Age:     age,
		Metadata: models.NamespaceMetadata{
			UID:             string(ns.UID),
			ResourceVersion: ns.ResourceVersion,
			Labels:          copyStringMap(ns.Labels),
			Annotations:     copyStringMap(ns.Annotations),
		},
	}

	return manifest, nil
}

// ValidateNamespace executa um server-side apply com dry-run
func (c *Client) ValidateNamespace(ctx context.Context, yamlContent, fieldManager string) (*corev1.Namespace, error) {
	return c.applyNamespace(ctx, yamlContent, fieldManager, "", true)
}

// ApplyNamespace aplica (ou dry-run opcionalmente) o Namespace no cluster
func (c *Client) ApplyNamespace(ctx context.Context, yamlContent, fieldManager, enforceName string, dryRun bool) (*corev1.Namespace, error) {
	return c.applyNamespace(ctx, yamlContent, fieldManager, enforceName, dryRun)
}

func (c *Client) applyNamespace(ctx context.Context, yamlContent, fieldManager, enforceName string, dryRun bool) (*corev1.Namespace, error) {
	if strings.TrimSpace(yamlContent) == "" {
		return nil, fmt.Errorf("namespace yaml content cannot be empty")
	}
	if fieldManager == "" {
		fieldManager = "web-namespace-editor"
	}

	payload, name, err := prepareNamespaceApplyPayload(yamlContent, enforceName)
	if err != nil {
		return nil, err
	}

	// Force=true permite assumir ownership de campos gerenciados por outros field managers
	forceFlag := true
	options := metav1.PatchOptions{
		FieldManager: fieldManager,
		Force:        &forceFlag,
	}
	if dryRun {
		options.DryRun = []string{metav1.DryRunAll}
	}

	result, err := c.clientset.CoreV1().Namespaces().Patch(ctx, name, types.ApplyPatchType, payload, options)
	if err != nil {
		return nil, fmt.Errorf("failed to apply namespace %s in cluster %s: %w", name, c.cluster, err)
	}

	return result, nil
}

func prepareNamespaceApplyPayload(yamlContent, enforceName string) ([]byte, string, error) {
	var ns map[string]interface{}
	if err := yaml.Unmarshal([]byte(yamlContent), &ns); err != nil {
		return nil, "", fmt.Errorf("invalid namespace yaml: %w", err)
	}

	if len(ns) == 0 {
		return nil, "", fmt.Errorf("namespace yaml cannot be empty")
	}

	apiVersion, _ := ns["apiVersion"].(string)
	if strings.TrimSpace(apiVersion) == "" {
		ns["apiVersion"] = "v1"
	}
	kind, _ := ns["kind"].(string)
	if strings.TrimSpace(kind) == "" {
		ns["kind"] = "Namespace"
	} else if !strings.EqualFold(kind, "Namespace") {
		return nil, "", fmt.Errorf("expected kind Namespace, got %s", kind)
	}

	metadata, _ := ns["metadata"].(map[string]interface{})
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
			return nil, "", fmt.Errorf("namespace name mismatch: expected %s, got %s", enforceName, name)
		}
	}
	if name == "" {
		return nil, "", fmt.Errorf("namespace metadata.name is required")
	}
	metadata["name"] = name

	// Remover namespace do metadata (namespace não tem namespace)
	delete(metadata, "namespace")
	ns["metadata"] = metadata

	jsonPayload, err := json.Marshal(ns)
	if err != nil {
		return nil, "", fmt.Errorf("failed to marshal namespace payload: %w", err)
	}

	return jsonPayload, name, nil
}

// CreateNamespace cria um novo namespace no cluster
// Se isSpotInstance for true, adiciona annotations para tolerar spot instances do Azure
func (c *Client) CreateNamespace(ctx context.Context, name string, isSpotInstance bool) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("namespace name cannot be empty")
	}

	// Criar objeto namespace
	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}

	// Adicionar annotations para spot instances se solicitado
	if isSpotInstance {
		namespace.ObjectMeta.Annotations = map[string]string{
			"scheduler.alpha.kubernetes.io/defaultTolerations": `[{"Key": "kubernetes.azure.com/scalesetpriority","Operator": "Equal", "Value": "spot", "Effect": "NoSchedule"}]`,
		}
	}

	// Criar namespace
	_, err := c.clientset.CoreV1().Namespaces().Create(ctx, namespace, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create namespace %s in cluster %s: %w", name, c.cluster, err)
	}

	return nil
}

// DeleteNamespace deleta um namespace do cluster
func (c *Client) DeleteNamespace(ctx context.Context, name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("namespace name cannot be empty")
	}

	// Verificar se namespace existe antes de deletar
	_, err := c.clientset.CoreV1().Namespaces().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get namespace %s: %w", name, err)
	}

	// Deletar namespace
	err = c.clientset.CoreV1().Namespaces().Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("failed to delete namespace %s in cluster %s: %w", name, c.cluster, err)
	}

	return nil
}

// ============================================================================
// Deployments Methods
// ============================================================================

// ListDeployments lista deployments com filtros
func (c *Client) ListDeployments(ctx context.Context, namespaces []string, search string, showSystemNamespaces bool) ([]models.DeploymentSummary, error) {
	var result []models.DeploymentSummary
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

	appendSummaries := func(items []appsv1.Deployment) {
		for _, dep := range items {
			if !showSystemNamespaces && isSystemNamespace(dep.Namespace) {
				continue
			}
			if search != "" && !matchesDeploymentSearch(&dep, search) {
				continue
			}
			result = append(result, buildDeploymentSummary(c.cluster, &dep))
		}
	}

	if listAllNamespaces {
		deps, err := c.clientset.AppsV1().Deployments("").List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to list deployments in cluster %s: %w", c.cluster, err)
		}
		appendSummaries(deps.Items)
		return result, nil
	}

	for ns := range uniqueNamespaces {
		deps, err := c.clientset.AppsV1().Deployments(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to list deployments in %s/%s: %w", c.cluster, ns, err)
		}
		appendSummaries(deps.Items)
	}

	return result, nil
}

// GetDeployment retorna o manifesto YAML completo do Deployment
func (c *Client) GetDeployment(ctx context.Context, namespace, name string) (*models.DeploymentManifest, error) {
	dep, err := c.clientset.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get deployment %s/%s in cluster %s: %w", namespace, name, c.cluster, err)
	}

	yamlBytes, err := yaml.Marshal(dep)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal deployment %s/%s: %w", namespace, name, err)
	}

	manifest := &models.DeploymentManifest{
		Cluster:   c.cluster,
		Namespace: namespace,
		Name:      name,
		YAML:      string(yamlBytes),
		Metadata: models.DeploymentMetadata{
			UID:             string(dep.UID),
			ResourceVersion: dep.ResourceVersion,
			Labels:          copyStringMap(dep.Labels),
			Annotations:     copyStringMap(dep.Annotations),
		},
	}

	return manifest, nil
}

func matchesDeploymentSearch(dep *appsv1.Deployment, search string) bool {
	name := strings.ToLower(dep.Name)
	if strings.Contains(name, search) {
		return true
	}
	for k, v := range dep.Labels {
		candidate := strings.ToLower(fmt.Sprintf("%s=%s", k, v))
		if strings.Contains(candidate, search) {
			return true
		}
	}
	return false
}

func buildDeploymentSummary(cluster string, dep *appsv1.Deployment) models.DeploymentSummary {
	replicas := int32(0)
	if dep.Spec.Replicas != nil {
		replicas = *dep.Spec.Replicas
	}

	updatedAt := dep.CreationTimestamp.Time
	return models.DeploymentSummary{
		Cluster:             cluster,
		Namespace:           dep.Namespace,
		Name:                dep.Name,
		Labels:              copyStringMap(dep.Labels),
		Replicas:            replicas,
		ReadyReplicas:       dep.Status.ReadyReplicas,
		AvailableReplicas:   dep.Status.AvailableReplicas,
		UpdatedReplicas:     dep.Status.UpdatedReplicas,
		UnavailableReplicas: dep.Status.UnavailableReplicas,
		CurrentReplicas:     dep.Status.Replicas,
		ResourceVersion:     dep.ResourceVersion,
		UpdatedAt:           updatedAt,
	}
}

// ValidateDeployment executa um server-side apply com dry-run
func (c *Client) ValidateDeployment(ctx context.Context, yamlContent, fieldManager, enforceNamespace string) (*appsv1.Deployment, error) {
	return c.applyDeployment(ctx, yamlContent, fieldManager, enforceNamespace, "", true)
}

// ApplyDeployment aplica (ou dry-run opcionalmente) o Deployment no cluster
func (c *Client) ApplyDeployment(ctx context.Context, yamlContent, fieldManager, enforceNamespace, enforceName string, dryRun bool) (*appsv1.Deployment, error) {
	return c.applyDeployment(ctx, yamlContent, fieldManager, enforceNamespace, enforceName, dryRun)
}

func (c *Client) applyDeployment(ctx context.Context, yamlContent, fieldManager, enforceNamespace, enforceName string, dryRun bool) (*appsv1.Deployment, error) {
	if strings.TrimSpace(yamlContent) == "" {
		return nil, fmt.Errorf("deployment yaml content cannot be empty")
	}
	if fieldManager == "" {
		fieldManager = "web-deployment-editor"
	}

	payload, namespace, name, err := prepareDeploymentApplyPayload(yamlContent, enforceNamespace, enforceName)
	if err != nil {
		return nil, err
	}

	forceFlag := true
	options := metav1.PatchOptions{
		FieldManager: fieldManager,
		Force:        &forceFlag, // Force ownership transfer (resolve conflicts with helm/other managers)
	}
	if dryRun {
		options.DryRun = []string{metav1.DryRunAll}
		fmt.Printf("[DEBUG] Applying deployment %s/%s in DRY-RUN mode\n", namespace, name)
	} else {
		fmt.Printf("[DEBUG] Applying deployment %s/%s with Force=true\n", namespace, name)
	}

	result, err := c.clientset.AppsV1().Deployments(namespace).Patch(ctx, name, types.ApplyPatchType, payload, options)
	if err != nil {
		return nil, fmt.Errorf("failed to apply deployment %s/%s in cluster %s: %w", namespace, name, c.cluster, err)
	}

	fmt.Printf("[DEBUG] Successfully applied deployment %s/%s, resourceVersion: %s\n", namespace, name, result.ResourceVersion)
	return result, nil
}

func prepareDeploymentApplyPayload(yamlContent, enforceNamespace, enforceName string) ([]byte, string, string, error) {
	var dep map[string]interface{}
	if err := yaml.Unmarshal([]byte(yamlContent), &dep); err != nil {
		return nil, "", "", fmt.Errorf("invalid deployment yaml: %w", err)
	}

	if len(dep) == 0 {
		return nil, "", "", fmt.Errorf("deployment yaml cannot be empty")
	}

	apiVersion, _ := dep["apiVersion"].(string)
	if strings.TrimSpace(apiVersion) == "" {
		dep["apiVersion"] = "apps/v1"
	}
	kind, _ := dep["kind"].(string)
	if strings.TrimSpace(kind) == "" {
		dep["kind"] = "Deployment"
	} else if !strings.EqualFold(kind, "Deployment") {
		return nil, "", "", fmt.Errorf("expected kind Deployment, got %s", kind)
	}

	metadata, _ := dep["metadata"].(map[string]interface{})
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
			return nil, "", "", fmt.Errorf("deployment name mismatch: expected %s, got %s", enforceName, name)
		}
	}
	if name == "" {
		return nil, "", "", fmt.Errorf("deployment metadata.name is required")
	}
	metadata["name"] = name

	namespace := strings.TrimSpace(enforceNamespace)
	if nsRaw, ok := metadata["namespace"].(string); ok {
		ns := strings.TrimSpace(nsRaw)
		if namespace == "" {
			namespace = ns
		} else if ns != "" && ns != namespace {
			return nil, "", "", fmt.Errorf("deployment namespace mismatch: expected %s, got %s", namespace, ns)
		}
	}
	if namespace == "" {
		return nil, "", "", fmt.Errorf("deployment metadata.namespace is required")
	}
	metadata["namespace"] = namespace
	dep["metadata"] = metadata

	jsonPayload, err := json.Marshal(dep)
	if err != nil {
		return nil, "", "", fmt.Errorf("failed to marshal deployment payload: %w", err)
	}

	return jsonPayload, namespace, name, nil
}

// ============================================================================
// DaemonSet Methods
// ============================================================================

// ListDaemonSets lista DaemonSets com filtros
func (c *Client) ListDaemonSets(ctx context.Context, namespaces []string, search string, showSystemNamespaces bool) ([]models.DaemonSetSummary, error) {
	var result []models.DaemonSetSummary
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

	appendSummaries := func(items []appsv1.DaemonSet) {
		for _, ds := range items {
			if !showSystemNamespaces && isSystemNamespace(ds.Namespace) {
				continue
			}
			if search != "" && !matchesDaemonSetSearch(&ds, search) {
				continue
			}
			result = append(result, buildDaemonSetSummary(c.cluster, &ds))
		}
	}

	if listAllNamespaces {
		dss, err := c.clientset.AppsV1().DaemonSets("").List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to list daemonsets in cluster %s: %w", c.cluster, err)
		}
		appendSummaries(dss.Items)
		return result, nil
	}

	for ns := range uniqueNamespaces {
		dss, err := c.clientset.AppsV1().DaemonSets(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to list daemonsets in %s/%s: %w", c.cluster, ns, err)
		}
		appendSummaries(dss.Items)
	}

	return result, nil
}

// GetDaemonSet retorna o manifesto YAML completo do DaemonSet
func (c *Client) GetDaemonSet(ctx context.Context, namespace, name string) (*models.DaemonSetManifest, error) {
	ds, err := c.clientset.AppsV1().DaemonSets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get daemonset %s/%s in cluster %s: %w", namespace, name, c.cluster, err)
	}

	yamlBytes, err := yaml.Marshal(ds)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal daemonset %s/%s: %w", namespace, name, err)
	}

	manifest := &models.DaemonSetManifest{
		Cluster:   c.cluster,
		Namespace: namespace,
		Name:      name,
		YAML:      string(yamlBytes),
		Metadata: models.DaemonSetMetadata{
			UID:             string(ds.UID),
			ResourceVersion: ds.ResourceVersion,
			Labels:          copyStringMap(ds.Labels),
			Annotations:     copyStringMap(ds.Annotations),
		},
	}

	return manifest, nil
}

func matchesDaemonSetSearch(ds *appsv1.DaemonSet, search string) bool {
	name := strings.ToLower(ds.Name)
	if strings.Contains(name, search) {
		return true
	}
	for k, v := range ds.Labels {
		candidate := strings.ToLower(fmt.Sprintf("%s=%s", k, v))
		if strings.Contains(candidate, search) {
			return true
		}
	}
	return false
}

func buildDaemonSetSummary(cluster string, ds *appsv1.DaemonSet) models.DaemonSetSummary {
	updatedAt := ds.CreationTimestamp.Time
	return models.DaemonSetSummary{
		Cluster:                cluster,
		Namespace:              ds.Namespace,
		Name:                   ds.Name,
		Labels:                 copyStringMap(ds.Labels),
		DesiredNumberScheduled: ds.Status.DesiredNumberScheduled,
		CurrentNumberScheduled: ds.Status.CurrentNumberScheduled,
		NumberReady:            ds.Status.NumberReady,
		NumberAvailable:        ds.Status.NumberAvailable,
		NumberMisscheduled:     ds.Status.NumberMisscheduled,
		UpdatedNumberScheduled: ds.Status.UpdatedNumberScheduled,
		ResourceVersion:        ds.ResourceVersion,
		UpdatedAt:              updatedAt,
	}
}

// ValidateDaemonSet executa um server-side apply com dry-run
func (c *Client) ValidateDaemonSet(ctx context.Context, yamlContent, fieldManager, enforceNamespace string) (*appsv1.DaemonSet, error) {
	return c.applyDaemonSet(ctx, yamlContent, fieldManager, enforceNamespace, "", true)
}

// ApplyDaemonSet aplica (ou dry-run opcionalmente) o DaemonSet no cluster
func (c *Client) ApplyDaemonSet(ctx context.Context, yamlContent, fieldManager, enforceNamespace, enforceName string, dryRun bool) (*appsv1.DaemonSet, error) {
	return c.applyDaemonSet(ctx, yamlContent, fieldManager, enforceNamespace, enforceName, dryRun)
}

func (c *Client) applyDaemonSet(ctx context.Context, yamlContent, fieldManager, enforceNamespace, enforceName string, dryRun bool) (*appsv1.DaemonSet, error) {
	if strings.TrimSpace(yamlContent) == "" {
		return nil, fmt.Errorf("daemonset yaml content cannot be empty")
	}
	if fieldManager == "" {
		fieldManager = "web-daemonset-editor"
	}

	payload, namespace, name, err := prepareDaemonSetApplyPayload(yamlContent, enforceNamespace, enforceName)
	if err != nil {
		return nil, err
	}

	forceFlag := true
	options := metav1.PatchOptions{
		FieldManager: fieldManager,
		Force:        &forceFlag,
	}
	if dryRun {
		options.DryRun = []string{metav1.DryRunAll}
		fmt.Printf("[DEBUG] Applying daemonset %s/%s in DRY-RUN mode\n", namespace, name)
	} else {
		fmt.Printf("[DEBUG] Applying daemonset %s/%s with Force=true\n", namespace, name)
	}

	result, err := c.clientset.AppsV1().DaemonSets(namespace).Patch(ctx, name, types.ApplyPatchType, payload, options)
	if err != nil {
		return nil, fmt.Errorf("failed to apply daemonset %s/%s in cluster %s: %w", namespace, name, c.cluster, err)
	}

	fmt.Printf("[DEBUG] Successfully applied daemonset %s/%s, resourceVersion: %s\n", namespace, name, result.ResourceVersion)
	return result, nil
}

func prepareDaemonSetApplyPayload(yamlContent, enforceNamespace, enforceName string) ([]byte, string, string, error) {
	var ds map[string]interface{}
	if err := yaml.Unmarshal([]byte(yamlContent), &ds); err != nil {
		return nil, "", "", fmt.Errorf("invalid daemonset yaml: %w", err)
	}

	if len(ds) == 0 {
		return nil, "", "", fmt.Errorf("daemonset yaml cannot be empty")
	}

	apiVersion, _ := ds["apiVersion"].(string)
	if strings.TrimSpace(apiVersion) == "" {
		ds["apiVersion"] = "apps/v1"
	}
	kind, _ := ds["kind"].(string)
	if strings.TrimSpace(kind) == "" {
		ds["kind"] = "DaemonSet"
	} else if !strings.EqualFold(kind, "DaemonSet") {
		return nil, "", "", fmt.Errorf("expected kind DaemonSet, got %s", kind)
	}

	metadata, _ := ds["metadata"].(map[string]interface{})
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
			return nil, "", "", fmt.Errorf("daemonset name mismatch: expected %s, got %s", enforceName, name)
		}
	}
	if name == "" {
		return nil, "", "", fmt.Errorf("daemonset metadata.name is required")
	}
	metadata["name"] = name

	namespace := strings.TrimSpace(enforceNamespace)
	if nsRaw, ok := metadata["namespace"].(string); ok {
		ns := strings.TrimSpace(nsRaw)
		if namespace == "" {
			namespace = ns
		} else if ns != "" && ns != namespace {
			return nil, "", "", fmt.Errorf("daemonset namespace mismatch: expected %s, got %s", namespace, ns)
		}
	}
	if namespace == "" {
		return nil, "", "", fmt.Errorf("daemonset metadata.namespace is required")
	}
	metadata["namespace"] = namespace
	ds["metadata"] = metadata

	jsonPayload, err := json.Marshal(ds)
	if err != nil {
		return nil, "", "", fmt.Errorf("failed to marshal daemonset payload: %w", err)
	}

	return jsonPayload, namespace, name, nil
}

// DeleteDaemonSet deleta um DaemonSet específico
func (c *Client) DeleteDaemonSet(ctx context.Context, namespace, daemonSetName string) error {
	err := c.clientset.AppsV1().DaemonSets(namespace).Delete(ctx, daemonSetName, metav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("failed to delete daemonset %s/%s/%s: %w", c.cluster, namespace, daemonSetName, err)
	}
	return nil
}

// RolloutRestartDaemonSet reinicia um DaemonSet (alias para RolloutDaemonSet)
func (c *Client) RolloutRestartDaemonSet(ctx context.Context, namespace, daemonSetName string) error {
	return c.RolloutDaemonSet(ctx, namespace, daemonSetName)
}

// ============================================================================
// StatefulSet Methods
// ============================================================================

// ListStatefulSets lista StatefulSets com filtros
func (c *Client) ListStatefulSets(ctx context.Context, namespaces []string, search string, showSystemNamespaces bool) ([]models.StatefulSetSummary, error) {
	var result []models.StatefulSetSummary
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

	appendSummaries := func(items []appsv1.StatefulSet) {
		for _, sts := range items {
			if !showSystemNamespaces && isSystemNamespace(sts.Namespace) {
				continue
			}
			if search != "" && !matchesStatefulSetSearch(&sts, search) {
				continue
			}
			result = append(result, buildStatefulSetSummary(c.cluster, &sts))
		}
	}

	if listAllNamespaces {
		stss, err := c.clientset.AppsV1().StatefulSets("").List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to list statefulsets in cluster %s: %w", c.cluster, err)
		}
		appendSummaries(stss.Items)
		return result, nil
	}

	for ns := range uniqueNamespaces {
		stss, err := c.clientset.AppsV1().StatefulSets(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to list statefulsets in %s/%s: %w", c.cluster, ns, err)
		}
		appendSummaries(stss.Items)
	}

	return result, nil
}

// GetStatefulSet retorna o manifesto YAML completo do StatefulSet
func (c *Client) GetStatefulSet(ctx context.Context, namespace, name string) (*models.StatefulSetManifest, error) {
	sts, err := c.clientset.AppsV1().StatefulSets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get statefulset %s/%s in cluster %s: %w", namespace, name, c.cluster, err)
	}

	yamlBytes, err := yaml.Marshal(sts)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal statefulset %s/%s: %w", namespace, name, err)
	}

	manifest := &models.StatefulSetManifest{
		Cluster:   c.cluster,
		Namespace: namespace,
		Name:      name,
		YAML:      string(yamlBytes),
		Metadata: models.StatefulSetMetadata{
			UID:             string(sts.UID),
			ResourceVersion: sts.ResourceVersion,
			Labels:          copyStringMap(sts.Labels),
			Annotations:     copyStringMap(sts.Annotations),
		},
	}

	return manifest, nil
}

func matchesStatefulSetSearch(sts *appsv1.StatefulSet, search string) bool {
	name := strings.ToLower(sts.Name)
	if strings.Contains(name, search) {
		return true
	}
	for k, v := range sts.Labels {
		candidate := strings.ToLower(fmt.Sprintf("%s=%s", k, v))
		if strings.Contains(candidate, search) {
			return true
		}
	}
	return false
}

func buildStatefulSetSummary(cluster string, sts *appsv1.StatefulSet) models.StatefulSetSummary {
	replicas := int32(0)
	if sts.Spec.Replicas != nil {
		replicas = *sts.Spec.Replicas
	}

	updatedAt := sts.CreationTimestamp.Time
	return models.StatefulSetSummary{
		Cluster:           cluster,
		Namespace:         sts.Namespace,
		Name:              sts.Name,
		Labels:            copyStringMap(sts.Labels),
		Replicas:          replicas,
		ReadyReplicas:     sts.Status.ReadyReplicas,
		CurrentReplicas:   sts.Status.CurrentReplicas,
		UpdatedReplicas:   sts.Status.UpdatedReplicas,
		AvailableReplicas: sts.Status.AvailableReplicas,
		ResourceVersion:   sts.ResourceVersion,
		UpdatedAt:         updatedAt,
	}
}

// ValidateStatefulSet executa um server-side apply com dry-run
func (c *Client) ValidateStatefulSet(ctx context.Context, yamlContent, fieldManager, enforceNamespace string) (*appsv1.StatefulSet, error) {
	return c.applyStatefulSet(ctx, yamlContent, fieldManager, enforceNamespace, "", true)
}

// ApplyStatefulSet aplica (ou dry-run opcionalmente) o StatefulSet no cluster
func (c *Client) ApplyStatefulSet(ctx context.Context, yamlContent, fieldManager, enforceNamespace, enforceName string, dryRun bool) (*appsv1.StatefulSet, error) {
	return c.applyStatefulSet(ctx, yamlContent, fieldManager, enforceNamespace, enforceName, dryRun)
}

func (c *Client) applyStatefulSet(ctx context.Context, yamlContent, fieldManager, enforceNamespace, enforceName string, dryRun bool) (*appsv1.StatefulSet, error) {
	if strings.TrimSpace(yamlContent) == "" {
		return nil, fmt.Errorf("statefulset yaml content cannot be empty")
	}
	if fieldManager == "" {
		fieldManager = "web-statefulset-editor"
	}

	payload, namespace, name, err := prepareStatefulSetApplyPayload(yamlContent, enforceNamespace, enforceName)
	if err != nil {
		return nil, err
	}

	forceFlag := true
	options := metav1.PatchOptions{
		FieldManager: fieldManager,
		Force:        &forceFlag,
	}
	if dryRun {
		options.DryRun = []string{metav1.DryRunAll}
		fmt.Printf("[DEBUG] Applying statefulset %s/%s in DRY-RUN mode\n", namespace, name)
	} else {
		fmt.Printf("[DEBUG] Applying statefulset %s/%s with Force=true\n", namespace, name)
	}

	result, err := c.clientset.AppsV1().StatefulSets(namespace).Patch(ctx, name, types.ApplyPatchType, payload, options)
	if err != nil {
		return nil, fmt.Errorf("failed to apply statefulset %s/%s in cluster %s: %w", namespace, name, c.cluster, err)
	}

	fmt.Printf("[DEBUG] Successfully applied statefulset %s/%s, resourceVersion: %s\n", namespace, name, result.ResourceVersion)
	return result, nil
}

func prepareStatefulSetApplyPayload(yamlContent, enforceNamespace, enforceName string) ([]byte, string, string, error) {
	var sts map[string]interface{}
	if err := yaml.Unmarshal([]byte(yamlContent), &sts); err != nil {
		return nil, "", "", fmt.Errorf("invalid statefulset yaml: %w", err)
	}

	if len(sts) == 0 {
		return nil, "", "", fmt.Errorf("statefulset yaml cannot be empty")
	}

	apiVersion, _ := sts["apiVersion"].(string)
	if strings.TrimSpace(apiVersion) == "" {
		sts["apiVersion"] = "apps/v1"
	}
	kind, _ := sts["kind"].(string)
	if strings.TrimSpace(kind) == "" {
		sts["kind"] = "StatefulSet"
	} else if !strings.EqualFold(kind, "StatefulSet") {
		return nil, "", "", fmt.Errorf("expected kind StatefulSet, got %s", kind)
	}

	metadata, _ := sts["metadata"].(map[string]interface{})
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
			return nil, "", "", fmt.Errorf("statefulset name mismatch: expected %s, got %s", enforceName, name)
		}
	}
	if name == "" {
		return nil, "", "", fmt.Errorf("statefulset metadata.name is required")
	}
	metadata["name"] = name

	namespace := strings.TrimSpace(enforceNamespace)
	if nsRaw, ok := metadata["namespace"].(string); ok {
		ns := strings.TrimSpace(nsRaw)
		if namespace == "" {
			namespace = ns
		} else if ns != "" && ns != namespace {
			return nil, "", "", fmt.Errorf("statefulset namespace mismatch: expected %s, got %s", namespace, ns)
		}
	}
	if namespace == "" {
		return nil, "", "", fmt.Errorf("statefulset metadata.namespace is required")
	}
	metadata["namespace"] = namespace
	sts["metadata"] = metadata

	jsonPayload, err := json.Marshal(sts)
	if err != nil {
		return nil, "", "", fmt.Errorf("failed to marshal statefulset payload: %w", err)
	}

	return jsonPayload, namespace, name, nil
}

// DeleteStatefulSet deleta um StatefulSet específico
func (c *Client) DeleteStatefulSet(ctx context.Context, namespace, statefulSetName string) error {
	err := c.clientset.AppsV1().StatefulSets(namespace).Delete(ctx, statefulSetName, metav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("failed to delete statefulset %s/%s/%s: %w", c.cluster, namespace, statefulSetName, err)
	}
	return nil
}

// RolloutRestartStatefulSet reinicia um StatefulSet (alias para RolloutStatefulSet)
func (c *Client) RolloutRestartStatefulSet(ctx context.Context, namespace, statefulSetName string) error {
	return c.RolloutStatefulSet(ctx, namespace, statefulSetName)
}

// ============================================================================
// Secrets Methods
// ============================================================================

// ListSecrets lista secrets com filtros
func (c *Client) ListSecrets(ctx context.Context, namespaces []string, search string, showSystemNamespaces bool) ([]models.SecretSummary, error) {
	var result []models.SecretSummary
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

	appendSummaries := func(items []corev1.Secret) {
		for _, secret := range items {
			if !showSystemNamespaces && isSystemNamespace(secret.Namespace) {
				continue
			}
			if search != "" && !matchesSecretSearch(&secret, search) {
				continue
			}
			result = append(result, buildSecretSummary(c.cluster, &secret))
		}
	}

	if listAllNamespaces {
		secrets, err := c.clientset.CoreV1().Secrets("").List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to list secrets in cluster %s: %w", c.cluster, err)
		}
		appendSummaries(secrets.Items)
		return result, nil
	}

	for ns := range uniqueNamespaces {
		secrets, err := c.clientset.CoreV1().Secrets(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to list secrets in %s/%s: %w", c.cluster, ns, err)
		}
		appendSummaries(secrets.Items)
	}

	return result, nil
}

// GetSecret retorna o manifesto YAML completo do Secret
func (c *Client) GetSecret(ctx context.Context, namespace, name string) (*models.SecretManifest, error) {
	secret, err := c.clientset.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get secret %s/%s in cluster %s: %w", namespace, name, c.cluster, err)
	}

	yamlBytes, err := yaml.Marshal(secret)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal secret %s/%s: %w", namespace, name, err)
	}

	manifest := &models.SecretManifest{
		Cluster:   c.cluster,
		Namespace: namespace,
		Name:      name,
		Type:      string(secret.Type),
		YAML:      string(yamlBytes),
		Metadata: models.SecretMetadata{
			UID:             string(secret.UID),
			ResourceVersion: secret.ResourceVersion,
			Labels:          copyStringMap(secret.Labels),
			Annotations:     copyStringMap(secret.Annotations),
		},
	}

	return manifest, nil
}

func matchesSecretSearch(secret *corev1.Secret, search string) bool {
	name := strings.ToLower(secret.Name)
	if strings.Contains(name, search) {
		return true
	}
	for k, v := range secret.Labels {
		candidate := strings.ToLower(fmt.Sprintf("%s=%s", k, v))
		if strings.Contains(candidate, search) {
			return true
		}
	}
	return false
}

func buildSecretSummary(cluster string, secret *corev1.Secret) models.SecretSummary {
	dataKeys := make([]string, 0, len(secret.Data))
	for key := range secret.Data {
		dataKeys = append(dataKeys, key)
	}
	sort.Strings(dataKeys)

	updatedAt := secret.CreationTimestamp.Time
	return models.SecretSummary{
		Cluster:         cluster,
		Namespace:       secret.Namespace,
		Name:            secret.Name,
		Type:            string(secret.Type),
		Labels:          copyStringMap(secret.Labels),
		DataKeys:        dataKeys,
		ResourceVersion: secret.ResourceVersion,
		UpdatedAt:       updatedAt,
	}
}

// ValidateSecret executa um server-side apply com dry-run
func (c *Client) ValidateSecret(ctx context.Context, yamlContent, fieldManager, enforceNamespace string) (*corev1.Secret, error) {
	return c.applySecret(ctx, yamlContent, fieldManager, enforceNamespace, "", true)
}

// ApplySecret aplica (ou dry-run opcionalmente) o Secret no cluster
func (c *Client) ApplySecret(ctx context.Context, yamlContent, fieldManager, enforceNamespace, enforceName string, dryRun bool) (*corev1.Secret, error) {
	return c.applySecret(ctx, yamlContent, fieldManager, enforceNamespace, enforceName, dryRun)
}

func (c *Client) applySecret(ctx context.Context, yamlContent, fieldManager, enforceNamespace, enforceName string, dryRun bool) (*corev1.Secret, error) {
	if strings.TrimSpace(yamlContent) == "" {
		return nil, fmt.Errorf("secret yaml content cannot be empty")
	}
	if fieldManager == "" {
		fieldManager = "web-secret-editor"
	}

	payload, namespace, name, err := prepareSecretApplyPayload(yamlContent, enforceNamespace, enforceName)
	if err != nil {
		return nil, err
	}

	// Force=true to assume ownership of fields previously managed via kubectl/helm/etc.
	forceFlag := true
	options := metav1.PatchOptions{
		FieldManager: fieldManager,
		Force:        &forceFlag,
	}
	if dryRun {
		options.DryRun = []string{metav1.DryRunAll}
	}

	result, err := c.clientset.CoreV1().Secrets(namespace).Patch(ctx, name, types.ApplyPatchType, payload, options)
	if err != nil {
		return nil, fmt.Errorf("failed to apply secret %s/%s in cluster %s: %w", namespace, name, c.cluster, err)
	}

	return result, nil
}

func prepareSecretApplyPayload(yamlContent, enforceNamespace, enforceName string) ([]byte, string, string, error) {
	var secret map[string]interface{}
	if err := yaml.Unmarshal([]byte(yamlContent), &secret); err != nil {
		return nil, "", "", fmt.Errorf("invalid secret yaml: %w", err)
	}

	if len(secret) == 0 {
		return nil, "", "", fmt.Errorf("secret yaml cannot be empty")
	}

	apiVersion, _ := secret["apiVersion"].(string)
	if strings.TrimSpace(apiVersion) == "" {
		secret["apiVersion"] = "v1"
	}
	kind, _ := secret["kind"].(string)
	if strings.TrimSpace(kind) == "" {
		secret["kind"] = "Secret"
	} else if !strings.EqualFold(kind, "Secret") {
		return nil, "", "", fmt.Errorf("expected kind Secret, got %s", kind)
	}

	metadata, _ := secret["metadata"].(map[string]interface{})
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
			return nil, "", "", fmt.Errorf("secret name mismatch: expected %s, got %s", enforceName, name)
		}
	}
	if name == "" {
		return nil, "", "", fmt.Errorf("secret metadata.name is required")
	}
	metadata["name"] = name

	namespace, _ := metadata["namespace"].(string)
	namespace = strings.TrimSpace(namespace)
	if enforceNamespace != "" {
		enforceNamespace = strings.TrimSpace(enforceNamespace)
		if namespace == "" {
			namespace = enforceNamespace
		}
		if namespace != enforceNamespace {
			return nil, "", "", fmt.Errorf("secret namespace mismatch: expected %s, got %s", enforceNamespace, namespace)
		}
	}
	if namespace == "" {
		return nil, "", "", fmt.Errorf("secret metadata.namespace is required")
	}
	metadata["namespace"] = namespace
	secret["metadata"] = metadata

	jsonPayload, err := json.Marshal(secret)
	if err != nil {
		return nil, "", "", fmt.Errorf("failed to marshal secret payload: %w", err)
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
	startTime := time.Now()
	_, err = c.clientset.AppsV1().Deployments(hpa.Namespace).Update(ctx, deployment, metav1.UpdateOptions{})
	if err != nil {
		// Log failure no audit
		if c.historyTracker != nil {
			c.historyTracker.Log(history.HistoryEntry{
				Action:   history.ActionRolloutDeployment,
				Resource: fmt.Sprintf("%s/%s", hpa.Namespace, deploymentName),
				Cluster:  c.cluster,
				Status:   history.StatusFailed,
				ErrorMsg: err.Error(),
				Duration: time.Since(startTime).Milliseconds(),
			})
		}
		return fmt.Errorf("failed to trigger rollout for deployment %s/%s: %w", hpa.Namespace, deploymentName, err)
	}

	// Log success no audit
	if c.historyTracker != nil {
		c.historyTracker.Log(history.HistoryEntry{
			Action:   history.ActionRolloutDeployment,
			Resource: fmt.Sprintf("%s/%s", hpa.Namespace, deploymentName),
			Cluster:  c.cluster,
			Status:   history.StatusSuccess,
			Duration: time.Since(startTime).Milliseconds(),
			Before: map[string]interface{}{
				"hpa":             hpa.Name,
				"perform_rollout": hpa.PerformRollout,
			},
			After: map[string]interface{}{
				"deployment":   deploymentName,
				"restarted_at": deployment.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"],
			},
		})
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
	startTime := time.Now()
	_, err = c.clientset.AppsV1().DaemonSets(hpa.Namespace).Update(ctx, daemonSet, metav1.UpdateOptions{})
	if err != nil {
		// Log failure no audit
		if c.historyTracker != nil {
			c.historyTracker.Log(history.HistoryEntry{
				Action:   history.ActionRolloutDaemonSet,
				Resource: fmt.Sprintf("%s/%s", hpa.Namespace, targetName),
				Cluster:  c.cluster,
				Status:   history.StatusFailed,
				ErrorMsg: err.Error(),
				Duration: time.Since(startTime).Milliseconds(),
			})
		}
		return fmt.Errorf("failed to trigger rollout for daemonset %s/%s: %w", hpa.Namespace, targetName, err)
	}

	// Log success no audit
	if c.historyTracker != nil {
		c.historyTracker.Log(history.HistoryEntry{
			Action:   history.ActionRolloutDaemonSet,
			Resource: fmt.Sprintf("%s/%s", hpa.Namespace, targetName),
			Cluster:  c.cluster,
			Status:   history.StatusSuccess,
			Duration: time.Since(startTime).Milliseconds(),
			Before: map[string]interface{}{
				"hpa":                       hpa.Name,
				"perform_daemonset_rollout": hpa.PerformDaemonSetRollout,
			},
			After: map[string]interface{}{
				"daemonset":    targetName,
				"restarted_at": daemonSet.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"],
			},
		})
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
	startTime := time.Now()
	_, err = c.clientset.AppsV1().StatefulSets(hpa.Namespace).Update(ctx, statefulSet, metav1.UpdateOptions{})
	if err != nil {
		// Log failure no audit
		if c.historyTracker != nil {
			c.historyTracker.Log(history.HistoryEntry{
				Action:   history.ActionRolloutStatefulSet,
				Resource: fmt.Sprintf("%s/%s", hpa.Namespace, statefulSetName),
				Cluster:  c.cluster,
				Status:   history.StatusFailed,
				ErrorMsg: err.Error(),
				Duration: time.Since(startTime).Milliseconds(),
			})
		}
		return fmt.Errorf("failed to trigger rollout for statefulset %s/%s: %w", hpa.Namespace, statefulSetName, err)
	}

	// Log success no audit
	if c.historyTracker != nil {
		c.historyTracker.Log(history.HistoryEntry{
			Action:   history.ActionRolloutStatefulSet,
			Resource: fmt.Sprintf("%s/%s", hpa.Namespace, statefulSetName),
			Cluster:  c.cluster,
			Status:   history.StatusSuccess,
			Duration: time.Since(startTime).Milliseconds(),
			Before: map[string]interface{}{
				"hpa":                         hpa.Name,
				"perform_statefulset_rollout": hpa.PerformStatefulSetRollout,
			},
			After: map[string]interface{}{
				"statefulset":  statefulSetName,
				"restarted_at": statefulSet.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"],
			},
		})
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

// RolloutRestartDeployment reinicia um deployment (alias para RolloutDeployment)
func (c *Client) RolloutRestartDeployment(ctx context.Context, namespace, deploymentName string) error {
	return c.RolloutDeployment(ctx, namespace, deploymentName)
}

// DeleteDeployment deleta um deployment específico
func (c *Client) DeleteDeployment(ctx context.Context, namespace, deploymentName string) error {
	err := c.clientset.AppsV1().Deployments(namespace).Delete(ctx, deploymentName, metav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("failed to delete deployment %s/%s/%s: %w", c.cluster, namespace, deploymentName, err)
	}

	return nil
}

// ScaleDeployment escala um deployment para o número especificado de réplicas
func (c *Client) ScaleDeployment(ctx context.Context, namespace, deploymentName string, replicas int32) error {
	scale, err := c.clientset.AppsV1().Deployments(namespace).GetScale(ctx, deploymentName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get scale for deployment %s/%s/%s: %w", c.cluster, namespace, deploymentName, err)
	}

	scale.Spec.Replicas = replicas
	_, err = c.clientset.AppsV1().Deployments(namespace).UpdateScale(ctx, deploymentName, scale, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to scale deployment %s/%s/%s to %d replicas: %w", c.cluster, namespace, deploymentName, replicas, err)
	}

	return nil
}

// ScaleStatefulSet escala um statefulset para o número especificado de réplicas
func (c *Client) ScaleStatefulSet(ctx context.Context, namespace, statefulSetName string, replicas int32) error {
	scale, err := c.clientset.AppsV1().StatefulSets(namespace).GetScale(ctx, statefulSetName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get scale for statefulset %s/%s/%s: %w", c.cluster, namespace, statefulSetName, err)
	}

	scale.Spec.Replicas = replicas
	_, err = c.clientset.AppsV1().StatefulSets(namespace).UpdateScale(ctx, statefulSetName, scale, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to scale statefulset %s/%s/%s to %d replicas: %w", c.cluster, namespace, statefulSetName, replicas, err)
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

		// Extrair versão da imagem (tag ou digest)
		if container.Image != "" {
			// Formato: registry/image:tag ou registry/image@sha256:digest
			image := container.Image
			if idx := strings.LastIndex(image, ":"); idx != -1 {
				hpa.ImageVersion = image[idx+1:]
			} else if idx := strings.LastIndex(image, "@"); idx != -1 {
				hpa.ImageVersion = image[idx+1:]
			} else {
				hpa.ImageVersion = "latest"
			}
		}

		// CPU Request (configurado no deployment) - converter para millicores
		if cpuReq, exists := container.Resources.Requests[corev1.ResourceCPU]; exists {
			milliValue := cpuReq.MilliValue()
			hpa.TargetCPURequest = fmt.Sprintf("%dm", milliValue)
		}

		// CPU Limit (configurado no deployment) - converter para millicores
		if cpuLimit, exists := container.Resources.Limits[corev1.ResourceCPU]; exists {
			milliValue := cpuLimit.MilliValue()
			hpa.TargetCPULimit = fmt.Sprintf("%dm", milliValue)
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

// ===========================
// Node Pool Cordon/Drain Operations
// ===========================

// GetNodesInNodePool retorna lista de nodes de um node pool específico
func (c *Client) GetNodesInNodePool(ctx context.Context, nodePoolName string) ([]string, error) {
	// Listar todos os nodes do cluster
	nodes, err := c.clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list nodes in cluster %s: %w", c.cluster, err)
	}

	// Filtrar nodes pelo label agentpool=<nodePoolName>
	var nodeNames []string
	for _, node := range nodes.Items {
		if agentpool, ok := node.Labels["agentpool"]; ok && agentpool == nodePoolName {
			nodeNames = append(nodeNames, node.Name)
		}
	}

	if len(nodeNames) == 0 {
		return nil, fmt.Errorf("no nodes found for node pool '%s' in cluster %s", nodePoolName, c.cluster)
	}

	return nodeNames, nil
}

// CordonNode marca um node como unschedulable (kubectl cordon)
func (c *Client) CordonNode(ctx context.Context, nodeName string) error {
	// Obter node atual
	node, err := c.clientset.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get node %s in cluster %s: %w", nodeName, c.cluster, err)
	}

	// Se já está cordoned, não fazer nada
	if node.Spec.Unschedulable {
		return nil // Já está cordoned
	}

	// Marcar como unschedulable
	node.Spec.Unschedulable = true

	// Atualizar node
	_, err = c.clientset.CoreV1().Nodes().Update(ctx, node, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to cordon node %s in cluster %s: %w", nodeName, c.cluster, err)
	}

	return nil
}

// UncordonNode marca um node como schedulable (kubectl uncordon)
func (c *Client) UncordonNode(ctx context.Context, nodeName string) error {
	// Obter node atual
	node, err := c.clientset.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get node %s in cluster %s: %w", nodeName, c.cluster, err)
	}

	// Se já está schedulable, não fazer nada
	if !node.Spec.Unschedulable {
		return nil // Já está uncordoned
	}

	// Marcar como schedulable
	node.Spec.Unschedulable = false

	// Atualizar node
	_, err = c.clientset.CoreV1().Nodes().Update(ctx, node, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to uncordon node %s in cluster %s: %w", nodeName, c.cluster, err)
	}

	return nil
}

// DrainNode remove todos os pods de um node (kubectl drain)
func (c *Client) DrainNode(ctx context.Context, nodeName string, opts *models.DrainOptions) error {
	if opts == nil {
		opts = models.DefaultDrainOptions()
	}

	// Validar opções antes de executar
	if err := ValidateDrainOptions(opts); err != nil {
		return fmt.Errorf("invalid drain options: %w", err)
	}

	// Listar todos os pods no node
	pods, err := c.clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{
		FieldSelector: fmt.Sprintf("spec.nodeName=%s", nodeName),
	})
	if err != nil {
		return fmt.Errorf("failed to list pods on node %s: %w", nodeName, err)
	}

	// Filtrar pods por selector (se fornecido)
	podsToEvict := []corev1.Pod{}
	for _, pod := range pods.Items {
		// Pular DaemonSets se ignoreDaemonsets=true
		if opts.IgnoreDaemonsets && isDaemonSetPod(pod) {
			continue
		}

		// Filtrar por pod selector (se fornecido)
		if opts.PodSelector != "" {
			// TODO: Implementar label selector matching
			// Por enquanto, incluir todos os pods
		}

		podsToEvict = append(podsToEvict, pod)
	}

	// Dry-run: apenas listar pods que seriam evicted
	if opts.DryRun {
		return nil // Não executar, apenas validar
	}

	// Evict pods em chunks
	chunkSize := opts.ChunkSize
	if chunkSize < 1 {
		chunkSize = 1
	}

	for i := 0; i < len(podsToEvict); i += chunkSize {
		end := i + chunkSize
		if end > len(podsToEvict) {
			end = len(podsToEvict)
		}

		// Evict chunk de pods
		for _, pod := range podsToEvict[i:end] {
			if err := c.evictPod(ctx, &pod, opts); err != nil {
				return fmt.Errorf("failed to evict pod %s/%s: %w", pod.Namespace, pod.Name, err)
			}
		}

		// Aguardar pods serem deletados (com timeout)
		if err := c.waitForPodsDeleted(ctx, podsToEvict[i:end], opts); err != nil {
			return err
		}
	}

	return nil
}

// evictPod evict um único pod
func (c *Client) evictPod(ctx context.Context, pod *corev1.Pod, opts *models.DrainOptions) error {
	// Se --force=true e pod não tem controller, usar DELETE
	if opts.Force && !hasController(pod) {
		gracePeriod := int64(opts.GracePeriod)
		deleteOptions := metav1.DeleteOptions{
			GracePeriodSeconds: &gracePeriod,
		}
		return c.clientset.CoreV1().Pods(pod.Namespace).Delete(ctx, pod.Name, deleteOptions)
	}

	// Usar Eviction API (respeita PDBs)
	if !opts.DisableEviction {
		gracePeriod := int64(opts.GracePeriod)
		eviction := &policyv1.Eviction{
			ObjectMeta: metav1.ObjectMeta{
				Name:      pod.Name,
				Namespace: pod.Namespace,
			},
			DeleteOptions: &metav1.DeleteOptions{
				GracePeriodSeconds: &gracePeriod,
			},
		}
		return c.clientset.PolicyV1().Evictions(pod.Namespace).Evict(ctx, eviction)
	}

	// Fallback: DELETE direto (não respeita PDBs)
	gracePeriod := int64(opts.GracePeriod)
	deleteOptions := metav1.DeleteOptions{
		GracePeriodSeconds: &gracePeriod,
	}
	return c.clientset.CoreV1().Pods(pod.Namespace).Delete(ctx, pod.Name, deleteOptions)
}

// CountPodsOnNode conta quantos pods estão rodando em um node específico
func (c *Client) CountPodsOnNode(ctx context.Context, nodeName string) (int, error) {
	// Listar todos os pods no node
	fieldSelector := fmt.Sprintf("spec.nodeName=%s", nodeName)
	pods, err := c.clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{
		FieldSelector: fieldSelector,
	})
	if err != nil {
		return 0, fmt.Errorf("failed to list pods on node %s: %w", nodeName, err)
	}

	// Filtrar pods que não devem ser contados (completed, failed, etc)
	count := 0
	for _, pod := range pods.Items {
		// Contar apenas pods em Running ou Pending
		if pod.Status.Phase == corev1.PodRunning || pod.Status.Phase == corev1.PodPending {
			count++
		}
	}

	return count, nil
}

// waitForPodsDeleted aguarda pods serem deletados
func (c *Client) waitForPodsDeleted(ctx context.Context, pods []corev1.Pod, opts *models.DrainOptions) error {
	timeout, err := parseDuration(opts.Timeout)
	if err != nil {
		return err
	}

	deadline := time.Now().Add(timeout)

	for _, pod := range pods {
		for {
			// Verificar se ainda existe
			_, err := c.clientset.CoreV1().Pods(pod.Namespace).Get(ctx, pod.Name, metav1.GetOptions{})
			if err != nil {
				// Pod não existe mais (deletado)
				break
			}

			// Verificar timeout
			if time.Now().After(deadline) {
				return fmt.Errorf("timeout waiting for pod %s/%s to be deleted", pod.Namespace, pod.Name)
			}

			// Aguardar antes de verificar novamente
			time.Sleep(2 * time.Second)
		}
	}

	return nil
}

// IsNodeDrained verifica se um node está completamente drained (sem pods)
func (c *Client) IsNodeDrained(ctx context.Context, nodeName string) (bool, error) {
	// Listar pods no node
	pods, err := c.clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{
		FieldSelector: fmt.Sprintf("spec.nodeName=%s", nodeName),
	})
	if err != nil {
		return false, fmt.Errorf("failed to list pods on node %s: %w", nodeName, err)
	}

	// Contar pods não-DaemonSet
	nonDaemonSetPods := 0
	for _, pod := range pods.Items {
		if !isDaemonSetPod(pod) {
			nonDaemonSetPods++
		}
	}

	return nonDaemonSetPods == 0, nil
}

// ===========================
// Validation Functions
// ===========================

// ValidateDrainOptions valida todas as opções de drain
func ValidateDrainOptions(opts *models.DrainOptions) error {
	if opts == nil {
		return fmt.Errorf("drain options cannot be nil")
	}

	// Validar timeout
	if err := ValidateTimeout(opts.Timeout); err != nil {
		return err
	}

	// Validar grace period
	if opts.GracePeriod < 0 {
		return fmt.Errorf("grace period must be >= 0, got %d", opts.GracePeriod)
	}

	// Validar skip wait timeout
	if opts.SkipWaitForDeleteTimeout < 0 {
		return fmt.Errorf("skip wait timeout must be >= 0, got %d", opts.SkipWaitForDeleteTimeout)
	}

	// Validar chunk size
	if opts.ChunkSize < 1 {
		return fmt.Errorf("chunk size must be >= 1, got %d", opts.ChunkSize)
	}

	// Validar pod selector (se fornecido)
	if opts.PodSelector != "" {
		if err := ValidatePodSelector(opts.PodSelector); err != nil {
			return err
		}
	}

	return nil
}

// ValidateTimeout valida formato de timeout (5m, 300s, 1h)
func ValidateTimeout(timeout string) error {
	if timeout == "" {
		return fmt.Errorf("timeout cannot be empty")
	}

	// Tentar parsear
	_, err := parseDuration(timeout)
	if err != nil {
		return fmt.Errorf("invalid timeout format '%s': expected formats like '5m', '300s', '1h'", timeout)
	}

	return nil
}

// ValidatePodSelector valida sintaxe de label selector
func ValidatePodSelector(selector string) error {
	if selector == "" {
		return nil // Vazio é válido (sem filtro)
	}

	// Validação básica: verificar formato key=value ou key!=value
	// Formato: app=nginx,tier!=frontend
	pairs := strings.Split(selector, ",")
	for _, pair := range pairs {
		if !strings.Contains(pair, "=") && !strings.Contains(pair, "!=") {
			return fmt.Errorf("invalid pod selector '%s': expected format 'key=value' or 'key!=value'", pair)
		}
	}

	return nil
}

// ===========================
// Helper Functions
// ===========================

// isDaemonSetPod verifica se um pod pertence a um DaemonSet
func isDaemonSetPod(pod corev1.Pod) bool {
	for _, owner := range pod.OwnerReferences {
		if owner.Kind == "DaemonSet" {
			return true
		}
	}
	return false
}

// hasController verifica se um pod tem um controller (Deployment, ReplicaSet, etc.)
func hasController(pod *corev1.Pod) bool {
	return len(pod.OwnerReferences) > 0
}

// parseDuration converte string de timeout para time.Duration
func parseDuration(timeout string) (time.Duration, error) {
	// Suportar formatos: 5m, 300s, 1h, 1h30m
	duration, err := time.ParseDuration(timeout)
	if err != nil {
		return 0, fmt.Errorf("invalid duration '%s': %w", timeout, err)
	}
	return duration, nil
}

// ===========================
// PodDisruptionBudget Validation
// ===========================

// PDBValidationResult contém resultado da validação de PDB
type PDBValidationResult struct {
	CanProceed      bool
	BlockingPDBs    []string
	TotalPDBs       int
	PodsProtected   int
	WarningMessages []string
}

// ValidatePDBsForNode valida se PDBs permitem drain de um node
func (c *Client) ValidatePDBsForNode(ctx context.Context, nodeName string) (*PDBValidationResult, error) {
	result := &PDBValidationResult{
		CanProceed:      true,
		BlockingPDBs:    []string{},
		WarningMessages: []string{},
	}

	// Listar todos os pods no node
	pods, err := c.clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{
		FieldSelector: fmt.Sprintf("spec.nodeName=%s", nodeName),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list pods on node %s: %w", nodeName, err)
	}

	// Listar todos os PDBs do cluster
	pdbs, err := c.clientset.PolicyV1().PodDisruptionBudgets("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list PDBs: %w", err)
	}

	result.TotalPDBs = len(pdbs.Items)

	// Para cada PDB, verificar se afeta pods no node
	for _, pdb := range pdbs.Items {
		affectedPods := c.getPodsAffectedByPDB(&pdb, pods.Items)

		if len(affectedPods) == 0 {
			continue // PDB não afeta este node
		}

		result.PodsProtected += len(affectedPods)

		// Verificar se PDB permite disrupção
		canDisrupt, reason := c.canDisruptPods(&pdb, affectedPods)

		if !canDisrupt {
			result.CanProceed = false
			result.BlockingPDBs = append(result.BlockingPDBs, fmt.Sprintf("%s/%s", pdb.Namespace, pdb.Name))
			result.WarningMessages = append(result.WarningMessages, reason)
		} else if reason != "" {
			// PDB permite mas com avisos
			result.WarningMessages = append(result.WarningMessages, reason)
		}
	}

	return result, nil
}

// getPodsAffectedByPDB retorna pods que são afetados por um PDB
func (c *Client) getPodsAffectedByPDB(pdb *policyv1.PodDisruptionBudget, allPods []corev1.Pod) []corev1.Pod {
	if pdb.Spec.Selector == nil {
		return []corev1.Pod{}
	}

	affectedPods := []corev1.Pod{}

	for _, pod := range allPods {
		// Verificar se pod está no mesmo namespace que o PDB
		if pod.Namespace != pdb.Namespace {
			continue
		}

		// Verificar se labels do pod fazem match com selector do PDB
		if c.podMatchesSelector(&pod, pdb.Spec.Selector) {
			affectedPods = append(affectedPods, pod)
		}
	}

	return affectedPods
}

// podMatchesSelector verifica se pod faz match com label selector
func (c *Client) podMatchesSelector(pod *corev1.Pod, selector *metav1.LabelSelector) bool {
	if selector == nil {
		return false
	}

	// Verificar matchLabels
	for key, value := range selector.MatchLabels {
		podValue, exists := pod.Labels[key]
		if !exists || podValue != value {
			return false
		}
	}

	// TODO: Verificar matchExpressions (in, notin, exists, doesnotexist)
	// Por enquanto, apenas matchLabels

	return true
}

// canDisruptPods verifica se PDB permite disrupção dos pods
func (c *Client) canDisruptPods(pdb *policyv1.PodDisruptionBudget, affectedPods []corev1.Pod) (bool, string) {
	// Contar pods running afetados pelo PDB
	runningPods := 0
	for _, pod := range affectedPods {
		if pod.Status.Phase == corev1.PodRunning {
			runningPods++
		}
	}

	if runningPods == 0 {
		return true, "" // Nenhum pod running, pode prosseguir
	}

	// Status do PDB
	disruptionsAllowed := pdb.Status.DisruptionsAllowed
	currentHealthy := pdb.Status.CurrentHealthy
	desiredHealthy := pdb.Status.DesiredHealthy

	// Se DisruptionsAllowed == 0, não pode evict nenhum pod
	if disruptionsAllowed == 0 {
		reason := fmt.Sprintf(
			"PDB %s/%s blocks eviction: DisruptionsAllowed=0 (Current: %d, Desired: %d)",
			pdb.Namespace, pdb.Name, currentHealthy, desiredHealthy,
		)
		return false, reason
	}

	// Se DisruptionsAllowed > 0 mas menor que número de pods no node
	if int(disruptionsAllowed) < runningPods {
		reason := fmt.Sprintf(
			"PDB %s/%s allows only %d disruptions but node has %d pods (Current: %d, Desired: %d)",
			pdb.Namespace, pdb.Name, disruptionsAllowed, runningPods, currentHealthy, desiredHealthy,
		)
		return false, reason
	}

	// PDB permite, mas adicionar warning informativo
	if disruptionsAllowed > 0 {
		warning := fmt.Sprintf(
			"PDB %s/%s allows %d disruptions (%d pods on node)",
			pdb.Namespace, pdb.Name, disruptionsAllowed, runningPods,
		)
		return true, warning
	}

	return true, ""
}

// NamespaceEvent representa um evento do Kubernetes relacionado ao namespace
type NamespaceEvent struct {
	Type          string    `json:"type"`          // Normal, Warning
	Reason        string    `json:"reason"`        // Razão do evento (ex: FailedScheduling, Pulled)
	Message       string    `json:"message"`       // Mensagem descritiva
	Count         int32     `json:"count"`         // Número de ocorrências
	FirstTime     time.Time `json:"firstTime"`     // Primeira ocorrência
	LastTime      time.Time `json:"lastTime"`      // Última ocorrência
	Source        string    `json:"source"`        // Componente que gerou (ex: kubelet, scheduler)
	Object        string    `json:"object"`        // Recurso afetado (ex: Pod/nginx-xxx)
	ObjectKind    string    `json:"objectKind"`    // Tipo do objeto (Pod, Deployment, etc)
	LastTimestamp string    `json:"lastTimestamp"` // Timestamp formatado para display
}

// GetNamespaceEvents retorna eventos recentes de um namespace
func (c *Client) GetNamespaceEvents(ctx context.Context, namespace string, limitStr string) ([]NamespaceEvent, error) {
	// Parse do limit (padrão: 50, máximo: 200)
	limit := int64(50)
	if limitStr != "" {
		if l := parseInt64(limitStr); l > 0 {
			limit = l
			if limit > 200 {
				limit = 200
			}
		}
	}

	// Buscar eventos do namespace
	eventList, err := c.clientset.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{
		Limit: limit,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list events in namespace %s: %w", namespace, err)
	}

	// Ordenar por LastTimestamp (mais recente primeiro)
	sort.Slice(eventList.Items, func(i, j int) bool {
		return eventList.Items[i].LastTimestamp.Time.After(eventList.Items[j].LastTimestamp.Time)
	})

	// Converter para NamespaceEvent
	var events []NamespaceEvent
	for _, event := range eventList.Items {
		// Formatar timestamp relativo (ex: "2m ago", "1h ago")
		lastTime := event.LastTimestamp.Time
		if lastTime.IsZero() {
			lastTime = event.EventTime.Time
		}

		timeAgo := formatTimeAgo(lastTime)

		// Determinar objeto afetado
		objectName := event.InvolvedObject.Name
		if event.InvolvedObject.Namespace != "" && event.InvolvedObject.Namespace != namespace {
			objectName = event.InvolvedObject.Namespace + "/" + objectName
		}

		events = append(events, NamespaceEvent{
			Type:          event.Type,
			Reason:        event.Reason,
			Message:       event.Message,
			Count:         event.Count,
			FirstTime:     event.FirstTimestamp.Time,
			LastTime:      lastTime,
			Source:        event.Source.Component,
			Object:        objectName,
			ObjectKind:    event.InvolvedObject.Kind,
			LastTimestamp: timeAgo,
		})
	}

	return events, nil
}

// formatTimeAgo formata um timestamp como "5m ago", "2h ago", etc
func formatTimeAgo(t time.Time) string {
	if t.IsZero() {
		return "unknown"
	}

	duration := time.Since(t)

	if duration < time.Minute {
		return fmt.Sprintf("%ds ago", int(duration.Seconds()))
	}
	if duration < time.Hour {
		return fmt.Sprintf("%dm ago", int(duration.Minutes()))
	}
	if duration < 24*time.Hour {
		return fmt.Sprintf("%dh ago", int(duration.Hours()))
	}
	days := int(duration.Hours() / 24)
	return fmt.Sprintf("%dd ago", days)
}

// parseInt64 faz parse de string para int64
func parseInt64(s string) int64 {
	var result int64
	fmt.Sscanf(s, "%d", &result)
	return result
}

// ExecuteKubectlDescribe executa kubectl describe para um recurso
func ExecuteKubectlDescribe(cluster, resourceType, name, namespace string) (string, error) {
	cmd := exec.Command("kubectl", "describe", resourceType, name,
		"--context", cluster,
		"--namespace", namespace)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("kubectl describe failed: %w - %s", err, string(output))
	}

	return string(output), nil
}

// ApplyPod aplica um Pod a partir de YAML
func (c *Client) ApplyPod(ctx context.Context, yamlContent, fieldManager, enforceNamespace, enforceName string, dryRun bool) (*corev1.Pod, error) {
	return c.applyPod(ctx, yamlContent, fieldManager, enforceNamespace, enforceName, dryRun)
}

func (c *Client) applyPod(ctx context.Context, yamlContent, fieldManager, enforceNamespace, enforceName string, dryRun bool) (*corev1.Pod, error) {
	if strings.TrimSpace(yamlContent) == "" {
		return nil, fmt.Errorf("pod yaml content cannot be empty")
	}
	if fieldManager == "" {
		fieldManager = "web-pod-editor"
	}

	payload, namespace, name, err := preparePodApplyPayload(yamlContent, enforceNamespace, enforceName)
	if err != nil {
		return nil, err
	}

	// Force=true permite assumir ownership de campos gerenciados por outros field managers
	forceFlag := true
	options := metav1.PatchOptions{
		FieldManager: fieldManager,
		Force:        &forceFlag,
	}
	if dryRun {
		options.DryRun = []string{metav1.DryRunAll}
	}

	result, err := c.clientset.CoreV1().Pods(namespace).Patch(ctx, name, types.ApplyPatchType, payload, options)
	if err != nil {
		return nil, fmt.Errorf("failed to apply pod %s/%s in cluster %s: %w", namespace, name, c.cluster, err)
	}

	return result, nil
}

func preparePodApplyPayload(yamlContent, enforceNamespace, enforceName string) ([]byte, string, string, error) {
	var pod map[string]interface{}
	if err := yaml.Unmarshal([]byte(yamlContent), &pod); err != nil {
		return nil, "", "", fmt.Errorf("invalid pod yaml: %w", err)
	}

	if len(pod) == 0 {
		return nil, "", "", fmt.Errorf("pod yaml cannot be empty")
	}

	apiVersion, _ := pod["apiVersion"].(string)
	if strings.TrimSpace(apiVersion) == "" {
		pod["apiVersion"] = "v1"
	}
	kind, _ := pod["kind"].(string)
	if strings.TrimSpace(kind) == "" {
		pod["kind"] = "Pod"
	} else if !strings.EqualFold(kind, "Pod") {
		return nil, "", "", fmt.Errorf("expected kind Pod, got %s", kind)
	}

	metadata, _ := pod["metadata"].(map[string]interface{})
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
			return nil, "", "", fmt.Errorf("pod name mismatch: expected %s, got %s", enforceName, name)
		}
	}
	if name == "" {
		return nil, "", "", fmt.Errorf("pod metadata.name is required")
	}
	metadata["name"] = name

	namespace := strings.TrimSpace(enforceNamespace)
	if nsRaw, ok := metadata["namespace"].(string); ok {
		ns := strings.TrimSpace(nsRaw)
		if namespace == "" {
			namespace = ns
		} else if ns != "" && ns != namespace {
			return nil, "", "", fmt.Errorf("pod namespace mismatch: expected %s, got %s", namespace, ns)
		}
	}
	if namespace == "" {
		return nil, "", "", fmt.Errorf("pod metadata.namespace is required")
	}
	metadata["namespace"] = namespace
	pod["metadata"] = metadata

	jsonPayload, err := json.Marshal(pod)
	if err != nil {
		return nil, "", "", fmt.Errorf("failed to marshal pod payload: %w", err)
	}

	return jsonPayload, namespace, name, nil
}

// GetPodMetricsFromServer retorna métricas atuais de um Pod específico usando metrics-server
func (c *Client) GetPodMetricsFromServer(ctx context.Context, namespace, name string) (*metricsv1beta1.PodMetrics, error) {
	// Usar raw REST client para acessar metrics.k8s.io API
	restClient := c.clientset.CoreV1().RESTClient()

	// Buscar via metrics.k8s.io/v1beta1
	data, err := restClient.Get().
		AbsPath("/apis/metrics.k8s.io/v1beta1/namespaces/" + namespace + "/pods/" + name).
		DoRaw(ctx)

	if err != nil {
		return nil, fmt.Errorf("failed to get pod metrics: %w", err)
	}

	var metrics metricsv1beta1.PodMetrics
	if err := json.Unmarshal(data, &metrics); err != nil {
		return nil, fmt.Errorf("failed to unmarshal metrics: %w", err)
	}

	return &metrics, nil
}

// CopyFromPod copia arquivo/diretório do pod para arquivo temporário local
// Retorna o caminho do arquivo temporário criado
func (c *Client) CopyFromPod(namespace, podName, container, remotePath string, isDirectory bool) (string, error) {
	ctx := context.Background()

	// Validar que o container existe no pod (se especificado)
	if container != "" {
		pod, err := c.clientset.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
		if err != nil {
			return "", fmt.Errorf("failed to get pod: %w", err)
		}

		// Verificar se container existe
		containerExists := false
		var availableContainers []string
		for _, c := range pod.Spec.Containers {
			availableContainers = append(availableContainers, c.Name)
			if c.Name == container {
				containerExists = true
				break
			}
		}

		if !containerExists {
			return "", fmt.Errorf("container '%s' not found in pod '%s'. Available containers: %s",
				container, podName, strings.Join(availableContainers, ", "))
		}
	}

	// Validar que o arquivo/diretório existe no pod
	validateArgs := []string{"exec", podName, "-n", namespace, "--context", c.cluster}
	if container != "" {
		validateArgs = append(validateArgs, "-c", container)
	}
	validateArgs = append(validateArgs, "--", "test", "-e", remotePath)

	validateCmd := exec.Command("kubectl", validateArgs...)
	if err := validateCmd.Run(); err != nil {
		if isDirectory {
			return "", fmt.Errorf("diretório '%s' não encontrado no pod '%s'", remotePath, podName)
		}
		return "", fmt.Errorf("arquivo '%s' não encontrado no pod '%s'", remotePath, podName)
	}

	// Criar arquivo temporário
	tmpFile, err := os.CreateTemp("", "pod-download-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()

	// Se for diretório, deletar arquivo temporário e adicionar .tar.gz
	// kubectl cp criará o arquivo tar.gz automaticamente
	if isDirectory {
		os.Remove(tmpPath) // Remover arquivo vazio criado
		tmpPath = tmpPath + ".tar.gz"
	}

	// Construir comando kubectl cp
	// Formato: kubectl cp namespace/pod:/path /local/path --context cluster -c container
	source := fmt.Sprintf("%s/%s:%s", namespace, podName, remotePath)

	args := []string{"cp", source, tmpPath}

	// CRÍTICO: Adicionar --context ANTES de outras flags
	args = append(args, "--context", c.cluster)

	if container != "" {
		args = append(args, "-c", container)
	}

	// Log do comando para debug
	fmt.Printf("[DEBUG] kubectl command: kubectl %v\n", strings.Join(args, " "))

	// Executar kubectl cp
	cmd := exec.Command("kubectl", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Limpar arquivo temporário em caso de erro
		os.Remove(tmpPath)
		return "", fmt.Errorf("kubectl cp failed: %v - output: %s", err, string(output))
	}

	// Log de sucesso
	fmt.Printf("[DEBUG] kubectl cp success, file created: %s\n", tmpPath)

	return tmpPath, nil
}

// CleanupTempFile remove arquivo temporário criado por CopyFromPod
func (c *Client) CleanupTempFile(path string) error {
	if path == "" {
		return nil
	}
	return os.Remove(path)
}

// CopyMultipleFromPod copia múltiplos arquivos/diretórios do pod e empacota em tar.gz
// Retorna o caminho do arquivo tar.gz criado
func (c *Client) CopyMultipleFromPod(namespace, podName, container string, remotePaths []string) (string, error) {
	if len(remotePaths) == 0 {
		return "", fmt.Errorf("no files specified")
	}

	// Criar diretório temporário para armazenar os arquivos
	tempDir, err := os.MkdirTemp("", "pod-batch-download-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tempDir) // Limpar diretório ao final

	fmt.Printf("[DEBUG] Batch download: created temp dir %s\n", tempDir)

	// Copiar cada arquivo para o diretório temporário
	for i, remotePath := range remotePaths {
		fmt.Printf("[DEBUG] Copying file %d/%d: %s\n", i+1, len(remotePaths), remotePath)

		// Extrair nome do arquivo do path
		fileName := remotePath
		if strings.Contains(fileName, "/") {
			parts := strings.Split(fileName, "/")
			fileName = parts[len(parts)-1]
		}

		// Path de destino no temp dir
		localPath := fmt.Sprintf("%s/%s", tempDir, fileName)

		// Executar kubectl cp
		source := fmt.Sprintf("%s/%s:%s", namespace, podName, remotePath)
		args := []string{"cp", source, localPath, "--context", c.cluster}
		if container != "" {
			args = append(args, "-c", container)
		}

		cmd := exec.Command("kubectl", args...)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("failed to copy %s: %v - output: %s", remotePath, err, string(output))
		}
	}

	// Criar tar.gz com todos os arquivos
	tarPath := fmt.Sprintf("%s.tar.gz", tempDir)
	fmt.Printf("[DEBUG] Creating tar.gz: %s\n", tarPath)

	tarArgs := []string{"-czf", tarPath, "-C", tempDir, "."}
	tarCmd := exec.Command("tar", tarArgs...)
	tarOutput, err := tarCmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to create tar.gz: %v - output: %s", err, string(tarOutput))
	}

	// Verificar se tar.gz foi criado
	if _, err := os.Stat(tarPath); err != nil {
		return "", fmt.Errorf("tar.gz file not created: %w", err)
	}

	fmt.Printf("[DEBUG] Batch download complete: %s (%d files)\n", tarPath, len(remotePaths))

	return tarPath, nil
}

// FileInfo representa informações de um arquivo/diretório no pod
type FileInfo struct {
	Name        string    `json:"name"`
	Path        string    `json:"path"`
	Size        int64     `json:"size"`
	IsDirectory bool      `json:"isDirectory"`
	Permissions string    `json:"permissions"`
	ModTime     time.Time `json:"modTime"`
}

// ListDirectory lista arquivos e diretórios em um caminho do pod
func (c *Client) ListDirectory(namespace, podName, container, remotePath string) ([]FileInfo, error) {
	// Comando: ls -la --time-style='+%Y-%m-%d %H:%M:%S' <path>
	// Output: drwxr-xr-x 2 root root 4096 2024-10-31 13:07:34 dirname
	//         -rw-r----- 1 root root 214K 2024-10-31 13:07:34 filename.jpg

	lsCmd := fmt.Sprintf("ls -la --time-style='+%%Y-%%m-%%d %%H:%%M:%%S' %s 2>&1", remotePath)

	args := []string{"exec", podName, "-n", namespace, "--context", c.cluster}
	if container != "" {
		args = append(args, "-c", container)
	}
	args = append(args, "--", "sh", "-c", lsCmd)

	cmd := exec.Command("kubectl", args...)
	output, err := cmd.CombinedOutput()

	// Se houve erro mas temos output, tentar fazer parse mesmo assim
	// (alguns erros de ls são não-fatais)
	outputStr := string(output)
	if err != nil && len(outputStr) == 0 {
		return nil, fmt.Errorf("failed to list directory: %v", err)
	}

	// Parse output do ls
	lines := strings.Split(outputStr, "\n")
	var files []FileInfo

	for _, line := range lines {
		line = strings.TrimSpace(line)
		// Ignorar linhas vazias, total e mensagens de erro
		if line == "" ||
			strings.HasPrefix(line, "total ") ||
			strings.HasPrefix(line, "ls:") ||
			strings.HasPrefix(line, "cannot access") {
			continue
		}

		// Parse: drwxr-xr-x 2 root root 4096 2024-10-31 13:07:34 filename
		parts := strings.Fields(line)
		if len(parts) < 9 {
			continue // Linha inválida
		}

		permissions := parts[0]
		isDir := strings.HasPrefix(permissions, "d")
		sizeStr := parts[4]
		dateStr := parts[5] + " " + parts[6]
		name := strings.Join(parts[7:], " ")

		// Ignorar . e ..
		if name == "." || name == ".." {
			continue
		}

		// Parse size (pode ter sufixo K, M, G)
		var size int64
		if strings.HasSuffix(sizeStr, "K") {
			sizeStr = strings.TrimSuffix(sizeStr, "K")
			if s, err := strconv.ParseFloat(sizeStr, 64); err == nil {
				size = int64(s * 1024)
			}
		} else if strings.HasSuffix(sizeStr, "M") {
			sizeStr = strings.TrimSuffix(sizeStr, "M")
			if s, err := strconv.ParseFloat(sizeStr, 64); err == nil {
				size = int64(s * 1024 * 1024)
			}
		} else if strings.HasSuffix(sizeStr, "G") {
			sizeStr = strings.TrimSuffix(sizeStr, "G")
			if s, err := strconv.ParseFloat(sizeStr, 64); err == nil {
				size = int64(s * 1024 * 1024 * 1024)
			}
		} else {
			if s, err := strconv.ParseInt(sizeStr, 10, 64); err == nil {
				size = s
			}
		}

		// Parse modTime
		modTime, _ := time.Parse("2006-01-02 15:04:05", dateStr)

		// Construir path completo
		fullPath := strings.TrimSuffix(remotePath, "/") + "/" + name

		files = append(files, FileInfo{
			Name:        name,
			Path:        fullPath,
			Size:        size,
			IsDirectory: isDir,
			Permissions: permissions,
			ModTime:     modTime,
		})
	}

	return files, nil
}

// ResourcePatchRequest representa os campos que podem ser atualizados via patch
type ResourcePatchRequest struct {
	CPURequest    string
	MemoryRequest string
	CPULimit      string
	MemoryLimit   string
	Replicas      *int32
	ContainerName string // Nome do container a atualizar (opcional, padrão: primeiro container)
}

// PatchDeploymentResources atualiza apenas os recursos (CPU/memória) de um Deployment
// usando Server-Side Apply para compatibilidade com Helm
func (c *Client) PatchDeploymentResources(ctx context.Context, namespace, name string, req ResourcePatchRequest) (*appsv1.Deployment, error) {
	// Buscar deployment atual para obter estrutura
	deployment, err := c.clientset.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get deployment %s/%s: %w", namespace, name, err)
	}

	// Encontrar o container correto
	containerIdx := 0
	if req.ContainerName != "" {
		found := false
		for i, container := range deployment.Spec.Template.Spec.Containers {
			if container.Name == req.ContainerName {
				containerIdx = i
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("container %s not found in deployment %s/%s", req.ContainerName, namespace, name)
		}
	}

	if len(deployment.Spec.Template.Spec.Containers) == 0 {
		return nil, fmt.Errorf("deployment %s/%s has no containers", namespace, name)
	}

	// Aplicar mudanças
	container := &deployment.Spec.Template.Spec.Containers[containerIdx]
	if container.Resources.Requests == nil {
		container.Resources.Requests = corev1.ResourceList{}
	}
	if container.Resources.Limits == nil {
		container.Resources.Limits = corev1.ResourceList{}
	}

	if req.CPURequest != "" {
		if qty, err := resource.ParseQuantity(req.CPURequest); err == nil {
			container.Resources.Requests[corev1.ResourceCPU] = qty
		}
	}
	if req.MemoryRequest != "" {
		if qty, err := resource.ParseQuantity(req.MemoryRequest); err == nil {
			container.Resources.Requests[corev1.ResourceMemory] = qty
		}
	}
	if req.CPULimit != "" {
		if qty, err := resource.ParseQuantity(req.CPULimit); err == nil {
			container.Resources.Limits[corev1.ResourceCPU] = qty
		}
	}
	if req.MemoryLimit != "" {
		if qty, err := resource.ParseQuantity(req.MemoryLimit); err == nil {
			container.Resources.Limits[corev1.ResourceMemory] = qty
		}
	}
	if req.Replicas != nil {
		deployment.Spec.Replicas = req.Replicas
	}

	// Converter para YAML e usar Apply com Server-Side Apply
	yamlBytes, err := yaml.Marshal(deployment)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal deployment: %w", err)
	}

	// Usar ApplyDeployment com Force=true para resolver conflitos com Helm
	return c.ApplyDeployment(ctx, string(yamlBytes), "prometheus-resource-editor", namespace, name, false)
}

// PatchStatefulSetResources atualiza apenas os recursos (CPU/memória) de um StatefulSet
// usando Server-Side Apply para compatibilidade com Helm
func (c *Client) PatchStatefulSetResources(ctx context.Context, namespace, name string, req ResourcePatchRequest) (*appsv1.StatefulSet, error) {
	// Buscar statefulset atual
	sts, err := c.clientset.AppsV1().StatefulSets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get statefulset %s/%s: %w", namespace, name, err)
	}

	// Encontrar o container correto
	containerIdx := 0
	if req.ContainerName != "" {
		found := false
		for i, container := range sts.Spec.Template.Spec.Containers {
			if container.Name == req.ContainerName {
				containerIdx = i
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("container %s not found in statefulset %s/%s", req.ContainerName, namespace, name)
		}
	}

	if len(sts.Spec.Template.Spec.Containers) == 0 {
		return nil, fmt.Errorf("statefulset %s/%s has no containers", namespace, name)
	}

	// Aplicar mudanças
	container := &sts.Spec.Template.Spec.Containers[containerIdx]
	if container.Resources.Requests == nil {
		container.Resources.Requests = corev1.ResourceList{}
	}
	if container.Resources.Limits == nil {
		container.Resources.Limits = corev1.ResourceList{}
	}

	if req.CPURequest != "" {
		if qty, err := resource.ParseQuantity(req.CPURequest); err == nil {
			container.Resources.Requests[corev1.ResourceCPU] = qty
		}
	}
	if req.MemoryRequest != "" {
		if qty, err := resource.ParseQuantity(req.MemoryRequest); err == nil {
			container.Resources.Requests[corev1.ResourceMemory] = qty
		}
	}
	if req.CPULimit != "" {
		if qty, err := resource.ParseQuantity(req.CPULimit); err == nil {
			container.Resources.Limits[corev1.ResourceCPU] = qty
		}
	}
	if req.MemoryLimit != "" {
		if qty, err := resource.ParseQuantity(req.MemoryLimit); err == nil {
			container.Resources.Limits[corev1.ResourceMemory] = qty
		}
	}
	if req.Replicas != nil {
		sts.Spec.Replicas = req.Replicas
	}

	// Converter para YAML e usar Apply com Server-Side Apply
	yamlBytes, err := yaml.Marshal(sts)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal statefulset: %w", err)
	}

	return c.ApplyStatefulSet(ctx, string(yamlBytes), "prometheus-resource-editor", namespace, name, false)
}

// PatchDaemonSetResources atualiza apenas os recursos (CPU/memória) de um DaemonSet
// usando Server-Side Apply para compatibilidade com Helm
func (c *Client) PatchDaemonSetResources(ctx context.Context, namespace, name string, req ResourcePatchRequest) (*appsv1.DaemonSet, error) {
	// Buscar daemonset atual
	ds, err := c.clientset.AppsV1().DaemonSets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get daemonset %s/%s: %w", namespace, name, err)
	}

	// Encontrar o container correto
	containerIdx := 0
	if req.ContainerName != "" {
		found := false
		for i, container := range ds.Spec.Template.Spec.Containers {
			if container.Name == req.ContainerName {
				containerIdx = i
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("container %s not found in daemonset %s/%s", req.ContainerName, namespace, name)
		}
	}

	if len(ds.Spec.Template.Spec.Containers) == 0 {
		return nil, fmt.Errorf("daemonset %s/%s has no containers", namespace, name)
	}

	// Aplicar mudanças
	container := &ds.Spec.Template.Spec.Containers[containerIdx]
	if container.Resources.Requests == nil {
		container.Resources.Requests = corev1.ResourceList{}
	}
	if container.Resources.Limits == nil {
		container.Resources.Limits = corev1.ResourceList{}
	}

	if req.CPURequest != "" {
		if qty, err := resource.ParseQuantity(req.CPURequest); err == nil {
			container.Resources.Requests[corev1.ResourceCPU] = qty
		}
	}
	if req.MemoryRequest != "" {
		if qty, err := resource.ParseQuantity(req.MemoryRequest); err == nil {
			container.Resources.Requests[corev1.ResourceMemory] = qty
		}
	}
	if req.CPULimit != "" {
		if qty, err := resource.ParseQuantity(req.CPULimit); err == nil {
			container.Resources.Limits[corev1.ResourceCPU] = qty
		}
	}
	if req.MemoryLimit != "" {
		if qty, err := resource.ParseQuantity(req.MemoryLimit); err == nil {
			container.Resources.Limits[corev1.ResourceMemory] = qty
		}
	}

	// Converter para YAML e usar Apply com Server-Side Apply
	yamlBytes, err := yaml.Marshal(ds)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal daemonset: %w", err)
	}

	return c.ApplyDaemonSet(ctx, string(yamlBytes), "prometheus-resource-editor", namespace, name, false)
}
