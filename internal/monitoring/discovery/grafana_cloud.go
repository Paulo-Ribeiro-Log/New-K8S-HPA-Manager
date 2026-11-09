package discovery

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// Clusters que deixaram o Prometheus próprio e enviam as métricas ao Grafana Cloud (Alloy/
// k8s-monitoring) não têm mais um endpoint por cluster: há um tenant único para todos, com
// autenticação básica e a série de cada cluster distinguida pelo rótulo `cluster`.
//
// Em vez de mexer nas dezenas de chamadores que concatenam `URL + "api/v1/..."`, o cluster ganha um
// proxy local em 127.0.0.1 (mesmo desenho do túnel de port-forward, ver portforward.go) que:
//   - injeta a autenticação básica (o token nunca sai deste processo);
//   - acrescenta `cluster="<nome>"` a todo seletor PromQL de query/query_range/series/labels,
//     para que um cluster nunca enxergue (nem some) as séries de outro;
//   - traduz `/api/v1/status/config` (inexistente no Mimir) numa consulta `up`.
//
// Configuração em ~/.k8s-hpa-manager/grafana-cloud.json (0600, o token fica fora dos
// clusters-config.json que são compartilhados/versionáveis):
//
//	{
//	  "url": "https://prometheus-prod-40-prod-sa-east-1.grafana.net/api/prom",
//	  "username": "<id numérico da instância Prometheus>",
//	  "token": "glc_...",                      // política de acesso com metrics:read
//	  "clusters": ["akspriv-oferta-hlg"],
//	  "clusterLabels": {"akspriv-oferta-hlg": "akspriv-oferta-hlg"}   // opcional
//	}
//
// "tokenEnv" pode nomear uma variável de ambiente em vez de "token".

const (
	grafanaCloudFile  = "grafana-cloud.json"
	grafanaClusterLbl = "cluster"
	grafanaMaxBody    = 1 << 20
)

// GrafanaCloudConfig é o conteúdo de grafana-cloud.json.
type GrafanaCloudConfig struct {
	URL           string            `json:"url"`
	Username      string            `json:"username"`
	Token         string            `json:"token,omitempty"`
	TokenEnv      string            `json:"tokenEnv,omitempty"`
	Clusters      []string          `json:"clusters"`
	ClusterLabels map[string]string `json:"clusterLabels,omitempty"`
}

func (c GrafanaCloudConfig) token() string {
	if c.Token != "" {
		return c.Token
	}
	if c.TokenEnv != "" {
		return os.Getenv(c.TokenEnv)
	}
	return ""
}

// labelFor devolve o valor do rótulo `cluster` e se o cluster está na lista.
func (c GrafanaCloudConfig) labelFor(cluster string) (string, bool) {
	short := shortClusterName(cluster)
	for _, name := range c.Clusters {
		if shortClusterName(name) != short {
			continue
		}
		if v := c.ClusterLabels[name]; v != "" {
			return v, true
		}
		if v := c.ClusterLabels[short]; v != "" {
			return v, true
		}
		return short, true
	}
	return "", false
}

func shortClusterName(cluster string) string {
	switch {
	case strings.HasPrefix(cluster, "gke_"):
		return gkeShortName(cluster)
	case strings.HasPrefix(cluster, "arn:aws:eks:"):
		return eksShortName(cluster)
	}
	return strings.TrimSuffix(cluster, "-admin")
}

func loadGrafanaCloudConfig() (GrafanaCloudConfig, bool) {
	data, err := os.ReadFile(configFilePath(grafanaCloudFile))
	if err != nil {
		return GrafanaCloudConfig{}, false
	}
	var cfg GrafanaCloudConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		log.Warn().Err(err).Msg("grafana-cloud.json inválido; ignorado")
		return GrafanaCloudConfig{}, false
	}
	return cfg, true
}

// IsGrafanaCloudCluster indica se o cluster lê métricas do Grafana Cloud (sem rede).
func IsGrafanaCloudCluster(cluster string) bool {
	if getPrometheusURLOverride(cluster) != "" || !getPortForwardOverride(cluster).isZero() {
		return false // override manual explícito vence
	}
	cfg, ok := loadGrafanaCloudConfig()
	if !ok {
		return false
	}
	_, in := cfg.labelFor(cluster)
	return in
}

// resolveGrafanaCloud devolve a URL do proxy local do cluster. handled=false quando o cluster não
// usa Grafana Cloud (o chamador segue o caminho de sempre).
func resolveGrafanaCloud(cluster string) (localURL string, handled bool, err error) {
	if !IsGrafanaCloudCluster(cluster) {
		return "", false, nil
	}
	cfg, _ := loadGrafanaCloudConfig()
	label, _ := cfg.labelFor(cluster)
	switch {
	case cfg.URL == "" || cfg.Username == "":
		return "", true, fmt.Errorf("grafana-cloud.json sem \"url\" ou \"username\"")
	case cfg.token() == "":
		return "", true, fmt.Errorf("grafana-cloud.json sem token de leitura (\"token\" ou \"tokenEnv\")")
	}
	u, err := gcProxies.get(cluster, cfg, label)
	return u, true, err
}

// --- proxy local ---

type gcProxy struct {
	key      string // muda quando a configuração muda -> recria
	base     string // http://127.0.0.1:porta/<segredo>/
	listener net.Listener
}

type gcProxyRegistry struct {
	mu sync.Mutex
	m  map[string]*gcProxy
}

var gcProxies = &gcProxyRegistry{m: map[string]*gcProxy{}}

func (r *gcProxyRegistry) get(cluster string, cfg GrafanaCloudConfig, label string) (string, error) {
	key := strings.Join([]string{cfg.URL, cfg.Username, cfg.token(), label}, "\x00")
	r.mu.Lock()
	defer r.mu.Unlock()
	if p := r.m[cluster]; p != nil {
		if p.key == key {
			return p.base, nil
		}
		_ = p.listener.Close() // token ou rótulo mudou
		delete(r.m, cluster)
	}
	p, err := startGrafanaProxy(cfg, label)
	if err != nil {
		return "", err
	}
	p.key = key
	r.m[cluster] = p
	return p.base, nil
}

func startGrafanaProxy(cfg GrafanaCloudConfig, label string) (*gcProxy, error) {
	upstream, err := url.Parse(strings.TrimRight(cfg.URL, "/"))
	if err != nil || upstream.Scheme == "" || upstream.Host == "" {
		return nil, fmt.Errorf("url do Grafana Cloud inválida: %q", cfg.URL)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("abrir proxy local do Grafana Cloud: %w", err)
	}
	// Segredo no caminho: outro processo local não usa o token só por achar a porta.
	raw := make([]byte, 12)
	_, _ = rand.Read(raw)
	prefix := "/" + hex.EncodeToString(raw)

	h := newGrafanaHandler(upstream, cfg.Username, cfg.token(), label, prefix, nil)
	go func() {
		srv := &http.Server{Handler: h, ReadHeaderTimeout: 10 * time.Second}
		_ = srv.Serve(ln)
	}()
	return &gcProxy{base: "http://" + ln.Addr().String() + prefix + "/", listener: ln}, nil
}

func newGrafanaHandler(upstream *url.URL, user, token, label, prefix string, rt http.RoundTripper) http.Handler {
	if rt == nil {
		rt = &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 90 * time.Second,
			MaxIdleConnsPerHost:   4,
		}
	}
	rp := &httputil.ReverseProxy{
		Transport: rt,
		Rewrite: func(pr *httputil.ProxyRequest) {
			in := pr.In
			path := strings.TrimPrefix(in.URL.Path, prefix)
			q := in.URL.Query()
			var form url.Values
			isForm := in.Method == http.MethodPost &&
				strings.HasPrefix(in.Header.Get("Content-Type"), "application/x-www-form-urlencoded")
			if isForm {
				body, _ := io.ReadAll(io.LimitReader(in.Body, grafanaMaxBody))
				form, _ = url.ParseQuery(string(body))
			}

			if path == "/api/v1/status/config" { // Mimir não tem; `up` prova rede + auth + tenant
				path = "/api/v1/query"
				q = url.Values{"query": {"up"}}
			}
			scopeParams(q, label)
			if form != nil {
				scopeParams(form, label)
			}
			// labels/series sem match[] listariam o tenant inteiro: restringe ao cluster.
			if isMetadataPath(path) && len(q["match[]"]) == 0 && len(form["match[]"]) == 0 {
				if form != nil {
					form.Set("match[]", "{"+grafanaClusterLbl+`="`+label+`"}`)
				} else {
					q.Set("match[]", "{"+grafanaClusterLbl+`="`+label+`"}`)
				}
			}

			pr.Out.URL.Scheme = upstream.Scheme
			pr.Out.URL.Host = upstream.Host
			pr.Out.Host = upstream.Host
			pr.Out.URL.Path = upstream.Path + path
			pr.Out.URL.RawPath = ""
			pr.Out.URL.RawQuery = q.Encode()
			if form != nil {
				enc := form.Encode()
				pr.Out.Body = io.NopCloser(strings.NewReader(enc))
				pr.Out.ContentLength = int64(len(enc))
				pr.Out.Header.Set("Content-Length", fmt.Sprint(len(enc)))
			}
			pr.Out.Header.Del("Authorization")
			pr.Out.SetBasicAuth(user, token)
		},
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, prefix+"/") {
			http.NotFound(w, r)
			return
		}
		rp.ServeHTTP(w, r)
	})
}

func isMetadataPath(path string) bool {
	return path == "/api/v1/labels" || path == "/api/v1/series" ||
		(strings.HasPrefix(path, "/api/v1/label/") && strings.HasSuffix(path, "/values"))
}

// scopeParams reescreve os parâmetros PromQL de uma requisição para o cluster.
func scopeParams(v url.Values, label string) {
	for _, k := range []string{"query", "match[]"} {
		if vals, ok := v[k]; ok {
			out := make([]string, len(vals))
			for i, s := range vals {
				out[i] = injectLabelMatcher(s, grafanaClusterLbl, label)
			}
			v[k] = out
		}
	}
}
