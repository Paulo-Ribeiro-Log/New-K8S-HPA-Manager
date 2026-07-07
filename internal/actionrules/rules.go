// Package actionrules é a fonte única de verdade pra "essa métrica está ruim, e o que fazer a
// respeito" — extraído porque a mesma pergunta era respondida de 3 jeitos diferentes e divergentes
// no projeto: generateActionItems (internal/web/handlers/dynatrace.go, thresholds hardcoded),
// evaluateRisk (internal/healthcheck/oneagent_signals_checker.go, thresholds configuráveis via
// OneAgentThresholds) e EnrichWithLatencyBreach (internal/healthcheck/latency_breach_checker.go,
// threshold fixo local). Ver DYNATRACE-DIAGNOSTICS-PLAN.md Fase 1 para o levantamento completo da
// divergência (inclusive percentil de latência diferente entre os 3: P95/P90/P95-outro-valor) e as
// decisões tomadas antes de codar.
//
// Fica em internal/actionrules/ (top-level, não aninhado em healthcheck nem em web/handlers)
// porque precisa ser importável pelos dois sem inverter a direção de dependência já estabelecida
// (internal/healthcheck não pode depender de internal/web/handlers) — mesmo motivo de
// internal/monitoring/latencylookup.
package actionrules

import "fmt"

// Level é o veredito neutro de 3 estados — cada chamador mapeia pro seu próprio vocabulário:
// generateActionItems usa "MONITORAR"/"ALTA"/"IMEDIATA", evaluateRisk usa healthcheck.Severity
// (info/medium/high/critical, 5 níveis). Os dois vocabulários não têm a mesma cardinalidade, por
// isso este pacote não tenta unificá-los — só a lógica de threshold→ação é compartilhada.
type Level int

const (
	LevelOK Level = iota
	LevelWarn
	LevelCritical
)

// Signal identifica o tipo de métrica avaliada — chave compartilhada entre todos os chamadores.
type Signal string

const (
	SignalErrorRate    Signal = "error_rate"
	SignalLatencyP95   Signal = "response_p95"
	SignalPodRestarts  Signal = "pod_restarts"
	SignalPodsReadyPct Signal = "pods_ready_pct"
	// SignalCPUThrottleMilliCores — renomeado de "percentual" pra "mCores" porque a métrica usada
	// (builtin:containers.cpu.throttlingTime) não existe mais no Dynatrace (testado ao vivo: 404
	// "No metric found") e não há métrica nativa de "% de tempo throttled" no catálogo. O nome
	// correto hoje é builtin:containers.cpu.throttledMilliCores — unidade mCores, não %.
	SignalCPUThrottleMilliCores Signal = "cpu_throttle_millicores"
)

// Thresholds é o conjunto único de limiares warn/critical usado por todos os chamadores —
// substitui os 3 conjuntos de números divergentes que existiam antes (ver Fase 1 do plano).
type Thresholds struct {
	ErrorRateWarnPct     float64 `json:"error_rate_warn_pct,omitempty"`
	ErrorRateCriticalPct float64 `json:"error_rate_critical_pct,omitempty"`
	LatencyP95WarnMs     float64 `json:"latency_p95_warn_ms,omitempty"`
	LatencyP95CritMs     float64 `json:"latency_p95_crit_ms,omitempty"`
	PodRestartsWarn      float64 `json:"pod_restarts_warn,omitempty"`
	PodRestartsCrit      float64 `json:"pod_restarts_crit,omitempty"`
	PodsReadyWarnPct     float64 `json:"pods_ready_warn_pct,omitempty"`
	PodsReadyCritPct     float64 `json:"pods_ready_crit_pct,omitempty"`
	// CPUThrottleWarn/CritMilliCores ainda não têm base empírica de calibração (diferente dos
	// outros 4 sinais, que já tinham threshold funcionando de alguma forma antes desta unificação)
	// — são uma primeira aproximação, ajustar depois de ver dado real de produção.
	CPUThrottleWarnMilliCores float64 `json:"cpu_throttle_warn_millicores,omitempty"`
	CPUThrottleCritMilliCores float64 `json:"cpu_throttle_crit_millicores,omitempty"`
}

// Resolve preenche com o padrão qualquer campo zerado — permite ao chamador customizar só ALGUNS
// thresholds e deixar o resto no padrão (mesmo comportamento do antigo
// healthcheck.OneAgentThresholds.resolve(), agora centralizado aqui).
func (t Thresholds) Resolve() Thresholds {
	d := DefaultThresholds()
	if t.ErrorRateWarnPct == 0 {
		t.ErrorRateWarnPct = d.ErrorRateWarnPct
	}
	if t.ErrorRateCriticalPct == 0 {
		t.ErrorRateCriticalPct = d.ErrorRateCriticalPct
	}
	if t.LatencyP95WarnMs == 0 {
		t.LatencyP95WarnMs = d.LatencyP95WarnMs
	}
	if t.LatencyP95CritMs == 0 {
		t.LatencyP95CritMs = d.LatencyP95CritMs
	}
	if t.PodRestartsWarn == 0 {
		t.PodRestartsWarn = d.PodRestartsWarn
	}
	if t.PodRestartsCrit == 0 {
		t.PodRestartsCrit = d.PodRestartsCrit
	}
	if t.PodsReadyWarnPct == 0 {
		t.PodsReadyWarnPct = d.PodsReadyWarnPct
	}
	if t.PodsReadyCritPct == 0 {
		t.PodsReadyCritPct = d.PodsReadyCritPct
	}
	if t.CPUThrottleWarnMilliCores == 0 {
		t.CPUThrottleWarnMilliCores = d.CPUThrottleWarnMilliCores
	}
	if t.CPUThrottleCritMilliCores == 0 {
		t.CPUThrottleCritMilliCores = d.CPUThrottleCritMilliCores
	}
	return t
}

// DefaultThresholds são os valores padrão — mesmos números já usados em
// healthcheck.DefaultOneAgentThresholds() antes desta unificação (fonte escolhida por já ser
// configurável via request, mais madura que os hardcoded de generateActionItems).
func DefaultThresholds() Thresholds {
	return Thresholds{
		ErrorRateWarnPct:          5,
		ErrorRateCriticalPct:      10,
		LatencyP95WarnMs:          2000,
		LatencyP95CritMs:          5000,
		PodRestartsWarn:           3,
		PodRestartsCrit:           10,
		PodsReadyWarnPct:          90,
		PodsReadyCritPct:          70,
		CPUThrottleWarnMilliCores: 50,
		CPUThrottleCritMilliCores: 200,
	}
}

// Evaluation é o resultado de avaliar um valor de métrica contra os thresholds compartilhados.
type Evaluation struct {
	Signal     Signal
	Level      Level
	Reason     string
	Action     string
	AppSection string // seção do HPA Manager onde agir: "HPA" | "Deployments" | "Resource Explorer"
}

type rule struct {
	lowerIsWorse bool // true só pra pods_ready_pct (quanto MENOR, pior)
	appSection   string
	warnAction   string
	critAction   string
	reasonFmt    string // 2 verbos %: valor observado, threshold usado
}

var rules = map[Signal]rule{
	SignalErrorRate: {
		appSection: "Deployments",
		warnAction: "Verificar logs dos pods para identificar causa dos erros",
		critAction: "Verificar crash loops e considerar rollback para versão anterior",
		reasonFmt:  "Taxa de erro %.1f%% ≥ %.0f%%",
	},
	SignalLatencyP95: {
		appSection: "HPA",
		warnAction: "Revisar minReplicas do HPA — latência elevada",
		critAction: "Aumentar maxReplicas — latência P95 indica sobrecarga",
		reasonFmt:  "Latência P95 %.0fms ≥ %.0fms",
	},
	SignalPodRestarts: {
		appSection: "Deployments",
		warnAction: "Verificar logs de crash — possível OOMKill ou falha na inicialização",
		critAction: "Aumentar memory limit do container — provável OOMKill",
		reasonFmt:  "Pod restarts %.0f ≥ %.0f",
	},
	SignalPodsReadyPct: {
		lowerIsWorse: true,
		appSection:   "Deployments",
		warnAction:   "Verificar saúde dos pods — alguns não respondem ao readiness probe",
		critAction:   "Escalar HPA — pods insuficientes servindo tráfego",
		reasonFmt:    "Pods prontos %.1f%% ≤ %.0f%%",
	},
	SignalCPUThrottleMilliCores: {
		appSection: "Resource Explorer",
		warnAction: "Revisar CPU requests — container subdimensionado",
		critAction: "Aumentar CPU limit/request — throttling severo está causando lentidão",
		reasonFmt:  "CPU throttling %.0f mCores ≥ %.0f mCores",
	},
}

func (t Thresholds) warnCrit(s Signal) (warn, crit float64) {
	switch s {
	case SignalErrorRate:
		return t.ErrorRateWarnPct, t.ErrorRateCriticalPct
	case SignalLatencyP95:
		return t.LatencyP95WarnMs, t.LatencyP95CritMs
	case SignalPodRestarts:
		return t.PodRestartsWarn, t.PodRestartsCrit
	case SignalPodsReadyPct:
		return t.PodsReadyWarnPct, t.PodsReadyCritPct
	case SignalCPUThrottleMilliCores:
		return t.CPUThrottleWarnMilliCores, t.CPUThrottleCritMilliCores
	default:
		return 0, 0
	}
}

// Evaluate compara value contra os thresholds compartilhados de signal, devolvendo nil quando o
// valor está dentro do esperado (nenhuma ação necessária) ou quando signal/thresholds são
// desconhecidos (threshold zerado = sinal desabilitado, mesmo comportamento do
// OneAgentThresholds.resolve() anterior).
func Evaluate(signal Signal, value float64, t Thresholds) *Evaluation {
	r, ok := rules[signal]
	if !ok {
		return nil
	}
	warn, crit := t.warnCrit(signal)
	if warn == 0 && crit == 0 {
		return nil
	}

	var level Level
	var threshold float64
	if r.lowerIsWorse {
		switch {
		case value <= crit:
			level, threshold = LevelCritical, crit
		case value <= warn:
			level, threshold = LevelWarn, warn
		default:
			return nil
		}
	} else {
		switch {
		case value >= crit:
			level, threshold = LevelCritical, crit
		case value >= warn:
			level, threshold = LevelWarn, warn
		default:
			return nil
		}
	}

	action := r.warnAction
	if level == LevelCritical {
		action = r.critAction
	}

	return &Evaluation{
		Signal:     signal,
		Level:      level,
		Reason:     fmt.Sprintf(r.reasonFmt, value, threshold),
		Action:     action,
		AppSection: r.appSection,
	}
}
