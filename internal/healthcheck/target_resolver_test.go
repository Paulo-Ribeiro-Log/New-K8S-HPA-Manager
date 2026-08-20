package healthcheck

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

// fakeTargetSource é um TargetSource de teste — devolve exatamente o TargetSourceResult
// configurado, sem nenhuma chamada de rede.
type fakeTargetSource struct {
	name   string
	result TargetSourceResult
}

func (f fakeTargetSource) Name() string { return f.name }
func (f fakeTargetSource) Resolve(_ context.Context, _ string) TargetSourceResult {
	return f.result
}

// TestResolveTriageTargets_AvailableButEmpty cobre o ponto de risco #2 do
// HEALTHCHECK-TRIAGE-MODE-PLAN.md (seção 4): uma fonte disponível que não achou nenhum problema
// deve reportar AnyAvailable=true com Namespaces vazio — NUNCA ser confundida com "fonte
// indisponível" (que exigiria cair pra Varredura Completa).
func TestResolveTriageTargets_AvailableButEmpty(t *testing.T) {
	sources := []TargetSource{
		fakeTargetSource{name: "Dynatrace", result: TargetSourceResult{Available: true, Namespaces: nil}},
		fakeTargetSource{name: "Prometheus", result: TargetSourceResult{Available: true, Namespaces: nil}},
	}

	res := resolveTriageTargets(context.Background(), "cluster-a", sources)

	if !res.AnyAvailable {
		t.Fatalf("esperava AnyAvailable=true (fontes disponíveis, só sem problema) — veio false")
	}
	if len(res.Namespaces) != 0 {
		t.Fatalf("esperava 0 namespaces — veio %v", res.Namespaces)
	}
	if len(res.Sources) != 2 {
		t.Fatalf("esperava 2 status de fonte — veio %d", len(res.Sources))
	}
	for _, s := range res.Sources {
		if !s.Available {
			t.Errorf("fonte %s deveria estar Available=true", s.Name)
		}
	}
}

// TestResolveTriageTargets_NoneAvailable cobre o outro lado do mesmo risco: quando NENHUMA fonte
// está disponível (erro/não configurada), AnyAvailable deve ser false — sinal pro orchestrator
// cair pra Varredura Completa, nunca interpretar como "cluster saudável".
func TestResolveTriageTargets_NoneAvailable(t *testing.T) {
	sources := []TargetSource{
		fakeTargetSource{name: "Dynatrace", result: TargetSourceResult{Available: false}},
		fakeTargetSource{name: "Prometheus", result: TargetSourceResult{Available: false, Err: errors.New("prometheus indisponível")}},
	}

	res := resolveTriageTargets(context.Background(), "cluster-a", sources)

	if res.AnyAvailable {
		t.Fatalf("esperava AnyAvailable=false (nenhuma fonte disponível) — veio true")
	}
	if len(res.Namespaces) != 0 {
		t.Fatalf("esperava 0 namespaces — veio %v", res.Namespaces)
	}
	if res.Sources[1].Error == "" {
		t.Errorf("esperava o erro da fonte Prometheus propagado em TriageSourceStatus.Error")
	}
}

// TestResolveTriageTargets_UnionAndDedup: namespaces sinalizados por mais de uma fonte não devem
// duplicar no resultado final, e a lista final deve vir ordenada (determinismo pra UI/testes).
func TestResolveTriageTargets_UnionAndDedup(t *testing.T) {
	sources := []TargetSource{
		fakeTargetSource{name: "Dynatrace", result: TargetSourceResult{
			Available:  true,
			Namespaces: []string{"checkout", "busca"},
			Reasons:    map[string][]string{"checkout": {"Dynatrace: latência alta (ERROR)"}},
		}},
		fakeTargetSource{name: "Prometheus", result: TargetSourceResult{
			Available:  true,
			Namespaces: []string{"checkout", "pagamentos"},
			Reasons:    map[string][]string{"checkout": {"Prometheus: HighErrorRate (critical)"}},
		}},
		// Fonte indisponível não deve contribuir nem quebrar a agregação das demais.
		fakeTargetSource{name: "Zabbix", result: TargetSourceResult{Available: false}},
	}

	res := resolveTriageTargets(context.Background(), "cluster-a", sources)

	if !res.AnyAvailable {
		t.Fatalf("esperava AnyAvailable=true")
	}
	want := []string{"busca", "checkout", "pagamentos"}
	if !reflect.DeepEqual(res.Namespaces, want) {
		t.Fatalf("namespaces = %v, want %v", res.Namespaces, want)
	}
	if len(res.Reasons["checkout"]) != 2 {
		t.Fatalf("esperava 2 motivos para 'checkout' (Dynatrace + Prometheus) — veio %v", res.Reasons["checkout"])
	}
}

// TestResolveTriageTargets_DedupsRepeatedReasons cobre o achado real de 2026-08-20 (validação ao
// vivo): Prometheus dispara um alerta por objeto afetado (um KubePodNotReady por pod), então o
// mesmo texto de motivo aparecia repetido dezenas de vezes na UI pro mesmo namespace. A agregação
// deve colapsar textos idênticos numa única entrada com contagem, não listar cada ocorrência.
func TestResolveTriageTargets_DedupsRepeatedReasons(t *testing.T) {
	sources := []TargetSource{
		fakeTargetSource{name: "Prometheus", result: TargetSourceResult{
			Available:  true,
			Namespaces: []string{"checkout"},
			Reasons: map[string][]string{"checkout": {
				"Prometheus: KubePodNotReady (warning)",
				"Prometheus: KubePodNotReady (warning)",
				"Prometheus: KubePodNotReady (warning)",
				"Prometheus: KubeHpaMaxedOut (warning)",
			}},
		}},
	}

	res := resolveTriageTargets(context.Background(), "cluster-a", sources)

	reasons := res.Reasons["checkout"]
	if len(reasons) != 2 {
		t.Fatalf("esperava 2 motivos únicos (não 4 repetidos) — veio %v", reasons)
	}
	want := []string{"Prometheus: KubeHpaMaxedOut (warning)", "Prometheus: KubePodNotReady (warning) (×3)"}
	if !reflect.DeepEqual(reasons, want) {
		t.Fatalf("reasons = %v, want %v", reasons, want)
	}
}

func TestIntersectOrUse(t *testing.T) {
	tests := []struct {
		name       string
		userFilter []string
		resolved   []string
		want       []string
	}{
		{
			name:       "sem filtro manual usa o escopo resolvido inteiro",
			userFilter: nil,
			resolved:   []string{"checkout", "busca"},
			want:       []string{"checkout", "busca"},
		},
		{
			name:       "filtro manual restringe ao que também está no escopo resolvido",
			userFilter: []string{"checkout", "outro-namespace"},
			resolved:   []string{"checkout", "busca"},
			want:       []string{"checkout"},
		},
		{
			name:       "filtro manual sem sobreposição resulta em lista vazia, não painc/nil confuso",
			userFilter: []string{"outro-namespace"},
			resolved:   []string{"checkout", "busca"},
			want:       []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := intersectOrUse(tt.userFilter, tt.resolved)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("intersectOrUse(%v, %v) = %v, want %v", tt.userFilter, tt.resolved, got, tt.want)
			}
		})
	}
}
