package discovery

import (
	"testing"
)

func TestParseClusterName(t *testing.T) {
	tests := []struct {
		cluster  string
		wantName string
		wantEnv  string
	}{
		{
			cluster:  "akspriv-faturamento-hlg-admin",
			wantName: "akspriv-faturamento",
			wantEnv:  "hlg",
		},
		{
			cluster:  "akspriv-checkout-prod-admin",
			wantName: "akspriv-checkout",
			wantEnv:  "prod",
		},
		{
			cluster:  "akspriv-pagamento-dev-admin",
			wantName: "akspriv-pagamento",
			wantEnv:  "dev",
		},
		{
			cluster:  "akspriv-log-reversa-prd-admin",
			wantName: "akspriv-log-reversa",
			wantEnv:  "prd",
		},
	}

	for _, tt := range tests {
		t.Run(tt.cluster, func(t *testing.T) {
			gotName, gotEnv := parseClusterName(tt.cluster)
			if gotName != tt.wantName {
				t.Errorf("parseClusterName() gotName = %v, want %v", gotName, tt.wantName)
			}
			if gotEnv != tt.wantEnv {
				t.Errorf("parseClusterName() gotEnv = %v, want %v", gotEnv, tt.wantEnv)
			}
		})
	}
}

func TestBuildPrometheusURL(t *testing.T) {
	tests := []struct {
		name string
		env  string
		want string
	}{
		{
			name: "akspriv-faturamento",
			env:  "hlg",
			want: "https://prometheus-akspriv-faturamento-hlg.viavarejo.com.br/",
		},
		{
			name: "akspriv-checkout",
			env:  "prod",
			want: "https://prometheus-akspriv-checkout-prod.viavarejo.com.br/",
		},
		{
			name: "akspriv-log-reversa",
			env:  "prd",
			want: "https://prometheus-akspriv-log-reversa-prd.viavarejo.com.br/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name+"-"+tt.env, func(t *testing.T) {
			got := buildPrometheusURL(tt.name, tt.env)
			if got != tt.want {
				t.Errorf("buildPrometheusURL() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetPrometheusURL(t *testing.T) {
	tests := []struct {
		cluster string
		want    string
	}{
		{
			cluster: "akspriv-faturamento-hlg-admin",
			want:    "https://prometheus-akspriv-faturamento-hlg.viavarejo.com.br/",
		},
		{
			cluster: "akspriv-checkout-prod-admin",
			want:    "https://prometheus-akspriv-checkout-prod.viavarejo.com.br/",
		},
		{
			cluster: "akspriv-logreversa-prd-admin",
			want:    "https://prometheus-akspriv-logreversa-prd.viavarejo.com.br/",
		},
		{
			cluster: "akspriv-faturamento-prd-admin",
			want:    "https://prometheus-akspriv-faturamento-prd.viavarejo.com.br/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.cluster, func(t *testing.T) {
			got := GetPrometheusURL(tt.cluster)
			t.Logf("Cluster: %s → URL: %s", tt.cluster, got)
			if got != tt.want {
				t.Errorf("GetPrometheusURL() = %v, want %v", got, tt.want)
			}
		})
	}
}

// Teste de integração - requer endpoint real acessível
func TestDiscoverEndpoint_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	// Teste com cluster real
	cluster := "akspriv-logreversa-prd-admin"

	endpoint, err := DiscoverEndpoint(cluster)

	if err != nil {
		t.Logf("Endpoint não disponível (esperado se não houver VPN): %v", err)
		return
	}

	if endpoint.Cluster != cluster {
		t.Errorf("Expected cluster %s, got %s", cluster, endpoint.Cluster)
	}

	if endpoint.Name != "akspriv-logreversa" {
		t.Errorf("Expected name akspriv-logreversa, got %s", endpoint.Name)
	}

	if endpoint.Environment != "prd" {
		t.Errorf("Expected environment prd, got %s", endpoint.Environment)
	}

	if !endpoint.Available {
		t.Error("Expected endpoint to be available")
	}

	expectedURL := "https://prometheus-akspriv-logreversa-prd.viavarejo.com.br/"
	if endpoint.URL != expectedURL {
		t.Errorf("Expected URL %s, got %s", expectedURL, endpoint.URL)
	}
}
