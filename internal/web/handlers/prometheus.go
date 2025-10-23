package handlers

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"k8s-hpa-manager/internal/config"
	"k8s-hpa-manager/internal/kubernetes"

	"github.com/gin-gonic/gin"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PrometheusHandler gerencia requisições relacionadas ao Prometheus Stack
type PrometheusHandler struct {
	kubeManager *config.KubeConfigManager
}

// NewPrometheusHandler cria um novo handler de Prometheus
func NewPrometheusHandler(km *config.KubeConfigManager) *PrometheusHandler {
	return &PrometheusHandler{kubeManager: km}
}

// PrometheusResource representa um recurso do Prometheus Stack
type PrometheusResource struct {
	Name                 string `json:"name"`
	Namespace            string `json:"namespace"`
	Type                 string `json:"type"`      // Deployment, StatefulSet, DaemonSet
	Component            string `json:"component"` // prometheus-server, grafana, etc.
	Replicas             int32  `json:"replicas"`
	CurrentCPURequest    string `json:"current_cpu_request"`
	CurrentMemoryRequest string `json:"current_memory_request"`
	CurrentCPULimit      string `json:"current_cpu_limit"`
	CurrentMemoryLimit   string `json:"current_memory_limit"`
	CPUUsage             string `json:"cpu_usage,omitempty"`    // Uso atual (se disponível)
	MemoryUsage          string `json:"memory_usage,omitempty"` // Uso atual (se disponível)
}

// List retorna todos os recursos do Prometheus Stack (em todos os namespaces se namespace não especificado)
func (h *PrometheusHandler) List(c *gin.Context) {
	cluster := c.Query("cluster")
	namespace := c.Query("namespace")

	if cluster == "" {
		c.JSON(400, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "MISSING_PARAMETERS",
				"message": "Parameter 'cluster' is required",
			},
		})
		return
	}

	// Obter client do cluster
	client, err := h.kubeManager.GetClient(cluster)
	if err != nil {
		c.JSON(500, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "KUBERNETES_CLIENT_ERROR",
				"message": fmt.Sprintf("Failed to get Kubernetes client: %v", err),
			},
		})
		return
	}

	resources := make([]PrometheusResource, 0)

	// Definir namespace para busca (vazio significa todos os namespaces)
	namespaceFilter := namespace
	if namespace == "" {
		namespaceFilter = metav1.NamespaceAll
	}

	// Listar Deployments relacionados ao Prometheus/Grafana
	deployments, err := client.AppsV1().Deployments(namespaceFilter).List(context.Background(), metav1.ListOptions{})
	if err == nil {
		fmt.Printf("[DEBUG] Found %d deployments in namespace filter '%s'\n", len(deployments.Items), namespaceFilter)
		for _, dep := range deployments.Items {
			fmt.Printf("[DEBUG] Checking deployment: %s in namespace %s\n", dep.Name, dep.Namespace)
			if isPrometheusRelated(dep.Name) {
				fmt.Printf("[DEBUG] ✅ Deployment %s IS Prometheus-related (name match)\n", dep.Name)
				resource := extractResourceFromDeployment(&dep)
				resources = append(resources, resource)
			} else {
				fmt.Printf("[DEBUG] ❌ Deployment %s is NOT Prometheus-related\n", dep.Name)
			}
		}
	} else {
		fmt.Printf("[DEBUG] Error listing deployments: %v\n", err)
	}

	// Listar StatefulSets relacionados ao Prometheus
	statefulSets, err := client.AppsV1().StatefulSets(namespaceFilter).List(context.Background(), metav1.ListOptions{})
	if err == nil {
		fmt.Printf("[DEBUG] Found %d statefulsets in namespace filter '%s'\n", len(statefulSets.Items), namespaceFilter)
		for _, sts := range statefulSets.Items {
			fmt.Printf("[DEBUG] Checking statefulset: %s in namespace %s\n", sts.Name, sts.Namespace)
			if isPrometheusRelated(sts.Name) {
				fmt.Printf("[DEBUG] ✅ StatefulSet %s IS Prometheus-related (name match)\n", sts.Name)
				resource := extractResourceFromStatefulSet(&sts)
				resources = append(resources, resource)
			} else {
				fmt.Printf("[DEBUG] ❌ StatefulSet %s is NOT Prometheus-related\n", sts.Name)
			}
		}
	} else {
		fmt.Printf("[DEBUG] Error listing statefulsets: %v\n", err)
	}

	// Listar DaemonSets relacionados ao Prometheus
	daemonSets, err := client.AppsV1().DaemonSets(namespaceFilter).List(context.Background(), metav1.ListOptions{})
	if err == nil {
		fmt.Printf("[DEBUG] Found %d daemonsets in namespace filter '%s'\n", len(daemonSets.Items), namespaceFilter)
		for _, ds := range daemonSets.Items {
			fmt.Printf("[DEBUG] Checking daemonset: %s in namespace %s\n", ds.Name, ds.Namespace)
			if isPrometheusRelated(ds.Name) {
				fmt.Printf("[DEBUG] ✅ DaemonSet %s IS Prometheus-related (name match)\n", ds.Name)
				resource := extractResourceFromDaemonSet(&ds)
				resources = append(resources, resource)
			} else {
				fmt.Printf("[DEBUG] ❌ DaemonSet %s is NOT Prometheus-related\n", ds.Name)
			}
		}
	} else {
		fmt.Printf("[DEBUG] Error listing daemonsets: %v\n", err)
	}

	fmt.Printf("[DEBUG] Total Prometheus resources found: %d\n", len(resources))

	c.JSON(200, gin.H{
		"success": true,
		"data":    resources,
		"count":   len(resources),
	})
}

// Update atualiza recursos de um componente do Prometheus
func (h *PrometheusHandler) Update(c *gin.Context) {
	cluster := c.Param("cluster")
	namespace := c.Param("namespace")
	name := c.Param("name")
	resourceType := c.Param("type") // deployment, statefulset, daemonset

	var req struct {
		CPURequest    string `json:"cpu_request"`
		MemoryRequest string `json:"memory_request"`
		CPULimit      string `json:"cpu_limit"`
		MemoryLimit   string `json:"memory_limit"`
		Replicas      *int32 `json:"replicas,omitempty"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "INVALID_REQUEST",
				"message": fmt.Sprintf("Invalid request body: %v", err),
			},
		})
		return
	}

	// Converter para resourceUpdateRequest
	updateReq := resourceUpdateRequest{
		CPURequest:    req.CPURequest,
		MemoryRequest: req.MemoryRequest,
		CPULimit:      req.CPULimit,
		MemoryLimit:   req.MemoryLimit,
		Replicas:      req.Replicas,
	}

	// Atualizar baseado no tipo
	var err error
	switch resourceType {
	case "deployment":
		err = h.updateDeployment(h.kubeManager, cluster, namespace, name, updateReq)
	case "statefulset":
		err = h.updateStatefulSet(h.kubeManager, cluster, namespace, name, updateReq)
	case "daemonset":
		err = h.updateDaemonSet(h.kubeManager, cluster, namespace, name, updateReq)
	default:
		c.JSON(400, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "INVALID_TYPE",
				"message": fmt.Sprintf("Invalid resource type: %s", resourceType),
			},
		})
		return
	}

	if err != nil {
		c.JSON(500, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "UPDATE_FAILED",
				"message": fmt.Sprintf("Failed to update resource: %v", err),
			},
		})
		return
	}

	c.JSON(200, gin.H{
		"success": true,
		"message": fmt.Sprintf("Resource '%s' updated successfully", name),
	})
}

// Funções auxiliares

func isPrometheusRelated(name string) bool {
	nameLower := strings.ToLower(name)
	keywords := []string{
		// Prometheus core components
		"prometheus", "prom",
		// Grafana
		"grafana",
		// Alerting
		"alertmanager", "alert-manager",
		// Exporters
		"node-exporter", "node_exporter", "nodeexporter",
		"kube-state-metrics", "kube_state_metrics", "kubestatemetrics",
		"prometheus-pushgateway", "pushgateway", "push-gateway",
		"blackbox-exporter", "blackbox_exporter", "blackboxexporter",
		"cadvisor", "c-advisor",
		// Monitoring stack components
		"metrics-server", "metrics_server", "metricsserver",
		"thanos", "cortex", "mimir",
		// Common monitoring patterns
		"monitoring", "observability", "telemetry",
		// Helm chart patterns
		"kube-prometheus", "prometheus-operator", "prometheus_operator",
		"prometheus-stack", "monitoring-stack",
	}

	for _, keyword := range keywords {
		if strings.Contains(nameLower, keyword) {
			return true
		}
	}
	return false
}

func isMonitoringNamespace(namespace string) bool {
	namespaceLower := strings.ToLower(namespace)
	monitoringNamespaces := []string{
		"monitoring", "prometheus", "observability",
		"kube-prometheus", "grafana", "metrics",
		"telemetry", "logging", "ops",
	}
	for _, ns := range monitoringNamespaces {
		if namespaceLower == ns || strings.Contains(namespaceLower, ns) {
			return true
		}
	}
	return false
}

func contains(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}

func extractResourceFromDeployment(dep *appsv1.Deployment) PrometheusResource {
	resource := PrometheusResource{
		Name:      dep.Name,
		Namespace: dep.Namespace,
		Type:      "Deployment",
		Component: getComponentName(dep.Name),
		Replicas:  *dep.Spec.Replicas,
	}

	if len(dep.Spec.Template.Spec.Containers) > 0 {
		container := dep.Spec.Template.Spec.Containers[0]
		extractContainerResources(&resource, &container)
	}

	return resource
}

func extractResourceFromStatefulSet(sts *appsv1.StatefulSet) PrometheusResource {
	resource := PrometheusResource{
		Name:      sts.Name,
		Namespace: sts.Namespace,
		Type:      "StatefulSet",
		Component: getComponentName(sts.Name),
		Replicas:  *sts.Spec.Replicas,
	}

	if len(sts.Spec.Template.Spec.Containers) > 0 {
		container := sts.Spec.Template.Spec.Containers[0]
		extractContainerResources(&resource, &container)
	}

	return resource
}

func extractResourceFromDaemonSet(ds *appsv1.DaemonSet) PrometheusResource {
	resource := PrometheusResource{
		Name:      ds.Name,
		Namespace: ds.Namespace,
		Type:      "DaemonSet",
		Component: getComponentName(ds.Name),
		Replicas:  0, // DaemonSets não têm replicas fixas
	}

	if len(ds.Spec.Template.Spec.Containers) > 0 {
		container := ds.Spec.Template.Spec.Containers[0]
		extractContainerResources(&resource, &container)
	}

	return resource
}

func extractContainerResources(resource *PrometheusResource, container *corev1.Container) {
	if container.Resources.Requests != nil {
		if cpu, ok := container.Resources.Requests[corev1.ResourceCPU]; ok {
			resource.CurrentCPURequest = cpu.String()
		}
		if mem, ok := container.Resources.Requests[corev1.ResourceMemory]; ok {
			resource.CurrentMemoryRequest = mem.String()
		}
	}

	if container.Resources.Limits != nil {
		if cpu, ok := container.Resources.Limits[corev1.ResourceCPU]; ok {
			resource.CurrentCPULimit = cpu.String()
		}
		if mem, ok := container.Resources.Limits[corev1.ResourceMemory]; ok {
			resource.CurrentMemoryLimit = mem.String()
		}
	}
}

func getComponentName(name string) string {
	nameLower := strings.ToLower(name)

	components := map[string]string{
		// Prometheus core
		"prometheus-server":   "Prometheus Server",
		"prometheus-operator": "Prometheus Operator",
		"prometheus":          "Prometheus",
		"prom":                "Prometheus",

		// Grafana
		"grafana": "Grafana",

		// Alerting
		"alertmanager":  "Alertmanager",
		"alert-manager": "Alertmanager",

		// Exporters
		"node-exporter":          "Node Exporter",
		"node_exporter":          "Node Exporter",
		"nodeexporter":           "Node Exporter",
		"kube-state-metrics":     "Kube State Metrics",
		"kube_state_metrics":     "Kube State Metrics",
		"kubestatemetrics":       "Kube State Metrics",
		"prometheus-pushgateway": "Pushgateway",
		"pushgateway":            "Pushgateway",
		"push-gateway":           "Pushgateway",
		"blackbox-exporter":      "Blackbox Exporter",
		"blackbox_exporter":      "Blackbox Exporter",
		"blackboxexporter":       "Blackbox Exporter",
		"cadvisor":               "cAdvisor",
		"c-advisor":              "cAdvisor",

		// Metrics & Storage
		"metrics-server": "Metrics Server",
		"metrics_server": "Metrics Server",
		"metricsserver":  "Metrics Server",
		"thanos":         "Thanos",
		"cortex":         "Cortex",
		"mimir":          "Mimir",

		// Monitoring stacks
		"monitoring-stack": "Monitoring Stack",
		"prometheus-stack": "Prometheus Stack",
		"kube-prometheus":  "Kube Prometheus",
	}

	// Buscar match exato primeiro
	for key, value := range components {
		if nameLower == key || strings.Contains(nameLower, key) {
			return value
		}
	}

	// Se não encontrou match, retornar nome original com primeira letra maiúscula
	if len(name) > 0 {
		return strings.ToUpper(string(name[0])) + name[1:]
	}
	return name
}

type resourceUpdateRequest struct {
	CPURequest    string
	MemoryRequest string
	CPULimit      string
	MemoryLimit   string
	Replicas      *int32
}

func (h *PrometheusHandler) updateDeployment(kubeManager *config.KubeConfigManager, cluster, namespace, name string, req resourceUpdateRequest) error {
	client, err := kubeManager.GetClient(cluster)
	if err != nil {
		return err
	}

	deployment, err := client.AppsV1().Deployments(namespace).Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		return err
	}

	// Atualizar replicas se fornecido
	if req.Replicas != nil {
		deployment.Spec.Replicas = req.Replicas
	}

	// Atualizar recursos do primeiro container
	if len(deployment.Spec.Template.Spec.Containers) > 0 {
		updateContainerResources(&deployment.Spec.Template.Spec.Containers[0], req.CPURequest, req.MemoryRequest, req.CPULimit, req.MemoryLimit)
	}

	_, err = client.AppsV1().Deployments(namespace).Update(context.Background(), deployment, metav1.UpdateOptions{})
	return err
}

func (h *PrometheusHandler) updateStatefulSet(kubeManager *config.KubeConfigManager, cluster, namespace, name string, req resourceUpdateRequest) error {
	client, err := kubeManager.GetClient(cluster)
	if err != nil {
		return err
	}

	sts, err := client.AppsV1().StatefulSets(namespace).Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		return err
	}

	if req.Replicas != nil {
		sts.Spec.Replicas = req.Replicas
	}

	if len(sts.Spec.Template.Spec.Containers) > 0 {
		updateContainerResources(&sts.Spec.Template.Spec.Containers[0], req.CPURequest, req.MemoryRequest, req.CPULimit, req.MemoryLimit)
	}

	_, err = client.AppsV1().StatefulSets(namespace).Update(context.Background(), sts, metav1.UpdateOptions{})
	return err
}

func (h *PrometheusHandler) updateDaemonSet(kubeManager *config.KubeConfigManager, cluster, namespace, name string, req resourceUpdateRequest) error {
	client, err := kubeManager.GetClient(cluster)
	if err != nil {
		return err
	}

	ds, err := client.AppsV1().DaemonSets(namespace).Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		return err
	}

	if len(ds.Spec.Template.Spec.Containers) > 0 {
		updateContainerResources(&ds.Spec.Template.Spec.Containers[0], req.CPURequest, req.MemoryRequest, req.CPULimit, req.MemoryLimit)
	}

	_, err = client.AppsV1().DaemonSets(namespace).Update(context.Background(), ds, metav1.UpdateOptions{})
	return err
}

func updateContainerResources(container *corev1.Container, cpuReq, memReq, cpuLim, memLim string) {
	if container.Resources.Requests == nil {
		container.Resources.Requests = corev1.ResourceList{}
	}
	if container.Resources.Limits == nil {
		container.Resources.Limits = corev1.ResourceList{}
	}

	if cpuReq != "" {
		if qty, err := resource.ParseQuantity(cpuReq); err == nil {
			container.Resources.Requests[corev1.ResourceCPU] = qty
		}
	}
	if memReq != "" {
		if qty, err := resource.ParseQuantity(memReq); err == nil {
			container.Resources.Requests[corev1.ResourceMemory] = qty
		}
	}
	if cpuLim != "" {
		if qty, err := resource.ParseQuantity(cpuLim); err == nil {
			container.Resources.Limits[corev1.ResourceCPU] = qty
		}
	}
	if memLim != "" {
		if qty, err := resource.ParseQuantity(memLim); err == nil {
			container.Resources.Limits[corev1.ResourceMemory] = qty
		}
	}
}

// Rollout executa rollout de um recurso Prometheus específico (Deployment, StatefulSet ou DaemonSet)
func (h *PrometheusHandler) Rollout(c *gin.Context) {
	cluster := c.Param("cluster")
	namespace := c.Param("namespace")
	resourceType := c.Param("type") // deployment, statefulset, daemonset
	name := c.Param("name")

	if cluster == "" || namespace == "" || resourceType == "" || name == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "cluster, namespace, type e name são obrigatórios",
		})
		return
	}

	// Obter client K8s padrão
	clientSet, err := h.kubeManager.GetClient(cluster)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   fmt.Sprintf("failed to get kubernetes client: %v", err),
		})
		return
	}

	// Criar nosso wrapper personalizado com métodos de rollout
	client := kubernetes.NewClient(clientSet, cluster)

	ctx := context.Background()

	// Executar rollout com base no tipo de recurso
	switch strings.ToLower(resourceType) {
	case "deployment":
		err = client.RolloutDeployment(ctx, namespace, name)
	case "statefulset":
		err = client.RolloutStatefulSet(ctx, namespace, name)
	case "daemonset":
		err = client.RolloutDaemonSet(ctx, namespace, name)
	default:
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   fmt.Sprintf("tipo de recurso inválido: %s (use deployment, statefulset ou daemonset)", resourceType),
		})
		return
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   fmt.Sprintf("failed to rollout %s: %v", resourceType, err),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": fmt.Sprintf("Rollout de %s/%s iniciado com sucesso", resourceType, name),
	})
}
