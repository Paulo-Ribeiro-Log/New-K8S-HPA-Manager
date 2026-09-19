package handlers

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"

	"k8s-hpa-manager/internal/cloudprovider/aws"
	"k8s-hpa-manager/internal/cloudprovider/azure"
	"k8s-hpa-manager/internal/cloudprovider/gcp"
	"k8s-hpa-manager/internal/config"
	"k8s-hpa-manager/internal/finops"
	"k8s-hpa-manager/internal/models"
)

const (
	// unattachedDisksCacheTTL: a listagem no cloud custa alguns segundos e discos desatachados
	// mudam devagar. "Atualizar" no frontend manda refresh=true e ignora o cache.
	unattachedDisksCacheTTL = 5 * time.Minute
	// Orçamento total de uma varredura (cloud + PVs do cluster).
	unattachedDisksScanTimeout = 2 * time.Minute
	unattachedDisksPVTimeout   = 30 * time.Second
)

// cachedUnattachedDisks guarda só a parte cara e independente do cluster — a listagem no cloud.
// O cruzamento com os PVs e o preço são refeitos a cada request (baratos, e dependem do cluster).
type cachedUnattachedDisks struct {
	disks     []models.UnattachedDisk
	scannedAt time.Time
}

// diskScope descreve ONDE o cloud é varrido pra um cluster: EBS/PD/Managed Disk vivem na
// conta/projeto/subscription, não no cluster.
type diskScope struct {
	provider string // azure | gcp | aws
	key      string // chave de cache/singleflight
	label    string // texto exibido no frontend
	// azure
	subscription string
	// gcp
	project string
	// aws
	region  string
	profile string
}

func (h *FinOpsHandler) resolveDiskScope(cluster string) (diskScope, error) {
	serverURL := h.kubeManager.GetServerURL(cluster)
	switch config.DetectCloudProvider(serverURL, cluster) {
	case config.CloudProviderGKE:
		cfg := h.kubeManager.GetGKEClusterConfig(cluster)
		if cfg == nil || cfg.ProjectID == "" {
			return diskScope{}, fmt.Errorf("cluster GKE '%s' sem projectId em gke-clusters-config.json — rode o autodiscover", cluster)
		}
		return diskScope{
			provider: "gcp", project: cfg.ProjectID,
			key:   "gcp|" + cfg.ProjectID,
			label: "Projeto GCP " + cfg.ProjectID + " (todas as zonas/regiões)",
		}, nil

	case config.CloudProviderEKS:
		region, profile := h.awsRegionProfileForCluster(cluster)
		return diskScope{
			provider: "aws", region: region, profile: profile,
			key:   "aws|" + region + "|" + profile,
			label: "Conta AWS (profile " + orDefault(profile, "default") + ") — apenas a região " + region,
		}, nil

	case config.CloudProviderAKS:
		cfg := h.kubeManager.GetClusterConfig(cluster)
		if cfg == nil {
			return diskScope{}, fmt.Errorf("cluster AKS '%s' não encontrado em clusters-config.json — rode o autodiscover", cluster)
		}
		sub := cfg.SubscriptionID
		if sub == "" {
			sub = cfg.Subscription
		}
		if sub == "" {
			return diskScope{}, fmt.Errorf("cluster AKS '%s' sem subscription em clusters-config.json — rode o autodiscover", cluster)
		}
		label := "Subscription Azure " + sub
		if cfg.Subscription != "" && cfg.Subscription != sub {
			label = "Subscription Azure " + cfg.Subscription
		}
		return diskScope{provider: "azure", subscription: sub, key: "azure|" + sub, label: label}, nil
	}
	return diskScope{}, fmt.Errorf("não foi possível identificar o cloud do cluster '%s'", cluster)
}

func orDefault(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}

// listUnattachedDisks devolve a listagem do cloud, do cache (dentro do TTL) ou de uma varredura
// nova. singleflight evita duas varreduras idênticas simultâneas (duplo clique, duas abas).
func (h *FinOpsHandler) listUnattachedDisks(scope diskScope, forceRefresh bool) (cachedUnattachedDisks, bool, error) {
	if !forceRefresh {
		h.unattachedDisksMu.Lock()
		entry, ok := h.unattachedDisksCache[scope.key]
		h.unattachedDisksMu.Unlock()
		if ok && time.Since(entry.scannedAt) < unattachedDisksCacheTTL {
			return entry, true, nil
		}
	}

	v, err, _ := h.unattachedDisksSF.Do(scope.key, func() (interface{}, error) {
		ctx, cancel := context.WithTimeout(context.Background(), unattachedDisksScanTimeout)
		defer cancel()

		var disks []models.UnattachedDisk
		var err error
		switch scope.provider {
		case "azure":
			disks, err = azure.ListUnattachedDisks(ctx, scope.subscription)
		case "gcp":
			disks, err = gcp.ListUnattachedDisks(ctx, scope.project)
		case "aws":
			disks, err = aws.ListUnattachedVolumes(ctx, scope.region, scope.profile)
		default:
			err = fmt.Errorf("provider desconhecido: %s", scope.provider)
		}
		if err != nil {
			return nil, err
		}
		entry := cachedUnattachedDisks{disks: disks, scannedAt: time.Now()}
		h.unattachedDisksMu.Lock()
		h.unattachedDisksCache[scope.key] = entry
		h.unattachedDisksMu.Unlock()
		return entry, nil
	})
	if err != nil {
		return cachedUnattachedDisks{}, false, err
	}
	return v.(cachedUnattachedDisks), false, nil
}

// GetUnattachedDisks godoc
// GET /api/v1/finops/unattached-disks?cluster=X[&refresh=true]
//
// Lista os discos (Azure Managed Disk / GCP Persistent Disk / AWS EBS) da conta do cluster que não
// estão atachados a nenhuma VM, com custo mensal e um veredito por disco cruzando com os PVs do
// cluster (candidato a exclusão / revisar / em uso por um PV Bound). Somente leitura — a app nunca
// exclui nada; cada disco traz o comando de exclusão pra copiar.
func (h *FinOpsHandler) GetUnattachedDisks(c *gin.Context) {
	cluster := c.Query("cluster")
	if cluster == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "parâmetro 'cluster' é obrigatório"})
		return
	}
	refresh := c.Query("refresh") == "true"

	scope, err := h.resolveDiskScope(cluster)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	listed, fromCache, err := h.listUnattachedDisks(scope, refresh)
	if err != nil {
		log.Error().Err(err).Str("cluster", cluster).Str("provider", scope.provider).Msg("FinOps: falha ao listar discos desatachados")
		c.JSON(http.StatusBadGateway, gin.H{"error": "Falha ao listar discos desatachados (" + scope.provider + "): " + err.Error()})
		return
	}

	warnings := []string{}

	// Cruzamento com os PVs do cluster — best-effort: sem ele o relatório continua útil, só que
	// nenhum disco pode ser afirmado "candidato" (finops.BuildUnattachedDisksReport cuida disso).
	var pvIndex map[string]finops.PVDiskRef
	pvOK := false
	if client, cerr := h.kubeManager.GetClient(cluster); cerr != nil {
		warnings = append(warnings, "Não foi possível conectar ao cluster para cruzar com os PVs ("+cerr.Error()+"). Nenhum disco foi marcado como candidato a exclusão.")
	} else {
		pvCtx, cancel := context.WithTimeout(c.Request.Context(), unattachedDisksPVTimeout)
		pvIndex, err = finops.LoadPVDiskIndex(pvCtx, client)
		cancel()
		if err != nil {
			warnings = append(warnings, "Falha ao listar os PVs do cluster ("+err.Error()+"). Nenhum disco foi marcado como candidato a exclusão.")
			pvIndex = nil
		} else {
			pvOK = true
		}
	}

	rate, rateDate := h.exchange.Get()
	in := finops.UnattachedDisksInput{
		Cluster: cluster, Provider: scope.provider, Scope: scope.label,
		Disks: listed.disks, PVIndex: pvIndex, PVIndexOK: pvOK,
		Prices:       h.diskPriceFuncs(cluster, scope),
		ExchangeRate: rate, ExchangeDate: rateDate, Now: time.Now(),
		GCPProject: scope.project, AWSProfile: scope.profile,
	}
	report := finops.BuildUnattachedDisksReport(in)
	report.ScannedAt = listed.scannedAt
	report.FromCache = fromCache
	report.Warnings = append(report.Warnings, warnings...)
	report.Warnings = append(report.Warnings, h.unattachedDisksNotes(scope, report)...)

	c.JSON(http.StatusOK, report)
}

// diskPriceFuncs liga cada cloud ao pricer que a app já usa pro resto do FinOps.
func (h *FinOpsHandler) diskPriceFuncs(cluster string, scope diskScope) finops.DiskPriceFuncs {
	var p finops.DiskPriceFuncs
	switch scope.provider {
	case "azure":
		if h.diskPricer != nil {
			p.Azure = h.diskPricer.GetDiskPrice
		}
	case "gcp":
		if h.gcpPricer != nil {
			p.GCP = h.gcpPricer.GetDiskPricePerGBMonth
		}
	case "aws":
		if awsPricer := h.awsPricerForCluster(cluster); awsPricer != nil {
			p.AWS = awsPricer.GetDiskPricePerGBMonth
		}
	}
	return p
}

// unattachedDisksNotes lista as limitações da estimativa que valem pra este relatório, pra o
// usuário não ler o custo como mais exato do que é.
func (h *FinOpsHandler) unattachedDisksNotes(scope diskScope, r finops.UnattachedDisksReport) []string {
	var notes []string
	tableCount := 0
	for _, d := range r.Disks {
		if d.PriceSource == "table" {
			tableCount++
		}
	}
	if tableCount > 0 {
		notes = append(notes, fmt.Sprintf("%d disco(s) precificado(s) por tabela de referência (Ultra Disk, Premium SSD v2, Hyperdisk, EBS magnético): valor on-demand na região de referência (Azure brazilsouth, GCP São Paulo, AWS us-east-1), somando capacidade + IOPS/throughput provisionados acima da cota gratuita.", tableCount))
	}
	if r.Summary.UnpricedCount > 0 {
		notes = append(notes, fmt.Sprintf("%d disco(s) de tipo sem preço mapeado nem linha na tabela — o total subestima o custo real.", r.Summary.UnpricedCount))
	}
	switch scope.provider {
	case "gcp":
		if h.gcpPricer != nil {
			pricingRegion := h.gcpPricer.Region()
			for _, d := range r.Disks {
				if d.Location != "" && d.Location != pricingRegion {
					notes = append(notes, "Preços de GCP estimados pela região "+pricingRegion+"; discos em outras regiões podem custar diferente.")
					break
				}
			}
		}
	case "aws":
		notes = append(notes, "AWS: varredura restrita à região "+scope.region+" (EBS é regional). Volumes io1/io2 não incluem o custo de IOPS provisionados.")
	case "azure":
		notes = append(notes, "Azure: preço do tier LRS na região de cada disco; discos ZRS podem custar mais. Discos atachados a VMs desalocadas não entram (não estão 'Unattached').")
	}
	return notes
}
