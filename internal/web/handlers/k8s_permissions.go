package handlers

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"k8s-hpa-manager/internal/config"
	k8sperm "k8s-hpa-manager/internal/kubernetes"
)

// K8sPermissionsHandler expõe permissões reais do K8s via SelfSubjectRulesReview.
// Cada consulta usa o kubeconfig do servidor (que é o do usuário em modo local),
// portanto o resultado reflete exatamente o que o K8s RBAC permite para aquele usuário.
type K8sPermissionsHandler struct {
	kubeManager *config.KubeConfigManager
	cache       permissionCache
}

// NewK8sPermissionsHandler cria o handler de permissões K8s.
func NewK8sPermissionsHandler(kubeManager *config.KubeConfigManager) *K8sPermissionsHandler {
	return &K8sPermissionsHandler{
		kubeManager: kubeManager,
		cache: permissionCache{
			entries: make(map[string]cachedEntry),
		},
	}
}

// permissionCache é um cache em memória com TTL para resultados de SelfSubjectRulesReview.
// Evita chamadas repetidas ao K8s para cada render de componente no frontend.
type permissionCache struct {
	mu      sync.RWMutex
	entries map[string]cachedEntry
}

type cachedEntry struct {
	perms     *k8sperm.NamespacePermissions
	expiresAt time.Time
}

const permCacheTTL = 5 * time.Minute

func (c *permissionCache) get(key string) (*k8sperm.NamespacePermissions, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.entries[key]
	if !ok || time.Now().After(e.expiresAt) {
		return nil, false
	}
	return e.perms, true
}

func (c *permissionCache) set(key string, perms *k8sperm.NamespacePermissions) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = cachedEntry{perms: perms, expiresAt: time.Now().Add(permCacheTTL)}
}

// GetNamespacePermissions retorna o que o usuário pode fazer num cluster+namespace.
//
// GET /api/v1/permissions/k8s?cluster=<name>&namespace=<ns>
func (h *K8sPermissionsHandler) GetNamespacePermissions(c *gin.Context) {
	cluster := c.Query("cluster")
	namespace := c.Query("namespace")
	if cluster == "" || namespace == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "parâmetros 'cluster' e 'namespace' são obrigatórios"})
		return
	}

	cacheKey := fmt.Sprintf("%s:%s", cluster, namespace)
	if cached, ok := h.cache.get(cacheKey); ok {
		c.JSON(http.StatusOK, cached)
		return
	}

	client, err := h.kubeManager.GetClient(cluster)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("falha ao obter client K8s: %v", err)})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	perms, err := k8sperm.CheckNamespacePermissions(ctx, client, cluster, namespace)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("falha ao verificar permissões K8s: %v", err)})
		return
	}

	h.cache.set(cacheKey, perms)
	c.JSON(http.StatusOK, perms)
}
