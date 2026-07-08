package handlers

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gopkg.in/yaml.v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type AutoscalerStatus struct {
	Available  bool                  `json:"available"`
	Health     string                `json:"health"`
	ScaleUp    string                `json:"scale_up"`
	ScaleDown  string                `json:"scale_down"`
	NodeGroups []AutoscalerNodeGroup `json:"node_groups"`
	FetchedAt  string                `json:"fetched_at"`
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
			Available:  false,
			Health:     "cluster-autoscaler-status não encontrado (autoscaler pode não estar ativo)",
			NodeGroups: make([]AutoscalerNodeGroup, 0),
			FetchedAt:  time.Now().UTC().Format(time.RFC3339),
		})
		return
	}

	status := parseAutoscalerStatus(cm.Data["status"])
	status.Available = true
	status.FetchedAt = time.Now().UTC().Format(time.RFC3339)
	c.JSON(200, status)
}

// Estruturas para deserializar o YAML do ConfigMap (formato atual do cluster-autoscaler)
type caStatusYAML struct {
	ClusterWide struct {
		Health struct {
			Status     string `yaml:"status"`
			NodeCounts struct {
				Registered struct {
					Total int `yaml:"total"`
				} `yaml:"registered"`
			} `yaml:"nodeCounts"`
		} `yaml:"health"`
		ScaleUp struct {
			Status string `yaml:"status"`
		} `yaml:"scaleUp"`
		ScaleDown struct {
			Status string `yaml:"status"`
		} `yaml:"scaleDown"`
	} `yaml:"clusterWide"`
	NodeGroups []struct {
		Name   string `yaml:"name"`
		Health struct {
			Status     string `yaml:"status"`
			MinSize    int    `yaml:"minSize"`
			MaxSize    int    `yaml:"maxSize"`
			NodeCounts struct {
				Registered struct {
					Total int `yaml:"total"`
				} `yaml:"registered"`
			} `yaml:"nodeCounts"`
		} `yaml:"health"`
		ScaleUp struct {
			Status string `yaml:"status"`
		} `yaml:"scaleUp"`
		ScaleDown struct {
			Status string `yaml:"status"`
		} `yaml:"scaleDown"`
	} `yaml:"nodeGroups"`
}

func parseAutoscalerStatus(raw string) AutoscalerStatus {
	s := AutoscalerStatus{NodeGroups: make([]AutoscalerNodeGroup, 0)}

	var parsed caStatusYAML
	if err := yaml.Unmarshal([]byte(raw), &parsed); err == nil && parsed.ClusterWide.Health.Status != "" {
		// Formato YAML (cluster-autoscaler >= v1.28 / AKS moderno)
		s.Health = parsed.ClusterWide.Health.Status
		s.ScaleUp = parsed.ClusterWide.ScaleUp.Status
		s.ScaleDown = parsed.ClusterWide.ScaleDown.Status

		for _, ng := range parsed.NodeGroups {
			if ng.Name == "" {
				continue
			}
			s.NodeGroups = append(s.NodeGroups, AutoscalerNodeGroup{
				Name:      ng.Name,
				Health:    ng.Health.Status,
				ScaleUp:   ng.ScaleUp.Status,
				ScaleDown: ng.ScaleDown.Status,
				Min:       ng.Health.MinSize,
				Max:       ng.Health.MaxSize,
				Current:   ng.Health.NodeCounts.Registered.Total,
			})
		}
		return s
	}

	// Fallback: formato texto legado (cluster-autoscaler < v1.28)
	// Health/ScaleUp/ScaleDown podem estar indentados dentro de "Cluster-wide:"
	clusterWide := raw
	if idx := strings.Index(raw, "NodeGroups:"); idx >= 0 {
		clusterWide = raw[:idx]
	}
	extractFirst := func(text, key string) string {
		prefix := key + ":"
		for _, line := range strings.Split(text, "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, prefix) {
				val := strings.TrimSpace(trimmed[len(prefix):])
				// Pega só a primeira palavra (status)
				if sp := strings.IndexAny(val, " \t("); sp > 0 {
					return val[:sp]
				}
				return val
			}
		}
		return ""
	}
	s.Health = extractFirst(clusterWide, "Health")
	s.ScaleUp = extractFirst(clusterWide, "ScaleUp")
	s.ScaleDown = extractFirst(clusterWide, "ScaleDown")

	if idx := strings.Index(raw, "NodeGroups:"); idx >= 0 {
		for _, part := range strings.Split(raw[idx:], "Name:")[1:] {
			part = "Name:" + part
			ng := AutoscalerNodeGroup{}
			firstLine := strings.TrimSpace(strings.SplitN(part, "\n", 2)[0])
			// "Name: aks-xxx (min: 2, max: 10, current: 5)"
			if len(firstLine) > 5 {
				rest := strings.TrimPrefix(firstLine, "Name:")
				rest = strings.TrimSpace(rest)
				// separa nome dos parens
				if pi := strings.Index(rest, "("); pi > 0 {
					ng.Name = strings.TrimSpace(rest[:pi])
					parens := rest[pi:]
					fmt.Sscanf(parens, "(min: %d, max: %d, current: %d)", &ng.Min, &ng.Max, &ng.Current)
				} else {
					ng.Name = rest
				}
			}
			if ng.Name == "" {
				continue
			}
			ng.Health = extractFirst(part, "Health")
			ng.ScaleUp = extractFirst(part, "ScaleUp")
			ng.ScaleDown = extractFirst(part, "ScaleDown")
			s.NodeGroups = append(s.NodeGroups, ng)
		}
	}

	return s
}
