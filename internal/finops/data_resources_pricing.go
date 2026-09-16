package finops

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

// PriceDataResources tenta precificar cada recurso de dados. Achado real ao validar ao vivo
// contra um RG de dados de produção (ver comentário de AzureDataResource): a maior parte do
// custo real encontrado era VM (banco self-hosted em IaaS) + discos managed — não PaaS — então
// os dois tipos com MAIOR confiança de preço reutilizam os pricers JÁ TESTADOS do resto do FinOps
// (vmPricer/diskPricer, os mesmos usados pra precificar node pools do AKS), em vez de inventar
// uma consulta nova à Retail Prices API pra eles.
//
// Escopo deliberadamente conservador pros demais — nunca inventa um número quando o modelo de
// cobrança real do serviço não é confiavelmente derivável só do SKU (ver PricingNote em cada
// caso):
//   - VM (Microsoft.Compute/virtualMachines): preço confiável — reusa AzurePricer.GetPrice, o
//     MESMO mecanismo já usado (e validado) pra precificar os node pools do AKS.
//   - Disco managed (Microsoft.Compute/disks): preço confiável — reusa DiskPricer.GetDiskPrice
//     (MapStorageClassToAzureType + ResolveManagedDiskTier), o MESMO mecanismo já usado pra
//     precificar disco OS de node pool e PVCs.
//   - Azure Cache for Redis: preço confiável (serviço cobrado por hora de instância, fixo por
//     SKU+tamanho, igual a uma VM) — estimado pra TODOS os tiers.
//   - Service Bus/Event Hub PREMIUM: preço confiável (Messaging Units, cobradas por hora, fixo
//     por capacidade) — estimado. Basic/Standard são majoritariamente cobrados por OPERAÇÃO
//     (consumo), não por um valor fixo mensal — NÃO estimado, marcado como tal.
//   - Azure Database for PostgreSQL/MySQL (Flexible Server ou Single Server): tentativa
//     best-effort via Retail Prices API (cobrado por hora de compute, como uma VM) — NUNCA
//     validado ao vivo contra um tenant real (diferente de VM/disco, que já são mecanismos
//     usados em produção por este app) — se o filtro não encontrar preço, cai em "não estimado"
//     honestamente, nunca um número inventado.
//   - Storage Accounts (F3.1 do FINOPS-IMPROVEMENTS-PLAN.md): estimativa best-effort a partir do
//     volume REAL de uso (Azure Monitor, métrica UsedCapacity) × preço de Blob Storage por
//     access tier — ver priceStorageAccount. Só cobre kind StorageV2/Storage/BlobStorage
//     (validado ao vivo contra 2 Storage Accounts reais); FileStorage/BlockBlobStorage caem no
//     fallback "não estimado". Nunca inclui transações/banda (não observáveis via essa métrica).
//   - Azure SQL Database/Server: modelo DTU vs vCore vs Serverless varia demais por recurso pra
//     confiar num único filtro de preço sem validação ao vivo contra um tenant real — NÃO
//     estimado nesta versão.
//   - Cosmos DB (F3.2 do FINOPS-IMPROVEMENTS-PLAN.md, avaliado e NÃO implementado nesta versão):
//     cobrado por RU/s provisionado (ou serverless por request), configurado por banco/container,
//     não visível no nível da conta via `az resource list`. Diferente de Storage Account, essa
//     fase NUNCA pôde ser validada ao vivo — o tenant usado nesta investigação não tem NENHUMA
//     conta Cosmos DB provisionada (confirmado via `az resource list --query
//     "[?type=='Microsoft.DocumentDB/databaseAccounts']"`, lista vazia) — implementar um caminho
//     de preço (via `az cosmosdb sql database/container throughput show` + Retail Prices API RU/s)
//     sem nenhum recurso real pra confirmar a sintaxe/unidades contrariaria a disciplina de
//     validação ao vivo já seguida no resto deste arquivo. Mantido como "não estimado" honesto.
func PriceDataResources(ctx context.Context, resources []AzureDataResource, vmPricer *AzurePricer, diskPricer *DiskPricer, subscription string, rate float64) []AzureDataResource {
	out := make([]AzureDataResource, len(resources))
	copy(out, resources)

	for i := range out {
		r := &out[i]
		t := strings.ToLower(r.Type)
		region := normalizeRetailRegion(r.Location)

		switch {
		case t == "microsoft.compute/virtualmachines":
			priceVirtualMachine(r, vmPricer, rate)
		case t == "microsoft.compute/disks":
			priceManagedDisk(r, diskPricer, region, rate)
		case t == "microsoft.cache/redis":
			priceRedisCache(r, region, rate)
		case t == "microsoft.servicebus/namespaces" || t == "microsoft.eventhub/namespaces":
			priceMessagingNamespace(r, region, rate)
		case strings.HasPrefix(t, "microsoft.dbforpostgresql/") || strings.HasPrefix(t, "microsoft.dbformysql/") || t == "microsoft.dbformariadb/servers":
			priceFlexibleServer(r, region, rate)
		case t == "microsoft.storage/storageaccounts":
			priceStorageAccount(ctx, r, subscription, rate)
		case t == "microsoft.sql/servers/databases":
			r.PricingNote = "Modelo de cobrança (DTU/vCore/Serverless) varia por database — não estimado automaticamente nesta versão."
		case t == "microsoft.sql/servers":
			r.PricingNote = "O SQL Server em si não tem custo próprio — o custo está nas databases dentro dele (listadas separadamente)."
		case t == "microsoft.documentdb/databaseaccounts":
			r.PricingNote = "Cobrado por RU/s provisionado (ou por request no modo serverless), configurado por banco/container — não visível neste nível, não estimado automaticamente nesta versão."
		}
	}
	return out
}

// priceVirtualMachine reusa o MESMO AzurePricer.GetPrice já usado (e validado em produção) pra
// precificar node pools do AKS — uma VM self-hosted de banco é precificada exatamente igual a um
// node de cluster, é o mesmo SKU de VM Azure.
func priceVirtualMachine(r *AzureDataResource, vmPricer *AzurePricer, rate float64) {
	if vmPricer == nil || r.SKUName == "" {
		r.PricingNote = "vmSize não disponível — não foi possível estimar o custo."
		return
	}
	hourly, _, err := vmPricer.GetPrice(r.SKUName)
	if err != nil {
		r.PricingNote = fmt.Sprintf("Preço não encontrado pra VM SKU '%s' — não estimado (%s).", r.SKUName, err.Error())
		return
	}
	monthlyUSD := round2(hourly * hoursPerMonth)
	r.MonthlyCostUSD = monthlyUSD
	r.MonthlyCostBRL = round2(monthlyUSD * rate)
	r.PriceSource = "api"
}

// priceManagedDisk reusa o MESMO DiskPricer já usado pra precificar disco OS de node pool e PVCs
// — MapStorageClassToAzureType converte o SKU cru do disco (ex: "Standard_LRS") pro tipo interno
// ("Standard HDD"), ResolveManagedDiskTier resolve o tier de capacidade (ex: "S10") a partir do
// tamanho real em GB (`az disk list` devolve isso direto, diferente de PVC via K8s StorageClass).
func priceManagedDisk(r *AzureDataResource, diskPricer *DiskPricer, region string, rate float64) {
	if diskPricer == nil || r.SKUName == "" || r.SizeGB <= 0 {
		r.PricingNote = "SKU ou tamanho do disco não disponível — não foi possível estimar o custo."
		return
	}
	azureType, _ := MapStorageClassToAzureType("", "", r.SKUName)
	tier := ResolveManagedDiskTier(azureType, r.SizeGB)
	monthlyUSD, _, err := diskPricer.GetDiskPrice(azureType, tier, region)
	if err != nil {
		r.PricingNote = fmt.Sprintf("Preço não encontrado pra disco %s %s — não estimado (%s).", azureType, tier, err.Error())
		return
	}
	r.MonthlyCostUSD = round2(monthlyUSD)
	r.MonthlyCostBRL = round2(monthlyUSD * rate)
	r.PriceSource = "api"
}

// flexServerSKURe casa o padrão de nome de SKU de compute Azure usado por Flexible Server —
// "<Família><vCores><sufixo>_v<geração>" (ex: "D2ds_v5" → família "D", 2 vCores, sufixo "ds",
// geração "v5") — usado pra reconstruir o "family" tal como aparece no productName da Retail
// Prices API ("Ddsv5"). Séries que não seguem esse padrão (M-series "M64ds_v2", Burstable
// "B4ms") não casam aqui — ver fallback por nome cru em priceFlexibleServer.
var flexServerSKURe = regexp.MustCompile(`(?i)^([A-Za-z]+)(\d+)([A-Za-z]*)_v(\d+)$`)

// parseFlexServerFamily extrai (family, vCores) de um SKU de compute (já sem "Standard_"), ex:
// "D2ds_v5" → ("Ddsv5", 2). ok=false quando o SKU não segue o padrão reconhecido (M-series,
// Burstable) — nesse caso o chamador cai no fallback de nome cru.
func parseFlexServerFamily(sku string) (family string, vcores int, ok bool) {
	m := flexServerSKURe.FindStringSubmatch(sku)
	if m == nil {
		return "", 0, false
	}
	letter, digits, trailing, gen := m[1], m[2], m[3], m[4]
	n, err := strconv.Atoi(digits)
	if err != nil {
		return "", 0, false
	}
	return fmt.Sprintf("%s%sv%s", letter, trailing, gen), n, true
}

// priceFlexibleServer estima o custo de COMPUTE (vCore/hora) de um Azure Database for
// PostgreSQL/MySQL Flexible ou Single Server — validado ao vivo contra a Retail Prices API real
// (não um filtro às cegas): o campo `skuName` do catálogo NÃO é o nome do SKU do recurso — pras
// famílias comuns (General Purpose/Memory Optimized) é só a contagem de vCores ("2 vCore"), com
// a família/geração (ex: "Ddsv5") só aparecendo dentro do `productName`; pra M-series e Burstable
// o `skuName` volta a ser o código cru do SKU (ex: "M64ds_v2", "B4ms"). Por isso a busca é em 2
// passos: (1) filtro amplo por família no `productName` (funciona pra General Purpose/Memory
// Optimized); (2) entre os resultados, casa `skuName` contra "<N> vCore" (case-insensitive) OU
// contra o SKU cru (cobre M-series/Burstable, cujo productName não expõe uma família parseável
// do jeito acima). Não inclui custo de storage/IOPS provisionado (mesma limitação já documentada
// pra Storage Accounts — cobrado por GB, sem dado de volume aqui).
func priceFlexibleServer(r *AzureDataResource, region string, rate float64) {
	if r.SKUName == "" {
		r.PricingNote = "SKU de compute não disponível — não foi possível estimar o custo (nota: o custo de storage provisionado também não é coberto nesta versão)."
		return
	}

	serviceName := "Azure Database for PostgreSQL"
	t := strings.ToLower(r.Type)
	if strings.Contains(t, "mysql") {
		serviceName = "Azure Database for MySQL"
	} else if strings.Contains(t, "mariadb") {
		serviceName = "Azure Database for MariaDB"
	}

	rawSKU := strings.TrimPrefix(r.SKUName, "Standard_")
	rawSKU = strings.TrimPrefix(rawSKU, "standard_")
	family, vcores, hasFamily := parseFlexServerFamily(rawSKU)

	var filter string
	if hasFamily {
		filter = fmt.Sprintf(
			"serviceName eq '%s' and armRegionName eq '%s' and priceType eq 'Consumption' and contains(productName, '%s')",
			serviceName, region, family,
		)
	} else {
		// M-series/Burstable: sem família parseável no formato esperado — busca só por
		// serviceName+região e casa o skuName cru abaixo (catálogo menor por família não é
		// filtrável aqui, mas o serviço inteiro ainda é uma lista administrável).
		filter = fmt.Sprintf(
			"serviceName eq '%s' and armRegionName eq '%s' and priceType eq 'Consumption' and contains(skuName, '%s')",
			serviceName, region, rawSKU,
		)
	}

	items, err := fetchRetailPriceItems(filter)
	if err != nil {
		r.PricingNote = fmt.Sprintf("Falha ao consultar a Retail Prices API — não estimado (%s). Custo de storage provisionado também não coberto nesta versão.", err.Error())
		return
	}

	vcoreLabel := fmt.Sprintf("%d vCore", vcores)
	var hourly float64
	for _, it := range items {
		if it.UnitOfMeasure != "1 Hour" {
			continue
		}
		matches := (hasFamily && strings.EqualFold(it.SKUName, vcoreLabel)) || strings.EqualFold(it.SKUName, rawSKU)
		if !matches {
			continue
		}
		if it.RetailPrice > 0 && (hourly == 0 || it.RetailPrice < hourly) {
			hourly = it.RetailPrice
		}
	}

	if hourly == 0 {
		r.PricingNote = fmt.Sprintf("Nenhum preço de compute encontrado na Retail Prices API pra SKU '%s' na região %s — não estimado. Custo de storage provisionado também não coberto nesta versão.", r.SKUName, region)
		return
	}

	monthlyUSD := round2(hourly * hoursPerMonth)
	r.MonthlyCostUSD = monthlyUSD
	r.MonthlyCostBRL = round2(monthlyUSD * rate)
	r.PriceSource = "api"
	r.PricingNote = "Só compute (vCore/hora) — custo de storage provisionado não coberto nesta versão."
}

// priceStorageAccount (F3.1 do FINOPS-IMPROVEMENTS-PLAN.md) — estimativa best-effort a partir do
// uso REAL de armazenamento (Azure Monitor, métrica UsedCapacity — soma Blob+File+Table+Queue da
// conta inteira, confirmado ao vivo) × preço de "Data Stored" pra Blob Storage no access tier
// configurado (Hot/Cool/Cold + redundância). Achado real, confirmado ao vivo: a Retail Prices API
// tem preço TIERED por volume pra Data Stored (3 faixas: 0-50TB/50-500TB/500TB+, cada uma mais
// barata que a anterior) — pickTieredPrice escolhe a faixa certa pro volume em uso.
//
// Nunca inclui transações/banda (não observáveis via essa métrica isolada) — sempre PriceSource
// "estimated" (não "api", diferente de VM/disco/Redis/etc.) pra deixar claro que é uma
// aproximação, não um preço fixo por SKU. Cobertura: só kind StorageV2/Storage/BlobStorage — as
// convenções onde "Block Blob"/"Blob Storage" é o produto certo pra Data Stored (as 2 Storage
// Accounts reais encontradas nesta investigação eram ambas StorageV2); FileStorage/
// BlockBlobStorage (nunca confirmados ao vivo) caem no fallback "não estimado" honesto.
func priceStorageAccount(ctx context.Context, r *AzureDataResource, subscription string, rate float64) {
	const unavailableSuffix = " (transações/banda também não incluídas nesta versão)."

	if r.ResourceID == "" {
		r.PricingNote = "Cobrado por GB armazenado + transações + banda — requer volume real de uso (Azure Monitor), não estimado automaticamente nesta versão."
		return
	}

	kind := strings.ToLower(r.Kind)
	if kind != "storagev2" && kind != "storage" && kind != "blobstorage" {
		r.PricingNote = fmt.Sprintf("Kind '%s' fora da cobertura de estimativa automática desta versão%s", r.Kind, unavailableSuffix)
		return
	}

	resourceGroup := resourceGroupFromID(r.ResourceID)
	accessTier, err := getStorageAccountAccessTier(ctx, resourceGroup, r.Name, subscription)
	if err != nil || accessTier == "" {
		note := "Não foi possível determinar o access tier (Hot/Cool/Cold)"
		if err != nil {
			note += ": " + err.Error()
		}
		r.PricingNote = note + unavailableSuffix
		return
	}

	usedBytes, ok, err := getResourceMetricAverage(ctx, r.ResourceID, "UsedCapacity")
	if err != nil || !ok {
		note := "Não foi possível obter o volume real de uso via Azure Monitor"
		if err != nil {
			note += ": " + err.Error()
		}
		r.PricingNote = note + unavailableSuffix
		return
	}
	usedGB := usedBytes / (1024 * 1024 * 1024)

	redundancy := storageRedundancyFromSKU(r.SKUName)
	if redundancy == "" {
		redundancy = "LRS"
	}
	skuLabel := accessTier + " " + redundancy
	region := normalizeRetailRegion(r.Location)

	filter := fmt.Sprintf(
		"serviceName eq 'Storage' and armRegionName eq '%s' and contains(productName, 'Block Blob') and skuName eq '%s' and contains(meterName, 'Data Stored')",
		region, skuLabel,
	)
	items, err := fetchRetailPriceItems(filter)
	if err != nil {
		r.PricingNote = "Falha ao consultar a Retail Prices API" + unavailableSuffix + " (" + err.Error() + ")"
		return
	}

	pricePerGB, priceOk := pickTieredPrice(items, usedGB)
	if !priceOk {
		r.PricingNote = fmt.Sprintf("Nenhum preço de Blob %s encontrado na região %s%s", skuLabel, region, unavailableSuffix)
		return
	}

	monthlyUSD := round2(pricePerGB * usedGB)
	r.MonthlyCostUSD = monthlyUSD
	r.MonthlyCostBRL = round2(monthlyUSD * rate)
	r.PriceSource = "estimated"
	r.PricingNote = fmt.Sprintf(
		"Estimativa aproximada: %.2f GB em uso (Azure Monitor, média das últimas 48h) × preço de Blob %s — NÃO inclui transações/banda; assume que o uso é majoritariamente Blob (contas GPv2 também podem ter File/Table/Queue com preço próprio, não discriminado nesta métrica).",
		usedGB, skuLabel,
	)
}

// storageRedundancyFromSKU extrai a redundância (LRS/GRS/ZRS/GZRS/RAGRS) do SKU cru da Storage
// Account (ex: "Standard_LRS" → "LRS") — mesmo formato usado pelo sufixo do skuName da Retail
// Prices API ("Hot LRS", "Cool GRS").
func storageRedundancyFromSKU(sku string) string {
	sku = strings.TrimPrefix(sku, "Standard_")
	sku = strings.TrimPrefix(sku, "standard_")
	sku = strings.TrimPrefix(sku, "Premium_")
	sku = strings.TrimPrefix(sku, "premium_")
	return strings.ToUpper(sku)
}

// pickTieredPrice escolhe, entre itens de preço "Data Stored" (Retail Prices API, cobrança tiered
// por volume — ver priceStorageAccount), o preço por GB/mês da faixa correta pro volume em uso: a
// maior tierMinimumUnits que ainda seja <= usedGB. Confirmado ao vivo contra a API real (3 faixas:
// 0/51200/512000 GB, preços decrescentes). ok=false quando nenhum item de "1 GB/Month" bate.
func pickTieredPrice(items []retailPriceItem, usedGB float64) (pricePerGB float64, ok bool) {
	bestTier := -1.0
	for _, it := range items {
		if it.UnitOfMeasure != "1 GB/Month" || it.RetailPrice <= 0 {
			continue
		}
		if it.TierMinimumUnits > usedGB {
			continue
		}
		if it.TierMinimumUnits > bestTier {
			bestTier = it.TierMinimumUnits
			pricePerGB = it.RetailPrice
			ok = true
		}
	}
	return pricePerGB, ok
}

// normalizeRetailRegion converte o `location` do recurso (ex: "brazilsouth") pro armRegionName
// esperado pela Retail Prices API — na prática já é o mesmo valor pra quase todas as regiões
// Azure (location já vem em minúsculo sem espaço), então só normaliza minúsculo por segurança.
func normalizeRetailRegion(location string) string {
	if location == "" {
		return defaultPricingRegion
	}
	return strings.ToLower(strings.TrimSpace(location))
}

// priceRedisCache estima o custo mensal de uma instância Azure Cache for Redis a partir do SKU
// (tier + family + capacity, ex: Standard/C/1 → "C1"). Retail Prices API: serviceName "Redis
// Cache", productName varia por tier ("Redis Cache Standard"/"Redis Cache Premium"/"Redis Cache
// Basic"), meterName inclui o tamanho ("C1 Cache Instance").
func priceRedisCache(r *AzureDataResource, region string, rate float64) {
	if r.SKUName == "" {
		r.PricingNote = "SKU não disponível — não foi possível estimar o custo."
		return
	}
	sizeLabel := redisSizeLabel(r.SKUTier, r.SKUName)
	if sizeLabel == "" {
		r.PricingNote = fmt.Sprintf("Tamanho de instância não reconhecido (tier=%s, sku=%s) — não estimado.", r.SKUTier, r.SKUName)
		return
	}

	filter := fmt.Sprintf(
		"serviceName eq 'Redis Cache' and armRegionName eq '%s' and priceType eq 'Consumption' and contains(meterName, '%s')",
		region, sizeLabel,
	)
	items, err := fetchRetailPriceItems(filter)
	if err != nil {
		r.PricingNote = "Falha ao consultar a Retail Prices API — não estimado (" + err.Error() + ")."
		return
	}

	var hourly float64
	for _, it := range items {
		if it.UnitOfMeasure != "1 Hour" {
			continue
		}
		// Confirma que o meterName é EXATAMENTE o tamanho buscado (não um prefixo que também
		// bate via contains, ex: "C1" não deveria casar com "C10" se essa família existisse).
		if !meterMatchesRedisSize(it.MeterName, sizeLabel) {
			continue
		}
		if it.RetailPrice > 0 && (hourly == 0 || it.RetailPrice < hourly) {
			hourly = it.RetailPrice
		}
	}

	if hourly == 0 {
		r.PricingNote = fmt.Sprintf("Nenhum preço encontrado na Retail Prices API pra Redis %s na região %s — não estimado.", sizeLabel, region)
		return
	}

	monthlyUSD := round2(hourly * hoursPerMonth)
	r.MonthlyCostUSD = monthlyUSD
	r.MonthlyCostBRL = round2(monthlyUSD * rate)
	r.PriceSource = "api"
}

// redisSizeLabel converte tier+SKU name em rótulo de tamanho ("C0".."C6" pra Basic/Standard,
// "P1".."P5" pra Premium) — mesma convenção usada nos nomes de meter da Retail Prices API.
func redisSizeLabel(tier, skuName string) string {
	tier = strings.ToLower(tier)
	family := "C"
	if strings.Contains(tier, "premium") {
		family = "P"
	}
	// capacity nem sempre vem separado do skuName — tenta extrair um dígito final de skuName
	// (ex: "P1"/"C1" já no próprio nome) antes de desistir.
	for _, tok := range []string{skuName, tier} {
		if n, ok := extractTrailingDigits(tok); ok {
			return fmt.Sprintf("%s%d", family, n)
		}
	}
	return ""
}

func extractTrailingDigits(s string) (int, bool) {
	i := len(s)
	for i > 0 && s[i-1] >= '0' && s[i-1] <= '9' {
		i--
	}
	if i == len(s) {
		return 0, false
	}
	n, err := strconv.Atoi(s[i:])
	if err != nil {
		return 0, false
	}
	return n, true
}

func meterMatchesRedisSize(meterName, sizeLabel string) bool {
	// meterName real observado na documentação pública: "C1 Cache Instance" — o tamanho é
	// sempre o primeiro token separado por espaço.
	fields := strings.Fields(meterName)
	return len(fields) > 0 && strings.EqualFold(fields[0], sizeLabel)
}

// priceMessagingNamespace estima o custo de um namespace Service Bus/Event Hub — SÓ pro tier
// Premium (Messaging Units, capacity-based, cobrança fixa por hora). Basic/Standard são
// majoritariamente cobrados por operação (consumo) — nunca estimados aqui, ver PricingNote.
func priceMessagingNamespace(r *AzureDataResource, region string, rate float64) {
	tier := strings.ToLower(r.SKUTier)
	if !strings.Contains(tier, "premium") {
		svcLabel := "Service Bus"
		if strings.Contains(strings.ToLower(r.Type), "eventhub") {
			svcLabel = "Event Hubs"
		}
		r.PricingNote = fmt.Sprintf("%s tier %s é cobrado majoritariamente por operação/throughput consumido — não estimado automaticamente nesta versão.", svcLabel, r.SKUTier)
		return
	}

	capacity := 1
	if r.SKUName != "" {
		if n, ok := extractTrailingDigits(r.SKUName); ok && n > 0 {
			capacity = n
		}
	}

	serviceName := "Service Bus"
	if strings.Contains(strings.ToLower(r.Type), "eventhub") {
		serviceName = "Event Hubs"
	}
	filter := fmt.Sprintf(
		"serviceName eq '%s' and armRegionName eq '%s' and priceType eq 'Consumption' and contains(skuName, 'Premium')",
		serviceName, region,
	)
	items, err := fetchRetailPriceItems(filter)
	if err != nil {
		r.PricingNote = "Falha ao consultar a Retail Prices API — não estimado (" + err.Error() + ")."
		return
	}

	var hourlyPerUnit float64
	for _, it := range items {
		if it.UnitOfMeasure != "1 Hour" {
			continue
		}
		if !strings.Contains(strings.ToLower(it.MeterName), "messaging unit") {
			continue
		}
		if it.RetailPrice > 0 && (hourlyPerUnit == 0 || it.RetailPrice < hourlyPerUnit) {
			hourlyPerUnit = it.RetailPrice
		}
	}

	if hourlyPerUnit == 0 {
		r.PricingNote = fmt.Sprintf("Nenhum preço encontrado na Retail Prices API pra %s Premium na região %s — não estimado.", serviceName, region)
		return
	}

	monthlyUSD := round2(hourlyPerUnit * float64(capacity) * hoursPerMonth)
	r.MonthlyCostUSD = monthlyUSD
	r.MonthlyCostBRL = round2(monthlyUSD * rate)
	r.PriceSource = "api"
}

const hoursPerMonth = 730.0 // mesma convenção (média de horas/mês) já usada pro resto do FinOps

// retailPriceItem/retailPriceResponse — mesmo shape de azurePriceItem/azurePricingResponse em
// azure_pricing.go, mas não reaproveitado diretamente (esse tipo é privado ao AzurePricer, e
// pricing de recursos de dados não passa pelo cache SQLite de VM — ver comentário de
// PriceDataResources acima).
type retailPriceItem struct {
	SKUName       string  `json:"skuName"`
	RetailPrice   float64 `json:"retailPrice"`
	UnitOfMeasure string  `json:"unitOfMeasure"`
	ProductName   string  `json:"productName"`
	MeterName     string  `json:"meterName"`
	// TierMinimumUnits — só relevante pra preço TIERED por volume (ex: Storage "Data Stored",
	// que tem 3 faixas de preço decrescente por GB armazenado) — ver pickTieredPrice.
	TierMinimumUnits float64 `json:"tierMinimumUnits"`
}

type retailPriceResponse struct {
	Items []retailPriceItem `json:"Items"`
}

// fetchRetailPriceItems consulta a Azure Retail Prices API com um filtro OData arbitrário —
// generalização de AzurePricer.fetchFromAPI (azure_pricing.go) pra qualquer serviço, não só
// Virtual Machines. Sem cache (ver comentário de PriceDataResources).
func fetchRetailPriceItems(filter string) ([]retailPriceItem, error) {
	reqURL := azurePricingAPIURL + "?api-version=2023-01-01-preview&$filter=" + url.QueryEscape(filter)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(reqURL)
	if err != nil {
		return nil, fmt.Errorf("requisição Retail Prices API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Retail Prices API retornou status %d", resp.StatusCode)
	}

	var result retailPriceResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decodificar resposta Retail Prices API: %w", err)
	}

	log.Debug().Str("filter", filter).Int("items", len(result.Items)).Msg("FinOps/DataResources: Retail Prices API consultada")
	return result.Items, nil
}
