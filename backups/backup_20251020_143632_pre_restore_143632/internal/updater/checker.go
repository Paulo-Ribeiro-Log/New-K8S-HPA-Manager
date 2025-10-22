package updater

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// UpdateInfo contém informações sobre update disponível
type UpdateInfo struct {
	Available      bool
	CurrentVersion string
	LatestVersion  string
	ReleaseURL     string
	ReleaseNotes   string
}

// CheckForUpdates verifica se há updates disponíveis
func CheckForUpdates() (*UpdateInfo, error) {
	// Se versão é "dev", não verificar updates
	if Version == "dev" {
		return &UpdateInfo{
			Available:      false,
			CurrentVersion: "dev",
			LatestVersion:  "dev",
		}, nil
	}

	// Parse versão atual
	currentVer, err := ParseVersion(Version)
	if err != nil {
		return nil, fmt.Errorf("versão atual inválida: %w", err)
	}

	// Buscar última release do GitHub
	release, err := GetLatestRelease(RepoOwner, RepoName)
	if err != nil {
		// Se repositório não existe ou não há releases, considerar que está atualizado
		if strings.Contains(err.Error(), "repositório não encontrado") {
			return &UpdateInfo{
				Available:      false,
				CurrentVersion: currentVer.String(),
				LatestVersion:  currentVer.String(),
			}, nil
		}
		return nil, err
	}

	// Parse versão da release
	latestVer, err := ParseVersion(release.TagName)
	if err != nil {
		return nil, fmt.Errorf("versão da release inválida: %w", err)
	}

	// Comparar versões
	info := &UpdateInfo{
		Available:      latestVer.IsNewerThan(currentVer),
		CurrentVersion: currentVer.String(),
		LatestVersion:  latestVer.String(),
		ReleaseURL:     release.HTMLURL,
		ReleaseNotes:   release.Body,
	}

	return info, nil
}

// ShouldCheckForUpdates verifica se deve checar updates (1x por dia)
func ShouldCheckForUpdates() bool {
	// Se versão é "dev", não verificar
	if Version == "dev" {
		return false
	}

	cachePath := getUpdateCachePath()

	// Verificar última verificação
	info, err := os.Stat(cachePath)
	if err != nil {
		// Arquivo não existe - primeira verificação
		return true
	}

	// Verificar se passou 24 horas
	return time.Since(info.ModTime()) > 24*time.Hour
}

// MarkUpdateChecked marca que a verificação foi feita
func MarkUpdateChecked() error {
	cachePath := getUpdateCachePath()

	// Criar diretório se não existir
	dir := filepath.Dir(cachePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	// Criar arquivo vazio
	f, err := os.Create(cachePath)
	if err != nil {
		return err
	}
	defer f.Close()

	return nil
}

// getUpdateCachePath retorna o caminho do cache de update
func getUpdateCachePath() string {
	home := os.Getenv("HOME")
	return filepath.Join(home, ".k8s-hpa-manager", ".update-check")
}
