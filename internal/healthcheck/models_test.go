package healthcheck

import (
	"testing"
)

// TestGetTimeoutDeployments testa a função GetTimeoutDeployments com cenários de fallback
func TestGetTimeoutDeployments(t *testing.T) {
	tests := []struct {
		name               string
		timeoutDeployments int
		timeoutGeneral     int
		expected           int
		description        string
	}{
		{
			name:               "usa timeout específico quando definido",
			timeoutDeployments: 90,
			timeoutGeneral:     30,
			expected:           90,
			description:        "Timeout específico tem prioridade sobre geral",
		},
		{
			name:               "fallback para timeout geral quando específico é zero",
			timeoutDeployments: 0,
			timeoutGeneral:     45,
			expected:           45,
			description:        "Backward compatibility: timeout único funciona",
		},
		{
			name:               "usa padrão quando ambos são zero",
			timeoutDeployments: 0,
			timeoutGeneral:     0,
			expected:           DefaultTimeoutDeployments, // 60s
			description:        "Constante padrão é usada como último fallback",
		},
		{
			name:               "timeout específico muito alto",
			timeoutDeployments: 300,
			timeoutGeneral:     30,
			expected:           300,
			description:        "Aceita valores altos para clusters grandes",
		},
		{
			name:               "timeout específico mínimo",
			timeoutDeployments: 1,
			timeoutGeneral:     30,
			expected:           1,
			description:        "Aceita valores baixos (validação é na UI)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &HealthCheckRequest{
				TimeoutDeployments: tt.timeoutDeployments,
				Timeout:            tt.timeoutGeneral,
			}

			got := req.GetTimeoutDeployments()
			if got != tt.expected {
				t.Errorf("GetTimeoutDeployments() = %d, want %d (%s)", got, tt.expected, tt.description)
			}
		})
	}
}

// TestGetTimeoutServices testa a função GetTimeoutServices com cenários de fallback
func TestGetTimeoutServices(t *testing.T) {
	tests := []struct {
		name            string
		timeoutServices int
		timeoutGeneral  int
		expected        int
		description     string
	}{
		{
			name:            "usa timeout específico quando definido",
			timeoutServices: 60,
			timeoutGeneral:  30,
			expected:        60,
			description:     "Timeout específico tem prioridade sobre geral",
		},
		{
			name:            "fallback para timeout geral quando específico é zero",
			timeoutServices: 0,
			timeoutGeneral:  50,
			expected:        50,
			description:     "Backward compatibility: timeout único funciona",
		},
		{
			name:            "usa padrão quando ambos são zero",
			timeoutServices: 0,
			timeoutGeneral:  0,
			expected:        DefaultTimeoutServices, // 45s
			description:     "Constante padrão é usada como último fallback",
		},
		{
			name:            "timeout para testes de conectividade longos",
			timeoutServices: 120,
			timeoutGeneral:  30,
			expected:        120,
			description:     "Conexões lentas podem precisar de mais tempo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &HealthCheckRequest{
				TimeoutServices: tt.timeoutServices,
				Timeout:         tt.timeoutGeneral,
			}

			got := req.GetTimeoutServices()
			if got != tt.expected {
				t.Errorf("GetTimeoutServices() = %d, want %d (%s)", got, tt.expected, tt.description)
			}
		})
	}
}

// TestGetTimeoutConfigs testa a função GetTimeoutConfigs com cenários de fallback
func TestGetTimeoutConfigs(t *testing.T) {
	tests := []struct {
		name           string
		timeoutConfigs int
		timeoutGeneral int
		expected       int
		description    string
	}{
		{
			name:           "usa timeout específico quando definido",
			timeoutConfigs: 20,
			timeoutGeneral: 30,
			expected:       20,
			description:    "Timeout específico tem prioridade sobre geral",
		},
		{
			name:           "fallback para timeout geral quando específico é zero",
			timeoutConfigs: 0,
			timeoutGeneral: 40,
			expected:       40,
			description:    "Backward compatibility: timeout único funciona",
		},
		{
			name:           "usa padrão quando ambos são zero",
			timeoutConfigs: 0,
			timeoutGeneral: 0,
			expected:       DefaultTimeoutConfigs, // 30s
			description:    "Constante padrão é usada como último fallback",
		},
		{
			name:           "timeout curto para validação rápida",
			timeoutConfigs: 10,
			timeoutGeneral: 60,
			expected:       10,
			description:    "ConfigMaps/Secrets são rápidos de validar",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &HealthCheckRequest{
				TimeoutConfigs: tt.timeoutConfigs,
				Timeout:        tt.timeoutGeneral,
			}

			got := req.GetTimeoutConfigs()
			if got != tt.expected {
				t.Errorf("GetTimeoutConfigs() = %d, want %d (%s)", got, tt.expected, tt.description)
			}
		})
	}
}

// TestTimeoutDefaults verifica que as constantes padrão estão corretas
func TestTimeoutDefaults(t *testing.T) {
	// Garantir que os valores padrão documentados estão corretos
	if DefaultTimeoutGeneral != 30 {
		t.Errorf("DefaultTimeoutGeneral = %d, want 30", DefaultTimeoutGeneral)
	}
	if DefaultTimeoutDeployments != 60 {
		t.Errorf("DefaultTimeoutDeployments = %d, want 60", DefaultTimeoutDeployments)
	}
	if DefaultTimeoutServices != 45 {
		t.Errorf("DefaultTimeoutServices = %d, want 45", DefaultTimeoutServices)
	}
	if DefaultTimeoutConfigs != 30 {
		t.Errorf("DefaultTimeoutConfigs = %d, want 30", DefaultTimeoutConfigs)
	}
}

// TestBackwardCompatibility_SingleTimeout testa cenário real de migração
// onde usuário usava apenas timeout único antes dos timeouts específicos
func TestBackwardCompatibility_SingleTimeout(t *testing.T) {
	// Simula request antigo (antes de timeouts específicos)
	// Apenas Timeout definido, específicos são zero (omitidos no JSON)
	oldStyleRequest := &HealthCheckRequest{
		Timeout:            60, // Usuário configurou 60s para tudo
		TimeoutDeployments: 0,  // Não existia antes
		TimeoutServices:    0,  // Não existia antes
		TimeoutConfigs:     0,  // Não existia antes
	}

	// Todos devem usar o timeout geral como fallback
	if got := oldStyleRequest.GetTimeoutDeployments(); got != 60 {
		t.Errorf("Backward compatibility falhou para Deployments: got %d, want 60", got)
	}
	if got := oldStyleRequest.GetTimeoutServices(); got != 60 {
		t.Errorf("Backward compatibility falhou para Services: got %d, want 60", got)
	}
	if got := oldStyleRequest.GetTimeoutConfigs(); got != 60 {
		t.Errorf("Backward compatibility falhou para Configs: got %d, want 60", got)
	}
}

// TestMixedTimeouts testa cenário onde alguns timeouts específicos são definidos
func TestMixedTimeouts(t *testing.T) {
	req := &HealthCheckRequest{
		Timeout:            30,  // Timeout geral
		TimeoutDeployments: 120, // Específico para deployments (cluster grande)
		TimeoutServices:    0,   // Usar geral
		TimeoutConfigs:     15,  // Específico para configs (rápido)
	}

	if got := req.GetTimeoutDeployments(); got != 120 {
		t.Errorf("GetTimeoutDeployments() = %d, want 120 (específico)", got)
	}
	if got := req.GetTimeoutServices(); got != 30 {
		t.Errorf("GetTimeoutServices() = %d, want 30 (fallback geral)", got)
	}
	if got := req.GetTimeoutConfigs(); got != 15 {
		t.Errorf("GetTimeoutConfigs() = %d, want 15 (específico)", got)
	}
}

// TestZeroRequest testa request completamente vazio (usa todos os padrões)
func TestZeroRequest(t *testing.T) {
	req := &HealthCheckRequest{}

	if got := req.GetTimeoutDeployments(); got != DefaultTimeoutDeployments {
		t.Errorf("GetTimeoutDeployments() = %d, want %d (padrão)", got, DefaultTimeoutDeployments)
	}
	if got := req.GetTimeoutServices(); got != DefaultTimeoutServices {
		t.Errorf("GetTimeoutServices() = %d, want %d (padrão)", got, DefaultTimeoutServices)
	}
	if got := req.GetTimeoutConfigs(); got != DefaultTimeoutConfigs {
		t.Errorf("GetTimeoutConfigs() = %d, want %d (padrão)", got, DefaultTimeoutConfigs)
	}
	if got := req.GetTimeoutEvents(); got != DefaultTimeoutEvents {
		t.Errorf("GetTimeoutEvents() = %d, want %d (padrão)", got, DefaultTimeoutEvents)
	}
}

// TestGetTimeoutEvents testa a função GetTimeoutEvents com cenários de fallback
func TestGetTimeoutEvents(t *testing.T) {
	tests := []struct {
		name           string
		timeoutEvents  int
		timeoutGeneral int
		expected       int
		description    string
	}{
		{
			name:           "usa timeout específico quando definido",
			timeoutEvents:  45,
			timeoutGeneral: 30,
			expected:       45,
			description:    "Timeout específico tem prioridade sobre geral",
		},
		{
			name:           "fallback para timeout geral quando específico é zero",
			timeoutEvents:  0,
			timeoutGeneral: 60,
			expected:       60,
			description:    "Backward compatibility: timeout único funciona",
		},
		{
			name:           "usa padrão quando ambos são zero",
			timeoutEvents:  0,
			timeoutGeneral: 0,
			expected:       DefaultTimeoutEvents, // 30s
			description:    "Constante padrão é usada como último fallback",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &HealthCheckRequest{
				TimeoutEvents: tt.timeoutEvents,
				Timeout:       tt.timeoutGeneral,
			}

			got := req.GetTimeoutEvents()
			if got != tt.expected {
				t.Errorf("GetTimeoutEvents() = %d, want %d (%s)", got, tt.expected, tt.description)
			}
		})
	}
}
