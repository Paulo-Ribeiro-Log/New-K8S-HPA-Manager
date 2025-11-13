package handlers

import (
	"github.com/gin-gonic/gin"
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
