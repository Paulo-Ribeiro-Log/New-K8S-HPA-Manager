package dynatrace

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"
)

// fakeDT imita os trechos da API usados pelo relatório. O process group do frete-hub liga 5
// serviços (1 WEB_REQUEST_SERVICE, 4 WEB_SERVICE) mais um DATABASE_SERVICE e um "background threads".
func fakeDT(t *testing.T) *Client {
	t.Helper()
	svc := func(id, name, typ string) map[string]any {
		return map[string]any{"entityId": id, "displayName": name, "properties": map[string]any{"serviceType": typ}}
	}
	byID := map[string]map[string]any{
		"S-WRS":    svc("S-WRS", "frete-hub-*", "WEB_REQUEST_SERVICE"),
		"S-CTRL":   svc("S-CTRL", "FreteController", "WEB_SERVICE"),
		"S-DB":     svc("S-DB", "frete-hub", "DATABASE_SERVICE"),
		"S-BG":     svc("S-BG", "Requests executed in background threads of SpringBoot frete-hub-*", "WEB_SERVICE"),
		"S-B2C":    svc("S-B2C", "Frete.WebApi.dll frete-b*c-*", "WEB_REQUEST_SERVICE"),
		"S-B2C-PG": svc("S-B2C-PG", "FreteB2CController", "WEB_SERVICE"),
		// o process group do frete-b2c liga mais um serviço (além dos que os processos ligam)
		"PG-B2C": {"entityId": "PG-B2C", "displayName": "Frete.WebApi.dll frete-b*c-*",
			"toRelationships": map[string]any{"runsOn": []map[string]any{{"id": "S-B2C-PG"}}}},
	}
	pgi := func(id, ns, basePod, pod string) map[string]any {
		return map[string]any{"entityId": id, "displayName": id,
			"properties": map[string]any{"metadata": []map[string]any{
				{"key": "KUBERNETES_NAMESPACE", "value": ns},
				{"key": "KUBERNETES_BASE_POD_NAME", "value": basePod},
				{"key": "KUBERNETES_FULL_POD_NAME", "value": pod},
			}},
			"fromRelationships": map[string]any{"isInstanceOf": []map[string]any{{"id": "PG-B2C"}}},
			"toRelationships":   map[string]any{"runsOnProcessGroupInstance": []map[string]any{{"id": "S-B2C"}}},
		}
	}
	withTags := func(e map[string]any, tags ...map[string]any) map[string]any {
		e["tags"] = tags
		return e
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sel := r.URL.Query().Get("entitySelector")
		enc := json.NewEncoder(w)
		switch {
		case r.URL.Path == "/api/v2/entities" && strings.HasPrefix(sel, `type("PROCESS_GROUP")`):
			// o Dynatrace filtra por entityName.contains("<termo>")
			term := strings.ToLower(sel[strings.Index(sel, `contains("`)+len(`contains("`):])
			term = term[:strings.Index(term, `"`)]
			var out []map[string]any
			for _, pg := range []map[string]any{
				{"entityId": "PG-1", "displayName": "SpringBoot - frete-hub - calculo-de-fretes-prd",
					"toRelationships": map[string]any{"runsOn": []map[string]any{{"id": "S-WRS"}, {"id": "S-CTRL"}, {"id": "S-DB"}, {"id": "S-BG"}}}},
				{"entityId": "PG-2", "displayName": "SpringBoot - frete-hub-regionalizado - calculo-de-fretes-prd",
					"toRelationships": map[string]any{"runsOn": []map[string]any{{"id": "S-OUTRO"}}}},
				{"entityId": "PG-3", "displayName": "SpringBoot - frete-hub - calculo-de-fretes-hml", // outro namespace
					"toRelationships": map[string]any{"runsOn": []map[string]any{{"id": "S-HML"}}}},
				{"entityId": "PG-4", "displayName": "SpringBoot - frete-b2c-api-legacy - calculo-de-fretes-prd",
					"toRelationships": map[string]any{"runsOn": []map[string]any{{"id": "S-OUTRO"}}}},
				{"entityId": "PG-5", "displayName": "Java - cotacao-legada - calculo-de-fretes-prd"},
				{"entityId": "PG-B2C", "displayName": "Frete.WebApi.dll frete-b*c-*"}, // sem " - <deployment> - <namespace>"
			} {
				if strings.Contains(strings.ToLower(pg["displayName"].(string)), term) {
					out = append(out, pg)
				}
			}
			enc.Encode(map[string]any{"entities": out}) //nolint:errcheck
		case r.URL.Path == "/api/v2/entities" && strings.HasPrefix(sel, `type("HOST_GROUP")`):
			enc.Encode(map[string]any{"entities": []map[string]any{{"entityId": "HG-1", "displayName": "akspriv-calculofrete-prd"}}}) //nolint:errcheck
		case r.URL.Path == "/api/v2/entities" && strings.HasPrefix(sel, `type("PROCESS_GROUP_INSTANCE")`):
			enc.Encode(map[string]any{"entities": []map[string]any{ //nolint:errcheck
				pgi("PGI-1", "calculo-de-fretes-prd", "frete-b*c-*", "frete-b2c-7cc9fc5b46-b4kqn"),
				pgi("PGI-2", "calculo-de-fretes-prd", "frete-b*c-*", "frete-b2c-7cc9fc5b46-x9x9x"),
				pgi("PGI-3", "calculo-de-fretes-hml", "frete-b*c-*", "frete-b2c-aaaa-bbbb"), // outro namespace: não entra
				pgi("PGI-4", "calculo-de-fretes-prd", "frete-bff-*", "frete-bff-1-2"),       // outro Deployment: não casa
				withTags(pgi("PGI-5", "calculo-de-fretes-prd", "legado-xyz-*", "legado-xyz-1-2"),
					map[string]any{"context": "KUBERNETES", "key": "app", "value": "frete-tagado"}),
			}})
		case r.URL.Path == "/api/v2/entities" && strings.HasPrefix(sel, "entityId("):
			var out []map[string]any
			for _, m := range regexp.MustCompile(`"([^"]+)"`).FindAllStringSubmatch(sel, -1) {
				if e, ok := byID[m[1]]; ok {
					out = append(out, e)
				}
			}
			enc.Encode(map[string]any{"entities": out}) //nolint:errcheck
		case r.URL.Path == "/api/v2/metrics/query":
			enc.Encode(map[string]any{"result": []map[string]any{{"data": []map[string]any{ //nolint:errcheck
				{"dimensionMap": map[string]string{"dt.entity.service": "S-CTRL"}, "timestamps": []int64{1, 2, 3}, "values": []any{100.0, nil, 300.0}},
				{"dimensionMap": map[string]string{"dt.entity.service": "S-WRS"}, "timestamps": []int64{1, 2, 3}, "values": []any{0.0, 0.0, 0.0}},
			}}}})
		default:
			t.Errorf("rota inesperada: %s %s", r.URL.Path, r.URL.RawQuery)
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(srv.Close)
	c, err := NewClient(srv.URL, "tok")
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestResolveEntryServices(t *testing.T) {
	c := fakeDT(t)
	svcs, notes, err := c.ResolveEntryServices(context.Background(), "akspriv-calculofrete-prd-admin", "calculo-de-fretes-prd", "frete-hub")
	if err != nil {
		t.Fatal(err)
	}
	// só os pontos de entrada do PG EXATO (não o regionalizado, nem o namespace hml), sem banco nem threads
	if len(svcs) != 2 || svcs[0].Name != "FreteController" || svcs[1].Name != "frete-hub-*" {
		t.Fatalf("serviços: %+v", svcs)
	}
	if len(notes) != 2 || !strings.Contains(notes[0], "nome do process group") ||
		!strings.Contains(notes[1], "DATABASE_SERVICE") || !strings.Contains(notes[1], "background threads") {
		t.Errorf("notas: %v", notes)
	}
}

func TestResolveEntryServicesSemProcessGroup(t *testing.T) {
	c := fakeDT(t)
	if _, _, err := c.ResolveEntryServices(context.Background(), "akspriv-calculofrete-prd-admin", "outro-ns", "frete-hub"); !errors.Is(err, ErrNoProcessGroup) {
		t.Errorf("esperava ErrNoProcessGroup, veio %v", err)
	}
	for _, bad := range [][2]string{{`ns"x`, "app"}, {"ns", `a"),type("HOST`}, {"", "app"}} {
		if _, _, err := c.ResolveEntryServices(context.Background(), "c", bad[0], bad[1]); err == nil {
			t.Errorf("nome inválido %v deveria dar erro", bad)
		}
	}
}

func TestQueryServiceSeriesPreservaNulo(t *testing.T) {
	c := fakeDT(t)
	got, err := c.QueryServiceSeries(context.Background(), `builtin:service.response.time:splitBy("dt.entity.service"):percentile(99)`,
		[]string{"S-CTRL", "S-WRS"}, time.Unix(0, 0), time.Unix(10, 0), "5m", 0.001)
	if err != nil {
		t.Fatal(err)
	}
	ctrl := got["S-CTRL"]
	if len(ctrl.Values) != 3 || ctrl.Values[0] != 0.1 || !math.IsNaN(ctrl.Values[1]) || math.Abs(ctrl.Values[2]-0.3) > 1e-12 {
		t.Errorf("série com nulo e escala: %+v", ctrl)
	}
	if z := got["S-WRS"]; z.Values[0] != 0 || math.IsNaN(z.Values[0]) { // zero real continua zero
		t.Errorf("zero real: %+v", z)
	}
	if out, err := c.QueryServiceSeries(context.Background(), "x", nil, time.Now(), time.Now(), "1m", 1); err != nil || len(out) != 0 {
		t.Errorf("sem serviços: %v %v", out, err)
	}
}

func TestResolveEntryServicesPorMetadadosKubernetes(t *testing.T) {
	c := fakeDT(t)
	// o PG do frete-b2c se chama "Frete.WebApi.dll frete-b*c-*" (sem deployment/namespace no nome) e o
	// Deployment aparece como "frete-b*c" nos metadados: o "*" do Dynatrace é curinga de dígitos
	svcs, notes, err := c.ResolveEntryServices(context.Background(), "akspriv-calculofrete-prd-admin", "calculo-de-fretes-prd", "frete-b2c")
	if err != nil {
		t.Fatal(err)
	}
	// serviços dos processos (S-B2C) e do process group (S-B2C-PG); só do namespace pedido
	if len(svcs) != 2 || svcs[0].Name != "Frete.WebApi.dll frete-b*c-*" || svcs[1].Name != "FreteB2CController" {
		t.Fatalf("serviços: %+v", svcs)
	}
	if len(notes) == 0 || !strings.Contains(notes[0], "metadados Kubernetes") {
		t.Errorf("a nota deve dizer como foi resolvido: %v", notes)
	}
	// o mesmo Deployment no namespace de homologação não vaza para o de produção, e o namespace sem o
	// Deployment dá erro com a lista do que existe
	if _, _, err := c.ResolveEntryServices(context.Background(), "akspriv-calculofrete-prd-admin", "calculo-de-fretes-xyz", "frete-b2c"); !errors.Is(err, ErrNoProcessGroup) {
		t.Errorf("outro namespace: %v", err)
	}
}

func TestPorNomeDoPGTemPrioridadeESemMetadadosNaoChama(t *testing.T) {
	c := fakeDT(t)
	_, notes, err := c.ResolveEntryServices(context.Background(), "akspriv-calculofrete-prd-admin", "calculo-de-fretes-prd", "frete-hub")
	if err != nil || !strings.Contains(notes[0], "nome do process group") {
		t.Errorf("frete-hub deve resolver pelo nome do PG: %v %v", notes, err)
	}
}

func TestWorkloadMatches(t *testing.T) {
	casos := []struct {
		dt, pedido string
		want       bool
	}{
		{"frete-hub", "frete-hub", true},
		{"Frete-Hub", "frete-hub", true},
		{"frete-hub", "frete-hub-regionalizado", false},
		{"frete-b*c", "frete-b2c", true}, // "*" = dígitos
		{"frete-b*c", "frete-b12c", true},
		{"frete-b*c", "frete-bfc", false}, // letra não é dígito
		{"frete-b*c", "frete-b2c-admin", false},
		{"frete-*", "frete-1", true},
	}
	for _, c := range casos {
		if got := workloadMatches(c.dt, c.pedido); got != c.want {
			t.Errorf("workloadMatches(%q, %q) = %v, esperava %v", c.dt, c.pedido, got, c.want)
		}
	}
}

func TestNoProcessGroupDaPistaDoQueExiste(t *testing.T) {
	c := fakeDT(t)
	cluster := "akspriv-calculofrete-prd-admin"
	// o nome só "contém" o Deployment (não casa o padrão): o erro lista o PG parecido
	_, _, err := c.ResolveEntryServices(context.Background(), cluster, "calculo-de-fretes-prd", "frete-b2c-api")
	if !errors.Is(err, ErrNoProcessGroup) || !strings.Contains(err.Error(), "SpringBoot - frete-b2c-api-legacy - calculo-de-fretes-prd") {
		t.Errorf("esperava a pista com o PG parecido: %v", err)
	}
	// nada com o nome: lista os Deployments com processo monitorado no namespace (metadados Kubernetes)
	_, _, err = c.ResolveEntryServices(context.Background(), cluster, "calculo-de-fretes-prd", "naoexiste")
	if !errors.Is(err, ErrNoProcessGroup) || !strings.Contains(err.Error(), "frete-b*c") || !strings.Contains(err.Error(), "frete-bff") {
		t.Errorf("esperava os Deployments do namespace: %v", err)
	}
}

func TestResolveEntryServicesPorTagComoFallback(t *testing.T) {
	c := fakeDT(t)
	cluster := "akspriv-calculofrete-prd-admin"
	// o Deployment "frete-tagado" não aparece nem no nome do PG nem nos metadados (o processo diz
	// "legado-xyz"), mas o processo tem a tag app:frete-tagado no namespace
	svcs, notes, err := c.ResolveEntryServices(context.Background(), cluster, "calculo-de-fretes-prd", "frete-tagado")
	if err != nil || len(svcs) == 0 || len(notes) == 0 || !strings.Contains(notes[0], "tag do Dynatrace") {
		t.Fatalf("fallback por tag: %+v %v %v", svcs, notes, err)
	}
	// a tag só vale no namespace do processo
	if _, _, err := c.ResolveEntryServices(context.Background(), cluster, "calculo-de-fretes-hml", "frete-tagado"); !errors.Is(err, ErrNoProcessGroup) {
		t.Errorf("tag em outro namespace não pode casar: %v", err)
	}
	// os metadados têm prioridade sobre a tag: "frete-b2c" resolve por metadados
	if _, notes, err := c.ResolveEntryServices(context.Background(), cluster, "calculo-de-fretes-prd", "frete-b2c"); err != nil || !strings.Contains(notes[0], "metadados Kubernetes") {
		t.Errorf("metadados primeiro: %v %v", notes, err)
	}
}
