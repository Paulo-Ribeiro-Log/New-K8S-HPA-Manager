package finops

import "testing"

// TestDeriveDataResourceGroup cobre a convenção real confirmada com o usuário: app
// "rg-abastecimento-app-hlg" → data "rg-abastecimento-data-hlg".
func TestDeriveDataResourceGroup(t *testing.T) {
	cases := []struct {
		in     string
		wantRG string
		wantOK bool
	}{
		{"rg-abastecimento-app-hlg", "rg-abastecimento-data-hlg", true},
		{"rg-oferta-app-prd", "rg-oferta-data-prd", true},
		{"rg-multi-word-name-app-hlg", "rg-multi-word-name-data-hlg", true},
		{"rg-sem-convencao", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		gotRG, gotOK := DeriveDataResourceGroup(c.in)
		if gotOK != c.wantOK || gotRG != c.wantRG {
			t.Errorf("DeriveDataResourceGroup(%q) = (%q, %v), want (%q, %v)", c.in, gotRG, gotOK, c.wantRG, c.wantOK)
		}
	}
}

func TestRedisSizeLabel(t *testing.T) {
	cases := []struct {
		tier, sku, want string
	}{
		{"Standard", "C1", "C1"},
		{"Basic", "C0", "C0"},
		{"Premium", "P1", "P1"},
		{"Premium", "P4", "P4"},
		{"Standard", "", ""},
	}
	for _, c := range cases {
		got := redisSizeLabel(c.tier, c.sku)
		if got != c.want {
			t.Errorf("redisSizeLabel(%q, %q) = %q, want %q", c.tier, c.sku, got, c.want)
		}
	}
}

func TestMeterMatchesRedisSize(t *testing.T) {
	if !meterMatchesRedisSize("C1 Cache Instance", "C1") {
		t.Error("esperava match de 'C1 Cache Instance' com 'C1'")
	}
	if meterMatchesRedisSize("C10 Cache Instance", "C1") {
		t.Error("'C10' não deveria casar com 'C1' — bug real evitado: contains() sozinho casaria isso")
	}
}

func TestExtractTrailingDigits(t *testing.T) {
	cases := []struct {
		in     string
		wantN  int
		wantOK bool
	}{
		{"P1", 1, true},
		{"C10", 10, true},
		{"Standard", 0, false},
		{"", 0, false},
	}
	for _, c := range cases {
		n, ok := extractTrailingDigits(c.in)
		if ok != c.wantOK || (ok && n != c.wantN) {
			t.Errorf("extractTrailingDigits(%q) = (%d, %v), want (%d, %v)", c.in, n, ok, c.wantN, c.wantOK)
		}
	}
}

// TestListDataResourceGroup_FiltersToAllowlist confirma que tipos de recurso fora da allowlist
// (ex: private endpoints, key vaults — ruído comum em RGs reais) nunca entram na listagem, mesmo
// que o `az resource list` os devolva — sem precisar de uma chamada `az` real pra testar isso,
// já que a filtragem acontece depois do parse, sobre dados sintéticos.
func TestListDataResourceGroup_FiltersToAllowlist(t *testing.T) {
	raw := []azCLIResource{
		{Name: "sqldb1", Type: "Microsoft.Sql/servers/databases", Location: "brazilsouth"},
		{Name: "pe1", Type: "Microsoft.Network/privateEndpoints", Location: "brazilsouth"},
		{Name: "kv1", Type: "Microsoft.KeyVault/vaults", Location: "brazilsouth"},
		{Name: "redis1", Type: "Microsoft.Cache/Redis", Location: "brazilsouth"},
	}
	got := filterToAllowlist(raw)
	if len(got) != 2 {
		t.Fatalf("esperava 2 recursos relevantes (sqldb1, redis1), veio %d: %+v", len(got), got)
	}
	names := map[string]bool{got[0].Name: true, got[1].Name: true}
	if !names["sqldb1"] || !names["redis1"] {
		t.Errorf("esperava sqldb1 e redis1, veio %+v", got)
	}
}
