package handlers

import (
	"testing"
	"time"
)

// TestNmapAdvancedScanArgs_Structure confirma a estrutura do comando — timeout externo, flags
// -sT/-sV/-Pn/-n (nunca SYN scan, ver comentário completo em net_discovery_nmap.go sobre por que
// -sT é obrigatório pra funcionar sem NET_RAW), -oG - (saída greppable) e portas juntas por vírgula.
func TestNmapAdvancedScanArgs_Structure(t *testing.T) {
	args := nmapAdvancedScanArgs("10.0.0.5", []int{22, 80, 443})
	want := []string{
		"timeout", "25",
		"nmap", "-sT", "-sV", "-Pn", "-n", "--version-intensity", "5",
		"-p", "22,80,443", "-oG", "-", "10.0.0.5",
	}
	if len(args) != len(want) {
		t.Fatalf("len(args) = %d, want %d — args: %v", len(args), len(want), args)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Errorf("args[%d] = %q, want %q", i, args[i], want[i])
		}
	}
}

func TestNmapAdvancedScanArgs_NeverUsesSYNScan(t *testing.T) {
	args := nmapAdvancedScanArgs("10.0.0.5", []int{80})
	for _, a := range args {
		if a == "-sS" {
			t.Fatal("nunca deveria usar -sS (SYN scan) — exige NET_RAW, achado real testado ao vivo: -sT funciona sem essa capability, -sS/-O não")
		}
	}
}

// TestParseNmapGreppableOutput_RealCapture usa a saída REAL capturada rodando nmap -sT -sV -oG -
// ao vivo contra scanme.nmap.org (host oficial do próprio nmap, autorizado pra isso) — não é uma
// saída inventada, garante que o parser bate com o formato de verdade.
func TestParseNmapGreppableOutput_RealCapture(t *testing.T) {
	output := `# Nmap 7.94 scan initiated Fri Aug 28 13:42:22 2026 as: nmap -sT -sV -Pn --version-intensity 5 -p 22,80,443,3389,6379 -oG - scanme.nmap.org
Host: 45.33.32.156 (scanme.nmap.org)	Status: Up
Host: 45.33.32.156 (scanme.nmap.org)	Ports: 22/open/tcp//ssh//OpenSSH 6.6.1p1 Ubuntu 2ubuntu2.13 (Ubuntu Linux; protocol 2.0)/, 80/open/tcp//http//Apache httpd 2.4.7 ((Ubuntu))/, 443/closed/tcp//https///, 3389/closed/tcp//ms-wbt-server///, 6379/closed/tcp//redis///
# Nmap done at Fri Aug 28 13:42:29 2026 -- 1 IP address (1 host up) scanned in 6.98 seconds`

	got := parseNmapGreppableOutput(output)
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2 (só as portas OPEN — 443/3389/6379 são closed, não deveriam aparecer). got: %+v", len(got), got)
	}

	if got[0].Port != 22 || got[0].Service != "ssh" || got[0].Version != "OpenSSH 6.6.1p1 Ubuntu 2ubuntu2.13 (Ubuntu Linux; protocol 2.0)" {
		t.Errorf("got[0] = %+v, inesperado", got[0])
	}
	if got[1].Port != 80 || got[1].Service != "http" || got[1].Version != "Apache httpd 2.4.7 ((Ubuntu))" {
		t.Errorf("got[1] = %+v, inesperado", got[1])
	}
}

func TestParseNmapGreppableOutput_NoPortsLineReturnsEmpty(t *testing.T) {
	got := parseNmapGreppableOutput("# Nmap 7.94 scan initiated...\n# Nmap done at ... -- 0 hosts up")
	if len(got) != 0 {
		t.Errorf("got = %+v, esperava vazio (nenhuma linha \"Ports:\" na saída)", got)
	}
}

func TestParseNmapGreppableOutput_ClosedPortsExcluded(t *testing.T) {
	output := "Host: 10.0.0.1 ()\tPorts: 443/closed/tcp//https///"
	got := parseNmapGreppableOutput(output)
	if len(got) != 0 {
		t.Errorf("got = %+v, esperava vazio (porta closed não deveria aparecer, mesmo sem versão)", got)
	}
}

// TestParseNmapGreppableOutput_VersionWithEmbeddedSlash cobre o achado documentado no comentário
// da função: a versão pode conter "/" — a reconstrução via fields[6:len(fields)-1] precisa
// preservar isso, não só pegar fields[6] sozinho.
func TestParseNmapGreppableOutput_VersionWithEmbeddedSlash(t *testing.T) {
	output := "Host: 10.0.0.1 ()\tPorts: 8080/open/tcp//http//Custom Server 1.0/build-123/"
	got := parseNmapGreppableOutput(output)
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1. got: %+v", len(got), got)
	}
	if got[0].Version != "Custom Server 1.0/build-123" {
		t.Errorf("Version = %q, want %q (deveria preservar o \"/\" embutido)", got[0].Version, "Custom Server 1.0/build-123")
	}
}

func TestParseNmapGreppableOutput_ServiceOnlyNoVersion(t *testing.T) {
	output := "Host: 10.0.0.1 ()\tPorts: 22/open/tcp//ssh///"
	got := parseNmapGreppableOutput(output)
	if len(got) != 1 || got[0].Service != "ssh" || got[0].Version != "" {
		t.Errorf("got = %+v, esperava 1 entrada com service=ssh e version vazia", got)
	}
}

// TestNetDiscoveryTargetTimeout — cobre a integração entre o orçamento extra da detecção avançada
// de serviço (nmap) e o cap absoluto já existente (netDiscoveryOverallTimeoutCap).
func TestNetDiscoveryTargetTimeout(t *testing.T) {
	base := computeOverallTimeout(netDiscoveryProbeTimeoutSec, netDiscoveryProbeCount)

	t.Run("sem advanced scan é idêntico a computeOverallTimeout", func(t *testing.T) {
		got := netDiscoveryTargetTimeout(netDiscoveryProbeTimeoutSec, netDiscoveryProbeCount, false)
		if got != base {
			t.Errorf("got = %v, want %v (idêntico sem a flag)", got, base)
		}
	})

	t.Run("com advanced scan soma o orçamento extra", func(t *testing.T) {
		got := netDiscoveryTargetTimeout(netDiscoveryProbeTimeoutSec, netDiscoveryProbeCount, true)
		want := base + netDiscoveryAdvancedScanBudget
		if want > netDiscoveryOverallTimeoutCap {
			want = netDiscoveryOverallTimeoutCap
		}
		if got != want {
			t.Errorf("got = %v, want %v", got, want)
		}
		if got <= base {
			t.Errorf("got = %v, deveria ser maior que o base %v quando advancedServiceScan=true", got, base)
		}
	})

	t.Run("nunca ultrapassa o cap mesmo no combo mais extremo", func(t *testing.T) {
		got := netDiscoveryTargetTimeout(netDiscoveryProbeTimeoutMaxSec, netDiscoveryProbeCountMax, true)
		if got > netDiscoveryOverallTimeoutCap {
			t.Errorf("got = %v, excede netDiscoveryOverallTimeoutCap (%v)", got, netDiscoveryOverallTimeoutCap)
		}
	})
}

func TestNetDiscoveryAdvancedScanBudget_IsPositive(t *testing.T) {
	if netDiscoveryAdvancedScanBudget <= 0 {
		t.Fatal("netDiscoveryAdvancedScanBudget deveria ser positivo")
	}
	if netDiscoveryAdvancedScanBudget < time.Duration(netDiscoveryAdvancedScanTimeoutSec)*time.Second {
		t.Errorf("orçamento (%v) deveria cobrir com folga o timeout do próprio subprocesso (%ds)", netDiscoveryAdvancedScanBudget, netDiscoveryAdvancedScanTimeoutSec)
	}
}
