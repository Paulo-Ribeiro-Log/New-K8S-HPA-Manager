package azure

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"k8s-hpa-manager/internal/cloudprovider"
	"k8s-hpa-manager/internal/models"
	"k8s-hpa-manager/internal/web/validators"
)

// AzureNodeGroupProvider implementa cloudprovider.NodeGroupProvider usando Azure CLI.
type AzureNodeGroupProvider struct {
	clusterName   string // sem sufixo -admin
	resourceGroup string
	subscription  string
}

// NewAzureNodeGroupProvider cria um provider Azure para um cluster específico.
func NewAzureNodeGroupProvider(clusterName, resourceGroup, subscription string) *AzureNodeGroupProvider {
	return &AzureNodeGroupProvider{
		clusterName:   strings.TrimSuffix(clusterName, "-admin"),
		resourceGroup: resourceGroup,
		subscription:  subscription,
	}
}

// ValidateAuth verifica se o Azure CLI está autenticado.
func (p *AzureNodeGroupProvider) ValidateAuth(_ context.Context) error {
	return validators.ValidateAzureAuth()
}

// ListNodeGroups lista os node pools do cluster via Azure CLI.
func (p *AzureNodeGroupProvider) ListNodeGroups(ctx context.Context, _ string) ([]models.NodePool, error) {
	if err := p.setSubscription(ctx); err != nil {
		return nil, err
	}

	listCtx, listCancel := context.WithTimeout(ctx, 60*time.Second)
	defer listCancel()

	cmd := exec.CommandContext(listCtx, "az", "aks", "nodepool", "list",
		"--resource-group", p.resourceGroup,
		"--cluster-name", p.clusterName,
		"--output", "json")

	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			stderr := string(exitErr.Stderr)
			if strings.Contains(stderr, "AADSTS") ||
				strings.Contains(stderr, "expired") ||
				strings.Contains(stderr, "authentication") ||
				strings.Contains(stderr, "az login") {
				return nil, fmt.Errorf("Azure CLI não autenticado. Execute no servidor: az login")
			}
			return nil, fmt.Errorf("az aks nodepool list falhou: %s", stderr)
		}
		return nil, fmt.Errorf("falha ao executar az: %w", err)
	}

	var azPools []azureNodePool
	if err := json.Unmarshal(output, &azPools); err != nil {
		return nil, fmt.Errorf("falha ao parsear saída do Azure CLI: %w", err)
	}

	// Buscar metadados em paralelo (tags e subscription info)
	clusterTags, err := p.getClusterTags(ctx)
	if err != nil {
		log.Warn().Err(err).Str("cluster", p.clusterName).Msg("Falha ao buscar tags do cluster, continuando sem tags")
		clusterTags = make(map[string]string)
	}
	subscriptionName := p.getSubscriptionName(ctx)
	subscriptionUUID := p.getSubscriptionUUID(ctx)

	var pools []models.NodePool
	for _, az := range azPools {
		var minCount, maxCount int32
		if az.MinCount != nil {
			minCount = *az.MinCount
		}
		if az.MaxCount != nil {
			maxCount = *az.MaxCount
		}
		pools = append(pools, models.NodePool{
			Name:               az.Name,
			VMSize:             az.VmSize,
			NodeCount:          az.Count,
			MinNodeCount:       minCount,
			MaxNodeCount:       maxCount,
			AutoscalingEnabled: az.EnableAutoScaling,
			Status:             az.ProvisioningState,
			IsSystemPool:       az.Mode == "System",
			ClusterName:        p.clusterName,
			ResourceGroup:      p.resourceGroup,
			Subscription:       p.subscription,
			SubscriptionName:   subscriptionName,
			SubscriptionUUID:   subscriptionUUID,
			ClusterTags:        clusterTags,
		})
	}

	return pools, nil
}

// ScaleNodeGroup escala um node pool para o número de nodes desejado.
// Desabilita autoscaling antes de escalar se necessário.
func (p *AzureNodeGroupProvider) ScaleNodeGroup(ctx context.Context, _, group string, count int) error {
	if err := p.setSubscription(ctx); err != nil {
		return err
	}

	scaleCtx, scaleCancel := context.WithTimeout(ctx, 10*time.Minute)
	defer scaleCancel()

	out, err := exec.CommandContext(scaleCtx,
		"az", "aks", "nodepool", "scale",
		"--resource-group", p.resourceGroup,
		"--cluster-name", p.clusterName,
		"--name", group,
		"--node-count", fmt.Sprintf("%d", count),
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("az aks nodepool scale falhou: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// SetAutoscaling habilita ou desabilita o cluster autoscaler com min/max.
// Ao habilitar, tenta --update-cluster-autoscaler primeiro (autoscaling já ativo);
// se falhar, faz fallback para --enable-cluster-autoscaler (primeira habilitação).
func (p *AzureNodeGroupProvider) SetAutoscaling(ctx context.Context, _, group string, enable bool, min, max int) error {
	if err := p.setSubscription(ctx); err != nil {
		return err
	}

	opCtx, opCancel := context.WithTimeout(ctx, 10*time.Minute)
	defer opCancel()

	if enable {
		updateArgs := []string{
			"az", "aks", "nodepool", "update",
			"--resource-group", p.resourceGroup,
			"--cluster-name", p.clusterName,
			"--name", group,
			"--update-cluster-autoscaler",
			"--min-count", fmt.Sprintf("%d", min),
			"--max-count", fmt.Sprintf("%d", max),
		}
		if out, err := exec.CommandContext(opCtx, updateArgs[0], updateArgs[1:]...).CombinedOutput(); err == nil {
			return nil
		} else {
			log.Debug().Str("output", strings.TrimSpace(string(out))).Msg("--update-cluster-autoscaler falhou, tentando --enable-cluster-autoscaler")
		}
		enableArgs := []string{
			"az", "aks", "nodepool", "update",
			"--resource-group", p.resourceGroup,
			"--cluster-name", p.clusterName,
			"--name", group,
			"--enable-cluster-autoscaler",
			"--min-count", fmt.Sprintf("%d", min),
			"--max-count", fmt.Sprintf("%d", max),
		}
		out, err := exec.CommandContext(opCtx, enableArgs[0], enableArgs[1:]...).CombinedOutput()
		if err != nil {
			return fmt.Errorf("az aks nodepool update falhou: %s", strings.TrimSpace(string(out)))
		}
		return nil
	}

	disableArgs := []string{
		"az", "aks", "nodepool", "update",
		"--resource-group", p.resourceGroup,
		"--cluster-name", p.clusterName,
		"--name", group,
		"--disable-cluster-autoscaler",
	}
	out, err := exec.CommandContext(opCtx, disableArgs[0], disableArgs[1:]...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("az aks nodepool update falhou: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// AbortOperation cancela uma operação em andamento via Azure ARM abort API.
func (p *AzureNodeGroupProvider) AbortOperation(ctx context.Context, _, group string) error {
	if err := p.setSubscription(ctx); err != nil {
		return err
	}

	abortCtx, abortCancel := context.WithTimeout(ctx, 60*time.Second)
	defer abortCancel()

	out, err := exec.CommandContext(abortCtx,
		"az", "aks", "nodepool", "operation-abort",
		"--resource-group", p.resourceGroup,
		"--cluster-name", p.clusterName,
		"--name", group,
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("az aks nodepool operation-abort falhou: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// --- helpers internos ---

func (p *AzureNodeGroupProvider) setSubscription(ctx context.Context) error {
	subCtx, subCancel := context.WithTimeout(ctx, 30*time.Second)
	defer subCancel()
	out, err := exec.CommandContext(subCtx, "az", "account", "set", "--subscription", p.subscription).CombinedOutput()
	if err != nil {
		return fmt.Errorf("falha ao configurar subscription Azure: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

func (p *AzureNodeGroupProvider) getClusterTags(ctx context.Context) (map[string]string, error) {
	tagCtx, tagCancel := context.WithTimeout(ctx, 30*time.Second)
	defer tagCancel()

	out, err := exec.CommandContext(tagCtx,
		"az", "aks", "show",
		"--resource-group", p.resourceGroup,
		"--name", p.clusterName,
		"--query", "tags",
		"--output", "json",
	).Output()
	if err != nil {
		return nil, fmt.Errorf("az aks show falhou")
	}

	var tags map[string]string
	if err := json.Unmarshal(out, &tags); err != nil {
		return nil, fmt.Errorf("falha ao parsear tags: %w", err)
	}
	if tags == nil {
		tags = make(map[string]string)
	}
	return tags, nil
}

func (p *AzureNodeGroupProvider) getSubscriptionName(ctx context.Context) string {
	nameCtx, nameCancel := context.WithTimeout(ctx, 30*time.Second)
	defer nameCancel()

	out, err := exec.CommandContext(nameCtx,
		"az", "account", "show",
		"--subscription", p.subscription,
		"--query", "name",
		"--output", "tsv",
	).Output()
	if err != nil {
		log.Warn().Err(err).Str("subscription", p.subscription).Msg("Falha ao buscar nome da subscription")
		return ""
	}
	return strings.TrimSpace(string(out))
}

func (p *AzureNodeGroupProvider) getSubscriptionUUID(ctx context.Context) string {
	uuidCtx, uuidCancel := context.WithTimeout(ctx, 30*time.Second)
	defer uuidCancel()

	out, err := exec.CommandContext(uuidCtx,
		"az", "account", "show",
		"--subscription", p.subscription,
		"--query", "id",
		"--output", "tsv",
	).Output()
	if err != nil {
		log.Warn().Err(err).Str("subscription", p.subscription).Msg("Falha ao buscar UUID da subscription")
		return ""
	}
	return strings.TrimSpace(string(out))
}

// azureNodePool representa a estrutura retornada pela Azure CLI.
type azureNodePool struct {
	Name              string `json:"name"`
	VmSize            string `json:"vmSize"`
	Count             int32  `json:"count"`
	MinCount          *int32 `json:"minCount"`
	MaxCount          *int32 `json:"maxCount"`
	EnableAutoScaling bool   `json:"enableAutoScaling"`
	Mode              string `json:"mode"`
	ProvisioningState string `json:"provisioningState"`
}

// compile-time check
var _ cloudprovider.NodeGroupProvider = (*AzureNodeGroupProvider)(nil)
