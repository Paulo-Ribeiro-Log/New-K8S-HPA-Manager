package finops

import (
	"context"
	"strings"

	"github.com/rs/zerolog/log"

	"k8s-hpa-manager/internal/dynatrace"
)

// DTEnricher enriquece workloads com dados históricos reais do Dynatrace.
// É o enriquecedor primário — Prometheus atua como fallback quando DT não tem dados.
type DTEnricher struct {
	client     *dynatrace.Client
	windowDays int
	// cluster — nome "limpo" (sem "-admin"), usado pra escopar as queries DT a ESTE cluster —
	// ver bug real corrigido em internal/dynatrace/finops_metrics.go: sem esse escopo, um tenant
	// DT compartilhado entre toda a frota (o caso real desta empresa) agrega/soma a métrica de
	// qualquer workload cujo namespace+nome se repete em outros clusters, silenciosamente.
	cluster string
}

// NewDTEnricher cria um DTEnricher. windowDays define a janela histórica (padrão 30d).
// cluster: nome do cluster (com ou sem sufixo "-admin", normalizado aqui) — sempre obrigatório
// pra escopar as queries DT a este cluster específico.
func NewDTEnricher(client *dynatrace.Client, windowDays int, cluster string) *DTEnricher {
	if windowDays <= 0 {
		windowDays = 30
	}
	return &DTEnricher{client: client, windowDays: windowDays, cluster: strings.TrimSuffix(cluster, "-admin")}
}

// EnrichWorkloads consulta métricas de CPU e memória no DT em batch (4 queries para
// todos os workloads) e preenche os campos de uso real. Retorna o conjunto de chaves
// "namespace/workload" que foram enriquecidas — usado pelo calculator para aplicar
// Prometheus apenas nos workloads sem dados DT.
func (e *DTEnricher) EnrichWorkloads(ctx context.Context, workloads []FinOpsWorkload) map[string]bool {
	enriched := make(map[string]bool)

	metrics, err := e.client.GetAllWorkloadMetrics(ctx, e.windowDays, e.cluster)
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

		// Bug real corrigido: o gate antigo exigia CPUP95Millicores/MemP95Bytes > 0 — mas essa
		// família de métricas do DT (builtin:kubernetes.workload.*) NUNCA supre percentile neste
		// tenant (ver comentário em finops_metrics.go), então os dois campos são sempre 0 e o
		// gate antigo descartava TODO workload, mesmo com avg/max reais disponíveis — o
		// enriquecimento DT virava um no-op silencioso. Gate corrigido pra usar avg (sempre
		// populado quando o workload tem dado real).
		m, ok := metrics[key]
		if !ok || (m.CPUAvgMillicores == 0 && m.MemAvgBytes == 0) {
			continue // sem dados DT para este workload → fallback para Prometheus
		}

		wl.CPUAvgMillis = round2(m.CPUAvgMillicores)
		wl.CPUP95Millis = round2(m.CPUP95Millicores) // sempre 0 pro DT — nunca inventado, ver acima
		wl.CPUMaxMillis = round2(m.CPUMaxMillicores)
		wl.MemAvgMi = round2(m.MemAvgBytes / bytesPerMi)
		wl.MemP95Mi = round2(m.MemP95Bytes / bytesPerMi) // sempre 0 pro DT — nunca inventado, ver acima
		wl.MemMaxMi = round2(m.MemMaxBytes / bytesPerMi)

		// Base do Request recomendado: P95 quando disponível (não é o caso do DT nesta versão,
		// mas mantém a mesma regra do enriquecimento Prometheus se algum dia o DT passar a
		// suportar percentile); na ausência de P95, cai pro avg × margem — nunca inventa um valor
		// intermediário/"P95 sintético" (o campo CPUP95Millis/MemP95Mi continua honestamente 0,
		// nunca populado com avg disfarçado). Semântica defensável: Request deve refletir uso
		// típico/estável, não pico — Limit (abaixo) é quem protege contra spike, e esse já usa o
		// pico real (MemMaxMi) independente de ter P95 ou não.
		cpuRecBasis := wl.CPUP95Millis
		if cpuRecBasis == 0 {
			cpuRecBasis = wl.CPUAvgMillis
		}
		memRecBasis := wl.MemP95Mi
		if memRecBasis == 0 {
			memRecBasis = wl.MemAvgMi
		}
		if cpuRecBasis > 0 {
			wl.CPURecommendedMillis = round2(cpuRecBasis * SafetyMargin)
		}
		if memRecBasis > 0 {
			wl.MemRecommendedMi = round2(memRecBasis * SafetyMargin)
		}

		wl.CPULimitRecommendedMillis, wl.MemLimitRecommendedMi = recommendedLimits(
			wl.CPURecommendedMillis, wl.MemRecommendedMi, wl.MemP95Mi, wl.MemMaxMi,
			wl.CPULimitMillis, wl.CPURequestMillis, wl.MemLimitMi,
		)

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
