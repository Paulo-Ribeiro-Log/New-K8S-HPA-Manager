package handlers

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"sort"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	// latencyTestPodImage é pequena e já traz curl — evita instalar nada no pod efêmero.
	latencyTestPodImage = "curlimages/curl:8.10.1"
	// latencyTestPodActiveDeadlineSec é o cinto de segurança: o K8s mata o pod sozinho mesmo se
	// o processo do servidor morrer no meio do teste, sem depender só do cleanup() explícito.
	latencyTestPodActiveDeadlineSec int64 = 300
	latencyTestPodReadyTimeout            = 60 * time.Second
	latencyTestPodPollInterval            = 500 * time.Millisecond
)

// LatencyTestStats agrega estatísticas sobre as amostras de latência coletadas num teste.
type LatencyTestStats struct {
	MinMs    float64 `json:"min_ms"`
	AvgMs    float64 `json:"avg_ms"`
	MedianMs float64 `json:"median_ms"`
	P95Ms    float64 `json:"p95_ms"`
	P99Ms    float64 `json:"p99_ms"`
	MaxMs    float64 `json:"max_ms"`
}

// LatencyTestResult é o resultado completo de uma execução de teste de latência.
type LatencyTestResult struct {
	Samples       []float64        `json:"samples"` // ms, na ordem em que as requisições rodaram
	Stats         LatencyTestStats `json:"stats"`
	ErrorCount    int              `json:"error_count"`
	TotalRequests int              `json:"total_requests"`
}

// createTestPod cria um pod efêmero de curta duração no namespace alvo pra rodar o probe de
// latência via exec. Nasce no MESMO namespace do alvo — respeita NetworkPolicy de lá, já que a
// requisição parte de dentro, não de fora do cluster. `cleanup()` deve ser chamado sempre (defer),
// mas ActiveDeadlineSeconds garante que o K8s mata o pod sozinho mesmo se isso falhar.
func createTestPod(ctx context.Context, clientset kubernetes.Interface, namespace string) (podName string, cleanup func(), err error) {
	podName = fmt.Sprintf("latency-test-%d", rand.Int63())
	activeDeadline := latencyTestPodActiveDeadlineSec

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: namespace,
			Labels: map[string]string{
				"app":        "latency-test-tool",
				"created-by": "k8s-hpa-manager",
			},
		},
		Spec: corev1.PodSpec{
			RestartPolicy:         corev1.RestartPolicyNever,
			ActiveDeadlineSeconds: &activeDeadline,
			Containers: []corev1.Container{
				{
					Name:    "curl",
					Image:   latencyTestPodImage,
					Command: []string{"sleep", "300"},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("50m"),
							corev1.ResourceMemory: resource.MustParse("64Mi"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("200m"),
							corev1.ResourceMemory: resource.MustParse("128Mi"),
						},
					},
				},
			},
		},
	}

	if _, err = clientset.CoreV1().Pods(namespace).Create(ctx, pod, metav1.CreateOptions{}); err != nil {
		return "", nil, fmt.Errorf("falha ao criar pod de teste: %w", err)
	}

	cleanup = func() {
		delCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		gracePeriod := int64(0)
		_ = clientset.CoreV1().Pods(namespace).Delete(delCtx, podName, metav1.DeleteOptions{GracePeriodSeconds: &gracePeriod})
	}

	return podName, cleanup, nil
}

// waitPodRunning espera o pod ficar Running (poll simples) até o timeout, ou retorna erro se o
// pod terminar (Failed/Succeeded) antes disso — não deveria acontecer com `sleep 300`, mas cobre
// o caso de a imagem falhar o pull ou o namespace ter uma PodSecurityPolicy/admission bloqueando.
func waitPodRunning(ctx context.Context, clientset kubernetes.Interface, namespace, podName string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		pod, err := clientset.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("falha ao consultar status do pod de teste: %w", err)
		}
		switch pod.Status.Phase {
		case corev1.PodRunning:
			return nil
		case corev1.PodFailed, corev1.PodSucceeded:
			return fmt.Errorf("pod de teste terminou inesperadamente (fase: %s)", pod.Status.Phase)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(latencyTestPodPollInterval):
		}
	}
	return fmt.Errorf("timeout esperando pod de teste ficar pronto (%s)", timeout)
}

// runLatencyProbe roda `count` requisições HTTP via curl dentro do pod de teste num ÚNICO exec
// (não um exec por requisição — custaria uma chamada SPDY por amostra) e retorna as latências
// medidas em milissegundos, na ordem em que completaram. Linhas que o curl não conseguiu medir
// (timeout/erro de conexão) contam pra `errorCount` e não entram em `samples`.
func runLatencyProbe(ctx context.Context, clientset kubernetes.Interface, restConfig *rest.Config,
	namespace, podName, url string, count, timeoutMs int) (samples []float64, errorCount int, err error) {

	timeoutSec := float64(timeoutMs) / 1000.0
	script := fmt.Sprintf(
		`for i in $(seq 1 %d); do curl -o /dev/null -s -w "%%{time_total}\n" --max-time %.3f %q || echo ERR; done`,
		count, timeoutSec, url,
	)

	stdout, err := execCmdInPod(ctx, clientset, restConfig, namespace, podName, "curl", []string{"sh", "-c", script})
	if err != nil {
		return nil, 0, fmt.Errorf("falha ao executar probe de latência: %w", err)
	}

	for _, line := range strings.Split(strings.TrimSpace(stdout), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		seconds, parseErr := strconv.ParseFloat(line, 64)
		if parseErr != nil {
			errorCount++
			continue
		}
		samples = append(samples, seconds*1000.0)
	}

	return samples, errorCount, nil
}

// computeLatencyStats calcula min/avg/mediana/p95/p99/max sobre as amostras (ms). Amostras vazias
// retornam stats zeradas — o chamador decide como exibir "sem dados" (não é papel desta função).
func computeLatencyStats(samples []float64) LatencyTestStats {
	if len(samples) == 0 {
		return LatencyTestStats{}
	}

	sorted := make([]float64, len(samples))
	copy(sorted, samples)
	sort.Float64s(sorted)

	sum := 0.0
	for _, v := range sorted {
		sum += v
	}

	return LatencyTestStats{
		MinMs:    sorted[0],
		AvgMs:    sum / float64(len(sorted)),
		MedianMs: latencyPercentile(sorted, 50),
		P95Ms:    latencyPercentile(sorted, 95),
		P99Ms:    latencyPercentile(sorted, 99),
		MaxMs:    sorted[len(sorted)-1],
	}
}

// latencyPercentile assume `sorted` já ordenado ascendente — nearest-rank, mesmo método já usado
// em `percentile95` (internal/monitoring/nodepoolpredictions/cost_analyzer.go), só generalizado
// pra qualquer p (aquele é hardcoded em 95).
func latencyPercentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(math.Ceil(float64(len(sorted))*p/100.0)) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}
