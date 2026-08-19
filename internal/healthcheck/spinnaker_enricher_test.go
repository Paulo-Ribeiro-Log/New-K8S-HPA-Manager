package healthcheck

import (
	"testing"
	"time"

	"k8s-hpa-manager/internal/spinnaker"
)

func TestDeriveSpinnakerEnvGo(t *testing.T) {
	cases := []struct {
		cluster string
		want    string
	}{
		{"akspriv-logreversa-prd-admin", "prd"},
		{"akspriv-logreversa-hlg-admin", "hlg"},
		{"akspriv-logreversa-prd", "prd"},
		{"akspriv-tracking-hlg", "hlg"},
		{"akspriv-tracking-sit", ""}, // sem Gate Spinnaker próprio confirmado
		{"akspriv-tracking-stg", ""}, // idem
		{"gke_project_us-central1_x", ""},
	}
	for _, c := range cases {
		if got := deriveSpinnakerEnvGo(c.cluster); got != c.want {
			t.Errorf("deriveSpinnakerEnvGo(%q) = %q, want %q", c.cluster, got, c.want)
		}
	}
}

// mkTriggerHC é um mkTrigger local (não reaproveita o do pacote spinnaker, que é interno a ele)
// — só o suficiente pra montar Execution reais nesse contexto de teste.
func mkTriggerHC(nameApp, namespace, version, chg string) spinnaker.Trigger {
	return spinnaker.Trigger{
		Parameters: map[string]interface{}{
			"Application Name":             nameApp,
			"Application K8S Namespace":    namespace,
			"Application Version":          version,
			"Application SN Change Number": chg,
		},
	}
}

// TestSpinnakerEnricher_RecentRollback_DentroDaJanela cobre o caso principal: rollback
// explícito recente (dentro de spinnakerRecentRollbackWindow) deve ser retornado.
func TestSpinnakerEnricher_RecentRollback_DentroDaJanela(t *testing.T) {
	now := time.Now()
	recentMs := now.Add(-2 * time.Hour).UnixMilli()

	e := &SpinnakerEnricher{executionsByApp: map[string][]spinnaker.Execution{
		"logreversa": {
			{
				ID: "rollback-exec", Name: "rollback-aks-global", Status: "SUCCEEDED",
				StartTime: recentMs, EndTime: recentMs + 1000,
				Trigger: mkTriggerHC("app-x", "ns-x", "1.0.0", "CHG0001111"),
			},
		},
	}}

	got := e.RecentRollback("app-x", "ns-x", "1.0.0")
	if got == nil {
		t.Fatal("esperava rollback detectado")
	}
	if got.FailedCHG != "CHG0001111" {
		t.Errorf("FailedCHG = %q, want CHG0001111", got.FailedCHG)
	}
}

// TestSpinnakerEnricher_RecentRollback_ForaDaJanela garante que um rollback antigo demais
// (além de spinnakerRecentRollbackWindow) NÃO conta como "recente".
func TestSpinnakerEnricher_RecentRollback_ForaDaJanela(t *testing.T) {
	oldMs := time.Now().Add(-72 * time.Hour).UnixMilli() // 72h > janela de 48h

	e := &SpinnakerEnricher{executionsByApp: map[string][]spinnaker.Execution{
		"logreversa": {
			{
				ID: "rollback-exec-antigo", Name: "rollback-aks-global", Status: "SUCCEEDED",
				StartTime: oldMs, EndTime: oldMs + 1000,
				Trigger: mkTriggerHC("app-x", "ns-x", "1.0.0", "CHG0001111"),
			},
		},
	}}

	if got := e.RecentRollback("app-x", "ns-x", "1.0.0"); got != nil {
		t.Errorf("RecentRollback() = %+v, esperava nil (rollback fora da janela de 48h)", got)
	}
}

// TestSpinnakerEnricher_RecentRollback_DeployNormalNaoConta garante que um deploy normal
// (não-rollback) nunca é reportado como "rollback recente".
func TestSpinnakerEnricher_RecentRollback_DeployNormalNaoConta(t *testing.T) {
	recentMs := time.Now().Add(-1 * time.Hour).UnixMilli()

	e := &SpinnakerEnricher{executionsByApp: map[string][]spinnaker.Execution{
		"logreversa": {
			{
				ID: "deploy-ok", Name: "deploy-aks-global", Status: "SUCCEEDED",
				StartTime: recentMs,
				Trigger:   mkTriggerHC("app-x", "ns-x", "1.0.0", "CHG0001111"),
			},
		},
	}}

	if got := e.RecentRollback("app-x", "ns-x", "1.0.0"); got != nil {
		t.Errorf("RecentRollback() = %+v, esperava nil (deploy normal, não é rollback)", got)
	}
}

// TestSpinnakerEnricher_RecentRollback_NilEnricherNaoPanica garante que chamar RecentRollback
// num *SpinnakerEnricher nil (caso comum — enricher não configurado) não panica.
func TestSpinnakerEnricher_RecentRollback_NilEnricherNaoPanica(t *testing.T) {
	var e *SpinnakerEnricher
	if got := e.RecentRollback("app-x", "ns-x", "1.0.0"); got != nil {
		t.Errorf("RecentRollback() em enricher nil = %+v, esperava nil", got)
	}
}
