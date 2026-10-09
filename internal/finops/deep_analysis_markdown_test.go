package finops

import (
	"strings"
	"testing"
)

func TestMdBrl(t *testing.T) {
	cases := map[float64]string{0: "0,00", 6168.5: "6.168,50", 1234567.891: "1.234.567,89", -3084.2: "-3.084,20", 999: "999,00"}
	for v, want := range cases {
		if got := mdBrl(v); got != want {
			t.Errorf("mdBrl(%v) = %q; quer %q", v, got, want)
		}
	}
}

func TestRenderDeepAnalysisMarkdown(t *testing.T) {
	in := fixtureCalculofrete(true, true)
	in.HPAs = map[string]DeepHPA{"calculo-de-fretes-prd/frete-aggregate-sku-mktp": memHPA("agg", 4, 6, 6, 70, nil)}
	in.Throttling = map[string]DeepThrottle{}
	for _, p := range in.Pods {
		if p.Workload == "frete-b2c" {
			in.Throttling[p.Namespace+"/"+p.Name] = DeepThrottle{P95Pct: 22, CurrentPct: 3}
		}
	}
	in.ThrottleWindowDays = 7
	in.SKUCatalogStatus = SKUCatalogLoading
	md := RenderDeepAnalysisMarkdown(BuildPoolDeepAnalysis(in))

	for _, want := range []string{
		"# Deep Analysis do node pool `calculofrete` (akspriv-oferta-prd)",
		"## 1. Resumo", "🔴", "## 2. Alocação do pool", "## 3. Nodes", "## 4. DaemonSets", "## 5. Workloads do pool",
		"## 6. HPAs", "**preso no máximo**", "ajustado (o puro daria",
		"## 7. Throttling de CPU", "| frete-b2c | 900m | 22% |",
		"## 8. Simulação de VMs", "`Standard_F4s_v2` **(atual)**", "(sem SMT)", "carregando — disponibilidade não verificada",
		"## Metodologia", "R$ ",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("relatório sem %q", want)
		}
	}
	if strings.Contains(md, "%!") {
		t.Errorf("verbo de formatação quebrado no relatório:\n%s", md)
	}
}

func TestRenderDeepAnalysisMarkdownSemHistorico(t *testing.T) {
	md := RenderDeepAnalysisMarkdown(BuildPoolDeepAnalysis(fixtureCalculofrete(false, false)))
	if !strings.Contains(md, "**Sem histórico:**") || strings.Contains(md, "## 6. HPAs") || strings.Contains(md, "## 7. Throttling") {
		t.Errorf("relatório sem histórico/HPA/throttling inesperado:\n%s", md)
	}
	if strings.Contains(md, "%!") {
		t.Error("verbo de formatação quebrado")
	}
}
