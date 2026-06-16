package config

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	gcpprovider "k8s-hpa-manager/internal/cloudprovider/gcp"
)

// AutoDiscoverGKEClusters descobre clusters GKE de duas fontes em ordem de prioridade:
//  1. Contexts do kubeconfig com prefixo gke_ — extrai project/region/cluster sem credenciais
//  2. GKE REST API (Cloud Resource Manager + Container API) — usa ADC da app (mesmo token do Gemini)
//
// Não depende do gcloud CLI.
func (k *KubeConfigManager) AutoDiscoverGKEClusters(logFunc func(string)) ([]GKEClusterConfig, []error) {
	if logFunc != nil {
		logFunc("[GKE] 🔍 Descobrindo clusters GKE...")
	}

	seen := make(map[string]bool)
	var configs []GKEClusterConfig

	// Fonte 1: kubeconfig contexts com prefixo gke_ (sem credenciais, sempre disponível)
	kubeConfigs := k.discoverGKEFromKubeconfig()
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

	// Fonte 2: GKE REST API — usa ADC da app (mesmo token OAuth2 do Gemini)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	token, err := gcpprovider.GetGCPAccessToken(ctx)
	if err != nil {
		if logFunc != nil {
			if len(configs) == 0 {
				logFunc("[GKE] ⚠️  Sem credenciais GCP — autentique em AI Settings → Gemini → Código de Dispositivo")
			} else {
				logFunc("[GKE] ℹ️  Sem credenciais GCP para busca via API — usando apenas kubeconfig")
			}
		}
		return configs, nil
	}

	if logFunc != nil {
		logFunc("[GKE] 📋 Listando projetos GCP via API...")
	}

	projects, err := listGCPProjectsAPI(ctx, token)
	if err != nil {
		if logFunc != nil {
			logFunc(fmt.Sprintf("[GKE] ⚠️  Erro ao listar projetos: %v", err))
		}
		return configs, []error{err}
	}

	if logFunc != nil {
		logFunc(fmt.Sprintf("[GKE] 📋 %d projeto(s) encontrado(s) via API", len(projects)))
	}

	var allErrors []error
	for _, project := range projects {
		clusters, err := listGKEClustersAPI(ctx, token, project)
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
					logFunc(fmt.Sprintf("[GKE] ✅ %s — projeto: %s, região: %s (API)", c.Name, c.ProjectID, c.Region))
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

// listGCPProjectsAPI lista projetos GCP ativos via Cloud Resource Manager REST API.
func listGCPProjectsAPI(ctx context.Context, token string) ([]string, error) {
	url := "https://cloudresourcemanager.googleapis.com/v1/projects?filter=lifecycleState%3AACTIVE"

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Cloud Resource Manager API: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		var apiErr struct {
			Error struct{ Message string `json:"message"` } `json:"error"`
		}
		json.Unmarshal(body, &apiErr) //nolint:errcheck
		return nil, fmt.Errorf("Cloud Resource Manager API %d: %s", resp.StatusCode, apiErr.Error.Message)
	}

	var result struct {
		Projects []struct {
			ProjectID string `json:"projectId"`
		} `json:"projects"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse projects API: %w", err)
	}

	ids := make([]string, 0, len(result.Projects))
	for _, p := range result.Projects {
		if p.ProjectID != "" {
			ids = append(ids, p.ProjectID)
		}
	}
	return ids, nil
}

// listGKEClustersAPI lista clusters GKE em um projeto usando a Container API.
// Usa "-" como wildcard de localização para retornar clusters de todas as regiões/zonas.
func listGKEClustersAPI(ctx context.Context, token, projectID string) ([]GKEClusterConfig, error) {
	url := fmt.Sprintf("https://container.googleapis.com/v1/projects/%s/locations/-/clusters", projectID)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Goog-User-Project", projectID)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GKE clusters API: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == 403 || resp.StatusCode == 404 {
		// API não habilitada ou sem permissão — ignorar silenciosamente
		return nil, nil
	}
	if resp.StatusCode >= 400 {
		var apiErr struct {
			Error struct{ Message string `json:"message"` } `json:"error"`
		}
		json.Unmarshal(body, &apiErr) //nolint:errcheck
		msg := apiErr.Error.Message
		if strings.Contains(msg, "API has not been used") || strings.Contains(msg, "not enabled") {
			return nil, nil
		}
		return nil, fmt.Errorf("GKE API %d: %s", resp.StatusCode, msg)
	}

	var result struct {
		Clusters []struct {
			Name     string `json:"name"`
			Location string `json:"location"`
			Network  string `json:"network"`
		} `json:"clusters"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse clusters API (project %s): %w", projectID, err)
	}

	configs := make([]GKEClusterConfig, 0, len(result.Clusters))
	for _, c := range result.Clusters {
		configs = append(configs, GKEClusterConfig{
			Name:      c.Name,
			ProjectID: projectID,
			Region:    c.Location,
			Network:   c.Network,
		})
	}
	return configs, nil
}
