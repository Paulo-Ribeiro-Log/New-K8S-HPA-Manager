package handlers

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s-hpa-manager/internal/config"
)

type NetworkPolicyHandler struct {
	kubeManager *config.KubeConfigManager
}

func NewNetworkPolicyHandler(km *config.KubeConfigManager) *NetworkPolicyHandler {
	return &NetworkPolicyHandler{kubeManager: km}
}

type NetworkPolicySummary struct {
	Cluster     string   `json:"cluster"`
	Namespace   string   `json:"namespace"`
	Name        string   `json:"name"`
	PodSelector string   `json:"podSelector"`
	PolicyTypes []string `json:"policyTypes"`
	Ingress     string   `json:"ingress,omitempty"`
	Egress      string   `json:"egress,omitempty"`
}

func (h *NetworkPolicyHandler) List(c *gin.Context) {
	cluster := c.Query("cluster")
	namespaces := parseNamespaces(c.Query("namespaces"))

	if cluster == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "MISSING_PARAMETER",
				"message": "Parameter 'cluster' is required",
			},
		})
		return
	}

	clientset, err := h.kubeManager.GetClient(cluster)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "CLIENT_ERROR",
				"message": fmt.Sprintf("Failed to get client: %v", err),
			},
		})
		return
	}

	var policies []NetworkPolicySummary

	for _, ns := range namespaces {
		policyList, err := clientset.NetworkingV1().NetworkPolicies(ns).List(c.Request.Context(), metav1.ListOptions{})
		if err != nil {
			continue
		}

		for _, policy := range policyList.Items {
			policies = append(policies, buildNetworkPolicySummary(cluster, ns, policy))
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"policies": policies,
			"count":    len(policies),
		},
	})
}

func buildNetworkPolicySummary(cluster, namespace string, policy networkingv1.NetworkPolicy) NetworkPolicySummary {
	// Extrair pod selector
	podSelector := "Todos"
	if len(policy.Spec.PodSelector.MatchLabels) > 0 {
		var labels []string
		for k, v := range policy.Spec.PodSelector.MatchLabels {
			labels = append(labels, fmt.Sprintf("%s=%s", k, v))
		}
		podSelector = strings.Join(labels, ", ")
	}

	// PolicyTypes
	var policyTypes []string
	for _, pt := range policy.Spec.PolicyTypes {
		policyTypes = append(policyTypes, string(pt))
	}

	// Ingress/Egress rules count
	ingress := fmt.Sprintf("%d rules", len(policy.Spec.Ingress))
	egress := fmt.Sprintf("%d rules", len(policy.Spec.Egress))

	return NetworkPolicySummary{
		Cluster:     cluster,
		Namespace:   namespace,
		Name:        policy.Name,
		PodSelector: podSelector,
		PolicyTypes: policyTypes,
		Ingress:     ingress,
		Egress:      egress,
	}
}
