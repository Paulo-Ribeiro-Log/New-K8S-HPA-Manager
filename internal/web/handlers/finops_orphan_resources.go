package handlers

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"

	"k8s-hpa-manager/internal/cloudprovider/azure"
	"k8s-hpa-manager/internal/finops"
	"k8s-hpa-manager/internal/models"
)

// FinOps → Recursos órfãos (Azure): discos desatachados e demais recursos sem uso (NIC, IP
// público, Private Endpoint desconectado, VM desalocada, NSG/LB/NAT sem vínculo...) nos Resource
// Groups das jornadas selecionadas no cabeçalho — RGs com a tag "jornada", RG de app e node RG
// (MC_*) dos clusters da jornada e o RG de dados rg-<nome>-data-<env>. Substitui, para AKS, a
// varredura da subscription inteira de GET /finops/unattached-disks (que segue valendo para EKS/GKE).

const (
	orphanScanCacheTTL = 5 * time.Minute
	orphanScanTimeout  = 3 * time.Minute
	orphanPVTimeout    = 30 * time.Second
	orphanPVParallel   = 4
)

// cachedOrphanScan guarda a parte cara (Resource Graph). PVs e preços são refeitos a cada request.
type cachedOrphanScan struct {
	clusters  []azure.JourneyCluster
	rgs       []models.ScopedResourceGroup
	excluded  []models.ScopedResourceGroup // RGs da jornada de outro ambiente ou sem ambiente identificável
	disks     []models.UnattachedDisk
	others    []models.OrphanResource // já com idade (AnnotateOrphanAges)
	warnings  []string
	scannedAt time.Time
}

// parseJourneys normaliza ?journeys=a,b (minúsculas, sem duplicatas, ordenado) — vazio = todas.
func parseJourneys(raw string) []string {
	seen := map[string]bool{}
	var out []string
	for _, j := range strings.Split(raw, ",") {
		j = strings.ToLower(strings.TrimSpace(j))
		if j != "" && !seen[j] {
			seen[j] = true
			out = append(out, j)
		}
	}
	sort.Strings(out)
	return out
}

// journeyClusters são os clusters AKS (clusters-config.json) das jornadas e do ambiente env
// (journeys vazio = todas; env vazio = qualquer ambiente).
func (h *FinOpsHandler) journeyClusters(journeys []string, env string) []azure.JourneyCluster {
	want := map[string]bool{}
	for _, j := range journeys {
		want[j] = true
	}
	var out []azure.JourneyCluster
	for _, cfg := range h.kubeManager.GetAllClusterConfigs() {
		journey := cfg.Journey()
		if len(want) > 0 && !want[strings.ToLower(journey)] {
			continue
		}
		if env != "" && finops.EnvironmentOf(cfg.Name) != env {
			continue
		}
		out = append(out, azure.JourneyCluster{
			Name:             cfg.Name,
			Journey:          journey,
			AppResourceGroup: cfg.ResourceGroup,
			DataRGCandidates: finops.DataRGCandidates(cfg.Name, cfg.ResourceGroup),
		})
	}
	return out
}

func (h *FinOpsHandler) scanOrphans(journeys []string, env string, forceRefresh bool) (cachedOrphanScan, bool, error) {
	key := "orphans|" + env + "|" + strings.Join(journeys, ",")
	if !forceRefresh {
		h.orphanMu.Lock()
		entry, ok := h.orphanCache[key]
		h.orphanMu.Unlock()
		if ok && time.Since(entry.scannedAt) < orphanScanCacheTTL {
			return entry, true, nil
		}
	}

	v, err, _ := h.orphanSF.Do(key, func() (interface{}, error) {
		ctx, cancel := context.WithTimeout(context.Background(), orphanScanTimeout)
		defer cancel()

		scan := cachedOrphanScan{clusters: h.journeyClusters(journeys, env), scannedAt: time.Now()}
		arm, err := azure.NewARMClient(ctx, "")
		if err != nil {
			return nil, err
		}
		subs, err := arm.ListSubscriptions(ctx)
		if err != nil {
			return nil, fmt.Errorf("listar subscriptions: %w", err)
		}
		subIDs := make([]string, 0, len(subs))
		for _, s := range subs {
			subIDs = append(subIDs, s.ID)
		}

		rgs, err := azure.ResolveJourneyResourceGroups(ctx, arm, subIDs, journeys, scan.clusters)
		if err != nil {
			return nil, fmt.Errorf("resolver os resource groups da jornada: %w", err)
		}
		// HLG e PRD da mesma jornada compartilham a tag: só fica o ambiente do cluster analisado.
		scan.rgs, scan.excluded = finops.FilterScopeByEnvironment(rgs, env)
		if scan.disks, scan.others, err = azure.ListOrphanResources(ctx, arm, subIDs, scan.rgs); err != nil {
			return nil, fmt.Errorf("listar recursos órfãos: %w", err)
		}

		ids := make([]string, 0, len(scan.others))
		for _, o := range scan.others {
			ids = append(ids, o.ID)
		}
		changes, lcErr := azure.LastChanges(ctx, arm, subIDs, ids)
		finops.AnnotateOrphanAges(scan.others, changes, scan.scannedAt)
		if lcErr != nil {
			// Sem o histórico de alterações não dá para afirmar há quanto tempo estão órfãos.
			for i := range scan.others {
				scan.others[i].SinceBasis, scan.others[i].AgeDays, scan.others[i].Recent = "unknown", -1, false
				scan.others[i].LastChange = ""
			}
			scan.warnings = append(scan.warnings, "Não foi possível consultar o histórico de alterações (resourcechanges): a idade dos recursos não-disco é desconhecida — "+lcErr.Error())
		}

		h.orphanMu.Lock()
		h.orphanCache[key] = scan
		h.orphanMu.Unlock()
		return scan, nil
	})
	if err != nil {
		return cachedOrphanScan{}, false, err
	}
	return v.(cachedOrphanScan), false, nil
}

// clusterPVIndexes carrega os PVs dos clusters do escopo que têm disco no relatório (best-effort,
// em paralelo). O context do kubeconfig pode ser o nome do AKS ou "<nome>-admin".
func (h *FinOpsHandler) clusterPVIndexes(ctx context.Context, clusters []azure.JourneyCluster, disks []models.UnattachedDisk) ([]finops.ClusterPVIndex, []string) {
	var owners []string
	for _, cl := range clusters {
		needle := "_" + strings.ToLower(cl.Name) + "_"
		for _, d := range disks {
			if strings.Contains(strings.ToLower(d.K8sClusterHint), needle) {
				owners = append(owners, cl.Name)
				break
			}
		}
	}

	out := make([]finops.ClusterPVIndex, len(owners))
	failed := make([]string, len(owners))
	sem := make(chan struct{}, orphanPVParallel)
	var wg sync.WaitGroup
	for i, name := range owners {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			out[i] = finops.ClusterPVIndex{Cluster: name}
			client, err := h.kubeManager.GetClient(name)
			if err != nil {
				client, err = h.kubeManager.GetClient(name + "-admin")
			}
			if err != nil {
				failed[i] = name + " (sem acesso: " + err.Error() + ")"
				return
			}
			pvCtx, cancel := context.WithTimeout(ctx, orphanPVTimeout)
			defer cancel()
			idx, err := finops.LoadPVDiskIndex(pvCtx, client)
			if err != nil {
				failed[i] = name + " (" + err.Error() + ")"
				return
			}
			out[i].Index, out[i].OK = idx, true
		}()
	}
	wg.Wait()

	var warnings []string
	for _, f := range failed {
		if f != "" {
			warnings = append(warnings, "PVs não listados no cluster "+f+" — os discos dele ficam em \"revisar\".")
		}
	}
	return out, warnings
}

// GetOrphanResources godoc
// GET /api/v1/finops/orphan-resources?cluster=<cluster analisado>&journeys=logistica,backoffice[&refresh=true]
//
// O escopo é jornada × ambiente: o ambiente (prd/hlg/...) vem do nome do cluster analisado — HLG e
// PRD da mesma jornada nunca se misturam. Somente leitura — a app nunca exclui nada; cada recurso
// traz o comando de exclusão para copiar.
func (h *FinOpsHandler) GetOrphanResources(c *gin.Context) {
	journeys := parseJourneys(c.Query("journeys"))
	cluster := c.Query("cluster")
	env := finops.EnvironmentOf(cluster)
	scan, fromCache, err := h.scanOrphans(journeys, env, c.Query("refresh") == "true")
	if err != nil {
		log.Error().Err(err).Strs("journeys", journeys).Msg("FinOps: falha na varredura de recursos órfãos")
		c.JSON(http.StatusBadGateway, gin.H{"error": "Falha ao consultar o Azure: " + err.Error()})
		return
	}

	pvIndexes, pvWarnings := h.clusterPVIndexes(c.Request.Context(), scan.clusters, scan.disks)

	rate, rateDate := h.exchange.Get()
	var prices finops.DiskPriceFuncs
	if h.diskPricer != nil {
		prices.Azure = h.diskPricer.GetDiskPrice
	}
	scope := "Todas as jornadas"
	if len(journeys) > 0 {
		scope = "Jornada(s): " + strings.Join(journeys, ", ")
	}
	if env != "" {
		scope += " · ambiente " + env
	}
	disks := finops.BuildUnattachedDisksReportForClusters(finops.UnattachedDisksInput{
		Provider: "azure", Scope: scope, Disks: scan.disks, Prices: prices,
		ExchangeRate: rate, ExchangeDate: rateDate, Now: time.Now(),
	}, pvIndexes)
	disks.ScannedAt, disks.FromCache = scan.scannedAt, fromCache

	// Cópia: o cache é compartilhado entre requests e PriceOrphanResources ordena/preenche.
	others := append([]models.OrphanResource(nil), scan.others...)
	finops.PriceOrphanResources(others, rate, azure.OrphanDeleteCommand)

	warnings := append(append([]string{}, scan.warnings...), pvWarnings...)
	if env == "" {
		warnings = append(warnings, fmt.Sprintf("Não foi possível identificar o ambiente (prd/hlg/...) pelo nome do cluster %q — o escopo inclui todos os ambientes da jornada.", cluster))
	}
	if len(scan.rgs) == 0 {
		warnings = append(warnings, "Nenhum resource group encontrado para o escopo — confira a tag \"jornada\" nos RGs e se os clusters da jornada estão em clusters-config.json (rode o autodiscover).")
	}

	c.JSON(http.StatusOK, gin.H{
		"journeys":                 journeys,
		"scope":                    scope,
		"resource_groups":          scan.rgs,
		"excluded_resource_groups": scan.excluded,
		"environment":              env,
		"clusters":                 len(scan.clusters),
		"disks":                    disks,
		"orphans":                  others,
		"orphan_summary":           finops.SummarizeOrphans(others),
		"min_age_days":             finops.OrphanMinAgeDays,
		"exchange_rate":            rate,
		"scanned_at":               scan.scannedAt,
		"from_cache":               fromCache,
		"warnings":                 warnings,
	})
}
