package finops

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// Consultas PESADAS ao Prometheus: P95/média/pico de CPU e memória por pod são subqueries de
// janela longa (`[30d:5m]`) — o custo cresce com (nº de séries × nº de passos), e num cluster
// grande uma única query sobre todos os pods passa fácil de 1-2 minutos. Duas medidas juntas:
//
//  1. Tempo: promHeavyQueryTimeout (padrão 3 min, ligeiramente acima do --query.timeout padrão do
//     Prometheus, 2 min). Assim, se o servidor desistir, é ELE quem responde (com o erro real
//     "query timed out in expression evaluation") em vez de a app cancelar antes com um genérico
//     "context deadline exceeded". Prometheus configurado com --query.timeout maior: ver a
//     variável de ambiente abaixo.
//  2. Paralelismo: em vez de UMA query sobre todos os pods do cluster, restringe aos namespaces dos
//     workloads analisados (o resto — kube-system, monitoring... — nunca é usado) e reparte esses
//     namespaces em fatias que rodam em paralelo (shardedRun). Cada fatia é bem mais barata que a
//     query inteira, então cabe no tempo; e como a query agrupa por (namespace, pod) e o filtro é
//     por namespace, as fatias não se sobrepõem — juntar os resultados é exato (uma união).

const (
	// promHeavyTimeoutEnv permite ajustar o tempo das queries pesadas sem recompilar (Go duration:
	// "5m", "90s"). Útil quando o Prometheus roda com --query.timeout acima do padrão.
	promHeavyTimeoutEnv     = "K8S_HPA_PROM_HEAVY_TIMEOUT"
	promHeavyTimeoutDefault = 3 * time.Minute
	promHeavyTimeoutMin     = 30 * time.Second
	promHeavyTimeoutMax     = 30 * time.Minute

	// promMaxShards: quantas fatias de namespace no máximo. Cada fatia dispara as 8 queries
	// pesadas, então mais fatias = mais queries no total (cada uma menor).
	promMaxShards = 4
	// promMinNamespacesToShard: com poucos namespaces, repartir só adiciona overhead.
	promMinNamespacesToShard = 4
	// promHeavyConcurrency: teto de queries pesadas simultâneas contra o Prometheus. Igual ao nº de
	// queries pesadas que já rodavam juntas antes (8) — o Prometheus tem --query.max-concurrency
	// (padrão 20) e as demais queries (HPA etc.) também precisam de vaga na fila.
	promHeavyConcurrency = 8
)

var (
	promHeavyTimeoutOnce  sync.Once
	promHeavyTimeoutValue = promHeavyTimeoutDefault
)

// parseHeavyTimeout valida o valor da variável de ambiente (duração Go entre o mínimo e o máximo).
func parseHeavyTimeout(raw string) (time.Duration, bool) {
	d, err := time.ParseDuration(strings.TrimSpace(raw))
	if err != nil || d < promHeavyTimeoutMin || d > promHeavyTimeoutMax {
		return 0, false
	}
	return d, true
}

// promHeavyQueryTimeout devolve o tempo máximo de UMA query pesada (lê o ambiente uma vez).
func promHeavyQueryTimeout() time.Duration {
	promHeavyTimeoutOnce.Do(func() {
		raw := strings.TrimSpace(os.Getenv(promHeavyTimeoutEnv))
		if raw == "" {
			return
		}
		d, ok := parseHeavyTimeout(raw)
		if !ok {
			log.Warn().Str("valor", raw).Str("env", promHeavyTimeoutEnv).
				Msgf("FinOps/Prom: valor inválido (use uma duração entre %s e %s) — mantendo o padrão %s",
					promHeavyTimeoutMin, promHeavyTimeoutMax, promHeavyTimeoutDefault)
			return
		}
		promHeavyTimeoutValue = d
	})
	return promHeavyTimeoutValue
}

// k8sNamespaceRe: nome de namespace válido (DNS label). Só esses entram no matcher PromQL — nunca
// um valor arbitrário injetado dentro de uma regex.
var k8sNamespaceRe = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

// planNamespaceShards reparte os namespaces dos workloads em até maxShards fatias equilibradas pelo
// nº de workloads (namespace grande não fica sozinho com todos os outros pequenos na mesma fatia).
// Cada elemento é o conteúdo de um matcher regex ("ns-a|ns-b"). Devolve:
//   - []string{""}  → sem restrição de namespace (nenhum namespace válido: mantém a query inteira);
//   - 1 fatia       → poucos namespaces (não vale repartir), mas ainda restrita aos analisados;
//   - N fatias      → repartido.
//
// Determinístico (mesma entrada → mesmas fatias), pra logs e testes estáveis.
func planNamespaceShards(workloads []FinOpsWorkload, maxShards int) []string {
	count := map[string]int{}
	for _, w := range workloads {
		if k8sNamespaceRe.MatchString(w.Namespace) {
			count[w.Namespace]++
		}
	}
	if len(count) == 0 {
		return []string{""}
	}

	names := make([]string, 0, len(count))
	for ns := range count {
		names = append(names, ns)
	}
	// Maiores primeiro (empate: nome) — heurística clássica de balanceamento (LPT).
	sort.Slice(names, func(i, j int) bool {
		if count[names[i]] != count[names[j]] {
			return count[names[i]] > count[names[j]]
		}
		return names[i] < names[j]
	})

	n := 1
	if len(names) >= promMinNamespacesToShard {
		n = maxShards
		if n > len(names) {
			n = len(names)
		}
		if n < 1 {
			n = 1
		}
	}

	shards := make([][]string, n)
	load := make([]int, n)
	for _, ns := range names {
		lightest := 0
		for i := 1; i < n; i++ {
			if load[i] < load[lightest] {
				lightest = i
			}
		}
		shards[lightest] = append(shards[lightest], ns)
		load[lightest] += count[ns]
	}

	out := make([]string, 0, n)
	for _, sh := range shards {
		sort.Strings(sh)
		out = append(out, strings.Join(sh, "|"))
	}
	return out
}

// nsMatcher devolve o trecho de seletor PromQL que restringe aos namespaces da fatia (com a
// vírgula inicial, pra anexar dentro de `{...}`), ou "" quando a fatia não restringe.
func nsMatcher(shard string) string {
	if shard == "" {
		return ""
	}
	return fmt.Sprintf(`,namespace=~"%s"`, shard)
}

// shardLabel identifica a fatia no rótulo do erro ("cpu_p95[2/4]") — só quando há mais de uma.
func shardLabel(label string, idx, total int) string {
	if total <= 1 {
		return label
	}
	return fmt.Sprintf("%s[%d/%d]", label, idx+1, total)
}

// shardedRun executa fn uma vez por fatia, em paralelo (limitado por e.heavySem), e junta os mapas.
// Uma fatia que falha (fn devolve nil) NÃO derruba as outras: o resultado fica parcial — os
// workloads das fatias que deram certo continuam com dado real, e o erro da fatia que falhou já foi
// registrado por quem a executou (CollectionErrors). Só devolve nil se TODAS falharam, preservando
// a semântica "nil = a coleta falhou" que o restante do enricher espera.
func shardedRun[T any](e *PrometheusEnricher, shards []string, label string, fn func(matcher, label string) map[string]T) map[string]T {
	results := make([]map[string]T, len(shards))
	var wg sync.WaitGroup
	for i, sh := range shards {
		wg.Add(1)
		go func(i int, sh string) {
			defer wg.Done()
			// A vaga é tomada ANTES de a query criar o seu timeout: o tempo de espera na fila não
			// pode ser descontado do tempo da query.
			if e.heavySem != nil {
				e.heavySem <- struct{}{}
				defer func() { <-e.heavySem }()
			}
			results[i] = fn(nsMatcher(sh), shardLabel(label, i, len(shards)))
		}(i, sh)
	}
	wg.Wait()

	var merged map[string]T
	for _, r := range results {
		if r == nil {
			continue
		}
		if merged == nil {
			merged = make(map[string]T)
		}
		for k, v := range r {
			merged[k] = v
		}
	}
	return merged
}
