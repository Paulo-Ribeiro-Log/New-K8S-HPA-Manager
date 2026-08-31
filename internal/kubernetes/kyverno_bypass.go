package kubernetes

import (
	"context"
	"encoding/json"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// KyvernoBypassLabelKey — label de Namespace usada por esta empresa pra liberar mutações fora da
// esteira CI de uma política Kyverno que bloqueia deployments/patches manuais (achado real,
// relatado pelo usuário depois de testar o Rollback de Deployment num cluster real: "o kyverno
// bloqueia qualquer tentativa de deployments fora da esteira... em nossos clusters temos a opção
// de colocar uma label que inibe isso"). Confirmado ao vivo numa sessão anterior desta mesma
// feature: uma política "deployment-restrictions" rejeitou um `kubectl apply --server-side` de
// teste com o erro "nodeSelector nao definido" — mesma classe de bloqueio, sem a label de bypass
// aplicada. Procedimento manual que o time já usa e sempre repete: `kubectl label namespace <ns>
// devops.k8s.io/kyverno-bypass=true`, sempre removida depois "por segurança" — ver
// SetNamespaceKyvernoBypass/internal/web/handlers/kyverno_bypass.go pra automação dos 2 lados.
const KyvernoBypassLabelKey = "devops.k8s.io/kyverno-bypass"

// SetNamespaceKyvernoBypass liga (enable=true, valor "true") ou desliga (enable=false, remove a
// chave) a label de bypass no Namespace via JSON merge patch — mesmo efeito de `kubectl label
// namespace <ns> devops.k8s.io/kyverno-bypass=true --overwrite` (enable) ou `... label namespace
// <ns> devops.k8s.io/kyverno-bypass-` (disable — no merge patch, uma chave com valor `null` some
// do mapa). Patch idempotente: nunca falha só por já estar no estado desejado.
func (c *Client) SetNamespaceKyvernoBypass(ctx context.Context, namespace string, enable bool) error {
	var value interface{}
	if enable {
		value = "true"
	}
	// value fica nil (zero value de interface{}) quando !enable — json.Marshal serializa como
	// `null`, que num JSON merge patch (RFC 7396) remove a chave do mapa de labels.
	patch := map[string]interface{}{
		"metadata": map[string]interface{}{
			"labels": map[string]interface{}{
				KyvernoBypassLabelKey: value,
			},
		},
	}
	patchBytes, err := json.Marshal(patch)
	if err != nil {
		return fmt.Errorf("montar patch da label de bypass Kyverno: %w", err)
	}
	if _, err := c.clientset.CoreV1().Namespaces().Patch(ctx, namespace, types.MergePatchType, patchBytes, metav1.PatchOptions{}); err != nil {
		action := "habilitar"
		if !enable {
			action = "remover"
		}
		return fmt.Errorf("%s bypass Kyverno no namespace %q: %w", action, namespace, err)
	}
	return nil
}
