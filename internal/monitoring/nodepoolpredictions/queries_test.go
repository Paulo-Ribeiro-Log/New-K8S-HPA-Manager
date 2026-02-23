package nodepoolpredictions

import (
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// BuildInstanceRegex
// ---------------------------------------------------------------------------

func TestBuildInstanceRegex_SingleIP(t *testing.T) {
	result := BuildInstanceRegex([]string{"10.0.0.1:9100"})
	// Pontos devem ser escapados como "\\." (backslash-backslash-ponto)
	if result != `10\\.0\\.0\\.1:9100` {
		t.Errorf("esperado '10\\\\.0\\\\.0\\\\.1:9100', obtido %q", result)
	}
}

func TestBuildInstanceRegex_MultipleIPs(t *testing.T) {
	instances := []string{"10.0.0.1:9100", "10.0.0.2:9100", "192.168.1.100:9100"}
	result := BuildInstanceRegex(instances)

	parts := strings.Split(result, "|")
	if len(parts) != 3 {
		t.Fatalf("esperado 3 partes separadas por '|', obtido %d: %q", len(parts), result)
	}

	// Cada parte deve ter os pontos escapados como "\\."
	for _, p := range parts {
		if strings.Contains(p, "\\.") && !strings.Contains(p, `\\.`) {
			t.Errorf("parte %q contém '\\.' simples (inválido em PromQL) — esperado '\\\\.'", p)
		}
	}
}

func TestBuildInstanceRegex_Empty(t *testing.T) {
	result := BuildInstanceRegex([]string{})
	if result != "" {
		t.Errorf("esperado string vazia para entrada vazia, obtido %q", result)
	}
}

func TestBuildInstanceRegex_NoDotsInPort(t *testing.T) {
	// Porta não tem ponto — não deve ser alterada
	result := BuildInstanceRegex([]string{"10.1.2.3:9100"})
	if !strings.Contains(result, ":9100") {
		t.Errorf("porta :9100 foi incorretamente modificada em %q", result)
	}
}

// ---------------------------------------------------------------------------
// BuildNodeNameRegex
// ---------------------------------------------------------------------------

func TestBuildNodeNameRegex_Single(t *testing.T) {
	result := BuildNodeNameRegex([]string{"aks-nodepool1-12345-0"})
	if result != "aks-nodepool1-12345-0" {
		t.Errorf("esperado nome inalterado, obtido %q", result)
	}
}

func TestBuildNodeNameRegex_Multiple(t *testing.T) {
	nodes := []string{"node-a", "node-b", "node-c"}
	result := BuildNodeNameRegex(nodes)
	if result != "node-a|node-b|node-c" {
		t.Errorf("esperado 'node-a|node-b|node-c', obtido %q", result)
	}
}

func TestBuildNodeNameRegex_Empty(t *testing.T) {
	result := BuildNodeNameRegex([]string{})
	if result != "" {
		t.Errorf("esperado string vazia para entrada vazia, obtido %q", result)
	}
}

// ---------------------------------------------------------------------------
// ConntrackStatusFromPercent
// ---------------------------------------------------------------------------

func TestConntrackStatusFromPercent(t *testing.T) {
	cases := []struct {
		pct      float64
		expected string
	}{
		{0, "ok"},
		{50, "ok"},
		{69.9, "ok"},
		{70, "warning"},
		{80, "warning"},
		{84.9, "warning"},
		{85, "critical"},
		{90, "critical"},
		{94.9, "critical"},
		{95, "emergency"},
		{99.9, "emergency"},
		{100, "emergency"},
	}

	for _, tc := range cases {
		got := ConntrackStatusFromPercent(tc.pct)
		if got != tc.expected {
			t.Errorf("ConntrackStatusFromPercent(%.1f) = %q, esperado %q", tc.pct, got, tc.expected)
		}
	}
}

// ---------------------------------------------------------------------------
// DayOffsets
// ---------------------------------------------------------------------------

func TestDayOffsets_Values(t *testing.T) {
	offsets := DayOffsets()

	expected := map[string]time.Duration{
		"D-3":  3 * 24 * time.Hour,
		"D-7":  7 * 24 * time.Hour,
		"D-14": 14 * 24 * time.Hour,
	}

	for key, exp := range expected {
		got, ok := offsets[key]
		if !ok {
			t.Errorf("chave %q não encontrada em DayOffsets()", key)
			continue
		}
		if got != exp {
			t.Errorf("DayOffsets()[%q] = %v, esperado %v", key, got, exp)
		}
	}

	if len(offsets) != 3 {
		t.Errorf("esperado 3 entradas em DayOffsets(), obtido %d", len(offsets))
	}
}

// ---------------------------------------------------------------------------
// formatDuration (função interna — testada via queries geradas)
// ---------------------------------------------------------------------------

func TestFormatDuration_Days(t *testing.T) {
	q := NewNodePoolQueries()
	// Gerar uma query com offset de 3 dias para verificar a formatação
	regex := BuildInstanceRegex([]string{"10.0.0.1:9100"})
	query := q.GetNodeCPUUsageQuery(regex, 3*24*time.Hour)
	if !strings.Contains(query, "offset 3d") {
		t.Errorf("query com offset 3 dias deveria conter 'offset 3d', obtido: %s", query)
	}
}

func TestFormatDuration_Hours(t *testing.T) {
	q := NewNodePoolQueries()
	regex := BuildInstanceRegex([]string{"10.0.0.1:9100"})
	query := q.GetNodeCPUUsageQuery(regex, 6*time.Hour)
	if !strings.Contains(query, "offset 6h") {
		t.Errorf("query com offset 6 horas deveria conter 'offset 6h', obtido: %s", query)
	}
}

func TestFormatDuration_NoOffset(t *testing.T) {
	q := NewNodePoolQueries()
	regex := BuildInstanceRegex([]string{"10.0.0.1:9100"})
	query := q.GetNodeCPUUsageQuery(regex, 0)
	if strings.Contains(query, "offset") {
		t.Errorf("query sem offset não deveria conter 'offset', obtido: %s", query)
	}
}

// ---------------------------------------------------------------------------
// Queries — smoke tests (verifica estrutura básica, sem Prometheus real)
// ---------------------------------------------------------------------------

func TestGetNodeCPUUsageQuery_ContainsFilter(t *testing.T) {
	q := NewNodePoolQueries()
	regex := `10\\.0\\.0\\.1:9100|10\\.0\\.0\\.2:9100`
	query := q.GetNodeCPUUsageQuery(regex, 0)

	if !strings.Contains(query, regex) {
		t.Errorf("query de CPU deveria conter o regex de instâncias")
	}
	if !strings.Contains(query, `mode="idle"`) {
		t.Errorf("query de CPU deveria filtrar mode=idle")
	}
	if !strings.Contains(query, "node_cpu_seconds_total") {
		t.Errorf("query de CPU deveria usar node_cpu_seconds_total")
	}
}

func TestGetConntrackUsagePercentQuery(t *testing.T) {
	q := NewNodePoolQueries()
	regex := `10\\.0\\.0\\.1:9100`
	query := q.GetConntrackUsagePercentQuery(regex)

	if !strings.Contains(query, "node_nf_conntrack_entries") {
		t.Errorf("query conntrack deveria usar node_nf_conntrack_entries")
	}
	if !strings.Contains(query, "100") {
		t.Errorf("query conntrack deveria multiplicar por 100 para obter porcentagem")
	}
}

func TestGetNodeDiskUsagePercentQuery_RootMount(t *testing.T) {
	q := NewNodePoolQueries()
	regex := `10\\.0\\.0\\.1:9100`
	query := q.GetNodeDiskUsagePercentQuery(regex)

	if !strings.Contains(query, `mountpoint="/"`) {
		t.Errorf("query de disco deveria filtrar mountpoint=/")
	}
	if !strings.Contains(query, `fstype!="tmpfs"`) {
		t.Errorf("query de disco deveria excluir tmpfs")
	}
}

func TestGetConntrackEntriesClusterQuery_NoFilter(t *testing.T) {
	q := NewNodePoolQueries()
	query := q.GetConntrackEntriesClusterQuery()

	// Query cluster-wide NÃO deve ter filtro de instância
	if strings.Contains(query, "instance") {
		t.Errorf("query cluster-wide não deveria ter filtro de instância, obtido: %s", query)
	}
}
