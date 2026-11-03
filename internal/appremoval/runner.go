package appremoval

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const deleteTimeout = 180 * time.Second

// resourceFor mapeia o Kind para o recurso totalmente qualificado do kubectl. Também é a lista
// de tipos que a ferramenta aceita remover.
func resourceFor(kind string) string {
	switch kind {
	case KindDeployment:
		return "deployments.apps"
	case KindStatefulSet:
		return "statefulsets.apps"
	case KindDaemonSet:
		return "daemonsets.apps"
	case KindCronJob:
		return "cronjobs.batch"
	case KindService:
		return "services"
	case KindIngress:
		return "ingresses.networking.k8s.io"
	case KindHTTPRoute:
		return "httproutes.gateway.networking.k8s.io"
	case KindVirtualService:
		return "virtualservices.networking.istio.io"
	case KindHPA:
		return "horizontalpodautoscalers.autoscaling"
	case KindPDB:
		return "poddisruptionbudgets.policy"
	case KindNetworkPolicy:
		return "networkpolicies.networking.k8s.io"
	case KindServiceMonitor:
		return "servicemonitors.monitoring.coreos.com"
	case KindPodMonitor:
		return "podmonitors.monitoring.coreos.com"
	case KindConfigMap:
		return "configmaps"
	case KindSecret:
		return "secrets"
	case KindExternalSecret:
		return "externalsecrets.external-secrets.io"
	case KindPVC:
		return "persistentvolumeclaims"
	case KindSA:
		return "serviceaccounts"
	case KindRole:
		return "roles.rbac.authorization.k8s.io"
	case KindRoleBinding:
		return "rolebindings.rbac.authorization.k8s.io"
	}
	return ""
}

// kubectlRun roda o kubectl e devolve stdout; em erro, a mensagem é o stderr. Trocável nos testes.
var kubectlRun = func(ctx context.Context, args ...string) ([]byte, error) {
	out, err := exec.CommandContext(ctx, "kubectl", args...).Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && len(ee.Stderr) > 0 {
			return nil, fmt.Errorf("%s", strings.TrimSpace(string(ee.Stderr)))
		}
		return nil, err
	}
	return out, nil
}

// KubectlRunner implementa Runner com kubectl; o uninstall do Helm vem de fora (serviço Helm da app).
type KubectlRunner struct {
	Uninstall func(ctx context.Context, cluster, namespace, release string, dryRun bool) error
}

func (k KubectlRunner) CurrentUID(ctx context.Context, cluster, namespace, kind, name string) (string, error) {
	res := resourceFor(kind)
	if res == "" {
		return "", fmt.Errorf("%w: tipo %s", ErrInvalidTarget, kind)
	}
	ctx, cancel := context.WithTimeout(ctx, deleteTimeout)
	defer cancel()
	out, err := kubectlRun(ctx, "get", res, name, "-n", namespace, "--context", cluster,
		"-o", "jsonpath={.metadata.uid}")
	if err != nil {
		if isNotFound(err) {
			return "", ErrNotFoundInCluster
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func (k KubectlRunner) Delete(ctx context.Context, cluster, namespace, kind, name string, dryRun bool) error {
	res := resourceFor(kind)
	if res == "" {
		return fmt.Errorf("%w: tipo %s", ErrInvalidTarget, kind)
	}
	ctx, cancel := context.WithTimeout(ctx, deleteTimeout)
	defer cancel()
	args := []string{"delete", res, name, "-n", namespace, "--context", cluster, "--timeout=90s"}
	if isController(kind) {
		// foreground: o kubectl só volta depois que ReplicaSets e pods saíram, então nada mais usa
		// PVC/config e nada é recriado pelo controller enquanto seguimos para as próximas etapas.
		args = []string{"delete", res, name, "-n", namespace, "--context", cluster, "--timeout=150s", "--cascade=foreground"}
	}
	if dryRun {
		args = append(args, "--dry-run=server")
	}
	if _, err := kubectlRun(ctx, args...); err != nil {
		if isNotFound(err) {
			return nil
		}
		return err
	}
	return nil
}

func (k KubectlRunner) HelmUninstall(ctx context.Context, cluster, namespace, release string, dryRun bool) error {
	if k.Uninstall == nil {
		return errors.New("helm indisponível")
	}
	return k.Uninstall(ctx, cluster, namespace, release, dryRun)
}

func isController(kind string) bool {
	switch kind {
	case KindDeployment, KindStatefulSet, KindDaemonSet, KindCronJob:
		return true
	}
	return false
}

func isNotFound(err error) bool {
	m := strings.ToLower(err.Error())
	return strings.Contains(m, "notfound") || strings.Contains(m, "not found")
}
