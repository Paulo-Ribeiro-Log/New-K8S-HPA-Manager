package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"k8s-hpa-manager/internal/config"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// ServiceMeshHandler gerencia endpoints de service mesh (Kiali integration)
type ServiceMeshHandler struct {
	kubeManager *config.KubeConfigManager
}

// NewServiceMeshHandler cria um novo handler de service mesh
func NewServiceMeshHandler(kubeManager *config.KubeConfigManager) *ServiceMeshHandler {
	return &ServiceMeshHandler{
		kubeManager: kubeManager,
	}
}

// KialiGraphNode representa um nó no service graph
type KialiGraphNode struct {
	Data struct {
		ID                string  `json:"id"`
		NodeType          string  `json:"nodeType"`
		Namespace         string  `json:"namespace"`
		Workload          string  `json:"workload"`
		App               string  `json:"app"`
		Version           string  `json:"version"`
		Service           string  `json:"service"`
		DestServices      []struct {
			Name      string `json:"name"`
			Namespace string `json:"namespace"`
		} `json:"destServices,omitempty"`
		Traffic []struct {
			Protocol string  `json:"protocol"`
			Rates    struct {
				HTTP      string  `json:"http,omitempty"`
				HTTPPercentReq string `json:"httpPercentReq,omitempty"`
			} `json:"rates"`
		} `json:"traffic,omitempty"`
		IsInaccessible bool    `json:"isInaccessible,omitempty"`
		IsOutside      bool    `json:"isOutside,omitempty"`
		IsRoot         bool    `json:"isRoot,omitempty"`
		IsServiceEntry bool    `json:"isServiceEntry,omitempty"`
	} `json:"data"`
}

// KialiGraphEdge representa uma aresta no service graph
type KialiGraphEdge struct {
	Data struct {
		ID     string `json:"id"`
		Source string `json:"source"`
		Target string `json:"target"`
		Traffic struct {
			Protocol string `json:"protocol"`
			Rates    struct {
				HTTP           string  `json:"http,omitempty"`
				HTTPPercentReq string  `json:"httpPercentReq,omitempty"`
				TCP            string  `json:"tcp,omitempty"`
			} `json:"rates"`
			Responses struct {
				Status200 struct {
					Flags struct {
						PercentReq string `json:"-"`
					} `json:"flags"`
					Hosts map[string]struct {
						PercentReq string `json:"-"`
					} `json:"hosts"`
				} `json:"200,omitempty"`
				Status500 struct {
					Flags struct {
						PercentReq string `json:"-"`
					} `json:"flags"`
				} `json:"500,omitempty"`
			} `json:"responses,omitempty"`
		} `json:"traffic"`
		ResponseTime string `json:"responseTime,omitempty"`
	} `json:"data"`
}

// KialiGraphResponse representa a resposta completa da API Kiali
type KialiGraphResponse struct {
	Timestamp  int64            `json:"timestamp"`
	Duration   int              `json:"duration"`
	GraphType  string           `json:"graphType"`
	Elements   struct {
		Nodes []KialiGraphNode `json:"nodes"`
		Edges []KialiGraphEdge `json:"edges"`
	} `json:"elements"`
}

// SimplifiedNode representa um nó simplificado para o frontend
type SimplifiedNode struct {
	ID             string   `json:"id"`
	Label          string   `json:"label"`
	Type           string   `json:"type"` // workload, service, app
	Namespace      string   `json:"namespace"`
	App            string   `json:"app,omitempty"`
	Version        string   `json:"version,omitempty"`
	IsRoot         bool     `json:"isRoot,omitempty"`
	IsInaccessible bool     `json:"isInaccessible,omitempty"`
	IsOutside      bool     `json:"isOutside,omitempty"`
	RequestRate    string   `json:"requestRate,omitempty"`
	ErrorRate      string   `json:"errorRate,omitempty"`
}

// SimplifiedEdge representa uma aresta simplificada para o frontend
type SimplifiedEdge struct {
	ID           string  `json:"id"`
	Source       string  `json:"source"`
	Target       string  `json:"target"`
	Protocol     string  `json:"protocol,omitempty"`
	RequestRate  string  `json:"requestRate,omitempty"`
	ResponseTime string  `json:"responseTime,omitempty"`
	ErrorRate    float64 `json:"errorRate,omitempty"`
}

// ServiceGraphResponse representa o grafo simplificado
type ServiceGraphResponse struct {
	Nodes     []SimplifiedNode `json:"nodes"`
	Edges     []SimplifiedEdge `json:"edges"`
	Timestamp int64            `json:"timestamp"`
	Duration  int              `json:"duration"`
}

// GetServiceGraph retorna o grafo de serviços do Kiali
// GET /api/v1/servicemesh/graph?cluster=X&namespace=Y&duration=60s
func (h *ServiceMeshHandler) GetServiceGraph(c *gin.Context) {
	clusterName := c.Query("cluster")
	namespace := c.Query("namespace")
	duration := c.DefaultQuery("duration", "60s") // 1 minuto padrão
	graphType := c.DefaultQuery("graphType", "workload") // workload, app, service, versioned_app

	if clusterName == "" || namespace == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cluster e namespace são obrigatórios"})
		return
	}

	// Obter clientset do cluster
	clientset, restConfig, err := h.kubeManager.GetClientForCluster(clusterName)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("cluster inválido: %v", err)})
		return
	}

	// Descobrir o serviço Kiali no cluster
	kialiURL, err := h.discoverKialiService(clientset, restConfig)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "Kiali não encontrado no cluster",
			"details": err.Error(),
		})
		return
	}

	// Consultar a API do Kiali
	graphData, err := h.queryKialiGraph(restConfig, kialiURL, namespace, duration, graphType)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Erro ao consultar Kiali API",
			"details": err.Error(),
		})
		return
	}

	// Simplificar dados para o frontend
	response := h.simplifyGraphData(graphData)

	c.JSON(http.StatusOK, response)
}

// GetNamespaces retorna lista de namespaces com Istio habilitado
// GET /api/v1/servicemesh/namespaces?cluster=X
func (h *ServiceMeshHandler) GetNamespaces(c *gin.Context) {
	clusterName := c.Query("cluster")
	if clusterName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cluster é obrigatório"})
		return
	}

	clientset, _, err := h.kubeManager.GetClientForCluster(clusterName)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("cluster inválido: %v", err)})
		return
	}

	// Listar namespaces com label istio-injection=enabled
	namespaces, err := clientset.CoreV1().Namespaces().List(context.Background(), metav1.ListOptions{
		LabelSelector: "istio-injection=enabled",
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("erro ao listar namespaces: %v", err)})
		return
	}

	// Extrair nomes dos namespaces
	var namespaceNames []string
	for _, ns := range namespaces.Items {
		namespaceNames = append(namespaceNames, ns.Name)
	}

	c.JSON(http.StatusOK, gin.H{
		"cluster": clusterName,
		"namespaces": namespaceNames,
		"count": len(namespaceNames),
	})
}

// GetMetrics retorna métricas Istio agregadas de um namespace
// GET /api/v1/servicemesh/metrics?cluster=X&namespace=Y
func (h *ServiceMeshHandler) GetMetrics(c *gin.Context) {
	clusterName := c.Query("cluster")
	namespace := c.Query("namespace")

	if clusterName == "" || namespace == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cluster e namespace são obrigatórios"})
		return
	}

	clientset, restConfig, err := h.kubeManager.GetClientForCluster(clusterName)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("cluster inválido: %v", err)})
		return
	}

	// Descobrir Kiali
	kialiURL, err := h.discoverKialiService(clientset, restConfig)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "Kiali não encontrado no cluster",
		})
		return
	}

	// Consultar métricas do namespace via Kiali
	metrics, err := h.queryKialiMetrics(restConfig, kialiURL, namespace)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Erro ao consultar métricas",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, metrics)
}

// discoverKialiService descobre o endpoint do Kiali no cluster
func (h *ServiceMeshHandler) discoverKialiService(clientset *kubernetes.Clientset, restConfig *rest.Config) (string, error) {
	ctx := context.Background()

	// Tentar encontrar o serviço Kiali (normalmente em istio-system)
	namespaces := []string{"istio-system", "kiali", "kiali-operator"}
	
	for _, ns := range namespaces {
		services, err := clientset.CoreV1().Services(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			continue
		}

		for _, svc := range services.Items {
			// Procurar serviço com nome "kiali"
			if svc.Name == "kiali" {
				// Retornar URL interno do serviço
				port := 20001 // Porta padrão do Kiali
				if len(svc.Spec.Ports) > 0 {
					port = int(svc.Spec.Ports[0].Port)
				}
				return fmt.Sprintf("http://%s.%s.svc.cluster.local:%d", svc.Name, ns, port), nil
			}
		}
	}

	return "", fmt.Errorf("serviço Kiali não encontrado no cluster")
}

// queryKialiGraph consulta a API do Kiali para obter o service graph
func (h *ServiceMeshHandler) queryKialiGraph(restConfig *rest.Config, kialiURL, namespace, duration, graphType string) (*KialiGraphResponse, error) {
	// Construir URL da API Kiali
	url := fmt.Sprintf("%s/api/namespaces/%s/graph?duration=%s&graphType=%s&injectServiceNodes=true", 
		kialiURL, namespace, duration, graphType)

	// Criar HTTP client com kubeconfig para acesso interno ao cluster
	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: restConfig.TLSClientConfig.DeepCopy(),
		},
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("erro ao criar requisição: %w", err)
	}

	// Adicionar headers de autenticação se necessário
	if restConfig.BearerToken != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", restConfig.BearerToken))
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("erro ao executar requisição: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("kiali retornou status %d: %s", resp.StatusCode, string(body))
	}

	var graphData KialiGraphResponse
	if err := json.NewDecoder(resp.Body).Decode(&graphData); err != nil {
		return nil, fmt.Errorf("erro ao decodificar resposta: %w", err)
	}

	return &graphData, nil
}

// queryKialiMetrics consulta métricas agregadas via Kiali
func (h *ServiceMeshHandler) queryKialiMetrics(restConfig *rest.Config, kialiURL, namespace string) (map[string]interface{}, error) {
	// URL para métricas do namespace
	url := fmt.Sprintf("%s/api/namespaces/%s/metrics", kialiURL, namespace)

	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: restConfig.TLSClientConfig.DeepCopy(),
		},
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("erro ao criar requisição: %w", err)
	}

	if restConfig.BearerToken != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", restConfig.BearerToken))
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("erro ao executar requisição: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("kiali retornou status %d", resp.StatusCode)
	}

	var metrics map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&metrics); err != nil {
		return nil, fmt.Errorf("erro ao decodificar resposta: %w", err)
	}

	return metrics, nil
}

// simplifyGraphData converte dados do Kiali para formato simplificado
func (h *ServiceMeshHandler) simplifyGraphData(graphData *KialiGraphResponse) *ServiceGraphResponse {
	response := &ServiceGraphResponse{
		Nodes:     make([]SimplifiedNode, 0),
		Edges:     make([]SimplifiedEdge, 0),
		Timestamp: graphData.Timestamp,
		Duration:  graphData.Duration,
	}

	// Processar nós
	for _, node := range graphData.Elements.Nodes {
		simpleNode := SimplifiedNode{
			ID:             node.Data.ID,
			Type:           node.Data.NodeType,
			Namespace:      node.Data.Namespace,
			App:            node.Data.App,
			Version:        node.Data.Version,
			IsRoot:         node.Data.IsRoot,
			IsInaccessible: node.Data.IsInaccessible,
			IsOutside:      node.Data.IsOutside,
		}

		// Label = workload, service ou app
		switch node.Data.NodeType {
		case "workload":
			simpleNode.Label = node.Data.Workload
		case "service":
			simpleNode.Label = node.Data.Service
		case "app":
			simpleNode.Label = node.Data.App
		default:
			simpleNode.Label = node.Data.ID
		}

		// Extrair request rate (se disponível)
		if len(node.Data.Traffic) > 0 {
			simpleNode.RequestRate = node.Data.Traffic[0].Rates.HTTP
		}

		response.Nodes = append(response.Nodes, simpleNode)
	}

	// Processar arestas
	for _, edge := range graphData.Elements.Edges {
		simpleEdge := SimplifiedEdge{
			ID:           edge.Data.ID,
			Source:       edge.Data.Source,
			Target:       edge.Data.Target,
			Protocol:     edge.Data.Traffic.Protocol,
			RequestRate:  edge.Data.Traffic.Rates.HTTP,
			ResponseTime: edge.Data.ResponseTime,
		}

		response.Edges = append(response.Edges, simpleEdge)
	}

	return response
}
