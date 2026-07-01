package gcp

import (
	"context"
	"encoding/json"
	"fmt"
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

// ValidateAuth verifica se gcloud está instalado, com conta ativa, e garante
// que o gke-gcloud-auth-plugin está presente e USE_GKE_GCLOUD_AUTH_PLUGIN=True definido.
func (p *GCPNodeGroupProvider) ValidateAuth(ctx context.Context) error {
	if _, err := exec.LookPath("gcloud"); err != nil {
		return fmt.Errorf("gcloud CLI não encontrado — instale o Google Cloud SDK")
	}

	if err := EnsureGKEAuthPlugin(nil); err != nil {
		return err
	}

	return IsGcloudAuthActive(ctx)
}

// ListNodeGroups lista os node pools do cluster via gcloud CLI.
func (p *GCPNodeGroupProvider) ListNodeGroups(ctx context.Context, _ string) ([]models.NodePool, error) {
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

	var raw []struct {
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
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("parse node pools: %w", err)
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
