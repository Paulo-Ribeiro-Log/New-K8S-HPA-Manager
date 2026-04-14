package finops

import (
	"context"

	"github.com/rs/zerolog/log"

	"k8s-hpa-manager/internal/dynatrace"
)

// DTEnricher enriquece workloads com dados históricos reais do Dynatrace.
// É o enriquecedor primário — Prometheus atua como fallback quando DT não tem dados.
type DTEnricher struct {
	client     *dynatrace.Client
	windowDays int
}

// NewDTEnricher cria um DTEnricher. windowDays define a janela histórica (padrão 30d).
func NewDTEnricher(client *dynatrace.Client, windowDays int) *DTEnricher {
	if windowDays <= 0 {
		windowDays = 30
	}
	return &DTEnricher{client: client, windowDays: windowDays}
}

// EnrichWorkloads consulta métricas de CPU e memória no DT em batch (4 queries para
// todos os workloads) e preenche os campos de uso real. Retorna o conjunto de chaves
// "namespace/workload" que foram enriquecidas — usado pelo calculator para aplicar
// Prometheus apenas nos workloads sem dados DT.
func (e *DTEnricher) EnrichWorkloads(ctx context.Context, workloads []FinOpsWorkload) map[string]bool {
	enriched := make(map[string]bool)

	metrics, err := e.client.GetAllWorkloadMetrics(ctx, e.windowDays)
	if err != nil {
		log.Warn().Err(err).Msg("FinOps/DT: falha ao buscar métricas — sem enriquecimento DT")
		return enriched
	}
	if len(metrics) == 0 {
		log.Info().Msg("FinOps/DT: nenhuma métrica retornada (cluster sem OneAgent ou DT não configurado)")
		return enriched
	}

	const bytesPerMi = 1048576.0

	count := 0
	for i := range workloads {
		wl := &workloads[i]
		key := wl.Namespace + "/" + wl.Workload

		m, ok := metrics[key]
		if !ok || (m.CPUP95Millicores == 0 && m.MemP95Bytes == 0) {
			continue // sem dados DT para este workload → fallback para Prometheus
		}

		wl.CPUAvgMillis = round2(m.CPUAvgMillicores)
		wl.CPUP95Millis = round2(m.CPUP95Millicores)
		wl.MemAvgMi = round2(m.MemAvgBytes / bytesPerMi)
		wl.MemP95Mi = round2(m.MemP95Bytes / bytesPerMi)

		if wl.CPUP95Millis > 0 {
			wl.CPURecommendedMillis = round2(wl.CPUP95Millis * SafetyMargin)
		}
		if wl.MemP95Mi > 0 {
			wl.MemRecommendedMi = round2(wl.MemP95Mi * SafetyMargin)
		}

		wl.WasteBRL = calculateWaste(wl)
		wl.Verdict = verdictFromPrometheus(wl) // mesmas regras de verdict
		wl.MetricsSource = "dynatrace"

		enriched[key] = true
		count++
	}

	log.Info().Int("enriched", count).Int("window_days", e.windowDays).
		Msg("FinOps/DT: enriquecimento concluído")
	return enriched
}
