package handlers

import "testing"

func TestJourneyOf_PeloNome(t *testing.T) {
	h := &FinOpsHandler{}
	cases := map[string]string{
		"akspriv-oferta-prd-admin":          "oferta",
		"akspriv-mktplace-carrinho-hlg":     "mktplace-carrinho",
		"arn:aws:eks:us-east-1:1:cluster/x": "",
	}
	for in, want := range cases {
		if got := h.journeyOf(in); got != want {
			t.Errorf("journeyOf(%q) = %q, want %q", in, got, want)
		}
	}
}
