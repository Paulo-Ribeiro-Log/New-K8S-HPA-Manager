package azure

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

const (
	armVMsAPIVersion       = "2024-03-01"
	armResourcesAPIVersion = "2021-04-01"
)

// RGDisk é um Managed Disk de um Resource Group com tudo que a REST expõe e o `az disk list` com
// --query enxuto perdia: estado (Attached/Unattached/Reserved), VM dona (managedBy), tier de
// performance e performance provisionada.
type RGDisk struct {
	ID, Name, Location string
	SKU                string // Standard_LRS | StandardSSD_LRS | Premium_LRS | PremiumV2_LRS | UltraSSD_LRS ...
	SKUTier            string // Standard | Premium
	PerformanceTier    string // properties.tier (P20...) — só quando o disco foi ajustado; vazio = tier do tamanho
	State              string // Attached | Unattached | Reserved | ActiveSAS ...
	ManagedBy          string // resource ID da VM a que o disco está atachado ("" = ninguém)
	SizeGB             float64
	IOPS, MBps         float64 // provisionados (relevantes pra Ultra/Premium SSD v2)
	CreatedAt          string
	LastOwnership      string
}

// AttachedVMName devolve o nome da VM dona do disco ("" se desatachado).
func (d RGDisk) AttachedVMName() string {
	if d.ManagedBy == "" {
		return ""
	}
	return lastSegment(d.ManagedBy)
}

// RGVM é uma VM de um Resource Group.
type RGVM struct {
	ID, Name, Location, VMSize string
}

// RGResource é um recurso ARM genérico (qualquer tipo) de um Resource Group.
type RGResource struct {
	ID, Name, Type, Kind, Location string
	SKUName, SKUTier               string
}

func lastSegment(s string) string {
	if i := strings.LastIndex(s, "/"); i >= 0 {
		return s[i+1:]
	}
	return s
}

func (c *ARMClient) rgURL(rg, providerPath, apiVersion string) string {
	return fmt.Sprintf("%s/subscriptions/%s/resourceGroups/%s/%s?api-version=%s",
		armBaseURL, c.SubscriptionID, url.PathEscape(rg), providerPath, apiVersion)
}

// ListRGDisks lista TODOS os Managed Disks do RG (qualquer estado).
func (c *ARMClient) ListRGDisks(ctx context.Context, rg string) ([]RGDisk, error) {
	var all []RGDisk
	err := c.ListPages(ctx, c.rgURL(rg, "providers/Microsoft.Compute/disks", armDisksAPIVersion), func(body []byte) (string, error) {
		disks, next, err := parseRGDisks(body)
		all = append(all, disks...)
		return next, err
	})
	return all, err
}

func parseRGDisks(body []byte) ([]RGDisk, string, error) {
	var resp struct {
		Value []struct {
			ID        string `json:"id"`
			Name      string `json:"name"`
			Location  string `json:"location"`
			ManagedBy string `json:"managedBy"`
			SKU       struct {
				Name string `json:"name"`
				Tier string `json:"tier"`
			} `json:"sku"`
			Properties struct {
				DiskState               string  `json:"diskState"`
				DiskSizeGB              float64 `json:"diskSizeGB"`
				Tier                    string  `json:"tier"`
				DiskIOPSReadWrite       float64 `json:"diskIOPSReadWrite"`
				DiskMBpsReadWrite       float64 `json:"diskMBpsReadWrite"`
				TimeCreated             string  `json:"timeCreated"`
				LastOwnershipUpdateTime string  `json:"lastOwnershipUpdateTime"`
			} `json:"properties"`
		} `json:"value"`
		NextLink string `json:"nextLink"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, "", fmt.Errorf("parse discos do RG: %w", err)
	}
	out := make([]RGDisk, 0, len(resp.Value))
	for _, v := range resp.Value {
		out = append(out, RGDisk{
			ID: v.ID, Name: v.Name, Location: v.Location,
			SKU: v.SKU.Name, SKUTier: v.SKU.Tier, PerformanceTier: v.Properties.Tier,
			State: v.Properties.DiskState, ManagedBy: v.ManagedBy,
			SizeGB: v.Properties.DiskSizeGB, IOPS: v.Properties.DiskIOPSReadWrite, MBps: v.Properties.DiskMBpsReadWrite,
			CreatedAt: v.Properties.TimeCreated, LastOwnership: v.Properties.LastOwnershipUpdateTime,
		})
	}
	return out, resp.NextLink, nil
}

// ListRGVMs lista as VMs do RG com o vmSize.
func (c *ARMClient) ListRGVMs(ctx context.Context, rg string) ([]RGVM, error) {
	var all []RGVM
	err := c.ListPages(ctx, c.rgURL(rg, "providers/Microsoft.Compute/virtualMachines", armVMsAPIVersion), func(body []byte) (string, error) {
		vms, next, err := parseRGVMs(body)
		all = append(all, vms...)
		return next, err
	})
	return all, err
}

func parseRGVMs(body []byte) ([]RGVM, string, error) {
	var resp struct {
		Value []struct {
			ID         string `json:"id"`
			Name       string `json:"name"`
			Location   string `json:"location"`
			Properties struct {
				HardwareProfile struct {
					VMSize string `json:"vmSize"`
				} `json:"hardwareProfile"`
			} `json:"properties"`
		} `json:"value"`
		NextLink string `json:"nextLink"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, "", fmt.Errorf("parse VMs do RG: %w", err)
	}
	out := make([]RGVM, 0, len(resp.Value))
	for _, v := range resp.Value {
		out = append(out, RGVM{ID: v.ID, Name: v.Name, Location: v.Location, VMSize: v.Properties.HardwareProfile.VMSize})
	}
	return out, resp.NextLink, nil
}

// ListRGResources lista os recursos de QUALQUER tipo do RG (o chamador filtra os que interessam).
func (c *ARMClient) ListRGResources(ctx context.Context, rg string) ([]RGResource, error) {
	var all []RGResource
	err := c.ListPages(ctx, c.rgURL(rg, "resources", armResourcesAPIVersion), func(body []byte) (string, error) {
		res, next, err := parseRGResources(body)
		all = append(all, res...)
		return next, err
	})
	return all, err
}

func parseRGResources(body []byte) ([]RGResource, string, error) {
	var resp struct {
		Value []struct {
			ID       string `json:"id"`
			Name     string `json:"name"`
			Type     string `json:"type"`
			Kind     string `json:"kind"`
			Location string `json:"location"`
			SKU      *struct {
				Name string `json:"name"`
				Tier string `json:"tier"`
			} `json:"sku"`
		} `json:"value"`
		NextLink string `json:"nextLink"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, "", fmt.Errorf("parse recursos do RG: %w", err)
	}
	out := make([]RGResource, 0, len(resp.Value))
	for _, v := range resp.Value {
		r := RGResource{ID: v.ID, Name: v.Name, Type: v.Type, Kind: v.Kind, Location: v.Location}
		if v.SKU != nil {
			r.SKUName, r.SKUTier = v.SKU.Name, v.SKU.Tier
		}
		out = append(out, r)
	}
	return out, resp.NextLink, nil
}

// RGFlexServer é um Azure Database for PostgreSQL/MySQL Flexible Server com o que define o custo
// além do compute: storage provisionado, IOPS e alta disponibilidade. O `az resource list`
// genérico só entrega o SKU de compute — o storage (que num servidor Burstable pequeno costuma
// custar MAIS que o compute) fica de fora.
type RGFlexServer struct {
	ID, Name, Type, Location string
	SKUName, SKUTier         string
	StorageGB                float64
	StorageTier              string // PostgreSQL: P4/P10/P20... (MySQL não tem)
	IOPS                     float64
	AutoGrow                 bool
	HAMode                   string // Disabled | ZoneRedundant | SameZone ("" = desconhecido)
	BackupRetentionDays      int
	GeoRedundantBackup       bool
}

// HAEnabled diz se há réplica standby (que dobra compute e storage na cobrança).
func (s RGFlexServer) HAEnabled() bool {
	return s.HAMode != "" && !strings.EqualFold(s.HAMode, "Disabled")
}

const (
	armPGFlexAPIVersion    = "2023-06-01-preview"
	armMySQLFlexAPIVersion = "2023-12-30"
)

// ListRGFlexServers lista os Flexible Servers (PostgreSQL e MySQL) do RG com suas propriedades.
func (c *ARMClient) ListRGFlexServers(ctx context.Context, rg string) ([]RGFlexServer, error) {
	var all []RGFlexServer
	for _, p := range []struct{ path, version string }{
		{"providers/Microsoft.DBforPostgreSQL/flexibleServers", armPGFlexAPIVersion},
		{"providers/Microsoft.DBforMySQL/flexibleServers", armMySQLFlexAPIVersion},
	} {
		err := c.ListPages(ctx, c.rgURL(rg, p.path, p.version), func(body []byte) (string, error) {
			servers, next, err := parseRGFlexServers(body)
			all = append(all, servers...)
			return next, err
		})
		if err != nil {
			return nil, err
		}
	}
	return all, nil
}

func parseRGFlexServers(body []byte) ([]RGFlexServer, string, error) {
	var resp struct {
		Value []struct {
			ID       string `json:"id"`
			Name     string `json:"name"`
			Type     string `json:"type"`
			Location string `json:"location"`
			SKU      struct {
				Name string `json:"name"`
				Tier string `json:"tier"`
			} `json:"sku"`
			Properties struct {
				Storage struct {
					StorageSizeGB float64 `json:"storageSizeGB"`
					Tier          string  `json:"tier"`
					IOPS          float64 `json:"iops"`
					AutoGrow      string  `json:"autoGrow"`
				} `json:"storage"`
				HighAvailability struct {
					Mode string `json:"mode"`
				} `json:"highAvailability"`
				Backup struct {
					BackupRetentionDays int    `json:"backupRetentionDays"`
					GeoRedundantBackup  string `json:"geoRedundantBackup"`
				} `json:"backup"`
			} `json:"properties"`
		} `json:"value"`
		NextLink string `json:"nextLink"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, "", fmt.Errorf("parse Flexible Servers do RG: %w", err)
	}
	out := make([]RGFlexServer, 0, len(resp.Value))
	for _, v := range resp.Value {
		p := v.Properties
		out = append(out, RGFlexServer{
			ID: v.ID, Name: v.Name, Type: v.Type, Location: v.Location,
			SKUName: v.SKU.Name, SKUTier: v.SKU.Tier,
			StorageGB: p.Storage.StorageSizeGB, StorageTier: p.Storage.Tier, IOPS: p.Storage.IOPS,
			AutoGrow:            strings.EqualFold(p.Storage.AutoGrow, "Enabled"),
			HAMode:              p.HighAvailability.Mode,
			BackupRetentionDays: p.Backup.BackupRetentionDays,
			GeoRedundantBackup:  strings.EqualFold(p.Backup.GeoRedundantBackup, "Enabled"),
		})
	}
	return out, resp.NextLink, nil
}
