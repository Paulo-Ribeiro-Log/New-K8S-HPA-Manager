package handlers

import (
	"testing"
	"time"

	"k8s-hpa-manager/internal/notifications"
	"k8s-hpa-manager/internal/spinnaker"
	"k8s-hpa-manager/internal/storage"
)

func mkRollbackInfo(isRollback bool, executionID string) *spinnaker.RollbackInfo {
	return &spinnaker.RollbackInfo{
		Matched:              true,
		IsRollback:           &isRollback,
		SpinnakerExecutionID: executionID,
	}
}

func TestIsNewFailureSignal_NuncaVistoAntes(t *testing.T) {
	// Achado real que motivou este mecanismo: um rollback em PRD já em curso há dias, mas nunca
	// checado por este watcher antes — precisa notificar na primeira vez que vê, não esperar uma
	// "mudança" que nunca vai vir (o deployment já está parado nesse estado).
	info := mkRollbackInfo(true, "exec-1")
	if !isNewFailureSignal(nil, info) {
		t.Error("esperava notificar na primeira vez que vê um deployment já em falha")
	}
}

func TestIsNewFailureSignal_MesmaExecucaoNaoRenotifica(t *testing.T) {
	prev := &storage.SpinnakerRolloutRecord{IsRollback: true, SpinnakerExecutionID: "exec-1"}
	info := mkRollbackInfo(true, "exec-1")
	if isNewFailureSignal(prev, info) {
		t.Error("não deveria renotificar a mesma execução já vista (evita spam a cada tick de 10min)")
	}
}

func TestIsNewFailureSignal_NovaExecucaoFalhaNotifica(t *testing.T) {
	// Deployment já estava em falha (exec-1), mas uma NOVA tentativa de deploy falhou também
	// (exec-2) — merece um aviso novo, é uma falha diferente.
	prev := &storage.SpinnakerRolloutRecord{IsRollback: true, SpinnakerExecutionID: "exec-1"}
	info := mkRollbackInfo(true, "exec-2")
	if !isNewFailureSignal(prev, info) {
		t.Error("esperava notificar uma execução de falha diferente da última notificada")
	}
}

func TestIsNewFailureSignal_VoltouASucessoDepoisFalhaDeNovoNotifica(t *testing.T) {
	// Deployment tinha se recuperado (deploy correto aplicado, is_rollback=false persistido) e
	// depois falhou de novo com o MESMO ID de execução por algum motivo hipotético — não deveria
	// acontecer na prática (execução SUCCEEDED some do "latest" quando há deploy novo), mas o
	// código não deve depender disso: se o último estado conhecido não era falha, sempre notifica.
	prev := &storage.SpinnakerRolloutRecord{IsRollback: false, SpinnakerExecutionID: "exec-1"}
	info := mkRollbackInfo(true, "exec-1")
	if !isNewFailureSignal(prev, info) {
		t.Error("esperava notificar quando o último estado conhecido não era de falha")
	}
}

func TestIsNewFailureSignal_DeployNormalNuncaNotifica(t *testing.T) {
	info := mkRollbackInfo(false, "exec-3")
	if isNewFailureSignal(nil, info) {
		t.Error("deploy normal (is_rollback=false) nunca deve gerar notificação")
	}
	prev := &storage.SpinnakerRolloutRecord{IsRollback: true, SpinnakerExecutionID: "exec-2"}
	if isNewFailureSignal(prev, info) {
		t.Error("deploy normal nunca deve gerar notificação, mesmo vindo depois de uma falha conhecida")
	}
}

// TestNotifyStageFailureIfNew_DedupPorExecutionID reproduz o achado real (relatado ao vivo pelo
// usuário): pipeline com retry (etapa falha, Spinnaker tenta de novo, eventualmente sucede) tem
// que gerar notificação — mesmo com Matched:true/IsRollback:false ("deploy normal"), que o
// caminho de notify() antigo nunca cobria. Confirma também que a MESMA falha não é renotificada
// a cada tick (dedup por ExecutionID), mas uma falha DIFERENTE (execução nova) é.
func TestNotifyStageFailureIfNew_DedupPorExecutionID(t *testing.T) {
	nm := notifications.NewNotificationManager()
	w := NewSpinnakerFleetWatcher(t.TempDir(), nil, nil, nm, nil)

	now := time.Now().UnixMilli()
	info := &spinnaker.RollbackInfo{
		Matched: true,
		RecentStageFailures: []spinnaker.StageFailureSummary{
			{ExecutionID: "exec-1", ExecutionTime: now, StageName: "deploy-helm", StageStatus: "FAILED_CONTINUE", Log: "BackoffLimitExceeded"},
		},
	}

	w.notifyStageFailureIfNew("cluster-a", "ns-a", "app-a", "hlg", info)
	if got := nm.GetInAppNotifier().GetUnreadCount(); got != 1 {
		t.Fatalf("esperava 1 notificação após a 1ª chamada, veio %d", got)
	}

	// Mesma execução de novo (ex: próximo tick do watcher, ninguém corrigiu ainda) — não deve
	// gerar uma 2ª notificação.
	w.notifyStageFailureIfNew("cluster-a", "ns-a", "app-a", "hlg", info)
	if got := nm.GetInAppNotifier().GetUnreadCount(); got != 1 {
		t.Fatalf("não deveria renotificar a mesma execução (exec-1), contagem = %d", got)
	}

	// Execução DIFERENTE (nova falha de retry) — deve notificar de novo.
	info2 := &spinnaker.RollbackInfo{
		Matched: true,
		RecentStageFailures: []spinnaker.StageFailureSummary{
			{ExecutionID: "exec-2", ExecutionTime: now, StageName: "deploy-helm", StageStatus: "FAILED_CONTINUE", Log: "outro erro"},
		},
	}
	w.notifyStageFailureIfNew("cluster-a", "ns-a", "app-a", "hlg", info2)
	if got := nm.GetInAppNotifier().GetUnreadCount(); got != 2 {
		t.Fatalf("esperava notificar uma execução de falha diferente, contagem = %d", got)
	}
}

func TestNotifyStageFailureIfNew_SemFalhaNaoNotifica(t *testing.T) {
	nm := notifications.NewNotificationManager()
	w := NewSpinnakerFleetWatcher(t.TempDir(), nil, nil, nm, nil)

	w.notifyStageFailureIfNew("cluster-a", "ns-a", "app-a", "hlg", &spinnaker.RollbackInfo{Matched: false})
	if got := nm.GetInAppNotifier().GetUnreadCount(); got != 0 {
		t.Fatalf("sem RecentStageFailures não deveria notificar nada, contagem = %d", got)
	}
}

// TestNotifyStageFailureIfNew_FalhaAntigaNaoNotifica reproduz a 2ª rodada do achado real:
// RecentStageFailures (dado exposto pro badge/modal) não filtra mais por idade — precisaria
// senão sumir do card da lista (usuário pediu explicitamente pra não tirar isso). Mas não faz
// sentido EMPURRAR uma notificação sobre uma falha de semanas atrás — só a notificação continua
// gateada por spinnaker.StageFailureRecentWindow (48h).
func TestNotifyStageFailureIfNew_FalhaAntigaNaoNotifica(t *testing.T) {
	nm := notifications.NewNotificationManager()
	w := NewSpinnakerFleetWatcher(t.TempDir(), nil, nil, nm, nil)

	old := time.Now().Add(-30 * 24 * time.Hour).UnixMilli() // ~1 mês atrás
	info := &spinnaker.RollbackInfo{
		Matched: true,
		RecentStageFailures: []spinnaker.StageFailureSummary{
			{ExecutionID: "exec-antigo", ExecutionTime: old, StageName: "deploy-helm", StageStatus: "FAILED_CONTINUE", Log: "falha antiga"},
		},
	}

	w.notifyStageFailureIfNew("cluster-a", "ns-a", "app-a", "hlg", info)
	if got := nm.GetInAppNotifier().GetUnreadCount(); got != 0 {
		t.Fatalf("falha de ~1 mês atrás não deveria gerar notificação (janela é %s), contagem = %d", spinnaker.StageFailureRecentWindow, got)
	}
}

func TestIsNewFailureSignal_InfoSemIsRollbackNuncaNotifica(t *testing.T) {
	// IsRollback nil = "não determinado" (Matched=false, ou estado indeterminado) — nunca deve
	// ser tratado como falha por omissão (mesmo princípio documentado em RollbackInfo.IsRollback).
	info := &spinnaker.RollbackInfo{Matched: true, SpinnakerExecutionID: "exec-4"}
	if isNewFailureSignal(nil, info) {
		t.Error("IsRollback nil nunca deve ser inferido como falha")
	}
}
