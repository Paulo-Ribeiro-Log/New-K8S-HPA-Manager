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

// TestNormalizeClusterName_EKSArn cobre o bug real que fazia a correlação Dynatrace nunca
// funcionar pra clusters EKS sem alias amigável configurado: `selectedCluster` no frontend vem de
// `cluster.context`, que pra esses casos é o ARN completo, não o nome curto usado em qualquer
// tentativa de correlação (exata ou fuzzy).
func TestNormalizeClusterName_EKSArn(t *testing.T) {
	got := NormalizeClusterName("arn:aws:eks:us-east-1:400894646268:cluster/asaplog-production")
	want := "asaplog-production"
	if got != want {
		t.Errorf("NormalizeClusterName(ARN) = %q, want %q", got, want)
	}
}

func TestNormalizeClusterName_AKSAdminSuffix(t *testing.T) {
	got := NormalizeClusterName("akspriv-logreversa-prd-admin")
	want := "akspriv-logreversa-prd"
	if got != want {
		t.Errorf("NormalizeClusterName(-admin) = %q, want %q", got, want)
	}
}

func TestNormalizeClusterName_PlainNameUnchanged(t *testing.T) {
	got := NormalizeClusterName("asaplog-production")
	want := "asaplog-production"
	if got != want {
		t.Errorf("NormalizeClusterName(nome simples) = %q, want %q — não deveria alterar nomes que já não precisam de normalização", got, want)
	}
}

// TestExtractClusterDistinctiveTokens_HandlesARN cobre a extração fuzzy quando, por algum motivo,
// um ARN chega direto (defesa em profundidade — NormalizeClusterName já deveria ter tirado o ARN
// antes, mas o tokenizador não deve produzir tokens inúteis como "1:400894646268:cluster" se isso
// falhar por qualquer razão).
func TestExtractClusterDistinctiveTokens_HandlesARN(t *testing.T) {
	got := extractClusterDistinctiveTokens("arn:aws:eks:us-east-1:400894646268:cluster/asaplog-production")
	found := false
	for _, tok := range got {
		if tok == "asaplog" {
			found = true
		}
		if isNumericToken(tok) {
			t.Errorf("token puramente numérico não deveria sobreviver ao filtro: %q", tok)
		}
	}
	if !found {
		t.Errorf("esperava \"asaplog\" entre os tokens distintivos, got %v", got)
	}
}
