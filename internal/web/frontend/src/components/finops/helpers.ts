// F5.1 (FINOPS-IMPROVEMENTS-PLAN.md) — helpers puros compartilhados entre abas do FinOps,
// extraídos de FinOpsTab.tsx (ver comentário de types.ts, mesmo motivo). buildRecommendation é
// usado por WorkloadsTab.tsx e OpportunitiesTab.tsx; financeProviderInfo/
// metricsCollectionLikelyFailed são usados só pelo componente principal (FinOpsTab.tsx), mas
// vivem aqui por serem funções puras sem estado de UI, mesma separação já convencionada neste
// diretório (types vs. lógica vs. componente).

import { fmtBRL, fmtMillis, fmtMi } from "@/lib/finopsFormat";
import type { FinOpsSummary, FinOpsWorkload, Recommendation } from "./types";

/** true quando Prometheus/Dynatrace foram tentados mas NENHUM workload recebeu dado real de uso
 *  — sinal de falha de coleta (VPN/rede/API indisponível no momento do scan), não de "cluster
 *  genuinamente sem desperdício nenhum". Compartilhado entre FinOpsTab e RightsizingTab. */
export function metricsCollectionLikelyFailed(summary: FinOpsSummary): boolean {
  return !!summary.metrics_attempted && (summary.metrics_workloads_enriched ?? 0) === 0 && summary.workloads_analyzed > 0;
}

/**
 * Rótulo do cloud provider + fonte de preço real usada pelo backend pra este cluster
 * (FinOpsHandler.pricerForCluster, internal/web/handlers/finops.go). Mesma detecção por prefixo
 * de context já usada no backend (gke_/arn:aws:eks:) — sem chamada de API extra.
 * EKS ainda cai no AzurePricer (nenhum AWSPricer implementado) — texto avisa que o preço pode
 * estar incorreto em vez de fingir suporte completo.
 */
export function financeProviderInfo(clusterName: string): { label: string; source: string } {
  if (clusterName.startsWith("gke_")) {
    return { label: "GKE", source: "GCP Cloud Billing Catalog API (São Paulo)" };
  }
  if (clusterName.startsWith("arn:aws:eks:")) {
    return { label: "EKS", source: "Azure Pricing API (fallback — sem pricer AWS ainda, preço pode estar incorreto)" };
  }
  return { label: "AKS", source: "Azure Pricing API (pay-as-you-go, Brasil Sul)" };
}

// ─── Recomendações concretas ──────────────────────────────────────────────────

export function buildRecommendation(w: FinOpsWorkload, windowDays: number): Recommendation {
  const lines: { text: string; highlight?: boolean }[] = [];
  const podCostBRL = w.pods > 0 ? w.cost_share_brl / w.pods : 0;
  let safeMin: number | undefined;
  let safeMax: number | undefined;
  let savingBRL = 0;
  let needsPrometheus = false;

  // Exposição = custo se HPA escalar ao máximo configurado vs custo atual
  const exposureBRL = Math.max(0, w.hpa_cost_max_brl - w.hpa_cost_current_brl);

  // ── Caso 1: temos dados Prometheus de HPA ────────────────────────────────
  if ((w.hpa_avg_replicas ?? 0) > 0 && w.hpa_max > 0) {
    const avg    = w.hpa_avg_replicas!;
    const maxObs = w.hpa_max_observed ?? w.hpa_max;

    // Min sugerido: avg × 1.2, garantindo pelo menos 1
    const candidateMin = Math.max(1, Math.ceil(avg * 1.2));
    if (candidateMin < w.hpa_min) {
      safeMin  = candidateMin;
      savingBRL = (w.hpa_min - safeMin) * podCostBRL;
      lines.push({ text: `Reduzir min: ${w.hpa_min} → ${safeMin} réplicas`, highlight: true });
      lines.push({ text: `Média ${windowDays}d: ${avg.toFixed(1)} répl · pico real: ${maxObs}` });
      if (maxObs <= safeMin) {
        lines.push({ text: `Pico observado (${maxObs}) ≤ novo min (${safeMin}) — seguro para aplicar` });
      }
    } else if (w.verdict === "hpa_removable") {
      // Nunca escalou além do min — pode remover HPA
      safeMin = maxObs;
      lines.push({ text: `Remover HPA — fixar em ${safeMin} réplicas`, highlight: true });
      lines.push({ text: `Pico observado ${maxObs} ≤ min configurado ${w.hpa_min} em ${windowDays}d` });
      savingBRL = podCostBRL * 0.5; // overhead de gerenciamento HPA
    } else {
      // avg próxima do min — não reduzir min, mas verificar max
      lines.push({ text: `Min atual (${w.hpa_min}) já é adequado para a média observada (${avg.toFixed(1)})` });
    }

    // Max desnecessariamente alto vs pico real
    if (maxObs > 0 && maxObs < w.hpa_max * 0.6) {
      safeMax = Math.ceil(maxObs * 1.3);
      if (safeMax < w.hpa_max) {
        lines.push({ text: `Reduzir max: ${w.hpa_max} → ${safeMax} (pico foi ${maxObs}, +30% buffer)` });
      }
    }

  // ── Caso 2: sem Prometheus, workload JÁ está no mínimo (atual == min) ────
  } else if (w.hpa_max > 0 && w.hpa_min > 0 && w.hpa_current <= w.hpa_min) {
    needsPrometheus = true;
    const ratio = Math.round((w.hpa_current / w.hpa_max) * 100);
    lines.push({ text: `Rodando no mínimo configurado (${w.hpa_min} de ${w.hpa_max} max = ${ratio}% do teto)`, highlight: true });
    // Sugerir redução do max baseado em heurística 2× o atual
    const heuristicMax = Math.max(w.hpa_min + 1, w.hpa_current * 3);
    if (heuristicMax < w.hpa_max) {
      safeMax = heuristicMax;
      lines.push({ text: `Reduzir max de ${w.hpa_max} → ${heuristicMax} (3× o uso atual) para limitar exposição` });
    }
    lines.push({ text: `Ative "Usar Prometheus" para ver histórico real e recomendar min seguro` });

  // ── Caso 3: sem Prometheus, workload ACIMA do mínimo ─────────────────────
  } else if (w.hpa_max > 0 && w.hpa_current > w.hpa_min) {
    needsPrometheus = true;
    // Usar hpa_current como proxy — saving se reduzir min para atual
    if (w.hpa_min > w.hpa_current) {
      safeMin = w.hpa_current;
      savingBRL = (w.hpa_min - safeMin) * podCostBRL;
      lines.push({ text: `Reduzir min: ${w.hpa_min} → ${w.hpa_current} (já rodando com ${w.hpa_current})`, highlight: true });
    } else {
      const ratio = Math.round((w.hpa_current / w.hpa_max) * 100);
      lines.push({ text: `Rodando em ${ratio}% do max configurado (${w.hpa_current}/${w.hpa_max})`, highlight: true });
      lines.push({ text: `Ative "Usar Prometheus" para recomendar novo min baseado em histórico` });
    }
  }

  // ── CPU request muito acima do recomendado (Prometheus) ──────────────────
  if (w.cpu_recommended_millis && w.cpu_request_millis &&
      w.cpu_recommended_millis < w.cpu_request_millis * 0.85) {
    const pct = Math.round((1 - w.cpu_recommended_millis / w.cpu_request_millis) * 100);
    lines.push({
      text: `CPU request: ${fmtMillis(w.cpu_request_millis)} → ${fmtMillis(w.cpu_recommended_millis)} (-${pct}%, P95=${fmtMillis(w.cpu_p95_millis ?? 0)})`,
      highlight: !safeMin,
    });
  }

  // ── Mem request muito acima do recomendado (Prometheus) ──────────────────
  if (w.mem_recommended_mi && w.mem_request_mi &&
      w.mem_recommended_mi < w.mem_request_mi * 0.85) {
    const pct = Math.round((1 - w.mem_recommended_mi / w.mem_request_mi) * 100);
    lines.push({
      text: `Mem request: ${fmtMi(w.mem_request_mi)} → ${fmtMi(w.mem_recommended_mi)} (-${pct}%, P95=${fmtMi(w.mem_p95_mi ?? 0)})`,
      highlight: !safeMin && lines.length === 0,
    });
  }

  // ── OOM Risk ─────────────────────────────────────────────────────────────
  if (w.verdict === "oom_risk") {
    if (w.cpu_recommended_millis && w.cpu_request_millis)
      lines.push({ text: `CPU request: ${fmtMillis(w.cpu_request_millis)} → ${fmtMillis(w.cpu_recommended_millis)} (P95 ≥ 95% do request!)`, highlight: true });
    if (w.mem_recommended_mi && w.mem_request_mi)
      lines.push({ text: `Mem request: ${fmtMi(w.mem_request_mi)} → ${fmtMi(w.mem_recommended_mi)} (P95 ≥ 95% do request!)`, highlight: true });
    if (lines.length === 0)
      lines.push({ text: "Aumentar CPU/Mem request — risco real de throttling/OOM", highlight: true });
  }

  // ── Sem requests ─────────────────────────────────────────────────────────
  if (w.verdict === "no_request") {
    lines.push({ text: "Definir resource requests (CPU + Mem)", highlight: true });
    lines.push({ text: "Sem requests: scheduler não garante recursos — custo estimado por heurística" });
  }

  // ── kubectl: comandos HPA ─────────────────────────────────────────────────
  const kubectlList: string[] = [];

  // ── Alto custo fixo sem HPA ───────────────────────────────────────────────
  if (w.verdict === "fixed_high_cost") {
    lines.push({ text: `Workload caro (${fmtBRL(w.cost_share_brl)}/mês) rodando com réplicas fixas`, highlight: true });
    lines.push({ text: `Adicionar HPA permite escalar down em baixa demanda` });
    const hpaMin = Math.max(1, Math.ceil(w.pods * 0.5));
    const hpaMax = Math.max(hpaMin + 1, w.pods * 2);
    kubectlList.push(
      `kubectl autoscale deployment ${w.workload} -n ${w.namespace} --min=${hpaMin} --max=${hpaMax} --cpu-percent=70`
    );
  }

  if (safeMin !== undefined && safeMin < w.hpa_min && safeMax !== undefined && safeMax < w.hpa_max) {
    kubectlList.push(
      `kubectl patch hpa ${w.workload} -n ${w.namespace} -p '{"spec":{"minReplicas":${safeMin},"maxReplicas":${safeMax}}}'`
    );
  } else if (safeMin !== undefined && safeMin < w.hpa_min) {
    kubectlList.push(
      `kubectl patch hpa ${w.workload} -n ${w.namespace} -p '{"spec":{"minReplicas":${safeMin}}}'`
    );
  } else if (safeMax !== undefined && safeMax < w.hpa_max) {
    kubectlList.push(
      `kubectl patch hpa ${w.workload} -n ${w.namespace} -p '{"spec":{"maxReplicas":${safeMax}}}'`
    );
  }

  // ── kubectl: set resources (CPU e/ou Mem) ─────────────────────────────────
  const hasCpuRec = w.cpu_recommended_millis && w.cpu_request_millis &&
    Math.abs(w.cpu_recommended_millis - w.cpu_request_millis) / w.cpu_request_millis > 0.15;
  const hasMemRec = w.mem_recommended_mi && w.mem_request_mi &&
    Math.abs(w.mem_recommended_mi - w.mem_request_mi) / w.mem_request_mi > 0.15;

  if (hasCpuRec || hasMemRec) {
    const parts: string[] = [];
    if (hasCpuRec) parts.push(`cpu=${Math.round(w.cpu_recommended_millis!)}m`);
    if (hasMemRec) parts.push(`memory=${Math.round(w.mem_recommended_mi!)}Mi`);
    kubectlList.push(
      `kubectl set resources deployment ${w.workload} -n ${w.namespace} --requests=${parts.join(",")}`
    );
  }

  // waste_brl Prometheus é mais preciso que estimativa HPA
  if ((w.waste_brl ?? 0) > savingBRL) savingBRL = w.waste_brl!;

  return { lines, safeMin, safeMax, savingBRL, exposureBRL, needsPrometheus, kubectlList };
}
