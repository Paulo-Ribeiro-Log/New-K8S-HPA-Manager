package spinnaker

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func mkTrigger(nameApp, namespace, version, chg string) Trigger {
	return Trigger{
		Parameters: map[string]interface{}{
			paramAppName:   nameApp,
			paramNamespace: namespace,
			paramVersion:   version,
			paramCHGNumber: chg,
		},
	}
}

// TestDetectRollback_ExplicitPRD reproduz o caso real confirmado em PRD (seção 2 do plano):
// pipeline dedicado "rollback-aks-global" cuja versão-alvo já é a vigente.
func TestDetectRollback_ExplicitPRD(t *testing.T) {
	executions := []Execution{
		{
			ID:        "rollback-exec",
			Name:      "rollback-aks-global",
			Status:    "SUCCEEDED",
			StartTime: 1000,
			EndTime:   2000,
			Trigger:   mkTrigger("estoque-margem-seguranca", "oferta-estoque-1p-api-internas-prd", "3.3.0-4", "CHG0475290"),
		},
		{
			ID:        "deploy-exec-antigo",
			Name:      "deploy-aks-global",
			Status:    "SUCCEEDED",
			StartTime: 500,
			EndTime:   700,
			Trigger:   mkTrigger("estoque-margem-seguranca", "oferta-estoque-1p-api-internas-prd", "3.3.0-3", "CHG0470000"),
		},
	}

	got := DetectRollback(executions, "estoque-margem-seguranca", "oferta-estoque-1p-api-internas-prd", "3.3.0-4")

	if !got.Matched {
		t.Fatal("esperava Matched=true")
	}
	if got.IsRollback == nil || !*got.IsRollback {
		t.Fatal("esperava IsRollback=true")
	}
	if got.RollbackType != "explicit" {
		t.Errorf("RollbackType = %q, esperava \"explicit\"", got.RollbackType)
	}
	if got.FailedCHG != "CHG0475290" {
		t.Errorf("FailedCHG = %q, esperava CHG0475290", got.FailedCHG)
	}
	if got.RollbackPipelineName != "rollback-aks-global" {
		t.Errorf("RollbackPipelineName = %q, esperava rollback-aks-global", got.RollbackPipelineName)
	}
}

// TestDetectRollback_ImplicitHLG reproduz o caso real confirmado em HLG (seção 0.5/0.6 do
// plano): o upgrade mais recente falhou e a versão vigente continua sendo a anterior.
func TestDetectRollback_ImplicitHLG(t *testing.T) {
	executions := []Execution{
		{
			ID:        "falha-recente",
			Name:      "deploy-aks-global",
			Status:    "FAILED_CONTINUE",
			StartTime: 2000,
			EndTime:   2600,
			Trigger:   mkTrigger("dat-documento-vendas-api", "dat-sit", "0.0.2-3", ""),
		},
		{
			ID:        "sucesso-anterior",
			Name:      "deploy-aks-global",
			Status:    "SUCCEEDED",
			StartTime: 1000,
			EndTime:   1300,
			Trigger:   mkTrigger("dat-documento-vendas-api", "dat-sit", "0.0.2-2", ""),
		},
	}

	// versão vigente no K8s continua sendo a anterior (0.0.2-2) — o upgrade pra 0.0.2-3 falhou.
	got := DetectRollback(executions, "dat-documento-vendas-api", "dat-sit", "0.0.2-2")

	if !got.Matched {
		t.Fatal("esperava Matched=true")
	}
	if got.IsRollback == nil || !*got.IsRollback {
		t.Fatal("esperava IsRollback=true")
	}
	if got.RollbackType != "implicit" {
		t.Errorf("RollbackType = %q, esperava \"implicit\"", got.RollbackType)
	}
	if got.RollbackPipelineName != "" {
		t.Errorf("RollbackPipelineName deveria ficar vazio no caso implícito, veio %q", got.RollbackPipelineName)
	}
	if got.SpinnakerExecutionID != "falha-recente" {
		t.Errorf("SpinnakerExecutionID = %q, esperava apontar pra execução que falhou", got.SpinnakerExecutionID)
	}
}

// TestDetectRollback_DeployNormal garante que um deploy bem-sucedido comum não é marcado como
// rollback — regressão óbvia mas crítica, dado que o resto da lógica é toda sobre achar rollback.
func TestDetectRollback_DeployNormal(t *testing.T) {
	executions := []Execution{
		{
			ID:        "deploy-ok",
			Name:      "deploy-aks-global",
			Status:    "SUCCEEDED",
			StartTime: 1000,
			Trigger:   mkTrigger("app-x", "ns-x", "1.0.0", "CHG0000001"),
		},
	}

	got := DetectRollback(executions, "app-x", "ns-x", "1.0.0")

	if !got.Matched {
		t.Fatal("esperava Matched=true")
	}
	if got.IsRollback == nil || *got.IsRollback {
		t.Fatal("esperava IsRollback=false")
	}
	if got.RollbackType != "" {
		t.Errorf("RollbackType deveria ficar vazio, veio %q", got.RollbackType)
	}
	if got.LastCHGApplied != "CHG0000001" {
		t.Errorf("LastCHGApplied = %q, esperava CHG0000001", got.LastCHGApplied)
	}
}

// TestDetectRollback_SemExecucaoCorrespondente cobre o caso "não determinado" — nunca deve
// virar false por omissão (fraseologia neutra documentada no plano/CLAUDE.md).
func TestDetectRollback_SemExecucaoCorrespondente(t *testing.T) {
	executions := []Execution{
		{ID: "outra-app", Name: "deploy-aks-global", Status: "SUCCEEDED", Trigger: mkTrigger("outra-app", "outro-ns", "9.9.9", "")},
	}

	got := DetectRollback(executions, "app-x", "ns-x", "1.0.0")

	if got.Matched {
		t.Fatal("esperava Matched=false — nenhuma execução bate com nameApp/namespace")
	}
	if got.IsRollback != nil {
		t.Fatal("esperava IsRollback=nil quando Matched=false")
	}
}

// TestDetectRollback_VersaoVigenteForaDaJanela cobre o caso em que existem execuções pro
// target certo, mas nenhuma delas corresponde à versão vigente (ex: janela de busca não
// alcançou a execução relevante) — também deve ficar "não determinado", não false.
func TestDetectRollback_VersaoVigenteForaDaJanela(t *testing.T) {
	executions := []Execution{
		{ID: "e1", Name: "deploy-aks-global", Status: "SUCCEEDED", StartTime: 1000, Trigger: mkTrigger("app-x", "ns-x", "2.0.0", "")},
	}

	got := DetectRollback(executions, "app-x", "ns-x", "1.0.0-versao-antiga-nao-vista")

	if got.Matched {
		t.Fatal("esperava Matched=false — versão vigente não corresponde a nenhuma execução vista")
	}
	// LatestKnownExecutionAt precisa vir preenchido mesmo em Matched:false — é o que permite ao
	// chamador (applyRegistryFreshness, internal/web/handlers/spinnaker.go) distinguir "sem sinal
	// nenhum" de "o Deployment Registry está desatualizado" (achado real, ver comentário do campo).
	if got.LatestKnownExecutionAt != 1000 {
		t.Errorf("LatestKnownExecutionAt = %d, esperava 1000 (StartTime da única execução conhecida)", got.LatestKnownExecutionAt)
	}
	// LatestKnownExecution — mesmo dado de LatestKnownExecutionAt acima, agora exposto por
	// completo (pedido do usuário: o badge/modal de "dado desatualizado" precisa mostrar QUAL
	// pipeline/versão motivou a divergência, não só QUANDO). Também precisa vir preenchido em
	// Matched:false — é exatamente este caso (registry_stale) que mais precisa dele.
	if got.LatestKnownExecution == nil {
		t.Fatal("LatestKnownExecution não deveria ser nil em Matched:false — é o caso que mais precisa dele (ver registry_stale)")
	}
	if got.LatestKnownExecution.Version != "2.0.0" || got.LatestKnownExecution.ExecutionID != "e1" {
		t.Errorf("LatestKnownExecution = %+v, esperava version=2.0.0 execution_id=e1", got.LatestKnownExecution)
	}
}

// TestDetectRollback_ExplicitViaIsRollbackFlag_OutraSquad reproduz o achado da Fase 4
// (generalização entre squads, confirmado ao vivo contra a squad SRE Marketplace — diferente da
// SRE Logística usada nos demais testes): o sinal "Is Rollback" no trigger.parameters continua
// funcionando mesmo se, hipoteticamente, uma squad nomeasse o pipeline de rollback de outro
// jeito (não "rollback-*") — o pipeline "deploy-hotfix", sem "rollback" no nome nem manifesto
// helm-rollback.yaml, ainda é detectado via IsRollbackFlag.
func TestDetectRollback_ExplicitViaIsRollbackFlag_OutraSquad(t *testing.T) {
	trigger := mkTrigger("mp-pas-comercial", "marketplace-cross-prd", "2.8.4-1", "CHG0478329")
	trigger.Parameters["Is Rollback"] = true

	executions := []Execution{
		{
			ID:        "rollback-via-flag",
			Name:      "deploy-hotfix", // nome deliberadamente sem "rollback" — só o flag identifica
			Status:    "SUCCEEDED",
			StartTime: 1000,
			EndTime:   2000,
			Trigger:   trigger,
		},
	}

	got := DetectRollback(executions, "mp-pas-comercial", "marketplace-cross-prd", "2.8.4-1")

	if !got.Matched {
		t.Fatal("esperava Matched=true")
	}
	if got.IsRollback == nil || !*got.IsRollback {
		t.Fatal("esperava IsRollback=true (via Is Rollback flag, não nome de pipeline)")
	}
	if got.RollbackType != "explicit" {
		t.Errorf("RollbackType = %q, esperava \"explicit\"", got.RollbackType)
	}
	if got.FailedCHG != "CHG0478329" {
		t.Errorf("FailedCHG = %q, esperava CHG0478329", got.FailedCHG)
	}
}

// TestDetectRollback_PropagaCHGUrl cobre a seção 9 item 3 do plano (link direto pro ServiceNow):
// LastCHGAppliedURL/FailedCHGURL vêm de Trigger.CHGUrl() (Payload), propagados pelo DetectRollback
// tanto no caminho normal quanto no explícito.
func TestDetectRollback_PropagaCHGUrl(t *testing.T) {
	trigger := mkTrigger("app-x", "ns-x", "1.0.0", "CHG0000001")
	trigger.Payload = map[string]interface{}{
		"serviceNowChgUrl": "https://viavarejo.service-now.com/change_request.do?sys_id=xyz",
	}

	executions := []Execution{
		{ID: "deploy-ok", Name: "deploy-aks-global", Status: "SUCCEEDED", StartTime: 1000, Trigger: trigger},
	}

	got := DetectRollback(executions, "app-x", "ns-x", "1.0.0")

	if got.LastCHGAppliedURL != "https://viavarejo.service-now.com/change_request.do?sys_id=xyz" {
		t.Errorf("LastCHGAppliedURL = %q, esperava a URL do Payload", got.LastCHGAppliedURL)
	}
}

// TestDetectRollback_RecentExecutions cobre a seção 9 item 5 do plano (histórico curto): as
// últimas execuções desse nameApp/namespace vêm anexadas ao resultado, mais recente primeiro,
// limitadas a recentExecutionsLimit — não só a execução que decidiu o resultado principal.
func TestDetectRollback_RecentExecutions(t *testing.T) {
	executions := []Execution{
		{ID: "e1", Name: "deploy-aks-global", Status: "SUCCEEDED", StartTime: 4000, Trigger: mkTrigger("app-x", "ns-x", "1.0.3", "CHG0000004")},
		{ID: "e2", Name: "deploy-aks-global", Status: "TERMINAL", StartTime: 3000, Trigger: mkTrigger("app-x", "ns-x", "1.0.2", "CHG0000003")},
		{ID: "e3", Name: "deploy-aks-global", Status: "SUCCEEDED", StartTime: 2000, Trigger: mkTrigger("app-x", "ns-x", "1.0.1", "CHG0000002")},
		{ID: "e4", Name: "deploy-aks-global", Status: "SUCCEEDED", StartTime: 1000, Trigger: mkTrigger("app-x", "ns-x", "1.0.0", "CHG0000001")},
	}

	got := DetectRollback(executions, "app-x", "ns-x", "1.0.3")

	if len(got.RecentExecutions) != 4 {
		t.Fatalf("RecentExecutions tem %d itens, esperava 4 (menos que o limite de %d)", len(got.RecentExecutions), recentExecutionsLimit)
	}
	if got.RecentExecutions[0].ExecutionID != "e1" {
		t.Errorf("RecentExecutions[0].ExecutionID = %q, esperava e1 (mais recente primeiro)", got.RecentExecutions[0].ExecutionID)
	}
	if got.RecentExecutions[3].ExecutionID != "e4" {
		t.Errorf("RecentExecutions[3].ExecutionID = %q, esperava e4 (mais antiga por último)", got.RecentExecutions[3].ExecutionID)
	}
	if got.RecentExecutions[0].Version != "1.0.3" || got.RecentExecutions[0].CHG != "CHG0000004" {
		t.Errorf("RecentExecutions[0] = %+v, campos incorretos", got.RecentExecutions[0])
	}
}

// TestDetectRollback_RecentExecutions_RespeitaLimite garante que a lista nunca ultrapassa
// recentExecutionsLimit mesmo com muito mais execuções disponíveis.
func TestDetectRollback_RecentExecutions_RespeitaLimite(t *testing.T) {
	var executions []Execution
	for i := 0; i < 20; i++ {
		executions = append(executions, Execution{
			ID: fmt.Sprintf("e%d", i), Name: "deploy-aks-global", Status: "SUCCEEDED",
			StartTime: int64(i * 1000), Trigger: mkTrigger("app-x", "ns-x", "1.0.0", "CHG0000001"),
		})
	}

	got := DetectRollback(executions, "app-x", "ns-x", "1.0.0")

	if len(got.RecentExecutions) != recentExecutionsLimit {
		t.Fatalf("RecentExecutions tem %d itens, esperava exatamente %d (o limite)", len(got.RecentExecutions), recentExecutionsLimit)
	}
}

// TestDetectRollback_Stages cobre o pedido do usuário (screenshot da UI real do Deck): o
// detalhamento por etapa (Step/Started/Completed/Status) da execução que decidiu o resultado
// vem anexado — inclusive quando uma etapa individual tem status diferente do geral (SKIPPED
// dentro de uma execução SUCCEEDED, confirmado ao vivo).
func TestDetectRollback_Stages(t *testing.T) {
	trigger := mkTrigger("estoque-margem-seguranca", "oferta-estoque-1p-api-internas-prd", "3.3.0-4", "CHG0475290")
	executions := []Execution{
		{
			ID:        "rollback-exec",
			Name:      "rollback-aks-global",
			Status:    "SUCCEEDED",
			StartTime: 1000,
			EndTime:   2000,
			Trigger:   trigger,
			Stages: []Stage{
				{Name: "rollback", Status: "SUCCEEDED", StartTime: 1000, EndTime: 1500},
				{Name: "rollback-helm", Status: "SKIPPED", StartTime: 0, EndTime: 0},
				{Name: "xl-release-callback", Status: "SUCCEEDED", StartTime: 1500, EndTime: 2000},
			},
		},
	}

	got := DetectRollback(executions, "estoque-margem-seguranca", "oferta-estoque-1p-api-internas-prd", "3.3.0-4")

	if len(got.Stages) != 3 {
		t.Fatalf("Stages tem %d itens, esperava 3", len(got.Stages))
	}
	if got.Stages[0].Name != "rollback" || got.Stages[0].Status != "SUCCEEDED" {
		t.Errorf("Stages[0] = %+v, esperava rollback/SUCCEEDED", got.Stages[0])
	}
	if got.Stages[1].Name != "rollback-helm" || got.Stages[1].Status != "SKIPPED" {
		t.Errorf("Stages[1] = %+v, esperava rollback-helm/SKIPPED (status por-etapa diferente do geral)", got.Stages[1])
	}
	// ordem preservada — a mesma ordem que o Gate/pipeline usa, não reordenada por tempo.
	if got.Stages[2].Name != "xl-release-callback" {
		t.Errorf("Stages[2].Name = %q, esperava xl-release-callback (ordem original preservada)", got.Stages[2].Name)
	}
}

// TestDetectRollback_PreviousVersion responde a pergunta real do usuário: "qual a versão
// anterior da aplicação deployada antes da execução da pipeline?". A execução SUCCEEDED mais
// recente ANTES (cronologicamente) da execução decisiva é a "versão anterior" — pula execuções
// falhas no meio, que nunca chegaram a ficar live de verdade.
func TestDetectRollback_PreviousVersion(t *testing.T) {
	executions := []Execution{
		{ID: "e1", Name: "deploy-aks-global", Status: "SUCCEEDED", StartTime: 4000, Trigger: mkTrigger("app-x", "ns-x", "1.0.3", "CHG0000004")},
		{ID: "e2-falhou", Name: "deploy-aks-global", Status: "TERMINAL", StartTime: 3000, Trigger: mkTrigger("app-x", "ns-x", "1.0.2-tentativa", "CHG0000003")},
		{ID: "e3", Name: "deploy-aks-global", Status: "SUCCEEDED", StartTime: 2000, Trigger: mkTrigger("app-x", "ns-x", "1.0.1", "CHG0000002")},
		{ID: "e4", Name: "deploy-aks-global", Status: "SUCCEEDED", StartTime: 1000, Trigger: mkTrigger("app-x", "ns-x", "1.0.0", "CHG0000001")},
	}

	got := DetectRollback(executions, "app-x", "ns-x", "1.0.3")

	if got.PreviousVersion != "1.0.1" {
		t.Errorf("PreviousVersion = %q, esperava 1.0.1 (pula e2-falhou, que nunca ficou live de verdade)", got.PreviousVersion)
	}
	if got.PreviousVersionCHG != "CHG0000002" {
		t.Errorf("PreviousVersionCHG = %q, esperava CHG0000002", got.PreviousVersionCHG)
	}
	if got.PreviousVersionExecutedAt != 2000 {
		t.Errorf("PreviousVersionExecutedAt = %d, esperava 2000", got.PreviousVersionExecutedAt)
	}
}

// TestDetectRollback_PreviousVersion_NenhumaAnterior garante que o campo fica vazio (não
// panica, não inventa dado) quando não há nenhuma execução SUCCEEDED mais antiga na janela.
func TestDetectRollback_PreviousVersion_NenhumaAnterior(t *testing.T) {
	executions := []Execution{
		{ID: "unica", Name: "deploy-aks-global", Status: "SUCCEEDED", StartTime: 1000, Trigger: mkTrigger("app-x", "ns-x", "1.0.0", "CHG0000001")},
	}

	got := DetectRollback(executions, "app-x", "ns-x", "1.0.0")

	if got.PreviousVersion != "" {
		t.Errorf("PreviousVersion = %q, esperava vazio (só há 1 execução na janela)", got.PreviousVersion)
	}
}

// TestDetectRollback_PreviousVersion_PulaMesmaVersaoReimplantada — bug real reportado pelo
// usuário: o pipeline pode ser re-executado pra MESMA versão sob uma CHG diferente (ex:
// reprocessamento, mudança de infra sem bump de versão). Antes desse fix, previousSuccessfulExecution
// não checava se a versão da execução anterior era diferente da atual — "Versão anterior"
// mostrava a mesma versão de "Versão" (só com CHG diferente), o que é enganoso: "versão
// anterior" precisa significar "a última vez que rodou algo DIFERENTE", não só "a execução
// anterior, seja qual for a versão". Corrigido pulando execuções com versão igual e continuando
// a busca até achar uma genuinamente diferente.
func TestDetectRollback_PreviousVersion_PulaMesmaVersaoReimplantada(t *testing.T) {
	executions := []Execution{
		{ID: "e1", Name: "deploy-aks-global", Status: "SUCCEEDED", StartTime: 4000, Trigger: mkTrigger("app-x", "ns-x", "1.14.2-2", "CHG0480565")},
		// mesma versão da e1, CHG diferente — reprocessamento/redeploy sem mudança de código.
		{ID: "e2-mesma-versao", Name: "deploy-aks-global", Status: "SUCCEEDED", StartTime: 3000, Trigger: mkTrigger("app-x", "ns-x", "1.14.2-2", "CHG0479999")},
		{ID: "e3", Name: "deploy-aks-global", Status: "SUCCEEDED", StartTime: 2000, Trigger: mkTrigger("app-x", "ns-x", "1.12.0-5", "CHG0476326")},
	}

	got := DetectRollback(executions, "app-x", "ns-x", "1.14.2-2")

	if got.PreviousVersion != "1.12.0-5" {
		t.Errorf("PreviousVersion = %q, esperava 1.12.0-5 (pula e2-mesma-versao, que é a mesma versão da atual)", got.PreviousVersion)
	}
	if got.PreviousVersionCHG != "CHG0476326" {
		t.Errorf("PreviousVersionCHG = %q, esperava CHG0476326", got.PreviousVersionCHG)
	}
}

// TestDetectRollback_PreviousVersion_TodoHistoricoMesmaVersao garante que o campo fica vazio
// (não mostra uma versão igual à atual como se fosse "anterior") quando TODO o histórico
// disponível na janela é da mesma versão — nenhuma versão genuinamente anterior foi vista ainda.
func TestDetectRollback_PreviousVersion_TodoHistoricoMesmaVersao(t *testing.T) {
	executions := []Execution{
		{ID: "e1", Name: "deploy-aks-global", Status: "SUCCEEDED", StartTime: 2000, Trigger: mkTrigger("app-x", "ns-x", "1.0.0", "CHG0000002")},
		{ID: "e2", Name: "deploy-aks-global", Status: "SUCCEEDED", StartTime: 1000, Trigger: mkTrigger("app-x", "ns-x", "1.0.0", "CHG0000001")},
	}

	got := DetectRollback(executions, "app-x", "ns-x", "1.0.0")

	if got.PreviousVersion != "" {
		t.Errorf("PreviousVersion = %q, esperava vazio (todo histórico disponível é da mesma versão 1.0.0)", got.PreviousVersion)
	}
}

// TestDetectRollback_RecentStageFailures_ExecucaoRetryNaoAparecePerdida reproduz o achado real
// (relatado ao vivo pelo usuário depois da correção de registry_stale: "eu sei que a aplicação
// entrega-mais-bff teve a pipeline executada com erros e depois com sucesso, mas não houveram
// sinais nem os logs das exceptions"): confirmado contra a instância real do usuário — 3 de 4
// execuções recentes de entrega-mais-bff/entrega-mais-sit tinham o stage "deploy-helm" com status
// FAILED_CONTINUE (Kubernetes Job com BackoffLimitExceeded) mas a EXECUÇÃO como um todo terminava
// SUCCEEDED — invisível pro DetectRollback (só olha ex.Status) e pro resto do sistema (Stages só
// eram processados pra execução "decisiva", nunca pras demais do histórico recente).
func TestDetectRollback_RecentStageFailures_ExecucaoRetryNaoAparecePerdida(t *testing.T) {
	failedStageContext := []byte(`{
		"kato.tasks": [
			{
				"exception": {
					"message": "Job 'helm-deploy-x' failed. Reason: BackoffLimitExceeded. Container 'helm-deploy' exited with code: 1.",
					"operation": "waitOnJobCompletion"
				}
			}
		]
	}`)

	now := time.Now()
	executions := []Execution{
		{
			ID: "e-sucesso-final", Name: "deploy-aks-global", Status: "SUCCEEDED", StartTime: now.UnixMilli(),
			Trigger: mkTrigger("entrega-mais-bff", "entrega-mais-sit", "1.5.48-2", ""),
			Stages:  []Stage{{Name: "deploy-helm", Status: "SUCCEEDED", Context: []byte(`{}`)}},
		},
		{
			// Execução com retry: execução geral SUCCEEDED (o pipeline segue apesar da etapa
			// falha), mas o stage "deploy-helm" tem status FAILED_CONTINUE com exceção real.
			// StartTime há 1h — dentro de stageFailureRecentWindow (48h), precisa aparecer.
			ID: "e-com-retry", Name: "deploy-aks-global", Status: "SUCCEEDED", StartTime: now.Add(-1 * time.Hour).UnixMilli(),
			Trigger: mkTrigger("entrega-mais-bff", "entrega-mais-sit", "1.5.48-2", ""),
			Stages:  []Stage{{Name: "deploy-helm", Status: "FAILED_CONTINUE", Context: failedStageContext}},
		},
	}

	got := DetectRollback(executions, "entrega-mais-bff", "entrega-mais-sit", "1.5.48-2")

	// A execução mais recente bate com a versão vigente — "deploy normal" pela regra atual, o
	// que é correto (é o estado final de verdade). O ponto do teste é que isso NÃO deve esconder
	// a falha de retry encontrada na execução anterior.
	if got.IsRollback == nil || *got.IsRollback {
		t.Fatalf("esperava deploy normal (IsRollback=false) — o estado final É sucesso, isso não muda")
	}
	if len(got.RecentStageFailures) != 1 {
		t.Fatalf("esperava 1 falha de stage recente, veio %d: %+v", len(got.RecentStageFailures), got.RecentStageFailures)
	}
	failure := got.RecentStageFailures[0]
	if failure.ExecutionID != "e-com-retry" {
		t.Errorf("ExecutionID = %q, esperava e-com-retry", failure.ExecutionID)
	}
	if failure.StageName != "deploy-helm" || failure.StageStatus != "FAILED_CONTINUE" {
		t.Errorf("StageName/StageStatus = %q/%q, esperava deploy-helm/FAILED_CONTINUE", failure.StageName, failure.StageStatus)
	}
	if !strings.Contains(failure.Log, "BackoffLimitExceeded") {
		t.Errorf("Log não contém o erro real: %q", failure.Log)
	}
}

// TestDetectRollback_RecentStageFailures_ExecucaoAntigaAindaAparece garante que uma falha de
// etapa antiga (semanas/meses) CONTINUA aparecendo em RecentStageFailures — 2ª rodada do achado
// real: uma 1ª versão desta função filtrava por StageFailureRecentWindow (48h), mas isso teve o
// efeito colateral não pedido de remover o indicador de atenção dos cards da lista de Deployments
// pra qualquer app sem redeploy recente. Usuário pediu explicitamente pra não tirar isso — a
// filtragem por tempo foi revertida aqui e movida só pra decisão de NOTIFICAR
// (SpinnakerFleetWatcher.notifyStageFailureIfNew, internal/web/handlers/spinnaker_watcher_test.go).
func TestDetectRollback_RecentStageFailures_ExecucaoAntigaAindaAparece(t *testing.T) {
	failedStageContext := []byte(`{
		"kato.tasks": [
			{"exception": {"message": "falha antiga", "operation": "waitOnJobCompletion"}}
		]
	}`)
	oldExecution := time.Now().Add(-30 * 24 * time.Hour) // ~1 mês atrás — bem além de StageFailureRecentWindow (48h)

	executions := []Execution{
		{
			ID: "e-antiga-com-falha", Name: "deploy-aks-global", Status: "SUCCEEDED", StartTime: oldExecution.UnixMilli(),
			Trigger: mkTrigger("app-parado", "ns-x", "1.0.0", ""),
			Stages:  []Stage{{Name: "deploy-helm", Status: "FAILED_CONTINUE", Context: failedStageContext}},
		},
	}

	got := DetectRollback(executions, "app-parado", "ns-x", "1.0.0")
	if len(got.RecentStageFailures) != 1 {
		t.Fatalf("falha de ~1 mês atrás deveria continuar aparecendo (sem filtro de idade), veio %d: %+v", len(got.RecentStageFailures), got.RecentStageFailures)
	}
	if got.RecentStageFailures[0].ExecutionTime != oldExecution.UnixMilli() {
		t.Errorf("ExecutionTime = %d, esperava %d", got.RecentStageFailures[0].ExecutionTime, oldExecution.UnixMilli())
	}
}

// TestDetectRollback_RecentStageFailures_SemFalhaFicaVazio garante que o campo não aparece
// quando não há nenhum stage com FailureLog real — nunca inferir falha por omissão.
func TestDetectRollback_RecentStageFailures_SemFalhaFicaVazio(t *testing.T) {
	executions := []Execution{
		{
			ID: "e1", Name: "deploy-aks-global", Status: "SUCCEEDED", StartTime: 1000,
			Trigger: mkTrigger("app-x", "ns-x", "1.0.0", ""),
			Stages:  []Stage{{Name: "deploy-helm", Status: "SUCCEEDED", Context: []byte(`{"kato.tasks":[{"history":[{"phase":"X","status":"Y"}]}]}`)}},
		},
	}
	got := DetectRollback(executions, "app-x", "ns-x", "1.0.0")
	if len(got.RecentStageFailures) != 0 {
		t.Errorf("esperava RecentStageFailures vazio (history sem exception não é falha), veio %+v", got.RecentStageFailures)
	}
}

// TestManifestReference_ContextInvalido garante que Stage.ManifestReference nunca panica nem
// retorna erro fatal com um context malformado/ausente — dado best-effort.
func TestManifestReference_ContextInvalido(t *testing.T) {
	cases := []Stage{
		{},
		{Context: []byte(`{}`)},
		{Context: []byte(`not-json`)},
		{Context: []byte(`{"manifestArtifact":{"reference":"https://nexus/x/helm-rollback.yaml"}}`)},
	}
	want := []string{"", "", "", "https://nexus/x/helm-rollback.yaml"}

	for i, s := range cases {
		if got := s.ManifestReference(); got != want[i] {
			t.Errorf("caso %d: ManifestReference() = %q, esperava %q", i, got, want[i])
		}
	}
}
