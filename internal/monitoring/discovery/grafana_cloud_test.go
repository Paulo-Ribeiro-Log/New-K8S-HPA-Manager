package discovery

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestInjectLabelMatcher(t *testing.T) {
	c := `cluster="c1"`
	cases := []struct{ in, want string }{
		{`up`, `up{` + c + `}`},
		{`up{job="x"}`, `up{` + c + `,job="x"}`},
		{`up{}`, `up{` + c + `}`},
		{`{__name__=~"node_.*"}`, `{` + c + `,__name__=~"node_.*"}`},
		{`rate(container_cpu_usage_seconds_total{container!=""}[5m])`,
			`rate(container_cpu_usage_seconds_total{` + c + `,container!=""}[5m])`},
		{`sum by (namespace, pod) (rate(container_cpu_usage_seconds_total[5m] offset 1h))`,
			`sum by (namespace, pod) (rate(container_cpu_usage_seconds_total{` + c + `}[5m] offset 1h))`},
		{`sum(kube_pod_info) by (node)`, `sum(kube_pod_info{` + c + `}) by (node)`},
		{`kube_pod_info * on(namespace,pod) group_left(node) kube_pod_container_resource_requests{resource="cpu"}`,
			`kube_pod_info{` + c + `} * on(namespace,pod) group_left(node) kube_pod_container_resource_requests{` + c + `,resource="cpu"}`},
		{`histogram_quantile(0.95, sum by (le) (rate(istio_request_duration_milliseconds_bucket{reporter="destination"}[10m])))`,
			`histogram_quantile(0.95, sum by (le) (rate(istio_request_duration_milliseconds_bucket{` + c + `,reporter="destination"}[10m])))`},
		{`label_replace(up, "dst", "$1", "src", "(.*)")`, `label_replace(up{` + c + `}, "dst", "$1", "src", "(.*)")`},
		{`topk(5, node_load1) and on() vector(1)`, `topk(5, node_load1{` + c + `}) and on() vector(1)`},
		{`max_over_time(kubelet_volume_stats_used_bytes[7d:1h])`, `max_over_time(kubelet_volume_stats_used_bytes{` + c + `}[7d:1h])`},
		{`deriv(kubelet_volume_stats_used_bytes{namespace="a"}[7d]) * 86400`,
			`deriv(kubelet_volume_stats_used_bytes{` + c + `,namespace="a"}[7d]) * 86400`},
		{`a / 1e-3 + b`, `a{` + c + `} / 1e-3 + b{` + c + `}`},
		{`vector(1)`, `vector(1)`},
		{`node_nf_conntrack_entries{instance=~"10\\.0\\.0\\.1:9100"}`,
			`node_nf_conntrack_entries{` + c + `,instance=~"10\\.0\\.0\\.1:9100"}`},
		{`up{job="a}b"}`, `up{` + c + `,job="a}b"}`},
		{"up # comentário com up\n+ down", "up{" + c + "} # comentário com up\n+ down{" + c + "}"},
		{`sum without (instance) (up) > bool 0`, `sum without (instance) (up{` + c + `}) > bool 0`},
	}
	for _, tc := range cases {
		if got := injectLabelMatcher(tc.in, "cluster", "c1"); got != tc.want {
			t.Errorf("entrada: %s\n  quero: %s\n  veio:  %s", tc.in, tc.want, got)
		}
	}
}

func TestInjectLabelMatcherEscapaValor(t *testing.T) {
	got := injectLabelMatcher(`up`, "cluster", `a"b\c`)
	if got != `up{cluster="a\"b\\c"}` {
		t.Errorf("valor não escapado: %s", got)
	}
}

type captured struct {
	method, path, rawQuery, auth, body, ctype string
}

func newFakeUpstream(t *testing.T, got *captured) *httptest.Server {
	t.Helper()
	return httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		*got = captured{r.Method, r.URL.Path, r.URL.RawQuery, r.Header.Get("Authorization"), string(b), r.Header.Get("Content-Type")}
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[]}}`))
	}))
}

func newTestProxy(t *testing.T, up *httptest.Server) (*httptest.Server, string) {
	t.Helper()
	u, _ := url.Parse(up.URL + "/api/prom")
	const prefix = "/segredo"
	h := newGrafanaHandler(u, "123", "tok", "c1", prefix, up.Client().Transport)
	p := httptest.NewServer(h)
	t.Cleanup(p.Close)
	return p, prefix
}

func TestGrafanaProxyGETInjetaClusterEAuth(t *testing.T) {
	var got captured
	up := newFakeUpstream(t, &got)
	defer up.Close()
	p, prefix := newTestProxy(t, up)

	resp, err := http.Get(p.URL + prefix + "/api/v1/query?query=" + url.QueryEscape(`sum(up)`))
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("resp=%v err=%v", resp, err)
	}
	if got.path != "/api/prom/api/v1/query" {
		t.Errorf("path = %s", got.path)
	}
	q, _ := url.ParseQuery(got.rawQuery)
	if q.Get("query") != `sum(up{cluster="c1"})` {
		t.Errorf("query = %s", q.Get("query"))
	}
	if !strings.HasPrefix(got.auth, "Basic ") {
		t.Errorf("sem basic auth: %q", got.auth)
	}
}

func TestGrafanaProxyPOSTFormInjetaCluster(t *testing.T) {
	var got captured
	up := newFakeUpstream(t, &got)
	defer up.Close()
	p, prefix := newTestProxy(t, up)

	form := url.Values{"query": {`rate(x[5m])`}, "start": {"1"}}
	resp, err := http.Post(p.URL+prefix+"/api/v1/query_range", "application/x-www-form-urlencoded", strings.NewReader(form.Encode()))
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("resp=%v err=%v", resp, err)
	}
	b, _ := url.ParseQuery(got.body)
	if b.Get("query") != `rate(x{cluster="c1"}[5m])` || b.Get("start") != "1" {
		t.Errorf("corpo = %s", got.body)
	}
	if got.method != http.MethodPost {
		t.Errorf("método = %s", got.method)
	}
}

func TestGrafanaProxyStatusConfigViraUp(t *testing.T) {
	var got captured
	up := newFakeUpstream(t, &got)
	defer up.Close()
	p, prefix := newTestProxy(t, up)

	resp, err := http.Get(p.URL + prefix + "/api/v1/status/config")
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("resp=%v err=%v", resp, err)
	}
	q, _ := url.ParseQuery(got.rawQuery)
	if got.path != "/api/prom/api/v1/query" || q.Get("query") != `up{cluster="c1"}` {
		t.Errorf("path=%s query=%s", got.path, q.Get("query"))
	}
}

func TestGrafanaProxyLabelsSemMatchFicamNoCluster(t *testing.T) {
	var got captured
	up := newFakeUpstream(t, &got)
	defer up.Close()
	p, prefix := newTestProxy(t, up)

	for _, path := range []string{"/api/v1/labels", "/api/v1/label/namespace/values", "/api/v1/series"} {
		if resp, err := http.Get(p.URL + prefix + path); err != nil || resp.StatusCode != 200 {
			t.Fatalf("%s: resp=%v err=%v", path, resp, err)
		}
		q, _ := url.ParseQuery(got.rawQuery)
		if q.Get("match[]") != `{cluster="c1"}` {
			t.Errorf("%s: match[] = %q", path, q.Get("match[]"))
		}
	}
}

func TestGrafanaProxyExigeSegredoNoCaminho(t *testing.T) {
	var got captured
	up := newFakeUpstream(t, &got)
	defer up.Close()
	p, _ := newTestProxy(t, up)

	resp, err := http.Get(p.URL + "/api/v1/query?query=up")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusNotFound || got.path != "" {
		t.Errorf("sem segredo deveria dar 404 sem tocar o upstream: status=%d path=%q", resp.StatusCode, got.path)
	}
}

func TestGrafanaCloudConfigLabelFor(t *testing.T) {
	cfg := GrafanaCloudConfig{
		Clusters:      []string{"akspriv-oferta-hlg", "akspriv-outro-hlg"},
		ClusterLabels: map[string]string{"akspriv-outro-hlg": "outro"},
	}
	cases := []struct {
		cluster string
		want    string
		ok      bool
	}{
		{"akspriv-oferta-hlg-admin", "akspriv-oferta-hlg", true},
		{"akspriv-oferta-hlg", "akspriv-oferta-hlg", true},
		{"akspriv-outro-hlg-admin", "outro", true},
		{"akspriv-oferta-prd-admin", "", false},
	}
	for _, tc := range cases {
		got, ok := cfg.labelFor(tc.cluster)
		if got != tc.want || ok != tc.ok {
			t.Errorf("%s: got (%q,%v), quero (%q,%v)", tc.cluster, got, ok, tc.want, tc.ok)
		}
	}
}
