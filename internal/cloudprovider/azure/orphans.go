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

// Recursos órfãos por jornada (FinOps → Recursos órfãos), via Resource Graph. A busca começa pela
// tag "jornada" — só as jornadas do seletor do cabeçalho — e só depois vai atrás dos recursos:
//  1. ResolveJourneyResourceGroups — RGs com a tag numa das jornadas + node RG (MC_*) dos clusters
//     dessas jornadas (o node RG é exclusivo do cluster; não costuma ter a tag).
//  2. ListOrphanResources — órfãos nesses RGs OU com a própria tag numa das jornadas (em qualquer
//     RG, ex: rg-<nome>-data-<env> sem tag com recursos tagueados); tag de outra jornada é
//     descartada já na consulta.
//  3. LastChanges — última alteração de cada recurso (tabela resourcechanges, ~14 dias), base do
//     critério "órfão há 7 dias ou mais" para os tipos em que o Azure não registra a data.

// JourneyCluster é um cluster AKS do escopo (de clusters-config.json).
type JourneyCluster struct {
	Name    string
	Journey string
}

// journeyTagExpr lê a tag "jornada" tolerando variação de caixa na chave; envTagExpr, a tag de
// ambiente (nomes mais comuns).
const (
	journeyTagExpr = `tostring(coalesce(tags['jornada'], tags['Jornada'], tags['JORNADA']))`
	envTagExpr     = `tostring(coalesce(tags['ambiente'], tags['Ambiente'], tags['AMBIENTE'], tags['environment'], tags['Environment'], tags['env'], tags['Env'], tags['ENV']))`
)

func lowerAll(in []string) []string {
	out := make([]string, len(in))
	for i, v := range in {
		out[i] = strings.ToLower(strings.TrimSpace(v))
	}
	return out
}

// ResolveJourneyResourceGroups devolve os RGs do escopo: com a tag "jornada" numa das journeys e
// o node RG de cada cluster informado (já filtrados por jornada/ambiente pelo chamador).
func ResolveJourneyResourceGroups(ctx context.Context, c *ARMClient, subscriptions, journeys []string, clusters []JourneyCluster) ([]models.ScopedResourceGroup, error) {
	if len(journeys) == 0 {
		return nil, fmt.Errorf("nenhuma jornada para buscar")
	}
	clusterByName := map[string]JourneyCluster{}
	var clusterNames []string
	for _, cl := range clusters {
		clusterByName[strings.ToLower(cl.Name)] = cl
		clusterNames = append(clusterNames, cl.Name)
	}

	query := `resourcecontainers
| where type =~ 'microsoft.resources/subscriptions/resourcegroups'
| extend journey = ` + journeyTagExpr + `
| where tolower(journey) in ` + kqlList(lowerAll(journeys)) + `
| extend envTag = ` + envTagExpr + `
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
		rg := models.ScopedResourceGroup{SubscriptionID: r.SubscriptionID, ResourceGroup: r.ResourceGroup, Journey: r.Journey, JourneyTag: r.Journey, EnvTag: r.EnvTag, Source: "tag"}
		if r.Source == "node" {
			cl := clusterByName[strings.ToLower(r.ClusterName)]
			rg.Source, rg.Cluster, rg.Journey = "node", cl.Name, cl.Journey
		}
		key := strings.ToLower(rg.SubscriptionID + "/" + rg.ResourceGroup)
		if i, dup := seen[key]; dup {
			// Node RG que também tem a tag: fica como node (mais específico), com a tag registrada.
			if out[i].Source == "tag" && rg.Source == "node" {
				out[i].Source, out[i].Cluster = rg.Source, rg.Cluster
			}
			if out[i].JourneyTag == "" {
				out[i].JourneyTag = rg.JourneyTag
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

// orphanQuery monta a consulta de órfãos: recursos dos RGs do escopo (chaves "subscription/rg" em
// minúsculas) OU com a própria tag "jornada" numa das journeys — neste caso, só no ambiente
// envRegex (sobre a tag de ambiente do recurso ou o nome do RG; "" = qualquer ambiente). Recurso
// com tag de outra jornada sai já aqui, antes de avaliar se é órfão.
func orphanQuery(rgKeys, journeys []string, envRegex string) string {
	caseArgs := make([]string, 0, len(orphanRules)*2+1)
	for _, r := range orphanRules {
		caseArgs = append(caseArgs, r.cond, "'"+strings.ReplaceAll(r.reason, "'", `\'`)+"'")
	}
	caseArgs = append(caseArgs, "''")
	envCond := "true"
	if envRegex != "" {
		envCond = "(strcat(resEnv, ' ', resourceGroup) matches regex @'" + strings.ReplaceAll(envRegex, "'", "") + "')"
	}
	return `resources
| extend rj = tolower(` + journeyTagExpr + `), resEnv = ` + envTagExpr + `
| where rj == '' or rj in ` + kqlList(lowerAll(journeys)) + `
| extend rgKey = tolower(strcat(subscriptionId, '/', resourceGroup))
| where rgKey in ` + kqlList(rgKeys) + ` or (rj != '' and ` + envCond + `)
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
func ListOrphanResources(ctx context.Context, c *ARMClient, subscriptions []string, rgs []models.ScopedResourceGroup, journeys []string, envRegex string) ([]models.UnattachedDisk, []models.OrphanResource, error) {
	if len(journeys) == 0 {
		return nil, nil, nil
	}
	byKey := map[string]models.ScopedResourceGroup{}
	keys := make([]string, 0, len(rgs))
	for _, rg := range rgs {
		k := strings.ToLower(rg.SubscriptionID + "/" + rg.ResourceGroup)
		byKey[k] = rg
		keys = append(keys, k)
	}
	rows, err := c.ResourceGraphQuery(ctx, subscriptions, orphanQuery(keys, journeys, envRegex))
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
		// RG fora do escopo = entrou pela tag do próprio recurso (effectiveJourney usa essa tag).
		rg := byKey[strings.ToLower(r.SubscriptionID+"/"+r.ResourceGroup)]

		if r.Type == "microsoft.compute/disks" {
			// A linha do Resource Graph tem o mesmo formato do ARM (id, name, sku, properties...).
			var d armDisk
			if json.Unmarshal(raw, &d) != nil {
				continue
			}
			disk := armDiskToModel(d)
			disk.SubscriptionID = r.SubscriptionID
			disk.Journey, disk.JourneySource = effectiveJourney(d.Tags, rg)
			disks = append(disks, disk)
			continue
		}

		o := models.OrphanResource{
			ID: r.ID, Name: r.Name, Type: r.Type, Kind: r.Kind, Location: r.Location,
			ResourceGroup: r.ResourceGroup, SubscriptionID: r.SubscriptionID,
			Reason: r.Reason, Tags: r.Tags,
		}
		o.Journey, o.JourneySource = effectiveJourney(r.Tags, rg)
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

// effectiveJourney é a jornada de UM recurso: a tag "jornada" dele; sem ela, a tag do RG; sem as
// duas, a do cluster só quando o RG é o node RG (MC_*), exclusivo daquele cluster. RG de app/dados
// sem tag não define jornada (pode ser compartilhado entre jornadas) → "none".
func effectiveJourney(tags map[string]string, rg models.ScopedResourceGroup) (journey, source string) {
	for k, v := range tags {
		if strings.EqualFold(k, "jornada") && strings.TrimSpace(v) != "" {
			return v, "resource_tag"
		}
	}
	if rg.JourneyTag != "" {
		return rg.JourneyTag, "rg_tag"
	}
	if rg.Source == "node" && rg.Journey != "" {
		return rg.Journey, "node_rg"
	}
	return "", "none"
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
