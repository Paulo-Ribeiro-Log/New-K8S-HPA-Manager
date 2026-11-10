package handlers

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"

	"k8s-hpa-manager/internal/config"
)

// Busca por nome de storage na(s) subscription(s) dos clusters — o plano B de quando um nome
// (conta de armazenamento, disco, container, file share) não aparece em nenhum volume nem
// dependência dos clusters. Só leitura (GET no ARM pelo `az rest`), sempre a pedido.
//
// O texto da busca só filtra, no servidor, o que o ARM devolveu: nunca entra numa URL.

const (
	storageSearchTimeout    = 90 * time.Second
	storageSearchDeepOneTTL = 240 * time.Second // varredura de containers de UMA subscription (46 contas levaram ~87 s)
	storageSearchMaxPages   = 30                // páginas de listagem do ARM por tipo de recurso
	storageSearchMaxHits    = 50                // resultados por tipo
	storageSearchSubsPar    = 4                 // subscriptions consultadas em paralelo
	storageSearchEnrich     = 5                 // contas encontradas que ganham ciclo de vida e containers
	storageSearchDeepCap    = 30                // contas varridas por containers/shares no modo profundo, por subscription
	storageSearchDeepCapOne = 100               // idem, quando a busca é de UMA subscription só (a tela varre uma por vez)
	storageSearchDeepPar    = 6
	storageSearchContainers = 20 // containers listados por conta encontrada
)

var reStorageQuery = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{2,63}$`)

// SubscriptionInfo descreve uma subscription consultada e os clusters que a usam.
type SubscriptionInfo struct {
	ID       string   `json:"id"`
	Name     string   `json:"name,omitempty"`
	Clusters []string `json:"clusters"` // vazio = subscription que nenhum cluster usa

	// O que a listagem do ARM devolveu (antes de filtrar pelo nome): mostra, numa busca sem
	// resultado, que a subscription foi lida de fato.
	AccountsListed int `json:"accounts_listed"`
	DisksListed    int `json:"disks_listed"`
}

// LifecycleSummary é a política de ciclo de vida de uma conta encontrada.
type LifecycleSummary struct {
	HasPolicy bool            `json:"has_policy"`
	Rules     []LifecycleRule `json:"rules"`
	Error     string          `json:"error,omitempty"`
}

// StorageHit é um recurso encontrado na subscription.
type StorageHit struct {
	Type             string            `json:"type"` // storage_account | disk | container | file_share
	Name             string            `json:"name"`
	ResourceGroup    string            `json:"resource_group"`
	Location         string            `json:"location,omitempty"`
	SubscriptionID   string            `json:"subscription_id"`
	SubscriptionName string            `json:"subscription_name,omitempty"`
	Clusters         []string          `json:"clusters"`          // clusters que usam esta subscription
	AKSManaged       bool              `json:"aks_managed"`       // RG de infraestrutura do AKS (MC_…)
	Account          string            `json:"account,omitempty"` // container/share: a conta dona
	Kind             string            `json:"kind,omitempty"`
	SKU              string            `json:"sku,omitempty"`
	Tier             string            `json:"tier,omitempty"`
	SizeGB           string            `json:"size_gb,omitempty"`
	State            string            `json:"state,omitempty"`
	AttachedTo       string            `json:"attached_to,omitempty"`
	Containers       []string          `json:"containers,omitempty"` // conta: alguns containers
	Lifecycle        *LifecycleSummary `json:"lifecycle,omitempty"`
}

// StorageSearchResult é a resposta de GET /azure/storage-search.
type StorageSearchResult struct {
	Query         string             `json:"query"`
	Deep          bool               `json:"deep"`
	All           bool               `json:"all"` // incluiu as subscriptions sem cluster que o az enxerga
	Subscriptions []SubscriptionInfo `json:"subscriptions"`
	Results       []StorageHit       `json:"results"`
	Warnings      []string           `json:"warnings"`
	Truncated     bool               `json:"truncated,omitempty"`
	QueriedAt     string             `json:"queried_at"`
}

type AzureStorageSearchHandler struct {
	kubeManager *config.KubeConfigManager
}

func NewAzureStorageSearchHandler(km *config.KubeConfigManager) *AzureStorageSearchHandler {
	return &AzureStorageSearchHandler{kubeManager: km}
}

// Search — GET /azure/storage-search?q=<nome>[&cluster=<cluster>][&deep=true][&all=true][&subscription=<id>][&disks=false]
//
// Sem cluster, consulta todas as subscriptions dos clusters configurados. Com deep=true, além de
// contas e discos, varre os containers e file shares das contas (mais lento: uma chamada por conta).
func (h *AzureStorageSearchHandler) Search(c *gin.Context) {
	q := strings.TrimSpace(c.Query("q"))
	if !reStorageQuery.MatchString(q) {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": gin.H{"code": "INVALID_QUERY",
			"message": "Informe de 3 a 64 caracteres (letras, números, ponto, hífen ou sublinhado)."}})
		return
	}
	timeout := storageSearchTimeout
	if c.Query("deep") == "true" && c.Query("subscription") != "" {
		timeout = storageSearchDeepOneTTL
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), timeout)
	defer cancel()

	subs, err := h.resolveSubscriptions(ctx, c.Query("cluster"))
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"success": false, "error": gin.H{"code": "NO_SUBSCRIPTION", "message": err.Error()}})
		return
	}
	all := c.Query("all") == "true"
	var warns []string
	if all {
		var err error
		if subs, warns, err = addVisibleSubscriptions(ctx, subs); err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"success": false, "error": gin.H{"code": "AZURE_ERROR", "message": "Não consegui listar as subscriptions do az login: " + err.Error()}})
			return
		}
	}
	// subscription=<id[,id]>: varre só essas (a tela percorre uma por vez na busca em containers, para
	// cada chamada caber no prazo e uma falha não perder as demais).
	if sel := c.Query("subscription"); sel != "" {
		var err error
		if subs, err = selectSubscriptions(subs, sel); err != nil {
			c.JSON(http.StatusUnprocessableEntity, gin.H{"success": false, "error": gin.H{"code": "UNKNOWN_SUBSCRIPTION", "message": err.Error()}})
			return
		}
	}
	opts := searchOpts{SkipDisks: c.Query("disks") == "false"}
	if len(subs) == 1 {
		opts.DeepCap = storageSearchDeepCapOne
	}
	res := searchSubscriptionsOpts(ctx, subs, q, c.Query("deep") == "true", opts)
	res.All = all
	res.Warnings = append(append([]string{}, warns...), res.Warnings...) // nunca null no JSON: a tela faz .map
	c.JSON(http.StatusOK, gin.H{"success": true, "data": res})
}

// storageSearchMaxSubs limita quantas subscriptions sem cluster entram na busca "em todas".
const storageSearchMaxSubs = 40

// addVisibleSubscriptions acrescenta, às subscriptions dos clusters, as demais que o `az login` do
// servidor enxerga (GET /subscriptions, só as ativas). Elas ficam sem clusters: é exatamente o caso
// de um storage que mora numa subscription que nenhum cluster usa.
func addVisibleSubscriptions(ctx context.Context, known []SubscriptionInfo) ([]SubscriptionInfo, []string, error) {
	list, _, err := armList(ctx, armBase+"/subscriptions?api-version=2022-12-01")
	if err != nil {
		return known, nil, err
	}
	have := map[string]bool{}
	for _, k := range known {
		have[strings.ToLower(k.ID)] = true
	}
	out := append([]SubscriptionInfo{}, known...)
	var warns []string
	added := 0
	for _, it := range list {
		id := digStr(it, "subscriptionId")
		if id == "" || have[strings.ToLower(id)] || !strings.EqualFold(digStr(it, "state"), "Enabled") {
			continue
		}
		if added >= storageSearchMaxSubs {
			warns = append(warns, fmt.Sprintf("Mais de %d subscriptions sem cluster: só as %d primeiras foram consultadas.", storageSearchMaxSubs, storageSearchMaxSubs))
			break
		}
		out = append(out, SubscriptionInfo{ID: id, Name: digStr(it, "displayName"), Clusters: []string{}})
		have[strings.ToLower(id)] = true
		added++
	}
	return out, warns, nil
}

// resolveSubscriptions descobre as subscriptions (id, nome e clusters) pelo clusters-config.json.
func (h *AzureStorageSearchHandler) resolveSubscriptions(ctx context.Context, cluster string) ([]SubscriptionInfo, error) {
	byID := map[string]*SubscriptionInfo{}
	var order []string
	add := func(cfg config.ClusterConfig) {
		raw := cfg.SubscriptionID
		if raw == "" {
			raw = cfg.Subscription
		}
		if raw == "" {
			return
		}
		id, err := azSubscriptionID(ctx, raw)
		if err != nil || id == "" {
			return
		}
		si := byID[id]
		if si == nil {
			si = &SubscriptionInfo{ID: id, Name: cfg.Subscription}
			byID[id] = si
			order = append(order, id)
		}
		si.Clusters = append(si.Clusters, strings.TrimSuffix(cfg.Name, "-admin"))
	}
	if cluster != "" {
		cfg := h.kubeManager.GetClusterConfig(cluster)
		if cfg == nil {
			return nil, fmt.Errorf("o cluster %q não tem configuração com subscription — rode o autodiscover", cluster)
		}
		add(*cfg)
	} else {
		for _, cfg := range h.kubeManager.GetAllClusterConfigs() {
			add(cfg)
		}
	}
	if len(order) == 0 {
		return nil, fmt.Errorf("nenhuma subscription conhecida em clusters-config.json — rode o autodiscover")
	}
	out := make([]SubscriptionInfo, 0, len(order))
	for _, id := range order {
		sort.Strings(byID[id].Clusters)
		out = append(out, *byID[id])
	}
	return out, nil
}

// searchOpts ajusta a busca: a varredura de containers, uma subscription por vez, não precisa
// relistar os discos (já vieram na busca anterior) e pode olhar mais contas.
type searchOpts struct {
	SkipDisks bool
	DeepCap   int // 0 = storageSearchDeepCap
}

// searchSubscriptions faz a busca em todas as subscriptions (em paralelo limitado). Falha numa
// subscription vira aviso e não derruba as demais.
func searchSubscriptions(ctx context.Context, subs []SubscriptionInfo, q string, deep bool) *StorageSearchResult {
	return searchSubscriptionsOpts(ctx, subs, q, deep, searchOpts{})
}

func searchSubscriptionsOpts(ctx context.Context, subs []SubscriptionInfo, q string, deep bool, opts searchOpts) *StorageSearchResult {
	res := &StorageSearchResult{Query: q, Deep: deep, Subscriptions: subs, Results: []StorageHit{}, Warnings: []string{}, QueriedAt: time.Now().UTC().Format(time.RFC3339)}
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, storageSearchSubsPar)
	for i := range subs {
		wg.Add(1)
		go func(s *SubscriptionInfo) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			hits, warns, trunc := searchOneSubscription(ctx, s, q, deep, opts)
			mu.Lock()
			res.Results = append(res.Results, hits...)
			res.Warnings = append(res.Warnings, warns...)
			res.Truncated = res.Truncated || trunc
			mu.Unlock()
		}(&subs[i])
	}
	wg.Wait()
	sort.SliceStable(res.Results, func(i, j int) bool {
		a, b := res.Results[i], res.Results[j]
		if a.Type != b.Type {
			return storageTypeOrder(a.Type) < storageTypeOrder(b.Type)
		}
		return a.Name < b.Name
	})
	return res
}

func storageTypeOrder(t string) int {
	switch t {
	case "storage_account":
		return 0
	case "container":
		return 1
	case "file_share":
		return 2
	}
	return 3
}

func searchOneSubscription(ctx context.Context, s *SubscriptionInfo, q string, deep bool, opts searchOpts) (hits []StorageHit, warns []string, truncated bool) {
	label := s.Name
	if label == "" {
		label = s.ID
	}
	needle := strings.ToLower(q)
	base := armBase + "/subscriptions/" + url.PathEscape(s.ID)

	var accounts, disks []map[string]any
	var accErr, diskErr error
	var tr1, tr2 bool
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		accounts, tr1, accErr = armList(ctx, base+"/providers/Microsoft.Storage/storageAccounts?api-version="+storageAPIVer)
	}()
	if !opts.SkipDisks {
		wg.Add(1)
		go func() {
			defer wg.Done()
			disks, tr2, diskErr = armList(ctx, base+"/providers/Microsoft.Compute/disks?api-version="+diskAPIVer)
		}()
	}
	wg.Wait()
	truncated = tr1 || tr2
	s.AccountsListed = len(accounts) // cada goroutine escreve só na sua subscription
	if !opts.SkipDisks {
		s.DisksListed = len(disks)
	}
	if accErr != nil {
		warns = append(warns, fmt.Sprintf("%s: contas de armazenamento — %v", label, accErr))
	}
	if diskErr != nil {
		warns = append(warns, fmt.Sprintf("%s: discos — %v", label, diskErr))
	}

	newHit := func(typ, name, id, loc string) StorageHit {
		return StorageHit{Type: typ, Name: name, ResourceGroup: rgFromID(id), Location: loc,
			SubscriptionID: s.ID, SubscriptionName: s.Name, Clusters: s.Clusters,
			AKSManaged: strings.HasPrefix(strings.ToLower(rgFromID(id)), "mc_")}
	}

	var matchedAccounts []StorageHit
	for _, a := range accounts {
		name := digStr(a, "name")
		if !strings.Contains(strings.ToLower(name), needle) {
			continue
		}
		h := newHit("storage_account", name, digStr(a, "id"), digStr(a, "location"))
		h.Kind, h.SKU, h.Tier = digStr(a, "kind"), digStr(a, "sku", "name"), digStr(a, "properties", "accessTier")
		matchedAccounts = append(matchedAccounts, h)
	}
	if len(matchedAccounts) > storageSearchMaxHits {
		matchedAccounts, truncated = matchedAccounts[:storageSearchMaxHits], true
	}
	enrichAccounts(ctx, base, matchedAccounts)
	hits = append(hits, matchedAccounts...)

	nDisks := 0
	for _, d := range disks {
		name := digStr(d, "name")
		if !strings.Contains(strings.ToLower(name), needle) {
			continue
		}
		if nDisks++; nDisks > storageSearchMaxHits {
			truncated = true
			break
		}
		h := newHit("disk", name, digStr(d, "id"), digStr(d, "location"))
		h.SKU, h.SizeGB, h.State = digStr(d, "sku", "name"), digStr(d, "properties", "diskSizeGB"), digStr(d, "properties", "diskState")
		if mb := digStr(d, "managedBy"); mb != "" {
			h.AttachedTo = lastSegment(mb)
		}
		hits = append(hits, h)
	}

	if deep {
		h, w := deepSearchContainers(ctx, *s, accounts, needle, opts.DeepCap)
		hits = append(hits, h...)
		warns = append(warns, w...)
	}
	return hits, warns, truncated
}

// enrichAccounts acrescenta o ciclo de vida e os primeiros containers às contas encontradas (só as
// primeiras, para não multiplicar as chamadas).
func enrichAccounts(ctx context.Context, subBase string, accounts []StorageHit) {
	n := len(accounts)
	if n > storageSearchEnrich {
		n = storageSearchEnrich
	}
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(h *StorageHit) {
			defer wg.Done()
			accBase := fmt.Sprintf("%s/resourceGroups/%s/providers/Microsoft.Storage/storageAccounts/%s", subBase, url.PathEscape(h.ResourceGroup), url.PathEscape(h.Name))
			if h.Kind != "FileStorage" { // ciclo de vida só existe para blobs
				h.Lifecycle = fetchLifecycle(ctx, accBase)
				if list, _, err := armList(ctx, accBase+"/blobServices/default/containers?api-version="+storageAPIVer); err == nil {
					for _, ctn := range list {
						if len(h.Containers) >= storageSearchContainers {
							break
						}
						h.Containers = append(h.Containers, digStr(ctn, "name"))
					}
				}
			}
		}(&accounts[i])
	}
	wg.Wait()
}

func fetchLifecycle(ctx context.Context, accBase string) *LifecycleSummary {
	raw, err := azRestGet(ctx, accBase+"/managementPolicies/default?api-version="+storageAPIVer)
	if err != nil {
		if lifecycleNotFound(err) {
			return &LifecycleSummary{HasPolicy: false, Rules: []LifecycleRule{}}
		}
		return &LifecycleSummary{Rules: []LifecycleRule{}, Error: err.Error()}
	}
	rules := parseLifecycleRules(decode(raw))
	return &LifecycleSummary{HasPolicy: true, Rules: rules}
}

// deepSearchContainers procura o nome em containers e file shares das contas (uma chamada por conta
// e por tipo), limitado a storageSearchDeepCap contas.
func deepSearchContainers(ctx context.Context, s SubscriptionInfo, accounts []map[string]any, needle string, capAccounts int) ([]StorageHit, []string) {
	if capAccounts <= 0 {
		capAccounts = storageSearchDeepCap
	}
	var hits []StorageHit
	var warns []string
	var mu sync.Mutex
	var wg sync.WaitGroup
	var okCalls int32 // listagens que terminaram
	sem := make(chan struct{}, storageSearchDeepPar)
	subBase := armBase + "/subscriptions/" + url.PathEscape(s.ID)
	scanned := accounts
	if len(scanned) > capAccounts {
		scanned = scanned[:capAccounts]
		warns = append(warns, fmt.Sprintf("%s: busca em containers limitada às %d primeiras de %d contas.", subLabel(s), capAccounts, len(accounts)))
	}
	for _, a := range scanned {
		name, id, loc, kind := digStr(a, "name"), digStr(a, "id"), digStr(a, "location"), digStr(a, "kind")
		rg := rgFromID(id)
		accBase := fmt.Sprintf("%s/resourceGroups/%s/providers/Microsoft.Storage/storageAccounts/%s", subBase, url.PathEscape(rg), url.PathEscape(name))
		kinds := []struct{ typ, path string }{{"file_share", "/fileServices/default/shares"}}
		if kind != "FileStorage" {
			kinds = append(kinds, struct{ typ, path string }{"container", "/blobServices/default/containers"})
		}
		for _, k := range kinds {
			wg.Add(1)
			go func(name, rg, loc, typ, path string) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				list, _, err := armList(ctx, accBase+path+"?api-version="+storageAPIVer)
				if err != nil {
					return // conta sem permissão de listagem: segue sem ela
				}
				atomic.AddInt32(&okCalls, 1)
				for _, it := range list {
					n := digStr(it, "name")
					if !strings.Contains(strings.ToLower(n), needle) {
						continue
					}
					h := StorageHit{Type: typ, Name: n, ResourceGroup: rg, Location: loc, Account: name,
						SubscriptionID: s.ID, SubscriptionName: s.Name, Clusters: s.Clusters,
						AKSManaged: strings.HasPrefix(strings.ToLower(rg), "mc_"),
						Tier:       digStr(it, "properties", "accessTier")}
					mu.Lock()
					hits = append(hits, h)
					mu.Unlock()
				}
			}(name, rg, loc, k.typ, k.path)
		}
	}
	wg.Wait()
	if ctx.Err() != nil {
		warns = append(warns, fmt.Sprintf("%s: o tempo da varredura de containers acabou (%d de %d listagens concluídas): o resultado é parcial; rode de novo ou use \"Ver containers e file shares desta conta\" nas contas que interessam.",
			subLabel(s), atomic.LoadInt32(&okCalls), len(scanned)*2))
	}
	return hits, warns
}

// armList lê uma listagem do ARM seguindo o nextLink (no máximo storageSearchMaxPages páginas).
// Só segue links que apontam para o próprio management.azure.com.
func armList(ctx context.Context, rawURL string) (items []map[string]any, truncated bool, err error) {
	next := rawURL
	for page := 0; next != ""; page++ {
		if page >= storageSearchMaxPages {
			return items, true, nil
		}
		raw, err := azRestGet(ctx, next)
		if err != nil {
			return items, false, err
		}
		m := decode(raw)
		if m == nil {
			return items, false, fmt.Errorf("resposta inesperada da Azure")
		}
		if vals, ok := m["value"].([]any); ok {
			for _, v := range vals {
				if vm, ok := v.(map[string]any); ok {
					items = append(items, vm)
				}
			}
		}
		next = digStr(m, "nextLink")
		if next != "" && !strings.HasPrefix(next, armBase+"/") {
			return items, false, fmt.Errorf("nextLink fora do management.azure.com ignorado")
		}
	}
	return items, false, nil
}

// rgFromID extrai o resource group de /subscriptions/<id>/resourceGroups/<RG>/providers/...
func rgFromID(id string) string {
	parts := strings.Split(id, "/")
	for i, p := range parts {
		if strings.EqualFold(p, "resourceGroups") && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

func subLabel(s SubscriptionInfo) string {
	if s.Name != "" {
		return s.Name
	}
	return s.ID
}

// selectSubscriptions fica só com as subscriptions pedidas em `csv` (IDs separados por vírgula).
// Um ID fora da lista das consultadas é recusado: a busca nunca sai do escopo já conhecido.
func selectSubscriptions(subs []SubscriptionInfo, csv string) ([]SubscriptionInfo, error) {
	var out []SubscriptionInfo
	for _, raw := range strings.Split(csv, ",") {
		id := strings.TrimSpace(raw)
		if id == "" {
			continue
		}
		if !reUUID.MatchString(id) {
			return nil, fmt.Errorf("subscription inválida: %q", id)
		}
		found := false
		for _, s := range subs {
			if strings.EqualFold(s.ID, id) {
				out = append(out, s)
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("a subscription %s não está entre as consultadas (use a busca em todas as subscriptions)", id)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("nenhuma subscription informada")
	}
	return out, nil
}
