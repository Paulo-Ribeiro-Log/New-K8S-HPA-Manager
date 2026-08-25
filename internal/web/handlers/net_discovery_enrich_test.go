package handlers

import (
	"context"
	"encoding/json"
	"net"
	"testing"
)

// TestReverseIPv4Octets_MatchesLiveVerification — o achado real desta rodada: testar a
// convenção de octetos invertidos da Team Cymru só com um IP palíndromo (8.8.8.8) não provaria
// nada sobre a ordem certa. Este teste fixa o resultado confirmado ao vivo contra um IP
// assimétrico real (142.251.32.14, Google) — se essa função algum dia regredir pra ordem
// direta, o teste pega antes de virar um bug silencioso de novo (ASN sempre errado).
func TestReverseIPv4Octets_MatchesLiveVerification(t *testing.T) {
	ip := net.ParseIP("142.251.32.14").To4()
	got := reverseIPv4Octets(ip)
	want := "14.32.251.142"
	if got != want {
		t.Errorf("reverseIPv4Octets(142.251.32.14) = %q, want %q (verificado ao vivo contra a Team Cymru)", got, want)
	}
}

func TestMatchCloudRange_MatchesAndMisses(t *testing.T) {
	_, awsNet, _ := net.ParseCIDR("15.190.244.0/22")
	_, gcpNet, _ := net.ParseCIDR("34.1.208.0/20")
	ranges := []netDiscoveryCloudEntry{
		{Net: awsNet, Provider: "aws", Region: "ap-east-2"},
		{Net: gcpNet, Provider: "gcp", Region: "africa-south1"},
	}

	provider, region := matchCloudRange(net.ParseIP("15.190.245.10"), ranges)
	if provider != "aws" || region != "ap-east-2" {
		t.Errorf("provider=%q region=%q, esperava aws/ap-east-2", provider, region)
	}

	provider, region = matchCloudRange(net.ParseIP("34.1.208.5"), ranges)
	if provider != "gcp" || region != "africa-south1" {
		t.Errorf("provider=%q region=%q, esperava gcp/africa-south1", provider, region)
	}

	provider, _ = matchCloudRange(net.ParseIP("8.8.8.8"), ranges)
	if provider != "" {
		t.Errorf("provider = %q, esperava vazio (IP fora de qualquer faixa conhecida)", provider)
	}
}

// TestFetchAWSRanges_RealFieldNames / TestFetchGCPRanges_RealFieldNames — os nomes de campo JSON
// (`ip_prefix` vs. `ipv4Prefix`) são DIFERENTES entre os dois providers, achado real confirmado
// inspecionando os dois feeds ao vivo. Estes testes usam um payload sintético no formato real
// (não batem rede — não fazem HTTP), só garantem que o parser aceita o schema certo de cada um.
func TestParseAWSDoc_RealFieldNames(t *testing.T) {
	raw := `{"prefixes":[{"ip_prefix":"3.5.140.0/22","region":"ap-northeast-2","service":"AMAZON"}]}`
	var doc awsIPRangesDoc
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(doc.Prefixes) != 1 || doc.Prefixes[0].IPPrefix != "3.5.140.0/22" || doc.Prefixes[0].Region != "ap-northeast-2" {
		t.Errorf("doc = %+v, inesperado", doc)
	}
}

func TestParseGCPDoc_RealFieldNames(t *testing.T) {
	raw := `{"prefixes":[{"ipv4Prefix":"34.1.208.0/20","service":"Google Cloud","scope":"africa-south1"},{"ipv6Prefix":"2600:1900::/35","service":"Google Cloud","scope":"us-central1"}]}`
	var doc gcpIPRangesDoc
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(doc.Prefixes) != 2 {
		t.Fatalf("esperava 2 prefixos (1 ipv4 + 1 ipv6), veio %d", len(doc.Prefixes))
	}
	if doc.Prefixes[0].IPv4Prefix != "34.1.208.0/20" || doc.Prefixes[0].Scope != "africa-south1" {
		t.Errorf("prefixo ipv4 = %+v, inesperado", doc.Prefixes[0])
	}
	if doc.Prefixes[1].IPv4Prefix != "" {
		t.Error("entrada só-IPv6 não deveria preencher IPv4Prefix")
	}
}

func TestEnrichHops_SkipsTimedOutAndEmptyIP(t *testing.T) {
	hops := []NetDiscoveryHop{
		{Index: 1, IP: "", TimedOut: true},
		{Index: 2, IP: "", TimedOut: false}, // defensivo: sem IP mesmo sem timed_out explícito
	}
	// Não deve tentar nenhuma consulta de rede pra esses saltos — se tentasse, o teste ainda
	// passaria (best-effort), mas o objetivo aqui é confirmar que os campos continuam vazios
	// (nada foi escrito) sem precisar mockar rede.
	enrichHops(context.Background(), hops, "")
	for _, h := range hops {
		if h.ReverseDNS != "" || h.ASN != "" || h.CloudMatch != "" {
			t.Errorf("hop %+v não deveria ter sido enriquecido (timed_out ou sem IP)", h)
		}
	}
}

// TestEnrichHops_TargetHopGetsOriginalHostnameOverride cobre o bug real corrigido, relatado ao
// vivo pelo usuário contra um host atrás de um bastion/cofre Delinea: "não retornou o hostname
// correto". Quando a busca começou por hostname (originalHostname não-vazio), o salto ALVO
// (IsTarget=true) deve exibir esse hostname original — que é a fonte de verdade mais confiável
// disponível (foi literalmente o que resolveu pro IP) — em vez de depender só do PTR reverso, que
// pode legitimamente resolver pro nome do bastion/proxy, não do serviço real por trás dele.
func TestEnrichHops_TargetHopGetsOriginalHostnameOverride(t *testing.T) {
	hops := []NetDiscoveryHop{
		{Index: 1, IP: "127.0.0.1", IsTarget: false}, // não-alvo: PTR normal, sem override
		{Index: 2, IP: "127.0.0.1", IsTarget: true},  // alvo: hostname original deve prevalecer
	}
	enrichHops(context.Background(), hops, "meuservico.interno.exemplo.com")

	if hops[1].ReverseDNS != "meuservico.interno.exemplo.com" {
		t.Errorf("hop alvo: ReverseDNS = %q, want o hostname original (override)", hops[1].ReverseDNS)
	}
	// Hop não-alvo não deve ser afetado pelo override — só o PTR real dele (ou vazio, já que
	// 127.0.0.1 tipicamente não tem PTR configurado, mas o importante é NÃO ser o hostname
	// original, que só se aplica ao alvo).
	if hops[0].ReverseDNS == "meuservico.interno.exemplo.com" {
		t.Errorf("hop NÃO-alvo não deveria receber o override do hostname original")
	}
}
