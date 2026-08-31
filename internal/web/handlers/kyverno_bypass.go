package handlers

import (
	"context"
	"sync/atomic"
	"time"

	kubeclient "k8s-hpa-manager/internal/kubernetes"
)

// withKyvernoBypass executa fn() com a label devops.k8s.io/kyverno-bypass=true ativa no namespace
// (removida ao final, mesmo em caso de erro) — automatiza o procedimento manual que o time já usa
// hoje antes/depois de qualquer rollback manual. Pedido explícito do usuário: "acredito que ele
// precisa ser usada em qualquer modo que for executar o rollback" — aplicado nos 6 modos (Rollback/
// SetImage/ApplyManifest chamam direto; HelmRollbackWithBypass também).
//
// A janela de bypass é deliberadamente CURTA — dura só a chamada síncrona de mutação (patch
// estratégico, kubectl apply, ou o subprocesso `helm rollback`), nunca o rollout assíncrono que
// vem depois: o Kyverno intercepta a ADMISSÃO da mutação em si (a chamada que o cluster vê como
// "escrita"), não as leituras de status que streamRolloutStatus faz depois — não há necessidade
// nem risco de manter o bypass ligado além do instante da mutação.
//
// h.kyvernoBypassRefs conta quantas mutações concorrentes (Deployments DIFERENTES, mesmo
// namespace) estão dependendo do bypass agora — a label só é removida quando a última delas
// termina, nunca no meio de outra ainda em andamento no mesmo namespace.
//
// Habilitar é BEST-EFFORT (nunca bloqueia fn() mesmo se falhar): se o cluster genuinamente tiver
// essa política Kyverno, a mutação em fn() vai falhar com o erro de admissão real (mensagem clara,
// já validada ao vivo nesta app — ver CLAUDE.md); se o cluster não tiver, uma falha aqui (ex: RBAC
// sem permissão de patch em Namespace, plausível em kubeconfigs escopados só a workloads) não deve
// bloquear rollbacks que nunca precisaram do bypass em primeiro lugar. Desabilitar também é
// best-effort, mas logado como erro (não warning) — deixar a label ligada por engano é o cenário
// de segurança que este mecanismo existe pra evitar.
func (h *DeploymentRollbackHandler) withKyvernoBypass(ctx context.Context, kubeClient *kubeclient.Client, cluster, namespace string, fn func() error) error {
	key := cluster + "/" + namespace
	v, _ := h.kyvernoBypassRefs.LoadOrStore(key, new(int32))
	counter := v.(*int32)

	if atomic.AddInt32(counter, 1) == 1 {
		if err := kubeClient.SetNamespaceKyvernoBypass(ctx, namespace, true); err != nil {
			h.logf("não foi possível habilitar bypass Kyverno em %s/%s (best-effort, prosseguindo): %v", cluster, namespace, err)
		}
	}

	defer func() {
		if atomic.AddInt32(counter, -1) == 0 {
			// Contexto próprio, não o ctx da requisição (que já pode ter retornado até aqui) —
			// mesmo racional de streamRolloutStatus usar context.Background() pro streaming.
			disableCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			if err := kubeClient.SetNamespaceKyvernoBypass(disableCtx, namespace, false); err != nil {
				h.logf("FALHA ao remover bypass Kyverno de %s/%s — remova manualmente: kubectl label namespace %s devops.k8s.io/kyverno-bypass- --context %s (erro: %v)",
					cluster, namespace, namespace, cluster, err)
			}
		}
	}()

	return fn()
}

func (h *DeploymentRollbackHandler) logf(format string, args ...interface{}) {
	if h.logger == nil {
		return
	}
	h.logger.Warn().Msgf(format, args...)
}
