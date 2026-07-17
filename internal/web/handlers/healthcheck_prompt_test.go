package handlers

import (
	"strings"
	"testing"
	"time"

	"k8s-hpa-manager/internal/healthcheck"
)

// realisticCorrelatedItem monta um CorrelatedHealthItem com dados no mesmo formato do que foi
// observado num cluster real (akspriv-envvias-hlg-admin) durante a validação da Fase 1: um evento
// K8s crônico (BackOff/CrashLoopBackOff com centenas de ocorrências) correlacionado com um problem
// Dynatrace sintético — o cluster de teste real não tinha credenciais Dynatrace configuradas, então
// o lado DT é reconstruído aqui manualmente para validar a formatação do prompt ponta a ponta.
func realisticCorrelatedItem() healthcheck.CorrelatedHealthItem {
	firstSeen := time.Now().Add(-3 * time.Hour)
	return healthcheck.CorrelatedHealthItem{
		WorkloadName:  "multicd-3p",
		Namespace:     "envvias-hlg",
		Cluster:       "akspriv-envvias-hlg-admin",
		FinalSeverity: healthcheck.SeverityCritical,
		Correlated:    true,
		K8sIssues: []healthcheck.CorrelatedK8sIssue{
			{
				ResourceKind:   "Event",
				ResourceName:   "multicd-3p-d6f95bbff-f8zj9",
				Status:         healthcheck.StatusCritical,
				Message:        "CRASHLOOP: Pod envvias-hlg/multicd-3p-d6f95bbff-f8zj9 reiniciando em loop. Verificar logs: kubectl logs multicd-3p-d6f95bbff-f8zj9 -n envvias-hlg --previous",
				Severity:       healthcheck.SeverityCritical,
				Count:          747,
				FirstTimestamp: firstSeen,
				Chronicity: &healthcheck.EventChronicity{
					CumulativeCount: 747,
					FirstSeenEver:   firstSeen,
					IsChronic:       true,
				},
			},
		},
		DTProblems: []healthcheck.DynatraceHealth{
			{
				ProblemID:  "P-99001",
				DisplayID:  "P-99001",
				Title:      "multicd-3p: taxa de erro acima do threshold",
				DTSeverity: "ERROR",
				ImpactLevel: "SERVICE",
				StartTime:  time.Now().Add(-9 * 24 * time.Hour),
				Evidence:   []string{"[Root Cause] Container reiniciando repetidamente por OOMKilled"},
				MetricsSummary: map[string]float64{
					"error_rate": 95.74,
					"cpu_milli":  1850,
					"memory_mb":  980,
				},
			},
		},
	}
}

func TestBuildCorrelatedItemPrompt_StructuredOutput(t *testing.T) {
	prompt := buildCorrelatedItemPrompt(realisticCorrelatedItem())

	checks := []struct {
		name string
		want string
	}{
		{"emoji crítico no cabeçalho de severidade", "🔴"},
		{"citação verbatim da mensagem do evento", "CRASHLOOP: Pod envvias-hlg/multicd-3p-d6f95bbff-f8zj9 reiniciando em loop"},
		{"marcação de problema crônico", "Problema crônico"},
		{"contagem acumulada citada", "747"},
		{"problem Dynatrace com ID", "P-99001"},
		{"dias em aberto do problem DT", "dias em aberto"},
		{"métrica de CPU não descartada", "cpu_milli"},
		{"métrica de memória não descartada", "memory_mb"},
		{"instrução de 3 baldes de urgência - imediato", "### Imediato (hoje)"},
		{"instrução de 3 baldes de urgência - curto prazo", "### Curto prazo (esta semana)"},
		{"instrução de 3 baldes de urgência - contínuo", "### Monitoramento contínuo"},
	}

	for _, c := range checks {
		if !strings.Contains(prompt, c.want) {
			t.Errorf("%s: esperava encontrar %q no prompt, não encontrado.\n--- prompt completo ---\n%s", c.name, c.want, prompt)
		}
	}
}

func TestBuildBatchCorrelatedPrompt_StructuredOutput(t *testing.T) {
	items := []healthcheck.CorrelatedHealthItem{realisticCorrelatedItem()}
	prompt := buildBatchCorrelatedPrompt(items)

	checks := []struct {
		name string
		want string
	}{
		{"emoji crítico no item numerado", "🔴"},
		{"nome do cluster no cabeçalho", "akspriv-envvias-hlg-admin"},
		{"citação verbatim da mensagem do evento", "CRASHLOOP: Pod envvias-hlg/multicd-3p-d6f95bbff-f8zj9"},
		{"marcação de problema crônico", "Problema crônico"},
		{"pedido de tabela-resumo", "Tabela-resumo"},
		{"instrução de 3 baldes de urgência - imediato", "### Imediato (hoje)"},
	}

	for _, c := range checks {
		if !strings.Contains(prompt, c.want) {
			t.Errorf("%s: esperava encontrar %q no prompt, não encontrado.\n--- prompt completo ---\n%s", c.name, c.want, prompt)
		}
	}
}

// TestChronicityLine_NotChronicOmitsWarningIcon garante que um issue agudo (não-crônico) não usa a
// mesma formatação de alerta do crônico — evita alarme falso no relatório.
func TestChronicityLine_NotChronicOmitsWarningIcon(t *testing.T) {
	issue := healthcheck.CorrelatedK8sIssue{
		Chronicity: &healthcheck.EventChronicity{
			CumulativeCount: 3,
			FirstSeenEver:   time.Now().Add(-1 * time.Hour),
			IsChronic:       false,
		},
	}
	line := chronicityLine(issue)
	if strings.Contains(line, "Problema crônico") {
		t.Errorf("issue agudo não deveria ser rotulado como crônico: %q", line)
	}
	if !strings.Contains(line, "Agudo") {
		t.Errorf("esperava rótulo 'Agudo' para issue não-crônico, got %q", line)
	}
}

// TestChronicityLine_NilChronicityIsEmpty garante que issues sem Chronicity (ex: origem não é
// Event, ou histórico ainda não disponível) não geram uma linha vazia/quebrada no prompt.
func TestChronicityLine_NilChronicityIsEmpty(t *testing.T) {
	issue := healthcheck.CorrelatedK8sIssue{ResourceKind: "Deployment"}
	if line := chronicityLine(issue); line != "" {
		t.Errorf("esperava string vazia quando Chronicity é nil, got %q", line)
	}
}

func TestDaysOpen_Formatting(t *testing.T) {
	if got := daysOpen(time.Time{}); got != "data de início desconhecida" {
		t.Errorf("zero time: got %q", got)
	}
	if got := daysOpen(time.Now()); got != "aberto hoje" {
		t.Errorf("hoje: got %q", got)
	}
	if got := daysOpen(time.Now().Add(-9 * 24 * time.Hour)); !strings.Contains(got, "9 dias em aberto") {
		t.Errorf("9 dias: got %q", got)
	}
}
