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
