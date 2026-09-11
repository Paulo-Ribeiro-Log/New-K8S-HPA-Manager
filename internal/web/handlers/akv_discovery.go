package handlers

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"k8s-hpa-manager/internal/history"
)

// AKVDiscoveryHandler implementa a ferramenta "Descoberta AKV" (Tools menu): cria um
// ExternalSecret apartado, com um regex de `find` livre (escolhido pelo usuário), pra visualizar
// o conteúdo real de um Azure Key Vault sem depender do `find`/`rewrite` do ExternalSecret oficial
// do namespace — sem alterar esse ExternalSecret original em nada. Ver AKV-SECRET-VIEWER-STUDY.md
// pro histórico completo da investigação que motivou esta ferramenta (inclusive o achado real de
// que o mesmo Vault costuma ser compartilhado por várias outras aplicações — daí o cuidado de
// nunca sugerir `.*` como regex padrão sem aviso).
//
// Ciclo de vida do ExternalSecret de descoberta: nome gerado (nunca fixo, evita colisão entre
// sessões concorrentes), sempre com `target.creationPolicy: Owner` (o Secret resultante fica com
// ownerReference pro ExternalSecret — confirmado ao vivo que apagar o ExternalSecret é suficiente
// pro K8s recolher o Secret sozinho via garbage collection, sem precisar de um 2º delete). Rastreado
// em memória (akvDiscoverySession) só pra alimentar o reaper de segurança — nunca persistido em
// disco, mesmo espírito efêmero de outras ferramentas desta app (Kafka/DB Test Tool, Net Discovery).
type AKVDiscoveryHandler struct {
	historyTracker *history.HistoryTracker
	sessions       sync.Map // key: "cluster/namespace/name" -> *akvDiscoverySession
}

type akvDiscoverySession struct {
	Cluster   string
	Namespace string
	Name      string
	CreatedAt time.Time
}

const (
	// akvDiscoverySessionMaxAge — teto de segurança: se o usuário fechar o modal sem clicar em
	// "Encerrar" (aba fechada, navegador travou, etc.), o reaper garante que o ExternalSecret de
	// descoberta não fica pra sempre copiando segredo real de outras apps pro cluster.
	akvDiscoverySessionMaxAge = 30 * time.Minute
	akvDiscoveryReapInterval  = 5 * time.Minute
	akvDiscoveryTimeout       = 30 * time.Second
)

// NewAKVDiscoveryHandler cria o handler e já inicia o reaper em background — mesmo padrão de
// `go startDBTestContainerReaper()` (db_test_tool.go) / `go w.Run()` (SpinnakerFleetWatcher).
func NewAKVDiscoveryHandler(ht *history.HistoryTracker) *AKVDiscoveryHandler {
	h := &AKVDiscoveryHandler{historyTracker: ht}
	go h.reapLoop()
	return h
}

func (h *AKVDiscoveryHandler) reapLoop() {
	ticker := time.NewTicker(akvDiscoveryReapInterval)
	defer ticker.Stop()
	for range ticker.C {
		now := time.Now()
		h.sessions.Range(func(key, value interface{}) bool {
			session, ok := value.(*akvDiscoverySession)
			if !ok || now.Sub(session.CreatedAt) < akvDiscoverySessionMaxAge {
				return true
			}
			ctx, cancel := context.WithTimeout(context.Background(), akvDiscoveryTimeout)
			_ = deleteExternalSecret(ctx, session.Cluster, session.Namespace, session.Name)
			cancel()
			h.sessions.Delete(key)
			return true
		})
	}
}

func akvSessionKey(cluster, namespace, name string) string {
	return cluster + "/" + namespace + "/" + name
}

// ─── Modelos de resposta ────────────────────────────────────────────────────

type akvExternalSecretSummary struct {
	Name            string `json:"name"`
	SecretStoreKind string `json:"secret_store_kind"`
	SecretStoreName string `json:"secret_store_name"`
	FindRegexp      string `json:"find_regexp,omitempty"`
	RewriteSource   string `json:"rewrite_source,omitempty"`
	RewriteTarget   string `json:"rewrite_target,omitempty"`
	TargetName      string `json:"target_name"`
	Ready           bool   `json:"ready"`
	StatusReason    string `json:"status_reason,omitempty"`
	StatusMessage   string `json:"status_message,omitempty"`
}

// externalSecretRaw — só os campos que esta ferramenta precisa ler de `kubectl get externalsecret
// -o json`. Schema real confirmado ao vivo contra clusters de produção (ver AKV-SECRET-VIEWER-
// STUDY.md) — `dataFrom[0].find.name.regexp` e `dataFrom[0].rewrite[0].regexp.{source,target}`.
type externalSecretRaw struct {
	Metadata struct {
		Name string `json:"name"`
	} `json:"metadata"`
	Spec struct {
		SecretStoreRef struct {
			Kind string `json:"kind"`
			Name string `json:"name"`
		} `json:"secretStoreRef"`
		Target struct {
			Name string `json:"name"`
		} `json:"target"`
		DataFrom []struct {
			Find struct {
				Name struct {
					Regexp string `json:"regexp"`
				} `json:"name"`
			} `json:"find"`
			Rewrite []struct {
				Regexp struct {
					Source string `json:"source"`
					Target string `json:"target"`
				} `json:"regexp"`
			} `json:"rewrite"`
		} `json:"dataFrom"`
	} `json:"spec"`
	Status struct {
		Conditions []struct {
			Status  string `json:"status"`
			Reason  string `json:"reason"`
			Message string `json:"message"`
		} `json:"conditions"`
	} `json:"status"`
}

func summarizeExternalSecret(raw externalSecretRaw) akvExternalSecretSummary {
	s := akvExternalSecretSummary{
		Name:            raw.Metadata.Name,
		SecretStoreKind: raw.Spec.SecretStoreRef.Kind,
		SecretStoreName: raw.Spec.SecretStoreRef.Name,
		TargetName:      raw.Spec.Target.Name,
	}
	if len(raw.Spec.DataFrom) > 0 {
		s.FindRegexp = raw.Spec.DataFrom[0].Find.Name.Regexp
		if len(raw.Spec.DataFrom[0].Rewrite) > 0 {
			s.RewriteSource = raw.Spec.DataFrom[0].Rewrite[0].Regexp.Source
			s.RewriteTarget = raw.Spec.DataFrom[0].Rewrite[0].Regexp.Target
		}
	}
	if len(raw.Status.Conditions) > 0 {
		cond := raw.Status.Conditions[0]
		s.Ready = cond.Status == "True"
		s.StatusReason = cond.Reason
		s.StatusMessage = cond.Message
	}
	return s
}

// ─── Helpers de kubectl (ExternalSecret é CRD — sem dynamic client vendorizado nesta app, ver
// nota "Dynamic Client (CRDs)" do CLAUDE.md — sempre via `kubectl` shell, mesmo padrão já usado
// pelo Resync AKV em secrets.go). ────────────────────────────────────────────

func getExternalSecretsJSON(ctx context.Context, cluster, namespace string) ([]externalSecretRaw, error) {
	args := []string{"get", "externalsecret", "-n", namespace, "--context", cluster, "-o", "json"}
	output, err := exec.CommandContext(ctx, "kubectl", args...).Output()
	if err != nil {
		return nil, fmt.Errorf("falha ao listar ExternalSecrets: %w", err)
	}
	var list struct {
		Items []externalSecretRaw `json:"items"`
	}
	if err := json.Unmarshal(output, &list); err != nil {
		return nil, fmt.Errorf("falha ao parsear lista de ExternalSecrets: %w", err)
	}
	return list.Items, nil
}

func getExternalSecretJSON(ctx context.Context, cluster, namespace, name string) (*externalSecretRaw, error) {
	args := []string{"get", "externalsecret", name, "-n", namespace, "--context", cluster, "-o", "json"}
	output, err := exec.CommandContext(ctx, "kubectl", args...).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%s", strings.TrimSpace(string(output)))
	}
	var raw externalSecretRaw
	if err := json.Unmarshal(output, &raw); err != nil {
		return nil, fmt.Errorf("falha ao parsear ExternalSecret: %w", err)
	}
	return &raw, nil
}

func deleteExternalSecret(ctx context.Context, cluster, namespace, name string) error {
	args := []string{"delete", "externalsecret", name, "-n", namespace, "--context", cluster, "--ignore-not-found"}
	output, err := exec.CommandContext(ctx, "kubectl", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s", strings.TrimSpace(string(output)))
	}
	return nil
}

func randomDiscoveryName() string {
	buf := make([]byte, 4)
	_, _ = rand.Read(buf)
	return "akv-discovery-" + hex.EncodeToString(buf)
}

// ─── Handlers HTTP ──────────────────────────────────────────────────────────

// ListExternalSecrets — GET /akv-discovery/:cluster/:namespace/external-secrets. Leitura pura
// (nenhum valor de segredo, só a configuração do find/rewrite já em uso) — sem RBAC extra, mesmo
// padrão de `secrets.GET("", secretHandler.List)`.
func (h *AKVDiscoveryHandler) ListExternalSecrets(c *gin.Context) {
	cluster := c.Param("cluster")
	namespace := c.Param("namespace")
	if cluster == "" || namespace == "" {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_PARAMETER", "cluster e namespace são obrigatórios"))
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), akvDiscoveryTimeout)
	defer cancel()

	items, err := getExternalSecretsJSON(ctx, cluster, namespace)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorResponse("LIST_ERROR", err.Error()))
		return
	}

	summaries := make([]akvExternalSecretSummary, 0, len(items))
	for _, item := range items {
		summaries = append(summaries, summarizeExternalSecret(item))
	}

	c.JSON(http.StatusOK, gin.H{"external_secrets": summaries})
}

type startAKVDiscoveryRequest struct {
	Cluster         string `json:"cluster" binding:"required"`
	Namespace       string `json:"namespace" binding:"required"`
	SecretStoreKind string `json:"secret_store_kind"`
	SecretStoreName string `json:"secret_store_name" binding:"required"`
	Regexp          string `json:"regexp" binding:"required"`
	Reason          string `json:"reason" binding:"required"`
}

// discoveryManifestTemplate — mesmo shape confirmado ao vivo contra clusters reais nesta
// investigação (find.name.regexp + secretStoreRef + target.creationPolicy: Owner).
//
// Bug real corrigido — sem `deletionPolicy` explícito, o external-secrets aplica o default
// `Retain`, que (confirmado ao vivo) significa "só ADOTAR um Secret que já existe com esse nome"
// — nunca cria um novo do zero. Isso fazia o ExternalSecret de descoberta reportar
// `status.conditions[0].reason=SecretSynced`/`Ready:true` (mensagem "secret retained due to
// DeletionPolicy=Retain") mesmo sem NUNCA criar o Secret de destino — `status.binding.name` ficava
// vazio, e `kubectl get secret` confirmava "not found". `deletionPolicy: Delete` corrige a criação
// (permite criar do zero) e, de quebra, é a semântica certa pra esta ferramenta de qualquer forma —
// o Secret gerado deve mesmo ser apagado quando o ExternalSecret de descoberta for removido, sem
// depender só da garbage collection via ownerReference.
const discoveryManifestTemplate = `apiVersion: external-secrets.io/v1
kind: ExternalSecret
metadata:
  name: %s
  namespace: %s
  labels:
    app.kubernetes.io/managed-by: k8s-hpa-manager
    devops.k8s.io/akv-discovery: "true"
  annotations:
    devops.k8s.io/akv-discovery-reason: %q
spec:
  dataFrom:
  - find:
      name:
        regexp: %q
  refreshInterval: 1m
  secretStoreRef:
    kind: %s
    name: %s
  target:
    name: %s-secret
    creationPolicy: Owner
    deletionPolicy: Delete
`

// Start — POST /akv-discovery/start. Cria o ExternalSecret de descoberta. Protegido por
// RequireSREGroup() no router — copia segredo real do Vault pro cluster, mesmo nível de
// sensibilidade do Resync AKV.
func (h *AKVDiscoveryHandler) Start(c *gin.Context) {
	var req startAKVDiscoveryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse("INVALID_REQUEST", err.Error()))
		return
	}
	if req.SecretStoreKind == "" {
		req.SecretStoreKind = "ClusterSecretStore"
	}
	if req.SecretStoreKind != "ClusterSecretStore" && req.SecretStoreKind != "SecretStore" {
		c.JSON(http.StatusBadRequest, errorResponse("INVALID_REQUEST", "secret_store_kind deve ser ClusterSecretStore ou SecretStore"))
		return
	}
	if _, err := regexp.Compile(req.Regexp); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse("INVALID_REGEXP", fmt.Sprintf("regex inválido: %v", err)))
		return
	}

	name := randomDiscoveryName()
	manifest := fmt.Sprintf(discoveryManifestTemplate,
		name, req.Namespace, req.Reason, req.Regexp, req.SecretStoreKind, req.SecretStoreName, name,
	)

	ctx, cancel := context.WithTimeout(c.Request.Context(), akvDiscoveryTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "kubectl", "apply", "--context", req.Cluster, "-f", "-")
	cmd.Stdin = strings.NewReader(manifest)
	output, err := cmd.CombinedOutput()

	status := "success"
	if err != nil {
		status = "error"
	}
	if h.historyTracker != nil {
		_ = h.historyTracker.Log(history.HistoryEntry{
			Action:   "akv_discovery_start",
			Resource: fmt.Sprintf("%s/%s", req.Namespace, name),
			Cluster:  req.Cluster,
			After: map[string]interface{}{
				"secret_store": req.SecretStoreName,
				"regexp":       req.Regexp,
				"reason":       req.Reason,
			},
			Status: status,
		})
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "APPLY_ERROR",
				"message": strings.TrimSpace(string(output)),
			},
		})
		return
	}

	key := akvSessionKey(req.Cluster, req.Namespace, name)
	h.sessions.Store(key, &akvDiscoverySession{
		Cluster:   req.Cluster,
		Namespace: req.Namespace,
		Name:      name,
		CreatedAt: time.Now(),
	})

	c.JSON(http.StatusOK, gin.H{
		"name":        name,
		"target_name": name + "-secret",
		"cluster":     req.Cluster,
		"namespace":   req.Namespace,
	})
}

// Status — GET /akv-discovery/:cluster/:namespace/:name/status. Leitura pura, sem RBAC extra
// (não expõe nenhum valor de segredo, só o estado de sincronização).
func (h *AKVDiscoveryHandler) Status(c *gin.Context) {
	cluster := c.Param("cluster")
	namespace := c.Param("namespace")
	name := c.Param("name")

	ctx, cancel := context.WithTimeout(c.Request.Context(), akvDiscoveryTimeout)
	defer cancel()

	raw, err := getExternalSecretJSON(ctx, cluster, namespace, name)
	if err != nil {
		c.JSON(http.StatusNotFound, errorResponse("NOT_FOUND", err.Error()))
		return
	}

	summary := summarizeExternalSecret(*raw)
	c.JSON(http.StatusOK, gin.H{
		"ready":          summary.Ready,
		"status_reason":  summary.StatusReason,
		"status_message": summary.StatusMessage,
		"target_name":    summary.TargetName,
	})
}

type akvDiscoveredKey struct {
	Key          string `json:"key"`
	ValueBase64  string `json:"value_base64"`
	ValueDecoded string `json:"value_decoded,omitempty"`
	IsBinary     bool   `json:"is_binary"`
}

// Data — GET /akv-discovery/:cluster/:namespace/:name/data. Expõe valor real de segredo —
// protegido por RequireSREGroup() no router, mesmo nível do endpoint de revelar valor de Secret
// já existente nesta app.
func (h *AKVDiscoveryHandler) Data(c *gin.Context) {
	cluster := c.Param("cluster")
	namespace := c.Param("namespace")
	name := c.Param("name")

	ctx, cancel := context.WithTimeout(c.Request.Context(), akvDiscoveryTimeout)
	defer cancel()

	targetName := name + "-secret"
	args := []string{"get", "secret", targetName, "-n", namespace, "--context", cluster, "-o", "json"}
	output, err := exec.CommandContext(ctx, "kubectl", args...).CombinedOutput()
	if err != nil {
		c.JSON(http.StatusNotFound, errorResponse(
			"SECRET_NOT_FOUND",
			"O Secret ainda não foi sincronizado (ou a sincronização falhou) — confira o status antes de buscar os dados: "+strings.TrimSpace(string(output)),
		))
		return
	}

	var secret struct {
		Data map[string]string `json:"data"`
	}
	if err := json.Unmarshal(output, &secret); err != nil {
		c.JSON(http.StatusInternalServerError, errorResponse("PARSE_ERROR", err.Error()))
		return
	}

	keys := make([]akvDiscoveredKey, 0, len(secret.Data))
	for k, v := range secret.Data {
		decoded, decErr := base64.StdEncoding.DecodeString(v)
		entry := akvDiscoveredKey{Key: k, ValueBase64: v}
		if decErr != nil || !utf8.Valid(decoded) {
			entry.IsBinary = true
		} else {
			entry.ValueDecoded = string(decoded)
		}
		keys = append(keys, entry)
	}

	c.JSON(http.StatusOK, gin.H{"keys": keys})
}

// Stop — DELETE /akv-discovery/:cluster/:namespace/:name. Apaga o ExternalSecret de descoberta;
// o Secret gerado (creationPolicy: Owner) é recolhido automaticamente pelo garbage collector do
// K8s via ownerReference — confirmado ao vivo, sem precisar de um 2º delete explícito. Protegido
// por RequireSREGroup() no router.
func (h *AKVDiscoveryHandler) Stop(c *gin.Context) {
	cluster := c.Param("cluster")
	namespace := c.Param("namespace")
	name := c.Param("name")

	ctx, cancel := context.WithTimeout(c.Request.Context(), akvDiscoveryTimeout)
	defer cancel()

	err := deleteExternalSecret(ctx, cluster, namespace, name)

	status := "success"
	if err != nil {
		status = "error"
	}
	if h.historyTracker != nil {
		_ = h.historyTracker.Log(history.HistoryEntry{
			Action:   "akv_discovery_stop",
			Resource: fmt.Sprintf("%s/%s", namespace, name),
			Cluster:  cluster,
			Status:   status,
		})
	}

	h.sessions.Delete(akvSessionKey(cluster, namespace, name))

	if err != nil {
		c.JSON(http.StatusInternalServerError, errorResponse("DELETE_ERROR", err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}
