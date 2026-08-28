package handlers

import (
	"strings"
	"testing"
)

// TestParseFingerprintOutput_RealCapture usa a saída REAL capturada rodando
// netDiscoveryFingerprintScript ao vivo contra 1.1.1.1 (Cloudflare) dentro da imagem netshoot —
// não é uma saída inventada, garante que o parser bate com o formato de verdade.
func TestParseFingerprintOutput_RealCapture(t *testing.T) {
	output := `@@TTL 52
@@PORT 53 OPEN
@@PORT 443 OPEN
@@PORT 80 OPEN
@@PORT 8080 OPEN
@@PORT 8443 OPEN
@@PORT 3389 CLOSED
@@PORT 445 CLOSED
@@PORT 22 CLOSED
@@PORT 3306 CLOSED
@@PORT 5432 CLOSED
@@PORT 5986 CLOSED
@@PORT 25 CLOSED
@@PORT 5985 CLOSED
@@PORT 27017 CLOSED
@@PORT 21 CLOSED
@@PORT 6379 CLOSED
@@PORT 587 CLOSED
@@PORT 9200 CLOSED
@@HTTP Server: cloudflare
@@HTTPS server: cloudflare
@@TLS subject=C = US, ST = California, L = San Francisco, O = "Cloudflare, Inc.", CN = cloudflare-dns.com|issuer=C = US, ST = Texas, L = Houston, O = SSL Corp, CN = SSL.com SSL Intermediate CA ECC R2|`

	fp := parseFingerprintOutput(output)

	if fp.TTL != 52 {
		t.Errorf("TTL = %d, esperava 52", fp.TTL)
	}
	if len(fp.OpenPorts) != 5 {
		t.Fatalf("OpenPorts = %v, esperava 5 portas abertas", fp.OpenPorts)
	}
	wantPorts := []int{53, 80, 443, 8080, 8443}
	for i, p := range wantPorts {
		if fp.OpenPorts[i] != p {
			t.Errorf("OpenPorts[%d] = %d, esperava %d (esperava ordenado)", i, fp.OpenPorts[i], p)
		}
	}
	if !fp.IsWebServer {
		t.Error("IsWebServer deveria ser true (porta 80/443 aberta + TLS + header Server)")
	}
	if fp.HTTPServer != "cloudflare" {
		t.Errorf("HTTPServer = %q, esperava %q", fp.HTTPServer, "cloudflare")
	}
	if fp.TLSSubject != `C = US, ST = California, L = San Francisco, O = "Cloudflare, Inc.", CN = cloudflare-dns.com` {
		t.Errorf("TLSSubject = %q, inesperado", fp.TLSSubject)
	}
	if fp.TLSIssuer != "C = US, ST = Texas, L = Houston, O = SSL Corp, CN = SSL.com SSL Intermediate CA ECC R2" {
		t.Errorf("TLSIssuer = %q, inesperado", fp.TLSIssuer)
	}
	// TTL=52 → heurística de TTL (nenhuma porta característica de SO aberta) → linux provável
	if fp.OSGuess != "linux" {
		t.Errorf("OSGuess = %q, esperava linux (TTL 52 <= 64)", fp.OSGuess)
	}
}

// TestFormatExtraPortsArg — Fase D do roadmap de maturidade profissional (portas de fingerprint
// customizáveis). String espaço-separada, consumida pelo `for p in ... $EXTRAPORTS` do script via
// word-split natural do shell — vazio quando não há portas extras (comportamento idêntico a antes
// desta fase).
func TestFormatExtraPortsArg(t *testing.T) {
	if got := formatExtraPortsArg(nil); got != "" {
		t.Errorf("formatExtraPortsArg(nil) = %q, want \"\"", got)
	}
	if got := formatExtraPortsArg([]int{}); got != "" {
		t.Errorf("formatExtraPortsArg([]int{}) = %q, want \"\"", got)
	}
	if got := formatExtraPortsArg([]int{8081}); got != "8081" {
		t.Errorf("formatExtraPortsArg([8081]) = %q, want \"8081\"", got)
	}
	if got := formatExtraPortsArg([]int{8081, 9000}); got != "8081 9000" {
		t.Errorf("formatExtraPortsArg([8081, 9000]) = %q, want \"8081 9000\"", got)
	}
}

// TestParseFingerprintOutput_ExtraPortRecognizedGenerically confirma que uma porta EXTRA (fora da
// lista curada de 18) aparece em OpenPorts normalmente — parseFingerprintOutput já processa @@PORT
// genericamente, sem precisar saber quais portas eram "extras" (achado documentado: portas extras
// não influenciam inferOSGuess, só ExtraPorts em si é novo, o parser continua igual).
func TestParseFingerprintOutput_ExtraPortRecognizedGenerically(t *testing.T) {
	fp := parseFingerprintOutput("@@TTL 60\n@@PORT 9000 OPEN\n")
	if len(fp.OpenPorts) != 1 || fp.OpenPorts[0] != 9000 {
		t.Errorf("OpenPorts = %v, esperava [9000] (porta extra fora da lista curada)", fp.OpenPorts)
	}
}

func TestParseFingerprintOutput_NoSignalAtAll(t *testing.T) {
	// Nenhuma linha reconhecida (script falhou/timeout total) — não deveria panicar, só devolver
	// um fingerprint vazio/neutro.
	fp := parseFingerprintOutput("")
	if fp.TTL != 0 || len(fp.OpenPorts) != 0 || fp.IsWebServer {
		t.Errorf("fp = %+v, esperava tudo zerado", fp)
	}
	if fp.OSGuess != "" {
		t.Errorf("OSGuess = %q, esperava vazio sem nenhum sinal", fp.OSGuess)
	}
}

func TestInferOSGuess_WindowsPortWinsOverTTL(t *testing.T) {
	// Mesmo com TTL sugerindo Linux (<=64), porta característica de Windows deve prevalecer —
	// pedido explícito do plano: "porta é sinal mais confiável que TTL sozinho".
	guess, confidence := inferOSGuess(60, []int{3389}, nil)
	if guess != "windows" {
		t.Errorf("guess = %q, esperava windows (porta 3389 deveria prevalecer sobre TTL)", guess)
	}
	if confidence == "" {
		t.Error("confidence nunca deveria vir vazia quando há um guess")
	}
}

func TestInferOSGuess_SSHPortImpliesLinux(t *testing.T) {
	guess, _ := inferOSGuess(0, []int{22}, nil)
	if guess != "linux" {
		t.Errorf("guess = %q, esperava linux (porta 22 aberta)", guess)
	}
}

func TestInferOSGuess_TTLFallback(t *testing.T) {
	cases := []struct {
		ttl  int
		want string
	}{
		{50, "linux"},
		{64, "linux"},
		{116, "windows"},
		{128, "windows"},
		{200, ""}, // fora dos dois padrões conhecidos — não deveria inventar um palpite
	}
	for _, c := range cases {
		guess, confidence := inferOSGuess(c.ttl, nil, nil)
		if guess != c.want {
			t.Errorf("inferOSGuess(%d, nil, nil) = %q, esperava %q", c.ttl, guess, c.want)
		}
		if confidence == "" {
			t.Errorf("confidence vazia pra TTL=%d — sempre deveria explicar o motivo (mesmo motivo de ausência)", c.ttl)
		}
	}
}

func TestInferOSGuess_NoTTLNoPorts(t *testing.T) {
	guess, confidence := inferOSGuess(0, nil, nil)
	if guess != "" {
		t.Errorf("guess = %q, esperava vazio sem nenhum sinal", guess)
	}
	if confidence == "" {
		t.Error("confidence deveria explicar a ausência de sinal, não vir vazia")
	}
}

// TestInferOSGuess_K8sMatchOnlyUsedAsLastResort — achado de code review: o sinal de cross-reference
// K8s foi movido pra DENTRO de inferOSGuess (era um fallback hardcoded em runDiscovery, fora do
// único mecanismo documentado). Estes testes cobrem o contrato completo do parâmetro k8sMatch.
func TestInferOSGuess_K8sMatchOnlyUsedAsLastResort(t *testing.T) {
	liveMatch := &NetDiscoveryInternalRef{Kind: "node", Name: "aks-nodepool1-vmss0", FromCache: false}

	t.Run("usado quando não há sinal de TTL/porta", func(t *testing.T) {
		guess, confidence := inferOSGuess(0, nil, liveMatch)
		if guess != "linux" {
			t.Errorf("guess = %q, esperava linux via match K8s", guess)
		}
		if confidence == "" || !strings.Contains(confidence, "aks-nodepool1-vmss0") {
			t.Errorf("confidence deveria citar o recurso K8s por extenso, got %q", confidence)
		}
	})

	t.Run("nunca sobrescreve um guess já derivado de porta", func(t *testing.T) {
		guess, confidence := inferOSGuess(0, []int{3389}, liveMatch)
		if guess != "windows" {
			t.Errorf("guess = %q, porta 3389 deveria prevalecer sobre o match K8s", guess)
		}
		if strings.Contains(confidence, "aks-nodepool1-vmss0") {
			t.Error("confidence não deveria mencionar o match K8s quando a porta já decidiu")
		}
	})

	t.Run("nunca sobrescreve um guess já derivado de TTL", func(t *testing.T) {
		guess, confidence := inferOSGuess(50, nil, liveMatch)
		if guess != "linux" {
			t.Errorf("guess = %q, esperava linux via TTL", guess)
		}
		if strings.Contains(confidence, "aks-nodepool1-vmss0") {
			t.Error("confidence não deveria mencionar o match K8s quando o TTL já decidiu")
		}
	})

	t.Run("match de cache nunca conta como sinal — achado real: pode estar desatualizado", func(t *testing.T) {
		staleMatch := &NetDiscoveryInternalRef{Kind: "node", Name: "aks-nodepool1-vmss0", FromCache: true}
		guess, confidence := inferOSGuess(0, nil, staleMatch)
		if guess != "" {
			t.Errorf("guess = %q, match de CACHE nunca deveria virar sinal de SO (pode estar desatualizado)", guess)
		}
		if strings.Contains(confidence, "aks-nodepool1-vmss0") {
			t.Error("confidence não deveria mencionar um match de cache")
		}
	})
}

// TestParseFingerprintOutput_AdditionalHostsRealCapture usa a saída REAL capturada rodando o
// script estendido (com VHOSTS) ao vivo contra 1.1.1.1 dentro da imagem netshoot (as duas primeiras
// entradas "@@VHOST" — o Cloudflare respondeu com o mesmo certificado default independente do SNI
// pedido, o que é esperado desse endpoint específico, não uma falha do mecanismo) — confirma que
// múltiplos hostnames adicionais (achado real: um IP de Load Balancer/Ingress compartilhado pode
// ter dezenas de PTRs diferentes, um por app) são parseados corretamente, um por linha "@@VHOST",
// mesmo rodando em paralelo (cada linha já sai atômica, com os 3 campos combinados). A 3ª entrada
// (hostname sem NENHUMA resposta) é sintética — testa o caso de um vhost que não responde (curl/
// openssl retornam vazio), garantindo que ainda assim entra na lista sem quebrar o parse.
func TestParseFingerprintOutput_AdditionalHostsRealCapture(t *testing.T) {
	output := `@@TTL 52
@@PORT 443 OPEN
@@PORT 80 OPEN
@@HTTP Server: cloudflare
@@HTTPS server: cloudflare
@@TLS subject=CN = cloudflare-dns.com|issuer=CN = SSL.com SSL Intermediate CA ECC R2|
@@MTLS_USED 0
@@VHOST cloudflare-dns.com@@FS@@Server: cloudflare@@FS@@server: cloudflare@@FS@@subject=C = US, ST = California, L = San Francisco, O = "Cloudflare, Inc.", CN = cloudflare-dns.com|issuer=C = US, ST = Texas, L = Houston, O = SSL Corp, CN = SSL.com SSL Intermediate CA ECC R2|
@@VHOST 1dot1dot1dot1.cloudflare-dns.com@@FS@@Server: cloudflare@@FS@@server: cloudflare@@FS@@subject=C = US, ST = California, L = San Francisco, O = "Cloudflare, Inc.", CN = cloudflare-dns.com|issuer=C = US, ST = Texas, L = Houston, O = SSL Corp, CN = SSL.com SSL Intermediate CA ECC R2|
@@VHOST nonexistent-host-xyz123.invalid@@FS@@@@FS@@@@FS@@`

	fp := parseFingerprintOutput(output)

	if len(fp.AdditionalHosts) != 3 {
		t.Fatalf("AdditionalHosts = %d entradas, esperava 3: %+v", len(fp.AdditionalHosts), fp.AdditionalHosts)
	}
	// Ordenado alfabeticamente por host — resultado determinístico independente da ordem de
	// chegada das linhas (os probes rodam em paralelo dentro do script).
	wantOrder := []string{"1dot1dot1dot1.cloudflare-dns.com", "cloudflare-dns.com", "nonexistent-host-xyz123.invalid"}
	for i, h := range wantOrder {
		if fp.AdditionalHosts[i].Host != h {
			t.Errorf("AdditionalHosts[%d].Host = %q, esperava %q", i, fp.AdditionalHosts[i].Host, h)
		}
	}
	found := fp.AdditionalHosts[1] // cloudflare-dns.com
	if found.HTTPServer != "cloudflare" {
		t.Errorf("HTTPServer = %q, esperava cloudflare", found.HTTPServer)
	}
	if found.TLSSubject == "" || found.TLSIssuer == "" {
		t.Errorf("TLSSubject/TLSIssuer vazios, esperava valores extraídos: %+v", found)
	}
	// hostname sem resposta nenhuma (todos os 3 campos vazios) — entra na lista mesmo assim
	// (o probe rodou, só não achou nada), mas sem nenhum campo populado.
	empty := fp.AdditionalHosts[2] // nonexistent-host-xyz123.invalid
	if empty.HTTPServer != "" || empty.TLSSubject != "" || empty.TLSIssuer != "" {
		t.Errorf("esperava todos os campos vazios pra hostname sem resposta, obteve %+v", empty)
	}
}

func TestParseVHostLine_MalformedIgnored(t *testing.T) {
	cases := []struct {
		name string
		line string
		ok   bool
	}{
		{"formato completo", "host@@FS@@http@@FS@@https@@FS@@tls", true},
		{"campos faltando", "host@@FS@@http", false},
		{"host vazio", "@@FS@@http@@FS@@https@@FS@@tls", false},
		{"sem separador nenhum", "hostsemcampos", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, ok := parseVHostLine(tc.line)
			if ok != tc.ok {
				t.Errorf("parseVHostLine(%q) ok=%v, esperava %v", tc.line, ok, tc.ok)
			}
		})
	}
}

func TestValidVHostName(t *testing.T) {
	valid := []string{
		"example.com",
		"api.internal.company.com",
		"1dot1dot1dot1.cloudflare-dns.com",
		"a.b.c",
		"xn--80ak6aa92e.com", // IDN punycode — labels ASCII válidos
	}
	for _, name := range valid {
		if !validVHostName(name) {
			t.Errorf("validVHostName(%q) = false, esperava true", name)
		}
	}

	invalid := []string{
		"",
		"host;rm -rf /",
		"host with space",
		"$(whoami)",
		"host`id`",
		"host|cat",
		strings.Repeat("a", 254), // acima do limite de 253 chars
		"-startswithhyphen.com",
	}
	for _, name := range invalid {
		if validVHostName(name) {
			t.Errorf("validVHostName(%q) = true, esperava false (não é um nome DNS seguro)", name)
		}
	}
}
