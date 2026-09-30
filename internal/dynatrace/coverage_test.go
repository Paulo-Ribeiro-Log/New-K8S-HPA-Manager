package dynatrace

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

// Tenant falso no formato real da Entities API v2 (relações planas {"id","type"}): 1 host group,
// 2 hosts, PGIs em namespace de app, em namespace excluído (kube-system) e um sem serviço.
func coverageFakeTenant(t *testing.T) *httptest.Server {
	t.Helper()
	pgiWith := func(id, ns, host, pg string, extra map[string]interface{}, services ...string) map[string]interface{} {
		svcRefs := []map[string]string{}
		for _, s := range services {
			svcRefs = append(svcRefs, map[string]string{"id": s, "type": "SERVICE"})
		}
		props := map[string]interface{}{
			"metadata": []map[string]string{
				{"key": "KUBERNETES_NAMESPACE", "value": ns},
				{"key": "KUBERNETES_FULL_POD_NAME", "value": "pod-" + strings.ToLower(id)},
			},
		}
		for k, v := range extra {
			props[k] = v
		}
		return map[string]interface{}{
			"entityId": id, "type": "PROCESS_GROUP_INSTANCE", "displayName": id,
			"properties": props,
			"fromRelationships": map[string]interface{}{
				"isProcessOf":  []map[string]string{{"id": host, "type": "HOST"}},
				"isInstanceOf": []map[string]string{{"id": pg, "type": "PROCESS_GROUP"}},
			},
			"toRelationships": map[string]interface{}{"runsOnProcessGroupInstance": svcRefs},
		}
	}
	pgi := func(id, ns, host, pg string, services ...string) map[string]interface{} {
		return pgiWith(id, ns, host, pg, nil, services...)
	}
	state := func(actual string, restart bool) map[string]interface{} {
		return map[string]interface{}{"actualMonitoringState": actual, "expectedMonitoringState": "on", "restartRequired": restart}
	}
	byID := map[string]map[string]interface{}{
		"HOST-1":          {"entityId": "HOST-1", "type": "HOST", "properties": map[string]interface{}{"installerVersion": "1.305.2.20250101"}},
		"HOST-2":          {"entityId": "HOST-2", "type": "HOST", "properties": map[string]interface{}{"installerVersion": "1.305.2.20250101"}},
		"PROCESS_GROUP-A": {"entityId": "PROCESS_GROUP-A", "type": "PROCESS_GROUP", "displayName": "a-display", "properties": map[string]interface{}{"detectedName": "SpringBoot com.x.Api"}},
		"PROCESS_GROUP-B": {"entityId": "PROCESS_GROUP-B", "type": "PROCESS_GROUP", "displayName": "worker-b"},
		// Go sem serviço: tecnologia só no process group (runtime tem prioridade sobre framework).
		"PROCESS_GROUP-C": {"entityId": "PROCESS_GROUP-C", "type": "PROCESS_GROUP", "displayName": "go-consumer", "properties": map[string]interface{}{
			"softwareTechnologies": []map[string]string{{"type": "GRPC"}, {"type": "GO", "version": "1.22"}},
		}},
		"PROCESS_GROUP-D": {"entityId": "PROCESS_GROUP-D", "type": "PROCESS_GROUP", "displayName": "node-batch"},
		"PROCESS_GROUP-E": {"entityId": "PROCESS_GROUP-E", "type": "PROCESS_GROUP", "displayName": "dotnet-job"},
		"SERVICE-1":       {"entityId": "SERVICE-1", "type": "SERVICE", "properties": map[string]interface{}{"agentTechnologyType": "JAVA"}},
		"SERVICE-2":       {"entityId": "SERVICE-2", "type": "SERVICE", "properties": map[string]interface{}{}},
	}
	pgis := []map[string]interface{}{
		pgi("PGI-1", "loja", "HOST-1", "PROCESS_GROUP-A", "SERVICE-1"),
		pgi("PGI-2", "loja", "HOST-2", "PROCESS_GROUP-A", "SERVICE-1"),
		pgi("PGI-3", "loja", "HOST-2", "PROCESS_GROUP-B", "SERVICE-2"),
		pgi("PGI-4", "loja", "HOST-1", "PROCESS_GROUP-B"), // sem serviço, sem tipo/estado
		pgiWith("PGI-6", "loja", "HOST-1", "PROCESS_GROUP-C", map[string]interface{}{"monitoringState": state("on", false)}),
		pgiWith("PGI-7", "loja", "HOST-2", "PROCESS_GROUP-C", map[string]interface{}{"monitoringState": state("on", false)}),
		pgiWith("PGI-8", "loja", "HOST-1", "PROCESS_GROUP-D", map[string]interface{}{"processType": "PROCESS_TYPE_NODE_JS", "monitoringState": state("off", false)}),
		pgiWith("PGI-9", "loja", "HOST-1", "PROCESS_GROUP-E", map[string]interface{}{
			"softwareTechnologies": []map[string]string{{"type": "DOTNET"}}, "monitoringState": state("off", true)}),
		pgi("PGI-5", "kube-system", "HOST-1", "PROCESS_GROUP-A", "SERVICE-1"), // excluído
	}
	idRe := regexp.MustCompile(`"([^"]+)"`)

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sel := r.URL.Query().Get("entitySelector")
		var entities []map[string]interface{}
		switch {
		case strings.Contains(sel, `type("HOST_GROUP")`):
			entities = []map[string]interface{}{{"entityId": "HOST_GROUP-X", "type": "HOST_GROUP", "displayName": "akspriv-cov-prd"}}
		case strings.Contains(sel, `type("PROCESS_GROUP_INSTANCE")`):
			if !strings.Contains(r.URL.Query().Get("fields"), "Relationships") {
				t.Errorf("PGIs pedidas sem relações: fields=%q", r.URL.Query().Get("fields"))
			}
			entities = pgis
		case strings.HasPrefix(sel, "entityId("):
			for _, m := range idRe.FindAllStringSubmatch(sel, -1) {
				if e, ok := byID[m[1]]; ok {
					entities = append(entities, e)
				}
			}
		default:
			t.Errorf("selector inesperado: %s", sel)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"entities": entities})
	}))
}

func TestGetDeepMonitoringCoverage(t *testing.T) {
	srv := coverageFakeTenant(t)
	defer srv.Close()
	client, err := NewClient(srv.URL, "test-token")
	if err != nil {
		t.Fatal(err)
	}

	report, err := client.GetDeepMonitoringCoverage(context.Background(), "akspriv-cov-prd", true)
	if err != nil {
		t.Fatalf("GetDeepMonitoringCoverage: %v", err)
	}
	if !report.HostGroupFound {
		t.Fatal("host group deveria ter sido encontrado")
	}
	if report.ProcessesWithoutService != 5 {
		t.Errorf("ProcessesWithoutService = %d, want 5", report.ProcessesWithoutService)
	}

	const v = "1.305.2.20250101"
	want := []CoverageRow{
		// sort technology asc, depois nome; "" primeiro
		{Namespace: "loja", ServiceName: "worker-b", Technology: "", OneAgentVersion: v, DeepMonitoringStatus: CoverageStatusUnresolved, HostCount: 1, PodCount: 1},
		{Namespace: "loja", ServiceName: "worker-b", Technology: "", OneAgentVersion: v, DeepMonitoringStatus: CoverageStatusNoService, HostCount: 1, PodCount: 1},
		{Namespace: "loja", ServiceName: "dotnet-job", Technology: "DOTNET", OneAgentVersion: v, DeepMonitoringStatus: CoverageStatusRestartPending, HostCount: 1, PodCount: 1},
		// Go sem serviço: tipo descoberto e deep monitoring ativo pelo monitoringState do processo.
		{Namespace: "loja", ServiceName: "go-consumer", Technology: "GO", OneAgentVersion: v, DeepMonitoringStatus: CoverageStatusActive, HostCount: 2, PodCount: 2},
		{Namespace: "loja", ServiceName: "SpringBoot com.x.Api", Technology: "JAVA", OneAgentVersion: v, DeepMonitoringStatus: CoverageStatusActive, HostCount: 2, PodCount: 2},
		{Namespace: "loja", ServiceName: "node-batch", Technology: "NODE_JS", OneAgentVersion: v, DeepMonitoringStatus: CoverageStatusOff, HostCount: 1, PodCount: 1},
	}
	if len(report.Rows) != len(want) {
		t.Fatalf("rows = %+v, want %+v", report.Rows, want)
	}
	for i := range want {
		if report.Rows[i] != want[i] {
			t.Errorf("row %d = %+v, want %+v", i, report.Rows[i], want[i])
		}
	}
}

func TestGetDeepMonitoringCoverage_HostGroupAusente(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"entities":[]}`))
	}))
	defer srv.Close()
	client, _ := NewClient(srv.URL, "test-token")

	report, err := client.GetDeepMonitoringCoverage(context.Background(), "akspriv-sem-dt-prd", true)
	if err != nil {
		t.Fatalf("host group ausente não deveria ser erro: %v", err)
	}
	if report.HostGroupFound || len(report.Rows) != 0 {
		t.Errorf("esperava relatório vazio sem host group, got %+v", report)
	}
}

func TestHostAgentVersion_FormatoObjeto(t *testing.T) {
	h := &Entity{Properties: map[string]interface{}{
		"agentVersion": map[string]interface{}{"major": 1.0, "minor": 305.0, "revision": 2.0},
	}}
	if got := hostAgentVersion(h); got != "1.305.2" {
		t.Errorf("hostAgentVersion = %q, want 1.305.2", got)
	}
}

func TestGetPodCoverage(t *testing.T) {
	srv := coverageFakeTenant(t)
	defer srv.Close()
	client, _ := NewClient(srv.URL, "test-token")

	pods, found, err := client.GetPodCoverage(context.Background(), "akspriv-cov-pods-prd")
	if err != nil || !found {
		t.Fatalf("GetPodCoverage: found=%v err=%v", found, err)
	}

	api := pods["loja/pod-pgi-1"]
	if api == nil || api.OneAgentVersion != "1.305.2.20250101" || len(api.Processes) != 1 ||
		api.Processes[0] != (PodCoverageProcess{ProcessName: "SpringBoot com.x.Api", Technology: "JAVA", DeepMonitoringStatus: CoverageStatusActive}) {
		t.Errorf("loja/pod-pgi-1 = %+v", api)
	}
	// Processo sem serviço e sem estado informado pelo Dynatrace.
	if w := pods["loja/pod-pgi-4"]; w == nil || w.Processes[0].DeepMonitoringStatus != CoverageStatusNoService {
		t.Errorf("loja/pod-pgi-4 = %+v, esperava status %q", w, CoverageStatusNoService)
	}
	// Go sem serviço: tipo e deep monitoring vêm do próprio processo.
	if g := pods["loja/pod-pgi-6"]; g == nil || len(g.Processes) != 1 ||
		g.Processes[0] != (PodCoverageProcess{ProcessName: "go-consumer", Technology: "GO", DeepMonitoringStatus: CoverageStatusActive}) {
		t.Errorf("loja/pod-pgi-6 = %+v", g)
	}
	// Namespaces excluídos do relatório continuam com detalhe por pod.
	if pods["kube-system/pod-pgi-5"] == nil {
		t.Error("kube-system/pod-pgi-5 deveria estar no mapa por pod")
	}
}
