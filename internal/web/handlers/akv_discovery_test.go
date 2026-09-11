package handlers

import (
	"encoding/json"
	"testing"
)

// realExternalSecretJSON — schema real confirmado ao vivo contra um cluster de produção durante
// a investigação que motivou esta ferramenta (ver AKV-SECRET-VIEWER-STUDY.md).
const realExternalSecretJSON = `{
	"metadata": {"name": "akv-tms-prd-tms-embarcador-prd"},
	"spec": {
		"secretStoreRef": {"kind": "ClusterSecretStore", "name": "akv-tms-prd"},
		"target": {"name": "akv-tms-embarcador-prd"},
		"dataFrom": [{
			"find": {"name": {"regexp": "(?i)tmsembarcadorprd$"}},
			"rewrite": [{"regexp": {"source": "(?i)^TmsPrd-([^-]+)-([^-]+)-(.*)-tmsembarcadorprd$", "target": "$1-$2-$3"}}]
		}]
	},
	"status": {
		"conditions": [{"status": "False", "reason": "SecretSyncedError", "message": "could not get secret data from provider"}]
	}
}`

func TestSummarizeExternalSecret_RealSchema(t *testing.T) {
	var raw externalSecretRaw
	if err := json.Unmarshal([]byte(realExternalSecretJSON), &raw); err != nil {
		t.Fatalf("falha ao parsear JSON de teste: %v", err)
	}

	summary := summarizeExternalSecret(raw)

	if summary.Name != "akv-tms-prd-tms-embarcador-prd" {
		t.Errorf("Name = %q, esperado akv-tms-prd-tms-embarcador-prd", summary.Name)
	}
	if summary.SecretStoreKind != "ClusterSecretStore" || summary.SecretStoreName != "akv-tms-prd" {
		t.Errorf("SecretStoreRef = %s/%s, esperado ClusterSecretStore/akv-tms-prd", summary.SecretStoreKind, summary.SecretStoreName)
	}
	if summary.TargetName != "akv-tms-embarcador-prd" {
		t.Errorf("TargetName = %q, esperado akv-tms-embarcador-prd", summary.TargetName)
	}
	if summary.FindRegexp != "(?i)tmsembarcadorprd$" {
		t.Errorf("FindRegexp = %q", summary.FindRegexp)
	}
	if summary.RewriteSource != "(?i)^TmsPrd-([^-]+)-([^-]+)-(.*)-tmsembarcadorprd$" || summary.RewriteTarget != "$1-$2-$3" {
		t.Errorf("Rewrite = %q -> %q", summary.RewriteSource, summary.RewriteTarget)
	}
	if summary.Ready {
		t.Error("Ready deveria ser false (status.conditions[0].status == False)")
	}
	if summary.StatusReason != "SecretSyncedError" {
		t.Errorf("StatusReason = %q, esperado SecretSyncedError", summary.StatusReason)
	}
}

func TestSummarizeExternalSecret_ReadyTrue(t *testing.T) {
	raw := externalSecretRaw{}
	raw.Metadata.Name = "akv-example"
	raw.Status.Conditions = []struct {
		Status  string `json:"status"`
		Reason  string `json:"reason"`
		Message string `json:"message"`
	}{{Status: "True", Reason: "SecretSynced", Message: "reconciliation complete"}}

	summary := summarizeExternalSecret(raw)
	if !summary.Ready {
		t.Error("Ready deveria ser true quando status.conditions[0].status == True")
	}
}

func TestSummarizeExternalSecret_NoDataFromNoConditions(t *testing.T) {
	raw := externalSecretRaw{}
	raw.Metadata.Name = "akv-empty"

	summary := summarizeExternalSecret(raw)
	if summary.FindRegexp != "" || summary.RewriteSource != "" {
		t.Error("sem dataFrom, FindRegexp/RewriteSource devem ficar vazios")
	}
	if summary.Ready {
		t.Error("sem conditions, Ready deve ficar false (zero-value)")
	}
}

func TestRandomDiscoveryName_Unique(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		name := randomDiscoveryName()
		if seen[name] {
			t.Fatalf("nome duplicado gerado: %s", name)
		}
		seen[name] = true
		if len(name) == 0 {
			t.Fatal("nome vazio gerado")
		}
	}
}
