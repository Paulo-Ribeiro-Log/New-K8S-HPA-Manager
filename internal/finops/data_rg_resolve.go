package finops

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"

	"k8s-hpa-manager/internal/cloudprovider/azure"
)

// Convenção (confirmada): o RG de dados de um cluster AKS é
//
//	rg-<nome do cluster sem prefixo e sem sufixo>-data-<env>
//
// igual à do RG de app (rg-<nome>-app-<env>). O nome vem do context do kubeconfig, ex:
// "akspriv-abastecimento-hlg-admin" → prefixo "akspriv-", sufixo "-admin" e env "hlg" saem, sobra
// "abastecimento" → "rg-abastecimento-data-hlg". Cluster que só existe no kubeconfig (sem entrada em
// clusters-config.json) também funciona.
var clusterNameRe = regexp.MustCompile(`(?i)^aks[a-z]*-(.+)-(hlg|prd|hml|dev|sit|stg|uat|qa)(?:-admin)?$`)

// DeriveDataResourceGroupFromCluster aplica a convenção acima ao nome do cluster (context do
// kubeconfig ou nome curto). ok=false quando o nome não segue "aks*-<nome>-<env>".
func DeriveDataResourceGroupFromCluster(cluster string) (string, bool) {
	m := clusterNameRe.FindStringSubmatch(strings.TrimSpace(cluster))
	if m == nil || m[1] == "" {
		return "", false
	}
	return fmt.Sprintf("rg-%s-data-%s", strings.ToLower(m[1]), strings.ToLower(m[2])), true
}

// DataRGCandidates devolve, em ordem de prioridade, os nomes de RG de dados a tentar: primeiro o
// da convenção pelo nome do cluster; depois o derivado do RG de app registrado no cluster
// (clusters-config.json), porque alguns clusters têm o nome de RG diferente do nome do cluster
// (ex: cluster "akspriv-mktplacecarrinho-prd" com RG de app "rg-mktplace-app-prd" e RG de dados
// "rg-mktplace-data-prd"). Sem duplicatas (sem diferenciar caixa).
func DataRGCandidates(cluster, appResourceGroup string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(rg string, ok bool) {
		if ok && !seen[strings.ToLower(rg)] {
			seen[strings.ToLower(rg)] = true
			out = append(out, rg)
		}
	}
	rg, ok := DeriveDataResourceGroupFromCluster(cluster)
	add(rg, ok)
	if appResourceGroup != "" {
		rg, ok = DeriveDataResourceGroup(strings.ToLower(appResourceGroup))
		add(rg, ok)
	}
	return out
}

// RGProber abstrai as duas consultas do ARM que a resolução precisa (testável sem rede).
type RGProber interface {
	ListSubscriptions(ctx context.Context) ([]azure.Subscription, error)
	RGExists(ctx context.Context, subscriptionID, resourceGroup string) (bool, error)
}

// DataRGResolution é onde o RG de dados foi de fato encontrado.
type DataRGResolution struct {
	ResourceGroup    string   `json:"resource_group"`
	SubscriptionID   string   `json:"subscription_id"`
	SubscriptionName string   `json:"subscription_name"`
	Tried            []string `json:"tried"`
	// InClusterSubscription é false quando o RG de dados fica numa subscription DIFERENTE da do
	// cluster (acontece: ex. cshub-hlg roda em BACKOFFICE e o RG de dados está em ONLINE).
	InClusterSubscription bool `json:"in_cluster_subscription"`
	// OtherSubscriptions: outras subscriptions onde EXISTE um RG com o mesmo nome (conteúdo
	// diferente — ex: rg-oferta-data-prd tem 179 recursos em "PRD - ONLINE 2" e 50 em "PRD - ONLINE").
	// Não são somadas nem escolhidas: só avisadas, porque a app não sabe qual é o certo.
	OtherSubscriptions []string `json:"other_subscriptions,omitempty"`
}

// ErrDataRGNotFound: nenhum candidato existe em nenhuma subscription acessível.
type ErrDataRGNotFound struct {
	Tried        []string
	Subscription int // quantas subscriptions foram pesquisadas
}

func (e *ErrDataRGNotFound) Error() string {
	return fmt.Sprintf("nenhum Resource Group de dados encontrado (tentados: %s) em %d subscription(s) acessível(is)",
		strings.Join(e.Tried, ", "), e.Subscription)
}

const rgProbeConcurrency = 8

// ResolveDataResourceGroup procura o RG de dados entre os candidatos. Caminho rápido: a
// subscription do cluster (1 chamada por candidato). Se não estiver lá, procura em TODAS as
// subscriptions acessíveis — o RG de dados pode viver em outra. Nome que existe em mais de uma
// subscription (e nenhuma é a do cluster) é ambíguo e vira erro, nunca um palpite.
func ResolveDataResourceGroup(ctx context.Context, p RGProber, candidates []string, clusterSubscription string) (*DataRGResolution, error) {
	if len(candidates) == 0 {
		return nil, fmt.Errorf("não foi possível derivar o nome do Resource Group de dados a partir do cluster")
	}
	subs, err := p.ListSubscriptions(ctx)
	if err != nil {
		return nil, fmt.Errorf("listar subscriptions: %w", err)
	}
	clusterSub := matchSubscription(subs, clusterSubscription)

	found := func(rg string, sub azure.Subscription) *DataRGResolution {
		res := &DataRGResolution{ResourceGroup: rg, SubscriptionID: sub.ID, SubscriptionName: sub.Name,
			Tried: candidates, InClusterSubscription: sub.ID == clusterSub.ID}
		// Mesmo nome em outra subscription? Só avisa (o resultado é cacheado pelo chamador, então
		// esse custo extra é pago uma vez por cluster).
		for _, o := range probeAll(ctx, p, subs, rg) {
			if o.ID != sub.ID {
				res.OtherSubscriptions = append(res.OtherSubscriptions, o.Name)
			}
		}
		return res
	}

	// 1) Caminho rápido: subscription do cluster.
	if clusterSub.ID != "" {
		for _, rg := range candidates {
			if ok, err := p.RGExists(ctx, clusterSub.ID, rg); err == nil && ok {
				return found(rg, clusterSub), nil
			}
		}
	}

	// 2) Busca ampla, candidato por candidato (a ordem de prioridade continua valendo).
	for _, rg := range candidates {
		where := probeAll(ctx, p, subs, rg)
		switch len(where) {
		case 0:
			continue
		case 1:
			return &DataRGResolution{ResourceGroup: rg, SubscriptionID: where[0].ID, SubscriptionName: where[0].Name,
				Tried: candidates, InClusterSubscription: where[0].ID == clusterSub.ID}, nil
		default:
			names := make([]string, len(where))
			for i, w := range where {
				names[i] = w.Name
			}
			return nil, fmt.Errorf("o Resource Group '%s' existe em várias subscriptions (%s) e nenhuma é a do cluster — não dá pra saber qual é o certo",
				rg, strings.Join(names, ", "))
		}
	}
	return nil, &ErrDataRGNotFound{Tried: candidates, Subscription: len(subs)}
}

// matchSubscription acha a subscription do cluster por ID ou nome (sem diferenciar caixa); zero
// value quando desconhecida.
func matchSubscription(subs []azure.Subscription, idOrName string) azure.Subscription {
	idOrName = strings.TrimSpace(idOrName)
	if idOrName == "" {
		return azure.Subscription{}
	}
	for _, s := range subs {
		if strings.EqualFold(s.ID, idOrName) || strings.EqualFold(s.Name, idOrName) {
			return s
		}
	}
	return azure.Subscription{}
}

// probeAll pergunta a cada subscription (em paralelo) se o RG existe; devolve onde existe,
// ordenado por nome pra o resultado ser determinístico.
func probeAll(ctx context.Context, p RGProber, subs []azure.Subscription, rg string) []azure.Subscription {
	var mu sync.Mutex
	var where []azure.Subscription
	sem := make(chan struct{}, rgProbeConcurrency)
	var wg sync.WaitGroup
	for _, s := range subs {
		wg.Add(1)
		sem <- struct{}{}
		go func(s azure.Subscription) {
			defer wg.Done()
			defer func() { <-sem }()
			if ok, err := p.RGExists(ctx, s.ID, rg); err == nil && ok {
				mu.Lock()
				where = append(where, s)
				mu.Unlock()
			}
		}(s)
	}
	wg.Wait()
	sort.Slice(where, func(i, j int) bool { return where[i].Name < where[j].Name })
	return where
}
