package handlers

import "testing"

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
	guess, confidence := inferOSGuess(60, []int{3389})
	if guess != "windows" {
		t.Errorf("guess = %q, esperava windows (porta 3389 deveria prevalecer sobre TTL)", guess)
	}
	if confidence == "" {
		t.Error("confidence nunca deveria vir vazia quando há um guess")
	}
}

func TestInferOSGuess_SSHPortImpliesLinux(t *testing.T) {
	guess, _ := inferOSGuess(0, []int{22})
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
		guess, confidence := inferOSGuess(c.ttl, nil)
		if guess != c.want {
			t.Errorf("inferOSGuess(%d, nil) = %q, esperava %q", c.ttl, guess, c.want)
		}
		if confidence == "" {
			t.Errorf("confidence vazia pra TTL=%d — sempre deveria explicar o motivo (mesmo motivo de ausência)", c.ttl)
		}
	}
}

func TestInferOSGuess_NoTTLNoPorts(t *testing.T) {
	guess, confidence := inferOSGuess(0, nil)
	if guess != "" {
		t.Errorf("guess = %q, esperava vazio sem nenhum sinal", guess)
	}
	if confidence == "" {
		t.Error("confidence deveria explicar a ausência de sinal, não vir vazia")
	}
}
