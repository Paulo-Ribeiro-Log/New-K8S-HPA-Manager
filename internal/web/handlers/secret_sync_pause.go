package handlers

import (
	"context"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s-hpa-manager/internal/history"
	"k8s-hpa-manager/internal/storage"
)

// secret_sync_pause.go — "Pausar/Retomar sincronização" de um Secret gerenciado por um
// ExternalSecret (external-secrets). Motivado por um caso real: pra testar aplicações em HLG às
// vezes é preciso digitar manualmente um valor num Secret que o AKV não está sincronizando de
// verdade (falha na construção da secret na origem, ou algum impedimento técnico), mas editar e
// aplicar (secretApplyRequest/Apply, acima) não sobrevive nem alguns segundos quando o
// ExternalSecret está saudável — confirmado ao vivo (ver AKV-SECRET-VIEWER-STUDY.md): o Secret tem
// `ownerReferences` com `controller:true` apontando pro ExternalSecret, e uma anotação
// `reconcile.external-secrets.io/data-hash` — o operador vigia o Secret via watch e reconcilia
// (reverte) qualquer drift quase instantaneamente, não só no refreshInterval.
//
// "Pausar" apaga o ExternalSecret (parando o operador de vigiar) DEPOIS de guardar o manifesto
// completo no SecretSyncPauseStore — só quando `spec.target.deletionPolicy: Retain` (confirmado via
// leitura antes de qualquer escrita), que garante que apagar o ExternalSecret NÃO apaga o Secret
// (comportamento padrão do external-secrets seria `Delete`, que cascadearia). "Retomar" recria o
// ExternalSecret exatamente como estava a partir do manifesto guardado.
//
// Reaproveita os helpers de kubectl já existentes em akv_discovery.go (mesmo arquivo que introduziu
// o padrão "CRD sem dynamic client, sempre via kubectl shell" nesta app) — nenhum mecanismo de exec
// novo, só mais 2 chamadas (get -o yaml, apply -f -) no mesmo estilo.

const secretSyncPauseTimeout = 15 * time.Second

// findOwningExternalSecret devolve o nome do ExternalSecret dono do Secret, via
// metadata.ownerReferences (kind=ExternalSecret, controller=true) — mesmo campo que
// creationPolicy:Owner do external-secrets sempre popula. "" quando o Secret não é gerenciado por
// nenhum ExternalSecret (não é erro — só significa que Pausar/Retomar não se aplicam a ele).
func (h *SecretHandler) findOwningExternalSecret(ctx context.Context, cluster, namespace, name string) (string, error) {
	clientset, err := h.kubeManager.GetClient(cluster)
	if err != nil {
		return "", fmt.Errorf("failed to get client: %w", err)
	}
	secret, err := clientset.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	for _, ref := range secret.OwnerReferences {
		if ref.Kind == "ExternalSecret" {
			return ref.Name, nil
		}
	}
	return "", nil
}

// getExternalSecretYAML busca o manifesto COMPLETO em YAML (não só os campos parseados de
// externalSecretRaw) — é o que fica guardado pra "Retomar" recriar fielmente depois.
func getExternalSecretYAML(ctx context.Context, cluster, namespace, name string) (string, error) {
	args := []string{"get", "externalsecret", name, "-n", namespace, "--context", cluster, "-o", "yaml"}
	output, err := exec.CommandContext(ctx, "kubectl", args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s", strings.TrimSpace(string(output)))
	}
	return string(output), nil
}

// applyExternalSecretYAML recria o ExternalSecret a partir de um manifesto YAML guardado — mesmo
// padrão de `kubectl apply --context <cluster> -f -` já usado em AKVDiscoveryHandler.Start.
func applyExternalSecretYAML(ctx context.Context, cluster, manifest string) error {
	cmd := exec.CommandContext(ctx, "kubectl", "apply", "--context", cluster, "-f", "-")
	cmd.Stdin = strings.NewReader(manifest)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s", strings.TrimSpace(string(output)))
	}
	return nil
}

// GetSyncStatus — GET /secrets/:cluster/:namespace/:name/sync-status. Leitura pura (sem
// RequireSREGroup, mesmo padrão do Get/Describe acima) — usada pelo frontend pra decidir se
// mostra/habilita "Pausar sync" (só faz sentido pra Secret gerenciado por ExternalSecret com
// deletionPolicy Retain) ou "Retomar sync" (quando já está pausado).
func (h *SecretHandler) GetSyncStatus(c *gin.Context) {
	cluster := strings.TrimSpace(c.Param("cluster"))
	namespace := strings.TrimSpace(c.Param("namespace"))
	name := strings.TrimSpace(c.Param("name"))
	if cluster == "" || namespace == "" || name == "" {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_PARAMETER", "cluster, namespace and name are required"))
		return
	}

	if h.syncPauseStore != nil {
		if rec, err := h.syncPauseStore.Get(cluster, namespace, name); err == nil && rec != nil {
			c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{
				"owned":                true,
				"externalSecretName":   rec.ExternalSecretName,
				"deletionPolicyRetain": true, // só foi possível pausar originalmente porque já era Retain
				"paused":               true,
				"pausedBy":             rec.PausedBy,
				"pausedAt":             rec.PausedAt,
				"reason":               rec.Reason,
			}})
			return
		}
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), secretSyncPauseTimeout)
	defer cancel()

	esName, err := h.findOwningExternalSecret(ctx, cluster, namespace, name)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorResponse("SECRET_ERROR", err.Error()))
		return
	}
	if esName == "" {
		c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"owned": false, "paused": false}})
		return
	}

	raw, err := getExternalSecretJSON(ctx, cluster, namespace, esName)
	if err != nil {
		// ExternalSecret referenciado no ownerReference já não existe mais (apagado por fora desta
		// app, por exemplo) — não é um erro fatal pro usuário, só significa "não dá pra pausar".
		c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"owned": false, "paused": false}})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{
		"owned":                true,
		"externalSecretName":   esName,
		"deletionPolicyRetain": raw.Spec.Target.DeletionPolicy == "Retain",
		"paused":               false,
	}})
}

type pauseSyncRequest struct {
	Reason string `json:"reason"`
}

// PauseSync — POST /secrets/:cluster/:namespace/:name/pause-sync. Escrita real e potencialmente
// arriscada (apaga um recurso do cluster) — atrás de RequireSREGroup + InjectUserEmail (ver rota em
// server.go), auditado via HistoryTracker como toda operação destrutiva desta app.
func (h *SecretHandler) PauseSync(c *gin.Context) {
	cluster := strings.TrimSpace(c.Param("cluster"))
	namespace := strings.TrimSpace(c.Param("namespace"))
	name := strings.TrimSpace(c.Param("name"))
	if cluster == "" || namespace == "" || name == "" {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_PARAMETER", "cluster, namespace and name are required"))
		return
	}
	if h.syncPauseStore == nil {
		c.JSON(http.StatusServiceUnavailable, errorResponse("STORE_UNAVAILABLE", "Secret sync pause store not available"))
		return
	}

	var req pauseSyncRequest
	_ = c.ShouldBindJSON(&req) // reason é opcional — corpo vazio é válido

	ctx, cancel := context.WithTimeout(c.Request.Context(), secretSyncPauseTimeout)
	defer cancel()

	esName, err := h.findOwningExternalSecret(ctx, cluster, namespace, name)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorResponse("SECRET_ERROR", err.Error()))
		return
	}
	if esName == "" {
		c.JSON(http.StatusBadRequest, errorResponse("NOT_MANAGED", "este Secret não é gerenciado por nenhum ExternalSecret"))
		return
	}

	raw, err := getExternalSecretJSON(ctx, cluster, namespace, esName)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorResponse("EXTERNALSECRET_ERROR", err.Error()))
		return
	}
	if raw.Spec.Target.DeletionPolicy != "Retain" {
		c.JSON(http.StatusBadRequest, errorResponse("UNSAFE_DELETION_POLICY",
			fmt.Sprintf("o ExternalSecret %s tem deletionPolicy=%q (não \"Retain\") — apagá-lo apagaria o Secret junto, então pausar não é seguro aqui", esName, raw.Spec.Target.DeletionPolicy)))
		return
	}

	manifest, err := getExternalSecretYAML(ctx, cluster, namespace, esName)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorResponse("EXTERNALSECRET_ERROR", err.Error()))
		return
	}

	// Guarda o manifesto ANTES de apagar — se o delete falhar depois, sobra só um registro de pausa
	// "órfão" (sem efeito real, corrigível manualmente), bem menos grave que apagar sem ter guardado.
	userEmail := c.GetString("user_email")
	if err := h.syncPauseStore.Pause(storage.SecretSyncPause{
		Cluster:                cluster,
		Namespace:              namespace,
		SecretName:             name,
		ExternalSecretName:     esName,
		ExternalSecretManifest: manifest,
		PausedBy:               userEmail,
		Reason:                 req.Reason,
	}); err != nil {
		c.JSON(http.StatusInternalServerError, errorResponse("STORE_ERROR", err.Error()))
		return
	}

	if err := deleteExternalSecret(ctx, cluster, namespace, esName); err != nil {
		// Reverte o registro de pausa — o ExternalSecret continua existindo de verdade, não fica
		// marcado como pausado incorretamente.
		_ = h.syncPauseStore.Resume(cluster, namespace, name)
		c.JSON(http.StatusInternalServerError, errorResponse("DELETE_ERROR", err.Error()))
		return
	}

	if h.historyTracker != nil {
		_ = h.historyTracker.Log(history.HistoryEntry{
			Action:   "pause_secret_sync",
			Resource: fmt.Sprintf("%s/%s", namespace, name),
			Cluster:  cluster,
			Status:   "success",
			After:    map[string]interface{}{"external_secret_name": esName, "reason": req.Reason},
		})
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{
		"externalSecretName": esName,
		"pausedBy":           userEmail,
	}})
}

// ResumeSync — POST /secrets/:cluster/:namespace/:name/resume-sync. Recria o ExternalSecret a
// partir do manifesto guardado — sem parâmetros no corpo (nada pra escolher, é sempre "volta pro
// que estava antes de pausar").
func (h *SecretHandler) ResumeSync(c *gin.Context) {
	cluster := strings.TrimSpace(c.Param("cluster"))
	namespace := strings.TrimSpace(c.Param("namespace"))
	name := strings.TrimSpace(c.Param("name"))
	if cluster == "" || namespace == "" || name == "" {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_PARAMETER", "cluster, namespace and name are required"))
		return
	}
	if h.syncPauseStore == nil {
		c.JSON(http.StatusServiceUnavailable, errorResponse("STORE_UNAVAILABLE", "Secret sync pause store not available"))
		return
	}

	rec, err := h.syncPauseStore.Get(cluster, namespace, name)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorResponse("STORE_ERROR", err.Error()))
		return
	}
	if rec == nil {
		c.JSON(http.StatusNotFound, errorResponse("NOT_PAUSED", "este Secret não está com a sincronização pausada"))
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), secretSyncPauseTimeout)
	defer cancel()

	if err := applyExternalSecretYAML(ctx, cluster, rec.ExternalSecretManifest); err != nil {
		c.JSON(http.StatusInternalServerError, errorResponse("APPLY_ERROR", err.Error()))
		return
	}

	if err := h.syncPauseStore.Resume(cluster, namespace, name); err != nil {
		c.JSON(http.StatusInternalServerError, errorResponse("STORE_ERROR", err.Error()))
		return
	}

	if h.historyTracker != nil {
		_ = h.historyTracker.Log(history.HistoryEntry{
			Action:   "resume_secret_sync",
			Resource: fmt.Sprintf("%s/%s", namespace, name),
			Cluster:  cluster,
			Status:   "success",
			After:    map[string]interface{}{"external_secret_name": rec.ExternalSecretName},
		})
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{
		"externalSecretName": rec.ExternalSecretName,
	}})
}
