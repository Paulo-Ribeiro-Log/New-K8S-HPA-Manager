package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"
	"k8s-hpa-manager/internal/pkg/nexus"
)

// SSOProfileHandler gerencia o perfil SSO corporativo centralizado.
// Armazena email, matrícula e senha única usada por todos os serviços SSO.
type SSOProfileHandler struct {
	baseDir string
}

// NewSSOProfileHandler cria o handler.
func NewSSOProfileHandler(baseDir string) *SSOProfileHandler {
	return &SSOProfileHandler{baseDir: baseDir}
}

func (h *SSOProfileHandler) profilePath() string {
	return filepath.Join(h.baseDir, "sso_profile.json")
}

// SSOProfileData contém credenciais SSO descriptografadas (uso interno).
type SSOProfileData struct {
	Email     string
	Matricula string
	Password  string
}

// ssoProfileFile é a estrutura persistida em disco (senha criptografada).
type ssoProfileFile struct {
	Email             string `json:"email"`
	Matricula         string `json:"matricula"`
	EncryptedPassword string `json:"encrypted_password"`
}

// LoadSSOProfile é exportada para uso por outros handlers do pacote.
func LoadSSOProfile(baseDir string) (*SSOProfileData, error) {
	data, err := os.ReadFile(filepath.Join(baseDir, "sso_profile.json"))
	if err != nil {
		return nil, fmt.Errorf("perfil SSO não configurado")
	}
	var p ssoProfileFile
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("arquivo de perfil SSO inválido")
	}
	if p.EncryptedPassword == "" {
		return nil, fmt.Errorf("senha SSO não configurada")
	}
	password, err := nexus.DecryptPassword(p.EncryptedPassword)
	if err != nil {
		return nil, fmt.Errorf("erro ao descriptografar senha SSO: %w", err)
	}
	return &SSOProfileData{
		Email:     p.Email,
		Matricula: p.Matricula,
		Password:  password,
	}, nil
}

// GetProfile — GET /api/v1/sso/profile
// Retorna email e matrícula configurados (nunca expõe senha).
func (h *SSOProfileHandler) GetProfile(c *gin.Context) {
	data, err := os.ReadFile(h.profilePath())
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"configured": false})
		return
	}
	var p ssoProfileFile
	if err := json.Unmarshal(data, &p); err != nil {
		c.JSON(http.StatusOK, gin.H{"configured": false, "error": "arquivo de perfil corrompido"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"configured": p.Email != "" || p.Matricula != "",
		"email":      p.Email,
		"matricula":  p.Matricula,
		"has_password": p.EncryptedPassword != "",
	})
}

// SaveProfile — PUT /api/v1/sso/profile
func (h *SSOProfileHandler) SaveProfile(c *gin.Context) {
	var req struct {
		Email     string `json:"email"`
		Matricula string `json:"matricula"`
		Password  string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "corpo inválido"})
		return
	}
	if req.Email == "" && req.Matricula == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "email ou matrícula são obrigatórios"})
		return
	}

	// Mantém senha atual se não enviada
	password := req.Password
	if password == "" {
		existing, err := LoadSSOProfile(h.baseDir)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "senha é obrigatória na primeira configuração"})
			return
		}
		password = existing.Password
	}

	encrypted, err := nexus.EncryptPassword(password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao criptografar senha: " + err.Error()})
		return
	}

	profile := ssoProfileFile{
		Email:             req.Email,
		Matricula:         req.Matricula,
		EncryptedPassword: encrypted,
	}

	raw, _ := json.MarshalIndent(profile, "", "  ")
	if err := os.MkdirAll(h.baseDir, 0700); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err := os.WriteFile(h.profilePath(), raw, 0600); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "message": "Perfil SSO salvo com sucesso"})
}

// DeleteProfile — DELETE /api/v1/sso/profile
func (h *SSOProfileHandler) DeleteProfile(c *gin.Context) {
	if err := os.Remove(h.profilePath()); err != nil && !os.IsNotExist(err) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}
