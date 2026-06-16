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

// GCPNodeGroupProvider implementa cloudprovider.NodeGroupProvider usando gcloud CLI.
type GCPNodeGroupProvider struct {
	clusterName string
	projectID   string
	region      string // região (us-central1) ou zona (us-central1-a)
}

// NewGCPNodeGroupProvider cria um provider GCP para um cluster GKE.
func NewGCPNodeGroupProvider(clusterName, projectID, region string) *GCPNodeGroupProvider {
	return &GCPNodeGroupProvider{
		clusterName: clusterName,
		projectID:   projectID,
		region:      region,
	}
}

// ValidateAuth verifica se o gcloud CLI está instalado e autenticado.
func (p *GCPNodeGroupProvider) ValidateAuth(_ context.Context) error {
	if _, err := exec.LookPath("gcloud"); err != nil {
		return fmt.Errorf("gcloud CLI não encontrado — instale o Google Cloud SDK")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "gcloud", "auth", "list",
		"--filter=status:ACTIVE",
		"--format=json",
	).Output()
	if err != nil {
		return fmt.Errorf("gcloud auth list falhou: %w", err)
	}
	var accounts []struct {
		Account string `json:"account"`
	}
	if json.Unmarshal(out, &accounts) != nil || len(accounts) == 0 {
		return fmt.Errorf("nenhuma conta GCP ativa — execute: gcloud auth login")
	}
	return nil
}

// ListNodeGroups lista os node pools do cluster via gcloud CLI.
func (p *GCPNodeGroupProvider) ListNodeGroups(ctx context.Context, _ string) ([]models.NodePool, error) {
	cmdCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	args := p.baseArgs("container", "node-pools", "list",
		"--cluster", p.clusterName,
		"--format=json",
	)
	out, err := exec.CommandContext(cmdCtx, "gcloud", args...).Output()
	if err != nil {
		return nil, fmt.Errorf("gcloud container node-pools list: %w — cluster=%s project=%s region=%s",
			err, p.clusterName, p.projectID, p.region)
	}

	var raw []struct {
		Name              string `json:"name"`
		Config            struct {
			MachineType string `json:"machineType"`
		} `json:"config"`
		InitialNodeCount int32 `json:"initialNodeCount"`
		Autoscaling      struct {
			Enabled      bool  `json:"enabled"`
			MinNodeCount int32 `json:"minNodeCount"`
			MaxNodeCount int32 `json:"maxNodeCount"`
		} `json:"autoscaling"`
		Status    string `json:"status"`
		Management struct {
			AutoUpgrade bool `json:"autoUpgrade"`
		} `json:"management"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("parse node pools: %w", err)
	}

	pools := make([]models.NodePool, 0, len(raw))
	for _, np := range raw {
		nodeCount := np.InitialNodeCount
		pools = append(pools, models.NodePool{
			Name:               np.Name,
			VMSize:             np.Config.MachineType,
			NodeCount:          nodeCount,
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

// ScaleNodeGroup define o número de nodes em um node pool.
func (p *GCPNodeGroupProvider) ScaleNodeGroup(ctx context.Context, _, group string, count int) error {
	cmdCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	args := p.baseArgs("container", "clusters", "resize", p.clusterName,
		"--node-pool", group,
		"--num-nodes", fmt.Sprintf("%d", count),
		"--quiet",
	)
	out, err := exec.CommandContext(cmdCtx, "gcloud", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("gcloud clusters resize: %w — %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// SetAutoscaling habilita ou desabilita o cluster autoscaler no node pool.
func (p *GCPNodeGroupProvider) SetAutoscaling(ctx context.Context, _, group string, enable bool, min, max int) error {
	cmdCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	var args []string
	if enable {
		args = p.baseArgs("container", "node-pools", "update", group,
			"--cluster", p.clusterName,
			"--enable-autoscaling",
			"--min-nodes", fmt.Sprintf("%d", min),
			"--max-nodes", fmt.Sprintf("%d", max),
			"--quiet",
		)
	} else {
		args = p.baseArgs("container", "node-pools", "update", group,
			"--cluster", p.clusterName,
			"--no-enable-autoscaling",
			"--quiet",
		)
	}

	out, err := exec.CommandContext(cmdCtx, "gcloud", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("gcloud node-pools update autoscaling: %w — %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// AbortOperation não é suportado no GKE (não há equivalente ao az aks nodepool operation-abort).
func (p *GCPNodeGroupProvider) AbortOperation(_ context.Context, _, _ string) error {
	return cloudprovider.ErrNotSupported
}

// baseArgs monta os args base para gcloud com --project e --region/--zone.
func (p *GCPNodeGroupProvider) baseArgs(subcmd ...string) []string {
	args := subcmd
	if p.projectID != "" {
		args = append(args, "--project", p.projectID)
	}
	if p.region != "" {
		// Zona (ex: us-central1-a) vs Região (ex: us-central1)
		if isGKEZone(p.region) {
			args = append(args, "--zone", p.region)
		} else {
			args = append(args, "--region", p.region)
		}
	}
	return args
}

// isGKEZone retorna true se a string parecer uma zona GCP (termina com -[a-f]).
func isGKEZone(location string) bool {
	if len(location) < 2 {
		return false
	}
	last := location[len(location)-1]
	secondLast := location[len(location)-2]
	return secondLast == '-' && last >= 'a' && last <= 'f'
}
