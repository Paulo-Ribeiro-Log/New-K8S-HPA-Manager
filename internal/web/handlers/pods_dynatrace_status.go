package handlers

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"k8s-hpa-manager/internal/config"
	dtclient "k8s-hpa-manager/internal/dynatrace"
)

// dynatraceClientForPods resolve um cliente Dynatrace para checagem de status de monitoramento.
// Mesma resolução de credenciais de DynatraceHandler.clientForUser: tokens salvos do usuário
// (se aiEmail fornecido) com fallback para env vars DT_API_URL/DT_API_TOKEN (service account).
func (h *PodHandler) dynatraceClientForPods(aiEmail string) (*dtclient.Client, error) {
	var dtURL, dtToken string

	if aiEmail != "" && h.tokensStore != nil {
		tokens, err := h.tokensStore.GetTokens(aiEmail)
		if err == nil && tokens != nil {
			dtURL = tokens.DynatraceURL
			dtToken = tokens.DynatraceToken
		}
	}

	return dtclient.NewClient(dtURL, dtToken)
}

// GetDynatraceStatus retorna quais pods de um cluster têm entidade Dynatrace correspondente
// (monitorados via OneAgent), para o indicador visual na aba Pods (painéis esquerdo e direito).
// GET /api/v1/pods/:cluster/dynatrace-status?ai_email=...
//
// cluster_supported=false cobre tanto "não é AKS" quanto "Dynatrace não configurado" — nesses
// casos "monitored" vem sempre vazio, sem chamar a API do Dynatrace. Falha ao consultar o
// Dynatrace (erro transitório) tem o mesmo efeito de cluster_supported=false — falha silenciosa,
// não bloqueia a tela de pods (mesmo princípio de outras checagens best-effort do app).
func (h *PodHandler) GetDynatraceStatus(c *gin.Context) {
	cluster := c.Param("cluster")
	aiEmail := c.Query("ai_email")

	resp := gin.H{
		"cluster_supported": false,
		"monitored":         []string{},
	}

	serverURL := h.kubeManager.GetServerURL(cluster)
	if config.DetectCloudProvider(serverURL, cluster) != config.CloudProviderAKS {
		c.JSON(http.StatusOK, resp)
		return
	}

	dtc, err := h.dynatraceClientForPods(aiEmail)
	if err != nil {
		// Dynatrace não configurado (nem tokens do usuário, nem env vars) — estado "não aplicável",
		// não um erro.
		c.JSON(http.StatusOK, resp)
		return
	}

	resp["cluster_supported"] = true

	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()

	// O nome real da entidade no Dynatrace (HOST_GROUP/KUBERNETES_CLUSTER) nunca tem o sufixo
	// "-admin" que os contexts do kubeconfig usam — sem isso, ListMonitoredPods nunca casava com
	// nada mesmo depois de corrigida a correlação em si (bug real: confirmado que
	// "akspriv-logreversa-prd-admin" retornava 0 pods, "akspriv-logreversa-prd" retornava 166).
	monitored, err := dtc.ListMonitoredPods(ctx, strings.TrimSuffix(cluster, "-admin"))
	if err != nil {
		// Falha transitória na API do Dynatrace — não bloquear a tela, mas o cluster É suportado
		// (cluster_supported continua true), só sem dados de monitoramento nesta chamada.
		c.JSON(http.StatusOK, resp)
		return
	}

	keys := make([]string, 0, len(monitored))
	for key := range monitored {
		keys = append(keys, key)
	}
	resp["monitored"] = keys

	c.JSON(http.StatusOK, resp)
}
