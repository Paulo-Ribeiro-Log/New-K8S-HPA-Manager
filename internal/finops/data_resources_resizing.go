package finops

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/rs/zerolog/log"

	"k8s-hpa-manager/internal/cloudprovider/azure"
)

// Ofertas de resizing pros recursos do RG de dados (VMs de banco, discos, PostgreSQL/MySQL
// Flexible Server). Duas fontes, propositalmente separadas:
//
//   - INVENTÁRIO (sempre, custo zero): estado do disco/VM que o ARM já entrega — disco desatachado,
//     disco de VM desalocada, Premium SSD em HLG.
//   - USO REAL (sob demanda, Azure Monitor): CPU/memória de VM e CPU/memória/storage de Flexible
//     Server — só isso sustenta "esta VM está grande demais".
//
// Nenhuma oferta é aplicada pela app; são sugestões com a evidência ao lado.

// DataUtilization é o uso real de um recurso numa janela (percentuais 0-100).
type DataUtilization struct {
	Days      int     `json:"days"`
	Points    int     `json:"points"`
	CPUAvgPct float64 `json:"cpu_avg_pct"`
	CPUP95Pct float64 `json:"cpu_p95_pct"`
	CPUMaxPct float64 `json:"cpu_max_pct"`
	// Memória: VM não tem "% usado" na plataforma, só "Available Memory Bytes" — derivamos o usado
	// contra a memória do SKU. HasMemory=false quando a métrica não existe (VM sem a métrica,
	// SKU fora da tabela de specs) — aí nenhuma oferta de downsize é feita (não dá pra saber).
	HasMemory  bool    `json:"has_memory"`
	MemAvgPct  float64 `json:"mem_avg_pct,omitempty"`
	MemP95Pct  float64 `json:"mem_p95_pct,omitempty"`
	MemMaxPct  float64 `json:"mem_max_pct,omitempty"`
	StoragePct float64 `json:"storage_pct,omitempty"` // Flexible Server: P95 de storage_percent
}

// DataRecommendation é uma oferta (ou alerta) sobre um recurso.
type DataRecommendation struct {
	Kind              string  `json:"kind"`    // vm_resize | vm_burstable | vm_undersized | vm_deallocated | disk_unattached | disk_deallocated_vm | disk_sku | flex_resize | flex_storage | flex_undersized
	Verdict           string  `json:"verdict"` // recommended | consider | info
	Title             string  `json:"title"`
	Reason            string  `json:"reason"`
	TargetSKU         string  `json:"target_sku,omitempty"`
	MonthlySavingsBRL float64 `json:"monthly_savings_brl,omitempty"`
}

// dataEnvFromRG devolve "hlg" | "prd" | "" a partir do sufixo do RG (rg-<nome>-data-<env>).
func dataEnvFromRG(rg string) string {
	l := strings.ToLower(rg)
	switch {
	case strings.HasSuffix(l, "-hlg"):
		return "hlg"
	case strings.HasSuffix(l, "-prd"):
		return "prd"
	}
	return ""
}

func regionOf(location string) string { return normalizeRetailRegion(location) }

// ── Inventário ───────────────────────────────────────────────────────────────

// DiskPriceFunc consulta o preço mensal (USD) de um disco managed por tipo/tier/região — injetada
// (DiskPricer.GetDiskPrice) pra manter a lógica testável sem rede.
type DiskPriceFunc func(diskType, tier, region string) (float64, string, error)

// ApplyInventoryRecommendations preenche Recommendations dos discos e VMs a partir só do que o ARM
// já entregou (estado/anexo/SKU) — sem métricas. Idempotente: substitui as recomendações de
// inventário anteriores.
func ApplyInventoryRecommendations(resources []AzureDataResource, dataRG string, diskPrice DiskPriceFunc, rate float64) {
	env := dataEnvFromRG(dataRG)

	// Custo dos discos por VM desalocada (pra dizer o que ainda cobra).
	deallocDiskCost := map[string]float64{}
	deallocDiskCount := map[string]int{}
	for _, r := range resources {
		if isDiskResource(r) && strings.EqualFold(r.DiskState, "Reserved") && r.AttachedTo != "" {
			k := strings.ToLower(r.AttachedTo)
			deallocDiskCost[k] += r.MonthlyCostBRL
			deallocDiskCount[k]++
		}
	}

	for i := range resources {
		r := &resources[i]
		r.Recommendations = nil
		switch {
		case isDiskResource(*r):
			diskInventoryRecs(r, env, diskPrice, rate)
		case isVMResource(*r) && r.PowerState == "deallocated":
			k := strings.ToLower(r.Name)
			r.Recommendations = append(r.Recommendations, DataRecommendation{
				Kind: "vm_deallocated", Verdict: "info",
				Title: "VM desalocada",
				Reason: fmt.Sprintf("Sem custo de compute, mas %d disco(s) seguem cobrando %s/mês. Se a VM foi abandonada, snapshot + exclusão liberam esse custo.",
					deallocDiskCount[k], brl(deallocDiskCost[k])),
			})
		}
	}
}

func isDiskResource(r AzureDataResource) bool {
	return strings.EqualFold(r.Type, "Microsoft.Compute/disks")
}
func isVMResource(r AzureDataResource) bool {
	return strings.EqualFold(r.Type, "Microsoft.Compute/virtualMachines")
}
func isFlexResource(r AzureDataResource) bool {
	t := strings.ToLower(r.Type)
	return t == "microsoft.dbforpostgresql/flexibleservers" || t == "microsoft.dbformysql/flexibleservers"
}

func brl(v float64) string { return fmt.Sprintf("R$ %.2f", v) }

func diskInventoryRecs(r *AzureDataResource, env string, diskPrice DiskPriceFunc, rate float64) {
	switch {
	case strings.EqualFold(r.DiskState, "Unattached"):
		r.Recommendations = append(r.Recommendations, DataRecommendation{
			Kind: "disk_unattached", Verdict: "recommended",
			Title:             "Disco desatachado — excluir ou criar snapshot",
			Reason:            fmt.Sprintf("Nenhuma VM usa este disco%s. Custa %s/mês parado. Crie um snapshot (bem mais barato) se ainda houver dado útil e exclua o disco.", sinceText(r.UnattachedSince), brl(r.MonthlyCostBRL)),
			MonthlySavingsBRL: r.MonthlyCostBRL,
		})

	case strings.EqualFold(r.DiskState, "Reserved"):
		r.Recommendations = append(r.Recommendations, DataRecommendation{
			Kind: "disk_deallocated_vm", Verdict: "info",
			Title:  "Disco de VM desalocada",
			Reason: fmt.Sprintf("A VM %s está desalocada, mas este disco continua cobrando %s/mês.", orDash(r.AttachedTo), brl(r.MonthlyCostBRL)),
		})

	case strings.EqualFold(r.SKUName, "Premium_LRS") && r.SizeGB > 0 && env == "hlg" && diskPrice != nil && r.MonthlyCostUSD > 0:
		// Premium SSD em HLG: Standard SSD costuma bastar. Só uma OFERTA (verdict "consider") —
		// o ARM não informa a demanda de IOPS/throughput do disco, então a decisão é humana.
		tier := ResolveManagedDiskTier("Standard SSD", r.SizeGB)
		usd, _, err := diskPrice("Standard SSD", tier, regionOf(r.Location))
		if err == nil && usd > 0 && usd < r.MonthlyCostUSD {
			savings := round2((r.MonthlyCostUSD - usd) * rate)
			r.Recommendations = append(r.Recommendations, DataRecommendation{
				Kind: "disk_sku", Verdict: "consider",
				Title:     "Premium SSD em HLG — considerar Standard SSD",
				TargetSKU: "StandardSSD_LRS",
				Reason: fmt.Sprintf("Ambiente HLG. Standard SSD %s custaria %s/mês contra %s/mês do Premium %s. Standard SSD entrega menos IOPS/throughput — confirme que o banco não precisa da performance do Premium antes de trocar.",
					tier, brl(usd*rate), brl(r.MonthlyCostBRL), r.DiskTier),
				MonthlySavingsBRL: savings,
			})
		}
	}
}

func sinceText(since string) string {
	if len(since) >= 10 {
		return " desde " + since[:10]
	}
	return ""
}

// ── Uso real (Azure Monitor) ──────────────────────────────────────────────────

const dataUsageConcurrency = 8

// AnalyzeDataResourceUsage coleta uso real de VMs (não desalocadas) e Flexible Servers via Azure
// Monitor. Devolve índice do recurso → uso; recurso sem métrica simplesmente não aparece. Falha
// individual é logada e ignorada (uma VM sem a métrica de memória não derruba o resto).
func AnalyzeDataResourceUsage(ctx context.Context, c *azure.ARMClient, resources []AzureDataResource, days int, specs CloudPricer) map[int]DataUtilization {
	out := map[int]DataUtilization{}
	var mu sync.Mutex
	sem := make(chan struct{}, dataUsageConcurrency)
	var wg sync.WaitGroup

	for i := range resources {
		r := resources[i]
		if r.ResourceID == "" {
			continue
		}
		vm := isVMResource(r) && r.PowerState != "deallocated"
		flex := isFlexResource(r)
		if !vm && !flex {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, r AzureDataResource, vm bool) {
			defer wg.Done()
			defer func() { <-sem }()
			var u DataUtilization
			var ok bool
			var err error
			if vm {
				u, ok, err = collectVMUsage(ctx, c, r, days, specs)
			} else {
				u, ok, err = collectFlexUsage(ctx, c, r, days)
			}
			if err != nil {
				log.Debug().Err(err).Str("resource", r.Name).Msg("FinOps/DataResources: sem métricas de uso")
				return
			}
			if ok {
				mu.Lock()
				out[i] = u
				mu.Unlock()
			}
		}(i, r, vm)
	}
	wg.Wait()
	return out
}

func collectVMUsage(ctx context.Context, c *azure.ARMClient, r AzureDataResource, days int, specs CloudPricer) (DataUtilization, bool, error) {
	m, err := c.ResourceMetrics(ctx, r.ResourceID, []string{"Percentage CPU", "Available Memory Bytes"}, days)
	if err != nil {
		return DataUtilization{}, false, err
	}
	cpu, ok := m["Percentage CPU"]
	if !ok {
		return DataUtilization{}, false, nil
	}
	u := DataUtilization{Days: days, Points: cpu.Points, CPUAvgPct: round2(cpu.Avg), CPUP95Pct: round2(cpu.P95), CPUMaxPct: round2(cpu.Max)}
	if avail, ok := m["Available Memory Bytes"]; ok && specs != nil {
		if _, memGB := specs.GetVMSpecs(r.SKUName); memGB > 0 {
			total := float64(memGB) * 1024 * 1024 * 1024
			used := func(availBytes float64) float64 { return clampPct(100 - availBytes/total*100) }
			u.HasMemory = true
			u.MemAvgPct = round2(used(avail.Avg))
			u.MemP95Pct = round2(used(avail.P5)) // memória usada alta = memória DISPONÍVEL baixa
			u.MemMaxPct = round2(used(avail.Min))
		}
	}
	return u, true, nil
}

func collectFlexUsage(ctx context.Context, c *azure.ARMClient, r AzureDataResource, days int) (DataUtilization, bool, error) {
	m, err := c.ResourceMetrics(ctx, r.ResourceID, []string{"cpu_percent", "memory_percent", "storage_percent"}, days)
	if err != nil {
		return DataUtilization{}, false, err
	}
	cpu, ok := m["cpu_percent"]
	if !ok {
		return DataUtilization{}, false, nil
	}
	u := DataUtilization{Days: days, Points: cpu.Points, CPUAvgPct: round2(cpu.Avg), CPUP95Pct: round2(cpu.P95), CPUMaxPct: round2(cpu.Max)}
	if mem, ok := m["memory_percent"]; ok {
		u.HasMemory = true
		u.MemAvgPct, u.MemP95Pct, u.MemMaxPct = round2(mem.Avg), round2(mem.P95), round2(mem.Max)
	}
	if st, ok := m["storage_percent"]; ok {
		u.StoragePct = round2(st.P95)
	}
	return u, true, nil
}

func clampPct(v float64) float64 { return math.Max(0, math.Min(100, v)) }

// ── Ofertas a partir do uso ───────────────────────────────────────────────────

// FlexTargetPricer estima o custo mensal (USD) de um Flexible Server com outro SKU/tier —
// injetada (wrapper de priceFlexibleServer) pra testar sem a Retail Prices API.
type FlexTargetPricer func(target AzureDataResource) (usd float64, ok bool)

// DefaultFlexTargetPricer usa a Retail Prices API via priceFlexibleServer.
func DefaultFlexTargetPricer(rate float64) FlexTargetPricer {
	return func(t AzureDataResource) (float64, bool) {
		priceFlexibleServer(&t, regionOf(t.Location), rate)
		return t.MonthlyCostUSD, t.MonthlyCostUSD > 0
	}
}

// ApplyUsageRecommendations junta as ofertas baseadas em uso real às do inventário do recurso.
func ApplyUsageRecommendations(r *AzureDataResource, u DataUtilization, dataRG string, vmPricer CloudPricer, flexPrice FlexTargetPricer, rate float64) {
	cp := u
	r.Utilization = &cp
	env := dataEnvFromRG(dataRG)
	switch {
	case isVMResource(*r):
		r.Recommendations = append(r.Recommendations, vmUsageRecs(*r, u, env, vmPricer, rate)...)
	case isFlexResource(*r):
		r.Recommendations = append(r.Recommendations, flexUsageRecs(*r, u, env, flexPrice, rate)...)
	}
}

var burstableLadder = []string{"Standard_B2s", "Standard_B2ms", "Standard_B4ms", "Standard_B8ms"}

func vmUsageRecs(r AzureDataResource, u DataUtilization, env string, pricer CloudPricer, rate float64) []DataRecommendation {
	if pricer == nil {
		return nil
	}
	curCPU, curMem := pricer.GetVMSpecs(r.SKUName)
	if curCPU == 0 || curMem == 0 {
		return []DataRecommendation{{
			Kind: "vm_resize", Verdict: "info", Title: "Sem oferta de resizing",
			Reason: fmt.Sprintf("O SKU %s não está na tabela de specs (vCPU/memória) — não dá pra sugerir alternativas automaticamente. Uso: CPU P95 %.0f%%.", r.SKUName, u.CPUP95Pct),
		}}
	}
	evidence := fmt.Sprintf("CPU média %.0f%% / P95 %.0f%% / pico %.0f%%", u.CPUAvgPct, u.CPUP95Pct, u.CPUMaxPct)
	if u.HasMemory {
		evidence += fmt.Sprintf("; memória usada média %.0f%% / P95 %.0f%% / pico %.0f%%", u.MemAvgPct, u.MemP95Pct, u.MemMaxPct)
	}
	evidence += fmt.Sprintf(" (%dd)", u.Days)

	var recs []DataRecommendation

	if u.CPUP95Pct >= 80 || (u.HasMemory && u.MemP95Pct >= 90) {
		recs = append(recs, DataRecommendation{
			Kind: "vm_undersized", Verdict: "info", Title: "VM sob pressão de recursos",
			Reason: "Uso alto sustentado — não reduzir; avaliar aumentar. " + evidence,
		})
		return recs
	}
	if !u.HasMemory {
		return []DataRecommendation{{
			Kind: "vm_resize", Verdict: "info", Title: "Sem oferta de resizing",
			Reason: "Sem a métrica de memória disponível não há como afirmar que a VM está sobrando. " + evidence,
		}}
	}

	verdict := "consider"
	if env == "hlg" {
		verdict = "recommended"
	}

	// Redução dentro da lógica já usada nos node pools (mesmos limiares de "superdimensionada").
	for _, alt := range SuggestVMTier("aks", r.SKUName, u.CPUP95Pct, u.MemP95Pct, pricer, rate, 1) {
		if alt.Verdict != "cheaper" || alt.MonthlySavingsBRL <= 0 {
			continue
		}
		recs = append(recs, DataRecommendation{
			Kind: "vm_resize", Verdict: verdict,
			Title:             fmt.Sprintf("Reduzir para %s (%d vCPU / %d GB)", alt.VMSize, alt.CPUCores, alt.MemoryGB),
			TargetSKU:         alt.VMSize,
			Reason:            evidence + ". " + prodCaution(env),
			MonthlySavingsBRL: alt.MonthlySavingsBRL,
		})
	}

	// VM de 2 vCPU não tem "metade" na mesma família — a saída realista é Burstable (B-series),
	// que só serve pra uso baixo e com picos curtos.
	if len(recs) == 0 && !strings.HasPrefix(strings.ToUpper(strings.TrimPrefix(r.SKUName, "Standard_")), "B") &&
		u.CPUAvgPct < 20 && u.CPUP95Pct < 40 {
		if rec, ok := burstableRec(r, u, curCPU, curMem, pricer, rate, evidence, env); ok {
			recs = append(recs, rec)
		}
	}
	return recs
}

func burstableRec(r AzureDataResource, u DataUtilization, curCPU, curMem int, pricer CloudPricer, rate float64, evidence, env string) (DataRecommendation, bool) {
	curPrice, _, err := pricer.GetPrice(r.SKUName)
	if err != nil || curPrice <= 0 {
		return DataRecommendation{}, false
	}
	needGB := u.MemMaxPct / 100 * float64(curMem) * 1.25 // pico de memória + 25% de folga
	for _, sku := range burstableLadder {
		cpu, mem := pricer.GetVMSpecs(sku)
		if cpu == 0 || cpu > curCPU || float64(mem) < needGB {
			continue
		}
		price, _, err := pricer.GetPrice(sku)
		if err != nil || price <= 0 || price >= curPrice*0.9 {
			continue
		}
		return DataRecommendation{
			Kind: "vm_burstable", Verdict: "consider",
			Title:             fmt.Sprintf("Considerar Burstable %s (%d vCPU / %d GB)", sku, cpu, mem),
			TargetSKU:         sku,
			Reason:            evidence + ". B-series acumula crédito de CPU enquanto ocioso e gasta nos picos — serve pra uso baixo com picos curtos; se houver carga sustentada a VM é limitada. " + prodCaution(env),
			MonthlySavingsBRL: round2((curPrice - price) * hoursPerMonth * rate),
		}, true
	}
	return DataRecommendation{}, false
}

func prodCaution(env string) string {
	if env == "prd" {
		return "Ambiente PRD: valide com o time dono do banco antes de mudar."
	}
	return ""
}

var flexSKURe = regexp.MustCompile(`(?i)^(Standard_[A-Za-z]+)(\d+)([A-Za-z]*_v\d+)$`)
var flexVCoreLadder = []int{2, 4, 8, 16, 32, 48, 64, 96}

// halveFlexSKU devolve o SKU com o degrau anterior de vCores da mesma família (D4ds_v5 → D2ds_v5).
func halveFlexSKU(sku string) (string, bool) {
	m := flexSKURe.FindStringSubmatch(sku)
	if m == nil {
		return "", false
	}
	n, _ := strconv.Atoi(m[2])
	for i, v := range flexVCoreLadder {
		if v == n && i > 0 {
			return m[1] + strconv.Itoa(flexVCoreLadder[i-1]) + m[3], true
		}
	}
	return "", false
}

func flexUsageRecs(r AzureDataResource, u DataUtilization, env string, price FlexTargetPricer, rate float64) []DataRecommendation {
	var recs []DataRecommendation
	evidence := fmt.Sprintf("CPU média %.0f%% / P95 %.0f%% / pico %.0f%%", u.CPUAvgPct, u.CPUP95Pct, u.CPUMaxPct)
	if u.HasMemory {
		evidence += fmt.Sprintf("; memória média %.0f%% / P95 %.0f%%", u.MemAvgPct, u.MemP95Pct)
	}
	evidence += fmt.Sprintf("; storage %.0f%% (%dd)", u.StoragePct, u.Days)

	if u.StoragePct >= 80 {
		recs = append(recs, DataRecommendation{
			Kind: "flex_storage", Verdict: "info", Title: "Storage do servidor quase cheio",
			Reason: fmt.Sprintf("%.0f%% do storage provisionado em uso. Flexible Server só permite AUMENTAR o storage (nunca reduzir) e o auto-grow depende da configuração. %s", u.StoragePct, evidence),
		})
	}
	if u.CPUP95Pct >= 80 || (u.HasMemory && u.MemP95Pct >= 90) {
		recs = append(recs, DataRecommendation{
			Kind: "flex_undersized", Verdict: "info", Title: "Servidor sob pressão de recursos",
			Reason: "Uso alto sustentado — não reduzir. " + evidence,
		})
		return recs
	}
	if !u.HasMemory || u.CPUP95Pct >= 30 || u.MemP95Pct >= 50 {
		return recs
	}

	// Alvo: degrau anterior de vCores na mesma família; se já está no menor degrau (2 vCores) de
	// GeneralPurpose, o próximo passo é Burstable B2ms (2 vCores / 8 GB — mesma memória).
	target := AzureDataResource{Name: r.Name, Type: r.Type, Location: r.Location, SKUTier: r.SKUTier}
	title := ""
	if sku, ok := halveFlexSKU(r.SKUName); ok {
		target.SKUName = sku
		title = fmt.Sprintf("Reduzir compute para %s", sku)
	} else if strings.EqualFold(r.SKUTier, "GeneralPurpose") {
		target.SKUName, target.SKUTier = "Standard_B2ms", "Burstable"
		title = "Considerar Burstable Standard_B2ms (2 vCore / 8 GB)"
	} else {
		return recs
	}
	if price == nil {
		return recs
	}
	targetUSD, ok := price(target)
	// Compara COMPUTE com COMPUTE: MonthlyCostUSD inclui storage, que a troca de SKU não altera.
	curCompute := r.ComputeCostUSD
	if curCompute <= 0 {
		curCompute = r.MonthlyCostUSD
	}
	if r.HAMode != "" && !strings.EqualFold(r.HAMode, "Disabled") {
		targetUSD *= 2 // ComputeCostUSD já vem em dobro com HA; o alvo também precisa
	}
	// Só vale a oferta se o alvo for de fato mais barato (>10%).
	if !ok || curCompute <= 0 || targetUSD >= curCompute*0.9 {
		return recs
	}
	recs = append(recs, DataRecommendation{
		Kind: "flex_resize", Verdict: "consider", Title: title, TargetSKU: target.SKUName,
		Reason:            evidence + ". Só compute (vCore/hora) considerado no preço; a troca de tier reinicia o servidor. " + prodCaution(env),
		MonthlySavingsBRL: round2((curCompute - targetUSD) * rate),
	})
	return recs
}
