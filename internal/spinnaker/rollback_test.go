package spinnaker

import "testing"

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
