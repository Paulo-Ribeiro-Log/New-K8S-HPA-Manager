package healthcheck

import (
	"testing"
	"time"
)

// TestNewEventChecker verifica criação do event checker
func TestNewEventChecker(t *testing.T) {
	checker := NewEventChecker()

	if checker == nil {
		t.Fatal("NewEventChecker retornou nil")
	}

	if checker.MaxEventAge != 1*time.Hour {
		t.Errorf("MaxEventAge = %v, want 1h", checker.MaxEventAge)
	}
}

// TestSetMaxEventAge verifica configuração de idade máxima de eventos
func TestSetMaxEventAge(t *testing.T) {
	checker := NewEventChecker()

	checker.SetMaxEventAge(30 * time.Minute)

	if checker.MaxEventAge != 30*time.Minute {
		t.Errorf("MaxEventAge = %v, want 30m", checker.MaxEventAge)
	}
}

// TestDetermineSeverity_Critical verifica que eventos críticos são identificados corretamente
func TestDetermineSeverity_Critical(t *testing.T) {
	checker := NewEventChecker()

	criticalReasons := []string{
		"FailedScheduling",
		"BackOff",
		"CrashLoopBackOff",
		"Failed",
		"FailedCreate",
		"ErrImagePull",
		"ImagePullBackOff",
		"FailedMount",
		"FailedAttachVolume",
		"NodeNotReady",
		"InsufficientCPU",
		"InsufficientMemory",
		"OutOfMemory",
		"OOMKilling",
	}

	for _, reason := range criticalReasons {
		severity := checker.determineSeverity(reason)
		if severity != SeverityCritical {
			t.Errorf("determineSeverity(%q) = %v, want SeverityCritical", reason, severity)
		}
	}
}

// TestDetermineSeverity_Medium verifica que eventos de warning são identificados como Medium
func TestDetermineSeverity_Medium(t *testing.T) {
	checker := NewEventChecker()

	// Eventos que não são críticos são tratados como Medium (antigo "warning")
	mediumReasons := []string{
		"Unhealthy",
		"ProbeWarning",
		"FailedGetScale",
		"FailedRescale",
		"FailedUpdate",
		"Evicted",
		"Preempted",
		"VolumeResizeFailed",
		"ProvisioningFailed",
	}

	for _, reason := range mediumReasons {
		severity := checker.determineSeverity(reason)
		if severity != SeverityMedium {
			t.Errorf("determineSeverity(%q) = %v, want SeverityMedium", reason, severity)
		}
	}
}

// TestDetermineSeverity_Unknown verifica que eventos desconhecidos são tratados como Medium
func TestDetermineSeverity_Unknown(t *testing.T) {
	checker := NewEventChecker()

	// Reason desconhecido deve ser tratado como Medium (não crítico, não info)
	severity := checker.determineSeverity("SomeUnknownReason")
	if severity != SeverityMedium {
		t.Errorf("determineSeverity(unknown) = %v, want SeverityMedium", severity)
	}
}

// TestGetSuggestions_FailedScheduling verifica sugestões para FailedScheduling
func TestGetSuggestions_FailedScheduling(t *testing.T) {
	checker := NewEventChecker()

	suggestions := checker.getSuggestions("FailedScheduling", "Pod", "default", "my-pod")

	if len(suggestions) == 0 {
		t.Fatal("getSuggestions retornou lista vazia para FailedScheduling")
	}

	// Deve conter sugestão sobre nodes
	found := false
	for _, s := range suggestions {
		if containsString(s, "nodes") || containsString(s, "recursos") {
			found = true
			break
		}
	}
	if !found {
		t.Error("Sugestões para FailedScheduling devem mencionar nodes ou recursos")
	}
}

// TestGetSuggestions_CrashLoopBackOff verifica sugestões para CrashLoopBackOff
func TestGetSuggestions_CrashLoopBackOff(t *testing.T) {
	checker := NewEventChecker()

	suggestions := checker.getSuggestions("CrashLoopBackOff", "Pod", "production", "api-server")

	if len(suggestions) == 0 {
		t.Fatal("getSuggestions retornou lista vazia para CrashLoopBackOff")
	}

	// Deve conter sugestão de logs
	found := false
	for _, s := range suggestions {
		if containsString(s, "logs") {
			found = true
			break
		}
	}
	if !found {
		t.Error("Sugestões para CrashLoopBackOff devem mencionar logs")
	}
}

// TestGetSuggestions_ImagePullBackOff verifica sugestões para ImagePullBackOff
func TestGetSuggestions_ImagePullBackOff(t *testing.T) {
	checker := NewEventChecker()

	suggestions := checker.getSuggestions("ImagePullBackOff", "Pod", "default", "my-pod")

	if len(suggestions) == 0 {
		t.Fatal("getSuggestions retornou lista vazia para ImagePullBackOff")
	}

	// Deve conter sugestão sobre imagem ou registry
	found := false
	for _, s := range suggestions {
		if containsString(s, "imagem") || containsString(s, "registry") || containsString(s, "ImagePullSecrets") {
			found = true
			break
		}
	}
	if !found {
		t.Error("Sugestões para ImagePullBackOff devem mencionar imagem ou registry")
	}
}

// TestGetSuggestions_FailedMount verifica sugestões para FailedMount
func TestGetSuggestions_FailedMount(t *testing.T) {
	checker := NewEventChecker()

	suggestions := checker.getSuggestions("FailedMount", "Pod", "default", "db-pod")

	if len(suggestions) == 0 {
		t.Fatal("getSuggestions retornou lista vazia para FailedMount")
	}

	// Deve conter sugestão sobre PV/PVC
	found := false
	for _, s := range suggestions {
		if containsString(s, "PV") || containsString(s, "PVC") || containsString(s, "pvc") {
			found = true
			break
		}
	}
	if !found {
		t.Error("Sugestões para FailedMount devem mencionar PV ou PVC")
	}
}

// TestGetSuggestions_OOMKilling verifica sugestões para OOMKilling
func TestGetSuggestions_OOMKilling(t *testing.T) {
	checker := NewEventChecker()

	suggestions := checker.getSuggestions("OOMKilling", "Pod", "default", "memory-hog")

	if len(suggestions) == 0 {
		t.Fatal("getSuggestions retornou lista vazia para OOMKilling")
	}

	// Deve conter sugestão sobre memória
	found := false
	for _, s := range suggestions {
		if containsString(s, "memória") || containsString(s, "memory") || containsString(s, "limits") {
			found = true
			break
		}
	}
	if !found {
		t.Error("Sugestões para OOMKilling devem mencionar memória ou limits")
	}
}

// TestGetCriticalCount verifica contagem de eventos críticos
func TestGetCriticalCount(t *testing.T) {
	checker := NewEventChecker()

	events := []EventHealth{
		{Severity: SeverityCritical, Status: StatusCritical},
		{Severity: SeverityMedium, Status: StatusWarning},
		{Severity: SeverityCritical, Status: StatusCritical},
		{Severity: SeverityMedium, Status: StatusWarning},
		{Severity: SeverityCritical, Status: StatusCritical},
	}

	count := checker.GetCriticalCount(events)
	if count != 3 {
		t.Errorf("GetCriticalCount() = %d, want 3", count)
	}
}

// TestGetWarningCount verifica contagem de eventos Medium (warnings)
func TestGetWarningCount(t *testing.T) {
	checker := NewEventChecker()

	events := []EventHealth{
		{Severity: SeverityCritical, Status: StatusCritical},
		{Severity: SeverityMedium, Status: StatusWarning},
		{Severity: SeverityCritical, Status: StatusCritical},
		{Severity: SeverityMedium, Status: StatusWarning},
	}

	count := checker.GetWarningCount(events)
	if count != 2 {
		t.Errorf("GetWarningCount() = %d, want 2", count)
	}
}

// TestGetCriticalCount_Empty verifica contagem com lista vazia
func TestGetCriticalCount_Empty(t *testing.T) {
	checker := NewEventChecker()

	events := []EventHealth{}

	count := checker.GetCriticalCount(events)
	if count != 0 {
		t.Errorf("GetCriticalCount(empty) = %d, want 0", count)
	}
}

// TestGroupByResource verifica agrupamento por recurso
func TestGroupByResource(t *testing.T) {
	checker := NewEventChecker()

	events := []EventHealth{
		{Namespace: "default", InvolvedKind: "Pod", InvolvedName: "pod-1"},
		{Namespace: "default", InvolvedKind: "Pod", InvolvedName: "pod-1"},
		{Namespace: "default", InvolvedKind: "Pod", InvolvedName: "pod-2"},
		{Namespace: "kube-system", InvolvedKind: "Node", InvolvedName: "node-1"},
	}

	grouped := checker.GroupByResource(events)

	if len(grouped) != 3 {
		t.Errorf("GroupByResource() retornou %d grupos, want 3", len(grouped))
	}

	// Verificar grupo default/Pod/pod-1
	key := "default/Pod/pod-1"
	if len(grouped[key]) != 2 {
		t.Errorf("Grupo %s tem %d eventos, want 2", key, len(grouped[key]))
	}
}

// TestFilterByKind verifica filtro por tipo de recurso
func TestFilterByKind(t *testing.T) {
	checker := NewEventChecker()

	events := []EventHealth{
		{InvolvedKind: "Pod", InvolvedName: "pod-1"},
		{InvolvedKind: "Node", InvolvedName: "node-1"},
		{InvolvedKind: "Pod", InvolvedName: "pod-2"},
		{InvolvedKind: "Deployment", InvolvedName: "deploy-1"},
	}

	// Filtrar apenas Pods
	podEvents := checker.FilterByKind(events, "Pod")
	if len(podEvents) != 2 {
		t.Errorf("FilterByKind(Pod) retornou %d eventos, want 2", len(podEvents))
	}

	// Filtrar apenas Nodes
	nodeEvents := checker.FilterByKind(events, "Node")
	if len(nodeEvents) != 1 {
		t.Errorf("FilterByKind(Node) retornou %d eventos, want 1", len(nodeEvents))
	}

	// Filtro case-insensitive
	podEventsLower := checker.FilterByKind(events, "pod")
	if len(podEventsLower) != 2 {
		t.Errorf("FilterByKind(pod) deve ser case-insensitive, retornou %d eventos, want 2", len(podEventsLower))
	}
}

// TestFilterByKind_NotFound verifica filtro com tipo inexistente
func TestFilterByKind_NotFound(t *testing.T) {
	checker := NewEventChecker()

	events := []EventHealth{
		{InvolvedKind: "Pod", InvolvedName: "pod-1"},
		{InvolvedKind: "Node", InvolvedName: "node-1"},
	}

	// Filtrar tipo inexistente
	filtered := checker.FilterByKind(events, "Secret")
	if len(filtered) != 0 {
		t.Errorf("FilterByKind(Secret) retornou %d eventos, want 0", len(filtered))
	}
}

// TestSortBySeverity verifica ordenação por severidade
func TestSortBySeverity(t *testing.T) {
	checker := NewEventChecker()

	now := time.Now()
	events := []EventHealth{
		{Severity: SeverityMedium, LastTimestamp: now.Add(-1 * time.Minute)},
		{Severity: SeverityCritical, LastTimestamp: now.Add(-2 * time.Minute)},
		{Severity: SeverityMedium, LastTimestamp: now.Add(-3 * time.Minute)},
		{Severity: SeverityCritical, LastTimestamp: now},
	}

	checker.sortBySeverity(events)

	// Críticos devem vir primeiro
	if events[0].Severity != SeverityCritical {
		t.Error("Primeiro evento deveria ser crítico")
	}
	if events[1].Severity != SeverityCritical {
		t.Error("Segundo evento deveria ser crítico")
	}

	// Dentro dos críticos, mais recente primeiro
	if events[0].LastTimestamp.Before(events[1].LastTimestamp) {
		t.Error("Eventos críticos devem estar ordenados por timestamp (mais recente primeiro)")
	}
}

// TestCriticalEventReasons verifica que todos os reasons críticos estão mapeados
func TestCriticalEventReasons(t *testing.T) {
	expectedCritical := []string{
		"FailedScheduling",
		"BackOff",
		"CrashLoopBackOff",
		"Failed",
		"ErrImagePull",
		"ImagePullBackOff",
		"FailedMount",
		"FailedAttachVolume",
		"NodeNotReady",
		"InsufficientCPU",
		"InsufficientMemory",
		"OutOfMemory",
		"OOMKilling",
	}

	for _, reason := range expectedCritical {
		if !CriticalEventReasons[reason] {
			t.Errorf("Reason %q deveria estar em CriticalEventReasons", reason)
		}
	}
}

// TestEventHealthStatus verifica mapeamento correto de severidade para status
func TestEventHealthStatus(t *testing.T) {
	checker := NewEventChecker()

	// Evento crítico deve ter SeverityCritical
	if checker.determineSeverity("FailedScheduling") != SeverityCritical {
		t.Error("FailedScheduling deve ser SeverityCritical")
	}

	// Evento de warning deve ter SeverityMedium
	if checker.determineSeverity("Unhealthy") != SeverityMedium {
		t.Error("Unhealthy deve ser SeverityMedium")
	}
}

// Helper function
func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && searchString(s, substr)))
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
