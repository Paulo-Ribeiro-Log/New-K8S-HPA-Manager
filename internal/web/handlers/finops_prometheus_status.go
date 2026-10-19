package handlers

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"

	"k8s-hpa-manager/internal/finops"
	"k8s-hpa-manager/internal/monitoring/discovery"
)

// Checagem de presença do Prometheus antes da análise: uma consulta constante (`vector(1)`) diz se o endpoint
// do cluster serve a API do Prometheus. Sem ela, um host inexistente (ingress devolvendo 404, caso
// dos clusters hlg) só aparecia depois do scan, como dezenas de consultas com erro.

const (
	promStatusTimeout = 15 * time.Second
	promStatusTTL     = 2 * time.Minute
)

type promStatus struct {
	Available bool      `json:"available"`
	URL       string    `json:"url"`
	Reason    string    `json:"reason,omitempty"`
	CheckedAt time.Time `json:"checked_at"`
}

// promStatusReason traduz o erro da consulta de teste em um motivo legível.
func promStatusReason(err error) string {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "404"):
		return "o endereço responde HTTP 404: não serve a API do Prometheus (host padrão não publicado para o cluster ou Prometheus em outro caminho)"
	case strings.Contains(msg, "503") || strings.Contains(msg, "502"):
		return "o ingress existe, mas o Prometheus não está respondendo (HTTP 503/502: pod fora do ar — veja se está em CrashLoop/OOMKilled no namespace de monitoring)"
	case strings.Contains(msg, "deadline exceeded") || strings.Contains(msg, "Client.Timeout"):
		return "o Prometheus não respondeu em 15 s (VPN/rede ou Prometheus sobrecarregado)"
	case strings.Contains(msg, "no such host"):
		return "o host do Prometheus não existe no DNS (VPN desconectada ou host não publicado)"
	case strings.Contains(msg, "401") || strings.Contains(msg, "403"):
		return "o Prometheus recusou a autenticação (" + msg + ")"
	default:
		return msg
	}
}

// probePrometheus consulta `vector(1)` no endpoint e guarda o resultado (cache curto, só para a rota de
// status — o scan sempre testa de novo).
func (h *FinOpsHandler) probePrometheus(ctx context.Context, url string, requiresGCPAuth bool) promStatus {
	st := promStatus{URL: url, CheckedAt: time.Now()}
	if url == "" {
		st.Reason = "nenhum endereço de Prometheus resolvido para o cluster (falha no port-forward configurado?)"
	} else if e, err := finops.NewPrometheusEnricher(url, 1, requiresGCPAuth); err != nil {
		st.Reason = err.Error()
	} else {
		pctx, cancel := context.WithTimeout(ctx, promStatusTimeout)
		defer cancel()
		if err := e.Probe(pctx); err != nil {
			st.Reason = promStatusReason(err)
		} else {
			st.Available = true
		}
	}
	h.promStatusMu.Lock()
	if h.promStatusCache == nil {
		h.promStatusCache = map[string]promStatus{}
	}
	h.promStatusCache[url] = st
	h.promStatusMu.Unlock()
	if !st.Available {
		log.Warn().Str("prometheus_url", url).Str("motivo", st.Reason).Msg("FinOps: Prometheus indisponível")
	}
	return st
}

// GetPrometheusStatus godoc
// GET /api/v1/finops/prometheus-status?cluster=X[&refresh=true]
func (h *FinOpsHandler) GetPrometheusStatus(c *gin.Context) {
	cluster := c.Query("cluster")
	if cluster == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "parâmetro 'cluster' é obrigatório"})
		return
	}
	url := discovery.GetPrometheusURL(cluster)
	if c.Query("refresh") != "true" {
		h.promStatusMu.Lock()
		cached, ok := h.promStatusCache[url]
		h.promStatusMu.Unlock()
		if ok && time.Since(cached.CheckedAt) < promStatusTTL {
			c.JSON(http.StatusOK, cached)
			return
		}
	}
	c.JSON(http.StatusOK, h.probePrometheus(c.Request.Context(), url, discovery.RequiresGCPAuth(cluster)))
}
