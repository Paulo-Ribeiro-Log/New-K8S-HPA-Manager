package dynatrace

import (
	"reflect"
	"testing"
)

// TestExtractClusterDistinctiveTokens_IgnoresEnvAndGenericTokens cobre o caso real que motivou o
// fallback fuzzy: "asaplog-production" (nome do cluster no app) não bate com a entidade Dynatrace
// "eks-asaplog-prd" via nome exato — só "asaplog" sobra como token distintivo depois de remover o
// marcador de ambiente ("production") e prefixos genéricos ("eks").
func TestExtractClusterDistinctiveTokens_IgnoresEnvAndGenericTokens(t *testing.T) {
	got := extractClusterDistinctiveTokens("asaplog-production")
	want := []string{"asaplog"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestExtractClusterDistinctiveTokens_SortsLongestFirst(t *testing.T) {
	got := extractClusterDistinctiveTokens("categorizador-prdt-prd")
	if len(got) == 0 || got[0] != "categorizador" {
		t.Errorf("esperava token mais longo primeiro, got %v", got)
	}
}

func TestExtractClusterEnvToken(t *testing.T) {
	cases := map[string]string{
		"asaplog-production": "prd",
		"eks-asaplog-prd":    "prd",
		"asaplog-preprod":    "preprod",
		"akspriv-oferta-hlg": "hlg",
		"algumcluster":       "",
	}
	for name, want := range cases {
		if got := extractClusterEnvToken(name); got != want {
			t.Errorf("extractClusterEnvToken(%q) = %q, want %q", name, got, want)
		}
	}
}
