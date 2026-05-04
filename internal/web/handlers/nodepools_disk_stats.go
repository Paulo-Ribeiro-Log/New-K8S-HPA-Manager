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

	prom, promErr := h.getPromClient(cluster)
	promAvailable := promErr == nil

	result := make([]NodeDiskStats, 0, len(nodeList.Items))
	for _, node := range nodeList.Items {
		s := NodeDiskStats{
			NodeName:            node.Name,
			PrometheusAvailable: promAvailable,
		}

		// ── Condições K8s ────────────────────────────────────────────────────
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

		if !promAvailable {
			result = append(result, s)
			continue
		}

		// ── Prometheus ───────────────────────────────────────────────────────
		nodeIP, ipErr := getNodeInternalIP(ctx, clientset, node.Name)
		if ipErr != nil {
			result = append(result, s)
			continue
		}
		inst := strings.ReplaceAll(nodeIP+":9100", ".", `\.`)

		// Inodes
		qInodesTotal := fmt.Sprintf(`node_filesystem_files{instance=~"%s",mountpoint="/",fstype!="tmpfs"}`, inst)
		qInodesFree := fmt.Sprintf(`node_filesystem_files_free{instance=~"%s",mountpoint="/",fstype!="tmpfs"}`, inst)

		if r, qErr := prom.Query(ctx, qInodesTotal); qErr == nil && len(r.Data.Result) > 0 {
			s.InodesTotal = parsePromScalar(r.Data.Result[0].Value)
		}
		if r, qErr := prom.Query(ctx, qInodesFree); qErr == nil && len(r.Data.Result) > 0 {
			s.InodesFree = parsePromScalar(r.Data.Result[0].Value)
		}
		if s.InodesTotal > 0 {
			s.InodesPct = (1 - s.InodesFree/s.InodesTotal) * 100
		}

		// I/O rates (5m)
		qRead := fmt.Sprintf(`sum(rate(node_disk_read_bytes_total{instance=~"%s"}[5m]))`, inst)
		qWrite := fmt.Sprintf(`sum(rate(node_disk_write_bytes_total{instance=~"%s"}[5m]))`, inst)
		qUtil := fmt.Sprintf(`sum(rate(node_disk_io_time_seconds_total{instance=~"%s"}[5m])) * 100`, inst)

		if r, qErr := prom.Query(ctx, qRead); qErr == nil && len(r.Data.Result) > 0 {
			s.ReadBytesPerSec = parsePromScalar(r.Data.Result[0].Value)
		}
		if r, qErr := prom.Query(ctx, qWrite); qErr == nil && len(r.Data.Result) > 0 {
			s.WriteBytesPerSec = parsePromScalar(r.Data.Result[0].Value)
		}
		if r, qErr := prom.Query(ctx, qUtil); qErr == nil && len(r.Data.Result) > 0 {
			s.IOUtilPct = parsePromScalar(r.Data.Result[0].Value)
		}

		result = append(result, s)
	}

	c.JSON(200, gin.H{"nodes": result, "total": len(result), "prometheus_available": promAvailable})
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
