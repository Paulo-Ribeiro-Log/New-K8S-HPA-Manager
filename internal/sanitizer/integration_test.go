package sanitizer

import (
	"strings"
	"testing"
)

// TestSanitizeLogsIntegration testa o fluxo completo de sanitização de logs
// (simula o que acontece no AI Diagnostics)
func TestSanitizeLogsIntegration(t *testing.T) {
	sanitizer := New()

	// Simula logs reais de um pod com múltiplos dados sensíveis
	rawLogs := `2026-01-06 10:00:00 [INFO] Starting application
2026-01-06 10:00:01 [INFO] Connecting to mongodb://svc_legacy_hlg:fhfjfurenfndhdhdhdhdhf@mdbh-legacydata-1.dc.nova:27017/?retryWrites=true
2026-01-06 10:00:02 [ERROR] Failed to connect to external API at 172.168.1.213:8080
2026-01-06 10:00:03 [INFO] Using internal database at 10.0.0.50:5432
2026-01-06 10:00:04 [DEBUG] Loading certificate certificado-tls.cert from /etc/ssl/certs
2026-01-06 10:00:05 [DEBUG] Private key loaded: app-private.key
2026-01-06 10:00:06 [ERROR] Authentication failed with token: MDFhghthghthghthghthghthghthghtTRk4=
2026-01-06 10:00:07 [INFO] Connecting with user:s6Yxbn1I9i98GHIJcJdc@mdbp-logreversa-1.dc.nova
2026-01-06 10:00:08 [WARN] External service timeout: http://external-api.example.com:9090
2026-01-06 10:00:09 [INFO] Local service OK: http://192.168.1.100:3000`

	// Sanitiza logs (simula o que o AI Diagnostics faz)
	sanitizedLogs := sanitizer.SanitizeLogs(rawLogs)

	// Verificações: dados sensíveis devem estar mascarados
	tests := []struct {
		name           string
		shouldContain  string
		shouldNotContain string
	}{
		{
			name:             "Connection string MongoDB - senha mascarada",
			shouldContain:    "mongodb://svc_legacy_hlg:fhf***dhf@mdbh-legacydata-1.dc.nova",
			shouldNotContain: "fhfjfurenfndhdhdhdhdhdhf",
		},
		{
			name:             "IP externo mascarado",
			shouldContain:    "172.***.***.213",
			shouldNotContain: "172.168.1.213",
		},
		{
			name:             "IP interno NÃO mascarado",
			shouldContain:    "10.0.0.50",
			shouldNotContain: "",
		},
		{
			name:             "IP interno 192.168.x NÃO mascarado",
			shouldContain:    "192.168.1.100",
			shouldNotContain: "",
		},
		{
			name:             "Certificado .cert mascarado",
			shouldContain:    "[cert:certificado-tls]",
			shouldNotContain: "certificado-tls.cert",
		},
		{
			name:             "Chave privada .key mascarada",
			shouldContain:    "[key:app-private]",
			shouldNotContain: "app-private.key",
		},
		{
			name:             "Base64 mascarado",
			shouldContain:    "MDF***Rk4=",
			shouldNotContain: "MDFhghthghthghthghthghthghthghtTRk4=",
		},
		{
			name:             "Connection string sem protocolo - senha mascarada",
			shouldContain:    "user:s6Y***Jdc@mdbp-logreversa-1.dc.nova",
			shouldNotContain: "s6Yxbn1I9i98GHIJcJdc",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Verifica se contém o valor esperado (mascarado)
			if tt.shouldContain != "" && !strings.Contains(sanitizedLogs, tt.shouldContain) {
				t.Errorf("Expected sanitized logs to contain %q, but it doesn't.\nSanitized:\n%s",
					tt.shouldContain, sanitizedLogs)
			}

			// Verifica se NÃO contém o valor sensível original
			if tt.shouldNotContain != "" && strings.Contains(sanitizedLogs, tt.shouldNotContain) {
				t.Errorf("Expected sanitized logs to NOT contain %q, but it does.\nSanitized:\n%s",
					tt.shouldNotContain, sanitizedLogs)
			}
		})
	}

	// Debug: Imprime logs sanitizados (útil para verificação manual)
	t.Logf("=== LOGS ORIGINAIS ===\n%s", rawLogs)
	t.Logf("\n=== LOGS SANITIZADOS ===\n%s", sanitizedLogs)
}

// TestSanitizeCrashLoopBackOffScenario testa cenário real de CrashLoopBackOff
func TestSanitizeCrashLoopBackOffScenario(t *testing.T) {
	sanitizer := New()

	// Simula logs de um pod em CrashLoopBackOff (caso comum no AI Diagnostics)
	crashLogs := `2026-01-06 10:15:00 Starting server on port 8080
2026-01-06 10:15:01 Loading config from environment
2026-01-06 10:15:02 DB_HOST=10.0.0.100
2026-01-06 10:15:03 DB_CONNECTION_STRING=mongodb://app_user:MyP@ssw0rd!2024@mdbh-prod-cluster-1.dc.example.com:27017/app_db?ssl=true
2026-01-06 10:15:04 REDIS_URL=redis://cache:secret123@cache-master.internal.svc:6379
2026-01-06 10:15:05 API_KEY=sk-live-AbCdEfGhIjKlMnOpQrStUvWxYz0123456789
2026-01-06 10:15:06 Attempting to connect to database...
2026-01-06 10:15:07 ERROR: Connection timeout to 8.8.8.8:443
2026-01-06 10:15:08 ERROR: Failed to resolve external-api.example.com (213.123.45.67)
2026-01-06 10:15:09 FATAL: Cannot start application, exiting with code 1
2026-01-06 10:15:10 Loading TLS certificate: /etc/ssl/certs/production.cert
2026-01-06 10:15:11 Loading TLS private key: /etc/ssl/private/production.key
2026-01-06 10:15:12 JWT Token: eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U`

	sanitized := sanitizer.SanitizeLogs(crashLogs)

	// Verificações
	tests := []struct {
		name             string
		shouldNotContain string
		description      string
	}{
		{
			name:             "Senha MongoDB não exposta",
			shouldNotContain: "MyP@ssw0rd!2024",
			description:      "Senha deve ser mascarada como MyP***024",
		},
		{
			name:             "Senha Redis não exposta",
			shouldNotContain: "secret123",
			description:      "Senha deve ser mascarada como sec***123",
		},
		{
			name:             "API Key não exposta completa",
			shouldNotContain: "sk-live-AbCdEfGhIjKlMnOpQrStUvWxYz0123456789",
			description:      "API key deve ser mascarada (base64 pattern)",
		},
		{
			name:             "IP externo 8.8.8.8 mascarado",
			shouldNotContain: "8.8.8.8",
			description:      "IP externo deve ser mascarado como 8.***.***.8",
		},
		{
			name:             "IP externo 213.123.45.67 mascarado",
			shouldNotContain: "213.123.45.67",
			description:      "IP externo deve ser mascarado como 213.***.***.67",
		},
		{
			name:             "Certificado .cert não exposto",
			shouldNotContain: "production.cert",
			description:      "Certificado deve ser mascarado como [cert:production]",
		},
		{
			name:             "Chave privada .key não exposta",
			shouldNotContain: "production.key",
			description:      "Chave deve ser mascarada como [key:production]",
		},
		{
			name:             "JWT token não exposto completo",
			shouldNotContain: "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U",
			description:      "JWT deve ser mascarado (base64 pattern)",
		},
	}

	// IPs internos NÃO devem ser mascarados
	if !strings.Contains(sanitized, "10.0.0.100") {
		t.Error("IP interno 10.0.0.100 foi mascarado incorretamente (deveria ser preservado)")
	}

	// Verifica que dados sensíveis NÃO estão presentes
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if strings.Contains(sanitized, tt.shouldNotContain) {
				t.Errorf("%s\nFound sensitive data: %q\nSanitized logs:\n%s",
					tt.description, tt.shouldNotContain, sanitized)
			}
		})
	}

	// Debug
	t.Logf("=== CRASH LOGS ORIGINAIS ===\n%s", crashLogs)
	t.Logf("\n=== CRASH LOGS SANITIZADOS ===\n%s", sanitized)
}

// TestSanitizeMultilineConnectionString testa connection strings em múltiplas linhas
func TestSanitizeMultilineConnectionString(t *testing.T) {
	sanitizer := New()

	logs := `Initializing database connections:
  Primary: mongodb://primary_user:senha_super_secreta_123@mdbp-prod-1.dc.acme.com:27017/prod
  Secondary: postgresql://reader:P@ssw0rd!@db-replica.internal.net:5432/analytics
  Cache: redis://cache_user:cache_pass_456@redis-cluster.svc:6379/0`

	sanitized := sanitizer.SanitizeLogs(logs)

	// Verifica que NENHUMA senha está exposta
	if strings.Contains(sanitized, "senha_super_secreta_123") {
		t.Error("MongoDB password not sanitized")
	}
	if strings.Contains(sanitized, "P@ssw0rd!") {
		t.Error("PostgreSQL password not sanitized")
	}
	if strings.Contains(sanitized, "cache_pass_456") {
		t.Error("Redis password not sanitized")
	}

	// Verifica que senhas foram mascaradas corretamente (3+3+***)
	if !strings.Contains(sanitized, "sen***123") {
		t.Error("MongoDB password not masked correctly (expected sen***123)")
	}
	if !strings.Contains(sanitized, "P@s***rd!") {
		t.Error("PostgreSQL password not masked correctly (expected P@s***rd!)")
	}
	if !strings.Contains(sanitized, "cac***456") {
		t.Error("Redis password not masked correctly (expected cac***456)")
	}

	t.Logf("=== MULTILINE ORIGINAL ===\n%s", logs)
	t.Logf("\n=== MULTILINE SANITIZED ===\n%s", sanitized)
}
