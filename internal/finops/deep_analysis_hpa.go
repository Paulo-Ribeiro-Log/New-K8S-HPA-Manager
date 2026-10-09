package finops

import (
	"strconv"
	"strings"
)

// hpaTolerance é a tolerância padrão do HPA: só escala quando utilização/alvo passa de 1,1.
const hpaTolerance = 0.10

// DeepHPAMetric é uma métrica de um HPA (spec + valor atual do status, quando houver).
type DeepHPAMetric struct {
	Type string `json:"type"` // Resource | ContainerResource | Pods | Object | External
	Name string `json:"name"` // cpu | memory | nome da métrica customizada
	// TargetType: Utilization | AverageValue | Value.
	TargetType         string `json:"target_type"`
	TargetUtilization  int    `json:"target_utilization,omitempty"`
	CurrentUtilization *int   `json:"current_utilization,omitempty"`
	TargetValue        string `json:"target_value,omitempty"`
}

// DeepHPA é um HPA coletado do cluster.
type DeepHPA struct {
	Name    string
	Min     int
	Max     int
	Current int
	Metrics []DeepHPAMetric
}

// DeepHPAProjection projeta a utilização que o HPA vai medir com o request recomendado.
type DeepHPAProjection struct {
	Resource   string  `json:"resource"` // cpu | memory
	Target     int     `json:"target"`
	CurrentPct float64 `json:"current_pct"`
	// PlainProjectedPct: utilização com o request recomendado puro (P95 × 1,2).
	PlainProjectedPct float64 `json:"plain_projected_pct"`
	// ProjectedPct: utilização com o request final (depois do ajuste ao alvo, se houve).
	ProjectedPct float64 `json:"projected_pct"`
	// WouldTriggerScaleUp: o request recomendado puro faria o HPA escalar.
	WouldTriggerScaleUp bool `json:"would_trigger_scale_up"`
	// AdjustedForHPA: o request recomendado foi elevado para manter a utilização no alvo.
	AdjustedForHPA bool `json:"adjusted_for_hpa"`
}

// DeepHPAView é a visão do HPA de um workload na Deep Analysis.
type DeepHPAView struct {
	Name    string          `json:"name"`
	Min     int             `json:"min"`
	Max     int             `json:"max"`
	Current int             `json:"current"`
	Metrics []DeepHPAMetric `json:"metrics"`
	// State: fixed (min = max) | pinned_min | pinned_max | scaling.
	State       string              `json:"state"`
	AvgReplicas float64             `json:"avg_replicas,omitempty"`
	MaxObserved int                 `json:"max_observed,omitempty"`
	MinObserved int                 `json:"min_observed,omitempty"`
	ScaleEvents int                 `json:"scale_events,omitempty"`
	NeverScaled bool                `json:"never_scaled,omitempty"`
	Projections []DeepHPAProjection `json:"projections"`
}

// hpaResourceMetric devolve a métrica de utilização do recurso ("cpu"/"memory"), se houver.
func hpaResourceMetric(h DeepHPA, resource string) (DeepHPAMetric, bool) {
	for _, m := range h.Metrics {
		if (m.Type == "Resource" || m.Type == "ContainerResource") && m.Name == resource && m.TargetType == "Utilization" && m.TargetUtilization > 0 {
			return m, true
		}
	}
	return DeepHPAMetric{}, false
}

func hpaHasMemoryMetric(h DeepHPA) bool {
	for _, m := range h.Metrics {
		if m.Name == "memory" && (m.Type == "Resource" || m.Type == "ContainerResource") {
			return true
		}
	}
	return false
}

// hpaState classifica o HPA. pinned_min só quando as métricas de utilização estão abaixo do alvo
// (o HPA quer menos réplicas, o minReplicas segura) ou o histórico diz que nunca escalou.
func hpaState(h DeepHPA, hist DeepHistory) string {
	switch {
	case h.Min > 0 && h.Min == h.Max:
		return "fixed"
	case h.Max > 0 && h.Current >= h.Max:
		return "pinned_max"
	case h.Current <= h.Min:
		if hist.HPANeverScaled {
			return "pinned_min"
		}
		below, known := true, false
		for _, m := range h.Metrics {
			if m.TargetType == "Utilization" && m.TargetUtilization > 0 && m.CurrentUtilization != nil {
				known = true
				if float64(*m.CurrentUtilization) >= float64(m.TargetUtilization)*(1-hpaTolerance) {
					below = false
				}
			}
		}
		if known && below {
			return "pinned_min"
		}
	}
	return "scaling"
}

// applyHPA anexa o HPA à linha do workload, projeta a utilização com o request recomendado e, quando
// o recomendado puro faria o HPA escalar, eleva o recomendado ao mínimo que mantém a utilização no
// alvo (o HPA usa a utilização MÉDIA = uso médio / request). avgCPU/avgMem são o uso médio por pod.
func applyHPA(r *DeepWorkloadRow, h DeepHPA, hist DeepHistory, avgCPU, avgMem float64) {
	v := &DeepHPAView{
		Name: h.Name, Min: h.Min, Max: h.Max, Current: h.Current, Metrics: h.Metrics,
		State:       hpaState(h, hist),
		AvgReplicas: round2(hist.HPAAvgReplicas), MaxObserved: hist.HPAMaxObserved, MinObserved: hist.HPAMinObserved,
		ScaleEvents: hist.HPAScaleEvents, NeverScaled: hist.HPANeverScaled,
		Projections: []DeepHPAProjection{},
	}
	if v.Metrics == nil {
		v.Metrics = []DeepHPAMetric{}
	}
	r.HPA = v
	switch v.State {
	case "pinned_max":
		r.Flags = append(r.Flags, "hpa_pinned_max")
	case "pinned_min":
		r.Flags = append(r.Flags, "hpa_pinned_min")
	}
	if hpaHasMemoryMetric(h) {
		r.Flags = append(r.Flags, "hpa_memory_metric")
	}
	// Sem uso conhecido não há o que projetar; com min = max o HPA nunca escala, então ajustar o
	// request ao alvo dele não tem efeito.
	if r.UsageBasis == "" || v.State == "fixed" {
		return
	}

	for _, res := range []string{"cpu", "memory"} {
		m, ok := hpaResourceMetric(h, res)
		if !ok {
			continue
		}
		curReq, avg := r.CPUReqMillis, avgCPU
		rec := &r.CPURecMillis
		step := 10.0
		if res == "memory" {
			curReq, avg, rec, step = r.MemReqMi, avgMem, &r.MemRecMi, 16
		}
		if *rec <= 0 {
			continue
		}
		p := DeepHPAProjection{Resource: res, Target: m.TargetUtilization}
		// Uso médio por pod: a utilização atual do status do HPA é a fonte mais fiel (é o que ele
		// mede); sem ela, o uso médio coletado.
		if m.CurrentUtilization != nil && curReq > 0 {
			p.CurrentPct = float64(*m.CurrentUtilization)
			avg = p.CurrentPct / 100 * curReq
		} else if curReq > 0 {
			p.CurrentPct = deepPct(avg, curReq)
		}
		if avg <= 0 {
			continue
		}
		p.PlainProjectedPct = deepPct(avg, *rec)
		p.ProjectedPct = p.PlainProjectedPct
		limit := float64(m.TargetUtilization) * (1 + hpaTolerance)
		if p.PlainProjectedPct > limit {
			p.WouldTriggerScaleUp = true
		}
		// Mantém a utilização projetada no alvo (não só abaixo da tolerância): request mínimo =
		// uso médio / alvo.
		if p.PlainProjectedPct > float64(m.TargetUtilization) {
			need := ceilTo(avg/(float64(m.TargetUtilization)/100), step)
			if need > *rec {
				*rec = need
				p.AdjustedForHPA = true
				p.ProjectedPct = deepPct(avg, *rec)
			}
		}
		v.Projections = append(v.Projections, p)
		if p.AdjustedForHPA {
			r.Flags = append(r.Flags, "rec_adjusted_for_hpa")
			if res == "memory" && r.MemLimitRecMi < r.MemRecMi {
				r.MemLimitRecMi = r.MemRecMi
			}
		}
	}
}

// hpaMetricsSummary descreve as métricas do HPA em texto curto (ex.: "memória 85%, cpu 70%").
func hpaMetricsSummary(h *DeepHPAView) string {
	var parts []string
	for _, m := range h.Metrics {
		name := m.Name
		if name == "memory" {
			name = "memória"
		}
		switch {
		case m.TargetUtilization > 0:
			parts = append(parts, name+" "+strconv.Itoa(m.TargetUtilization)+"%")
		case m.TargetValue != "":
			parts = append(parts, name+" "+m.TargetValue)
		default:
			parts = append(parts, name)
		}
	}
	return strings.Join(parts, ", ")
}
