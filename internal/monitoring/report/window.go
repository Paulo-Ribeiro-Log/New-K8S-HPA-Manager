// Package report coleta o relatório de métricas por aplicação (tráfego, latência, erros e consumo)
// numa janela de tempo escolhida — ver MONITORING-REPORT-PLAN.md. Este arquivo cuida só da janela e
// da resolução; não faz chamada de rede.
package report

import (
	"fmt"
	"time"
)

const (
	// MinWindow e MaxWindow limitam a janela pedida. 400 dias cobre a retenção de 1 h do Dynatrace
	// (eventos antigos, como um stress test de meses atrás), com aviso de pico aproximado.
	MinWindow = 5 * time.Minute
	MaxWindow = 400 * 24 * time.Hour

	// maxPoints é o teto de pontos por série pedido ao Dynatrace (a resolução sobe até caber).
	maxPoints = 3000
	// p99Resolution é a resolução mínima do P99: percentil de bucket muito fino é ruído.
	p99Resolution = 5 * time.Minute
)

// Window é a janela absoluta do relatório (em UTC internamente; o fuso só aparece na tela).
type Window struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

func (w Window) Duration() time.Duration { return w.End.Sub(w.Start) }

// Normalize valida a janela contra "agora". Fim no futuro é cortado em agora (com aviso).
func (w Window) Normalize(now time.Time) (Window, []string, error) {
	var warns []string
	if w.Start.IsZero() || w.End.IsZero() {
		return w, nil, fmt.Errorf("informe o início e o fim da janela")
	}
	w.Start, w.End = w.Start.UTC(), w.End.UTC()
	if !w.End.After(w.Start) {
		return w, nil, fmt.Errorf("o fim da janela precisa ser depois do início")
	}
	if w.Start.After(now) {
		return w, nil, fmt.Errorf("o início da janela está no futuro")
	}
	if w.End.After(now) {
		w.End = now.UTC().Truncate(time.Second)
		warns = append(warns, "o fim da janela estava no futuro e foi cortado em agora")
	}
	if d := w.Duration(); d < MinWindow {
		return w, nil, fmt.Errorf("janela curta demais (%s): o mínimo é %s", d.Round(time.Second), MinWindow)
	} else if d > MaxWindow {
		return w, nil, fmt.Errorf("janela longa demais (%s): o máximo é %d dias", d.Round(time.Hour), int(MaxWindow.Hours()/24))
	}
	return w, warns, nil
}

// Tier é uma faixa de retenção: até MaxAge de idade a fonte guarda dados com resolução Res.
type Tier struct {
	MaxAge time.Duration
	Res    time.Duration
}

// DynatraceTiers são os valores padrão do Dynatrace SaaS (1 min ≈ 14 d, 5 min ≈ 28 d, 1 h ≈ 400 d).
// O Managed pode diferir; ajustável sem mexer no resto.
var DynatraceTiers = []Tier{
	{MaxAge: 14 * 24 * time.Hour, Res: time.Minute},
	{MaxAge: 28 * 24 * time.Hour, Res: 5 * time.Minute},
	{MaxAge: 400 * 24 * time.Hour, Res: time.Hour},
}

// resolutionLadder são as resoluções pedidas ao Dynatrace, da mais fina para a mais grossa.
var resolutionLadder = []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour, 24 * time.Hour}

// Plan é a resolução escolhida para a janela, registrada no relatório.
type Plan struct {
	// DTResolution é a resolução do tráfego no Dynatrace (ex.: "1m"); DTRes é o mesmo em Duration.
	DTResolution string        `json:"dt_resolution"`
	DTRes        time.Duration `json:"-"`
	// P99Resolution é a do P99 (nunca mais fina que 5 min).
	P99Resolution string        `json:"p99_resolution"`
	P99Res        time.Duration `json:"-"`
	// PromStep é o passo das consultas de consumo no Prometheus.
	PromStep time.Duration `json:"-"`
	// PeakApprox: a resolução é mais grossa que 1 min, então o pico de RPS é suavizado.
	PeakApprox bool     `json:"peak_approx"`
	Notes      []string `json:"notes,omitempty"`
}

func durLabel(d time.Duration) string {
	switch {
	case d%(24*time.Hour) == 0:
		return fmt.Sprintf("%dd", int(d/(24*time.Hour)))
	case d%time.Hour == 0:
		return fmt.Sprintf("%dh", int(d/time.Hour))
	}
	return fmt.Sprintf("%dm", int(d/time.Minute))
}

// PlanFor escolhe a resolução: a mais fina que a retenção ainda guarda para o INÍCIO da janela (o
// ponto mais antigo manda), e que não passe de maxPoints pontos. Janela mais antiga que a retenção
// máxima é erro (a fonte não tem o dado).
func PlanFor(w Window, now time.Time, tiers []Tier) (Plan, error) {
	if len(tiers) == 0 {
		tiers = DynatraceTiers
	}
	age := now.Sub(w.Start)
	res := time.Duration(0)
	for _, t := range tiers {
		if age <= t.MaxAge {
			res = t.Res
			break
		}
	}
	if res == 0 {
		last := tiers[len(tiers)-1]
		return Plan{}, fmt.Errorf("a janela começa há %d dias, além da retenção da fonte (%d dias): o dado não existe mais", int(age.Hours()/24), int(last.MaxAge.Hours()/24))
	}
	// sobe na escada até caber em maxPoints
	dur := w.Duration()
	for _, r := range resolutionLadder {
		if r < res {
			continue
		}
		res = r
		if int(dur/r) <= maxPoints {
			break
		}
	}
	p99 := res
	if p99 < p99Resolution {
		p99 = p99Resolution
	}
	// passo do Prometheus: no máximo ~1500 pontos, múltiplo de 1 min, no mínimo 1 min
	step := dur / 1500
	if step < time.Minute {
		step = time.Minute
	}
	step = ((step + time.Minute - 1) / time.Minute) * time.Minute

	p := Plan{
		DTResolution: durLabel(res), DTRes: res,
		P99Resolution: durLabel(p99), P99Res: p99,
		PromStep: step, PeakApprox: res > time.Minute,
	}
	if p.PeakApprox {
		p.Notes = append(p.Notes, fmt.Sprintf("pico aproximado: a resolução disponível é %s (o pico de RPS fica suavizado dentro de cada intervalo)", p.DTResolution))
	}
	return p, nil
}
