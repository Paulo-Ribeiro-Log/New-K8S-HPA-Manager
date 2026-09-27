package azure

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"k8s-hpa-manager/internal/models"
)

// Recursos órfãos por jornada (FinOps → Recursos órfãos), via Resource Graph:
//  1. ResolveJourneyResourceGroups — RGs do escopo: com a tag "jornada", RG de app e node RG
//     (MC_*) de cada cluster da jornada, e o RG de dados rg-<nome>-data-<env> (em qualquer
//     subscription, só os que existem).
//  2. ListOrphanResources — recursos desatachados/sem conexões nesses RGs, por tipo.
//  3. LastChanges — última alteração de cada recurso (tabela resourcechanges, ~14 dias), base do
//     critério "órfão há 7 dias ou mais" para os tipos em que o Azure não registra a data.

// JourneyCluster é um cluster AKS do escopo (de clusters-config.json).
type JourneyCluster struct {
	Name             string
	Journey          string
	AppResourceGroup string
	DataRGCandidates []string // finops.DataRGCandidates — só os que existem entram no escopo
}

// journeyTagExpr lê a tag "jornada" tolerando variação de caixa na chave; envTagExpr, a tag de
// ambiente do RG (nomes mais comuns).
const (
	journeyTagExpr = `tostring(coalesce(tags['jornada'], tags['Jornada'], tags['JORNADA']))`
	envTagExpr     = `tostring(coalesce(tags['ambiente'], tags['Ambiente'], tags['AMBIENTE'], tags['environment'], tags['Environment'], tags['env'], tags['Env'], tags['ENV']))`
)

// ResolveJourneyResourceGroups devolve os RGs do escopo. journeys vazio = todas as jornadas (RGs
// com qualquer valor na tag, mais os RGs de todos os clusters informados).
func ResolveJourneyResourceGroups(ctx context.Context, c *ARMClient, subscriptions, journeys []string, clusters []JourneyCluster) ([]models.ScopedResourceGroup, error) {
	type namedRG struct{ journey, source, cluster string }
	named := map[string]namedRG{}
	clusterByName := map[string]JourneyCluster{}
	var clusterNames, namedList []string
	for _, cl := range clusters {
		clusterByName[strings.ToLower(cl.Name)] = cl
		clusterNames = append(clusterNames, cl.Name)
		if cl.AppResourceGroup != "" {
			named[strings.ToLower(cl.AppResourceGroup)] = namedRG{cl.Journey, "cluster", cl.Name}
		}
		for _, rg := range cl.DataRGCandidates {
			if _, ok := named[strings.ToLower(rg)]; !ok {
				named[strings.ToLower(rg)] = namedRG{cl.Journey, "data", cl.Name}
			}
		}
	}
	for rg := range named {
		namedList = append(namedList, rg)
	}
	sort.Strings(namedList)

	journeyCond := "isnotempty(journey)"
	if len(journeys) > 0 {
		lower := make([]string, len(journeys))
		for i, j := range journeys {
			lower[i] = strings.ToLower(j)
		}
		journeyCond = "tolower(journey) in " + kqlList(lower)
	}

	query := `resourcecontainers
| where type =~ 'microsoft.resources/subscriptions/resourcegroups'
| extend journey = ` + journeyTagExpr + `, envTag = ` + envTagExpr + `
| where ` + journeyCond + ` or name in~ ` + kqlList(namedList) + `
| project subscriptionId, resourceGroup = name, journey, envTag, clusterName = '', source = 'rg'
| union (resources
  | where type =~ 'microsoft.containerservice/managedclusters' and name in~ ` + kqlList(clusterNames) + `
  | project subscriptionId, resourceGroup = tostring(properties.nodeResourceGroup), journey = '', envTag = '', clusterName = name, source = 'node')`

	rows, err := c.ResourceGraphQuery(ctx, subscriptions, query)
	if err != nil {
		return nil, err
	}

	seen := map[string]int{}
	var out []models.ScopedResourceGroup
	for _, raw := range rows {
		var r struct {
			SubscriptionID string `json:"subscriptionId"`
			ResourceGroup  string `json:"resourceGroup"`
			Journey        string `json:"journey"`
			EnvTag         string `json:"envTag"`
			ClusterName    string `json:"clusterName"`
			Source         string `json:"source"`
		}
		if json.Unmarshal(raw, &r) != nil || r.ResourceGroup == "" {
			continue
		}
		rg := models.ScopedResourceGroup{SubscriptionID: r.SubscriptionID, ResourceGroup: r.ResourceGroup, Journey: r.Journey, EnvTag: r.EnvTag, Source: "tag"}
		if r.Source == "node" {
			cl := clusterByName[strings.ToLower(r.ClusterName)]
			rg.Source, rg.Cluster, rg.Journey = "node", cl.Name, cl.Journey
		} else if n, ok := named[strings.ToLower(r.ResourceGroup)]; ok {
			rg.Source, rg.Cluster = n.source, n.cluster
			if n.journey != "" {
				rg.Journey = n.journey
			}
		}
		key := strings.ToLower(rg.SubscriptionID + "/" + rg.ResourceGroup)
		if i, dup := seen[key]; dup {
			// Mesmo RG por dois caminhos (ex: RG de app que também tem a tag): fica a origem mais
			// específica (cluster/node/data) e a jornada que houver.
			if out[i].Source == "tag" && rg.Source != "tag" {
				out[i].Source, out[i].Cluster = rg.Source, rg.Cluster
			}
			if out[i].Journey == "" {
				out[i].Journey = rg.Journey
			}
			continue
		}
		seen[key] = len(out)
		out = append(out, rg)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Journey != out[j].Journey {
			return out[i].Journey < out[j].Journey
		}
		return strings.ToLower(out[i].ResourceGroup) < strings.ToLower(out[j].ResourceGroup)
	})
	return out, nil
}

// orphanRules: condição KQL (sobre t = tolower(type) e properties) e motivo exibido, por tipo.
// Discos entram aqui também, mas saem de ListOrphanResources separados (relatório de discos).
var orphanRules = []struct{ cond, reason string }{
	{`t == 'microsoft.compute/disks' and tostring(properties.diskState) =~ 'Unattached'`, "Disco não atachado a nenhuma VM"},
	{`t == 'microsoft.compute/virtualmachines' and tostring(properties.extended.instanceView.powerState.code) =~ 'PowerState/deallocated'`, "VM desalocada — os discos continuam sendo cobrados"},
	{`t == 'microsoft.compute/virtualmachines' and tostring(properties.extended.instanceView.powerState.code) =~ 'PowerState/stopped'`, "VM parada sem desalocar — o compute continua sendo cobrado"},
	{`t == 'microsoft.network/networkinterfaces' and isnull(properties.virtualMachine) and isnull(properties.privateEndpoint) and isnull(properties.privateLinkService)`, "NIC sem VM nem Private Endpoint"},
	{`t == 'microsoft.network/publicipaddresses' and isnull(properties.ipConfiguration) and isnull(properties.natGateway)`, "IP público sem associação"},
	{`t == 'microsoft.network/privateendpoints' and tostring(array_concat(coalesce(properties.privateLinkServiceConnections, dynamic([])), coalesce(properties.manualPrivateLinkServiceConnections, dynamic([])))) matches regex @'"status":\s*"(Disconnected|Rejected)"'`, "Private Endpoint com conexão desconectada ou rejeitada (destino removido?)"},
	{`t == 'microsoft.network/networksecuritygroups' and array_length(coalesce(properties.subnets, dynamic([]))) == 0 and array_length(coalesce(properties.networkInterfaces, dynamic([]))) == 0`, "NSG sem subnet nem NIC"},
	{`t == 'microsoft.network/loadbalancers' and not(tostring(properties.backendAddressPools) matches regex @'"(backendIPConfigurations|loadBalancerBackendAddresses)":\s*\[\s*\{')`, "Load Balancer sem backend"},
	{`t == 'microsoft.network/applicationgateways' and not(tostring(properties.backendAddressPools) matches regex @'"(backendAddresses|backendIPConfigurations)":\s*\[\s*\{')`, "Application Gateway sem backend"},
	{`t == 'microsoft.network/natgateways' and array_length(coalesce(properties.subnets, dynamic([]))) == 0`, "NAT Gateway sem subnet"},
	{`t == 'microsoft.network/routetables' and array_length(coalesce(properties.subnets, dynamic([]))) == 0`, "Route table sem subnet"},
	{`t == 'microsoft.network/privatednszones' and toint(properties.numberOfVirtualNetworkLinks) == 0`, "Private DNS zone sem vínculo com VNet"},
	{`t == 'microsoft.web/serverfarms' and toint(properties.numberOfSites) == 0`, "App Service Plan sem apps"},
	{`t == 'microsoft.compute/availabilitysets' and array_length(coalesce(properties.virtualMachines, dynamic([]))) == 0`, "Availability Set sem VMs"},
}

// orphanQuery monta a consulta de órfãos restrita às chaves "subscription/rg" (minúsculas).
func orphanQuery(rgKeys []string) string {
	caseArgs := make([]string, 0, len(orphanRules)*2+1)
	for _, r := range orphanRules {
		caseArgs = append(caseArgs, r.cond, "'"+strings.ReplaceAll(r.reason, "'", `\'`)+"'")
	}
	caseArgs = append(caseArgs, "''")
	return `resources
| extend rgKey = tolower(strcat(subscriptionId, '/', resourceGroup))
| where rgKey in ` + kqlList(rgKeys) + `
| extend t = tolower(type)
| extend reason = case(
    ` + strings.Join(caseArgs, ",\n    ") + `)
| where reason != ''
| project id, name, type = t, kind, location, resourceGroup, subscriptionId, tags, sku, reason,
    properties = iff(t == 'microsoft.compute/disks', properties, dynamic(null)),
    vmSize = tostring(properties.hardwareProfile.vmSize)`
}

// ListOrphanResources lista os órfãos dos RGs do escopo: discos (no formato do relatório de discos,
// com pista de cluster/PVC) e os demais tipos. Jornada vem do RG.
func ListOrphanResources(ctx context.Context, c *ARMClient, subscriptions []string, rgs []models.ScopedResourceGroup) ([]models.UnattachedDisk, []models.OrphanResource, error) {
	if len(rgs) == 0 {
		return nil, nil, nil
	}
	byKey := map[string]models.ScopedResourceGroup{}
	keys := make([]string, 0, len(rgs))
	for _, rg := range rgs {
		k := strings.ToLower(rg.SubscriptionID + "/" + rg.ResourceGroup)
		byKey[k] = rg
		keys = append(keys, k)
	}
	rows, err := c.ResourceGraphQuery(ctx, subscriptions, orphanQuery(keys))
	if err != nil {
		return nil, nil, err
	}

	var disks []models.UnattachedDisk
	var others []models.OrphanResource
	for _, raw := range rows {
		var r struct {
			ID             string            `json:"id"`
			Name           string            `json:"name"`
			Type           string            `json:"type"`
			Kind           string            `json:"kind"`
			Location       string            `json:"location"`
			ResourceGroup  string            `json:"resourceGroup"`
			SubscriptionID string            `json:"subscriptionId"`
			Tags           map[string]string `json:"tags"`
			SKU            *struct {
				Name string `json:"name"`
				Tier string `json:"tier"`
			} `json:"sku"`
			Reason string `json:"reason"`
			VMSize string `json:"vmSize"`
		}
		if json.Unmarshal(raw, &r) != nil {
			continue
		}
		rg := byKey[strings.ToLower(r.SubscriptionID+"/"+r.ResourceGroup)]

		if r.Type == "microsoft.compute/disks" {
			// A linha do Resource Graph tem o mesmo formato do ARM (id, name, sku, properties...).
			var d armDisk
			if json.Unmarshal(raw, &d) != nil {
				continue
			}
			disk := armDiskToModel(d)
			disk.SubscriptionID, disk.Journey = r.SubscriptionID, rg.Journey
			disks = append(disks, disk)
			continue
		}

		o := models.OrphanResource{
			ID: r.ID, Name: r.Name, Type: r.Type, Kind: r.Kind, Location: r.Location,
			ResourceGroup: r.ResourceGroup, SubscriptionID: r.SubscriptionID, Journey: rg.Journey,
			Reason: r.Reason, Tags: r.Tags,
		}
		switch {
		case r.VMSize != "":
			o.SKU = r.VMSize
		case r.SKU != nil:
			o.SKU = strings.TrimSpace(r.SKU.Name + " " + r.SKU.Tier)
		}
		others = append(others, o)
	}
	return disks, others, nil
}

// lastChangesChunk limita quantos IDs vão em cada consulta a resourcechanges (tamanho da query).
const lastChangesChunk = 150

// LastChanges devolve a data da última alteração registrada (resourcechanges, ~14 dias de
// retenção) de cada recurso, por ID em minúsculas. Recurso ausente = nenhuma alteração na janela.
func LastChanges(ctx context.Context, c *ARMClient, subscriptions, ids []string) (map[string]time.Time, error) {
	out := map[string]time.Time{}
	for start := 0; start < len(ids); start += lastChangesChunk {
		chunk := ids[start:min(start+lastChangesChunk, len(ids))]
		lower := make([]string, len(chunk))
		for i, id := range chunk {
			lower[i] = strings.ToLower(id)
		}
		query := `resourcechanges
| extend targetId = tolower(tostring(properties.targetResourceId)), changedAt = todatetime(properties.changeAttributes.timestamp)
| where targetId in ` + kqlList(lower) + `
| summarize lastChange = max(changedAt) by targetId`
		rows, err := c.ResourceGraphQuery(ctx, subscriptions, query)
		if err != nil {
			return nil, err
		}
		for _, raw := range rows {
			var r struct {
				TargetID   string `json:"targetId"`
				LastChange string `json:"lastChange"`
			}
			if json.Unmarshal(raw, &r) != nil {
				continue
			}
			if t, err := time.Parse(time.RFC3339Nano, r.LastChange); err == nil {
				out[r.TargetID] = t
			}
		}
	}
	return out, nil
}

// OrphanDeleteCommand devolve o comando de exclusão sugerido (a app nunca exclui nada).
func OrphanDeleteCommand(o models.OrphanResource) string {
	return fmt.Sprintf("az resource delete --ids %s", o.ID)
}
