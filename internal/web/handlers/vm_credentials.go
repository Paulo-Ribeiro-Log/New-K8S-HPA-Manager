package handlers

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"k8s-hpa-manager/internal/storage"
)

// VMCredentialsHandler gerencia os perfis de credencial SSH do usuário logado — escopados por
// user_email (InjectUserEmail middleware), nunca expõe os segredos decriptografados em nenhuma
// resposta HTTP (só ListProfiles/metadados; o segredo real só é lido internamente por
// VMTerminalHandler ao abrir uma conexão de verdade, ver vm_terminal.go).
type VMCredentialsHandler struct {
	store *storage.VMCredentialStore
}

func NewVMCredentialsHandler(store *storage.VMCredentialStore) *VMCredentialsHandler {
	return &VMCredentialsHandler{store: store}
}

// credentialStoreUnavailable escreve a resposta 503 comum aos 4 handlers abaixo quando o store
// falhou ao inicializar (ver NewVMCredentialsHandler em server.go) — mesmo padrão "melhor
// esforço" já usado por NewCertificatesHandler pros seus sub-stores.
func (h *VMCredentialsHandler) credentialStoreUnavailable(c *gin.Context) bool {
	if h.store != nil {
		return false
	}
	c.JSON(http.StatusServiceUnavailable, gin.H{
		"success": false,
		"error":   gin.H{"code": "CREDENTIAL_STORE_UNAVAILABLE", "message": "Store de credenciais SSH indisponível neste servidor"},
	})
	return true
}

// ListProfiles — GET /api/v1/vms/credentials
func (h *VMCredentialsHandler) ListProfiles(c *gin.Context) {
	if h.credentialStoreUnavailable(c) {
		return
	}
	userInfo := GetUserInfoForHistory(c)
	profiles, err := h.store.ListProfiles(userInfo.Email)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   gin.H{"code": "LIST_CREDENTIALS_ERROR", "message": err.Error()},
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": profiles})
}

// saveCredentialProfileRequest é o corpo esperado por Create/Update.
type saveCredentialProfileRequest struct {
	Name          string `json:"name" binding:"required"`
	Username      string `json:"username" binding:"required"`
	AuthMethod    string `json:"authMethod" binding:"required"` // "key" | "password"
	PrivateKeyPEM string `json:"privateKeyPEM"`
	Passphrase    string `json:"passphrase"`
	Password      string `json:"password"`
}

// CreateProfile — POST /api/v1/vms/credentials
func (h *VMCredentialsHandler) CreateProfile(c *gin.Context) {
	h.saveProfile(c, "")
}

// UpdateProfile — PUT /api/v1/vms/credentials/:id — não permite trocar de dono (o WHERE interno
// do store já escopa por user_email, então tentar editar o perfil de outro usuário só falha com
// "não encontrado", nunca vaza nem sobrescreve).
func (h *VMCredentialsHandler) UpdateProfile(c *gin.Context) {
	h.saveProfile(c, c.Param("id"))
}

func (h *VMCredentialsHandler) saveProfile(c *gin.Context, id string) {
	if h.credentialStoreUnavailable(c) {
		return
	}
	var req saveCredentialProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   gin.H{"code": "INVALID_REQUEST", "message": fmt.Sprintf("requisição inválida: %v", err)},
		})
		return
	}

	userInfo := GetUserInfoForHistory(c)
	newID, err := h.store.SaveProfile(userInfo.Email, storage.SaveSSHCredentialProfileInput{
		ID:            id,
		Name:          req.Name,
		Username:      req.Username,
		AuthMethod:    req.AuthMethod,
		PrivateKeyPEM: req.PrivateKeyPEM,
		Passphrase:    req.Passphrase,
		Password:      req.Password,
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   gin.H{"code": "SAVE_CREDENTIAL_ERROR", "message": err.Error()},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"id": newID}})
}

// DeleteProfile — DELETE /api/v1/vms/credentials/:id
func (h *VMCredentialsHandler) DeleteProfile(c *gin.Context) {
	if h.credentialStoreUnavailable(c) {
		return
	}
	userInfo := GetUserInfoForHistory(c)
	if err := h.store.DeleteProfile(userInfo.Email, c.Param("id")); err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"error":   gin.H{"code": "DELETE_CREDENTIAL_ERROR", "message": err.Error()},
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"message": "perfil removido com sucesso"}})
}
