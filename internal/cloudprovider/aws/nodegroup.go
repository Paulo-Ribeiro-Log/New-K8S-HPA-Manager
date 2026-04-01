package aws

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"k8s-hpa-manager/internal/cloudprovider"
	"k8s-hpa-manager/internal/models"
)

// AWSNodeGroupProvider implementa cloudprovider.NodeGroupProvider usando AWS CLI.
type AWSNodeGroupProvider struct {
	clusterName string // nome curto para AWS CLI (ex: "asaplog-preprod")
	contextName string // context do kubeconfig (ARN completo, para K8s API)
	region      string
	profile     string
}

// NewAWSNodeGroupProvider cria um provider AWS para um cluster EKS.
// Normaliza automaticamente ARNs (arn:aws:eks:REGION:ACCOUNT:cluster/NAME → NAME).
// Se a região não for fornecida mas puder ser extraída do ARN, usa a do ARN.
func NewAWSNodeGroupProvider(clusterName, region, profile string) *AWSNodeGroupProvider {
	name, arnRegion := parseEKSClusterName(clusterName)
	if region == "" && arnRegion != "" {
		region = arnRegion
	}
	return &AWSNodeGroupProvider{
		clusterName: name,
		contextName: clusterName, // preserva o ARN original como context
		region:      region,
		profile:     profile,
	}
}

// parseEKSClusterName extrai o nome curto e a região de um context EKS.
// Aceita ARN (arn:aws:eks:REGION:ACCOUNT:cluster/NAME) ou nome simples.
// Retorna (shortName, region).
func parseEKSClusterName(input string) (string, string) {
	if !strings.HasPrefix(input, "arn:aws:eks:") {
		return input, ""
	}
	// arn:aws:eks:REGION:ACCOUNT:cluster/NAME
	parts := strings.Split(input, ":")
	arnRegion := ""
	if len(parts) >= 4 {
		arnRegion = parts[3]
	}
	// Pegar a parte após "cluster/"
	if idx := strings.LastIndex(input, "/"); idx >= 0 {
		return input[idx+1:], arnRegion
	}
	return input, arnRegion
}

// args base para todos os comandos AWS CLI
func (p *AWSNodeGroupProvider) baseArgs(subcmd ...string) []string {
	args := append([]string{}, subcmd...)
	if p.region != "" {
		args = append(args, "--region", p.region)
	}
	if p.profile != "" {
		args = append(args, "--profile", p.profile)
	}
	args = append(args, "--output", "json")
	return args
}

func (p *AWSNodeGroupProvider) run(ctx context.Context, args []string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "aws", args...)
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("%w: %s", err, string(exitErr.Stderr))
		}
		return nil, err
	}
	return out, nil
}

// ValidateAuth retorna ErrNotSupported para EKS — autenticação é gerenciada
// transparentemente pelo exec plugin no kubeconfig (aws eks get-token).
// O erro real de auth surfacing via ListNodeGroups quando aws CLI falha.
func (p *AWSNodeGroupProvider) ValidateAuth(_ context.Context) error {
	return cloudprovider.ErrNotSupported
}

// --- structs para parse da resposta da AWS CLI ---

type awsListNodegroupsResponse struct {
	Nodegroups []string `json:"nodegroups"`
}

type awsDescribeNodegroupResponse struct {
	Nodegroup awsNodegroup `json:"nodegroup"`
}

type awsNodegroup struct {
	NodegroupName string            `json:"nodegroupName"`
	Status        string            `json:"status"`
	ScalingConfig awsScalingConfig  `json:"scalingConfig"`
	InstanceTypes []string          `json:"instanceTypes"`
	CapacityType  string            `json:"capacityType"` // ON_DEMAND | SPOT
	Labels        map[string]string `json:"labels"`
}

type awsScalingConfig struct {
	MinSize     int32 `json:"minSize"`
	MaxSize     int32 `json:"maxSize"`
	DesiredSize int32 `json:"desiredSize"`
}

// ListNodeGroups lista os node groups do cluster EKS.
func (p *AWSNodeGroupProvider) ListNodeGroups(ctx context.Context, _ string) ([]models.NodePool, error) {
	listCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// 1. Listar nomes dos node groups
	listArgs := p.baseArgs("eks", "list-nodegroups", "--cluster-name", p.clusterName)
	out, err := p.run(listCtx, listArgs)
	if err != nil {
		return nil, classifyAWSError(err, p.profile)
	}

	var listResp awsListNodegroupsResponse
	if err := json.Unmarshal(out, &listResp); err != nil {
		return nil, fmt.Errorf("failed to parse EKS node groups list: %w", err)
	}

	if len(listResp.Nodegroups) == 0 {
		return []models.NodePool{}, nil
	}

	// 2. Descrever cada node group em paralelo
	type result struct {
		pool models.NodePool
		err  error
	}
	results := make([]result, len(listResp.Nodegroups))
	var wg sync.WaitGroup

	for i, ngName := range listResp.Nodegroups {
		wg.Add(1)
		go func(idx int, name string) {
			defer wg.Done()
			descCtx, descCancel := context.WithTimeout(ctx, 20*time.Second)
			defer descCancel()

			descArgs := p.baseArgs("eks", "describe-nodegroup",
				"--cluster-name", p.clusterName,
				"--nodegroup-name", name)
			descOut, descErr := p.run(descCtx, descArgs)
			if descErr != nil {
				results[idx] = result{err: fmt.Errorf("describe-nodegroup %s: %w", name, descErr)}
				return
			}

			var descResp awsDescribeNodegroupResponse
			if jsonErr := json.Unmarshal(descOut, &descResp); jsonErr != nil {
				results[idx] = result{err: fmt.Errorf("parse describe-nodegroup %s: %w", name, jsonErr)}
				return
			}

			ng := descResp.Nodegroup
			instanceType := ""
			if len(ng.InstanceTypes) > 0 {
				instanceType = ng.InstanceTypes[0]
			}

			// Autoscaling considerado ativo se min != max
			autoscaling := ng.ScalingConfig.MinSize != ng.ScalingConfig.MaxSize

			// Capacity type como parte do status legível
			status := ng.Status
			if ng.CapacityType == "SPOT" {
				status = status + " (SPOT)"
			}

			results[idx] = result{
				pool: models.NodePool{
					Name:               ng.NodegroupName,
					VMSize:             instanceType,
					NodeCount:          ng.ScalingConfig.DesiredSize,
					MinNodeCount:       ng.ScalingConfig.MinSize,
					MaxNodeCount:       ng.ScalingConfig.MaxSize,
					AutoscalingEnabled: autoscaling,
					Status:             status,
					IsSystemPool:       false, // EKS não tem conceito de system pool
					ClusterName:        p.contextName, // ARN completo para K8s API (resolveContext)
				},
			}
		}(i, ngName)
	}

	wg.Wait()

	pools := make([]models.NodePool, 0, len(results))
	for _, r := range results {
		if r.err != nil {
			// Log do erro mas continua com os outros pools
			fmt.Printf("[AWSNodeGroupProvider] Warning: %v\n", r.err)
			continue
		}
		pools = append(pools, r.pool)
	}

	return pools, nil
}

// ScaleNodeGroup define o desiredSize do node group.
func (p *AWSNodeGroupProvider) ScaleNodeGroup(ctx context.Context, _, group string, count int) error {
	scaleCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	scalingConfig := fmt.Sprintf("desiredSize=%d", count)
	args := p.baseArgs("eks", "update-nodegroup-config",
		"--cluster-name", p.clusterName,
		"--nodegroup-name", group,
		"--scaling-config", scalingConfig)

	if _, err := p.run(scaleCtx, args); err != nil {
		return fmt.Errorf("failed to scale EKS node group %s: %w", group, err)
	}
	return nil
}

// SetAutoscaling define min/max do node group.
// Para EKS, min/max são configurados diretamente no node group independente do enable flag.
func (p *AWSNodeGroupProvider) SetAutoscaling(ctx context.Context, _, group string, _ bool, min, max int) error {
	autoCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	scalingConfig := fmt.Sprintf("minSize=%d,maxSize=%d", min, max)
	args := p.baseArgs("eks", "update-nodegroup-config",
		"--cluster-name", p.clusterName,
		"--nodegroup-name", group,
		"--scaling-config", scalingConfig)

	if _, err := p.run(autoCtx, args); err != nil {
		// Tentar formato alternativo (alguns perfis AWS CLI usam espaço)
		if strings.Contains(err.Error(), "scaling-config") {
			altArgs := p.baseArgs("eks", "update-nodegroup-config",
				"--cluster-name", p.clusterName,
				"--nodegroup-name", group,
				"--scaling-config", fmt.Sprintf("minSize=%d", min),
				"--scaling-config", fmt.Sprintf("maxSize=%d", max))
			if _, altErr := p.run(autoCtx, altArgs); altErr != nil {
				return fmt.Errorf("failed to set autoscaling for EKS node group %s: %w", group, err)
			}
			return nil
		}
		return fmt.Errorf("failed to set autoscaling for EKS node group %s: %w", group, err)
	}
	return nil
}

// AbortOperation não é suportado no EKS.
func (p *AWSNodeGroupProvider) AbortOperation(_ context.Context, _, _ string) error {
	return cloudprovider.ErrNotSupported
}

// classifyAWSError transforma erros brutos do AWS CLI em mensagens acionáveis.
func classifyAWSError(err error, profile string) error {
	msg := err.Error()
	profileHint := ""
	if profile != "" {
		profileHint = fmt.Sprintf(" --profile %s", profile)
	}
	switch {
	case strings.Contains(msg, "Partial credentials") || strings.Contains(msg, "aws_secret_access_key"):
		return fmt.Errorf("credenciais AWS incompletas (SSO expirado?). Execute: aws sso login%s", profileHint)
	case strings.Contains(msg, "ExpiredToken") || strings.Contains(msg, "expired"):
		return fmt.Errorf("token AWS expirado. Execute: aws sso login%s", profileHint)
	case strings.Contains(msg, "NoCredentialProviders") || strings.Contains(msg, "no credentials"):
		return fmt.Errorf("sem credenciais AWS configuradas para o perfil '%s'. Execute: aws sso login%s", profile, profileHint)
	case strings.Contains(msg, "executable file not found") || strings.Contains(msg, "aws: command not found"):
		return fmt.Errorf("AWS CLI não encontrado no PATH do servidor. Instale: https://aws.amazon.com/cli/")
	case strings.Contains(msg, "cluster not found") || strings.Contains(msg, "ResourceNotFoundException"):
		return fmt.Errorf("cluster '%s' não encontrado na AWS. Verifique se a VPN AWS está ativa", "")
	default:
		return fmt.Errorf("falha ao listar EKS node groups: %w", err)
	}
}

// compile-time check
var _ cloudprovider.NodeGroupProvider = (*AWSNodeGroupProvider)(nil)
