package config

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// AutoDiscoverGKEClusters descobre clusters GKE de duas fontes em ordem de prioridade:
//  1. Contexts do kubeconfig com prefixo gke_ — extrai project/region/cluster sem gcloud
//  2. gcloud container clusters list — para clusters não presentes no kubeconfig
//
// Requer gcloud CLI instalado e autenticado (gcloud auth list).
func (k *KubeConfigManager) AutoDiscoverGKEClusters(logFunc func(string)) ([]GKEClusterConfig, []error) {
	if logFunc != nil {
		logFunc("[GKE] 🔍 Descobrindo clusters GKE...")
	}

	seen := make(map[string]bool)
	var configs []GKEClusterConfig

	// Fonte 1: kubeconfig contexts com prefixo gke_
	kubeConfigs := k.discoverGKEFromKubeconfig(logFunc)
	for _, c := range kubeConfigs {
		key := c.ProjectID + "|" + c.Region + "|" + c.Name
		if !seen[key] {
			seen[key] = true
			configs = append(configs, c)
			if logFunc != nil {
				logFunc(fmt.Sprintf("[GKE] ✅ %s — projeto: %s, região: %s (kubeconfig)", c.Name, c.ProjectID, c.Region))
			}
		}
	}

	// Fonte 2: gcloud CLI (requer autenticação)
	if gcloudAvailable() {
		gcloudConfigs, errs := k.discoverGKEFromGcloud(logFunc)
		for _, c := range gcloudConfigs {
			key := c.ProjectID + "|" + c.Region + "|" + c.Name
			if !seen[key] {
				seen[key] = true
				configs = append(configs, c)
				if logFunc != nil {
					logFunc(fmt.Sprintf("[GKE] ✅ %s — projeto: %s, região: %s (gcloud)", c.Name, c.ProjectID, c.Region))
				}
			}
		}
		if logFunc != nil && len(errs) > 0 {
			for _, e := range errs {
				logFunc(fmt.Sprintf("[GKE] ⚠️  %v", e))
			}
		}
		return configs, errs
	}

	if logFunc != nil {
		if len(configs) == 0 {
			logFunc("[GKE] ⚠️  gcloud CLI não encontrado e nenhum context gke_ no kubeconfig")
		} else {
			logFunc("[GKE] ℹ️  gcloud CLI não encontrado — usando apenas kubeconfig")
		}
	}

	return configs, nil
}

// discoverGKEFromKubeconfig extrai clusters GKE dos contexts do kubeconfig (sem gcloud).
func (k *KubeConfigManager) discoverGKEFromKubeconfig(_ func(string)) []GKEClusterConfig {
	if k.config == nil {
		return nil
	}

	var configs []GKEClusterConfig
	for contextName := range k.config.Contexts {
		project, region, cluster := splitGKEContext(contextName)
		if cluster == "" {
			continue
		}
		configs = append(configs, GKEClusterConfig{
			Name:      cluster,
			ProjectID: project,
			Region:    region,
		})
	}
	return configs
}

// discoverGKEFromGcloud lista clusters GKE via gcloud CLI em todos os projetos acessíveis.
func (k *KubeConfigManager) discoverGKEFromGcloud(logFunc func(string)) ([]GKEClusterConfig, []error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	// Listar projetos acessíveis
	if logFunc != nil {
		logFunc("[GKE] 📋 Listando projetos GCP via gcloud...")
	}
	projects, err := listGCPProjects(ctx)
	if err != nil {
		return nil, []error{fmt.Errorf("gcloud projects list: %w", err)}
	}
	if len(projects) == 0 {
		if logFunc != nil {
			logFunc("[GKE] ⚠️  Nenhum projeto GCP acessível (verifique: gcloud auth list)")
		}
		return nil, nil
	}

	if logFunc != nil {
		logFunc(fmt.Sprintf("[GKE] 📋 %d projeto(s) encontrado(s)", len(projects)))
	}

	var allConfigs []GKEClusterConfig
	var allErrors []error

	for _, project := range projects {
		clusters, err := listGKEClusters(ctx, project)
		if err != nil {
			allErrors = append(allErrors, fmt.Errorf("projeto %s: %w", project, err))
			continue
		}
		allConfigs = append(allConfigs, clusters...)
	}

	return allConfigs, allErrors
}

// listGCPProjects lista os IDs de projetos acessíveis via gcloud.
func listGCPProjects(ctx context.Context) ([]string, error) {
	cmdCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	out, err := exec.CommandContext(cmdCtx,
		"gcloud", "projects", "list",
		"--format=json(projectId)",
		"--filter=lifecycleState:ACTIVE",
	).Output()
	if err != nil {
		return nil, fmt.Errorf("gcloud projects list: %w", err)
	}

	var projects []struct {
		ProjectID string `json:"projectId"`
	}
	if err := json.Unmarshal(out, &projects); err != nil {
		return nil, fmt.Errorf("parse projects: %w", err)
	}

	ids := make([]string, 0, len(projects))
	for _, p := range projects {
		if p.ProjectID != "" {
			ids = append(ids, p.ProjectID)
		}
	}
	return ids, nil
}

// listGKEClusters lista os clusters GKE em um projeto via gcloud.
func listGKEClusters(ctx context.Context, projectID string) ([]GKEClusterConfig, error) {
	cmdCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	out, err := exec.CommandContext(cmdCtx,
		"gcloud", "container", "clusters", "list",
		"--project", projectID,
		"--format=json",
	).Output()
	if err != nil {
		errStr := string(out)
		// API não habilitada ou sem permissão — não é erro fatal
		if strings.Contains(errStr, "PERMISSION_DENIED") ||
			strings.Contains(errStr, "not enabled") ||
			strings.Contains(errStr, "API has not been used") {
			return nil, nil
		}
		return nil, fmt.Errorf("gcloud container clusters list --project %s: %w", projectID, err)
	}

	var raw []struct {
		Name     string `json:"name"`
		Location string `json:"location"`
		Network  string `json:"network"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("parse clusters (project %s): %w", projectID, err)
	}

	configs := make([]GKEClusterConfig, 0, len(raw))
	for _, c := range raw {
		configs = append(configs, GKEClusterConfig{
			Name:      c.Name,
			ProjectID: projectID,
			Region:    c.Location,
			Network:   c.Network,
		})
	}
	return configs, nil
}

// gcloudAvailable retorna true se o gcloud CLI estiver instalado.
func gcloudAvailable() bool {
	_, err := exec.LookPath("gcloud")
	return err == nil
}
