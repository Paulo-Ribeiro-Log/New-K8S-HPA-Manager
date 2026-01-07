package sanitizer

import (
	"strings"
	"testing"
)

// TestMaskBase64 testa o mascaramento de base64
func TestMaskBase64(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Base64 com = no final",
			input:    "MDFhghthghthghthghthghthghthghtTRk4=",
			expected: "MDF***Rk4=",
		},
		{
			name:     "Base64 sem = no final",
			input:    "MDFhghthghthghthghthghthghthghtTRk4",
			expected: "MDF***Rk4",
		},
		{
			name:     "Base64 curta (não mascara)",
			input:    "ABC123",
			expected: "ABC123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MaskBase64(tt.input)
			if result != tt.expected {
				t.Errorf("MaskBase64(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// TestMaskPassword testa o mascaramento de senhas em connection strings
func TestMaskPassword(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Senha longa",
			input:    "s6Yxbn1I9i98GHIJcJdc",
			expected: "s6Y***Jdc",
		},
		{
			name:     "Senha com @ interno",
			input:    "MyP@ssw0rd",
			expected: "MyP***0rd",
		},
		{
			name:     "Senha média",
			input:    "fhfjfurenfndhdhdhdhdhdhf",
			expected: "fhf***dhf",
		},
		{
			name:     "Senha curta (só ***)",
			input:    "abc",
			expected: "***",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MaskPassword(tt.input)
			if result != tt.expected {
				t.Errorf("MaskPassword(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// TestMaskConnectionString testa o mascaramento de connection strings completas
func TestMaskConnectionString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Connection string sem protocolo",
			input:    "svc_arvore_defeitos:s6Yxbn1I9i98GHIJcJdc@mdbp-logreversa-1.dc.nova",
			expected: "svc_arvore_defeitos:s6Y***Jdc@mdbp-logreversa-1.dc.nova",
		},
		{
			name:     "MongoDB connection string",
			input:    "mongodb://svc_legacy_hlg:fhfjfurenfndhdhdhdhdhdhf@mdbh-legacydata-1.dc.nova:27017/?retryWrites=true",
			expected: "mongodb://svc_legacy_hlg:fhf***dhf@mdbh-legacydata-1.dc.nova:27017/?retryWrites=true",
		},
		{
			name:     "PostgreSQL connection string",
			input:    "postgresql://user:MyP@ssw0rd@db.example.com:5432/mydb",
			expected: "postgresql://user:MyP***0rd@db.example.com:5432/mydb",
		},
		{
			name:     "MySQL connection string",
			input:    "mysql://root:secret123@localhost:3306/db",
			expected: "mysql://root:sec***123@localhost:3306/db",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MaskConnectionString(tt.input)
			if result != tt.expected {
				t.Errorf("MaskConnectionString(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// TestIsInternalIP testa a detecção de IPs internos
func TestIsInternalIP(t *testing.T) {
	tests := []struct {
		name     string
		ip       string
		expected bool
	}{
		// IPs internos (devem retornar true)
		{name: "IP privado 10.x", ip: "10.0.0.1", expected: true},
		{name: "IP privado 10.x (outro)", ip: "10.255.255.255", expected: true},
		{name: "IP privado 172.16.x", ip: "172.16.0.1", expected: true},
		{name: "IP privado 172.31.x", ip: "172.31.255.255", expected: true},
		{name: "IP privado 192.168.x", ip: "192.168.1.1", expected: true},
		{name: "Localhost", ip: "127.0.0.1", expected: true},
		{name: "Link-local", ip: "169.254.1.1", expected: true},

		// IPs externos (devem retornar false)
		{name: "IP público 8.8.8.8", ip: "8.8.8.8", expected: false},
		{name: "IP público 172.168.1.213 (fora do range 172.16-31)", ip: "172.168.1.213", expected: false},
		{name: "IP público 172.32.x", ip: "172.32.0.1", expected: false},
		{name: "IP público 1.1.1.1", ip: "1.1.1.1", expected: false},

		// IPs inválidos (devem retornar false)
		{name: "IP inválido", ip: "999.999.999.999", expected: false},
		{name: "String vazia", ip: "", expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsInternalIP(tt.ip)
			if result != tt.expected {
				t.Errorf("IsInternalIP(%q) = %v, want %v", tt.ip, result, tt.expected)
			}
		})
	}
}

// TestMaskExternalIP testa o mascaramento de IPs externos
func TestMaskExternalIP(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		// IPs externos (devem ser mascarados)
		{
			name:     "IP externo 172.168.1.213",
			input:    "172.168.1.213",
			expected: "172.***.***.213",
		},
		{
			name:     "IP externo 8.8.8.8",
			input:    "8.8.8.8",
			expected: "8.***.***.8",
		},
		{
			name:     "IP externo 1.1.1.1",
			input:    "1.1.1.1",
			expected: "1.***.***.1",
		},

		// IPs internos (NÃO devem ser mascarados)
		{
			name:     "IP interno 10.0.0.1",
			input:    "10.0.0.1",
			expected: "10.0.0.1",
		},
		{
			name:     "IP interno 172.16.0.1",
			input:    "172.16.0.1",
			expected: "172.16.0.1",
		},
		{
			name:     "IP interno 192.168.1.1",
			input:    "192.168.1.1",
			expected: "192.168.1.1",
		},
		{
			name:     "Localhost",
			input:    "127.0.0.1",
			expected: "127.0.0.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MaskExternalIP(tt.input)
			if result != tt.expected {
				t.Errorf("MaskExternalIP(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// TestSanitizeText testa a sanitização completa de um texto
func TestSanitizeText(t *testing.T) {
	sanitizer := New()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name: "Connection string MongoDB",
			input: `Error connecting to mongodb://svc_legacy_hlg:fhfjfurenfndhdhdhdhdhdhf@mdbh-legacydata-1.dc.nova:27017/`,
			expected: `Error connecting to mongodb://svc_legacy_hlg:fhf***dhf@mdbh-legacydata-1.dc.nova:27017/`,
		},
		{
			name: "Base64 longo",
			input: `Token: MDFhghthghthghthghthghthghthghtTRk4=`,
			expected: `Token: MDF***Rk4=`,
		},
		{
			name: "IP externo",
			input: `Connection to 172.168.1.213 failed`,
			expected: `Connection to 172.***.***.213 failed`,
		},
		{
			name: "IP interno (não mascara)",
			input: `Connection to 10.0.0.1 successful`,
			expected: `Connection to 10.0.0.1 successful`,
		},
		{
			name: "Certificado .cert",
			input: `Loading certificado-tls.cert from disk`,
			expected: `Loading [cert:certificado-tls] from disk`,
		},
		{
			name: "Chave privada .key",
			input: `Using private key app-private.key`,
			expected: `Using private key [key:app-private]`,
		},
		{
			name: "Texto com múltiplos dados sensíveis",
			input: `mongodb://user:senha123@10.0.0.1:27017/ and external IP 8.8.8.8 with token MDFhghthghthghthghthghthghthghtTRk4=`,
			expected: `mongodb://user:sen***123@10.0.0.1:27017/ and external IP 8.***.***.8 with token MDF***Rk4=`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizer.SanitizeText(tt.input)
			if result != tt.expected {
				t.Errorf("SanitizeText() mismatch\nInput:    %q\nGot:      %q\nExpected: %q", tt.input, result, tt.expected)
			}
		})
	}
}

// TestSanitizeTextWithResult testa a sanitização com resultado detalhado
func TestSanitizeTextWithResult(t *testing.T) {
	sanitizer := New()

	input := `mongodb://user:senha123@db.host.com:27017/ with IP 8.8.8.8 and token MDFhghthghthghthghthghthghthghtTRk4= and cert app.cert`

	result := sanitizer.SanitizeTextWithResult(input)

	// Verifica se sanitizou corretamente
	if !strings.Contains(result.Sanitized, "sen***123") {
		t.Error("Connection string não foi sanitizada corretamente")
	}

	if !strings.Contains(result.Sanitized, "8.***.***.8") {
		t.Error("IP externo não foi mascarado")
	}

	if !strings.Contains(result.Sanitized, "MDF***Rk4=") {
		t.Error("Base64 não foi mascarada")
	}

	if !strings.Contains(result.Sanitized, "[cert:app]") {
		t.Error("Certificado não foi mascarado")
	}

	// Verifica contadores
	if result.MaskedItems["connection_string"] != 1 {
		t.Errorf("Expected 1 connection_string, got %d", result.MaskedItems["connection_string"])
	}

	if result.MaskedItems["external_ip"] != 1 {
		t.Errorf("Expected 1 external_ip, got %d", result.MaskedItems["external_ip"])
	}

	if result.MaskedItems["base64"] != 1 {
		t.Errorf("Expected 1 base64, got %d", result.MaskedItems["base64"])
	}

	if result.MaskedItems["certificate"] != 1 {
		t.Errorf("Expected 1 certificate, got %d", result.MaskedItems["certificate"])
	}
}
