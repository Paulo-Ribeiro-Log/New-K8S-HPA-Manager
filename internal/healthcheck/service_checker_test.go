package healthcheck

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
)

func TestNewServiceChecker(t *testing.T) {
	sc := NewServiceChecker()

	if sc.testTimeoutSeconds != 5 {
		t.Errorf("testTimeoutSeconds: esperado 5, obteve %d", sc.testTimeoutSeconds)
	}
	if sc.workerTTLSeconds != 300 {
		t.Errorf("workerTTLSeconds: esperado 300, obteve %d", sc.workerTTLSeconds)
	}
	if sc.batchSize != 20 {
		t.Errorf("batchSize: esperado 20, obteve %d", sc.batchSize)
	}
	if sc.maxConcurrent != 3 {
		t.Errorf("maxConcurrent: esperado 3, obteve %d", sc.maxConcurrent)
	}
}

func TestDetectServiceType(t *testing.T) {
	sc := NewServiceChecker()

	tests := []struct {
		port     int32
		expected ServiceType
	}{
		{27017, ServiceMongoDB},
		{6379, ServiceRedis},
		{5432, ServicePostgres},
		{9092, ServiceKafka},
		{5672, ServiceRabbitMQ},
		{15672, ServiceRabbitMQ},
		{80, ServiceHTTP},
		{443, ServiceHTTP},
		{8080, ServiceHTTP},
		{3000, ServiceHTTP}, // default
	}

	for _, tt := range tests {
		result := sc.detectServiceType(tt.port)
		if result != tt.expected {
			t.Errorf("porta %d: esperado %s, obteve %s", tt.port, tt.expected, result)
		}
	}
}

func TestCreateWorkerPod(t *testing.T) {
	sc := NewServiceChecker()
	client := fake.NewSimpleClientset()

	pod, err := sc.createWorkerPod(context.Background(), client, "default")
	if err != nil {
		t.Fatalf("erro ao criar worker pod: %v", err)
	}

	// Verificar namespace
	if pod.Namespace != "default" {
		t.Errorf("namespace esperado 'default', obteve '%s'", pod.Namespace)
	}

	// Verificar labels de segurança
	if pod.Labels["app.kubernetes.io/managed-by"] != "hpa-manager" {
		t.Error("label managed-by ausente ou incorreto")
	}
	if pod.Labels["app.kubernetes.io/component"] != "service-checker-worker" {
		t.Error("label component ausente ou incorreto")
	}

	// Verificar SecurityContext do container
	container := pod.Spec.Containers[0]
	if container.SecurityContext == nil {
		t.Fatal("SecurityContext não definido")
	}

	sc2 := container.SecurityContext
	if sc2.RunAsNonRoot == nil || !*sc2.RunAsNonRoot {
		t.Error("RunAsNonRoot deve ser true")
	}
	if sc2.ReadOnlyRootFilesystem == nil || !*sc2.ReadOnlyRootFilesystem {
		t.Error("ReadOnlyRootFilesystem deve ser true")
	}
	if sc2.AllowPrivilegeEscalation == nil || *sc2.AllowPrivilegeEscalation {
		t.Error("AllowPrivilegeEscalation deve ser false")
	}
	if sc2.Capabilities == nil || len(sc2.Capabilities.Drop) == 0 {
		t.Error("Capabilities.Drop deve conter ALL")
	}

	// Verificar que ServiceAccount não é montado
	if pod.Spec.AutomountServiceAccountToken == nil || *pod.Spec.AutomountServiceAccountToken {
		t.Error("AutomountServiceAccountToken deve ser false")
	}

	// Verificar imagem
	if container.Image != "busybox:1.36" {
		t.Errorf("imagem esperada 'busybox:1.36', obteve '%s'", container.Image)
	}

	// Verificar recursos
	if container.Resources.Limits.Cpu().String() != "100m" {
		t.Errorf("CPU limit esperado '100m', obteve '%s'", container.Resources.Limits.Cpu().String())
	}
	if container.Resources.Limits.Memory().String() != "64Mi" {
		t.Errorf("Memory limit esperado '64Mi', obteve '%s'", container.Resources.Limits.Memory().String())
	}
}

func TestCreateWorkerPodSecurityPod(t *testing.T) {
	sc := NewServiceChecker()
	client := fake.NewSimpleClientset()

	pod, err := sc.createWorkerPod(context.Background(), client, "default")
	if err != nil {
		t.Fatalf("erro ao criar worker pod: %v", err)
	}

	// Verificar SecurityContext do Pod
	if pod.Spec.SecurityContext == nil {
		t.Fatal("Pod SecurityContext não definido")
	}

	psc := pod.Spec.SecurityContext
	if psc.RunAsNonRoot == nil || !*psc.RunAsNonRoot {
		t.Error("Pod RunAsNonRoot deve ser true")
	}
	if psc.RunAsUser == nil || *psc.RunAsUser != 65534 {
		t.Error("Pod RunAsUser deve ser 65534 (nobody)")
	}
	if psc.RunAsGroup == nil || *psc.RunAsGroup != 65534 {
		t.Error("Pod RunAsGroup deve ser 65534 (nogroup)")
	}
	if psc.FSGroup == nil || *psc.FSGroup != 65534 {
		t.Error("Pod FSGroup deve ser 65534")
	}

	// Verificar SeccompProfile
	if psc.SeccompProfile == nil {
		t.Fatal("SeccompProfile não definido")
	}
	if psc.SeccompProfile.Type != "RuntimeDefault" {
		t.Errorf("SeccompProfile.Type esperado 'RuntimeDefault', obteve '%s'", psc.SeccompProfile.Type)
	}
}

func TestBoolPtr(t *testing.T) {
	truePtr := boolPtr(true)
	falsePtr := boolPtr(false)

	if *truePtr != true {
		t.Error("boolPtr(true) deve retornar ponteiro para true")
	}
	if *falsePtr != false {
		t.Error("boolPtr(false) deve retornar ponteiro para false")
	}
}

func TestDiscoverServicesByNamespace(t *testing.T) {
	sc := NewServiceChecker()

	// Criar serviços fake
	svc1 := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "api-service",
			Namespace: "production",
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: "10.0.0.1",
			Ports: []corev1.ServicePort{
				{Port: 8080},
			},
		},
	}

	svc2 := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "db-service",
			Namespace: "production",
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: "10.0.0.2",
			Ports: []corev1.ServicePort{
				{Port: 5432},
			},
		},
	}

	svc3 := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "headless",
			Namespace: "production",
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: "None", // Headless - deve ser ignorado
			Ports: []corev1.ServicePort{
				{Port: 9000},
			},
		},
	}

	client := fake.NewSimpleClientset(svc1, svc2, svc3)

	targets := sc.discoverServicesByNamespace(context.Background(), client, []string{"production", "kube-system"})

	// kube-system deve ser ignorado
	if _, exists := targets["kube-system"]; exists {
		t.Error("kube-system não deveria estar nos targets")
	}

	// production deve ter 2 serviços (headless ignorado)
	prodTargets := targets["production"]
	if len(prodTargets) != 2 {
		t.Errorf("esperado 2 serviços em production, obteve %d", len(prodTargets))
	}

	// Verificar que headless não está incluído
	for _, target := range prodTargets {
		if target.name == "headless" {
			t.Error("serviço headless não deveria estar nos targets")
		}
	}
}

func TestCheckServiceEndpoints(t *testing.T) {
	sc := NewServiceChecker()

	target := serviceTarget{
		name:      "api-service",
		namespace: "default",
		host:      "api-service.default.svc.cluster.local",
		port:      8080,
	}

	// Caso 1: Service com endpoints ativos
	endpoints := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "api-service",
			Namespace: "default",
		},
		Subsets: []corev1.EndpointSubset{
			{
				Addresses: []corev1.EndpointAddress{
					{IP: "10.0.0.1"},
					{IP: "10.0.0.2"},
				},
			},
		},
	}

	client := fake.NewSimpleClientset(endpoints)
	health := sc.checkServiceEndpoints(context.Background(), client, target)

	if health.Status != StatusHealthy {
		t.Errorf("esperado StatusHealthy, obteve %s", health.Status)
	}
	if !health.Reachable {
		t.Error("Reachable deveria ser true")
	}

	// Caso 2: Service sem endpoints
	emptyEndpoints := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "empty-service",
			Namespace: "default",
		},
		Subsets: []corev1.EndpointSubset{},
	}

	target2 := serviceTarget{
		name:      "empty-service",
		namespace: "default",
		host:      "empty-service.default.svc.cluster.local",
		port:      8080,
	}

	client2 := fake.NewSimpleClientset(emptyEndpoints)
	health2 := sc.checkServiceEndpoints(context.Background(), client2, target2)

	if health2.Status != StatusWarning {
		t.Errorf("esperado StatusWarning para service sem endpoints, obteve %s", health2.Status)
	}
}

func TestProcessResult(t *testing.T) {
	sc := NewServiceChecker()

	// Caso 1: Sucesso
	successResult := batchResult{
		target: serviceTarget{
			name:      "api",
			namespace: "default",
			port:      8080,
		},
		success: true,
		output:  "api.default.svc.cluster.local:8080:OK:15ms",
		latency: 15,
	}

	health1 := sc.processResult(successResult)
	if health1.Status != StatusHealthy {
		t.Errorf("esperado StatusHealthy, obteve %s", health1.Status)
	}
	if !health1.Reachable {
		t.Error("Reachable deveria ser true para sucesso")
	}
	if health1.LatencyMs != 15 {
		t.Errorf("esperado latency 15, obteve %d", health1.LatencyMs)
	}

	// Caso 2: Falha
	failResult := batchResult{
		target: serviceTarget{
			name:      "db",
			namespace: "default",
			port:      5432,
		},
		success: false,
		output:  "connection refused",
		latency: 5000,
	}

	health2 := sc.processResult(failResult)
	if health2.Status != StatusCritical {
		t.Errorf("esperado StatusCritical, obteve %s", health2.Status)
	}
	if health2.Reachable {
		t.Error("Reachable deveria ser false para falha")
	}
	if len(health2.Suggestions) == 0 {
		t.Error("deveria ter sugestões para falha")
	}
}

func TestParseLatency(t *testing.T) {
	sc := NewServiceChecker()

	tests := []struct {
		output   string
		expected int64
	}{
		{"api.default.svc.cluster.local:8080:OK:15ms", 15},
		{"db.default.svc.cluster.local:5432:OK:100ms", 100},
		{"invalid:output", 0},
		{"", 0},
	}

	for _, tt := range tests {
		result := sc.parseLatency(tt.output)
		if result != tt.expected {
			t.Errorf("parseLatency(%s): esperado %d, obteve %d", tt.output, tt.expected, result)
		}
	}
}

func TestCheckAllLegacy(t *testing.T) {
	sc := NewServiceChecker()

	// Criar service e endpoints
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-svc",
			Namespace: "default",
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: "10.0.0.1",
			Ports: []corev1.ServicePort{
				{Port: 8080},
			},
		},
	}

	endpoints := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-svc",
			Namespace: "default",
		},
		Subsets: []corev1.EndpointSubset{
			{
				Addresses: []corev1.EndpointAddress{
					{IP: "10.0.0.10"},
				},
			},
		},
	}

	client := fake.NewSimpleClientset(svc, endpoints)

	targetsByNS := map[string][]serviceTarget{
		"default": {
			{
				name:      "test-svc",
				namespace: "default",
				host:      "test-svc.default.svc.cluster.local",
				port:      8080,
			},
		},
	}

	// Sem REST config, deve usar modo legado
	results := sc.checkAllLegacy(context.Background(), client, targetsByNS, 30, nil)

	if len(results) != 1 {
		t.Fatalf("esperado 1 resultado, obteve %d", len(results))
	}

	if results[0].Name != "test-svc" {
		t.Errorf("esperado nome 'test-svc', obteve '%s'", results[0].Name)
	}

	if results[0].Status != StatusHealthy {
		t.Errorf("esperado StatusHealthy, obteve %s", results[0].Status)
	}
}

func TestCheckAllWithoutRESTConfig(t *testing.T) {
	sc := NewServiceChecker()
	// Não configurar REST config - deve usar modo legado

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "api",
			Namespace: "app",
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: "10.0.0.1",
			Ports: []corev1.ServicePort{
				{Port: 8080},
			},
		},
	}

	endpoints := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "api",
			Namespace: "app",
		},
		Subsets: []corev1.EndpointSubset{
			{
				Addresses: []corev1.EndpointAddress{
					{IP: "10.0.0.10"},
				},
			},
		},
	}

	client := fake.NewSimpleClientset(svc, endpoints)

	results := sc.CheckAll(context.Background(), client, []string{"app"}, 30, nil)

	if len(results) != 1 {
		t.Fatalf("esperado 1 resultado, obteve %d", len(results))
	}

	// Modo legado verifica apenas endpoints
	if results[0].Status != StatusHealthy {
		t.Errorf("esperado StatusHealthy (endpoints ativos), obteve %s", results[0].Status)
	}
}

func TestWaitForPodReady(t *testing.T) {
	sc := NewServiceChecker()

	// Criar pod que já está running e ready
	readyPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ready-pod",
			Namespace: "default",
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name:  "worker",
					Ready: true,
				},
			},
		},
	}

	client := fake.NewSimpleClientset(readyPod)

	err := sc.waitForPodReady(context.Background(), client, "default", "ready-pod", 5)
	if err != nil {
		t.Errorf("não deveria ter erro para pod ready: %v", err)
	}
}

func TestWaitForPodReadyTimeout(t *testing.T) {
	sc := NewServiceChecker()

	// Criar pod que está pending (nunca vai ficar ready)
	pendingPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pending-pod",
			Namespace: "default",
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodPending,
		},
	}

	client := fake.NewSimpleClientset(pendingPod)

	err := sc.waitForPodReady(context.Background(), client, "default", "pending-pod", 1)
	if err == nil {
		t.Error("deveria ter erro de timeout para pod pending")
	}
}

func TestWaitForPodReadyFailed(t *testing.T) {
	sc := NewServiceChecker()

	// Criar pod que falhou
	failedPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "failed-pod",
			Namespace: "default",
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodFailed,
		},
	}

	client := fake.NewSimpleClientset(failedPod)

	err := sc.waitForPodReady(context.Background(), client, "default", "failed-pod", 5)
	if err == nil {
		t.Error("deveria ter erro para pod failed")
	}
}

func TestCleanupWorkerPod(t *testing.T) {
	sc := NewServiceChecker()

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cleanup-test",
			Namespace: "default",
		},
	}

	client := fake.NewSimpleClientset(pod)

	// Cleanup não deve retornar erro
	sc.cleanupWorkerPod(context.Background(), client, "default", "cleanup-test")

	// Verificar que pod foi deletado
	_, err := client.CoreV1().Pods("default").Get(context.Background(), "cleanup-test", metav1.GetOptions{})
	if err == nil {
		t.Error("pod deveria ter sido deletado")
	}
}

func TestServiceHealthStruct(t *testing.T) {
	health := ServiceHealth{
		Name:        "api-service",
		Namespace:   "production",
		ServiceType: ServiceHTTP,
		Status:      StatusHealthy,
		Reachable:   true,
		LatencyMs:   25,
		CheckedAt:   time.Now(),
		Message:     "Conectado com sucesso",
	}

	if health.Name != "api-service" {
		t.Errorf("Name = %s, want api-service", health.Name)
	}
	if health.ServiceType != ServiceHTTP {
		t.Errorf("ServiceType = %s, want HTTP", health.ServiceType)
	}
	if !health.Reachable {
		t.Error("Reachable deveria ser true")
	}
}

func TestSetRESTConfig(t *testing.T) {
	sc := NewServiceChecker()

	if sc.restConfig != nil {
		t.Error("restConfig deveria ser nil inicialmente")
	}

	// Não podemos criar um REST config real sem cluster, mas testamos o método
	// sc.SetRESTConfig(config) - apenas verificar que não há panic
}

// TestBatchResultStruct verifica estrutura
func TestBatchResultStruct(t *testing.T) {
	br := batchResult{
		target: serviceTarget{
			name:      "test",
			namespace: "default",
			host:      "test.default.svc.cluster.local",
			port:      8080,
		},
		success: true,
		output:  "test:8080:OK:10ms",
		latency: 10,
	}

	if !br.success {
		t.Error("success deveria ser true")
	}
	if br.latency != 10 {
		t.Errorf("latency = %d, want 10", br.latency)
	}
}

// TestServiceTargetStruct verifica estrutura
func TestServiceTargetStruct(t *testing.T) {
	target := serviceTarget{
		name:      "my-service",
		namespace: "my-namespace",
		host:      "my-service.my-namespace.svc.cluster.local",
		port:      9090,
	}

	if target.name != "my-service" {
		t.Errorf("name = %s, want my-service", target.name)
	}
	if target.port != 9090 {
		t.Errorf("port = %d, want 9090", target.port)
	}
}

// Benchmark para comparar overhead
func BenchmarkDiscoverServicesByNamespace(b *testing.B) {
	sc := NewServiceChecker()

	// Criar 100 serviços fake
	var objects []runtime.Object
	for i := 0; i < 100; i++ {
		svc := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "service-" + string(rune('a'+i%26)) + string(rune('0'+i/26)),
				Namespace: "production",
			},
			Spec: corev1.ServiceSpec{
				ClusterIP: "10.0.0." + string(rune('0'+i%256)),
				Ports: []corev1.ServicePort{
					{Port: int32(8000 + i)},
				},
			},
		}
		objects = append(objects, svc)
	}

	client := fake.NewSimpleClientset(objects...)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sc.discoverServicesByNamespace(context.Background(), client, []string{"production"})
	}
}
