package gcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"k8s-hpa-manager/internal/cloudprovider"
	"k8s-hpa-manager/internal/models"
)

const gkeAPIBase = "https://container.googleapis.com/v1"

// GCPNodeGroupProvider implementa cloudprovider.NodeGroupProvider usando a GKE REST API.
// Não depende do gcloud CLI — usa o ADC (Application Default Credentials) da própria app,
// gravado pelo Device Auth Grant em AI Settings → Gemini.
type GCPNodeGroupProvider struct {
	clusterName string
	projectID   string
	location    string // região (us-central1) ou zona (us-central1-a)
}

// NewGCPNodeGroupProvider cria um provider GCP para um cluster GKE.
func NewGCPNodeGroupProvider(clusterName, projectID, location string) *GCPNodeGroupProvider {
	return &GCPNodeGroupProvider{
		clusterName: clusterName,
		projectID:   projectID,
		location:    location,
	}
}

// ValidateAuth verifica se há credenciais GCP válidas (ADC acessível e token renovável).
func (p *GCPNodeGroupProvider) ValidateAuth(ctx context.Context) error {
	_, err := getGCPAccessToken(ctx)
	return err
}

// ListNodeGroups lista os node pools do cluster via GKE REST API.
func (p *GCPNodeGroupProvider) ListNodeGroups(ctx context.Context, _ string) ([]models.NodePool, error) {
	token, err := getGCPAccessToken(ctx)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/projects/%s/locations/%s/clusters/%s/nodePools",
		gkeAPIBase, p.projectID, p.location, p.clusterName)

	var resp struct {
		NodePools []gkeNodePool `json:"nodePools"`
	}
	if err := p.get(ctx, token, url, &resp); err != nil {
		return nil, fmt.Errorf("GKE list node pools: %w", err)
	}

	pools := make([]models.NodePool, 0, len(resp.NodePools))
	for _, np := range resp.NodePools {
		pools = append(pools, np.toModel(p.clusterName))
	}
	return pools, nil
}

// ScaleNodeGroup define o número desejado de nodes em um node pool.
func (p *GCPNodeGroupProvider) ScaleNodeGroup(ctx context.Context, _, group string, count int) error {
	token, err := getGCPAccessToken(ctx)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("%s/projects/%s/locations/%s/clusters/%s/nodePools/%s/setSize",
		gkeAPIBase, p.projectID, p.location, p.clusterName, group)

	body := map[string]any{"nodeCount": count}

	var op gkeOperation
	if err := p.post(ctx, token, url, body, &op); err != nil {
		return fmt.Errorf("GKE resize node pool %s: %w", group, err)
	}

	return p.waitOperation(ctx, token, op.Name, 10*time.Minute)
}

// SetAutoscaling habilita ou desabilita o cluster autoscaler no node pool.
func (p *GCPNodeGroupProvider) SetAutoscaling(ctx context.Context, _, group string, enable bool, min, max int) error {
	token, err := getGCPAccessToken(ctx)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("%s/projects/%s/locations/%s/clusters/%s/nodePools/%s/setAutoscaling",
		gkeAPIBase, p.projectID, p.location, p.clusterName, group)

	body := map[string]any{
		"autoscaling": map[string]any{
			"enabled":      enable,
			"minNodeCount": min,
			"maxNodeCount": max,
		},
	}

	var op gkeOperation
	if err := p.post(ctx, token, url, body, &op); err != nil {
		return fmt.Errorf("GKE set autoscaling node pool %s: %w", group, err)
	}

	return p.waitOperation(ctx, token, op.Name, 5*time.Minute)
}

// AbortOperation não é suportado na GKE REST API pública.
func (p *GCPNodeGroupProvider) AbortOperation(_ context.Context, _, _ string) error {
	return cloudprovider.ErrNotSupported
}

// ─── helpers HTTP ─────────────────────────────────────────────────────────────

func (p *GCPNodeGroupProvider) get(ctx context.Context, token, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Goog-User-Project", p.projectID)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("HTTP GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	return p.decodeResponse(resp, out)
}

func (p *GCPNodeGroupProvider) post(ctx context.Context, token, url string, body any, out any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Goog-User-Project", p.projectID)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("HTTP POST %s: %w", url, err)
	}
	defer resp.Body.Close()

	return p.decodeResponse(resp, out)
}

func (p *GCPNodeGroupProvider) decodeResponse(resp *http.Response, out any) error {
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("erro ao ler resposta: %w", err)
	}

	if resp.StatusCode >= 400 {
		var apiErr struct {
			Error struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		json.Unmarshal(bodyBytes, &apiErr) //nolint:errcheck
		msg := apiErr.Error.Message
		if msg == "" {
			msg = string(bodyBytes)
		}
		return fmt.Errorf("GKE API erro %d: %s", resp.StatusCode, msg)
	}

	return json.Unmarshal(bodyBytes, out)
}

// waitOperation aguarda a conclusão de uma operação GKE com polling a cada 5s.
func (p *GCPNodeGroupProvider) waitOperation(ctx context.Context, token, operationName string, timeout time.Duration) error {
	if operationName == "" {
		return nil
	}

	// operationName vem no formato "projects/P/locations/L/operations/ID"
	// ou apenas "operation-XXXX" dependendo da versão da API
	opURL := operationName
	if !strings.HasPrefix(operationName, "http") {
		if strings.HasPrefix(operationName, "projects/") {
			opURL = fmt.Sprintf("%s/%s", gkeAPIBase, operationName)
		} else {
			opURL = fmt.Sprintf("%s/projects/%s/locations/%s/operations/%s",
				gkeAPIBase, p.projectID, p.location, operationName)
		}
	}

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		var op gkeOperation
		if err := p.get(ctx, token, opURL, &op); err != nil {
			return fmt.Errorf("erro ao verificar operação: %w", err)
		}

		switch op.Status {
		case "DONE":
			if op.StatusMessage != "" {
				return fmt.Errorf("operação concluída com erro: %s", op.StatusMessage)
			}
			return nil
		case "ABORTING":
			return fmt.Errorf("operação abortada: %s", op.StatusMessage)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}

	return fmt.Errorf("timeout aguardando operação GKE (%s)", timeout)
}

// ─── tipos da GKE API ─────────────────────────────────────────────────────────

type gkeNodePool struct {
	Name        string `json:"name"`
	Config      struct {
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

func (np gkeNodePool) toModel(clusterName string) models.NodePool {
	return models.NodePool{
		Name:               np.Name,
		VMSize:             np.Config.MachineType,
		NodeCount:          np.InitialNodeCount,
		MinNodeCount:       np.Autoscaling.MinNodeCount,
		MaxNodeCount:       np.Autoscaling.MaxNodeCount,
		AutoscalingEnabled: np.Autoscaling.Enabled,
		Status:             strings.ToLower(np.Status),
		ClusterName:        clusterName,
		IsSystemPool:       np.Name == "default-pool",
	}
}

type gkeOperation struct {
	Name          string `json:"name"`
	Status        string `json:"status"` // RUNNING, DONE, ABORTING
	StatusMessage string `json:"statusMessage"`
}
