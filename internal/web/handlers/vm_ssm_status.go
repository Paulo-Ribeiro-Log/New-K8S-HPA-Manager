package handlers

import (
	"net/http"
	"os/exec"

	"github.com/gin-gonic/gin"
)

// VMSSMStatusHandler expõe se o servidor tem `aws` CLI + `session-manager-plugin` disponíveis no
// PATH — usado só pra popular um badge INFORMATIVO no frontend (Fase 5 do plano em
// /home/paulo/.claude/plans/scalable-greeting-kazoo.md). Nunca usado pra esconder o botão
// "Conectar via SSM" — a escolha entre SSH e SSM é sempre manual, mesmo quando a heurística sugere
// que um modo vai falhar (decisão explícita do usuário, ver mesmo princípio já aplicado a
// SupportedConnectionModes em ec2.go).
type VMSSMStatusHandler struct{}

func NewVMSSMStatusHandler() *VMSSMStatusHandler {
	return &VMSSMStatusHandler{}
}

type vmSSMStatusResponse struct {
	AWSCLIFound bool   `json:"awsCliFound"`
	PluginFound bool   `json:"pluginFound"`
	Available   bool   `json:"available"`
	Message     string `json:"message,omitempty"`
}

// CheckStatus — GET /api/v1/vms/ssm/status
func (h *VMSSMStatusHandler) CheckStatus(c *gin.Context) {
	_, awsErr := exec.LookPath("aws")
	_, pluginErr := exec.LookPath("session-manager-plugin")

	resp := vmSSMStatusResponse{
		AWSCLIFound: awsErr == nil,
		PluginFound: pluginErr == nil,
	}
	resp.Available = resp.AWSCLIFound && resp.PluginFound

	switch {
	case !resp.AWSCLIFound:
		resp.Message = "aws CLI não encontrado no PATH do servidor — necessário pra conexão via SSM Session Manager."
	case !resp.PluginFound:
		resp.Message = "session-manager-plugin não encontrado no PATH do servidor — instale em " +
			"https://docs.aws.amazon.com/systems-manager/latest/userguide/session-manager-working-with-install-plugin.html"
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": resp})
}
