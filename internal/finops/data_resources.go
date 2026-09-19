package finops

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"k8s-hpa-manager/internal/cloudprovider/azure"
)

// dataResourcesListTimeout — mesma convenção já documentada no projeto pra chamadas de leitura
// via Azure CLI (30s, ver CLAUDE.md "Azure CLI — Timeout Obrigatório").
const dataResourcesListTimeout = 60 * time.Second

// DeriveDataResourceGroup deriva o nome do Resource Group de DADOS a partir do RG de APP,
// seguindo a convenção confirmada com o usuário: "rg-<nome>-app-<env>" → "rg-<nome>-data-<env>"
// (troca "-app-" por "-data-", primeira ocorrência). Exemplo real confirmado: app
// "rg-abastecimento-app-hlg" → data "rg-abastecimento-data-hlg".
//
// Deliberadamente NÃO deriva o nome a partir do nome do cluster/context (que teria heurísticas
// de prefixo/sufixo arriscadas, ex: "akspriv-abastecimento-hlg-admin" → "abastecimento" exigiria
// assumir o prefixo "akspriv-" e múltiplos sufixos de ambiente) — parte do RG do APP já
// confirmado e correto (config.ClusterConfig.ResourceGroup, a mesma string já usada pra chamar
// `az aks nodepool`/`az aks show`), então só precisa de UMA substituição de string determinística.
//
// Retorna ok=false quando o RG de app não segue a convenção "-app-" — nesse caso o chamador não
// tenta nada (sem inventar um nome que pode não existir).
func DeriveDataResourceGroup(appResourceGroup string) (dataRG string, ok bool) {
	const marker = "-app-"
	idx := strings.Index(appResourceGroup, marker)
	if idx < 0 {
		return "", false
	}
	return appResourceGroup[:idx] + "-data-" + appResourceGroup[idx+len(marker):], true
}

// AzureDataResource é um recurso de dados (fora do cluster K8s) encontrado no RG de dados —
// pedido explícito do usuário: "o resource group de dados... também impacta custos de cloud...
// estamos analisando sempre apenas os resource groups de app". Cobre qualquer tipo de recurso
// reconhecido (não só os com preço automático) — visibilidade em primeiro lugar, mesmo quando o
// custo ainda não pode ser estimado.
//
// Achado real, validado ao vivo contra um RG de dados de produção desta empresa
// (rg-abastecimento-data-hlg) ANTES desta versão existir: a suposição original — SQL Database/
// Cosmos DB como serviços PaaS — não bateu com a infra real. O RG real continha VMs Linux
// auto-gerenciadas rodando banco (nomes "mdbh-*" — MongoDB self-hosted), os discos managed
// dessas VMs (data disk + OS disk), e Azure Database for PostgreSQL **Flexible Server** (tipo
// ARM "Microsoft.DBforPostgreSQL/flexibleServers", nunca "Microsoft.Sql/*"). A versão anterior
// desta feature não reconhecia NENHUM desses tipos — resultado: "0 recursos encontrados" mesmo
// com um RG cheio de infra de dados de verdade. Corrigido cobrindo os tipos reais encontrados.
type AzureDataResource struct {
	Name     string  `json:"name"`
	Type     string  `json:"type"` // tipo ARM completo, ex: "Microsoft.Compute/virtualMachines"
	Kind     string  `json:"kind,omitempty"`
	SKUName  string  `json:"sku_name,omitempty"` // VM: vmSize (ex: "Standard_D2s_v4"); disco: sku (ex: "Standard_LRS")
	SKUTier  string  `json:"sku_tier,omitempty"`
	SizeGB   float64 `json:"size_gb,omitempty"` // só discos managed
	Location string  `json:"location,omitempty"`

	// MonthlyCostUSD/BRL — só preenchido quando o modelo de cobrança do tipo é confiável de
	// estimar a partir só do SKU — ver data_resources_pricing.go. Zero + PricingNote preenchido
	// quando não há estimativa (nunca um valor inventado).
	MonthlyCostUSD float64 `json:"monthly_cost_usd,omitempty"`
	MonthlyCostBRL float64 `json:"monthly_cost_brl,omitempty"`
	PricingNote    string  `json:"pricing_note,omitempty"`
	PriceSource    string  `json:"price_source,omitempty"` // "api" (preço fixo por SKU) ou "estimated" (aproximação a partir de uso real — ver F3.1)

	// ── Inventário de disco/VM (ARM REST) ──────────────────────────────────────────────────
	// DiskTier é o tier COBRADO do disco (P20/E10/S6): properties.tier quando o disco foi ajustado,
	// senão o tier que cobre o tamanho. DiskState: Attached | Unattached | Reserved (Reserved =
	// atachado a uma VM DESALOCADA — o disco segue cobrando). AttachedTo é a VM dona.
	DiskTier        string  `json:"disk_tier,omitempty"`
	DiskState       string  `json:"disk_state,omitempty"`
	AttachedTo      string  `json:"attached_to,omitempty"`
	ProvisionedIOPS float64 `json:"provisioned_iops,omitempty"`
	ProvisionedMBps float64 `json:"provisioned_mbps,omitempty"`
	UnattachedSince string  `json:"unattached_since,omitempty"`
	// ── Flexible Server (PostgreSQL/MySQL): o que define o custo além do compute ───────────────
	// StorageGB/StorageTier/HAMode vêm do ARM (o `resource list` genérico só entrega o SKU de
	// compute). Num servidor Burstable pequeno o storage provisionado custa MAIS que o compute.
	// HAMode != Disabled dobra compute e storage na cobrança. IOPS provisionado fica em
	// ProvisionedIOPS. Compute*/Storage* decompõem MonthlyCost* pra tela mostrar de onde vem o valor.
	StorageGB      float64 `json:"storage_gb,omitempty"`
	StorageTier    string  `json:"storage_tier,omitempty"`
	HAMode         string  `json:"ha_mode,omitempty"`
	ComputeCostUSD float64 `json:"compute_cost_usd,omitempty"`
	ComputeCostBRL float64 `json:"compute_cost_brl,omitempty"`
	StorageCostUSD float64 `json:"storage_cost_usd,omitempty"`
	StorageCostBRL float64 `json:"storage_cost_brl,omitempty"`

	// PowerState só é preenchido pra VM quando dá pra afirmar: "deallocated" (deduzido de um disco
	// dela em estado Reserved). VM ligada/parada-não-desalocada fica vazio — o ARM só entrega o
	// power state via instanceView por VM, chamada que esta listagem evita.
	PowerState string `json:"power_state,omitempty"`

	// Utilization/Recommendations — uso real (Azure Monitor) e ofertas de resizing. Recommendations
	// de inventário (disco desatachado, VM desalocada...) vêm sempre; as baseadas em métricas só
	// quando a análise de uso é pedida (ver AnalyzeDataResourceUsage).
	Utilization     *DataUtilization     `json:"utilization,omitempty"`
	Recommendations []DataRecommendation `json:"recommendations,omitempty"`

	// ResourceID — o Resource ID ARM completo, capturado só do `az resource list` genérico (nunca
	// exposto na API — uso interno de pricing). F3.1 (FINOPS-IMPROVEMENTS-PLAN.md): precificar
	// Storage Account exige consultar a métrica UsedCapacity (Azure Monitor) pra ESTE recurso
	// específico, que precisa do ID completo (`az monitor metrics list --resource <id>`) — mais
	// confiável que reconstruir o ID à mão a partir de subscription+rg+type+name.
	ResourceID string `json:"-"`
}

// azCLIResource é o shape genérico devolvido por `az resource list` — cobre qualquer tipo de
// recurso ARM sem precisar de subcomando dedicado, mas NÃO inclui `properties` (ex: vmSize de
// uma VM, diskSizeGB de um disco) — por isso VMs e discos são listados separadamente via `az vm
// list`/`az disk list` (ver listVirtualMachines/listManagedDisks abaixo), que devolvem esses
// campos de forma direta e com contrato de saída estável.
type azCLIResource struct {
	Id       string `json:"id"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Kind     string `json:"kind"`
	Location string `json:"location"`
	Sku      *struct {
		Name string `json:"name"`
		Tier string `json:"tier"`
	} `json:"sku"`
}

// dataResourceTypeAllowlist são os tipos ARM reconhecidos como "recurso de dados" pra esta
// feature (via `az resource list` genérico — VMs e discos managed são tratados à parte, ver
// listVirtualMachines/listManagedDisks). Cobre os PaaS de banco relacional confirmados ao vivo
// (SQL, PostgreSQL, MySQL, MariaDB — nem todos usados por toda empresa, mas nenhum custa nada
// listar), Storage/Redis/Cosmos/ServiceBus/EventHub (pedido original do usuário). Qualquer OUTRO
// tipo encontrado no RG (private endpoints, key vaults, availability sets, NICs, DNS zones) é
// deliberadamente omitido — não são "recursos de dados" no sentido pedido, listá-los só
// adicionaria ruído de itens sempre-R$0.
var dataResourceTypeAllowlist = map[string]bool{
	"microsoft.sql/servers":                     true,
	"microsoft.sql/servers/databases":           true,
	"microsoft.dbforpostgresql/flexibleservers": true,
	"microsoft.dbforpostgresql/servers":         true,
	"microsoft.dbformysql/flexibleservers":      true,
	"microsoft.dbformysql/servers":              true,
	"microsoft.dbformariadb/servers":            true,
	"microsoft.storage/storageaccounts":         true,
	"microsoft.cache/redis":                     true,
	"microsoft.documentdb/databaseaccounts":     true,
	"microsoft.servicebus/namespaces":           true,
	"microsoft.eventhub/namespaces":             true,
}

// ListDataResourceGroup lista TODOS os recursos de dados de um Resource Group via ARM REST: os
// recursos genéricos (allowlist), as VMs e os Managed Disks — 3 listagens independentes em
// PARALELO com o mesmo token. Os discos vêm com estado/VM dona/tier/performance provisionada (o
// `az disk list` enxuto perdia tudo isso), o que permite marcar VM desalocada e disco desatachado.
func ListDataResourceGroup(ctx context.Context, resourceGroup, subscription string) ([]AzureDataResource, error) {
	ctx, cancel := context.WithTimeout(ctx, dataResourcesListTimeout)
	defer cancel()

	c, err := azure.NewARMClient(ctx, subscription)
	if err != nil {
		return nil, err
	}
	return ListDataResourceGroupWith(ctx, c, resourceGroup)
}

// ListDataResourceGroupWith é ListDataResourceGroup com um ARMClient já autenticado (o handler
// reaproveita o mesmo cliente pra métricas na análise de uso).
func ListDataResourceGroupWith(ctx context.Context, c *azure.ARMClient, resourceGroup string) ([]AzureDataResource, error) {
	var (
		generic []azure.RGResource
		vms     []azure.RGVM
		disks   []azure.RGDisk
		flex    []azure.RGFlexServer
		errs    [4]error
	)
	var wg sync.WaitGroup
	wg.Add(4)
	go func() { defer wg.Done(); flex, errs[3] = c.ListRGFlexServers(ctx, resourceGroup) }()
	go func() { defer wg.Done(); generic, errs[0] = c.ListRGResources(ctx, resourceGroup) }()
	go func() { defer wg.Done(); vms, errs[1] = c.ListRGVMs(ctx, resourceGroup) }()
	go func() { defer wg.Done(); disks, errs[2] = c.ListRGDisks(ctx, resourceGroup) }()
	wg.Wait()

	// As 3 chamadas miram o MESMO Resource Group: se uma falha (RG inexistente, permissão) as outras
	// tendem a falhar pelo mesmo motivo — propaga o 1º erro, nunca mascara.
	for i, name := range []string{"resources", "virtualMachines", "disks", "flexibleServers"} {
		if errs[i] != nil {
			return nil, fmt.Errorf("ARM %s (rg=%s): %w", name, resourceGroup, errs[i])
		}
	}

	resources := buildDataResources(generic, vms, disks)
	mergeFlexServers(resources, flex)
	log.Info().Str("resource_group", resourceGroup).Int("relevant", len(resources)).
		Int("vms", len(vms)).Int("disks", len(disks)).
		Msg("FinOps/DataResources: RG de dados listado (ARM)")
	return resources, nil
}

// buildDataResources monta a lista final (genéricos da allowlist + VMs + discos) e cruza VM↔disco:
// disco Reserved ⇒ a VM dona está desalocada.
func buildDataResources(generic []azure.RGResource, vms []azure.RGVM, disks []azure.RGDisk) []AzureDataResource {
	raw := make([]azCLIResource, 0, len(generic))
	for _, g := range generic {
		r := azCLIResource{Id: g.ID, Name: g.Name, Type: g.Type, Kind: g.Kind, Location: g.Location}
		if g.SKUName != "" || g.SKUTier != "" {
			r.Sku = &struct {
				Name string `json:"name"`
				Tier string `json:"tier"`
			}{Name: g.SKUName, Tier: g.SKUTier}
		}
		raw = append(raw, r)
	}
	out := filterToAllowlist(raw)

	deallocated := map[string]bool{}
	for _, d := range disks {
		if strings.EqualFold(d.State, "Reserved") && d.AttachedVMName() != "" {
			deallocated[strings.ToLower(d.AttachedVMName())] = true
		}
	}

	for _, v := range vms {
		res := AzureDataResource{
			Name: v.Name, Type: "Microsoft.Compute/virtualMachines",
			SKUName: v.VMSize, Location: v.Location, ResourceID: v.ID,
		}
		if deallocated[strings.ToLower(v.Name)] {
			res.PowerState = "deallocated"
		}
		out = append(out, res)
	}

	for _, d := range disks {
		out = append(out, AzureDataResource{
			Name: d.Name, Type: "Microsoft.Compute/disks",
			SKUName: d.SKU, SKUTier: d.SKUTier, SizeGB: d.SizeGB, Location: d.Location, ResourceID: d.ID,
			DiskTier:        resolveDiskTier(d.SKU, d.SizeGB, d.PerformanceTier),
			DiskState:       d.State,
			AttachedTo:      d.AttachedVMName(),
			ProvisionedIOPS: d.IOPS,
			ProvisionedMBps: d.MBps,
			UnattachedSince: d.LastOwnership,
		})
	}
	return out
}

// mergeFlexServers copia storage/IOPS/HA dos Flexible Servers pra o recurso correspondente da
// lista genérica (mesmo resource ID, sem diferenciar caixa — o ARM devolve o RG em caixas
// diferentes conforme o endpoint).
func mergeFlexServers(resources []AzureDataResource, flex []azure.RGFlexServer) {
	byID := make(map[string]azure.RGFlexServer, len(flex))
	for _, f := range flex {
		byID[strings.ToLower(f.ID)] = f
	}
	for i := range resources {
		f, ok := byID[strings.ToLower(resources[i].ResourceID)]
		if !ok {
			continue
		}
		resources[i].StorageGB = f.StorageGB
		resources[i].StorageTier = f.StorageTier
		resources[i].ProvisionedIOPS = f.IOPS
		resources[i].HAMode = f.HAMode
	}
}

// resolveDiskTier devolve o tier cobrado de um disco managed (P20/E10/S6): o tier explícito
// (properties.tier — disco com performance ajustada) ou o que cobre o tamanho. Vazio pra SKU sem
// tier de capacidade (Ultra, Premium SSD v2 — capacidade/IOPS/throughput são provisionados à parte).
func resolveDiskTier(sku string, sizeGB float64, perfTier string) string {
	if perfTier != "" {
		return perfTier
	}
	if !isKnownAzureSKU(sku) || sizeGB <= 0 {
		return ""
	}
	azType, _ := MapStorageClassToAzureType("", "", sku)
	if _, managed := managedDiskTiers[azType]; !managed {
		return ""
	}
	return ResolveManagedDiskTier(azType, sizeGB)
}

func runAzJSON(ctx context.Context, args []string, out interface{}) error {
	raw, err := exec.CommandContext(ctx, "az", args...).Output()
	if err != nil {
		// Inclui o stderr real do az CLI (ex: "ResourceGroupNotFound", erro de auth) — sem isso
		// o erro genérico do exec ("exit status 3") não diz nada acionável sobre a causa real.
		if exitErr, ok := err.(*exec.ExitError); ok && len(exitErr.Stderr) > 0 {
			return fmt.Errorf("%s", strings.TrimSpace(string(exitErr.Stderr)))
		}
		return err
	}
	return json.Unmarshal(raw, out)
}

// resourceGroupFromID extrai o Resource Group de um Resource ID ARM completo
// (".../resourceGroups/<rg>/providers/...") — usado pra reconstruir chamadas `az` que exigem
// --resource-group separado do nome (ex: `az storage account show`), já que AzureDataResource só
// guarda o ID completo (ResourceID), nunca o RG isolado.
func resourceGroupFromID(resourceID string) string {
	parts := strings.Split(resourceID, "/")
	for i, p := range parts {
		if strings.EqualFold(p, "resourceGroups") && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

// storageAccountMetricsTimeout — mesma convenção de timeout de leitura documentada no CLAUDE.md
// (Azure CLI — Timeout Obrigatório), só que menor que dataResourcesListTimeout: aqui é sempre UM
// recurso/UMA métrica por chamada, não uma listagem paginada de um RG inteiro.
const storageAccountMetricsTimeout = 20 * time.Second

// getStorageAccountAccessTier busca o access tier (Hot/Cool/Cold/Premium) de uma Storage Account —
// achado real (F3.1 do FINOPS-IMPROVEMENTS-PLAN.md), confirmado ao vivo: o `az resource list`
// genérico usado por listGenericResources NÃO captura esse campo (só expõe sku.name/sku.tier, não
// properties.accessTier) — precisa de uma chamada dedicada.
func getStorageAccountAccessTier(ctx context.Context, resourceGroup, name, subscription string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, storageAccountMetricsTimeout)
	defer cancel()
	args := []string{
		"storage", "account", "show",
		"--name", name,
		"--resource-group", resourceGroup,
		"--query", "accessTier",
		"-o", "tsv",
	}
	if subscription != "" {
		args = append(args, "--subscription", subscription)
	}
	raw, err := exec.CommandContext(ctx, "az", args...).Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && len(exitErr.Stderr) > 0 {
			return "", fmt.Errorf("%s", strings.TrimSpace(string(exitErr.Stderr)))
		}
		return "", err
	}
	return strings.TrimSpace(string(raw)), nil
}

// azMetricsListResponse é o shape reduzido de `az monitor metrics list` — só o necessário pra
// extrair os pontos de dado de uma métrica (ex: UsedCapacity de Storage Account).
type azMetricsListResponse struct {
	Value []struct {
		Timeseries []struct {
			Data []struct {
				Average *float64 `json:"average"`
			} `json:"data"`
		} `json:"timeseries"`
	} `json:"value"`
}

// getResourceMetricAverage busca a média de uma métrica do Azure Monitor pra um recurso, numa
// janela de 48h/intervalo de 1h — validado ao vivo contra uma Storage Account real (métrica
// UsedCapacity, F3.1 do FINOPS-IMPROVEMENTS-PLAN.md). Usa a MÉDIA de todos os pontos não-nulos da
// janela (não só o último ponto) — uso de armazenamento varia pouco hora a hora, mais estável e
// menos sujeito a um ponto isolado ausente/zerado. ok=false quando a métrica não tem nenhum dado
// no período (conta nova, sem uso ainda, ou recurso sem essa métrica) — nunca inventa um valor.
func getResourceMetricAverage(ctx context.Context, resourceID, metricName string) (value float64, ok bool, err error) {
	ctx, cancel := context.WithTimeout(ctx, storageAccountMetricsTimeout)
	defer cancel()
	now := time.Now().UTC()
	start := now.Add(-48 * time.Hour)
	args := []string{
		"monitor", "metrics", "list",
		"--resource", resourceID,
		"--metric", metricName,
		"--aggregation", "Average",
		"--interval", "PT1H",
		"--start-time", start.Format(time.RFC3339),
		"--end-time", now.Format(time.RFC3339),
		"-o", "json",
	}
	var resp azMetricsListResponse
	if err := runAzJSON(ctx, args, &resp); err != nil {
		return 0, false, err
	}
	var sum float64
	var count int
	for _, v := range resp.Value {
		for _, ts := range v.Timeseries {
			for _, d := range ts.Data {
				if d.Average != nil {
					sum += *d.Average
					count++
				}
			}
		}
	}
	if count == 0 {
		return 0, false, nil
	}
	return sum / float64(count), true, nil
}

// filterToAllowlist converte a saída crua do `az resource list` genérico na lista de recursos de
// dados relevantes — extraída pra ser testável sem precisar de `az` real.
func filterToAllowlist(raw []azCLIResource) []AzureDataResource {
	resources := make([]AzureDataResource, 0, len(raw))
	for _, r := range raw {
		if !dataResourceTypeAllowlist[strings.ToLower(r.Type)] {
			continue
		}
		res := AzureDataResource{
			Name:       r.Name,
			Type:       r.Type,
			Kind:       r.Kind,
			Location:   r.Location,
			ResourceID: r.Id,
		}
		if r.Sku != nil {
			res.SKUName = r.Sku.Name
			res.SKUTier = r.Sku.Tier
		}
		resources = append(resources, res)
	}
	return resources
}
