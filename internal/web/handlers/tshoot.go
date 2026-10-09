package handlers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Pod de troubleshooting com --rm (aba Pods/Namespaces): o equivalente a
//
//	kubectl run tshoot-xxx --rm -it --image nicolaka/netshoot --restart=Never -- bash
//
// O ciclo de vida é do SERVIDOR, não do browser: a mesma conexão cria o pod, espera ficar Running,
// abre o bash com TTY e, quando a sessão termina por qualquer motivo (exit no shell, terminal
// fechado, conexão caída), apaga o pod. Se o próprio servidor morrer no meio, o pod tem prazo
// máximo de vida (activeDeadlineSeconds) e a próxima sessão no namespace apaga as sobras.

const (
	tshootImage       = "nicolaka/netshoot"
	tshootLabel       = "k8s-hpa-manager.io/tshoot"
	tshootMaxLifetime = 4 * time.Hour   // backstop se o servidor morrer com a sessão aberta
	tshootReadyWait   = 3 * time.Minute // pull da imagem incluso
	tshootDeleteWait  = 30 * time.Second
)

// tshootPodSpec monta o pod (separado para teste). Sem `sleep infinity`: o processo principal
// dura no máximo tshootMaxLifetime e o kubelet o mata em activeDeadlineSeconds de qualquer jeito.
func tshootPodSpec(namespace, name, user string) *corev1.Pod {
	deadline := int64(tshootMaxLifetime.Seconds())
	grace := int64(1)
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				tshootLabel:                    "true",
				"app.kubernetes.io/name":       "tshoot",
				"app.kubernetes.io/managed-by": "k8s-hpa-manager",
			},
			Annotations: map[string]string{
				"k8s-hpa-manager.io/created-by": user,
				"k8s-hpa-manager.io/purpose":    "pod de troubleshooting temporário (--rm): apagado ao fim da sessão",
			},
		},
		Spec: corev1.PodSpec{
			RestartPolicy:                 corev1.RestartPolicyNever,
			ActiveDeadlineSeconds:         &deadline,
			TerminationGracePeriodSeconds: &grace,
			Containers: []corev1.Container{{
				Name:            "tshoot",
				Image:           tshootImage,
				ImagePullPolicy: corev1.PullIfNotPresent,
				Command:         []string{"sleep", fmt.Sprint(int64(tshootMaxLifetime.Seconds()))},
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("50m"), corev1.ResourceMemory: resource.MustParse("64Mi")},
					Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("500m"), corev1.ResourceMemory: resource.MustParse("512Mi")},
				},
			}},
		},
	}
}

// nodeShellPodSpec: pod privilegiado no node, com os namespaces do host (PID/rede/IPC) e o / do
// node montado em /host — o mesmo que `kubectl debug node/<node>` (perfil "sysadmin"). Tolera
// qualquer taint para subir em nodes de sistema/cordonados. Políticas que barram privileged/
// hostPath (Gatekeeper/Kyverno) recusam na criação, e a mensagem aparece no terminal.
func nodeShellPodSpec(namespace, name, node, user string) *corev1.Pod {
	p := tshootPodSpec(namespace, name, user)
	p.Labels["app.kubernetes.io/name"] = "node-shell"
	p.Annotations["k8s-hpa-manager.io/node"] = node
	p.Annotations["k8s-hpa-manager.io/purpose"] = "shell no node temporário (--rm): apagado ao fim da sessão"
	privileged := true
	p.Spec.NodeName = node
	p.Spec.HostPID, p.Spec.HostNetwork, p.Spec.HostIPC = true, true, true
	p.Spec.DNSPolicy = corev1.DNSClusterFirstWithHostNet
	p.Spec.Tolerations = []corev1.Toleration{{Operator: corev1.TolerationOpExists}}
	p.Spec.Volumes = []corev1.Volume{{Name: "host-root", VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/"}}}}
	c := &p.Spec.Containers[0]
	c.Name = "node-shell"
	c.SecurityContext = &corev1.SecurityContext{Privileged: &privileged}
	c.VolumeMounts = []corev1.VolumeMount{{Name: "host-root", MountPath: "/host"}}
	return p
}

func newNodeShellName() string {
	return "node-shell-" + strings.TrimPrefix(newTshootName(), "tshoot-")
}

func newTshootName() string {
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	return "tshoot-" + hex.EncodeToString(b)
}

// cleanupStaleTshootPods apaga sobras de sessões anteriores no namespace: só pods que já
// terminaram (prazo estourado/encerrados). Pods Running não são tocados — podem ser sessões
// ativas de OUTRA instância do app (cada usuário roda a sua) no mesmo cluster.
func cleanupStaleTshootPods(ctx context.Context, client kubernetes.Interface, namespace string) []string {
	pods, err := client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: tshootLabel + "=true"})
	if err != nil {
		return nil
	}
	var removed []string
	for _, p := range pods.Items {
		if p.Status.Phase != corev1.PodSucceeded && p.Status.Phase != corev1.PodFailed {
			continue
		}
		if err := client.CoreV1().Pods(namespace).Delete(ctx, p.Name, metav1.DeleteOptions{}); err == nil {
			removed = append(removed, p.Name)
		}
	}
	return removed
}

// waitTshootReady espera o pod ficar Running, avisando o progresso; erro se a imagem não puder
// ser baixada, o pod terminar ou o tempo estourar.
func waitTshootReady(ctx context.Context, client kubernetes.Interface, namespace, name string, progress func(string)) error {
	deadline := time.Now().Add(tshootReadyWait)
	last := ""
	for time.Now().Before(deadline) {
		p, err := client.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("falha ao consultar o pod: %w", err)
		}
		switch p.Status.Phase {
		case corev1.PodRunning:
			return nil
		case corev1.PodFailed, corev1.PodSucceeded:
			return fmt.Errorf("o pod terminou antes de ficar pronto (%s: %s)", p.Status.Phase, p.Status.Reason)
		}
		state := string(p.Status.Phase)
		for _, cs := range p.Status.ContainerStatuses {
			if w := cs.State.Waiting; w != nil && w.Reason != "" {
				state = w.Reason
				switch w.Reason {
				case "ErrImagePull", "ImagePullBackOff", "InvalidImageName", "CreateContainerConfigError":
					return fmt.Errorf("%s: %s", w.Reason, w.Message)
				}
			}
		}
		for _, cond := range p.Status.Conditions {
			if cond.Type == corev1.PodScheduled && cond.Status == corev1.ConditionFalse && cond.Message != "" {
				state = "não agendado: " + cond.Message
			}
		}
		if state != last {
			progress(state)
			last = state
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return fmt.Errorf("o pod não ficou pronto em %s (último estado: %s)", tshootReadyWait, last)
}

// HandleTshoot — WS /api/v1/tshoot/:cluster/:namespace/ws
func (h *PodExecHandler) HandleTshoot(c *gin.Context) {
	namespace := c.Param("namespace")
	pod := tshootPodSpec(namespace, newTshootName(), GetUserInfoForHistory(c).Email)
	h.runRmPodSession(c, pod, "tshoot", "")
}

// HandleNodeShell — WS /api/v1/node-shell/:cluster/:node/ws?namespace=<ns>
// Shell no node (como `kubectl debug node/<node> -it --image nicolaka/netshoot`), também com --rm.
func (h *PodExecHandler) HandleNodeShell(c *gin.Context) {
	node := c.Param("node")
	namespace := c.DefaultQuery("namespace", "default")
	pod := nodeShellPodSpec(namespace, newNodeShellName(), node, GetUserInfoForHistory(c).Email)
	h.runRmPodSession(c, pod, "node_shell",
		"\x1b[1;33mDica:\x1b[0m \x1b[1mchroot /host\x1b[0m entra no sistema do próprio node (o / dele está montado em /host).\r\n")
}

// runRmPodSession é o ciclo comum dos pods com --rm (tshoot e shell no node): limpa sobras,
// cria o pod, espera Running, abre o bash com TTY e APAGA o pod quando a sessão termina por
// qualquer motivo (exit, terminal fechado, conexão caída). kind vai para o histórico.
func (h *PodExecHandler) runRmPodSession(c *gin.Context, pod *corev1.Pod, kind, readyHint string) {
	cluster, namespace, name := c.Param("cluster"), pod.Namespace, pod.Name
	clientset, restConfig, err := h.getClientAndConfig(cluster)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to get client: %v", err)})
		return
	}
	conn, err := h.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("[TSHOOT] upgrade falhou: %v", err)
		return
	}
	defer conn.Close()
	ctx := c.Request.Context()

	if removed := cleanupStaleTshootPods(ctx, clientset, namespace); len(removed) > 0 {
		h.sendOutput(conn, fmt.Sprintf("\x1b[2m🧹 Sobras de sessões anteriores removidas: %s\x1b[0m\r\n", strings.Join(removed, ", ")))
	}

	where := namespace
	if pod.Spec.NodeName != "" {
		where = fmt.Sprintf("%s, no node %s", namespace, pod.Spec.NodeName)
	}
	h.sendOutput(conn, fmt.Sprintf("\x1b[1;34m⚡ Criando pod %s (%s) em %s...\x1b[0m\r\n", name, tshootImage, where))
	start := time.Now()
	if _, err := clientset.CoreV1().Pods(namespace).Create(ctx, pod, metav1.CreateOptions{}); err != nil {
		// Recusa de Gatekeeper/Kyverno chega aqui com a mensagem do webhook.
		h.sendError(conn, fmt.Sprintf("não foi possível criar o pod: %v", err))
		h.recordTshoot(c, "create_"+kind+"_pod", namespace, name, cluster, "failed", err.Error(), start)
		return
	}
	h.recordTshoot(c, "create_"+kind+"_pod", namespace, name, cluster, "success", "", start)

	// --rm: aconteça o que acontecer daqui em diante, o pod sai. Contexto próprio porque o da
	// requisição já está cancelado quando o browser fecha.
	defer func() {
		delCtx, cancel := context.WithTimeout(context.Background(), tshootDeleteWait)
		defer cancel()
		zero := int64(0)
		delStart := time.Now()
		err := clientset.CoreV1().Pods(namespace).Delete(delCtx, name, metav1.DeleteOptions{GracePeriodSeconds: &zero})
		status, msg := "success", ""
		if err != nil {
			status, msg = "failed", err.Error()
			log.Printf("[TSHOOT] falha ao apagar %s/%s: %v (o activeDeadlineSeconds encerra em até %s)", namespace, name, err, tshootMaxLifetime)
			h.sendOutput(conn, fmt.Sprintf("\r\n\x1b[1;31m❌ Falha ao apagar o pod %s: %v\x1b[0m\r\n", name, err))
		} else {
			h.sendOutput(conn, fmt.Sprintf("\r\n\x1b[1;32m🗑  Pod %s removido (--rm).\x1b[0m\r\n", name))
		}
		h.recordTshoot(c, "delete_"+kind+"_pod", namespace, name, cluster, status, msg, delStart)
	}()

	if err := waitTshootReady(ctx, clientset, namespace, name, func(state string) {
		h.sendOutput(conn, fmt.Sprintf("\x1b[2m⏳ %s\x1b[0m\r\n", state))
	}); err != nil {
		h.sendError(conn, err.Error())
		return
	}
	h.sendOutput(conn, fmt.Sprintf("\x1b[1;32m✓ Pod %s pronto.\x1b[0m Saia com \x1b[1mexit\x1b[0m (ou fechando o terminal) — o pod é apagado em seguida.\r\n%s\r\n", name, readyHint))

	h.execInPod(ctx, conn, clientset, restConfig, namespace, name, pod.Spec.Containers[0].Name, "bash", false, "", false, false)
}

func (h *PodExecHandler) recordTshoot(c *gin.Context, action, namespace, name, cluster, status, errMsg string, start time.Time) {
	if h.historyTracker == nil {
		return
	}
	entry := CreateHistoryEntry(c, action, namespace+"/"+name, cluster, status, nil,
		map[string]interface{}{"image": tshootImage, "rm": true}, time.Since(start).Milliseconds(), errMsg)
	if err := h.historyTracker.Log(entry); err != nil {
		log.Printf("[TSHOOT] falha ao registrar histórico: %v", err)
	}
}
