package handlers

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"k8s-hpa-manager/internal/config"

	"github.com/gin-gonic/gin"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
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
		ID           string `json:"id"`
		NodeType     string `json:"nodeType"`
		Namespace    string `json:"namespace"`
		Workload     string `json:"workload"`
		App          string `json:"app"`
		Version      string `json:"version"`
		Service      string `json:"service"`
		DestServices []struct {
			Name      string `json:"name"`
			Namespace string `json:"namespace"`
		} `json:"destServices,omitempty"`
		Traffic []struct {
			Protocol string `json:"protocol"`
			Rates    struct {
				HTTP           string `json:"http,omitempty"`
				HTTPPercentReq string `json:"httpPercentReq,omitempty"`
			} `json:"rates"`
		} `json:"traffic,omitempty"`
		IsInaccessible bool `json:"isInaccessible,omitempty"`
		IsOutside      bool `json:"isOutside,omitempty"`
		IsRoot         bool `json:"isRoot,omitempty"`
		IsServiceEntry bool `json:"isServiceEntry,omitempty"`
	} `json:"data"`
}

// KialiGraphEdge representa uma aresta no service graph
type KialiGraphEdge struct {
	Data struct {
		ID      string `json:"id"`
		Source  string `json:"source"`
		Target  string `json:"target"`
		Traffic struct {
			Protocol string `json:"protocol"`
			Rates    struct {
				HTTP           string `json:"http,omitempty"`
				HTTPPercentReq string `json:"httpPercentReq,omitempty"`
				TCP            string `json:"tcp,omitempty"`
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
	Timestamp int64  `json:"timestamp"`
	Duration  int    `json:"duration"`
	GraphType string `json:"graphType"`
	Elements  struct {
		Nodes []KialiGraphNode `json:"nodes"`
		Edges []KialiGraphEdge `json:"edges"`
	} `json:"elements"`
}

// SimplifiedNode representa um nó simplificado para o frontend
type SimplifiedNode struct {
	ID             string `json:"id"`
	Label          string `json:"label"`
	Type           string `json:"type"` // workload, service, app
	Namespace      string `json:"namespace"`
	App            string `json:"app,omitempty"`
	Version        string `json:"version,omitempty"`
	IsRoot         bool   `json:"isRoot,omitempty"`
	IsInaccessible bool   `json:"isInaccessible,omitempty"`
	IsOutside      bool   `json:"isOutside,omitempty"`
	RequestRate    string `json:"requestRate,omitempty"`
	ErrorRate      string `json:"errorRate,omitempty"`
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
// GET /api/v1/servicemesh/graph?cluster=X&namespace=Y&duration=60s&graphType=workload&injectServiceNodes=true&includeIdleEdges=false&appenders=...
func (h *ServiceMeshHandler) GetServiceGraph(c *gin.Context) {
	clusterName := c.Query("cluster")
	namespace := c.Query("namespace")
	duration := c.DefaultQuery("duration", "60s")        // 1 minuto padrão
	graphType := c.DefaultQuery("graphType", "workload") // workload, app, service, versioned_app

	// Parâmetros opcionais do Kiali
	injectServiceNodes := c.DefaultQuery("injectServiceNodes", "true")
	includeIdleEdges := c.DefaultQuery("includeIdleEdges", "false")
	includeIdleNodes := c.DefaultQuery("includeIdleNodes", "false")
	appenders := c.DefaultQuery("appenders", "deadNode,sidecarsCheck,serviceEntry,istio")

	if clusterName == "" || namespace == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cluster e namespace são obrigatórios"})
		return
	}

	// Método 1: Tentar URL externa do Kiali (via Ingress) - PREFERIDO
	kialiURL, err := getKialiURL(clusterName)
	if err == nil {
		fmt.Printf("[ServiceMesh] ✅ Usando Kiali via URL externa: %s\n", kialiURL)

		// Consultar via HTTP direto ao Ingress
		graphData, err := h.queryKialiGraph(kialiURL, namespace, duration, graphType, injectServiceNodes, includeIdleEdges, includeIdleNodes, appenders)
		if err == nil {
			fmt.Printf("[ServiceMesh] ✅ Dados recebidos via URL externa: %d nodes, %d edges\n", len(graphData.Elements.Nodes), len(graphData.Elements.Edges))
			// Sucesso! Simplificar e retornar
			response := h.simplifyGraphData(graphData)
			c.JSON(http.StatusOK, response)
			return
		}

		fmt.Printf("[ServiceMesh] ❌ Erro ao consultar via URL externa: %v\n", err)
		fmt.Printf("[ServiceMesh] Tentando fallback para proxy do Kubernetes...\n")
	} else {
		fmt.Printf("[ServiceMesh] ❌ URL externa não disponível: %v\n", err)
		fmt.Printf("[ServiceMesh] Tentando proxy do Kubernetes...\n")
	}

	// Método 2 (Fallback): Tentar proxy do Kubernetes
	clientset, err := h.kubeManager.GetClient(clusterName)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("cluster inválido: %v", err)})
		return
	}

	// Descobrir o serviço Kiali no cluster
	kialiService, kialiNamespace, kialiPort, err := h.discoverKialiService(clientset)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error":   "Kiali não encontrado",
			"details": "Não foi possível acessar Kiali via URL externa nem proxy interno",
			"hint":    "Certifique-se de que o Kiali está acessível via Ingress ou instalado no cluster",
		})
		return
	}

	// Consultar a API do Kiali via proxy do Kubernetes
	graphData, err := h.queryKialiGraphViaProxy(clientset, kialiService, kialiNamespace, kialiPort, namespace, duration, graphType, injectServiceNodes, includeIdleEdges, includeIdleNodes, appenders)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":         "Erro ao consultar Kiali API",
			"details":       err.Error(),
			"kiali_service": fmt.Sprintf("%s.%s:%d", kialiService, kialiNamespace, kialiPort),
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

	// SEMPRE usar Kubernetes API para listar namespaces (não depende do Kiali)
	clientset, err := h.kubeManager.GetClient(clusterName)
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
		"cluster":    clusterName,
		"namespaces": namespaceNames,
		"count":      len(namespaceNames),
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

	clientset, err := h.kubeManager.GetClient(clusterName)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("cluster inválido: %v", err)})
		return
	}

	// Descobrir Kiali
	kialiService, kialiNamespace, kialiPort, err := h.discoverKialiService(clientset)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error":   "Kiali não encontrado no cluster",
			"details": err.Error(),
		})
		return
	}

	// Consultar métricas do namespace via Kiali
	metrics, err := h.queryKialiMetricsViaProxy(clientset, kialiService, kialiNamespace, kialiPort, namespace)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Erro ao consultar métricas",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, metrics)
}

// discoverKialiService descobre o serviço Kiali no cluster
func (h *ServiceMeshHandler) discoverKialiService(clientset kubernetes.Interface) (string, string, int, error) {
	ctx := context.Background()

	// Tentar encontrar o serviço Kiali (normalmente em istio-system)
	namespaces := []string{"istio-system", "kiali", "kiali-operator"}

	fmt.Printf("[ServiceMesh] Procurando serviço Kiali nos namespaces: %v\n", namespaces)

	for _, ns := range namespaces {
		services, err := clientset.CoreV1().Services(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			fmt.Printf("[ServiceMesh] Erro ao listar services em %s: %v\n", ns, err)
			continue
		}

		fmt.Printf("[ServiceMesh] Encontrados %d services em %s\n", len(services.Items), ns)

		for _, svc := range services.Items {
			// Procurar serviço com nome "kiali"
			if svc.Name == "kiali" {
				port := 20001 // Porta padrão do Kiali
				if len(svc.Spec.Ports) > 0 {
					port = int(svc.Spec.Ports[0].Port)
				}
				fmt.Printf("[ServiceMesh] Kiali encontrado! Service: %s, Namespace: %s, Port: %d\n", svc.Name, ns, port)
				return svc.Name, ns, port, nil
			}
		}
	}

	fmt.Printf("[ServiceMesh] Serviço Kiali NÃO encontrado em nenhum namespace\n")
	return "", "", 0, fmt.Errorf("serviço Kiali não encontrado nos namespaces: %v", namespaces)
}

// queryKialiGraphViaProxy consulta a API do Kiali via proxy do Kubernetes
func (h *ServiceMeshHandler) queryKialiGraphViaProxy(clientset kubernetes.Interface, serviceName, serviceNamespace string, servicePort int, namespace, duration, graphType, injectServiceNodes, includeIdleEdges, includeIdleNodes, appenders string) (*KialiGraphResponse, error) {
	// Construir URL via proxy do Kubernetes API
	// Formato correto: /api/v1/namespaces/{namespace}/services/[http:]name[:port]/proxy/{path}
	// API do Kiali usa /api/namespaces/graph?namespaces=X (não /api/namespaces/X/graph)
	proxyPath := fmt.Sprintf("/api/v1/namespaces/%s/services/http:%s:%d/proxy/api/namespaces/graph",
		serviceNamespace, serviceName, servicePort)

	queryParams := fmt.Sprintf("?namespaces=%s&duration=%s&graphType=%s&injectServiceNodes=%s&includeIdleEdges=%s&includeIdleNodes=%s&appenders=%s",
		namespace, duration, graphType, injectServiceNodes, includeIdleEdges, includeIdleNodes, appenders)
	proxyPath += queryParams

	fmt.Printf("[ServiceMesh] Proxy path completo: %s\n", proxyPath)

	// Fazer requisição via proxy
	result := clientset.CoreV1().RESTClient().Get().AbsPath(proxyPath).Do(context.Background())

	if result.Error() != nil {
		fmt.Printf("[ServiceMesh] Erro no proxy do Kubernetes: %v\n", result.Error())
		return nil, fmt.Errorf("erro ao consultar Kiali via proxy: %w", result.Error())
	}

	var graphData KialiGraphResponse
	rawData, err := result.Raw()
	if err != nil {
		fmt.Printf("[ServiceMesh] Erro ao ler dados brutos: %v\n", err)
		return nil, fmt.Errorf("erro ao ler resposta do Kiali: %w", err)
	}

	fmt.Printf("[ServiceMesh] Resposta recebida: %d bytes\n", len(rawData))

	if err := json.Unmarshal(rawData, &graphData); err != nil {
		preview := string(rawData)
		if len(preview) > 200 {
			preview = preview[:200]
		}
		fmt.Printf("[ServiceMesh] Erro ao decodificar JSON: %v\n", err)
		fmt.Printf("[ServiceMesh] Preview da resposta: %s\n", preview)
		return nil, fmt.Errorf("erro ao decodificar resposta do Kiali: %w (resposta: %s)", err, preview)
	}

	fmt.Printf("[ServiceMesh] Grafo carregado: %d nós, %d arestas\n", len(graphData.Elements.Nodes), len(graphData.Elements.Edges))
	return &graphData, nil
}

// queryKialiGraph consulta a API do Kiali para obter o service graph (método legado - mantido para compatibilidade)
func (h *ServiceMeshHandler) queryKialiGraph(kialiURL, namespace, duration, graphType, injectServiceNodes, includeIdleEdges, includeIdleNodes, appenders string) (*KialiGraphResponse, error) {
	// Construir URL da API Kiali (formato correto: /api/namespaces/graph?namespaces=X)
	url := fmt.Sprintf("%sapi/namespaces/graph?namespaces=%s&duration=%s&graphType=%s&injectServiceNodes=%s&includeIdleEdges=%s&includeIdleNodes=%s&appenders=%s",
		kialiURL, namespace, duration, graphType, injectServiceNodes, includeIdleEdges, includeIdleNodes, appenders)

	// Criar HTTP client com suporte para TLS (certificados auto-assinados)
	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true, // Certificado auto-assinado do Ingress
			},
		},
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("erro ao criar requisição: %w", err)
	}

	fmt.Printf("[ServiceMesh] Fazendo requisição para: %s\n", url)
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("[ServiceMesh] Erro na requisição: %v\n", err)
		return nil, fmt.Errorf("erro ao executar requisição: %w", err)
	}
	defer resp.Body.Close()

	fmt.Printf("[ServiceMesh] Status da resposta: %d\n", resp.StatusCode)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		fmt.Printf("[ServiceMesh] Corpo da resposta de erro: %s\n", string(body))
		return nil, fmt.Errorf("kiali retornou status %d: %s", resp.StatusCode, string(body))
	}

	var graphData KialiGraphResponse
	if err := json.NewDecoder(resp.Body).Decode(&graphData); err != nil {
		return nil, fmt.Errorf("erro ao decodificar resposta: %w", err)
	}

	return &graphData, nil
}

// queryKialiMetricsViaProxy consulta métricas via proxy do Kubernetes
func (h *ServiceMeshHandler) queryKialiMetricsViaProxy(clientset kubernetes.Interface, serviceName, serviceNamespace string, servicePort int, namespace string) (map[string]interface{}, error) {
	proxyPath := fmt.Sprintf("/api/v1/namespaces/%s/services/%s:%d/proxy/api/namespaces/%s/metrics",
		serviceNamespace, serviceName, servicePort, namespace)

	result := clientset.CoreV1().RESTClient().Get().AbsPath(proxyPath).Do(context.Background())

	if result.Error() != nil {
		return nil, fmt.Errorf("erro ao consultar métricas via proxy: %w", result.Error())
	}

	var metrics map[string]interface{}
	rawData, err := result.Raw()
	if err != nil {
		return nil, fmt.Errorf("erro ao ler resposta: %w", err)
	}

	if err := json.Unmarshal(rawData, &metrics); err != nil {
		return nil, fmt.Errorf("erro ao decodificar resposta: %w", err)
	}

	return metrics, nil
}

// queryKialiMetrics consulta métricas agregadas via Kiali (método legado)
func (h *ServiceMeshHandler) queryKialiMetrics(kialiURL, namespace string) (map[string]interface{}, error) {
	// URL para métricas do namespace
	url := fmt.Sprintf("%s/api/namespaces/%s/metrics", kialiURL, namespace)

	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("erro ao criar requisição: %w", err)
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
			ID:       edge.Data.ID,
			Source:   edge.Data.Source,
			Target:   edge.Data.Target,
			Protocol: edge.Data.Traffic.Protocol,
		}

		// Request Rate (HTTP ou TCP)
		if edge.Data.Traffic.Rates.HTTP != "" {
			simpleEdge.RequestRate = edge.Data.Traffic.Rates.HTTP
		} else if edge.Data.Traffic.Rates.TCP != "" {
			simpleEdge.RequestRate = edge.Data.Traffic.Rates.TCP
		}

		// Response Time (se disponível no Kiali)
		if edge.Data.ResponseTime != "" {
			simpleEdge.ResponseTime = edge.Data.ResponseTime
		}

		// Calcular error rate se houver responses
		// Nota: Kiali pode não incluir essas informações dependendo da configuração

		response.Edges = append(response.Edges, simpleEdge)
	}

	return response
}
