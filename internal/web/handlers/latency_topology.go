package handlers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// TopologyNode representa um nó do grafo de topologia (Fase 6.4): um cluster testado ou um alvo
// (Service K8s ou região de nuvem) já testado a partir de algum cluster.
type TopologyNode struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Kind     string `json:"kind"`     // "cluster" | "cloud_target" | "service_target"
	Provider string `json:"provider"` // "aks"|"eks"|"gke" (cluster) ou "aws"|"gcp"|"azure" (cloud_target); "" se desconhecido
}

// TopologyEdge é o resultado MAIS RECENTE de um par cluster→alvo — sem teste rodado ainda pra um
// par = sem aresta (não desenhamos aresta "desconhecido", polui o grafo à toa).
type TopologyEdge struct {
	ID            string  `json:"id"`
	Source        string  `json:"source"` // cluster
	Target        string  `json:"target"` // host normalizado (sem schema)
	Protocol      string  `json:"protocol"`
	P95Ms         float64 `json:"p95_ms"`
	P99Ms         float64 `json:"p99_ms"`
	ErrorCount    int     `json:"error_count"`
	TotalRequests int     `json:"total_requests"`
	TestedAt      string  `json:"tested_at"`
}

// targetNodeKind classifica um host normalizado como alvo de nuvem curado (Fase 6.2) ou alvo
// genérico (Service K8s testado via URL livre) — usado só pra escolher ícone/cor no frontend.
func targetNodeKind(host string) (kind, provider string) {
	for _, t := range cloudRegionTargets {
		if t.Host == host {
			return "cloud_target", t.Provider
		}
	}
	return "service_target", ""
}

// GetTopology monta o grafo agregado — nós (clusters + alvos já testados) e arestas (resultado
// mais recente de cada par cluster→alvo), lido do LatencyTestHistoryStore (Fase 6.3). Usado pela
// aba "Topologia" do LatencyTestTab.tsx, alternativa visual à tabela de histórico da sessão
// (essa agrega TODOS os testes já feitos, não só os da sessão atual do navegador).
// GET /api/v1/latency-test/topology
func (h *LatencyTestHandler) GetTopology(c *gin.Context) {
	if h.testHistory == nil {
		c.JSON(http.StatusOK, gin.H{"nodes": []TopologyNode{}, "edges": []TopologyEdge{}})
		return
	}

	records, err := h.testHistory.GetRecent(500)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorResponse("TOPOLOGY_QUERY_FAILED", err.Error()))
		return
	}

	clusterProviders := make(map[string]string)
	for _, cl := range h.kubeManager.DiscoverClusters() {
		clusterProviders[cl.Context] = cl.CloudProvider
	}

	// records vem ordenado do mais recente pro mais antigo (GetRecent) — a primeira ocorrência
	// de cada par cluster→alvo que encontramos JÁ é a mais recente, então as próximas do mesmo
	// par são descartadas.
	seenPairs := make(map[string]bool)
	clusterNodes := make(map[string]bool)
	targetHosts := make(map[string]bool)
	edges := []TopologyEdge{}

	for _, r := range records {
		host := normalizeICMPTarget(r.Target)
		pairKey := r.Cluster + "->" + host
		if seenPairs[pairKey] {
			continue
		}
		seenPairs[pairKey] = true

		clusterNodes[r.Cluster] = true
		targetHosts[host] = true

		edges = append(edges, TopologyEdge{
			ID:            pairKey,
			Source:        r.Cluster,
			Target:        host,
			Protocol:      r.Protocol,
			P95Ms:         r.P95Ms,
			P99Ms:         r.P99Ms,
			ErrorCount:    r.ErrorCount,
			TotalRequests: r.TotalRequests,
			TestedAt:      r.TestedAt.Format(time.RFC3339),
		})
	}

	nodes := make([]TopologyNode, 0, len(clusterNodes)+len(targetHosts))
	for cluster := range clusterNodes {
		nodes = append(nodes, TopologyNode{
			ID: cluster, Label: cluster, Kind: "cluster", Provider: clusterProviders[cluster],
		})
	}
	for host := range targetHosts {
		kind, provider := targetNodeKind(host)
		nodes = append(nodes, TopologyNode{ID: host, Label: host, Kind: kind, Provider: provider})
	}

	c.JSON(http.StatusOK, gin.H{"nodes": nodes, "edges": edges})
}
