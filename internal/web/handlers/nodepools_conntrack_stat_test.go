package handlers

import (
	"reflect"
	"testing"
)

// Formato de /proc/net/stat/nf_conntrack em kernel 5.x: cabeçalho + 1 linha hexa por CPU (valores de exemplo).
const nfStatSample = `entries  clashres found new invalid ignore delete chainlength insert insert_failed drop early_drop icmp_error  expect_new expect_create expect_delete search_restart
00000c21  00000000 00000000 00000000 00000010 0000aaaa 00000000 00000000 00000000 00000002 00000005 00000001 00000000  00000000 00000000 00000000 00000003
00000c21  00000000 00000000 00000000 00000004 0000bbbb 00000000 00000000 00000000 00000001 0000000a 00000000 00000000  00000000 00000000 00000000 00000000
`

// Saída real de `conntrack -S` num nó AKS (kernel 5.15.0-1111-azure, sem /proc/net/stat/nf_conntrack).
const conntrackSSample = `cpu=0           found=986 invalid=89035 insert=0 insert_failed=4 drop=4 early_drop=939985 error=1 search_restart=2329761182 clash_resolve=770239427 chaintoolong=0
cpu=1           found=863 invalid=80118 insert=0 insert_failed=1 drop=1 early_drop=938060 error=17 search_restart=2313195993 clash_resolve=771663566 chaintoolong=0
`

func TestParseDropCounters_ConntrackS(t *testing.T) {
	dc := parseDropCounters("\nconntrack-S\n" + conntrackSSample)
	if dc.Drop != 5 || dc.EarlyDrop != 939985+938060 || dc.InsertFailed != 5 || dc.Source != "conntrack -S" || dc.Err != "" {
		t.Errorf("totais = %+v", dc)
	}
	want := []ConntrackCPUDrops{
		{CPU: 0, Drop: 4, EarlyDrop: 939985, InsertFailed: 4},
		{CPU: 1, Drop: 1, EarlyDrop: 938060, InsertFailed: 1},
	}
	if !reflect.DeepEqual(dc.PerCPU, want) {
		t.Errorf("por CPU = %+v", dc.PerCPU)
	}
}

func TestParseDropCounters_Procfs(t *testing.T) {
	dc := parseDropCounters("\nprocfs\n" + nfStatSample)
	if dc.Drop != 15 || dc.EarlyDrop != 1 || dc.InsertFailed != 3 || dc.Source != "procfs" {
		t.Errorf("totais = %+v", dc)
	}
	if len(dc.PerCPU) != 2 || dc.PerCPU[1] != (ConntrackCPUDrops{CPU: 1, Drop: 10, EarlyDrop: 0, InsertFailed: 1}) {
		t.Errorf("por CPU = %+v", dc.PerCPU)
	}
	// Kernel antigo sem a coluna early_drop
	dc = parseDropCounters("procfs\nentries drop\n00000001 00000007\n")
	if dc.Drop != 7 || dc.EarlyDrop != -1 {
		t.Errorf("sem early_drop: %+v", dc)
	}
}

func TestParseDropCounters_Unavailable(t *testing.T) {
	if dc := parseDropCounters("\nnone\n"); dc.Drop != -1 || dc.Err == "" {
		t.Errorf("none: %+v", dc)
	}
	// conntrack sem CAP_NET_ADMIN
	if dc := parseDropCounters("conntrack-S\nconntrack v1.4.6 (conntrack-tools): Operation not permitted\n"); dc.Drop != -1 || dc.Err == "" || dc.PerCPU != nil {
		t.Errorf("sem permissão: %+v", dc)
	}
	if dc := parseDropCounters(""); dc.Drop != -1 || dc.Err == "" {
		t.Errorf("vazio: %+v", dc)
	}
}

func TestSplitConntrackProbeOutput(t *testing.T) {
	out := "100\n262144\n65536\n65530\n" + conntrackStatMarker + "\nconntrack-S\n" + conntrackSSample + conntrackUptimeMarker + "\n86400.55 1000.00\n"
	sys, stat, up := splitConntrackProbeOutput(out)
	if sys != "100\n262144\n65536\n65530\n" {
		t.Errorf("sysctls = %q", sys)
	}
	if dc := parseDropCounters(stat); dc.Drop != 5 {
		t.Errorf("drop = %d", dc.Drop)
	}
	if parseUptimeSeconds(up) != 86400 {
		t.Errorf("uptime = %d", parseUptimeSeconds(up))
	}
	// Exec sem marcadores: tudo vai pra sysctls e os contadores ficam -1
	sys, stat, up = splitConntrackProbeOutput("1\n2\n")
	if sys != "1\n2\n" || parseDropCounters(stat).Drop != -1 || parseUptimeSeconds(up) != -1 {
		t.Errorf("sem marcadores: %q %q %q", sys, stat, up)
	}
}
