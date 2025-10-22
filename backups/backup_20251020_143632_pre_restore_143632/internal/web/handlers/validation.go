package handlers

import (
	"github.com/gin-gonic/gin"
	"k8s-hpa-manager/internal/web/validators"
)

// ValidationHandler gerencia validações de pré-requisitos (VPN + Azure)
type ValidationHandler struct{}

// NewValidationHandler cria um novo handler de validação
func NewValidationHandler() *ValidationHandler {
	return &ValidationHandler{}
}

// ValidationStatus representa o status de validação completo
type ValidationStatus struct {
	Success      bool     `json:"success"`
	VPNConnected bool     `json:"vpn_connected"`
	AzureAuth    bool     `json:"azure_authenticated"`
	Errors       []string `json:"errors"`
	Warnings     []string `json:"warnings"`
}

// Validate verifica VPN e Azure CLI antes de permitir uso da aplicação
func (h *ValidationHandler) Validate(c *gin.Context) {
	status := ValidationStatus{
		Success:      true,
		VPNConnected: false,
		AzureAuth:    false,
		Errors:       []string{},
		Warnings:     []string{},
	}

	// 1. Validar VPN (requisito CRÍTICO)
	if err := validators.ValidateVPNConnectivity(); err != nil {
		status.Success = false
		status.VPNConnected = false
		status.Errors = append(status.Errors, "VPN desconectada - Kubernetes clusters inacessíveis")
		status.Warnings = append(status.Warnings, "Conecte-se à VPN corporativa para acessar os clusters AKS")
	} else {
		status.VPNConnected = true
	}

	// 2. Validar Azure CLI (requisito CRÍTICO)
	if err := validators.ValidateAzureAuth(); err != nil {
		status.Success = false
		status.AzureAuth = false
		status.Errors = append(status.Errors, "Azure CLI não autenticado")
		status.Warnings = append(status.Warnings, "Execute 'az login' no servidor para autenticar")
	} else {
		status.AzureAuth = true
	}

	// Retornar status
	if status.Success {
		c.JSON(200, gin.H{
			"success":           true,
			"vpn_connected":     status.VPNConnected,
			"azure_authenticated": status.AzureAuth,
			"message":           "✅ Todas as validações passaram - aplicação pronta para uso",
		})
	} else {
		c.JSON(200, gin.H{
			"success":           false,
			"vpn_connected":     status.VPNConnected,
			"azure_authenticated": status.AzureAuth,
			"errors":            status.Errors,
			"warnings":          status.Warnings,
		})
	}
}
