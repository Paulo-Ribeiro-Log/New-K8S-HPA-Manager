package finops

import (
	"testing"

	"k8s-hpa-manager/internal/models"
)

func TestEnvironmentOf(t *testing.T) {
	cases := map[string]string{
		"akspriv-entregas-prd-admin":                              "prd",
		"akspriv-entregas-hlg":                                    "hlg",
		"rg-entregas-data-hml":                                    "hlg",
		"MC_rg-entregas-app-hlg_akspriv-entregas-hlg_brazilsouth": "hlg",
		"asaplog-production":                                      "prd",
		"rg-logistica-shared":                                     "",
		"Produção":                                                "",
	}
	for in, want := range cases {
		if got := EnvironmentOf(in); got != want {
			t.Errorf("EnvironmentOf(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFilterScopeByEnvironment(t *testing.T) {
	rgs := []models.ScopedResourceGroup{
		{ResourceGroup: "rg-entregas-app-hlg", Source: "cluster"},
		{ResourceGroup: "rg-entregas-app-prd", Source: "tag"},                          // PRD da mesma jornada
		{ResourceGroup: "rg-logistica-shared", Source: "tag"},                          // sem ambiente
		{ResourceGroup: "rg-logistica-shared-2", Source: "tag", EnvTag: "Homologacao"}, // ambiente pela tag
		{ResourceGroup: "rg-sem-marcador", Source: "data"},                             // veio de cluster do ambiente
	}
	kept, excluded := FilterScopeByEnvironment(rgs, "hlg")
	names := func(l []models.ScopedResourceGroup) map[string]bool {
		m := map[string]bool{}
		for _, r := range l {
			m[r.ResourceGroup] = true
		}
		return m
	}
	k, e := names(kept), names(excluded)
	for _, rg := range []string{"rg-entregas-app-hlg", "rg-logistica-shared-2", "rg-sem-marcador"} {
		if !k[rg] {
			t.Errorf("%s deveria ficar no escopo", rg)
		}
	}
	for _, rg := range []string{"rg-entregas-app-prd", "rg-logistica-shared"} {
		if !e[rg] {
			t.Errorf("%s deveria ser excluído", rg)
		}
	}
	for _, r := range excluded {
		if r.ExcludedReason == "" {
			t.Errorf("%s excluído sem motivo", r.ResourceGroup)
		}
	}
	if all, none := FilterScopeByEnvironment(rgs, ""); len(all) != len(rgs) || len(none) != 0 {
		t.Error("ambiente desconhecido não deveria filtrar")
	}
}

func TestEnvironmentRegex(t *testing.T) {
	if got := EnvironmentRegex("hlg"); got != `(?i)(^|[-_./: ])(hlg|hml|homolog|homologacao)($|[-_./: ])` {
		t.Errorf("EnvironmentRegex(hlg) = %s", got)
	}
	if EnvironmentRegex("") != "" {
		t.Error("ambiente vazio deveria gerar regex vazio")
	}
}
