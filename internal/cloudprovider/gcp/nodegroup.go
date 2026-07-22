package gcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"k8s-hpa-manager/internal/cloudprovider"
	"k8s-hpa-manager/internal/models"
)

// GCPNodeGroupProvider implementa cloudprovider.NodeGroupProvider usando o gcloud CLI.
// Requer que o gcloud esteja instalado e autenticado no servidor (gcloud auth login).
type GCPNodeGroupProvider struct {
	clusterName string
	projectID   string
	location    string // região (southamerica-east1) ou zona (us-central1-a)
}

// NewGCPNodeGroupProvider cria um provider GCP para um cluster GKE.
func NewGCPNodeGroupProvider(clusterName, projectID, location string) *GCPNodeGroupProvider {
	return &GCPNodeGroupProvider{
		clusterName: clusterName,
		projectID:   projectID,
		location:    location,
	}
}

// ValidateAuth verifica se há credenciais GCP válidas. Prioriza o ADC da própria aplicação
// (Device Auth Grant, mesmo token usado por ListNodeGroups via REST API) — só exige o gcloud
// CLI local quando não há ADC válido, pois ScaleNodeGroup/SetAutoscaling ainda dependem do
// CLI (sem equivalente REST implementado).
func (p *GCPNodeGroupProvider) ValidateAuth(ctx context.Context) error {
	if NewGCPAuthManager().CheckStatus(ctx).Authenticated {
		return nil
	}

	if _, err := exec.LookPath("gcloud"); err != nil {
		return fmt.Errorf("gcloud CLI não encontrado e nenhuma credencial ADC configurada — instale o Google Cloud SDK ou autentique via Device Auth Grant (Auto-Discover)")
	}

	if err := EnsureGKEAuthPlugin(nil); err != nil {
		return err
	}

	return IsGcloudAuthActive(ctx)
}

// gcpNodePool é o formato de node pool retornado tanto pela Container REST API
// quanto por `gcloud container node-pools list --format=json` (mesmos nomes de campo —
// o gcloud é um proxy fino da API).
type gcpNodePool struct {
	Name   string `json:"name"`
	Config struct {
		MachineType string `json:"machineType"`
	} `json:"config"`
	InitialNodeCount int32 `json:"initialNodeCount"`
	Autoscaling      struct {
		Enabled      bool  `json:"enabled"`
		MinNodeCount int32 `json:"minNodeCount"`
		MaxNodeCount int32 `json:"maxNodeCount"`
	} `json:"autoscaling"`
	Status string `json:"status"`
}

// ListNodeGroups lista os node pools do cluster.
//
// Tenta primeiro a Container Engine REST API usando o token ADC da própria aplicação
// (GetFreshGKEToken — mesmo mecanismo já usado para o client-go em GetRestConfig). Isso evita
// depender da sessão `gcloud auth login` local, que é uma credencial independente do ADC da
// app e pode expirar sem que o usuário perceba ("Reauthentication failed"). Só cai para o
// gcloud CLI se não houver token disponível ou se a chamada REST falhar.
func (p *GCPNodeGroupProvider) ListNodeGroups(ctx context.Context, _ string) ([]models.NodePool, error) {
	var raw []gcpNodePool

	if token := GetFreshGKEToken(ctx); token != "" {
		if fromAPI, err := p.listNodePoolsViaAPI(ctx, token); err == nil {
			raw = fromAPI
		}
	}

	if raw == nil {
		fromCLI, err := p.listNodePoolsViaCLI(ctx)
		if err != nil {
			return nil, err
		}
		raw = fromCLI
	}

	pools := make([]models.NodePool, 0, len(raw))
	for _, np := range raw {
		pools = append(pools, models.NodePool{
			Name:               np.Name,
			VMSize:             np.Config.MachineType,
			NodeCount:          np.InitialNodeCount,
			MinNodeCount:       np.Autoscaling.MinNodeCount,
			MaxNodeCount:       np.Autoscaling.MaxNodeCount,
			AutoscalingEnabled: np.Autoscaling.Enabled,
			Status:             strings.ToLower(np.Status),
			ClusterName:        p.clusterName,
			IsSystemPool:       np.Name == "default-pool",
		})
	}
	return pools, nil
}

// listNodePoolsViaAPI busca os node pools via Container Engine REST API (sem depender do
// gcloud CLI local). location aceita tanto região quanto zona — a API não distingue via flag
// como o CLI faz (--region vs --zone).
func (p *GCPNodeGroupProvider) listNodePoolsViaAPI(ctx context.Context, token string) ([]gcpNodePool, error) {
	url := fmt.Sprintf(
		"https://container.googleapis.com/v1/projects/%s/locations/%s/clusters/%s/nodePools",
		p.projectID, p.location, p.clusterName,
	)
	reqCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("container API returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var result struct {
		NodePools []gcpNodePool `json:"nodePools"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse node pools response: %w", err)
	}
	return result.NodePools, nil
}

// listNodePoolsViaCLI é o fallback original via `gcloud container node-pools list`.
func (p *GCPNodeGroupProvider) listNodePoolsViaCLI(ctx context.Context) ([]gcpNodePool, error) {
	out, err := p.run(ctx, 60*time.Second,
		"gcloud", "container", "node-pools", "list",
		"--cluster", p.clusterName,
		"--project", p.projectID,
		p.locationFlag(), p.location,
		"--format=json",
	)
	if err != nil {
		return nil, fmt.Errorf("gcloud container node-pools list: %w", err)
	}

	var raw []gcpNodePool
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("parse node pools: %w", err)
	}
	return raw, nil
}

// ScaleNodeGroup define o número de nodes no node pool via gcloud clusters resize.
func (p *GCPNodeGroupProvider) ScaleNodeGroup(ctx context.Context, _, group string, count int) error {
	_, err := p.run(ctx, 10*time.Minute,
		"gcloud", "container", "clusters", "resize", p.clusterName,
		"--node-pool", group,
		"--num-nodes", fmt.Sprintf("%d", count),
		"--project", p.projectID,
		p.locationFlag(), p.location,
		"--quiet",
	)
	if err != nil {
		return fmt.Errorf("gcloud clusters resize node pool %s: %w", group, err)
	}
	return nil
}

// SetAutoscaling habilita ou desabilita o cluster autoscaler no node pool.
func (p *GCPNodeGroupProvider) SetAutoscaling(ctx context.Context, _, group string, enable bool, min, max int) error {
	args := []string{
		"gcloud", "container", "node-pools", "update", group,
		"--cluster", p.clusterName,
		"--project", p.projectID,
		p.locationFlag(), p.location,
		"--quiet",
	}
	if enable {
		args = append(args,
			"--enable-autoscaling",
			"--min-nodes", fmt.Sprintf("%d", min),
			"--max-nodes", fmt.Sprintf("%d", max),
		)
	} else {
		args = append(args, "--no-enable-autoscaling")
	}

	if _, err := p.run(ctx, 5*time.Minute, args...); err != nil {
		return fmt.Errorf("gcloud node-pools update autoscaling %s: %w", group, err)
	}
	return nil
}

// AbortOperation não tem equivalente no gcloud GKE.
func (p *GCPNodeGroupProvider) AbortOperation(_ context.Context, _, _ string) error {
	return cloudprovider.ErrNotSupported
}

// ─── helpers ──────────────────────────────────────────────────────────────────

// run executa um comando com timeout e retorna stdout.
// O primeiro elemento de args é o executável; os demais são os argumentos.
func (p *GCPNodeGroupProvider) run(ctx context.Context, timeout time.Duration, args ...string) ([]byte, error) {
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, args[0], args[1:]...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%s: %w\n%s",
			strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

// locationFlag retorna --region ou --zone dependendo da localização configurada.
func (p *GCPNodeGroupProvider) locationFlag() string {
	if isGKEZone(p.location) {
		return "--zone"
	}
	return "--region"
}

// isGKEZone retorna true se a string for uma zona GCP (termina com -[a-f]).
func isGKEZone(location string) bool {
	if len(location) < 2 {
		return false
	}
	last := location[len(location)-1]
	prev := location[len(location)-2]
	return prev == '-' && last >= 'a' && last <= 'f'
}
