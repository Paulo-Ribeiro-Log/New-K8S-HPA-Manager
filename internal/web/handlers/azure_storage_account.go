package handlers

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// Conteúdo de UMA conta de armazenamento: seus containers (blob) e file shares. É o que a tela chama
// depois de achar a conta numa busca por subscription — olha só para ela, sem refazer a busca.
// Só leitura (GET no ARM pelo `az rest`); todos os parâmetros são validados antes de virar URL.

const (
	accountContentsTimeout = 45 * time.Second
	accountContentsMax     = 500 // itens mostrados por tipo
)

// ContainerInfo é um container (blob) da conta.
type ContainerInfo struct {
	Name         string `json:"name"`
	PublicAccess string `json:"public_access,omitempty"` // None | Blob | Container
	LeaseState   string `json:"lease_state,omitempty"`
	LastModified string `json:"last_modified,omitempty"`
	Immutable    bool   `json:"immutable,omitempty"` // tem política de imutabilidade (WORM)
	Deleted      bool   `json:"deleted,omitempty"`
}

// ShareInfo é um file share da conta.
type ShareInfo struct {
	Name         string `json:"name"`
	Tier         string `json:"tier,omitempty"`
	Protocol     string `json:"protocol,omitempty"` // SMB | NFS
	QuotaGB      string `json:"quota_gb,omitempty"`
	LastModified string `json:"last_modified,omitempty"`
}

// AccountContents é a resposta de GET /azure/storage-account/contents.
type AccountContents struct {
	Account        string          `json:"account"`
	ResourceGroup  string          `json:"resource_group"`
	SubscriptionID string          `json:"subscription_id"`
	Containers     []ContainerInfo `json:"containers"`
	Shares         []ShareInfo     `json:"shares"`
	Warnings       []string        `json:"warnings"`
	Truncated      bool            `json:"truncated,omitempty"`
	QueriedAt      string          `json:"queried_at"`
}

// AccountContents — GET /azure/storage-account/contents?subscription=<id>&resource_group=<rg>&account=<nome>
func (h *AzureStorageSearchHandler) AccountContents(c *gin.Context) {
	sub, rg, acc := strings.TrimSpace(c.Query("subscription")), strings.TrimSpace(c.Query("resource_group")), strings.TrimSpace(c.Query("account"))
	switch {
	case !reUUID.MatchString(sub):
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": gin.H{"code": "INVALID_SUBSCRIPTION", "message": "subscription inválida"}})
		return
	case !reRGName.MatchString(rg):
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": gin.H{"code": "INVALID_RESOURCE_GROUP", "message": "resource group inválido"}})
		return
	case !reAccountName.MatchString(acc):
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": gin.H{"code": "INVALID_ACCOUNT", "message": "nome de conta inválido (3 a 24 letras minúsculas e números)"}})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), accountContentsTimeout)
	defer cancel()
	c.JSON(http.StatusOK, gin.H{"success": true, "data": readAccountContents(ctx, sub, rg, acc)})
}

// readAccountContents lê containers e file shares em paralelo. Falha de um dos dois vira aviso (uma
// conta FileStorage não tem blob, uma BlobStorage não tem file shares, e a permissão pode faltar).
func readAccountContents(ctx context.Context, sub, rg, acc string) *AccountContents {
	out := &AccountContents{Account: acc, ResourceGroup: rg, SubscriptionID: sub, Containers: []ContainerInfo{}, Shares: []ShareInfo{}, Warnings: []string{}, QueriedAt: time.Now().UTC().Format(time.RFC3339)}
	base := armBase + "/subscriptions/" + url.PathEscape(sub) + "/resourceGroups/" + url.PathEscape(rg) + "/providers/Microsoft.Storage/storageAccounts/" + url.PathEscape(acc)

	var wg sync.WaitGroup
	var ctnList, shrList []map[string]any
	var ctnErr, shrErr error
	var tr1, tr2 bool
	wg.Add(2)
	go func() {
		defer wg.Done()
		ctnList, tr1, ctnErr = armList(ctx, base+"/blobServices/default/containers?api-version="+storageAPIVer)
	}()
	go func() {
		defer wg.Done()
		shrList, tr2, shrErr = armList(ctx, base+"/fileServices/default/shares?api-version="+storageAPIVer)
	}()
	wg.Wait()
	out.Truncated = tr1 || tr2

	if ctnErr != nil {
		out.Warnings = append(out.Warnings, "Containers: "+ctnErr.Error())
	}
	for _, it := range ctnList {
		if len(out.Containers) >= accountContentsMax {
			out.Truncated = true
			break
		}
		out.Containers = append(out.Containers, ContainerInfo{
			Name: digStr(it, "name"), PublicAccess: digStr(it, "properties", "publicAccess"), LeaseState: digStr(it, "properties", "leaseState"),
			LastModified: digStr(it, "properties", "lastModifiedTime"), Deleted: digStr(it, "properties", "deleted") == "true",
			Immutable: dig(it, "properties", "immutabilityPolicy") != nil,
		})
	}
	if shrErr != nil {
		out.Warnings = append(out.Warnings, "File shares: "+shrErr.Error())
	}
	for _, it := range shrList {
		if len(out.Shares) >= accountContentsMax {
			out.Truncated = true
			break
		}
		out.Shares = append(out.Shares, ShareInfo{
			Name: digStr(it, "name"), Tier: digStr(it, "properties", "accessTier"), Protocol: digStr(it, "properties", "enabledProtocols"),
			QuotaGB: digStr(it, "properties", "shareQuota"), LastModified: digStr(it, "properties", "lastModifiedTime"),
		})
	}
	return out
}
