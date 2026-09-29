package handlers

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"

	"k8s-hpa-manager/internal/config"
	"k8s-hpa-manager/internal/history"
)

// http_client_tool.go — ferramenta "Cliente HTTP" (menu Tools): monta e envia uma requisição HTTP
// (ou cola um cURL) e mostra a resposta, no estilo do Postman. Dois modos de execução:
//   - "server": sai do próprio servidor da aplicação (net/http) — alcança o que a rede/VPN do
//     servidor alcança.
//   - "pod": sai de um pod do Deployment escolhido, via curl num Ephemeral Container anexado a ele
//     — alcança serviços internos (*.svc.cluster.local) e reflete NetworkPolicy/Istio do workload.

const (
	// httpClientPodImage: curl oficial, leve (~9 MB), com sh/sleep/cat/head e usuário não-root.
	// Tag FIXA de propósito (nunca `latest`).
	httpClientPodImage             = "curlimages/curl:8.10.1"
	httpClientEphemeralPrefix      = "http-client-"
	httpClientEphemeralReadyTimout = 30 * time.Second
	httpClientEphemeralPoll        = 500 * time.Millisecond

	httpClientDefaultTimeoutMs = 30000
	httpClientMaxTimeoutMs     = 120000
	// httpClientMaxBodyBytes: teto do corpo da resposta devolvido à tela (o resto é cortado e
	// sinalizado em Truncated) — protege o navegador de respostas enormes.
	httpClientMaxBodyBytes = 5 * 1024 * 1024
)

var httpClientMethods = map[string]bool{
	"GET": true, "POST": true, "PUT": true, "PATCH": true, "DELETE": true, "HEAD": true, "OPTIONS": true,
}

// HTTPClientRequest é o body do POST /http-client/send.
type HTTPClientRequest struct {
	ExecutionMode      string             `json:"execution_mode"` // server (default) | pod
	Cluster            string             `json:"cluster"`
	Namespace          string             `json:"namespace"`
	Deployment         string             `json:"deployment"`
	PodName            string             `json:"pod_name,omitempty"`
	ContainerName      string             `json:"container_name,omitempty"`
	Method             string             `json:"method"`
	URL                string             `json:"url"`
	Headers            []HTTPClientHeader `json:"headers"`
	Body               string             `json:"body"`
	FollowRedirects    bool               `json:"follow_redirects"`
	InsecureSkipVerify bool               `json:"insecure_skip_verify"`
	TimeoutMs          int                `json:"timeout_ms"`
}

// HTTPClientResponse é o resultado mostrado na tela. Error preenchido = não houve resposta HTTP
// (DNS, conexão recusada, timeout, TLS…) — status HTTP de erro (4xx/5xx) NÃO é Error.
type HTTPClientResponse struct {
	Status       int                `json:"status,omitempty"`
	StatusText   string             `json:"status_text,omitempty"`
	Headers      []HTTPClientHeader `json:"headers,omitempty"`
	Body         string             `json:"body"`
	BodyIsBinary bool               `json:"body_is_binary,omitempty"`
	SizeBytes    int64              `json:"size_bytes"`
	Truncated    bool               `json:"truncated,omitempty"`
	DurationMs   int64              `json:"duration_ms"`
	ExecutedFrom string             `json:"executed_from"`
	Error        string             `json:"error,omitempty"`
}

// HTTPClientHandler — handler da ferramenta Cliente HTTP.
type HTTPClientHandler struct {
	kubeManager    *config.KubeConfigManager
	historyTracker *history.HistoryTracker
}

func NewHTTPClientHandler(km *config.KubeConfigManager, ht *history.HistoryTracker) *HTTPClientHandler {
	return &HTTPClientHandler{kubeManager: km, historyTracker: ht}
}

// ParseCurl — POST /api/v1/http-client/parse-curl {"curl": "..."}.
func (h *HTTPClientHandler) ParseCurl(c *gin.Context) {
	var req struct {
		Curl string `json:"curl"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Curl) == "" {
		c.JSON(http.StatusBadRequest, errorResponse("INVALID_REQUEST", "campo \"curl\" é obrigatório"))
		return
	}
	parsed, err := ParseCurl(req.Curl)
	if err != nil {
		c.JSON(http.StatusBadRequest, errorResponse("INVALID_CURL", err.Error()))
		return
	}
	c.JSON(http.StatusOK, parsed)
}

// HTTPClientPodOption é uma opção do seletor de pod/container do modo pod.
type HTTPClientPodOption struct {
	Name       string   `json:"name"`
	Containers []string `json:"containers"`
}

// ListPods — GET /api/v1/http-client/pods?cluster=&namespace=&deployment=
func (h *HTTPClientHandler) ListPods(c *gin.Context) {
	cluster := strings.TrimSpace(c.Query("cluster"))
	namespace := strings.TrimSpace(c.Query("namespace"))
	deployment := strings.TrimSpace(c.Query("deployment"))
	if cluster == "" || namespace == "" || deployment == "" {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_PARAMS", "cluster, namespace e deployment são obrigatórios"))
		return
	}
	clientset, err := h.kubeManager.GetClient(cluster)
	if err != nil {
		c.JSON(http.StatusBadGateway, errorResponse("CLUSTER_ERROR", err.Error()))
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()

	pods, err := listRunningPodsForDeployment(ctx, clientset, namespace, deployment)
	if err != nil {
		c.JSON(http.StatusBadGateway, errorResponse("POD_LIST_ERROR", err.Error()))
		return
	}
	options := make([]HTTPClientPodOption, 0, len(pods))
	for _, pod := range pods {
		containers := make([]string, 0, len(pod.Spec.Containers))
		for _, ct := range pod.Spec.Containers {
			containers = append(containers, ct.Name)
		}
		options = append(options, HTTPClientPodOption{Name: pod.Name, Containers: containers})
	}
	c.JSON(http.StatusOK, gin.H{"pods": options})
}

// Send — POST /api/v1/http-client/send. Síncrono: uma requisição, uma resposta.
func (h *HTTPClientHandler) Send(c *gin.Context) {
	var req HTTPClientRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse("INVALID_REQUEST", err.Error()))
		return
	}
	if msg := normalizeHTTPClientRequest(&req); msg != "" {
		c.JSON(http.StatusBadRequest, errorResponse("INVALID_REQUEST", msg))
		return
	}

	start := time.Now()
	var resp HTTPClientResponse
	if req.ExecutionMode == "pod" {
		ctx, cancel := context.WithTimeout(c.Request.Context(),
			httpClientEphemeralReadyTimout+time.Duration(req.TimeoutMs)*time.Millisecond+10*time.Second)
		defer cancel()
		var err error
		resp, err = h.sendFromPod(ctx, req)
		if err != nil {
			c.JSON(http.StatusBadGateway, errorResponse("POD_EXEC_ERROR", err.Error()))
			h.logHistory(c, req, start, nil, err)
			return
		}
	} else {
		resp = sendFromServer(c.Request.Context(), req)
	}
	h.logHistory(c, req, start, &resp, nil)
	c.JSON(http.StatusOK, resp)
}

// normalizeHTTPClientRequest valida e aplica defaults. Devolve a mensagem de erro, ou "".
func normalizeHTTPClientRequest(req *HTTPClientRequest) string {
	req.ExecutionMode = strings.ToLower(strings.TrimSpace(req.ExecutionMode))
	if req.ExecutionMode == "" {
		req.ExecutionMode = "server"
	}
	if req.ExecutionMode != "server" && req.ExecutionMode != "pod" {
		return "execution_mode deve ser server ou pod"
	}
	req.Method = strings.ToUpper(strings.TrimSpace(req.Method))
	if req.Method == "" {
		req.Method = "GET"
	}
	if !httpClientMethods[req.Method] {
		return "método não suportado: " + req.Method
	}
	req.URL = strings.TrimSpace(req.URL)
	u, err := url.Parse(req.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "URL inválida — use http:// ou https:// com host"
	}
	if req.ExecutionMode == "pod" {
		req.Cluster = strings.TrimSpace(req.Cluster)
		req.Namespace = strings.TrimSpace(req.Namespace)
		req.Deployment = strings.TrimSpace(req.Deployment)
		if req.Cluster == "" || req.Namespace == "" || req.Deployment == "" {
			return "cluster, namespace e deployment são obrigatórios no modo pod"
		}
	}
	headers := req.Headers[:0]
	for _, hd := range req.Headers {
		hd.Key = strings.TrimSpace(hd.Key)
		if hd.Key == "" {
			continue
		}
		if strings.ContainsAny(hd.Key, "\r\n:") || strings.ContainsAny(hd.Value, "\r\n") {
			return "header inválido: " + hd.Key
		}
		headers = append(headers, hd)
	}
	req.Headers = headers
	if req.TimeoutMs <= 0 {
		req.TimeoutMs = httpClientDefaultTimeoutMs
	}
	if req.TimeoutMs > httpClientMaxTimeoutMs {
		req.TimeoutMs = httpClientMaxTimeoutMs
	}
	return ""
}

// sendFromServer executa a requisição a partir do próprio servidor (net/http).
func sendFromServer(parent context.Context, req HTTPClientRequest) HTTPClientResponse {
	resp := HTTPClientResponse{ExecutedFrom: "servidor"}
	ctx, cancel := context.WithTimeout(parent, time.Duration(req.TimeoutMs)*time.Millisecond)
	defer cancel()

	var body io.Reader
	if req.Body != "" {
		body = strings.NewReader(req.Body)
	}
	httpReq, err := http.NewRequestWithContext(ctx, req.Method, req.URL, body)
	if err != nil {
		resp.Error = err.Error()
		return resp
	}
	for _, hd := range req.Headers {
		if strings.EqualFold(hd.Key, "Host") {
			httpReq.Host = hd.Value
			continue
		}
		httpReq.Header.Add(hd.Key, hd.Value)
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	if req.InsecureSkipVerify {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // opção explícita do usuário
	}
	client := &http.Client{Transport: transport}
	if !req.FollowRedirects {
		client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	}

	start := time.Now()
	httpResp, err := client.Do(httpReq)
	if err != nil {
		resp.DurationMs = time.Since(start).Milliseconds()
		resp.Error = err.Error()
		return resp
	}
	defer httpResp.Body.Close()
	data, readErr := io.ReadAll(io.LimitReader(httpResp.Body, httpClientMaxBodyBytes+1))
	// Tamanho real mesmo além do teto: drena o resto só para contar (sem guardar).
	extra, _ := io.Copy(io.Discard, httpResp.Body)
	resp.DurationMs = time.Since(start).Milliseconds()
	if readErr != nil {
		resp.Error = "falha ao ler a resposta: " + readErr.Error()
	}

	resp.Status = httpResp.StatusCode
	resp.StatusText = strings.TrimSpace(strings.TrimPrefix(httpResp.Status, strconv.Itoa(httpResp.StatusCode)))
	resp.Headers = flattenHTTPHeaders(httpResp.Header)
	resp.SizeBytes = int64(len(data)) + extra
	setHTTPClientBody(&resp, data)
	return resp
}

// setHTTPClientBody aplica o teto de exibição e a detecção de binário.
func setHTTPClientBody(resp *HTTPClientResponse, data []byte) {
	if len(data) > httpClientMaxBodyBytes {
		data = data[:httpClientMaxBodyBytes]
		resp.Truncated = true
	}
	if !utf8.Valid(data) {
		resp.BodyIsBinary = true
		resp.Body = ""
		return
	}
	resp.Body = string(data)
}

func flattenHTTPHeaders(h http.Header) []HTTPClientHeader {
	keys := make([]string, 0, len(h))
	for k := range h {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]HTTPClientHeader, 0, len(keys))
	for _, k := range keys {
		for _, v := range h[k] {
			out = append(out, HTTPClientHeader{Key: k, Value: v})
		}
	}
	return out
}

// Marcadores da saída do script de curl no pod (ver buildHTTPClientCurlScript).
const (
	httpClientMarkMeta = "___HTTPCLIENT_META___"
	httpClientMarkErr  = "___HTTPCLIENT_ERR___"
	httpClientMarkRC   = "___HTTPCLIENT_RC___"
	httpClientMarkHdr  = "___HTTPCLIENT_HDR___"
	httpClientMarkBody = "___HTTPCLIENT_BODY___"
)

// buildHTTPClientCurlScript monta o script que roda no Ephemeral Container. Headers, métricas,
// stderr e corpo vão para arquivos temporários e são impressos em seções com marcadores — o corpo
// por último (pode conter qualquer coisa) e cortado com `head -c`. O script sempre sai 0, para o
// exec não descartar a saída; o código real do curl vai na seção RC.
func buildHTTPClientCurlScript(req HTTPClientRequest, tmp string) string {
	args := []string{"curl", "-sS", "--max-time", strconv.FormatFloat(float64(req.TimeoutMs)/1000, 'f', 3, 64),
		"-X", req.Method, "-o", tmp + ".body", "-D", tmp + ".hdr",
		"-w", "%{http_code} %{size_download} %{time_total}"}
	if req.FollowRedirects {
		args = append(args, "-L")
	}
	if req.InsecureSkipVerify {
		args = append(args, "-k")
	}
	if req.Method == "HEAD" {
		args = append(args, "-I")
	}
	for _, hd := range req.Headers {
		args = append(args, "-H", hd.Key+": "+hd.Value)
	}
	if req.Body != "" {
		// Sem Content-Type explícito, o curl mandaria application/x-www-form-urlencoded; o modo
		// servidor não manda nenhum. `-H 'Content-Type:'` remove o default e deixa os dois iguais.
		if !hasHeader(req.Headers, "Content-Type") {
			args = append(args, "-H", "Content-Type:")
		}
		args = append(args, "--data-binary", req.Body)
	}
	args = append(args, "--", req.URL)

	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = quoteShellArg(a)
	}
	t := quoteShellArg(tmp)
	return fmt.Sprintf(`%[1]s >%[2]s.meta 2>%[2]s.err; rc=$?; `+
		`printf '%[3]s'; cat %[2]s.meta 2>/dev/null; printf '\n%[4]s'; cat %[2]s.err 2>/dev/null; `+
		`printf '\n%[5]s%%d\n%[6]s\n' "$rc"; cat %[2]s.hdr 2>/dev/null; printf '\n%[7]s\n'; `+
		`head -c %[8]d %[2]s.body 2>/dev/null; rm -f %[2]s.meta %[2]s.err %[2]s.hdr %[2]s.body; exit 0`,
		strings.Join(quoted, " "), t, httpClientMarkMeta, httpClientMarkErr, httpClientMarkRC, httpClientMarkHdr,
		httpClientMarkBody, httpClientMaxBodyBytes+1)
}

// between devolve o texto entre dois marcadores (ou até o fim, se `end` for vazio).
func between(s, start, end string) string {
	i := strings.Index(s, start)
	if i == -1 {
		return ""
	}
	s = s[i+len(start):]
	if end == "" {
		return s
	}
	if j := strings.Index(s, end); j != -1 {
		return s[:j]
	}
	return s
}

// parseHTTPClientCurlOutput interpreta a saída de buildHTTPClientCurlScript.
func parseHTTPClientCurlOutput(out string) (HTTPClientResponse, error) {
	var resp HTTPClientResponse
	if !strings.Contains(out, httpClientMarkBody) {
		return resp, fmt.Errorf("saída inesperada do curl no pod: %s", truncateForError(out))
	}
	meta := strings.Fields(between(out, httpClientMarkMeta, "\n"+httpClientMarkErr))
	errText := strings.TrimSpace(between(out, httpClientMarkErr, "\n"+httpClientMarkRC))
	rc, _ := strconv.Atoi(strings.TrimSpace(between(out, httpClientMarkRC, "\n"+httpClientMarkHdr)))
	hdrText := between(out, httpClientMarkHdr+"\n", "\n"+httpClientMarkBody+"\n")
	body := between(out, httpClientMarkBody+"\n", "")

	if len(meta) >= 3 {
		resp.SizeBytes, _ = strconv.ParseInt(meta[1], 10, 64)
		if secs, err := strconv.ParseFloat(meta[2], 64); err == nil {
			resp.DurationMs = int64(secs * 1000)
		}
	}
	status, statusText, headers := parseCurlHeaderDump(hdrText)
	if rc != 0 && status == 0 {
		if errText == "" {
			errText = fmt.Sprintf("curl terminou com código %d", rc)
		}
		resp.Error = errText
		return resp, nil
	}
	resp.Status, resp.StatusText, resp.Headers = status, statusText, headers
	setHTTPClientBody(&resp, []byte(body))
	if rc != 0 {
		resp.Error = errText // ex.: timeout no meio do download — há status, mas incompleto
	}
	return resp, nil
}

// parseCurlHeaderDump lê o arquivo do `-D`: com -L há um bloco por salto; vale o último.
func parseCurlHeaderDump(dump string) (int, string, []HTTPClientHeader) {
	dump = strings.ReplaceAll(dump, "\r\n", "\n")
	var status int
	var statusText string
	var headers []HTTPClientHeader
	for _, line := range strings.Split(dump, "\n") {
		if strings.HasPrefix(line, "HTTP/") {
			parts := strings.SplitN(line, " ", 3)
			status, statusText, headers = 0, "", nil
			if len(parts) >= 2 {
				status, _ = strconv.Atoi(parts[1])
			}
			if len(parts) == 3 {
				statusText = strings.TrimSpace(parts[2])
			}
			continue
		}
		if k, v, ok := strings.Cut(line, ":"); ok && status != 0 {
			headers = append(headers, HTTPClientHeader{Key: strings.TrimSpace(k), Value: strings.TrimSpace(v)})
		}
	}
	return status, statusText, headers
}

func truncateForError(s string) string {
	if len(s) > 500 {
		return s[:500] + "…"
	}
	return s
}

// sendFromPod resolve o pod do Deployment, anexa (ou reaproveita) o Ephemeral Container de curl e
// executa a requisição de dentro dele.
func (h *HTTPClientHandler) sendFromPod(ctx context.Context, req HTTPClientRequest) (HTTPClientResponse, error) {
	clientset, err := h.kubeManager.GetClient(req.Cluster)
	if err != nil {
		return HTTPClientResponse{}, err
	}
	restConfig, err := h.kubeManager.GetRestConfig(req.Cluster)
	if err != nil {
		return HTTPClientResponse{}, err
	}
	podName, target, err := resolvePodForDeployment(ctx, clientset, req.Namespace, req.Deployment, req.PodName, req.ContainerName)
	if err != nil {
		return HTTPClientResponse{}, err
	}
	container, err := getOrCreateHTTPClientEphemeralContainer(ctx, clientset, req.Namespace, podName, target)
	if err != nil {
		return HTTPClientResponse{}, err
	}
	if err := waitHTTPClientEphemeralRunning(ctx, clientset, req.Namespace, podName, container); err != nil {
		return HTTPClientResponse{}, err
	}

	script := buildHTTPClientCurlScript(req, "/tmp/k8s-hpa-http-"+uuid.New().String())
	out, execErr := execCmdInPod(ctx, clientset, restConfig, req.Namespace, podName, container, []string{"sh", "-c", script})
	if execErr != nil {
		return HTTPClientResponse{}, fmt.Errorf("falha ao executar o curl no pod %s: %v", podName, execErr)
	}
	resp, err := parseHTTPClientCurlOutput(out)
	if err != nil {
		return HTTPClientResponse{}, err
	}
	resp.ExecutedFrom = fmt.Sprintf("pod %s/%s", req.Namespace, podName)
	return resp, nil
}

// getOrCreateHTTPClientEphemeralContainer anexa um Ephemeral Container de curl ao pod (ou
// reaproveita um já Running com a mesma imagem/alvo). Ephemeral containers não podem ser
// removidos pela API — ficam listados no pod até ele reiniciar; o `sleep 300` limita o tempo
// que o processo fica vivo.
func getOrCreateHTTPClientEphemeralContainer(ctx context.Context, clientset kubernetes.Interface, namespace, podName, target string) (string, error) {
	pod, err := clientset.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("falha ao buscar pod %s: %w", podName, err)
	}
	for _, ec := range pod.Spec.EphemeralContainers {
		if ec.Image != httpClientPodImage || ec.TargetContainerName != target {
			continue
		}
		for _, st := range pod.Status.EphemeralContainerStatuses {
			if st.Name == ec.Name && st.State.Running != nil {
				return ec.Name, nil
			}
		}
	}

	name := fmt.Sprintf("%s%d", httpClientEphemeralPrefix, time.Now().Unix())
	patch, err := json.Marshal(map[string]interface{}{
		"spec": map[string]interface{}{
			"ephemeralContainers": []corev1.EphemeralContainer{{
				EphemeralContainerCommon: corev1.EphemeralContainerCommon{
					Name:            name,
					Image:           httpClientPodImage,
					Command:         []string{"sleep", "300"},
					ImagePullPolicy: corev1.PullIfNotPresent,
				},
				TargetContainerName: target,
			}},
		},
	})
	if err != nil {
		return "", err
	}
	if _, err := clientset.CoreV1().Pods(namespace).Patch(ctx, podName, types.StrategicMergePatchType, patch, metav1.PatchOptions{}, "ephemeralcontainers"); err != nil {
		return "", fmt.Errorf("falha ao anexar ephemeral container: %w", err)
	}
	return name, nil
}

func waitHTTPClientEphemeralRunning(ctx context.Context, clientset kubernetes.Interface, namespace, podName, container string) error {
	deadline := time.Now().Add(httpClientEphemeralReadyTimout)
	for time.Now().Before(deadline) {
		pod, err := clientset.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("falha ao consultar o pod: %w", err)
		}
		for _, st := range pod.Status.EphemeralContainerStatuses {
			if st.Name != container {
				continue
			}
			if st.State.Running != nil {
				return nil
			}
			if st.State.Terminated != nil {
				return fmt.Errorf("container de curl terminou inesperadamente: %s", st.State.Terminated.Reason)
			}
			if st.State.Waiting != nil && st.State.Waiting.Reason != "ContainerCreating" {
				return fmt.Errorf("container de curl aguardando: %s %s", st.State.Waiting.Reason, st.State.Waiting.Message)
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(httpClientEphemeralPoll):
		}
	}
	return fmt.Errorf("timeout esperando o container de curl ficar pronto (%s)", httpClientEphemeralReadyTimout)
}

// logHistory registra a execução. Só método, host+caminho (sem query string), modo e status —
// headers e corpo podem conter tokens, então nunca vão para o histórico.
func (h *HTTPClientHandler) logHistory(c *gin.Context, req HTTPClientRequest, start time.Time, resp *HTTPClientResponse, opErr error) {
	if h.historyTracker == nil {
		return
	}
	userInfo := GetUserInfoForHistory(c)
	target := req.URL
	if u, err := url.Parse(req.URL); err == nil {
		target = u.Scheme + "://" + u.Host + u.Path
	}
	status, errMsg := "success", ""
	after := map[string]interface{}{"method": req.Method, "url": target, "execution_mode": req.ExecutionMode}
	if req.ExecutionMode == "pod" {
		after["namespace"] = req.Namespace
		after["deployment"] = req.Deployment
	}
	if opErr != nil {
		status, errMsg = "failed", opErr.Error()
	} else if resp != nil {
		after["http_status"] = resp.Status
		if resp.Error != "" {
			status, errMsg = "failed", resp.Error
		}
	}
	h.historyTracker.Log(history.HistoryEntry{
		UserEmail: userInfo.Email,
		UserName:  userInfo.Name,
		Action:    "http_client_request",
		Resource:  target,
		Cluster:   req.Cluster,
		Status:    status,
		After:     after,
		Duration:  time.Since(start).Milliseconds(),
		ErrorMsg:  errMsg,
	})
}
