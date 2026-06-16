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
//  1. Contexts do kubeconfig com prefixo gke_ — sem credenciais, sempre disponível
//  2. gcloud CLI — lista projetos e clusters ativos (requer gcloud auth login)
func (k *KubeConfigManager) AutoDiscoverGKEClusters(logFunc func(string)) ([]GKEClusterConfig, []error) {
	if logFunc != nil {
		logFunc("[GKE] Descobrindo clusters GKE...")
	}

	seen := make(map[string]bool)
	var configs []GKEClusterConfig

	// Fonte 1: kubeconfig contexts com prefixo gke_ (sem credenciais)
	for _, c := range k.discoverGKEFromKubeconfig() {
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
	if _, err := exec.LookPath("gcloud"); err != nil {
		if logFunc != nil {
			logFunc("[GKE] ⚠️  gcloud CLI não encontrado — usando apenas kubeconfig")
		}
		return configs, nil
	}

	if logFunc != nil {
		logFunc("[GKE] Listando projetos via gcloud...")
	}

	projects, err := listGCPProjectsGcloud()
	if err != nil {
		if logFunc != nil {
			if len(configs) == 0 {
				logFunc("[GKE] ⚠️  gcloud não autenticado — execute: gcloud auth login")
			} else {
				logFunc("[GKE] ℹ️  gcloud sem autenticação — usando apenas kubeconfig")
			}
		}
		return configs, nil
	}

	if logFunc != nil {
		logFunc(fmt.Sprintf("[GKE] %d projeto(s) encontrado(s)", len(projects)))
	}

	var allErrors []error
	for _, project := range projects {
		clusters, err := listGKEClustersGcloud(project)
		if err != nil {
			allErrors = append(allErrors, fmt.Errorf("projeto %s: %w", project, err))
			continue
		}
		for _, c := range clusters {
			key := c.ProjectID + "|" + c.Region + "|" + c.Name
			if !seen[key] {
				seen[key] = true
				configs = append(configs, c)
				if logFunc != nil {
					logFunc(fmt.Sprintf("[GKE] ✅ %s — projeto: %s, região: %s (gcloud)", c.Name, c.ProjectID, c.Region))
				}
			}
		}
	}

	return configs, allErrors
}

// discoverGKEFromKubeconfig extrai clusters GKE dos contexts do kubeconfig (sem credenciais).
func (k *KubeConfigManager) discoverGKEFromKubeconfig() []GKEClusterConfig {
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

// listGCPProjectsGcloud lista projetos GCP ativos via gcloud CLI.
func listGCPProjectsGcloud() ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "gcloud", "projects", "list",
		"--filter=lifecycleState:ACTIVE", "--format=json(projectId)")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("gcloud projects list: %w", err)
	}

	var result []struct {
		ProjectID string `json:"projectId"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		return nil, fmt.Errorf("parse projetos: %w", err)
	}

	ids := make([]string, 0, len(result))
	for _, p := range result {
		if p.ProjectID != "" {
			ids = append(ids, p.ProjectID)
		}
	}
	return ids, nil
}

// listGKEClustersGcloud lista clusters GKE de um projeto via gcloud CLI.
func listGKEClustersGcloud(projectID string) ([]GKEClusterConfig, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "gcloud", "container", "clusters", "list",
		"--project", projectID,
		"--format=json(name,location,network)")
	out, err := cmd.Output()
	if err != nil {
		errMsg := strings.TrimSpace(string(out))
		// API não habilitada ou sem permissão — ignorar silenciosamente
		if strings.Contains(errMsg, "API has not been used") ||
			strings.Contains(errMsg, "not enabled") ||
			strings.Contains(errMsg, "PERMISSION_DENIED") {
			return nil, nil
		}
		return nil, fmt.Errorf("gcloud container clusters list: %w", err)
	}

	var result []struct {
		Name     string `json:"name"`
		Location string `json:"location"`
		Network  string `json:"network"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		return nil, fmt.Errorf("parse clusters (projeto %s): %w", projectID, err)
	}

	configs := make([]GKEClusterConfig, 0, len(result))
	for _, c := range result {
		configs = append(configs, GKEClusterConfig{
			Name:      c.Name,
			ProjectID: projectID,
			Region:    c.Location,
			Network:   c.Network,
		})
	}
	return configs, nil
}
