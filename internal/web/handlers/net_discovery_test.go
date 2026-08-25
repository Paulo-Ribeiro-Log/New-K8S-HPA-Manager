package handlers

import (
	"context"
	"io"
	"testing"
	"time"
)

func TestIsIPAddress(t *testing.T) {
	cases := map[string]bool{
		"8.8.8.8":              true,
		"192.168.1.1":          true,
		"2001:4860:4860::8888": true,
		"google.com":           false,
		"":                     false,
		"not-an-ip":            false,
		"999.999.999.999":      false,
	}
	for input, want := range cases {
		if got := isIPAddress(input); got != want {
			t.Errorf("isIPAddress(%q) = %v, want %v", input, got, want)
		}
	}
}

// TestTracerouteArgs_UsesGivenPort cobre o bug real corrigido (porta de sonda configurável,
// relatado ao vivo: "servidores windows dentro do delinea vault falha miseravelmente" — 443 fixo
// nunca respondia contra um alvo Windows atrás de PAM, só 3389/445/5985/5986). O último argumento
// posicional pro tcptraceroute deve ser a porta escolhida, nunca mais hardcoded.
func TestTracerouteArgs_UsesGivenPort(t *testing.T) {
	args := tracerouteArgs("10.0.0.5", 3389, netDiscoveryProbeTimeoutSec)
	if got := args[len(args)-1]; got != "3389" {
		t.Errorf("última posição dos args = %q, want \"3389\" (porta explícita)", got)
	}
	if got := args[len(args)-2]; got != "10.0.0.5" {
		t.Errorf("penúltima posição = %q, want o IP alvo", got)
	}

	// Default (443) continua funcionando idêntico a antes desta correção.
	argsDefault := tracerouteArgs("10.0.0.5", netDiscoveryTCPPort, netDiscoveryProbeTimeoutSec)
	if got := argsDefault[len(argsDefault)-1]; got != "443" {
		t.Errorf("porta default = %q, want \"443\"", got)
	}
}

// TestTracerouteArgs_UsesGivenProbeTimeout cobre o override de timeout por salto (pedido explícito
// do usuário: descartar rede lenta/alta latência antes de aceitar bloqueio de verdade atrás de um
// cofre PAM). O valor de "-w" deve refletir o timeout escolhido, não o default hardcoded.
func TestTracerouteArgs_UsesGivenProbeTimeout(t *testing.T) {
	args := tracerouteArgs("10.0.0.5", netDiscoveryTCPPort, 8)
	// "-w" é seguido do valor — acha o índice de "-w" e confere o próximo elemento.
	for i, a := range args {
		if a == "-w" {
			if args[i+1] != "8" {
				t.Errorf("-w = %q, want \"8\"", args[i+1])
			}
			return
		}
	}
	t.Fatal("flag -w não encontrada nos args")
}

// TestComputeOverallTimeout_DefaultUnaffected garante que o timeout de sonda default (2s) nunca
// muda o teto geral da descoberta — comportamento idêntico ao de antes desta correção.
func TestComputeOverallTimeout_DefaultUnaffected(t *testing.T) {
	got := computeOverallTimeout(netDiscoveryProbeTimeoutSec)
	if got != netDiscoveryOverallTimeout {
		t.Errorf("computeOverallTimeout(%d) = %v, want o default %v (sem mudança pro caso comum)",
			netDiscoveryProbeTimeoutSec, got, netDiscoveryOverallTimeout)
	}
}

// TestComputeOverallTimeout_ExtendsForLargerProbeTimeout cobre o bug que este mecanismo evita: sem
// estender o teto geral, um ProbeTimeoutSec maior faria o pior caso (todos os netDiscoveryMaxHops
// saltos sem resposta) facilmente estourar o teto FIXO antigo, abortando o traceroute no meio pelo
// contexto em vez de terminar normalmente com "não alcançado".
func TestComputeOverallTimeout_ExtendsForLargerProbeTimeout(t *testing.T) {
	got := computeOverallTimeout(netDiscoveryProbeTimeoutMaxSec) // 10s/salto, o máximo permitido
	worstCaseTraceroute := time.Duration(netDiscoveryProbeTimeoutMaxSec*netDiscoveryMaxHops) * time.Second
	if got <= worstCaseTraceroute {
		t.Errorf("computeOverallTimeout(%d) = %v, precisa ser MAIOR que o pior caso do traceroute (%v) — senão o contexto mata a descoberta no meio",
			netDiscoveryProbeTimeoutMaxSec, got, worstCaseTraceroute)
	}
}

// TestComputeOverallTimeout_NeverExceedsCap garante que o teto nunca ultrapassa
// netDiscoveryOverallTimeoutCap, mesmo no timeout de sonda máximo — fica sempre abaixo do
// ActiveDeadlineSeconds do pod de descoberta (modo pod), que mataria o pod de qualquer forma.
func TestComputeOverallTimeout_NeverExceedsCap(t *testing.T) {
	got := computeOverallTimeout(netDiscoveryProbeTimeoutMaxSec)
	if got > netDiscoveryOverallTimeoutCap {
		t.Errorf("computeOverallTimeout(%d) = %v, excede o teto %v", netDiscoveryProbeTimeoutMaxSec, got, netDiscoveryOverallTimeoutCap)
	}
}

// TestParseTracerouteLine_HeaderLineIgnored garante que a linha de cabeçalho do traceroute
// ("traceroute to X (X), 30 hops max...") nunca é confundida com uma linha de salto — não começa
// com dígitos, então tracerouteHopLineRegex não deveria casar.
func TestParseTracerouteLine_HeaderLineIgnored(t *testing.T) {
	_, ok := parseTracerouteLine("traceroute to 8.8.8.8 (8.8.8.8), 30 hops max, 60 byte packets", "8.8.8.8")
	if ok {
		t.Fatal("linha de cabeçalho não deveria ser reconhecida como salto")
	}
}

func TestParseTracerouteLine_SingleProbeSuccess(t *testing.T) {
	hop, ok := parseTracerouteLine(" 1  192.168.1.1  1.234 ms", "8.8.8.8")
	if !ok {
		t.Fatal("esperava reconhecer a linha como salto")
	}
	if hop.Index != 1 || hop.IP != "192.168.1.1" || hop.TimedOut {
		t.Errorf("hop = %+v, inesperado", hop)
	}
	if hop.RTTMs < 1.2 || hop.RTTMs > 1.3 {
		t.Errorf("RTTMs = %f, esperava ~1.234", hop.RTTMs)
	}
	if hop.IsTarget {
		t.Error("hop intermediário não deveria ser marcado como IsTarget")
	}
}

func TestParseTracerouteLine_MultiProbeAverages(t *testing.T) {
	hop, ok := parseTracerouteLine(" 2  10.0.0.1  5.000 ms  7.000 ms  6.000 ms", "8.8.8.8")
	if !ok {
		t.Fatal("esperava reconhecer a linha como salto")
	}
	if hop.RTTMs != 6.0 {
		t.Errorf("RTTMs = %f, esperava a média (6.0)", hop.RTTMs)
	}
}

func TestParseTracerouteLine_SingleAsteriskTimedOut(t *testing.T) {
	hop, ok := parseTracerouteLine(" 3  *", "8.8.8.8")
	if !ok {
		t.Fatal("esperava reconhecer a linha como salto")
	}
	if !hop.TimedOut || hop.IP != "" {
		t.Errorf("hop = %+v, esperava TimedOut=true e IP vazio", hop)
	}
}

func TestParseTracerouteLine_TripleAsteriskTimedOut(t *testing.T) {
	hop, ok := parseTracerouteLine(" 4  * * *", "8.8.8.8")
	if !ok {
		t.Fatal("esperava reconhecer a linha como salto")
	}
	if !hop.TimedOut || hop.IP != "" {
		t.Errorf("hop = %+v, esperava TimedOut=true e IP vazio", hop)
	}
}

func TestParseTracerouteLine_MarksTargetHop(t *testing.T) {
	hop, ok := parseTracerouteLine(" 5  8.8.8.8  12.500 ms", "8.8.8.8")
	if !ok {
		t.Fatal("esperava reconhecer a linha como salto")
	}
	if !hop.IsTarget {
		t.Error("hop cujo IP bate com o alvo deveria ter IsTarget=true")
	}
}

// TestParseTracerouteLine_MixedProbesInSameHop cobre o caso de algumas sondas de um mesmo salto
// responderem e outras não (achado documentado no comentário de parseTracerouteLine) — não deve
// contar como TimedOut, já que o hop respondeu pelo menos uma vez.
func TestParseTracerouteLine_MixedProbesInSameHop(t *testing.T) {
	hop, ok := parseTracerouteLine(" 6  172.217.0.1  10.000 ms  *  12.000 ms", "8.8.8.8")
	if !ok {
		t.Fatal("esperava reconhecer a linha como salto")
	}
	if hop.TimedOut {
		t.Error("hop com pelo menos uma sonda respondendo não deveria ser TimedOut")
	}
	if hop.RTTMs != 11.0 {
		t.Errorf("RTTMs = %f, esperava a média das 2 sondas que responderam (11.0)", hop.RTTMs)
	}
}

// TestParseTracerouteLine_TcptracerouteHeaderLinesIgnored cobre as duas linhas de cabeçalho reais
// do tcptraceroute (confirmadas rodando ao vivo contra a imagem netshoot), diferentes das do
// traceroute genérico — nenhuma começa com dígito, então não deveriam ser reconhecidas como salto.
func TestParseTracerouteLine_TcptracerouteHeaderLinesIgnored(t *testing.T) {
	headers := []string{
		"Selected device eth0, address 172.23.230.72, port 51949 for outgoing packets",
		"Tracing the path to 8.8.8.8 on TCP port 443 (https), 30 hops max",
		"Destination not reached",
	}
	for _, h := range headers {
		if _, ok := parseTracerouteLine(h, "8.8.8.8"); ok {
			t.Errorf("linha de cabeçalho/status %q não deveria ser reconhecida como salto", h)
		}
	}
}

// TestParseTracerouteLine_TcptracerouteOpenPortSuffix cobre o achado real: o salto final do
// tcptraceroute inclui um sufixo "[open]"/"[closed]" entre o IP e a latência quando consegue
// determinar o estado da porta — não deveria atrapalhar nem a extração do IP nem a do RTT.
func TestParseTracerouteLine_TcptracerouteOpenPortSuffix(t *testing.T) {
	hop, ok := parseTracerouteLine(" 12  8.8.8.8 [open]  41.755 ms", "8.8.8.8")
	if !ok {
		t.Fatal("esperava reconhecer a linha como salto")
	}
	if hop.IP != "8.8.8.8" {
		t.Errorf("IP = %q, esperava 8.8.8.8 (sufixo [open] não deveria vazar pro campo IP)", hop.IP)
	}
	if hop.RTTMs < 41.7 || hop.RTTMs > 41.8 {
		t.Errorf("RTTMs = %f, esperava ~41.755", hop.RTTMs)
	}
	if !hop.IsTarget {
		t.Error("hop deveria ser marcado como IsTarget")
	}
}

func TestResolveTarget_AlreadyAnIP(t *testing.T) {
	ip, resolved, err := resolveTarget(context.Background(), "8.8.8.8")
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if resolved {
		t.Error("um IP literal não deveria precisar de resolução")
	}
	if ip != "8.8.8.8" {
		t.Errorf("ip = %q, esperava 8.8.8.8", ip)
	}
}

func TestResolveTarget_EmptyInput(t *testing.T) {
	_, _, err := resolveTarget(context.Background(), "   ")
	if err == nil {
		t.Fatal("esperava erro pra entrada vazia")
	}
}

// TestStreamCommandLines_CallbackFiresPerLine confirma o mecanismo central da animação ao vivo
// (pedido do usuário): cada linha escrita pelo `run` chega em `onLine` — inclusive quando o
// comando escreve em pedaços (io.Writer.Write chamado várias vezes, como aconteceria de verdade
// com dados chegando aos poucos de um processo real), e mesmo a última linha sem newline final.
func TestStreamCommandLines_CallbackFiresPerLine(t *testing.T) {
	var got []string
	err := streamCommandLines(context.Background(), func(stdout io.Writer) error {
		_, _ = stdout.Write([]byte("linha 1\nlinha 2\n"))
		_, _ = stdout.Write([]byte("linha "))
		_, _ = stdout.Write([]byte("3\n"))
		_, _ = stdout.Write([]byte("linha 4 sem newline final"))
		return nil
	}, func(line string) {
		got = append(got, line)
	})
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	want := []string{"linha 1", "linha 2", "linha 3", "linha 4 sem newline final"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("linha %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestStreamCommandLines_PropagatesRunError garante que um erro do `run` (ex: comando saiu com
// código != 0) continua chegando ao chamador mesmo com o mecanismo de streaming no meio.
func TestStreamCommandLines_PropagatesRunError(t *testing.T) {
	sentinel := errTestSentinel{}
	err := streamCommandLines(context.Background(), func(stdout io.Writer) error {
		_, _ = stdout.Write([]byte("alguma coisa\n"))
		return sentinel
	}, func(line string) {})
	if err != sentinel {
		t.Fatalf("erro = %v, want %v", err, sentinel)
	}
}

type errTestSentinel struct{}

func (errTestSentinel) Error() string { return "sentinel" }
