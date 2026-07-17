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
			{
				ResourceKind:       "Deployment",
				ResourceName:       "multicd-3p",
				Status:             healthcheck.StatusWarning,
				Message:            "DEGRADADO: Deployment envvias-hlg/multicd-3p com 2 de 3 replicas prontas. Capacidade reduzida.",
				Severity:           healthcheck.SeverityMedium,
				ResourceVerdict:    "oom_risk",
				CPUUsagePercent:    97.8,
				MemoryUsagePercent: 62.0,
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

// realisticNodes replica a tabela de utilização do exemplo de relatório que motivou a Fase 2:
// baixa ocupação de pods, mas nós menores ("capacidade 17") mais perto do limite.
func realisticNodes() []healthcheck.NodeHealth {
	return []healthcheck.NodeHealth{
		{
			Name:              "ip-10-0-13-130",
			Status:            healthcheck.StatusHealthy,
			Allocated:         healthcheck.NodeResources{Pods: 1},
			Allocatable:       healthcheck.NodeResources{Pods: 17},
			PodUtilization:    5.9,
			CPUUtilization:    92.0,
			MemoryUtilization: 88.0,
		},
		{
			Name:              "ip-10-0-104-4",
			Status:            healthcheck.StatusHealthy,
			Allocated:         healthcheck.NodeResources{Pods: 1},
			Allocatable:       healthcheck.NodeResources{Pods: 58},
			PodUtilization:    2.1,
			CPUUtilization:    15.0,
			MemoryUtilization: 20.0,
		},
	}
}

func TestBuildCorrelatedItemPrompt_StructuredOutput(t *testing.T) {
	prompt := buildCorrelatedItemPrompt(realisticCorrelatedItem(), realisticNodes())

	checks := []struct {
		name string
		want string
	}{
		{"emoji crítico no cabeçalho de severidade", "🔴"},
		{"citação verbatim da mensagem do evento", "CRASHLOOP: Pod envvias-hlg/multicd-3p-d6f95bbff-f8zj9 reiniciando em loop"},
		{"marcação de problema crônico", "Problema crônico"},
		{"contagem acumulada citada", "747"},
		{"veredicto de uso real de recursos (oom_risk)", "Uso real de recursos"},
		{"percentual de CPU citado no veredicto", "CPU 98% / memória 62%"},
		{"problem Dynatrace com ID", "P-99001"},
		{"dias em aberto do problem DT", "dias em aberto"},
		{"métrica de CPU não descartada", "cpu_milli"},
		{"métrica de memória não descartada", "memory_mb"},
		{"tabela de utilização dos nós", "## Utilização dos Nós"},
		{"nó com baixa capacidade citado", "ip-10-0-13-130"},
		{"percentual de utilização de pods do nó", "5.9%"},
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

func TestBuildCorrelatedItemPrompt_NoNodesOmitsSection(t *testing.T) {
	prompt := buildCorrelatedItemPrompt(realisticCorrelatedItem(), nil)
	if strings.Contains(prompt, "## Utilização dos Nós") {
		t.Error("sem dados de nó (CheckNodes desabilitado), a seção não deveria aparecer no prompt")
	}
}

func TestBuildBatchCorrelatedPrompt_StructuredOutput(t *testing.T) {
	items := []healthcheck.CorrelatedHealthItem{realisticCorrelatedItem()}
	prompt := buildBatchCorrelatedPrompt(items, realisticNodes())

	checks := []struct {
		name string
		want string
	}{
		{"emoji crítico no item numerado", "🔴"},
		{"nome do cluster no cabeçalho", "akspriv-envvias-hlg-admin"},
		{"citação verbatim da mensagem do evento", "CRASHLOOP: Pod envvias-hlg/multicd-3p-d6f95bbff-f8zj9"},
		{"marcação de problema crônico", "Problema crônico"},
		{"veredicto de uso real de recursos (oom_risk)", "Uso real de recursos"},
		{"pedido de tabela-resumo", "Tabela-resumo"},
		{"tabela de utilização dos nós", "## Utilização dos Nós"},
		{"instrução de 3 baldes de urgência - imediato", "### Imediato (hoje)"},
	}

	for _, c := range checks {
		if !strings.Contains(prompt, c.want) {
			t.Errorf("%s: esperava encontrar %q no prompt, não encontrado.\n--- prompt completo ---\n%s", c.name, c.want, prompt)
		}
	}
}

func TestBuildNodeUtilizationSection_Empty(t *testing.T) {
	if got := buildNodeUtilizationSection(nil); got != "" {
		t.Errorf("esperava string vazia sem nodes, got %q", got)
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

// TestResourceVerdictLine_OOMRisk garante que o veredicto de risco de OOM/throttling é citado com
// os percentuais de uso.
func TestResourceVerdictLine_OOMRisk(t *testing.T) {
	issue := healthcheck.CorrelatedK8sIssue{ResourceVerdict: "oom_risk", CPUUsagePercent: 97.8, MemoryUsagePercent: 62.0}
	line := resourceVerdictLine(issue)
	if !strings.Contains(line, "Uso real de recursos") || !strings.Contains(line, "risco de OOM/throttling") {
		t.Errorf("esperava linha de risco OOM, got %q", line)
	}
}

// TestResourceVerdictLine_Superprovisioned garante o texto de superprovisionamento.
func TestResourceVerdictLine_Superprovisioned(t *testing.T) {
	issue := healthcheck.CorrelatedK8sIssue{ResourceVerdict: "superprovisioned", CPUUsagePercent: 8.0, MemoryUsagePercent: 5.0}
	line := resourceVerdictLine(issue)
	if !strings.Contains(line, "superdimensionados") {
		t.Errorf("esperava linha de superprovisionamento, got %q", line)
	}
}

// TestResourceVerdictLine_OkOrEmptyOmitsLine garante que veredictos "ok" ou vazios não geram ruído
// no prompt — só vale a pena citar quando há algo acionável.
func TestResourceVerdictLine_OkOrEmptyOmitsLine(t *testing.T) {
	for _, verdict := range []string{"", "ok"} {
		issue := healthcheck.CorrelatedK8sIssue{ResourceVerdict: verdict}
		if line := resourceVerdictLine(issue); line != "" {
			t.Errorf("verdict=%q: esperava linha vazia, got %q", verdict, line)
		}
	}
}
