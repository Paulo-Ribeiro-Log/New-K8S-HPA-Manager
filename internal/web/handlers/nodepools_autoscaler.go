package handlers

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type AutoscalerStatus struct {
	Available  bool                   `json:"available"`
	Health     string                 `json:"health"`
	ScaleUp    string                 `json:"scale_up"`
	ScaleDown  string                 `json:"scale_down"`
	NodeGroups []AutoscalerNodeGroup  `json:"node_groups"`
	FetchedAt  string                 `json:"fetched_at"`
}

type AutoscalerNodeGroup struct {
	Name      string `json:"name"`
	Health    string `json:"health"`
	ScaleUp   string `json:"scale_up"`
	ScaleDown string `json:"scale_down"`
	Min       int    `json:"min"`
	Max       int    `json:"max"`
	Current   int    `json:"current"`
}

// GetAutoscalerStatus lê o ConfigMap cluster-autoscaler-status do kube-system.
func (h *NodePoolHandler) GetAutoscalerStatus(c *gin.Context) {
	cluster := strings.TrimSpace(c.Query("cluster"))
	if cluster == "" {
		c.JSON(400, gin.H{"error": "cluster obrigatório"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	client, err := h.kubeManager.GetClient(cluster)
	if err != nil {
		c.JSON(500, gin.H{"error": fmt.Sprintf("cluster inacessível: %v", err)})
		return
	}

	cm, err := client.CoreV1().ConfigMaps("kube-system").Get(ctx, "cluster-autoscaler-status", metav1.GetOptions{})
	if err != nil {
		c.JSON(200, AutoscalerStatus{
			Available: false,
			Health:    "cluster-autoscaler-status não encontrado (autoscaler pode não estar ativo)",
			FetchedAt: time.Now().UTC().Format(time.RFC3339),
		})
		return
	}

	status := parseAutoscalerStatus(cm.Data["status"])
	status.Available = true
	status.FetchedAt = time.Now().UTC().Format(time.RFC3339)
	c.JSON(200, status)
}

var (
	reASHealth    = regexp.MustCompile(`(?m)^Health:\s+(.+)`)
	reASScaleUp   = regexp.MustCompile(`(?m)^ScaleUp:\s*(.+)`)
	reASScaleDown = regexp.MustCompile(`(?m)^ScaleDown:\s*(.+)`)
	reASNGName    = regexp.MustCompile(`Name:\s+(\S+)\s+\(min:\s*(\d+),\s*max:\s*(\d+),\s*current:\s*(\d+)\)`)
	reASNGHealth  = regexp.MustCompile(`Health:\s+(\w+)`)
	reASNGSUp     = regexp.MustCompile(`ScaleUp:\s*(\w+)`)
	reASNGSDn     = regexp.MustCompile(`ScaleDown:\s*(\w+)`)
)

func parseAutoscalerStatus(raw string) AutoscalerStatus {
	s := AutoscalerStatus{}

	if m := reASHealth.FindStringSubmatch(raw); len(m) > 1 {
		s.Health = strings.TrimSpace(m[1])
	}
	if m := reASScaleUp.FindStringSubmatch(raw); len(m) > 1 {
		s.ScaleUp = strings.TrimSpace(m[1])
	}
	if m := reASScaleDown.FindStringSubmatch(raw); len(m) > 1 {
		s.ScaleDown = strings.TrimSpace(m[1])
	}

	if idx := strings.Index(raw, "NodeGroups:"); idx >= 0 {
		for _, part := range strings.Split(raw[idx:], "Name:")[1:] {
			part = "Name:" + part
			ng := AutoscalerNodeGroup{}
			if m := reASNGName.FindStringSubmatch(part); len(m) > 4 {
				ng.Name = m[1]
				ng.Min, _ = strconv.Atoi(m[2])
				ng.Max, _ = strconv.Atoi(m[3])
				ng.Current, _ = strconv.Atoi(m[4])
			}
			if ng.Name == "" {
				continue
			}
			// Pular as primeiras linhas que podem ter o campo global re-encontrado
			sub := part
			if m := reASNGHealth.FindStringSubmatch(sub); len(m) > 1 {
				ng.Health = strings.TrimSpace(m[1])
			}
			if m := reASNGSUp.FindStringSubmatch(sub); len(m) > 1 {
				ng.ScaleUp = strings.TrimSpace(m[1])
			}
			if m := reASNGSDn.FindStringSubmatch(sub); len(m) > 1 {
				ng.ScaleDown = strings.TrimSpace(m[1])
			}
			s.NodeGroups = append(s.NodeGroups, ng)
		}
	}

	return s
}
