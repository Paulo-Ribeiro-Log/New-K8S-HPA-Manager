package handlers

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

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
// (monitorados via OneAgent ou via OpenTelemetry/Cloud Native Full Stack), para o indicador
// visual na aba Pods (painéis esquerdo e direito).
// GET /api/v1/pods/:cluster/dynatrace-status?ai_email=...
//
// cluster_supported=false cobre "Dynatrace não configurado" (nem tokens do usuário, nem env
// vars) — nesse caso "monitored" vem sempre vazio, sem chamar a API do Dynatrace. Falha ao
// consultar o Dynatrace (erro transitório) tem o mesmo efeito — falha silenciosa, não bloqueia a
// tela de pods (mesmo princípio de outras checagens best-effort do app).
//
// Bug real corrigido: antes cortava aqui para qualquer cluster que não fosse AKS, assumindo que
// só a frota AKS usa Dynatrace (EKS usaria New Relic — suposição documentada no plano de FinOps
// NR Metrics, válida pra ALGUMAS contas EKS, mas não todas). Confirmado contra um cluster EKS
// real (asaplog-production) que roda Dynatrace com OneAgent em modo Cloud Native Full Stack +
// Kubernetes API Monitoring + ingest OpenTelemetry — cluster genuinamente monitorado que sempre
// aparecia como "não suportado" só por não ser AKS. A checagem de cloud provider não tem mais
// nenhum papel aqui: ListMonitoredPods já lida com "cluster não encontrado no Dynatrace" de forma
// graciosa (retorna mapa vazio, sem erro) para QUALQUER provider — mesmo comportamento que um
// cluster AKS não onboardado no Dynatrace já tinha antes desta mudança.
func (h *PodHandler) GetDynatraceStatus(c *gin.Context) {
	cluster := c.Param("cluster")
	aiEmail := c.Query("ai_email")

	resp := gin.H{
		"cluster_supported": false,
		"monitored":         []string{},
	}

	dtc, err := h.dynatraceClientForPods(aiEmail)
	if err != nil {
		// Dynatrace não configurado (nem tokens do usuário, nem env vars) — estado "não aplicável",
		// não um erro.
		c.JSON(http.StatusOK, resp)
		return
	}

	resp["cluster_supported"] = true

	// 90s (era 20s) — ListMonitoredPods agora pagina de verdade (listEntitiesBySelectorMaxPages,
	// até 50 páginas/25.000 entidades) pra não descartar resultado além do teto antigo (bugs reais
	// confirmados: cluster AKS com 103 nós tinha 4.561 PROCESS_GROUP_INSTANCE cortadas em 500; o
	// cluster EKS asaplog-production tem 10.804 CLOUD_APPLICATION_INSTANCE, cortadas em 10.000 com
	// um teto de 20 páginas testado antes deste). Paginar mais fundo leva mais tempo — margem
	// generosa pro pior caso (cluster com bastante churn de CronJobs acumulando entidades).
	ctx, cancel := context.WithTimeout(c.Request.Context(), 90*time.Second)
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
