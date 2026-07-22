package handlers

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type NodeResourceInfo struct {
	NodeName       string  `json:"node_name"`
	CPUAllocatable int64   `json:"cpu_allocatable_m"`
	CPURequested   int64   `json:"cpu_requested_m"`
	CPUPct         float64 `json:"cpu_pct"`
	MemAllocatable int64   `json:"mem_allocatable_bytes"`
	MemRequested   int64   `json:"mem_requested_bytes"`
	MemPct         float64 `json:"mem_pct"`
	PodCount       int     `json:"pod_count"`
	PodCapacity    int     `json:"pod_capacity"`
}

// GetNodeResources retorna utilização real de CPU/Memory por node do pool, via K8s API.
func (h *NodePoolHandler) GetNodeResources(c *gin.Context) {
	cluster := strings.TrimSpace(c.Query("cluster"))
	nodepool := strings.TrimSpace(c.Query("nodepool"))
	if cluster == "" || nodepool == "" {
		c.JSON(400, gin.H{"error": "cluster e nodepool obrigatórios"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client, err := h.kubeManager.GetClient(cluster)
	if err != nil {
		c.JSON(500, gin.H{"error": fmt.Sprintf("cluster inacessível: %v", err)})
		return
	}

	allNodes, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		c.JSON(500, gin.H{"error": fmt.Sprintf("erro ao listar nodes: %v", err)})
		return
	}

	// Node pool label varia por cloud provider: AKS usa "agentpool"/"kubernetes.azure.com/agentpool",
	// EKS usa "eks.amazonaws.com/nodegroup", GKE usa "cloud.google.com/gke-nodepool" — mesmo
	// conjunto de labels já usado em GetNodesInNodePool (internal/kubernetes/client.go).
	poolLabelKeys := []string{"agentpool", "kubernetes.azure.com/agentpool", "eks.amazonaws.com/nodegroup", "cloud.google.com/gke-nodepool"}
	nodes := make([]corev1.Node, 0, len(allNodes.Items))
	for _, node := range allNodes.Items {
		for _, key := range poolLabelKeys {
			if node.Labels[key] == nodepool {
				nodes = append(nodes, node)
				break
			}
		}
	}

	result := make([]NodeResourceInfo, 0, len(nodes))
	for _, node := range nodes {
		info := NodeResourceInfo{NodeName: node.Name}

		if q, ok := node.Status.Allocatable[corev1.ResourceCPU]; ok {
			info.CPUAllocatable = q.MilliValue()
		}
		if q, ok := node.Status.Allocatable[corev1.ResourceMemory]; ok {
			info.MemAllocatable = q.Value()
		}
		if q, ok := node.Status.Allocatable[corev1.ResourcePods]; ok {
			info.PodCapacity = int(q.Value())
		}

		podList, podErr := client.CoreV1().Pods("").List(ctx, metav1.ListOptions{
			FieldSelector: fmt.Sprintf("spec.nodeName=%s", node.Name),
		})
		if podErr == nil {
			for _, pod := range podList.Items {
				if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
					continue
				}
				info.PodCount++
				for _, c := range pod.Spec.Containers {
					if q, ok := c.Resources.Requests[corev1.ResourceCPU]; ok {
						info.CPURequested += q.MilliValue()
					}
					if q, ok := c.Resources.Requests[corev1.ResourceMemory]; ok {
						info.MemRequested += q.Value()
					}
				}
			}
		}

		if info.CPUAllocatable > 0 {
			info.CPUPct = float64(info.CPURequested) / float64(info.CPUAllocatable) * 100
		}
		if info.MemAllocatable > 0 {
			info.MemPct = float64(info.MemRequested) / float64(info.MemAllocatable) * 100
		}

		result = append(result, info)
	}

	c.JSON(200, gin.H{"nodes": result, "total": len(result)})
}
