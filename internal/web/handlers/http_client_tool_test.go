package handlers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

// Exemplo real que motivou a ferramenta.
const curlExemploFrete = `curl --location 'http://frete-hub-plataforma-frete-prd.ocp-eqx.dc.nova/frete/v2/calculo/detalhe' \
--header 'Content-Type: application/json' \
--data '{
    "Canal": "SITE",
    "Cep": "66910005",
    "UnidadeNegocio": "B2CCasasBahia",
    "Produtos": [
        {
            "IdLojista":  10037,
            "IdSku": 55032021,
            "Quantidade": 2,
            "ValorUnitario": "239.35",
            "Componentes": []
        }
    ]
}'`

func TestParseCurlExemploFrete(t *testing.T) {
	p, err := ParseCurl(curlExemploFrete)
	if err != nil {
		t.Fatal(err)
	}
	if p.Method != "POST" || !p.FollowRedirects {
		t.Errorf("method=%s follow=%v", p.Method, p.FollowRedirects)
	}
	if p.URL != "http://frete-hub-plataforma-frete-prd.ocp-eqx.dc.nova/frete/v2/calculo/detalhe" {
		t.Errorf("url=%s", p.URL)
	}
	if !reflect.DeepEqual(p.Headers, []HTTPClientHeader{{Key: "Content-Type", Value: "application/json"}}) {
		t.Errorf("headers=%v", p.Headers)
	}
	var body map[string]interface{}
	if err := json.Unmarshal([]byte(p.Body), &body); err != nil || body["Cep"] != "66910005" {
		t.Errorf("corpo não é o JSON original: %v %q", err, p.Body)
	}
	if len(p.Warnings) != 0 {
		t.Errorf("warnings inesperados: %v", p.Warnings)
	}
}

func TestParseCurlVariacoes(t *testing.T) {
	cases := []struct {
		name, cmd        string
		method, url      string
		headers          []HTTPClientHeader
		body             string
		follow, insecure bool
		warnings         int
	}{
		{name: "GET simples sem aspas", cmd: `curl https://api.exemplo.com/v1/itens?x=1`, method: "GET", url: "https://api.exemplo.com/v1/itens?x=1", headers: []HTTPClientHeader{}},
		{name: "Postman com --request", cmd: `curl --location --request PUT 'https://h/x' --header 'A: 1'`, method: "PUT", url: "https://h/x", headers: []HTTPClientHeader{{"A", "1"}}, follow: true},
		{name: "flags curtas agrupadas e valor colado", cmd: `curl -sSLk -XDELETE -H'X-Id: 7' https://h/x`, method: "DELETE", url: "https://h/x", headers: []HTTPClientHeader{{"X-Id", "7"}}, follow: true, insecure: true},
		{name: "Chrome bash com $'...'", cmd: `curl 'https://h/x' -H 'accept: */*' --data-raw $'{"a":"linha1\nlinha2","b":"it\'s"}'`, method: "POST", url: "https://h/x", headers: []HTTPClientHeader{{"accept", "*/*"}}, body: "{\"a\":\"linha1\nlinha2\",\"b\":\"it's\"}"},
		{name: "aspas duplas com escape e acento", cmd: `curl -d "{\"cidade\":\"São Paulo\"}" h.com/api`, method: "POST", url: "http://h.com/api", headers: []HTTPClientHeader{}, body: `{"cidade":"São Paulo"}`},
		{name: "basic auth", cmd: `curl -u user:senha https://h/x`, method: "GET", url: "https://h/x", headers: []HTTPClientHeader{{"Authorization", "Basic dXNlcjpzZW5oYQ=="}}},
		{name: "--json", cmd: `curl --json '{"a":1}' https://h/x`, method: "POST", url: "https://h/x", headers: []HTTPClientHeader{{"Content-Type", "application/json"}, {"Accept", "application/json"}}, body: `{"a":1}`},
		{name: "-G move dados para a query", cmd: `curl -G -d a=1 -d b=2 https://h/x`, method: "GET", url: "https://h/x?a=1&b=2", headers: []HTTPClientHeader{}},
		{name: "--header=valor e -I", cmd: `curl -I --header=X:1 https://h/x`, method: "HEAD", url: "https://h/x", headers: []HTTPClientHeader{{"X", "1"}}},
		{name: "flag desconhecida vira aviso", cmd: `curl --foo -F a=b https://h/x`, method: "GET", url: "https://h/x", headers: []HTTPClientHeader{}, warnings: 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, err := ParseCurl(c.cmd)
			if err != nil {
				t.Fatal(err)
			}
			if p.Method != c.method || p.URL != c.url || p.Body != c.body || p.FollowRedirects != c.follow || p.InsecureSkipVerify != c.insecure {
				t.Errorf("got method=%s url=%s body=%q follow=%v insecure=%v", p.Method, p.URL, p.Body, p.FollowRedirects, p.InsecureSkipVerify)
			}
			if !reflect.DeepEqual(p.Headers, c.headers) {
				t.Errorf("headers=%v, want %v", p.Headers, c.headers)
			}
			if len(p.Warnings) != c.warnings {
				t.Errorf("warnings=%v", p.Warnings)
			}
		})
	}
}

func TestParseCurlErros(t *testing.T) {
	for _, cmd := range []string{"curl", `curl -H 'x: y'`, `curl 'https://h/x`, `curl -X`} {
		if _, err := ParseCurl(cmd); err == nil {
			t.Errorf("esperava erro para %q", cmd)
		}
	}
}

// echoServer devolve método, headers e corpo recebidos; /redir redireciona para /.
func echoServer(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redir" {
			http.Redirect(w, r, "/", http.StatusFound)
			return
		}
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Echo", "sim")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{
			"method": r.Method, "ct": r.Header.Get("Content-Type"), "x": r.Header.Get("X-Teste"), "body": string(body),
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestSendFromServer(t *testing.T) {
	srv := echoServer(t)
	req := HTTPClientRequest{Method: "post", URL: srv.URL + "/api", Body: `{"Cep":"66910005"}`,
		Headers: []HTTPClientHeader{{"Content-Type", "application/json"}, {"X-Teste", "1"}, {" ", "ignorado"}}}
	if msg := normalizeHTTPClientRequest(&req); msg != "" {
		t.Fatal(msg)
	}
	resp := sendFromServer(context.Background(), req)
	if resp.Error != "" || resp.Status != 201 || resp.StatusText != "Created" {
		t.Fatalf("resp=%+v", resp)
	}
	var echo map[string]string
	json.Unmarshal([]byte(resp.Body), &echo)
	if echo["method"] != "POST" || echo["ct"] != "application/json" || echo["x"] != "1" || echo["body"] != `{"Cep":"66910005"}` {
		t.Errorf("echo=%v", echo)
	}
	if !hasHeader(resp.Headers, "X-Echo") || resp.SizeBytes != int64(len(resp.Body)) {
		t.Errorf("headers=%v size=%d", resp.Headers, resp.SizeBytes)
	}

	noFollow := HTTPClientRequest{Method: "GET", URL: srv.URL + "/redir"}
	normalizeHTTPClientRequest(&noFollow)
	if r := sendFromServer(context.Background(), noFollow); r.Status != 302 {
		t.Errorf("sem follow esperava 302, veio %d", r.Status)
	}
	noFollow.FollowRedirects = true
	if r := sendFromServer(context.Background(), noFollow); r.Status != 201 {
		t.Errorf("com follow esperava 201, veio %d", r.Status)
	}

	down := HTTPClientRequest{Method: "GET", URL: "http://127.0.0.1:1/"}
	normalizeHTTPClientRequest(&down)
	if r := sendFromServer(context.Background(), down); r.Error == "" || r.Status != 0 {
		t.Errorf("conexão recusada deveria virar Error: %+v", r)
	}
}

func TestNormalizeHTTPClientRequest(t *testing.T) {
	for _, r := range []HTTPClientRequest{
		{URL: "ftp://h/x"},
		{URL: "h/x"},
		{URL: "http://h/x", Method: "TRACE"},
		{URL: "http://h/x", ExecutionMode: "pod"},
		{URL: "http://h/x", Headers: []HTTPClientHeader{{"X", "a\r\nInjetado: 1"}}},
	} {
		if msg := normalizeHTTPClientRequest(&r); msg == "" {
			t.Errorf("esperava erro para %+v", r)
		}
	}
}

// TestHTTPClientCurlScriptNaImagem roda o script do modo pod na imagem real do curl (Docker,
// rede do host) contra o servidor de teste — valida script, marcadores e parse de ponta a ponta.
// Pulado sem Docker.
func TestHTTPClientCurlScriptNaImagem(t *testing.T) {
	if exec.Command("docker", "image", "inspect", httpClientPodImage).Run() != nil {
		t.Skip("imagem " + httpClientPodImage + " indisponível no Docker local")
	}
	srv := echoServer(t)
	run := func(req HTTPClientRequest) HTTPClientResponse {
		t.Helper()
		if msg := normalizeHTTPClientRequest(&req); msg != "" {
			t.Fatal(msg)
		}
		script := buildHTTPClientCurlScript(req, "/tmp/k8s-hpa-http-teste")
		out, err := exec.Command("docker", "run", "--rm", "--network", "host", "--entrypoint", "sh", httpClientPodImage, "-c", script).Output()
		if err != nil {
			t.Fatalf("docker: %v", err)
		}
		resp, err := parseHTTPClientCurlOutput(string(out))
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}

	p, _ := ParseCurl(strings.Replace(curlExemploFrete, "http://frete-hub-plataforma-frete-prd.ocp-eqx.dc.nova", srv.URL, 1))
	resp := run(HTTPClientRequest{Method: p.Method, URL: p.URL, Headers: p.Headers, Body: p.Body, FollowRedirects: p.FollowRedirects})
	var echo map[string]string
	json.Unmarshal([]byte(resp.Body), &echo)
	if resp.Error != "" || resp.Status != 201 || echo["method"] != "POST" || echo["ct"] != "application/json" || echo["body"] != p.Body {
		t.Fatalf("resp=%+v echo=%v", resp, echo)
	}
	if !hasHeader(resp.Headers, "X-Echo") || resp.SizeBytes != int64(len(resp.Body)) || resp.DurationMs < 0 {
		t.Errorf("headers=%v size=%d", resp.Headers, resp.SizeBytes)
	}

	// Sem Content-Type o curl não pode inventar x-www-form-urlencoded (igual ao modo servidor).
	resp = run(HTTPClientRequest{Method: "POST", URL: srv.URL, Body: "abc"})
	json.Unmarshal([]byte(resp.Body), &echo)
	if echo["ct"] != "" {
		t.Errorf("content-type inesperado: %q", echo["ct"])
	}

	if r := run(HTTPClientRequest{Method: "GET", URL: srv.URL + "/redir", FollowRedirects: true}); r.Status != 201 || len(r.Headers) == 0 {
		t.Errorf("com -L esperava o último salto (201), veio %d %v", r.Status, r.Headers)
	}
	if r := run(HTTPClientRequest{Method: "GET", URL: srv.URL + "/redir"}); r.Status != 302 {
		t.Errorf("sem -L esperava 302, veio %d", r.Status)
	}
	if r := run(HTTPClientRequest{Method: "GET", URL: "http://host-que-nao-existe.invalid/"}); r.Error == "" || r.Status != 0 {
		t.Errorf("DNS inexistente deveria virar Error: %+v", r)
	}
}
