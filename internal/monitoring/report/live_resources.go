package report

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Itens que só existem no Prometheus e que o tempo real mostra ao lado das métricas do Dynatrace:
// réplicas, restarts, CPU e memória. Tráfego, erros e P50/P95/P99 continuam vindo SÓ do Dynatrace.
//
// Custo: um conjunto fixo de consultas por CLUSTER (não por aplicação): cada métrica é pedida para
// todos os namespaces do cluster de uma vez, agrupada por pod, e dividida entre as aplicações aqui.

// LiveResources é o consumo atual de uma aplicação. Ponteiro nulo = sem dado (nunca zero).
type LiveResources struct {
	ReplicasAvailable *float64 `json:"replicas_available"`
	ReplicasDesired   *float64 `json:"replicas_desired"`
	Restarts15m       *float64 `json:"restarts_15m"`
	CPUCores          *float64 `json:"cpu_cores"`
	CPUReqPct         *float64 `json:"cpu_req_pct"` // uso total / request total
	CPULimPct         *float64 `json:"cpu_lim_pct"` // uso total / limit total
	MemMB             *float64 `json:"mem_mb"`
	MemReqPct         *float64 `json:"mem_req_pct"`
	MemLimPct         *float64 `json:"mem_lim_pct"`
	Pods              int      `json:"pods"` // pods com métrica de CPU ou memória
}

// liveResourceQueries monta as consultas de um cluster para os namespaces dados (ns = regex já escapada).
// Cada uma agrupa por (namespace, pod) e usa `max by (namespace,pod,container)` por dentro: o
// kube-state-metrics e o cAdvisor podem repetir série, e somar duas vezes dobraria o valor.
// Função pura (testável sem rede).
func liveResourceQueries(ns string) map[string]string {
	perPod := func(inner string) string {
		return fmt.Sprintf(`sum by (namespace,pod) (max by (namespace,pod,container) (%s))`, inner)
	}
	cont := fmt.Sprintf(`namespace=~"%s",container!="",container!="POD"`, ns)
	kube := func(metric, resource string) string {
		return perPod(fmt.Sprintf(`%s{namespace=~"%s",resource="%s"}`, metric, ns, resource))
	}
	return map[string]string{
		"replicas_avail":   fmt.Sprintf(`max by (namespace,deployment) (kube_deployment_status_replicas_available{namespace=~"%s"})`, ns),
		"replicas_desired": fmt.Sprintf(`max by (namespace,deployment) (kube_deployment_spec_replicas{namespace=~"%s"})`, ns),
		"restarts":         perPod(fmt.Sprintf(`increase(kube_pod_container_status_restarts_total{namespace=~"%s"}[15m])`, ns)),
		"cpu":              perPod(fmt.Sprintf(`rate(container_cpu_usage_seconds_total{%s}[2m])`, cont)),
		"cpu_req":          kube("kube_pod_container_resource_requests", "cpu"),
		"cpu_lim":          kube("kube_pod_container_resource_limits", "cpu"),
		"mem":              perPod(fmt.Sprintf(`container_memory_working_set_bytes{%s}`, cont)),
		"mem_req":          kube("kube_pod_container_resource_requests", "memory"),
		"mem_lim":          kube("kube_pod_container_resource_limits", "memory"),
	}
}

// lastSample devolve o último valor válido de uma série do Prometheus.
func lastSample(values [][]interface{}) (float64, bool) {
	for i := len(values) - 1; i >= 0; i-- {
		if len(values[i]) < 2 {
			continue
		}
		s, ok := values[i][1].(string)
		if !ok {
			continue
		}
		v, err := strconv.ParseFloat(s, 64)
		if err == nil && !math.IsNaN(v) && !math.IsInf(v, 0) {
			return v, true
		}
	}
	return 0, false
}

// podOwner diz se o pod é de um Deployment ("<deploy>-<hash do ReplicaSet>-<sufixo>"), com a mesma regra
// do seletor do relatório: "frete-hub" não pode levar os pods de "frete-hub-regionalizado".
func podOwner(workload string) *regexp.Regexp {
	return regexp.MustCompile(`^` + regexp.QuoteMeta(workload) + `-[a-z0-9]{5,10}-[a-z0-9]{5}$`)
}

type nsKey struct{ ns, name string }

// sumFor soma o valor por pod das séries de uma consulta que pertencem a (namespace, workload).
// n = quantos pods entraram; ok=false quando nenhum.
func sumFor(series map[nsKey]float64, ns string, owner *regexp.Regexp) (sum float64, n int) {
	for k, v := range series {
		if k.ns == ns && owner.MatchString(k.name) {
			sum += v
			n++
		}
	}
	return
}

func ptr(v float64) *float64 { return &v }

func pctOf(use, total float64) *float64 {
	if total <= 0 {
		return nil
	}
	return ptr(use / total * 100)
}

// CollectLiveResources consulta o Prometheus de UM cluster e devolve o consumo de cada item (na ordem).
// Falha isolada deixa só aquele campo sem dado e anota o motivo; erro só se TODAS as consultas falharem.
func CollectLiveResources(ctx context.Context, q RangeQuerier, items []Item, now time.Time) ([]*LiveResources, []string, error) {
	nsSeen := map[string]bool{}
	var nss []string
	for _, it := range items {
		if !k8sNameStrict.MatchString(it.Namespace) || !k8sNameStrict.MatchString(it.Workload) {
			return nil, nil, fmt.Errorf("namespace ou deployment com nome inválido")
		}
		if !nsSeen[it.Namespace] {
			nsSeen[it.Namespace] = true
			nss = append(nss, strings.ReplaceAll(it.Namespace, ".", `\\.`))
		}
	}
	sort.Strings(nss)
	queries := liveResourceQueries(strings.Join(nss, "|"))

	var (
		mu     sync.Mutex
		wg     sync.WaitGroup
		got    = map[string]map[nsKey]float64{}
		notes  []string
		failed int
	)
	start := now.Add(-3 * time.Minute)
	for key, query := range queries {
		wg.Add(1)
		go func(key, query string) {
			defer wg.Done()
			res, err := q.QueryRange(ctx, query, start, now, time.Minute)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failed++
				notes = append(notes, fmt.Sprintf("%s: %v", key, err))
				return
			}
			m := map[nsKey]float64{}
			second := "pod"
			if strings.HasPrefix(key, "replicas") {
				second = "deployment"
			}
			for _, r := range res.Data.Result {
				if v, ok := lastSample(r.Values); ok {
					m[nsKey{r.Metric["namespace"], r.Metric[second]}] = v
				}
			}
			got[key] = m
		}(key, query)
	}
	wg.Wait()
	if failed == len(queries) {
		sort.Strings(notes)
		return nil, nil, fmt.Errorf("todas as consultas do Prometheus falharam: %s", strings.Join(notes, "; "))
	}
	sort.Strings(notes)

	out := make([]*LiveResources, len(items))
	for i, it := range items {
		r := &LiveResources{}
		if v, ok := got["replicas_avail"][nsKey{it.Namespace, it.Workload}]; ok {
			r.ReplicasAvailable = ptr(v)
		}
		if v, ok := got["replicas_desired"][nsKey{it.Namespace, it.Workload}]; ok {
			r.ReplicasDesired = ptr(v)
		}
		owner := podOwner(it.Workload)
		if v, n := sumFor(got["restarts"], it.Namespace, owner); n > 0 {
			r.Restarts15m = ptr(math.Round(v))
		}
		cpu, cpuPods := sumFor(got["cpu"], it.Namespace, owner)
		mem, memPods := sumFor(got["mem"], it.Namespace, owner)
		r.Pods = cpuPods
		if memPods > r.Pods {
			r.Pods = memPods
		}
		if cpuPods > 0 {
			r.CPUCores = ptr(cpu)
			if req, n := sumFor(got["cpu_req"], it.Namespace, owner); n > 0 {
				r.CPUReqPct = pctOf(cpu, req)
			}
			if lim, n := sumFor(got["cpu_lim"], it.Namespace, owner); n > 0 {
				r.CPULimPct = pctOf(cpu, lim)
			}
		}
		if memPods > 0 {
			r.MemMB = ptr(mem / 1024 / 1024)
			if req, n := sumFor(got["mem_req"], it.Namespace, owner); n > 0 {
				r.MemReqPct = pctOf(mem, req)
			}
			if lim, n := sumFor(got["mem_lim"], it.Namespace, owner); n > 0 {
				r.MemLimPct = pctOf(mem, lim)
			}
		}
		out[i] = r
	}
	return out, notes, nil
}
