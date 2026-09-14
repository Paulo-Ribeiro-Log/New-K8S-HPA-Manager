package finops

// recommendedMemLimitMargin é a margem de segurança pro Limit de memória — maior que a de
// request (SafetyMargin=1.20) porque estourar Mem limit mata o pod (OOMKill), não só throttla.
// Aplicada sobre o MAIOR entre P95 e o pico observado (max_over_time), não só o P95 — um spike
// acima do P95 (por definição ~5% do tempo) que ultrapasse o limit derruba o pod mesmo assim.
const recommendedMemLimitMargin = 1.30

// defaultCPULimitRatio é usado quando o workload não tem CPU limit configurado hoje — 2× é uma
// convenção comum de "headroom de burst" na ausência de dado histórico da proporção real.
const defaultCPULimitRatio = 2.0

// recommendedLimits calcula CPU/Mem Limit recomendados. Heurística confirmada com o usuário:
//   - Mem: max(P95, pico observado) × 1.30 — protege contra OOMKill mesmo em spike acima do P95.
//   - CPU: novo request recomendado × (limit atual / request atual) quando o workload já tem
//     limit configurado hoje (preserva a folga de burst que o time já tolerava, só realinhada ao
//     request menor); sem limit configurado, usa a proporção default — estourar CPU limit só
//     throttla, nunca derruba o pod, então o risco de errar pra mais é bem menor que em Mem.
//
// memMax pode ser 0 quando a fonte de métrica não expõe pico (ex: Dynatrace nesta 1ª versão) —
// nesse caso cai pro P95 recomendado × margem, sem o benefício extra do pico real.
func recommendedLimits(cpuReqRec, memReqRec, memP95, memMax, curCPULimit, curCPURequest, curMemLimit float64) (cpuLimitRec, memLimitRec float64) {
	if cpuReqRec > 0 {
		ratio := defaultCPULimitRatio
		if curCPULimit > 0 && curCPURequest > 0 {
			ratio = curCPULimit / curCPURequest
		}
		cpuLimitRec = round2(cpuReqRec * ratio)
	}
	if memReqRec > 0 {
		peak := memP95
		if memMax > peak {
			peak = memMax
		}
		if peak > 0 {
			memLimitRec = round2(peak * recommendedMemLimitMargin)
		}
	}
	return cpuLimitRec, memLimitRec
}
