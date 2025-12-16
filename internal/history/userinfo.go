package history

import (
	"context"
	"os/exec"
	"strings"

	"github.com/gin-gonic/gin"
)

// UserInfo contém informações do usuário para auditoria
type UserInfo struct {
	Email string
	Name  string
}

// GetCurrentUserInfo obtém informações do usuário atual
// Compatível com SSO futuro (JWT via gin.Context) e RBAC atual (Azure CLI)
func GetCurrentUserInfo(c *gin.Context) UserInfo {
	var userInfo UserInfo

	// Tentar obter do contexto primeiro (SSO via JWT - preparado para futuro)
	if email, exists := c.Get("user_email"); exists {
		userInfo.Email = email.(string)
	}

	if name, exists := c.Get("user_name"); exists {
		userInfo.Name = name.(string)
	}

	// Se não encontrou no contexto, usar Azure CLI (RBAC atual)
	if userInfo.Email == "" {
		userInfo.Email = getCurrentUserEmailFromAzureCLI()
	}

	// Se ainda não tem nome, tentar extrair do email
	if userInfo.Name == "" && userInfo.Email != "" {
		// Extrair nome do email (ex: joao.silva@empresa.com → João Silva)
		parts := strings.Split(userInfo.Email, "@")
		if len(parts) > 0 {
			nameParts := strings.Split(parts[0], ".")
			if len(nameParts) >= 2 {
				// Capitalizar primeira letra de cada parte
				firstName := strings.Title(nameParts[0])
				lastName := strings.Title(nameParts[1])
				userInfo.Name = firstName + " " + lastName
			} else {
				userInfo.Name = strings.Title(parts[0])
			}
		}
	}

	return userInfo
}

// getCurrentUserEmailFromAzureCLI obtém email do usuário via Azure CLI
// (usado quando não há SSO - desenvolvimento local)
func getCurrentUserEmailFromAzureCLI() string {
	ctx := context.Background()
	cmd := exec.CommandContext(ctx, "az", "account", "show", "--query", "user.name", "-o", "tsv")

	output, err := cmd.Output()
	if err != nil {
		// Em caso de erro, retornar vazio (não travar aplicação)
		return ""
	}

	email := strings.TrimSpace(string(output))
	return email
}
