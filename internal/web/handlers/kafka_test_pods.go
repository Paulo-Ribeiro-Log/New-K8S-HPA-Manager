package handlers

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// KafkaTestPodOption é uma opção de pod pro seletor de pod/container do Teste de Kafka.
type KafkaTestPodOption struct {
	Name       string   `json:"name"`
	Containers []string `json:"containers"`
}

// ListPodsForDeployment lista os pods Running de um Deployment, pro seletor de pod/container do
// Teste de Kafka — deixa o usuário escolher explicitamente qual pod recebe o Ephemeral Container
// em vez de sempre o primeiro Running (comportamento padrão de resolvePodForDeployment quando
// pod_name não é informado no request). Reaproveita a mesma lógica de seletor de labels de
// listRunningPodsForDeployment (kafka_test_tool.go).
// GET /api/v1/kafka-test/pods?cluster=X&namespace=Y&deployment=Z
func (h *KafkaTestHandler) ListPodsForDeployment(c *gin.Context) {
	cluster := strings.TrimSpace(c.Query("cluster"))
	namespace := strings.TrimSpace(c.Query("namespace"))
	deployment := strings.TrimSpace(c.Query("deployment"))
	if cluster == "" || namespace == "" || deployment == "" {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_PARAMS", "cluster, namespace e deployment são obrigatórios"))
		return
	}

	clientset, err := h.kubeManager.GetClient(cluster)
	if err != nil {
		c.JSON(http.StatusBadGateway, errorResponse("CLUSTER_ERROR", err.Error()))
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()

	pods, err := listRunningPodsForDeployment(ctx, clientset, namespace, deployment)
	if err != nil {
		c.JSON(http.StatusBadGateway, errorResponse("POD_LIST_ERROR", err.Error()))
		return
	}

	options := make([]KafkaTestPodOption, 0, len(pods))
	for _, pod := range pods {
		containers := make([]string, 0, len(pod.Spec.Containers))
		for _, ct := range pod.Spec.Containers {
			containers = append(containers, ct.Name)
		}
		options = append(options, KafkaTestPodOption{Name: pod.Name, Containers: containers})
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "pods": options})
}
