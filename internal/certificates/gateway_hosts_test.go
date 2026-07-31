package certificates

import "testing"

func TestHostnameMatchesListener(t *testing.T) {
	tests := []struct {
		name         string
		listenerHost string
		routeHost    string
		want         bool
	}{
		{"listener vazio casa qualquer hostname", "", "api.example.com", true},
		{"match exato", "api.example.com", "api.example.com", true},
		{"match exato diferente falha", "api.example.com", "other.example.com", false},
		{"wildcard casa subdominio", "*.example.com", "api.example.com", true},
		{"wildcard nao casa dominio nu", "*.example.com", "example.com", false},
		{"wildcard nao casa dominio parecido sem ponto", "*.example.com", "evilexample.com", false},
		{"wildcard casa outro subdominio", "*.example.com", "admin.example.com", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hostnameMatchesListener(tt.listenerHost, tt.routeHost)
			if got != tt.want {
				t.Errorf("hostnameMatchesListener(%q, %q) = %v, esperado %v", tt.listenerHost, tt.routeHost, got, tt.want)
			}
		})
	}
}
