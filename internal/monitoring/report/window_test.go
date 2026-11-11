package report

import (
	"strings"
	"testing"
	"time"
)

var now = time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)

func TestNormalizeJanela(t *testing.T) {
	ok := Window{Start: now.Add(-6 * time.Hour), End: now.Add(-time.Hour)}
	w, warns, err := ok.Normalize(now)
	if err != nil || len(warns) != 0 || !w.End.Equal(ok.End) {
		t.Fatalf("janela válida: %+v %v %v", w, warns, err)
	}
	// fim no futuro é cortado em agora, com aviso
	w, warns, err = Window{Start: now.Add(-2 * time.Hour), End: now.Add(3 * time.Hour)}.Normalize(now)
	if err != nil || !w.End.Equal(now) || len(warns) != 1 {
		t.Errorf("fim no futuro: %+v %v %v", w, warns, err)
	}
	casos := map[string]Window{
		"sem datas":           {},
		"fim antes do início": {Start: now.Add(-time.Hour), End: now.Add(-2 * time.Hour)},
		"início no futuro":    {Start: now.Add(time.Hour), End: now.Add(2 * time.Hour)},
		"curta demais":        {Start: now.Add(-2 * time.Minute), End: now.Add(-time.Minute)},
		"longa demais":        {Start: now.Add(-401 * 24 * time.Hour), End: now},
	}
	for nome, c := range casos {
		if _, _, err := c.Normalize(now); err == nil {
			t.Errorf("%s: esperava erro", nome)
		}
	}
	// o fuso não importa: instantes iguais em fusos diferentes normalizam igual
	brt := time.FixedZone("BRT", -3*3600)
	w1, _, _ := Window{Start: now.Add(-6 * time.Hour).In(brt), End: now.Add(-time.Hour).In(brt)}.Normalize(now)
	if w1.Start.Location() != time.UTC {
		t.Errorf("a janela deve sair em UTC: %v", w1.Start.Location())
	}
}

func TestPlanForPelaIdade(t *testing.T) {
	cada := func(idade, dur time.Duration) Plan {
		t.Helper()
		start := now.Add(-idade)
		p, err := PlanFor(Window{Start: start, End: start.Add(dur)}, now, nil)
		if err != nil {
			t.Fatalf("idade %s: %v", idade, err)
		}
		return p
	}
	d := 24 * time.Hour
	if p := cada(10*time.Hour, 6*time.Hour); p.DTResolution != "1m" || p.PeakApprox || p.P99Resolution != "5m" {
		t.Errorf("janela recente: %+v", p)
	}
	if p := cada(20*d, 5*time.Hour); p.DTResolution != "5m" || !p.PeakApprox || len(p.Notes) != 1 {
		t.Errorf("20 dias: %+v", p)
	}
	// janela de meses atrás: 1 h, pico aproximado
	if p := cada(120*d, 3*time.Hour); p.DTResolution != "1h" || !p.PeakApprox || !strings.Contains(p.Notes[0], "1h") {
		t.Errorf("4 meses: %+v", p)
	}
	// além da retenção: erro claro
	if _, err := PlanFor(Window{Start: now.Add(-401 * d), End: now.Add(-400 * d)}, now, nil); err == nil || !strings.Contains(err.Error(), "retenção") {
		t.Errorf("além da retenção: %v", err)
	}
}

func TestPlanForSobeResolucaoParaCaberNosPontos(t *testing.T) {
	// 12 dias recentes a 1 min seriam 17.280 pontos: sobe a escada até caber em maxPoints
	start := now.Add(-12 * 24 * time.Hour)
	p, err := PlanFor(Window{Start: start, End: now}, now, nil)
	if err != nil {
		t.Fatal(err)
	}
	if int(12*24*time.Hour/p.DTRes) > maxPoints || p.DTResolution == "1m" {
		t.Errorf("resolução %s não cabe em %d pontos", p.DTResolution, maxPoints)
	}
	if p.PromStep < time.Minute || (12*24*time.Hour)/p.PromStep > 1600 {
		t.Errorf("passo do Prometheus fora do esperado: %s", p.PromStep)
	}
}
