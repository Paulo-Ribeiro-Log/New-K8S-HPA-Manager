package handlers

import (
	"os"
	"os/exec"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
	"k8s-hpa-manager/internal/updater"
)

// VersionHandler lida com requisições de versão
type VersionHandler struct{}

// NewVersionHandler cria novo handler de versão
func NewVersionHandler() *VersionHandler {
	return &VersionHandler{}
}

// GetVersion retorna versão atual e verifica updates disponíveis
func (h *VersionHandler) GetVersion(c *gin.Context) {
	currentVersion := updater.Version

	// Verificar updates disponíveis via GitHub
	latestRelease, err := updater.GetLatestRelease(updater.RepoOwner, updater.RepoName)

	response := gin.H{
		"current_version":  currentVersion,
		"update_available": false,
	}

	if err == nil && latestRelease != nil {
		// Comparar versões
		currentVer, errCurrent := updater.ParseVersion(currentVersion)
		latestVer, errLatest := updater.ParseVersion(latestRelease.TagName)

		if errCurrent == nil && errLatest == nil && latestVer.IsNewerThan(currentVer) {
			response["update_available"] = true
			response["latest_version"] = latestRelease.TagName
			response["download_url"] = latestRelease.HTMLURL
		}
	}

	c.JSON(200, response)
}

// SelfUpdate executa o script de instalação para atualizar o binário
// POST /api/v1/version/update
func (h *VersionHandler) SelfUpdate(c *gin.Context) {
	c.JSON(200, gin.H{"success": true, "message": "Atualização iniciada. O servidor será reiniciado em breve."})

	go func() {
		time.Sleep(500 * time.Millisecond)
		// Sempre "main" — apontava para "new-k8s-hpa-dev", uma branch congelada ~1630 commits
		// atrás, então correções no install-from-github.sh (ex: restart automático do servidor
		// pós-update) nunca chegavam a este fluxo mesmo depois de mescladas na main.
		cmd := exec.Command("bash", "-c", "curl -fsSL https://raw.githubusercontent.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/main/install-from-github.sh | bash")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		log.Info().Msg("Iniciando self-update via install-from-github.sh")
		if err := cmd.Run(); err != nil {
			log.Error().Err(err).Msg("Self-update falhou")
		}
	}()
}
