package appremoval

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	collectTimeout = 45 * time.Second

	workloadKinds = "deployments,statefulsets,daemonsets,cronjobs"
	coreKinds     = workloadKinds + ",services,ingresses,horizontalpodautoscalers,poddisruptionbudgets," +
		"networkpolicies,configmaps,secrets,persistentvolumeclaims,serviceaccounts,roles,rolebindings"
)

// CRDs opcionais: se não estiverem instaladas no cluster, são ignoradas.
var optionalKinds = []string{
	"httproutes.gateway.networking.k8s.io",
	"virtualservices.networking.istio.io",
	"servicemonitors.monitoring.coreos.com",
	"podmonitors.monitoring.coreos.com",
}

// kubectlGet roda `kubectl get` somente leitura; trocável nos testes.
var kubectlGet = func(ctx context.Context, cluster, namespace, kinds string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "kubectl", "get", kinds, "-n", namespace, "--context", cluster, "-o", "json")
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && len(ee.Stderr) > 0 {
			return nil, fmt.Errorf("%s", strings.TrimSpace(string(ee.Stderr)))
		}
		return nil, err
	}
	return out, nil
}

// Collect lista tudo que o scanner precisa no namespace. As chamadas rodam em paralelo; só a
// listagem básica é obrigatória.
func Collect(ctx context.Context, cluster, namespace string) ([]Resource, error) {
	ctx, cancel := context.WithTimeout(ctx, collectTimeout)
	defer cancel()

	var (
		mu      sync.Mutex
		wg      sync.WaitGroup
		all     []Resource
		coreErr error
	)
	fetch := func(kinds string, required bool) {
		defer wg.Done()
		out, err := kubectlGet(ctx, cluster, namespace, kinds)
		if err == nil {
			var items []Resource
			if items, err = ParseList(out); err == nil {
				mu.Lock()
				all = append(all, items...)
				mu.Unlock()
				return
			}
		}
		if required {
			mu.Lock()
			coreErr = fmt.Errorf("falha ao listar recursos do namespace: %w", err)
			mu.Unlock()
		}
	}

	wg.Add(1 + len(optionalKinds))
	go fetch(coreKinds, true)
	for _, k := range optionalKinds {
		go fetch(k, false)
	}
	wg.Wait()
	if coreErr != nil {
		return nil, coreErr
	}
	return all, nil
}

// Candidates lista workloads e releases Helm do namespace, como alvos possíveis do scan.
func Candidates(ctx context.Context, cluster, namespace string) ([]Candidate, error) {
	ctx, cancel := context.WithTimeout(ctx, collectTimeout)
	defer cancel()

	out, err := kubectlGet(ctx, cluster, namespace, workloadKinds)
	if err != nil {
		return nil, fmt.Errorf("falha ao listar workloads: %w", err)
	}
	resources, err := ParseList(out)
	if err != nil {
		return nil, err
	}
	return candidatesFrom(resources), nil
}

func candidatesFrom(resources []Resource) []Candidate {
	out := []Candidate{}
	releases := map[string]bool{}
	for _, r := range resources {
		if !isWorkloadKind(r.Kind) {
			continue
		}
		rel := r.Annotations[annHelmRelease]
		out = append(out, Candidate{Kind: r.Kind, Name: r.Name, HelmRelease: rel})
		if rel != "" {
			releases[rel] = true
		}
	}
	for rel := range releases {
		out = append(out, Candidate{Kind: KindHelmRelease, Name: rel, HelmRelease: rel})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Kind < out[j].Kind
	})
	return out
}
