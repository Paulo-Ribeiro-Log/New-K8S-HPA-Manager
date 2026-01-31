package healthcheck

import (
	"testing"
)

func TestNewServiceChecker(t *testing.T) {
	sc := NewServiceChecker()

	if sc.jobTTLSeconds != 60 {
		t.Errorf("jobTTLSeconds: esperado 60, obteve %d", sc.jobTTLSeconds)
	}
	if sc.activeDeadlineSecs != 30 {
		t.Errorf("activeDeadlineSecs: esperado 30, obteve %d", sc.activeDeadlineSecs)
	}
	if sc.backoffLimit != 0 {
		t.Errorf("backoffLimit: esperado 0, obteve %d", sc.backoffLimit)
	}
	if sc.testTimeoutSeconds != 5 {
		t.Errorf("testTimeoutSeconds: esperado 5, obteve %d", sc.testTimeoutSeconds)
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

func TestCreateSecureJob(t *testing.T) {
	sc := NewServiceChecker()
	job := sc.createSecureJob("test-job", "default", "echo test")

	// Verificar nome e namespace
	if job.Name != "test-job" {
		t.Errorf("nome esperado 'test-job', obteve '%s'", job.Name)
	}
	if job.Namespace != "default" {
		t.Errorf("namespace esperado 'default', obteve '%s'", job.Namespace)
	}

	// Verificar labels de segurança
	if job.Labels["app.kubernetes.io/managed-by"] != "hpa-manager" {
		t.Error("label managed-by ausente ou incorreto")
	}

	// Verificar TTL
	if job.Spec.TTLSecondsAfterFinished == nil || *job.Spec.TTLSecondsAfterFinished != 60 {
		t.Error("TTLSecondsAfterFinished deve ser 60")
	}

	// Verificar SecurityContext do container
	container := job.Spec.Template.Spec.Containers[0]
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
	if job.Spec.Template.Spec.AutomountServiceAccountToken == nil || *job.Spec.Template.Spec.AutomountServiceAccountToken {
		t.Error("AutomountServiceAccountToken deve ser false")
	}

	// Verificar imagem
	if container.Image != "busybox:1.36" {
		t.Errorf("imagem esperada 'busybox:1.36', obteve '%s'", container.Image)
	}

	// Verificar recursos
	if container.Resources.Limits.Cpu().String() != "50m" {
		t.Errorf("CPU limit esperado '50m', obteve '%s'", container.Resources.Limits.Cpu().String())
	}
	if container.Resources.Limits.Memory().String() != "32Mi" {
		t.Errorf("Memory limit esperado '32Mi', obteve '%s'", container.Resources.Limits.Memory().String())
	}
}

func TestCreateSecureJobCommand(t *testing.T) {
	sc := NewServiceChecker()

	// Testar que o comando é passado corretamente
	cmd := "nc -zv -w5 test-host 8080 2>&1 && echo SUCCESS || echo FAILED"
	job := sc.createSecureJob("check-job", "myns", cmd)

	container := job.Spec.Template.Spec.Containers[0]

	// Verificar que usa /bin/sh -c
	if len(container.Command) != 3 {
		t.Fatalf("Command deve ter 3 elementos, obteve %d", len(container.Command))
	}
	if container.Command[0] != "/bin/sh" {
		t.Errorf("Command[0] esperado '/bin/sh', obteve '%s'", container.Command[0])
	}
	if container.Command[1] != "-c" {
		t.Errorf("Command[1] esperado '-c', obteve '%s'", container.Command[1])
	}
	if container.Command[2] != cmd {
		t.Errorf("Command[2] esperado '%s', obteve '%s'", cmd, container.Command[2])
	}
}

func TestCreateSecureJobSecurityPod(t *testing.T) {
	sc := NewServiceChecker()
	job := sc.createSecureJob("sec-job", "default", "echo test")

	podSpec := job.Spec.Template.Spec

	// Verificar SecurityContext do Pod
	if podSpec.SecurityContext == nil {
		t.Fatal("Pod SecurityContext não definido")
	}

	psc := podSpec.SecurityContext
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
