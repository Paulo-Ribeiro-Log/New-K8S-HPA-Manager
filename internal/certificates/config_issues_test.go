package certificates

import "testing"

func TestCertSANCoversHost(t *testing.T) {
	tests := []struct {
		name string
		sans []string
		host string
		want bool
	}{
		{"match exato", []string{"api.example.com"}, "api.example.com", true},
		{"match wildcard", []string{"*.example.com"}, "api.example.com", true},
		{"wildcard nao cobre dominio nu", []string{"*.example.com"}, "example.com", false},
		{"sem cobertura", []string{"other.example.com"}, "api.example.com", false},
		{"case insensitive", []string{"API.EXAMPLE.COM"}, "api.example.com", true},
		{"multiplos sans, segundo cobre", []string{"other.example.com", "*.example.com"}, "api.example.com", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := certSANCoversHost(tt.sans, tt.host)
			if got != tt.want {
				t.Errorf("certSANCoversHost(%v, %q) = %v, esperado %v", tt.sans, tt.host, got, tt.want)
			}
		})
	}
}

func TestDetectHostConflicts_MesmaClasseConflita(t *testing.T) {
	hostOwners := map[string][]hostOwner{
		"api.example.com": {
			{Kind: "ingress", Namespace: "ns1", Name: "ing1", SecretName: "secret1", IngressClass: "nginx"},
			{Kind: "ingress", Namespace: "ns2", Name: "ing2", SecretName: "secret2", IngressClass: "nginx"},
		},
	}

	result := detectHostConflicts(hostOwners)

	if len(result["ns1/secret1"]) != 1 {
		t.Fatalf("esperava 1 warning para ns1/secret1, obteve %v", result["ns1/secret1"])
	}
	if len(result["ns2/secret2"]) != 1 {
		t.Fatalf("esperava 1 warning para ns2/secret2, obteve %v", result["ns2/secret2"])
	}
}

func TestDetectHostConflicts_ClassesDiferentesNaoConflita(t *testing.T) {
	hostOwners := map[string][]hostOwner{
		"api.example.com": {
			{Kind: "ingress", Namespace: "ns1", Name: "ing1", SecretName: "secret1", IngressClass: "nginx-interno"},
			{Kind: "ingress", Namespace: "ns2", Name: "ing2", SecretName: "secret2", IngressClass: "nginx-externo"},
		},
	}

	result := detectHostConflicts(hostOwners)

	if len(result) != 0 {
		t.Errorf("esperava nenhum conflito entre classes diferentes, obteve %v", result)
	}
}

func TestDetectHostConflicts_MesmoSecretNaoConflita(t *testing.T) {
	hostOwners := map[string][]hostOwner{
		"api.example.com": {
			{Kind: "ingress", Namespace: "ns1", Name: "ing1", SecretName: "secret1", IngressClass: "nginx"},
			{Kind: "ingress", Namespace: "ns1", Name: "ing2", SecretName: "secret1", IngressClass: "nginx"},
		},
	}

	result := detectHostConflicts(hostOwners)

	if len(result) != 0 {
		t.Errorf("esperava nenhum conflito quando os dois Ingresses usam o mesmo secret, obteve %v", result)
	}
}

func TestDetectHostConflicts_GatewayIgnoraClasse(t *testing.T) {
	// Ingress e Gateway com "classes" diferentes (Gateway não tem IngressClass, fica vazio) —
	// mesmo assim deve conflitar, pois é o cenário de migração incompleta que vale flagar.
	hostOwners := map[string][]hostOwner{
		"api.example.com": {
			{Kind: "ingress", Namespace: "ns1", Name: "ing1", SecretName: "secret1", IngressClass: "nginx"},
			{Kind: "gateway", Namespace: "ns2", Name: "gw1", SecretName: "secret2", IngressClass: ""},
		},
	}

	result := detectHostConflicts(hostOwners)

	if len(result["ns1/secret1"]) != 1 {
		t.Fatalf("esperava 1 warning para ns1/secret1 (Ingress vs Gateway), obteve %v", result["ns1/secret1"])
	}
	if len(result["ns2/secret2"]) != 1 {
		t.Fatalf("esperava 1 warning para ns2/secret2 (Ingress vs Gateway), obteve %v", result["ns2/secret2"])
	}
}

func TestDetectHostConflicts_DoisGatewaysDiferentesSecretsConflita(t *testing.T) {
	hostOwners := map[string][]hostOwner{
		"api.example.com": {
			{Kind: "gateway", Namespace: "ns1", Name: "gw1", SecretName: "secret1"},
			{Kind: "gateway", Namespace: "ns2", Name: "gw2", SecretName: "secret2"},
		},
	}

	result := detectHostConflicts(hostOwners)

	if len(result["ns1/secret1"]) != 1 || len(result["ns2/secret2"]) != 1 {
		t.Errorf("esperava conflito entre 2 Gateways com secrets diferentes, obteve %v", result)
	}
}
