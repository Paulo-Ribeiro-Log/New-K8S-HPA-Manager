package actionrules

import "testing"

func TestEvaluate_TableDriven(t *testing.T) {
	th := DefaultThresholds()

	cases := []struct {
		name      string
		signal    Signal
		value     float64
		wantLevel Level // -1 = espera nil (OK, sem ação)
	}{
		{"error_rate ok", SignalErrorRate, 2, -1},
		{"error_rate warn", SignalErrorRate, 6, LevelWarn},
		{"error_rate warn no limite", SignalErrorRate, th.ErrorRateWarnPct, LevelWarn},
		{"error_rate critical", SignalErrorRate, 15, LevelCritical},
		{"error_rate critical no limite", SignalErrorRate, th.ErrorRateCriticalPct, LevelCritical},

		{"latency ok", SignalLatencyP95, 500, -1},
		{"latency warn", SignalLatencyP95, 3000, LevelWarn},
		{"latency critical", SignalLatencyP95, 6000, LevelCritical},

		{"pod_restarts ok", SignalPodRestarts, 0, -1},
		{"pod_restarts warn", SignalPodRestarts, 4, LevelWarn},
		{"pod_restarts critical", SignalPodRestarts, 12, LevelCritical},

		// pods_ready_pct é invertido: quanto MENOR, pior.
		{"pods_ready ok", SignalPodsReadyPct, 100, -1},
		{"pods_ready warn", SignalPodsReadyPct, 85, LevelWarn},
		{"pods_ready critical", SignalPodsReadyPct, 50, LevelCritical},

		{"cpu_throttle ok", SignalCPUThrottleMilliCores, 10, -1},
		{"cpu_throttle warn", SignalCPUThrottleMilliCores, 100, LevelWarn},
		{"cpu_throttle critical", SignalCPUThrottleMilliCores, 300, LevelCritical},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			eval := Evaluate(tc.signal, tc.value, th)
			if tc.wantLevel == -1 {
				if eval != nil {
					t.Fatalf("esperado nil (OK), obtido %+v", eval)
				}
				return
			}
			if eval == nil {
				t.Fatalf("esperado Level=%d, obtido nil", tc.wantLevel)
			}
			if eval.Level != tc.wantLevel {
				t.Errorf("Level = %d, esperado %d", eval.Level, tc.wantLevel)
			}
			if eval.Reason == "" {
				t.Errorf("Reason vazio")
			}
			if eval.Action == "" {
				t.Errorf("Action vazio")
			}
			if eval.AppSection == "" {
				t.Errorf("AppSection vazio")
			}
		})
	}
}

func TestEvaluate_WarnAndCriticalHaveDifferentActions(t *testing.T) {
	th := DefaultThresholds()
	for signal := range rules {
		warnEval := Evaluate(signal, valueJustAtWarn(signal, th), th)
		critEval := Evaluate(signal, valueJustAtCrit(signal, th), th)
		if warnEval == nil || critEval == nil {
			t.Fatalf("signal %q: esperado avaliação não-nil pros dois níveis", signal)
		}
		if warnEval.Action == critEval.Action {
			t.Errorf("signal %q: ação de warn e critical são iguais (%q) — devia ter texto mais urgente no critical", signal, warnEval.Action)
		}
	}
}

// valueJustAtWarn/valueJustAtCrit geram um valor que dispara warn (mas não critical) e um que
// dispara critical, respeitando a direção (lowerIsWorse ou não) de cada sinal — evita repetir a
// lógica de direção no teste em si.
func valueJustAtWarn(s Signal, th Thresholds) float64 {
	warn, crit := th.warnCrit(s)
	if rules[s].lowerIsWorse {
		if warn-crit > 1 {
			return warn - 1
		}
		return warn
	}
	if crit-warn > 1 {
		return warn + 1
	}
	return warn
}

func valueJustAtCrit(s Signal, th Thresholds) float64 {
	_, crit := th.warnCrit(s)
	if rules[s].lowerIsWorse {
		return crit - 1
	}
	return crit + 1
}

func TestEvaluate_UnknownSignalReturnsNil(t *testing.T) {
	if eval := Evaluate(Signal("nao_existe"), 999, DefaultThresholds()); eval != nil {
		t.Errorf("esperado nil pra sinal desconhecido, obtido %+v", eval)
	}
}

func TestEvaluate_ZeroThresholdsDisablesSignal(t *testing.T) {
	// Threshold zerado = sinal desabilitado (mesmo comportamento do OneAgentThresholds.resolve()
	// anterior — zero significa "não configurado", não "sempre critical").
	var empty Thresholds
	if eval := Evaluate(SignalErrorRate, 999, empty); eval != nil {
		t.Errorf("esperado nil com thresholds zerados, obtido %+v", eval)
	}
}
