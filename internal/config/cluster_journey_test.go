package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestClusterConfig_Journey(t *testing.T) {
	tests := []struct {
		name string
		tags map[string]string
		want string
	}{
		{name: "sem tags", tags: nil, want: ""},
		{name: "tags vazio", tags: map[string]string{}, want: ""},
		{name: "chave exata", tags: map[string]string{"jornada": "logistica"}, want: "logistica"},
		{name: "chave com caixa diferente", tags: map[string]string{"Jornada": "backoffice"}, want: "backoffice"},
		{name: "chave toda maiúscula", tags: map[string]string{"JORNADA": "backoffice"}, want: "backoffice"},
		{name: "sem a tag jornada, outras presentes", tags: map[string]string{"squad": "SRE", "env": "prd"}, want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := ClusterConfig{Tags: tt.tags}
			if got := c.Journey(); got != tt.want {
				t.Errorf("Journey() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestAKSListEntry_UnmarshalsTags confirma que o shape do "--query
// [].{name:name,resourceGroup:resourceGroup,id:id,tags:tags}" (az aks list) mapeia
// corretamente pra aksListEntry.Tags — a parte realmente arriscada dessa mudança, já que o
// resto (buildAKSClusterIndex em si) depende de exec.Command("az", ...) e não é mockado nesta
// suíte (mesma convenção já usada no resto do pacote — validação ao vivo pra código que
// depende de CLI externo).
func TestAKSListEntry_UnmarshalsTags(t *testing.T) {
	raw := `[
		{"name":"aks-logistica","resourceGroup":"rg-log","id":"/subscriptions/sub1/resourceGroups/rg-log/providers/Microsoft.ContainerService/managedClusters/aks-logistica","tags":{"jornada":"logistica","squad":"SRE"}},
		{"name":"aks-sem-tag","resourceGroup":"rg-x","id":"/subscriptions/sub1/resourceGroups/rg-x/providers/Microsoft.ContainerService/managedClusters/aks-sem-tag","tags":null}
	]`

	var entries []aksListEntry
	if err := json.Unmarshal([]byte(raw), &entries); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2", len(entries))
	}
	if got := entries[0].Tags["jornada"]; got != "logistica" {
		t.Errorf("entries[0].Tags[jornada] = %q, want %q", got, "logistica")
	}
	if entries[1].Tags != nil {
		t.Errorf("entries[1].Tags = %v, want nil (tags:null no az aks list)", entries[1].Tags)
	}
}

// TestGetAllClusterConfigs_RoundTripsTags confirma que Tags sobrevive a
// escrever/ler clusters-config.json (mesmo arquivo que SaveClusterConfigs grava e
// loadClustersFromConfig/GetAllClusterConfigs lê).
func TestGetAllClusterConfigs_RoundTripsTags(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	dir := filepath.Join(home, ".k8s-hpa-manager")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	fixture := []ClusterConfig{
		{Name: "aks-backoffice", ResourceGroup: "rg-bo", Subscription: "PRD", Tags: map[string]string{"jornada": "backoffice"}},
		{Name: "aks-sem-tag", ResourceGroup: "rg-x", Subscription: "PRD"},
	}
	data, err := json.Marshal(fixture)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "clusters-config.json"), data, 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	km := &KubeConfigManager{}
	configs := km.GetAllClusterConfigs()
	if len(configs) != 2 {
		t.Fatalf("len(configs) = %d, want 2", len(configs))
	}

	byName := make(map[string]ClusterConfig)
	for _, c := range configs {
		byName[c.Name] = c
	}

	if got := byName["aks-backoffice"].Journey(); got != "backoffice" {
		t.Errorf("aks-backoffice.Journey() = %q, want %q", got, "backoffice")
	}
	if got := byName["aks-sem-tag"].Journey(); got != "" {
		t.Errorf("aks-sem-tag.Journey() = %q, want empty", got)
	}
}
