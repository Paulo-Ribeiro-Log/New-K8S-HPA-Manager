package azure

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"k8s-hpa-manager/internal/models"
)

const (
	unattachedDisksTimeout = 2 * time.Minute
	armDisksAPIVersion     = "2023-04-02"
)

var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// armDisk cobre os campos usados de Microsoft.Compute/disks (ARM REST). encoding/json casa chaves
// sem diferenciar caixa — importante aqui: a API devolve "LastOwnershipUpdateTime" com L maiúsculo,
// enquanto o `az` CLI normaliza pra minúsculo.
type armDisk struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Location string            `json:"location"`
	Tags     map[string]string `json:"tags"`
	SKU      struct {
		Name string `json:"name"`
	} `json:"sku"`
	Properties struct {
		DiskState               string  `json:"diskState"`
		DiskSizeGB              float64 `json:"diskSizeGB"`
		DiskIOPSReadWrite       float64 `json:"diskIOPSReadWrite"` // provisionado (Ultra / Premium SSD v2)
		DiskMBpsReadWrite       float64 `json:"diskMBpsReadWrite"`
		TimeCreated             string  `json:"timeCreated"`
		LastOwnershipUpdateTime string  `json:"lastOwnershipUpdateTime"`
	} `json:"properties"`
}

// ListUnattachedDisks lista os Managed Disks desatachados (diskState == "Unattached") de uma
// subscription (UUID ou nome).
//
// Usa a ARM REST API em vez de `az disk list`: a partir do Azure CLI 2.x recente esse comando
// EXIGE --resource-group (não lista mais a subscription inteira), e a subscription toda é
// justamente o escopo que interessa — discos órfãos de PVC ficam no node resource group (MC_*) de
// cada cluster. O token vem do `az` já autenticado da máquina (mesmo mecanismo do lookup de grupos
// AAD via Graph). O estado "Reserved" (disco de VM desalocada) NÃO entra: é outro tipo de
// desperdício.
func ListUnattachedDisks(ctx context.Context, subscription string) ([]models.UnattachedDisk, error) {
	if strings.TrimSpace(subscription) == "" {
		return nil, fmt.Errorf("subscription do cluster não identificada — rode o autodiscover (clusters-config.json sem subscription)")
	}
	listCtx, cancel := context.WithTimeout(ctx, unattachedDisksTimeout)
	defer cancel()

	c, err := NewARMClient(listCtx, subscription)
	if err != nil {
		return nil, err
	}
	first := fmt.Sprintf("%s/subscriptions/%s/providers/Microsoft.Compute/disks?api-version=%s", armBaseURL, c.SubscriptionID, armDisksAPIVersion)
	all := make([]models.UnattachedDisk, 0)
	err = c.ListPages(listCtx, first, func(body []byte) (string, error) {
		disks, next, perr := parseARMDisksPage(body)
		all = append(all, disks...)
		return next, perr
	})
	if err != nil {
		return nil, err
	}
	return all, nil
}

// parseARMDisksPage extrai os discos desatachados de UMA página da listagem e o nextLink.
func parseARMDisksPage(body []byte) (disks []models.UnattachedDisk, nextLink string, err error) {
	var resp struct {
		Value    []armDisk `json:"value"`
		NextLink string    `json:"nextLink"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, "", fmt.Errorf("parse ARM disks: %w", err)
	}
	for _, d := range resp.Value {
		if d.Properties.DiskState != "Unattached" {
			continue
		}
		disks = append(disks, armDiskToModel(d))
	}
	return disks, resp.NextLink, nil
}

// Tags que o driver disk.csi.azure.com (e o in-tree azure-disk) grava ao provisionar um PV
// dinamicamente. Azure proíbe "/" em nome de tag, por isso o formato com hífens.
const (
	azureTagPVCName      = "kubernetes.io-created-for-pvc-name"
	azureTagPVCNamespace = "kubernetes.io-created-for-pvc-namespace"
	azureTagPVName       = "kubernetes.io-created-for-pv-name"
)

// resourceGroupFromID extrai o resource group de um resource ID
// (/subscriptions/x/resourceGroups/RG/providers/...), preservando a caixa original.
func resourceGroupFromID(id string) string {
	parts := strings.Split(id, "/")
	for i, p := range parts {
		if strings.EqualFold(p, "resourceGroups") && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

func armDiskToModel(d armDisk) models.UnattachedDisk {
	rg := resourceGroupFromID(d.ID)
	disk := models.UnattachedDisk{
		Provider:        "azure",
		ID:              d.ID,
		Name:            d.Name,
		ResourceGroup:   rg,
		Location:        d.Location,
		SizeGB:          d.Properties.DiskSizeGB,
		ProvisionedIOPS: d.Properties.DiskIOPSReadWrite,
		ProvisionedMBps: d.Properties.DiskMBpsReadWrite,
		DiskType:        d.SKU.Name,
		DiskState:       d.Properties.DiskState,
		CreatedAt:       d.Properties.TimeCreated,
		UnattachedSince: d.Properties.LastOwnershipUpdateTime,
		Tags:            d.Tags,
	}
	for k, v := range d.Tags {
		switch strings.ToLower(k) {
		case azureTagPVCName:
			disk.K8sPVCName = v
		case azureTagPVCNamespace:
			disk.K8sPVCNamespace = v
		case azureTagPVName:
			disk.K8sPVName = v
		}
	}
	// Discos de PVC vivem no node resource group do AKS (MC_<rg>_<cluster>_<região>). O nome do
	// cluster não dá pra extrair com certeza (nome de cluster e RG aceitam "_"), então guardamos o
	// RG inteiro como pista e quem consome procura "_<cluster>_" dentro dele.
	if strings.HasPrefix(strings.ToUpper(rg), "MC_") {
		disk.K8sClusterHint = rg
	}
	return disk
}
