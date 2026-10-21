package azure

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

const aksManagedClusterAPIVersion = "2024-05-01"

// AKSAdminGroupIDs devolve os Object IDs dos grupos do Entra ID que são administradores do
// cluster (aadProfile.adminGroupObjectIDs — o admin_group_object_ids do Terraform). Cluster sem
// integração com o Entra ID devolve lista vazia. Só leitura.
func (c *ARMClient) AKSAdminGroupIDs(ctx context.Context, subscriptionID, resourceGroup, cluster string) ([]string, error) {
	endpoint := fmt.Sprintf("%s/subscriptions/%s/resourceGroups/%s/providers/Microsoft.ContainerService/managedClusters/%s?api-version=%s",
		armBaseURL, url.PathEscape(subscriptionID), url.PathEscape(resourceGroup), url.PathEscape(cluster), aksManagedClusterAPIVersion)
	body, err := c.Get(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	return parseAKSAdminGroupIDs(body)
}

func parseAKSAdminGroupIDs(body []byte) ([]string, error) {
	var mc struct {
		Properties struct {
			AADProfile *struct {
				AdminGroupObjectIDs []string `json:"adminGroupObjectIDs"`
			} `json:"aadProfile"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(body, &mc); err != nil {
		return nil, fmt.Errorf("resposta do AKS inválida: %w", err)
	}
	ids := []string{}
	if mc.Properties.AADProfile == nil {
		return ids, nil
	}
	for _, id := range mc.Properties.AADProfile.AdminGroupObjectIDs {
		if id = strings.ToLower(strings.TrimSpace(id)); id != "" {
			ids = append(ids, id)
		}
	}
	return ids, nil
}
