package gcp

import "testing"

func TestParseBQRows(t *testing.T) {
	sc, err := parseBQRows([]byte(`[{"service":"Compute Engine","cost":"812.4","currency":"BRL"},{"service":"Networking","cost":12.5},{"cost":"1"}]`))
	if err != nil {
		t.Fatal(err)
	}
	if sc.ByService["Compute Engine"] != 812.4 || sc.ByService["Networking"] != 12.5 || sc.ByService["Outros"] != 1 || sc.Currency != "BRL" {
		t.Errorf("custo por serviço: %+v", sc)
	}
	if !bqTableRe.MatchString("meu-proj.billing.gcp_billing_export_v1_0123") || bqTableRe.MatchString("x; DROP") {
		t.Error("validação do nome da tabela")
	}
}
