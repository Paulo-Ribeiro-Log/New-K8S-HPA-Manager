package handlers

import (
	"testing"
	"time"

	"k8s-hpa-manager/internal/spinnaker"
	"k8s-hpa-manager/internal/storage"
)

// TestApplyRegistryFreshness_* cobrem o achado real que motivou este mecanismo, em 2 rodadas:
// (1) um Deployment Registry desatualizado (rec.LastSeen antigo) fazia DetectRollback cair num
// Matched:false silencioso, indistinguível de "sem sinal nenhum" (usuário relatou "em hlg
// simplesmente não funciona" — reproduzido ao vivo contra entrega-mais-bff/entrega-mais-hlg);
// (2) a 1ª correção (limiar de 2h contra o relógio) super-generalizou — usuário relatou "todos
// ficaram como dados desatualizados, até mesmo os que conhecidamente foram executados hoje" — a
// maioria dos deployments não muda por dias/semanas e tinha last_seen "velho" com dado correto.
// Corrigido comparando contra LatestKnownExecutionAt (preenchido por DetectRollback) em vez do
// relógio — só marca stale quando existe uma execução Spinnaker conhecida mais nova que a última
// leitura do registry.
func TestApplyRegistryFreshness_SemExecucaoConhecidaNuncaMarcaComoDesatualizado(t *testing.T) {
	// LatestKnownExecutionAt==0 (nunca preenchido, ex: DetectRollback nem achou execução nenhuma
	// pra esse nameApp/namespace) — mesmo com rec.LastSeen bem antigo, não há nada pra comparar.
	info := &spinnaker.RollbackInfo{}
	rec := storage.DeploymentRecord{LastSeen: time.Now().Add(-30 * 24 * time.Hour)}
	applyRegistryFreshness(info, rec)
	if info.RegistryStale {
		t.Error("sem LatestKnownExecutionAt não há base de comparação — nunca deveria marcar como desatualizado")
	}
	if info.RegistryLastSeen != rec.LastSeen.UnixMilli() {
		t.Errorf("RegistryLastSeen = %d, esperava %d", info.RegistryLastSeen, rec.LastSeen.UnixMilli())
	}
}

func TestApplyRegistryFreshness_DeploymentAntigoSemMudancaNaoMarcaComoDesatualizado(t *testing.T) {
	// Acha o caso relatado na 2ª rodada: deployment que não muda há semanas — LatestKnownExecutionAt
	// é antigo, mas ANTERIOR ao último scan do registry (que já viu esse estado). Não é desatualizado.
	oldExecution := time.Now().Add(-60 * 24 * time.Hour)
	lastSeen := time.Now().Add(-25 * time.Hour) // scan não roda toda hora, e tudo bem
	info := &spinnaker.RollbackInfo{LatestKnownExecutionAt: oldExecution.UnixMilli()}
	applyRegistryFreshness(info, storage.DeploymentRecord{LastSeen: lastSeen})
	if info.RegistryStale {
		t.Error("execução conhecida mais antiga que o último scan não deveria marcar como desatualizado, mesmo com last_seen \"velho\"")
	}
}

func TestApplyRegistryFreshness_ExecucaoNovaQueRegistryAindaNaoViuMarcaComoDesatualizado(t *testing.T) {
	// O caso real que motivou o mecanismo: Spinnaker já sabe de uma execução mais nova (ex: deploy
	// de hoje) do que a última vez que o registry foi lido (ex: ontem).
	newExecution := time.Now()
	lastSeen := time.Now().Add(-24 * time.Hour)
	info := &spinnaker.RollbackInfo{LatestKnownExecutionAt: newExecution.UnixMilli()}
	applyRegistryFreshness(info, storage.DeploymentRecord{LastSeen: lastSeen})
	if !info.RegistryStale {
		t.Error("execução Spinnaker mais nova que o último scan do registry deveria marcar como desatualizado")
	}
}

func TestApplyRegistryFreshness_LastSeenZeroNaoAlteraInfo(t *testing.T) {
	// DeploymentRecord.LastSeen zero-value (nunca deveria acontecer num record real, mas
	// applyRegistryFreshness não deve inventar um "desatualizado" a partir de um dado ausente —
	// mesmo princípio de "nunca inferir por omissão" já documentado no resto do pacote.
	info := &spinnaker.RollbackInfo{}
	applyRegistryFreshness(info, storage.DeploymentRecord{})
	if info.RegistryStale || info.RegistryLastSeen != 0 {
		t.Errorf("LastSeen zero-value não deveria alterar info, veio %+v", info)
	}
}

// TestToHistoryRecord_RoundTripsThroughFromHistoryRecord garante que persistir um RollbackInfo
// e reconstruí-lo depois (fallback quando o Gate não devolve nada dentro da janela atual)
// preserva os campos usados pelo frontend, e que fromHistoryRecord sempre marca FromCache.
func TestToHistoryRecord_RoundTripsThroughFromHistoryRecord(t *testing.T) {
	isRollback := true
	original := &spinnaker.RollbackInfo{
		Matched:               true,
		IsRollback:            &isRollback,
		RollbackType:          "implicit",
		LastCHGApplied:        "CHG0001234",
		LastCHGAppliedURL:     "https://viavarejo.service-now.com/change_request.do?sys_id=abc",
		PipelineExecutedAt:    1000,
		ExecutionStatus:       "TERMINAL",
		RollbackStartedAt:     1000,
		RollbackEndedAt:       2000,
		FailedCHG:             "CHG0001234",
		FailedCHGURL:          "https://viavarejo.service-now.com/change_request.do?sys_id=abc",
		RollbackPipelineName:  "deploy-aks-global",
		SpinnakerExecutionID:  "exec-1",
		SpinnakerExecutionURL: "https://deck/exec-1",
	}

	rec := toHistoryRecord("cluster-x", "ns-x", "deploy-x", original)
	if rec.Cluster != "cluster-x" || rec.Namespace != "ns-x" || rec.DeploymentName != "deploy-x" {
		t.Fatalf("chave incorreta: %+v", rec)
	}
	if !rec.IsRollback {
		t.Error("IsRollback deveria ser true")
	}
	if rec.LastCHGApplied != "CHG0001234" {
		t.Errorf("LastCHGApplied = %q, want CHG0001234", rec.LastCHGApplied)
	}

	// UpdatedAt é preenchido pelo Upsert real (não por toHistoryRecord) — zero value aqui é
	// esperado, fromHistoryRecord só precisa não panicar e propagar o que tiver.
	rebuilt := fromHistoryRecord(rec)

	if !rebuilt.Matched {
		t.Error("fromHistoryRecord deveria sempre marcar Matched=true")
	}
	if !rebuilt.FromCache {
		t.Error("fromHistoryRecord deveria sempre marcar FromCache=true")
	}
	if rebuilt.IsRollback == nil || *rebuilt.IsRollback != true {
		t.Errorf("IsRollback = %v, want true", rebuilt.IsRollback)
	}
	if rebuilt.LastCHGApplied != original.LastCHGApplied {
		t.Errorf("LastCHGApplied = %q, want %q", rebuilt.LastCHGApplied, original.LastCHGApplied)
	}
	if rebuilt.ExecutionStatus != original.ExecutionStatus {
		t.Errorf("ExecutionStatus = %q, want %q", rebuilt.ExecutionStatus, original.ExecutionStatus)
	}
	if rebuilt.SpinnakerExecutionURL != original.SpinnakerExecutionURL {
		t.Errorf("SpinnakerExecutionURL = %q, want %q", rebuilt.SpinnakerExecutionURL, original.SpinnakerExecutionURL)
	}
	if rebuilt.LastCHGAppliedURL != original.LastCHGAppliedURL {
		t.Errorf("LastCHGAppliedURL = %q, want %q", rebuilt.LastCHGAppliedURL, original.LastCHGAppliedURL)
	}
	if rebuilt.FailedCHGURL != original.FailedCHGURL {
		t.Errorf("FailedCHGURL = %q, want %q", rebuilt.FailedCHGURL, original.FailedCHGURL)
	}
}

// TestToHistoryRecord_NilIsRollback cobre o caso defensivo — DetectRollback nunca deveria
// retornar Matched=true com IsRollback nil, mas toHistoryRecord não deve panicar se acontecer.
func TestToHistoryRecord_NilIsRollback(t *testing.T) {
	info := &spinnaker.RollbackInfo{Matched: true, IsRollback: nil}
	rec := toHistoryRecord("c", "ns", "app", info)
	if rec.IsRollback {
		t.Error("IsRollback deveria ser false quando o ponteiro original é nil")
	}
}

// TestFromHistoryRecord_SetsCachedAtFromUpdatedAt confirma que o timestamp exposto ao frontend
// (CachedAt, epoch ms) vem do UpdatedAt persistido, não de time.Now() no momento da leitura —
// senão o frontend nunca saberia há quanto tempo o dado realmente é.
func TestFromHistoryRecord_SetsCachedAtFromUpdatedAt(t *testing.T) {
	rec := storage.SpinnakerRolloutRecord{
		Cluster: "c", Namespace: "ns", DeploymentName: "app",
	}
	rec.UpdatedAt = rec.UpdatedAt.AddDate(0, 0, -30) // ~30 dias atrás (zero value + delta, só pra ter um valor não-atual)

	info := fromHistoryRecord(rec)
	if info.CachedAt != rec.UpdatedAt.UnixMilli() {
		t.Errorf("CachedAt = %d, want %d (UpdatedAt.UnixMilli())", info.CachedAt, rec.UpdatedAt.UnixMilli())
	}
}
