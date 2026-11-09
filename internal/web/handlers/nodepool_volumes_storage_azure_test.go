package handlers

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func fakeAz(t *testing.T, routes map[string]string) func() {
	t.Helper()
	old := azRestGet
	azRestGet = func(_ context.Context, u string) ([]byte, error) {
		if strings.Contains(u, "geoReplicationStats") && !strings.Contains(u, "geoOK") {
			// como a Azure: o expand só é aceito em conta geo-replicada (rota do teste marca com "geoOK")
			return nil, fmt.Errorf(`Bad Request({"error":{"code":"InvalidAccountType","message":"AccountType Standard_LRS is invalid."}})`)
		}
		for frag, body := range routes {
			if strings.Contains(u, frag) {
				if body == "" {
					return nil, fmt.Errorf("403 Forbidden")
				}
				return []byte(body), nil
			}
		}
		return nil, fmt.Errorf("rota inesperada: %s", u)
	}
	return func() { azRestGet = old }
}

func TestAzureTargetFor(t *testing.T) {
	sub := "11111111-2222-3333-4444-555555555555"
	if _, err := azureTargetFor(&StorageInfo{Driver: "ebs.csi.aws.com"}, sub); err == nil {
		t.Error("EBS não é Azure")
	}
	if _, err := azureTargetFor(&StorageInfo{Driver: "file.csi.azure.com"}, sub); err == nil {
		t.Error("sem conta deveria falhar")
	}
	if _, err := azureTargetFor(&StorageInfo{Driver: "file.csi.azure.com", Account: "bad;name"}, sub); err == nil {
		t.Error("nome de conta inválido deveria falhar")
	}
	if _, err := azureTargetFor(&StorageInfo{Driver: "file.csi.azure.com", Account: "stfoo", Share: "../x"}, sub); err == nil {
		t.Error("share inválido deveria falhar")
	}
	tg, err := azureTargetFor(&StorageInfo{Driver: "file.csi.azure.com", Account: "stfoo", ResourceGroup: "rg-a", Share: "s1"}, sub)
	if err != nil || tg.kind != "storage" || tg.sub != sub || tg.share != "s1" {
		t.Fatalf("%+v %v", tg, err)
	}
	id := "/subscriptions/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee/resourceGroups/MC_rg_aks/providers/Microsoft.Compute/disks/pvc-1"
	tg, err = azureTargetFor(&StorageInfo{Driver: "disk.csi.azure.com", ResourceID: id}, sub)
	if err != nil || tg.kind != "disk" || tg.sub != "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee" {
		t.Fatalf("%+v %v", tg, err)
	}
	if _, err := azureTargetFor(&StorageInfo{Driver: "disk.csi.azure.com", ResourceID: "/x/y"}, sub); err == nil {
		t.Error("ID de disco inválido deveria falhar")
	}
}

func TestCollectAzureDetailsFileShare(t *testing.T) {
	defer fakeAz(t, map[string]string{
		"/fileServices/default/shares/s1": `{"properties":{"accessTier":"Hot","shareQuota":100,"shareUsageBytes":96636764160,"enabledProtocols":"NFS"}}`,
		"/fileServices/default?":          `{"properties":{"shareDeleteRetentionPolicy":{"enabled":false}}}`,
		"/storageAccounts/stfoo?":         `{"kind":"FileStorage","location":"brazilsouth","sku":{"name":"Premium_ZRS"},"properties":{"accessTier":"Premium","minimumTlsVersion":"TLS1_0","networkAcls":{"defaultAction":"Allow"},"publicNetworkAccess":"Enabled"}}`,
	})()
	d := collectAzureDetails(context.Background(), azTarget{kind: "storage", driver: "file.csi.azure.com", sub: "s", rg: "rg", account: "stfoo", share: "s1"})
	if len(d.Warnings) != 0 {
		t.Fatalf("avisos: %v", d.Warnings)
	}
	var filesNA bool
	for _, sec := range d.Sections {
		if sec.Title == "Ciclo de vida (regras)" && strings.Contains(sec.Fields[0].Value, "Azure Files") {
			filesNA = true
		}
	}
	if !filesNA {
		t.Errorf("Azure Files deveria dizer que não tem ciclo de vida: %+v", d.Sections)
	}
	var kind, tier bool
	for _, s := range d.Sections {
		for _, f := range s.Fields {
			if f.Label == "Kind" && f.Value == "FileStorage" {
				kind = true
			}
			if s.Title == "File share" && f.Label == "Tier" && f.Value == "Hot" {
				tier = true
			}
		}
	}
	if !kind || !tier {
		t.Fatalf("faltou kind/tier: %+v", d.Sections)
	}
	joined := strings.Join(d.Notes, "|")
	for _, want := range []string{"TLS mínimo", "Sem firewall", "soft delete desligado", "90%"} {
		if !strings.Contains(joined, want) {
			t.Errorf("faltou nota %q em %q", want, joined)
		}
	}
}

func TestCollectAzureDetailsLRSNaoPedeExpandGeo(t *testing.T) {
	defer fakeAz(t, map[string]string{
		"/storageAccounts/stfoo?":      `{"kind":"StorageV2","sku":{"name":"Standard_LRS"},"properties":{"accessTier":"Hot"}}`,
		"/blobServices/default?":       `{"properties":{"deleteRetentionPolicy":{"enabled":true,"days":7}}}`,
		"/managementPolicies/default?": lifecyclePolicyJSON,
	})()
	d := collectAzureDetails(context.Background(), azTarget{kind: "storage", driver: "blob.csi.azure.com", sub: "s", rg: "rg", account: "stfoo"})
	if len(d.Warnings) != 0 || len(d.Sections) == 0 {
		t.Fatalf("conta LRS não deveria dar aviso: warnings=%v sections=%d", d.Warnings, len(d.Sections))
	}
	var lifecycle bool
	for _, sec := range d.Sections {
		if sec.Title == "Ciclo de vida (regras)" && len(sec.Fields) == 2 {
			lifecycle = true
		}
	}
	if !lifecycle {
		t.Errorf("conta de blob deveria trazer a seção de ciclo de vida: %+v", d.Sections)
	}
}

func TestCollectAzureDetailsSemPoliticaDeCicloDeVida(t *testing.T) {
	// sem a rota managementPolicies o fake responde erro; aqui simulamos o 404 real do ARM
	old := azRestGet
	defer func() { azRestGet = old }()
	azRestGet = func(_ context.Context, u string) ([]byte, error) {
		switch {
		case strings.Contains(u, "/managementPolicies/"):
			return nil, fmt.Errorf(`ERROR: Not Found({"error":{"code":"ManagementPolicyNotFound"}})`)
		case strings.Contains(u, "/blobServices/default?"):
			return []byte(`{"properties":{}}`), nil
		default:
			return []byte(`{"kind":"StorageV2","sku":{"name":"Standard_LRS"}}`), nil
		}
	}
	d := collectAzureDetails(context.Background(), azTarget{kind: "storage", driver: "blob.csi.azure.com", sub: "s", rg: "rg", account: "stfoo"})
	if len(d.Warnings) != 0 {
		t.Fatalf("ausência de política não é aviso de falha: %v", d.Warnings)
	}
	if !strings.Contains(strings.Join(d.Notes, "|"), "sem política") {
		t.Errorf("deveria avisar que não há política: %v", d.Notes)
	}
}

func TestIsGeoSKU(t *testing.T) {
	for sku, want := range map[string]bool{"Standard_LRS": false, "Premium_ZRS": false, "Standard_GRS": true, "Standard_RAGRS": true, "Standard_GZRS": true, "Standard_RAGZRS": true, "": false} {
		if isGeoSKU(sku) != want {
			t.Errorf("%q: esperava %v", sku, want)
		}
	}
}

func TestCollectAzureDetailsPartialFailure(t *testing.T) {
	defer fakeAz(t, map[string]string{
		"/storageAccounts/stfoo?":      `{"kind":"StorageV2","sku":{"name":"Standard_LRS"}}`,
		"/blobServices/default?":       "", // 403
		"/managementPolicies/default?": lifecyclePolicyJSON,
	})()
	d := collectAzureDetails(context.Background(), azTarget{kind: "storage", driver: "blob.csi.azure.com", sub: "s", rg: "rg", account: "stfoo"})
	if len(d.Sections) == 0 || len(d.Warnings) != 1 || !strings.Contains(d.Warnings[0], "Proteção de dados") {
		t.Fatalf("sections=%d warnings=%v", len(d.Sections), d.Warnings)
	}
}

func TestCollectAzureDetailsDisk(t *testing.T) {
	defer fakeAz(t, map[string]string{"/disks/pvc-1": `{"sku":{"name":"Premium_LRS"},"location":"brazilsouth","zones":["1"],"properties":{"diskSizeGB":128,"diskState":"Unattached","tier":"P10"}}`})()
	id := "/subscriptions/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee/resourceGroups/rg/providers/Microsoft.Compute/disks/pvc-1"
	d := collectAzureDetails(context.Background(), azTarget{kind: "disk", diskID: id})
	if len(d.Sections) != 2 || !strings.Contains(strings.Join(d.Notes, "|"), "Unattached") {
		t.Fatalf("%+v", d)
	}
	// disco não tem ciclo de vida: a tela diz isso em vez de ficar muda
	if lc := d.Sections[1]; lc.Title != "Ciclo de vida (regras)" || !strings.HasPrefix(lc.Fields[0].Value, "não") || !strings.Contains(lc.Fields[0].Value, "disco gerenciado") {
		t.Errorf("seção de ciclo de vida do disco: %+v", lc)
	}
}

func TestDiskNotFound(t *testing.T) {
	err := fmt.Errorf(`ERROR: Not Found({"error":{"code":"ResourceNotFound","message":"The Resource 'Microsoft.Compute/disks/x' was not found."}})`)
	if !diskNotFound(err) {
		t.Error("ResourceNotFound do ARM deveria ser reconhecido")
	}
	if diskNotFound(fmt.Errorf("ERROR: Forbidden(AuthorizationFailed)")) {
		t.Error("AuthorizationFailed não é disco inexistente")
	}
}
