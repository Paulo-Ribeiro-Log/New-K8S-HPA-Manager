package discovery

import (
	"sync"
	"time"
)

// unreachableCacheTTL define por quanto tempo uma falha real de conexão a uma URL Prometheus é
// reaproveitada antes de deixar tentar de novo — evita pagar o timeout completo (30s no cliente
// de alertas, 10s na validação de discovery) em toda chamada subsequente enquanto o endpoint
// continua fora do ar.
//
// Bug real corrigido: um cluster com Prometheus inacessível fazia /api/v1/alerts/hpa,
// /api/v1/alerts/nodepool e /api/v1/alerts/summary — cada endpoint criando seu próprio
// *alerts.Client, sem nenhum estado compartilhado — pagarem 30s de timeout CADA, às vezes
// repetido 2-3x (retry do frontend), somando minutos de espera só por trocar de cluster.
// var (não const) para permitir encurtar em teste sem precisar de time.Sleep real de 60s.
var unreachableCacheTTL = 60 * time.Second

type unreachableEntry struct {
	err      error
	cachedAt time.Time
}

// unreachableCache é compartilhado por qualquer consumidor de URL Prometheus (alerts.Client,
// PrometheusClient, etc.) — a chave é a URL resolvida (mesma string pro mesmo cluster), não
// importa qual pacote/cliente HTTP foi quem tentou e falhou primeiro.
var unreachableCache sync.Map

// MarkPrometheusUnreachable registra que uma tentativa real de falar com url falhou. Chamadas
// subsequentes a CheckKnownUnreachable para a mesma url retornam esse erro sem nova tentativa de
// rede, até unreachableCacheTTL expirar.
func MarkPrometheusUnreachable(url string, err error) {
	if url == "" || err == nil {
		return
	}
	unreachableCache.Store(url, unreachableEntry{err: err, cachedAt: time.Now()})
}

// MarkPrometheusReachable limpa qualquer falha anterior registrada para url — chamar depois de
// uma resposta bem-sucedida, para a recuperação não ficar presa esperando o TTL expirar.
func MarkPrometheusReachable(url string) {
	if url == "" {
		return
	}
	unreachableCache.Delete(url)
}

// CheckKnownUnreachable retorna o erro cacheado se uma tentativa real falhou para esta URL há
// menos de unreachableCacheTTL, ou nil se não há falha recente conhecida (deve tentar de
// verdade).
func CheckKnownUnreachable(url string) error {
	if url == "" {
		return nil
	}
	raw, ok := unreachableCache.Load(url)
	if !ok {
		return nil
	}
	entry := raw.(unreachableEntry)
	if time.Since(entry.cachedAt) >= unreachableCacheTTL {
		unreachableCache.Delete(url)
		return nil
	}
	return entry.err
}
