package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// azureRedisHostRegex reconhece os dois formatos de hostname do Azure Cache for Redis — o nome do
// cache é sempre o primeiro label, independente do formato:
//   - Clássico (Basic/Standard/Premium): {cache}.redis.cache.windows.net
//   - Enterprise / Azure Managed Redis:  {cache}.{regiao}.redis.azure.net (formato usado no
//     comando `redis-cli -h ... --tls` que a própria Azure Portal sugere pra teste manual)
var azureRedisHostRegex = regexp.MustCompile(`(?i)^([a-z0-9-]+)\.(?:redis\.cache\.windows\.net|[a-z0-9-]+\.redis\.azure\.net)$`)

// isAzureRedisHost devolve o nome do cache quando o host bate com um dos dois formatos de DNS do
// Azure Cache for Redis — usado pra decidir se vale tentar buscar o tier via Azure CLI (não faz
// sentido tentar pra Redis self-hosted ou de outras clouds).
func isAzureRedisHost(host string) (cacheName string, ok bool) {
	m := azureRedisHostRegex.FindStringSubmatch(strings.TrimSpace(host))
	if m == nil {
		return "", false
	}
	return m[1], true
}

// AzureRedisTierInfo é a resposta de GET /api/v1/db-test/redis-azure-tier — dados do recurso
// Azure (SKU/tier, shards, versão) que o próprio protocolo Redis (comando INFO) não expõe, por
// serem propriedades do Azure Resource Manager, não do servidor Redis em si.
type AzureRedisTierInfo struct {
	Found bool `json:"found"`
	// ResourceType diferencia o clássico (Basic/Standard/Premium) do Enterprise/Azure Managed
	// Redis — os dois usam tipos de recurso ARM diferentes, com formatos de SKU incompatíveis
	// entre si (família+capacidade vs. nome de SKU já combinado).
	ResourceType  string `json:"resource_type,omitempty"`
	SKUName       string `json:"sku_name,omitempty"`
	SKUFamily     string `json:"sku_family,omitempty"`
	SKUCapacity   int    `json:"sku_capacity,omitempty"`
	TierLabel     string `json:"tier_label,omitempty"`
	Location      string `json:"location,omitempty"`
	ResourceGroup string `json:"resource_group,omitempty"`
	Subscription  string `json:"subscription,omitempty"`
	ShardCount    int    `json:"shard_count,omitempty"`
	RedisVersion  string `json:"redis_version,omitempty"`
	// Error é sempre uma mensagem amigável — az CLI ausente, não autenticado, cache não encontrado
	// em nenhuma subscription acessível, etc. Nunca bloqueia o teste de conectividade em si (essa
	// busca é só um extra informativo pro modal de saída bruta).
	Error string `json:"error,omitempty"`
}

// azureRedisTierCacheTTL — mesmo padrão de cache de CLI externa documentado no CLAUDE.md (ex.
// IsGcloudAuthActive, ListNodeGroups): TTL bem maior que o de docker-status (20s) porque o tier de
// um cache Azure só muda numa operação manual de scale, não a cada poucos segundos.
const azureRedisTierCacheTTL = 30 * time.Minute

var (
	azureRedisTierMu    sync.Mutex
	azureRedisTierCache = map[string]azureRedisTierCacheEntry{}
)

type azureRedisTierCacheEntry struct {
	info    AzureRedisTierInfo
	expires time.Time
}

// RedisAzureTier — GET /api/v1/db-test/redis-azure-tier?host=<host>. Leitura informacional best-
// effort (não destrutiva, não amarrada a um cluster K8s específico) — sem RequireSREGroup(),
// mesmo padrão de DockerStatus.
func (h *DBTestHandler) RedisAzureTier(c *gin.Context) {
	host := strings.TrimSpace(c.Query("host"))
	if host == "" {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_HOST", "host é obrigatório"))
		return
	}

	cacheName, ok := isAzureRedisHost(host)
	if !ok {
		c.JSON(http.StatusOK, AzureRedisTierInfo{Found: false})
		return
	}

	azureRedisTierMu.Lock()
	if entry, exists := azureRedisTierCache[host]; exists && time.Now().Before(entry.expires) {
		azureRedisTierMu.Unlock()
		c.JSON(http.StatusOK, entry.info)
		return
	}
	azureRedisTierMu.Unlock()

	info := lookupAzureRedisTier(c.Request.Context(), host, cacheName)

	azureRedisTierMu.Lock()
	azureRedisTierCache[host] = azureRedisTierCacheEntry{info: info, expires: time.Now().Add(azureRedisTierCacheTTL)}
	azureRedisTierMu.Unlock()

	c.JSON(http.StatusOK, info)
}

// azureRedisResourceTypes — os dois tipos de recurso ARM que podem hospedar um Azure Cache for
// Redis, verificados em paralelo já que o hostname sozinho não diz qual é (Enterprise/Azure
// Managed Redis também pode, em teoria, ser exposto sob o domínio clássico dependendo da versão
// do produto — mais barato tentar os dois que tentar adivinhar pelo formato do host).
var azureRedisResourceTypes = []string{"Microsoft.Cache/Redis", "Microsoft.Cache/redisEnterprise"}

const (
	azureRedisTierScanTimeout     = 45 * time.Second // teto total (todas as subscriptions)
	azureRedisTierSubTimeout      = 12 * time.Second // teto por chamada individual de az CLI
	azureRedisTierScanConcurrency = 8                // mesmo teto do scan de frota do Access Checker
)

type azureSubscription struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// listAzureSubscriptionsForRedisLookup lista as subscriptions Azure acessíveis via CLI já
// autenticada no servidor. Diferente de loadAllAzureSubscriptions (kubeconfig.go, usado pelo
// autodiscover), não tenta `az login` automaticamente — essa busca é um extra best-effort dentro
// de uma requisição HTTP síncrona, não um fluxo interativo de descoberta; se não estiver
// autenticado, só reporta o erro e desiste.
func listAzureSubscriptionsForRedisLookup(ctx context.Context) ([]azureSubscription, error) {
	listCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	out, err := exec.CommandContext(listCtx, "az", "account", "list",
		"--query", "[].{id:id,name:name}", "-o", "json", "--only-show-errors").Output()
	if err != nil {
		return nil, fmt.Errorf("Azure CLI não autenticada ou indisponível no servidor (execute 'az login')")
	}

	var entries []azureSubscription
	if jsonErr := json.Unmarshal(out, &entries); jsonErr != nil {
		return nil, fmt.Errorf("falha ao interpretar subscriptions do az CLI: %w", jsonErr)
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("nenhuma subscription Azure acessível")
	}
	return entries, nil
}

// azureResourceListEntry é o subconjunto de `az resource list` usado pra filtrar candidatos por
// nome antes do `az resource show` mais caro (que traz hostName, só disponível no properties bag
// completo — não incluído no list genérico).
type azureResourceListEntry struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Location      string `json:"location"`
	ResourceGroup string `json:"resourceGroup"`
	Sku           *struct {
		Name     string `json:"name"`
		Family   string `json:"family"`
		Capacity int    `json:"capacity"`
	} `json:"sku"`
}

// lookupAzureRedisTier varre, em paralelo, todas as subscriptions Azure acessíveis procurando um
// recurso Microsoft.Cache/Redis (clássico) ou Microsoft.Cache/redisEnterprise (Enterprise/Azure
// Managed Redis) cujo `hostName` bate com o host informado. Mesmo espírito de paralelismo/timeout
// do scan de frota do Access Checker (ver ACCESS-CHECK-PLAN.md) — best-effort: falha de
// autenticação, timeout ou cache não encontrado devolvem Found=false com Error preenchido, nunca
// erro HTTP (essa busca é só um extra informativo, não deve derrubar o teste de conectividade).
func lookupAzureRedisTier(ctx context.Context, host, cacheName string) AzureRedisTierInfo {
	scanCtx, cancel := context.WithTimeout(ctx, azureRedisTierScanTimeout)
	defer cancel()

	subs, err := listAzureSubscriptionsForRedisLookup(scanCtx)
	if err != nil {
		return AzureRedisTierInfo{Found: false, Error: err.Error()}
	}

	resultCh := make(chan AzureRedisTierInfo, len(subs)*len(azureRedisResourceTypes))
	sem := make(chan struct{}, azureRedisTierScanConcurrency)
	var wg sync.WaitGroup

	for _, sub := range subs {
		for _, resourceType := range azureRedisResourceTypes {
			wg.Add(1)
			go func(sub azureSubscription, resourceType string) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				if info, found := findAzureRedisResource(scanCtx, sub, resourceType, cacheName, host); found {
					resultCh <- info
				}
			}(sub, resourceType)
		}
	}

	go func() {
		wg.Wait()
		close(resultCh)
	}()

	for info := range resultCh {
		return info
	}
	return AzureRedisTierInfo{Found: false, Error: "cache não encontrado em nenhuma subscription acessível"}
}

// findAzureRedisResource procura, numa única subscription+tipo de recurso, um cache cujo nome
// bata com cacheName e cujo hostName (só disponível via `az resource show`, não no list genérico)
// bata exatamente com host. Devolve (info, false) em qualquer falha/não-match — best-effort, erros
// de uma subscription não devem interromper a busca nas demais.
func findAzureRedisResource(ctx context.Context, sub azureSubscription, resourceType, cacheName, host string) (AzureRedisTierInfo, bool) {
	listCtx, listCancel := context.WithTimeout(ctx, azureRedisTierSubTimeout)
	defer listCancel()

	out, err := exec.CommandContext(listCtx, "az", "resource", "list",
		"--resource-type", resourceType,
		"--subscription", sub.ID,
		"--query", fmt.Sprintf("[?name=='%s']", cacheName),
		"-o", "json", "--only-show-errors").Output()
	if err != nil {
		return AzureRedisTierInfo{}, false
	}

	var candidates []azureResourceListEntry
	if json.Unmarshal(out, &candidates) != nil || len(candidates) == 0 {
		return AzureRedisTierInfo{}, false
	}

	for _, cand := range candidates {
		showCtx, showCancel := context.WithTimeout(ctx, azureRedisTierSubTimeout)
		showOut, showErr := exec.CommandContext(showCtx, "az", "resource", "show",
			"--ids", cand.ID, "--subscription", sub.ID, "-o", "json", "--only-show-errors").Output()
		showCancel()
		if showErr != nil {
			continue
		}

		var full struct {
			Properties struct {
				HostName     string `json:"hostName"`
				ShardCount   int    `json:"shardCount"`
				RedisVersion string `json:"redisVersion"`
			} `json:"properties"`
		}
		if json.Unmarshal(showOut, &full) != nil {
			continue
		}
		if !strings.EqualFold(full.Properties.HostName, host) {
			continue
		}

		info := AzureRedisTierInfo{
			Found:         true,
			ResourceType:  resourceType,
			Location:      cand.Location,
			ResourceGroup: cand.ResourceGroup,
			Subscription:  sub.Name,
			ShardCount:    full.Properties.ShardCount,
			RedisVersion:  full.Properties.RedisVersion,
		}
		if cand.Sku != nil {
			info.SKUName = cand.Sku.Name
			info.SKUFamily = cand.Sku.Family
			info.SKUCapacity = cand.Sku.Capacity
			info.TierLabel = formatAzureRedisTierLabel(resourceType, cand.Sku.Name, cand.Sku.Family, cand.Sku.Capacity)
		}
		return info, true
	}
	return AzureRedisTierInfo{}, false
}

// formatAzureRedisTierLabel monta o rótulo do tier do jeito que a própria Azure Portal exibe:
// "{Família}{Capacidade}" pro clássico (ex: "Premium P1", "Standard C2"); pro Enterprise/Azure
// Managed Redis o SKU.Name já vem pronto (ex: "Enterprise_E10") — só anexa a contagem de nós
// quando capacity > 0.
func formatAzureRedisTierLabel(resourceType, skuName, skuFamily string, capacity int) string {
	if resourceType == "Microsoft.Cache/redisEnterprise" {
		if capacity > 0 {
			return fmt.Sprintf("%s (%d nó(s))", skuName, capacity)
		}
		return skuName
	}
	if skuFamily != "" {
		return fmt.Sprintf("%s %s%d", skuName, skuFamily, capacity)
	}
	return skuName
}
