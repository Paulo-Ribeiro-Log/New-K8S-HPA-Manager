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
			// Responses é complexo e varia, usar map genérico para não quebrar unmarshal
			Responses map[string]interface{} `json:"responses,omitempty"`
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
	ID                string `json:"id"`
	Label             string `json:"label"`
	Type              string `json:"type"` // workload, service, app
	Namespace         string `json:"namespace"`
	Workload          string `json:"workload,omitempty"`
	App               string `json:"app,omitempty"`
	Version           string `json:"version,omitempty"`
	Service           string `json:"service,omitempty"`
	IsRoot            bool   `json:"isRoot,omitempty"`
	IsInaccessible    bool   `json:"isInaccessible,omitempty"`
	IsOutside         bool   `json:"isOutside,omitempty"`
	RequestRate       string `json:"requestRate,omitempty"`
	ErrorRate         string `json:"errorRate,omitempty"`
	HasSidecar        bool   `json:"hasSidecar"`        // Missing Sidecars
	HasVirtualService bool   `json:"hasVirtualService"` // Virtual Services
	MtlsEnabled       bool   `json:"mtlsEnabled"`       // Security (mTLS)
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

	// Verificar se Kiali via Ingress requer autenticação
	kialiURL, needsAuth, err := getKialiURL(clusterName)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error":   "Kiali não acessível",
			"details": fmt.Sprintf("Não foi possível acessar o Kiali via Ingress: %v", err),
			"hint":    "Verifique se o Kiali está configurado e acessível",
		})
		return
	}

	// Se o Kiali requer autenticação (token strategy), criar token automaticamente
	if needsAuth {
		fmt.Printf("[ServiceMesh] 🔐 Kiali configurado com auth:token, criando token automaticamente...\n")

		// Obter clientset
		clientset, err := h.kubeManager.GetClient(clusterName)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("cluster inválido: %v", err)})
			return
		}

		// Criar token do service account kiali
		token, err := getOrCreateKialiToken(clientset, clusterName, "istio-system")
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Erro ao criar token",
				"details": err.Error(),
			})
			return
		}

		fmt.Printf("[ServiceMesh] ✅ Token criado com sucesso\n")

		// Autenticar no Kiali via POST /api/authenticate (recebe cookies de sessão)
		authenticatedClient, err := authenticateKiali(kialiURL, token)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error":   "Erro na autenticação do Kiali",
				"details": err.Error(),
			})
			return
		}

		fmt.Printf("[ServiceMesh] ✅ Autenticado com sucesso, usando sessão com cookies\n")

		// Consultar usando client autenticado com cookies de sessão
		graphData, err := h.queryKialiGraphWithClient(authenticatedClient, kialiURL, namespace, duration, graphType, injectServiceNodes, includeIdleEdges, includeIdleNodes, appenders)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Erro ao consultar Kiali",
				"details": err.Error(),
			})
			return
		}

		fmt.Printf("[ServiceMesh] ✅ Dados recebidos: %d nodes, %d edges\n", len(graphData.Elements.Nodes), len(graphData.Elements.Edges))

		// Obter clientset para verificações adicionais
		var clientsetForChecks kubernetes.Interface
		clientsetForChecks, err = h.kubeManager.GetClient(clusterName)
		if err == nil {
			response := h.simplifyGraphData(graphData, clientsetForChecks, namespace)
			c.JSON(http.StatusOK, response)
		} else {
			// Fallback: retornar sem verificações de sidecar/mTLS
			response := h.simplifyGraphData(graphData, nil, namespace)
			c.JSON(http.StatusOK, response)
		}
		return
	}

	// Kiali com autenticação anonymous, usar URL externa sem token
	fmt.Printf("[ServiceMesh] ℹ️  Kiali configurado com auth:anonymous, usando URL externa SEM token\n")

	// Consultar via HTTP direto (sem autenticação)
	graphData, err := h.queryKialiGraph(kialiURL, namespace, duration, graphType, injectServiceNodes, includeIdleEdges, includeIdleNodes, appenders, "")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Erro ao consultar Kiali",
			"details": err.Error(),
		})
		return
	}

	fmt.Printf("[ServiceMesh] ✅ Dados recebidos: %d nodes, %d edges\n", len(graphData.Elements.Nodes), len(graphData.Elements.Edges))

	// Obter clientset para verificações adicionais
	var clientsetForChecks kubernetes.Interface
	clientsetForChecks, err = h.kubeManager.GetClient(clusterName)
	if err == nil {
		response := h.simplifyGraphData(graphData, clientsetForChecks, namespace)
		c.JSON(http.StatusOK, response)
	} else {
		// Fallback: retornar sem verificações de sidecar/mTLS
		response := h.simplifyGraphData(graphData, nil, namespace)
		c.JSON(http.StatusOK, response)
	}
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
func (h *ServiceMeshHandler) queryKialiGraphViaProxy(clientset kubernetes.Interface, serviceName, serviceNamespace string, servicePort int, namespace, duration, graphType, injectServiceNodes, includeIdleEdges, includeIdleNodes, appenders, authToken string) (*KialiGraphResponse, error) {
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
	req := clientset.CoreV1().RESTClient().Get().AbsPath(proxyPath)

	// Adicionar token de autenticação se fornecido
	if authToken != "" {
		req.SetHeader("Authorization", fmt.Sprintf("Bearer %s", authToken))
		fmt.Printf("[ServiceMesh] Usando autenticação com token no proxy\n")
	}

	result := req.Do(context.Background())

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
func (h *ServiceMeshHandler) queryKialiGraph(kialiURL, namespace, duration, graphType, injectServiceNodes, includeIdleEdges, includeIdleNodes, appenders, authToken string) (*KialiGraphResponse, error) {
	// Construir URL da API Kiali (formato correto: /api/namespaces/graph?namespaces=X)
	url := fmt.Sprintf("%sapi/namespaces/graph?namespaces=%s&duration=%s&graphType=%s&injectServiceNodes=%s&includeIdleEdges=%s&includeIdleNodes=%s&appenders=%s",
		kialiURL, namespace, duration, graphType, injectServiceNodes, includeIdleEdges, includeIdleNodes, appenders)

	fmt.Printf("[ServiceMesh] 🌐 URL COMPLETA que será chamada:\n%s\n", url)
	fmt.Printf("[ServiceMesh] 📋 Parâmetros da query:\n")
	fmt.Printf("  - namespace: %s\n", namespace)
	fmt.Printf("  - duration: %s\n", duration)
	fmt.Printf("  - graphType: %s\n", graphType)
	fmt.Printf("  - injectServiceNodes: %s\n", injectServiceNodes)
	fmt.Printf("  - includeIdleEdges: %s\n", includeIdleEdges)
	fmt.Printf("  - includeIdleNodes: %s\n", includeIdleNodes)
	fmt.Printf("  - appenders: %s\n", appenders)

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

	// Adicionar token de autenticação se fornecido
	if authToken != "" {
		tokenPreview := authToken
		if len(authToken) > 20 {
			tokenPreview = authToken[:20] + "..."
		}
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", authToken))
		fmt.Printf("[ServiceMesh] 🔐 Usando autenticação com token (início: %s)\n", tokenPreview)
	} else {
		fmt.Printf("[ServiceMesh] ℹ️ Requisição sem token (autenticação anonymous)\n")
	}

	fmt.Printf("[ServiceMesh] 📡 Fazendo requisição para: %s\n", url)
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

// queryKialiGraphWithClient consulta a API do Kiali usando client HTTP pré-autenticado
func (h *ServiceMeshHandler) queryKialiGraphWithClient(client *http.Client, kialiURL, namespace, duration, graphType, injectServiceNodes, includeIdleEdges, includeIdleNodes, appenders string) (*KialiGraphResponse, error) {
	// Construir URL da API Kiali
	url := fmt.Sprintf("%sapi/namespaces/graph?namespaces=%s&duration=%s&graphType=%s&injectServiceNodes=%s&includeIdleEdges=%s&includeIdleNodes=%s&appenders=%s",
		kialiURL, namespace, duration, graphType, injectServiceNodes, includeIdleEdges, includeIdleNodes, appenders)

	fmt.Printf("[ServiceMesh] 📡 Fazendo requisição autenticada para: %s\n", url)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("erro ao criar requisição: %w", err)
	}

	// O client já tem os cookies de sessão configurados
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

	fmt.Printf("[ServiceMesh] ✅ Grafo carregado: %d nós, %d arestas\n", len(graphData.Elements.Nodes), len(graphData.Elements.Edges))
	return &graphData, nil
}

// queryKialiViaPodProxy acessa o Kiali diretamente via pod proxy, contornando autenticação do Ingress
func (h *ServiceMeshHandler) queryKialiViaPodProxy(clientset kubernetes.Interface, namespace, duration, graphType, injectServiceNodes, includeIdleEdges, includeIdleNodes, appenders string) (*KialiGraphResponse, error) {
	ctx := context.Background()

	// Buscar pod do Kiali no namespace istio-system
	fmt.Printf("[ServiceMesh] 🔍 Procurando pod do Kiali...\n")
	pods, err := clientset.CoreV1().Pods("istio-system").List(ctx, metav1.ListOptions{
		LabelSelector: "app=kiali",
		Limit:         1,
	})

	if err != nil || len(pods.Items) == 0 {
		return nil, fmt.Errorf("pod do Kiali não encontrado: %w", err)
	}

	podName := pods.Items[0].Name
	podNamespace := pods.Items[0].Namespace
	fmt.Printf("[ServiceMesh] ✅ Pod encontrado: %s/%s\n", podNamespace, podName)

	// Construir path para proxy do pod
	// Formato: /api/v1/namespaces/{namespace}/pods/{pod}/proxy/{path}
	proxyPath := fmt.Sprintf("/api/v1/namespaces/%s/pods/%s/proxy/api/namespaces/graph",
		podNamespace, podName)

	queryParams := fmt.Sprintf("?namespaces=%s&duration=%s&graphType=%s&injectServiceNodes=%s&includeIdleEdges=%s&includeIdleNodes=%s&appenders=%s",
		namespace, duration, graphType, injectServiceNodes, includeIdleEdges, includeIdleNodes, appenders)
	proxyPath += queryParams

	fmt.Printf("[ServiceMesh] 📡 Proxy path: %s\n", proxyPath)

	// Fazer requisição via REST client do Kubernetes
	result := clientset.CoreV1().RESTClient().Get().AbsPath(proxyPath).Do(ctx)

	if result.Error() != nil {
		fmt.Printf("[ServiceMesh] ❌ Erro no proxy do pod: %v\n", result.Error())
		return nil, fmt.Errorf("erro ao consultar Kiali via pod proxy: %w", result.Error())
	}

	// Ler resposta
	rawData, err := result.Raw()
	if err != nil {
		fmt.Printf("[ServiceMesh] ❌ Erro ao ler resposta: %v\n", err)
		return nil, fmt.Errorf("erro ao ler resposta do Kiali: %w", err)
	}

	fmt.Printf("[ServiceMesh] ✅ Resposta recebida: %d bytes\n", len(rawData))

	// Decodificar JSON
	var graphData KialiGraphResponse
	if err := json.Unmarshal(rawData, &graphData); err != nil {
		preview := string(rawData)
		if len(preview) > 200 {
			preview = preview[:200]
		}
		fmt.Printf("[ServiceMesh] ❌ Erro ao decodificar JSON: %v\n", err)
		fmt.Printf("[ServiceMesh] Preview: %s\n", preview)
		return nil, fmt.Errorf("erro ao decodificar resposta: %w", err)
	}

	fmt.Printf("[ServiceMesh] ✅ Grafo carregado: %d nós, %d arestas\n", len(graphData.Elements.Nodes), len(graphData.Elements.Edges))
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
func (h *ServiceMeshHandler) simplifyGraphData(graphData *KialiGraphResponse, clientset kubernetes.Interface, namespace string) *ServiceGraphResponse {
	response := &ServiceGraphResponse{
		Nodes:     make([]SimplifiedNode, 0),
		Edges:     make([]SimplifiedEdge, 0),
		Timestamp: graphData.Timestamp,
		Duration:  graphData.Duration,
	}

	fmt.Printf("[ServiceMesh] 📊 Simplificando dados: %d nós, %d arestas do Kiali\n", len(graphData.Elements.Nodes), len(graphData.Elements.Edges))

	// Verificar mTLS para o namespace (uma vez só)
	mtlsEnabled := false
	if clientset != nil {
		ctx := context.Background()
		mtlsEnabled = h.checkMTLS(ctx, clientset, namespace)
		fmt.Printf("[ServiceMesh] 🔒 mTLS para namespace %s: %v\n", namespace, mtlsEnabled)
	}

	// Processar nós
	for _, node := range graphData.Elements.Nodes {
		simpleNode := SimplifiedNode{
			ID:             node.Data.ID,
			Type:           node.Data.NodeType,
			Namespace:      node.Data.Namespace,
			Workload:       node.Data.Workload,
			App:            node.Data.App,
			Version:        node.Data.Version,
			Service:        node.Data.Service,
			IsRoot:         node.Data.IsRoot,
			IsInaccessible: node.Data.IsInaccessible,
			IsOutside:      node.Data.IsOutside,
			MtlsEnabled:    mtlsEnabled, // Aplicar mTLS do namespace
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

		// Verificar sidecar e VirtualService (se clientset disponível)
		if clientset != nil {
			ctx := context.Background()

			// Verificar sidecar para workloads
			if node.Data.Workload != "" {
				simpleNode.HasSidecar = h.checkSidecar(ctx, clientset, node.Data.Namespace, node.Data.Workload)
			} else if node.Data.App != "" {
				simpleNode.HasSidecar = h.checkSidecar(ctx, clientset, node.Data.Namespace, node.Data.App)
			}

			// Verificar VirtualService para services
			if node.Data.Service != "" {
				simpleNode.HasVirtualService = h.checkVirtualService(ctx, clientset, node.Data.Namespace, node.Data.Service)
			}
		}

		response.Nodes = append(response.Nodes, simpleNode)
	}

	fmt.Printf("[ServiceMesh] ✅ Processados %d nós\n", len(response.Nodes))
	for i, node := range response.Nodes {
		fmt.Printf("  Node %d: %s (type=%s, sidecar=%v, vs=%v, mtls=%v)\n",
			i+1, node.Label, node.Type, node.HasSidecar, node.HasVirtualService, node.MtlsEnabled)
	}

	// Processar arestas
	fmt.Printf("[ServiceMesh] 🔗 Processando %d arestas do Kiali\n", len(graphData.Elements.Edges))
	for i, edge := range graphData.Elements.Edges {
		simpleEdge := SimplifiedEdge{
			ID:       edge.Data.ID,
			Source:   edge.Data.Source,
			Target:   edge.Data.Target,
			Protocol: edge.Data.Traffic.Protocol,
		}

		fmt.Printf("  Edge %d: %s -> %s (protocol=%s)\n", i+1, edge.Data.Source, edge.Data.Target, edge.Data.Traffic.Protocol)

		// Request Rate (HTTP ou TCP)
		if edge.Data.Traffic.Rates.HTTP != "" {
			simpleEdge.RequestRate = edge.Data.Traffic.Rates.HTTP
			fmt.Printf("    HTTP Rate: %s\n", edge.Data.Traffic.Rates.HTTP)
		} else if edge.Data.Traffic.Rates.TCP != "" {
			simpleEdge.RequestRate = edge.Data.Traffic.Rates.TCP
			fmt.Printf("    TCP Rate: %s\n", edge.Data.Traffic.Rates.TCP)
		}

		// Response Time (se disponível no Kiali)
		if edge.Data.ResponseTime != "" {
			simpleEdge.ResponseTime = edge.Data.ResponseTime
			fmt.Printf("    Response Time: %s\n", edge.Data.ResponseTime)
		}

		// Calcular error rate a partir dos responses
		fmt.Printf("    Responses disponíveis: %d\n", len(edge.Data.Traffic.Responses))
		if len(edge.Data.Traffic.Responses) > 0 {
			var totalRequests float64
			var errorRequests float64

			fmt.Printf("    Analisando responses:\n")
			for code, value := range edge.Data.Traffic.Responses {
				// Tentar converter valor para float64
				var count float64
				switch v := value.(type) {
				case float64:
					count = v
				case string:
					// Se for string com formato "X.XX%", remover % e converter
					fmt.Sscanf(v, "%f", &count)
				}

				fmt.Printf("      Code %s: %.2f requests\n", code, count)
				totalRequests += count

				// Códigos 4xx e 5xx são erros
				if len(code) > 0 && (code[0] == '4' || code[0] == '5') {
					errorRequests += count
					fmt.Printf("        -> É ERRO!\n")
				}
			}

			// Se encontramos códigos de erro (mesmo com count 0), considerar 100% erro
			// Isso indica que TODAS as requisições estão falhando
			if errorRequests == 0 && totalRequests == 0 {
				// Verificar se há códigos de erro mesmo sem count
				hasErrorCodes := false
				for code := range edge.Data.Traffic.Responses {
					if len(code) > 0 && (code[0] == '4' || code[0] == '5') {
						hasErrorCodes = true
						break
					}
				}
				if hasErrorCodes {
					simpleEdge.ErrorRate = 100.0
					fmt.Printf("    ⚠️  Códigos de erro detectados sem requests: assumindo 100%% erro\n")
				}
			} else if totalRequests > 0 {
				// Calcular porcentagem de erro normalmente
				errorRate := (errorRequests / totalRequests) * 100
				simpleEdge.ErrorRate = errorRate
				fmt.Printf("    ✅ Error Rate calculado: %.2f%% (%.2f errors / %.2f total)\n",
					errorRate, errorRequests, totalRequests)
			}
		} else {
			fmt.Printf("    ⚠️  Nenhum dado de responses disponível\n")
		}

		response.Edges = append(response.Edges, simpleEdge)
	}

	fmt.Printf("[ServiceMesh] ✅ Processadas %d arestas\n", len(response.Edges))

	return response
}

// checkSidecar verifica se um workload tem sidecar do Istio injetado
func (h *ServiceMeshHandler) checkSidecar(ctx context.Context, clientset kubernetes.Interface, namespace, workloadName string) bool {
	if workloadName == "" {
		return false
	}

	// Buscar pods do workload
	pods, err := clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("app=%s", workloadName),
	})
	if err != nil || len(pods.Items) == 0 {
		return false
	}

	// Verificar se algum pod tem container istio-proxy
	for _, pod := range pods.Items {
		for _, container := range pod.Spec.Containers {
			if container.Name == "istio-proxy" {
				return true
			}
		}
	}

	return false
}

// checkVirtualService verifica se existe VirtualService para um serviço
func (h *ServiceMeshHandler) checkVirtualService(ctx context.Context, clientset kubernetes.Interface, namespace, serviceName string) bool {
	if serviceName == "" {
		return false
	}

	// Por enquanto, retornar false (implementação completa requer dynamic client)
	// TODO: Implementar com dynamic client para acessar CRDs do Istio
	return false
}

// checkMTLS verifica se mTLS está habilitado para o namespace
func (h *ServiceMeshHandler) checkMTLS(ctx context.Context, clientset kubernetes.Interface, namespace string) bool {
	// Por enquanto, retornar false (implementação completa requer dynamic client)
	// TODO: Implementar com dynamic client para acessar PeerAuthentication do Istio
	return false
}
