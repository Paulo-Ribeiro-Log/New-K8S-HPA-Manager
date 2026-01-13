package sanitizer

import (
	"os"
	"path/filepath"
	"testing"
)

// TestSanitizeToFileDemo demonstra sanitização com arquivos reais
func TestSanitizeToFileDemo(t *testing.T) {
	sanitizer := New()

	// Dados de exemplo com TODOS os tipos de dados sensíveis
	demoData := `# POD CRASH ANALYSIS - Demo
# Cluster: prod-cluster
# Namespace: ecommerce-api
# Pod: payment-service-7b5c9d8f4-x9k2l

=== EVENTS ===
Warning  BackOff  10m   kubelet  Back-off restarting failed container
Normal   Pulling  9m    kubelet  Pulling image "registry.internal/payment-api:v2.1.0"
Warning  Failed   9m    kubelet  Failed to pull image: connection timeout

=== LOGS (CURRENT) ===
2026-01-06 10:15:00 [INFO] Payment Service v2.1.0 starting...
2026-01-06 10:15:01 [INFO] Loading configuration from environment
2026-01-06 10:15:02 [DEBUG] DB_HOST=10.0.0.100
2026-01-06 10:15:03 [DEBUG] DB_PORT=27017
2026-01-06 10:15:04 [DEBUG] MONGODB_URI=mongodb://svc_payments:P@yM3nt$3cr3t2024@mdbp-payments-cluster-1.dc.company.com:27017/payments_db?ssl=true&replicaSet=rs-payments
2026-01-06 10:15:05 [DEBUG] REDIS_CONNECTION=redis://cache_user:C@ch3P@ss@redis-master.internal.svc:6379/0
2026-01-06 10:15:06 [DEBUG] API_KEY=sk-live-AbCdEfGhIjKlMnOpQrStUvWxYz0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ
2026-01-06 10:15:07 [INFO] Connecting to external payment gateway at 213.123.45.67:8443
2026-01-06 10:15:08 [ERROR] Connection timeout to external API server 8.8.8.8:443
2026-01-06 10:15:09 [ERROR] Failed to resolve payment-gateway.external.com (172.168.1.213)
2026-01-06 10:15:10 [INFO] Internal services OK: kafka.internal.svc:9092 (10.0.0.200)
2026-01-06 10:15:11 [DEBUG] Loading TLS certificate: /etc/ssl/certs/payment-api-prod.cert
2026-01-06 10:15:12 [DEBUG] Loading private key: /etc/ssl/private/payment-api-prod.key
2026-01-06 10:15:13 [ERROR] Certificate validation failed with token: MDFhZ2hhdGhnaHRoZ2h0aGdodGhnaHRoZ2h0aGdodGhnaHRoZ2h0aGdodFJrND0=
2026-01-06 10:15:14 [INFO] JWT Auth Token: eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyfQ.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c
2026-01-06 10:15:15 [ERROR] Authentication failed for user: admin@company.com
2026-01-06 10:15:16 [ERROR] Failed to connect to database server at db-replica-3.internal.net
2026-01-06 10:15:17 [INFO] Retrying connection string: svc_replica:R3pl1c@P@ss@db-replica-3.internal.net:5432
2026-01-06 10:15:18 [FATAL] Cannot start application - exiting with code 1

=== LOGS (PREVIOUS - BEFORE CRASH) ===
2026-01-06 10:10:00 [INFO] Payment Service v2.0.9 started successfully
2026-01-06 10:10:30 [WARN] High memory usage detected: 85%
2026-01-06 10:11:00 [ERROR] Out of memory error
2026-01-06 10:11:01 [INFO] Saving state to postgresql://backup_user:B@ckupP@ss123@backup-db.company.local:5432/backup_db
2026-01-06 10:11:02 [FATAL] Process killed by OOM killer

=== CONFIGMAP DATA ===
mongodb_connection: "mongodb://app:MyP@ssw0rd!2024@mdbh-prod-cluster-1.dc.example.com:27017/app_db?ssl=true"
redis_url: "redis://cache:secret123@cache-master.internal.svc:6379"
external_api_endpoint: "https://api.external.com:8443"
internal_api_endpoint: "http://10.0.0.150:8080"
certificate_path: "/etc/ssl/certs/app.cert"
private_key_path: "/etc/ssl/private/app.key"
auth_token_base64: "QXV0aEtleVNlY3JldFRva2VuVGhhdElzVmVyeUxvbmdBbmRTaG91bGRCZU1hc2tlZA=="
`

	// Sanitizar e salvar em arquivos
	sanitizedFile, originalFile, result, err := sanitizer.SanitizeFileWithReport(demoData, "Pod", "payment-service-7b5c9d8f4-x9k2l")
	if err != nil {
		t.Fatalf("Failed to sanitize to file: %v", err)
	}

	// Verificar que arquivos foram criados
	if _, err := os.Stat(originalFile); os.IsNotExist(err) {
		t.Errorf("Original file was not created: %s", originalFile)
	}
	if _, err := os.Stat(sanitizedFile); os.IsNotExist(err) {
		t.Errorf("Sanitized file was not created: %s", sanitizedFile)
	}

	// Imprimir localização dos arquivos
	t.Logf("\n======================================")
	t.Logf("✅ ARQUIVOS DE SANITIZAÇÃO GERADOS:")
	t.Logf("======================================")
	t.Logf("📁 Diretório: %s", filepath.Dir(originalFile))
	t.Logf("")
	t.Logf("📄 Arquivo ORIGINAL (dados sensíveis):")
	t.Logf("   %s", originalFile)
	t.Logf("")
	t.Logf("📄 Arquivo SANITIZADO (dados mascarados):")
	t.Logf("   %s", sanitizedFile)
	t.Logf("")
	t.Logf("📊 ESTATÍSTICAS:")
	for itemType, count := range result.MaskedItems {
		t.Logf("   - %s: %d ocorrências", itemType, count)
	}
	t.Logf("======================================")
	t.Logf("")
	t.Logf("💡 COMANDO PARA VISUALIZAR:")
	t.Logf("   cat %s", originalFile)
	t.Logf("   cat %s", sanitizedFile)
	t.Logf("")
	t.Logf("💡 COMANDO PARA COMPARAR LADO A LADO:")
	t.Logf("   diff -y --width=200 %s %s | less", originalFile, sanitizedFile)
	t.Logf("======================================")
}

// TestCleanupOldFiles testa a limpeza de arquivos antigos
func TestCleanupOldFiles(t *testing.T) {
	// Limpar arquivos com mais de 24 horas
	if err := CleanupOldFiles(24); err != nil {
		t.Logf("Cleanup completed with some errors: %v", err)
	} else {
		t.Logf("Cleanup completed successfully")
	}

	debugDir := GetDebugDirectory()
	t.Logf("Debug directory: %s", debugDir)
}

// TestGenerateMultipleScenarios gera arquivos para diferentes cenários
func TestGenerateMultipleScenarios(t *testing.T) {
	sanitizer := New()

	scenarios := []struct {
		name string
		data string
	}{
		{
			name: "scenario1-mongodb-crash",
			data: `Failed to connect: mongodb://user:senha123@mdbh-prod.dc.acme.com:27017/
External API timeout: 8.8.8.8:443
Certificate: production.cert
Token: MDFhZ2hhdGhnaHRoZ2h0aGdodGhnaHRoZ2h0aGdodGhnaHRoZ2h0aGdodFJrND0=`,
		},
		{
			name: "scenario2-redis-connection",
			data: `Connecting to Redis: redis://cache:secret123@cache-master.svc:6379
Internal service: 10.0.0.50:8080
External service: 213.123.45.67:8443
Private key: app-private.key`,
		},
		{
			name: "scenario3-mixed-connections",
			data: `PostgreSQL: postgresql://db_user:P@ssw0rd!@db-prod.internal.net:5432/app
MySQL: mysql://root:MyS3cr3t@db-mysql.svc:3306/data
External IP: 172.168.1.213
Internal IP: 192.168.1.100
Base64: QXV0aEtleVNlY3JldFRva2VuVGhhdElzVmVyeUxvbmdBbmRTaG91bGRCZU1hc2tlZA==`,
		},
	}

	t.Logf("\n======================================")
	t.Logf("GERANDO ARQUIVOS PARA %d CENÁRIOS", len(scenarios))
	t.Logf("======================================")

	for i, scenario := range scenarios {
		sanitizedFile, originalFile, _, err := sanitizer.SanitizeFileWithReport(scenario.data, "Pod", scenario.name)
		if err != nil {
			t.Errorf("Failed to sanitize scenario %s: %v", scenario.name, err)
			continue
		}

		t.Logf("\n%d. %s:", i+1, scenario.name)
		t.Logf("   Original:   %s", originalFile)
		t.Logf("   Sanitizado: %s", sanitizedFile)
	}

	t.Logf("\n======================================")
	t.Logf("💡 Para comparar um cenário específico:")
	t.Logf("   diff -y scenario1-mongodb-crash*ORIGINAL* scenario1-mongodb-crash*SANITIZED*")
	t.Logf("======================================")
}
