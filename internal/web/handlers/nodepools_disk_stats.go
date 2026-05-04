package handlers

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type NodeDiskStats struct {
	NodeName string `json:"node_name"`

	// Condições K8s (sem Prometheus)
	DiskPressure   bool `json:"disk_pressure"`
	MemoryPressure bool `json:"memory_pressure"`
	PIDPressure    bool `json:"pid_pressure"`

	// Inodes (Prometheus)
	InodesTotal float64 `json:"inodes_total"`
	InodesFree  float64 `json:"inodes_free"`
	InodesPct   float64 `json:"inodes_pct"` // % usados

	// I/O (taxa 5m — Prometheus)
	ReadBytesPerSec  float64 `json:"read_bytes_per_sec"`
	WriteBytesPerSec float64 `json:"write_bytes_per_sec"`
	IOUtilPct        float64 `json:"io_util_pct"` // % tempo em I/O

	PrometheusAvailable bool   `json:"prometheus_available"`
	Error               string `json:"error,omitempty"`
}

// GetNodeDiskStats retorna condições K8s + inodes + I/O por node do pool.
func (h *NodePoolHandler) GetNodeDiskStats(c *gin.Context) {
	cluster := strings.TrimSpace(c.Query("cluster"))
	nodepool := strings.TrimSpace(c.Query("nodepool"))
	if cluster == "" || nodepool == "" {
		c.JSON(400, gin.H{"error": "cluster e nodepool obrigatórios"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	clientset, err := h.kubeManager.GetClient(cluster)
	if err != nil {
		c.JSON(500, gin.H{"error": fmt.Sprintf("cluster inacessível: %v", err)})
		return
	}

	nodeList, err := clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("agentpool=%s", nodepool),
	})
	if err != nil || len(nodeList.Items) == 0 {
		nodeList, _ = clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{
			LabelSelector: fmt.Sprintf("kubernetes.azure.com/agentpool=%s", nodepool),
		})
	}

	// Mapa IP → nome do node e nome → node (para match com labels do Prometheus)
	ipToName := make(map[string]string, len(nodeList.Items))
	for _, node := range nodeList.Items {
		for _, addr := range node.Status.Addresses {
			if addr.Type == corev1.NodeInternalIP && addr.Address != "" {
				ipToName[addr.Address] = node.Name
			}
		}
	}

	// Condições K8s (sempre disponíveis)
	conditions := make(map[string]NodeDiskStats, len(nodeList.Items))
	for _, node := range nodeList.Items {
		s := NodeDiskStats{NodeName: node.Name}
		for _, cond := range node.Status.Conditions {
			if cond.Status != corev1.ConditionTrue {
				continue
			}
			switch cond.Type {
			case corev1.NodeDiskPressure:
				s.DiskPressure = true
			case corev1.NodeMemoryPressure:
				s.MemoryPressure = true
			case corev1.NodePIDPressure:
				s.PIDPressure = true
			}
		}
		conditions[node.Name] = s
	}

	prom, promErr := h.getPromClient(cluster)
	promAvailable := promErr == nil

	// Sem Prometheus — retorna só condições K8s
	if !promAvailable {
		result := make([]NodeDiskStats, 0, len(nodeList.Items))
		for _, node := range nodeList.Items {
			s := conditions[node.Name]
			s.PrometheusAvailable = false
			result = append(result, s)
		}
		c.JSON(200, gin.H{"nodes": result, "total": len(result), "prometheus_available": false})
		return
	}

	// Prometheus disponível — sem filtro de instance; match feito no Go por IP ou hostname
	// (o label instance pode ser "IP:9100", "IP" ou hostname dependendo do scrape config)
	extractHost := func(instance string) string {
		if idx := strings.LastIndex(instance, ":"); idx > 0 {
			return instance[:idx]
		}
		return instance
	}

	matchNodeName := func(instance string) string {
		host := extractHost(instance)
		if name, ok := ipToName[host]; ok {
			return name
		}
		// Fallback: o próprio hostname pode ser o nome do node K8s
		for _, node := range nodeList.Items {
			if node.Name == host || strings.HasPrefix(node.Name, host) || strings.HasPrefix(host, node.Name) {
				return node.Name
			}
		}
		return ""
	}

	parseVal := func(v []interface{}) float64 {
		if len(v) < 2 {
			return 0
		}
		s, ok := v[1].(string)
		if !ok {
			return 0
		}
		f, _ := strconv.ParseFloat(s, 64)
		return f
	}

	type nodeMetrics struct {
		inodesTotal      float64
		inodesFree       float64
		readBytesPerSec  float64
		writeBytesPerSec float64
		ioUtilPct        float64
	}
	metrics := make(map[string]*nodeMetrics, len(nodeList.Items))
	for _, node := range nodeList.Items {
		metrics[node.Name] = &nodeMetrics{}
	}

	// Inodes
	const qInodesTotal = `node_filesystem_files{mountpoint="/",fstype!="tmpfs",fstype!="overlay",fstype!="rootfs"}`
	const qInodesFree = `node_filesystem_files_free{mountpoint="/",fstype!="tmpfs",fstype!="overlay",fstype!="rootfs"}`

	if r, qErr := prom.Query(ctx, qInodesTotal); qErr == nil {
		for _, res := range r.Data.Result {
			name := matchNodeName(res.Metric["instance"])
			if m, ok := metrics[name]; ok {
				m.inodesTotal = parseVal(res.Value)
			}
		}
	}
	if r, qErr := prom.Query(ctx, qInodesFree); qErr == nil {
		for _, res := range r.Data.Result {
			name := matchNodeName(res.Metric["instance"])
			if m, ok := metrics[name]; ok {
				m.inodesFree = parseVal(res.Value)
			}
		}
	}

	// I/O rates — agrupados por instance para somar todos os discos do node
	const qRead = `sum by (instance) (rate(node_disk_read_bytes_total[5m]))`
	const qWrite = `sum by (instance) (rate(node_disk_write_bytes_total[5m]))`
	const qUtil = `sum by (instance) (rate(node_disk_io_time_seconds_total[5m])) * 100`

	if r, qErr := prom.Query(ctx, qRead); qErr == nil {
		for _, res := range r.Data.Result {
			name := matchNodeName(res.Metric["instance"])
			if m, ok := metrics[name]; ok {
				m.readBytesPerSec = parseVal(res.Value)
			}
		}
	}
	if r, qErr := prom.Query(ctx, qWrite); qErr == nil {
		for _, res := range r.Data.Result {
			name := matchNodeName(res.Metric["instance"])
			if m, ok := metrics[name]; ok {
				m.writeBytesPerSec = parseVal(res.Value)
			}
		}
	}
	if r, qErr := prom.Query(ctx, qUtil); qErr == nil {
		for _, res := range r.Data.Result {
			name := matchNodeName(res.Metric["instance"])
			if m, ok := metrics[name]; ok {
				m.ioUtilPct = parseVal(res.Value)
			}
		}
	}

	result := make([]NodeDiskStats, 0, len(nodeList.Items))
	for _, node := range nodeList.Items {
		s := conditions[node.Name]
		s.PrometheusAvailable = true
		if m, ok := metrics[node.Name]; ok {
			s.InodesTotal = m.inodesTotal
			s.InodesFree = m.inodesFree
			if m.inodesTotal > 0 {
				s.InodesPct = (1 - m.inodesFree/m.inodesTotal) * 100
			}
			s.ReadBytesPerSec = m.readBytesPerSec
			s.WriteBytesPerSec = m.writeBytesPerSec
			s.IOUtilPct = m.ioUtilPct
		}
		result = append(result, s)
	}

	c.JSON(200, gin.H{"nodes": result, "total": len(result), "prometheus_available": true})
}

// parsePromScalar extrai o valor float64 de um resultado escalar Prometheus [timestamp, "value"].
func parsePromScalar(val []interface{}) float64 {
	if len(val) < 2 {
		return 0
	}
	s, ok := val[1].(string)
	if !ok {
		return 0
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return f
}
