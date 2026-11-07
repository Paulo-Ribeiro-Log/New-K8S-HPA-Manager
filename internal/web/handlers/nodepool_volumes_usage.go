package handlers

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"time"

	promclient "k8s-hpa-manager/internal/monitoring/client"
)

// Uso real dos volumes e tendência de crescimento (Prometheus: kubelet_volume_stats_*). O kubelet só
// reporta volumes montados, então volume sem pod não tem uso. A tendência é uma regressão linear
// (deriv) dos últimos 7 dias, que ignora picos pontuais; com pouco histórico ela some e fica só o
// uso atual.

const (
	usageCriticalPct  = 90.0 // % usado a partir do qual o risco de encher é crítico
	usageWarningPct   = 80.0
	daysCritical      = 7  // enche em até N dias → crítico
	daysWarning       = 30 // enche em até N dias → atenção
	oversizedMaxPct   = 20.0
	oversizedMinGiB   = 10.0
	minGrowthBytesDay = 1 << 20 // abaixo de 1 MiB/dia é ruído, não crescimento
	usageQueryTimeout = 25 * time.Second
	usageTrendWindow  = "7d"
	bytesPerGiB       = 1024.0 * 1024.0 * 1024.0
)

// VolumeUsage é o uso de um PVC lido do Prometheus.
type VolumeUsage struct {
	UsedBytes, CapacityBytes float64
	GrowthPerDay             float64 // bytes/dia pela regressão dos últimos 7 dias
	HasGrowth                bool
}

// vectorByPVC converte um resultado instantâneo em "<ns>/<pvc>" → valor, ficando com o maior
// quando o mesmo PVC aparece em mais de uma série (réplicas do kubelet/prometheus).
func vectorByPVC(res *promclient.QueryResult) map[string]float64 {
	out := map[string]float64{}
	if res == nil {
		return out
	}
	for _, r := range res.Data.Result {
		ns, pvc := r.Metric["namespace"], r.Metric["persistentvolumeclaim"]
		if ns == "" || pvc == "" || len(r.Value) < 2 {
			continue
		}
		s, ok := r.Value[1].(string)
		if !ok {
			continue
		}
		v, err := strconv.ParseFloat(s, 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
			continue
		}
		k := ns + "/" + pvc
		if old, exists := out[k]; !exists || v > old {
			out[k] = v
		}
	}
	return out
}

// fetchVolumeUsage consulta uso, capacidade e tendência. Erro só quando nem o uso atual veio; a
// falha da tendência é tolerada (HasGrowth=false).
func fetchVolumeUsage(parent context.Context, cluster string) (map[string]VolumeUsage, error) {
	pc, err := promclient.NewPrometheusClient(cluster)
	if err != nil {
		return nil, fmt.Errorf("Prometheus indisponível para este cluster: %w", err)
	}
	ctx, cancel := context.WithTimeout(parent, usageQueryTimeout)
	defer cancel()

	usedRes, err := pc.Query(ctx, `kubelet_volume_stats_used_bytes{persistentvolumeclaim!=""}`)
	if err != nil {
		return nil, fmt.Errorf("falha ao consultar o uso dos volumes no Prometheus: %w", err)
	}
	used := vectorByPVC(usedRes)
	if len(used) == 0 {
		return nil, fmt.Errorf("o Prometheus não tem kubelet_volume_stats_used_bytes (o kubelet não está sendo coletado)")
	}
	capRes, _ := pc.Query(ctx, `kubelet_volume_stats_capacity_bytes{persistentvolumeclaim!=""}`)
	capacity := vectorByPVC(capRes)
	growthRes, gerr := pc.Query(ctx, `deriv(kubelet_volume_stats_used_bytes{persistentvolumeclaim!=""}[`+usageTrendWindow+`]) * 86400`)
	var growth map[string]float64
	if gerr == nil {
		growth = vectorByPVC(growthRes)
	}

	out := make(map[string]VolumeUsage, len(used))
	for k, u := range used {
		v := VolumeUsage{UsedBytes: u, CapacityBytes: capacity[k]}
		if g, ok := growth[k]; ok {
			v.GrowthPerDay, v.HasGrowth = g, true
		}
		out[k] = v
	}
	return out, nil
}

// daysToFull: dias até o volume encher mantido o ritmo de crescimento; nil quando não cresce
// (ou cresce menos que o ruído) ou quando não há capacidade/crescimento conhecidos.
func daysToFull(u VolumeUsage) *int {
	if !u.HasGrowth || u.GrowthPerDay < minGrowthBytesDay || u.CapacityBytes <= u.UsedBytes {
		return nil
	}
	d := int(math.Ceil((u.CapacityBytes - u.UsedBytes) / u.GrowthPerDay))
	return &d
}

// applyVolumeUsage preenche uso, tendência, classe e problemas de cada item que tem métrica.
func applyVolumeUsage(items []VolumeItem, usage map[string]VolumeUsage, sum *VolumesSummary) {
	for i := range items {
		it := &items[i]
		if it.Kind != "pvc" {
			continue
		}
		u, ok := usage[it.Namespace+"/"+it.PVC]
		if !ok {
			continue
		}
		sum.UsageKnown++
		it.UsedBytes = u.UsedBytes
		capBytes := u.CapacityBytes
		if capBytes <= 0 {
			capBytes = it.CapacityBytes
		}
		if capBytes > 0 {
			it.UsagePercent = u.UsedBytes / capBytes * 100
		}
		if u.HasGrowth {
			it.GrowthBytesPerDay = u.GrowthPerDay
		}
		u.CapacityBytes = capBytes
		it.DaysToFull = daysToFull(u)

		pct, days := it.UsagePercent, it.DaysToFull
		switch {
		case pct >= usageCriticalPct || (days != nil && *days <= daysCritical):
			it.UsageClass = "critical"
			sum.UsageCritical++
			if pct >= usageCriticalPct {
				it.Issues = append(it.Issues, fmt.Sprintf("Uso em %.0f%%: risco de encher", pct))
			}
			if days != nil && *days <= daysCritical {
				it.Issues = append(it.Issues, fmt.Sprintf("Enche em ~%d dia(s) no ritmo atual (+%.1f GiB/dia)", *days, u.GrowthPerDay/bytesPerGiB))
			}
		case pct >= usageWarningPct || (days != nil && *days <= daysWarning):
			it.UsageClass = "warning"
			sum.UsageWarning++
			if pct >= usageWarningPct {
				it.Issues = append(it.Issues, fmt.Sprintf("Uso em %.0f%%", pct))
			}
			if days != nil && *days <= daysWarning {
				it.Issues = append(it.Issues, fmt.Sprintf("Enche em ~%d dias no ritmo atual (+%.1f GiB/dia)", *days, u.GrowthPerDay/bytesPerGiB))
			}
		case capBytes/bytesPerGiB >= oversizedMinGiB && pct < oversizedMaxPct:
			it.UsageClass = "oversized"
			sum.UsageOversized++
			it.Issues = append(it.Issues, fmt.Sprintf("Superdimensionado: usa %.0f%% de %.0f GiB (disco não reduz no lugar: exige recriar o volume)", pct, capBytes/bytesPerGiB))
		default:
			it.UsageClass = "ok"
		}
	}
}

// recountIssues refaz o total "com problema" depois que o uso acrescentou itens à lista de problemas.
func recountIssues(items []VolumeItem, sum *VolumesSummary) {
	sum.WithIssues = 0
	for _, it := range items {
		if len(it.Issues) > 0 {
			sum.WithIssues++
		}
	}
}
