package handlers

import (
	"testing"
	"time"
)

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		duration time.Duration
		expected string
	}{
		{5 * time.Minute, "05 minutos"},
		{1 * time.Minute, "1 minuto"},
		{65 * time.Minute, "1 hora, 05 minutos"},
		{2 * time.Hour, "02 horas, 00 minutos"},
		{25 * time.Hour, "1 dia, 1 hora, 00 minutos"},
		{50 * time.Hour, "02 dias, 02 horas, 00 minutos"},
	}

	for _, tt := range tests {
		result := formatDuration(tt.duration)
		if result != tt.expected {
			t.Errorf("formatDuration(%v): esperado '%s', obteve '%s'", tt.duration, tt.expected, result)
		}
	}
}

func TestFormatClusterStatus(t *testing.T) {
	tests := []struct {
		accessible int
		total      int
		expected   string
	}{
		{5, 5, "5 clusters acessíveis"},
		{3, 5, "3/5 clusters acessíveis"},
		{0, 5, "0/5 clusters acessíveis"},
		{1, 1, "1 clusters acessíveis"},
	}

	for _, tt := range tests {
		result := formatClusterStatus(tt.accessible, tt.total)
		if result != tt.expected {
			t.Errorf("formatClusterStatus(%d, %d): esperado '%s', obteve '%s'", tt.accessible, tt.total, tt.expected, result)
		}
	}
}

func TestFormatMemoryUsage(t *testing.T) {
	result := formatMemoryUsage(256, 512)
	expected := "256MB alocados / 512MB sistema"

	if result != expected {
		t.Errorf("formatMemoryUsage(256, 512): esperado '%s', obteve '%s'", expected, result)
	}
}

func TestComponentHealthStatus(t *testing.T) {
	// Testar que struct ComponentHealth pode ser criado corretamente
	comp := ComponentHealth{
		Name:    "test",
		Status:  "healthy",
		Message: "Tudo ok",
		Latency: 50,
	}

	if comp.Status != "healthy" {
		t.Error("Status deveria ser 'healthy'")
	}
	if comp.Latency != 50 {
		t.Error("Latency deveria ser 50")
	}
}

func TestSystemHealthResponse(t *testing.T) {
	response := SystemHealthResponse{
		Status:     "healthy",
		Version:    "v1.3.16",
		Uptime:     "1 hora, 30 minutos",
		UptimeSecs: 5400,
		Components: []ComponentHealth{
			{Name: "kubernetes", Status: "healthy"},
			{Name: "storage", Status: "healthy"},
		},
	}

	if response.Status != "healthy" {
		t.Error("Status deveria ser 'healthy'")
	}
	if len(response.Components) != 2 {
		t.Error("Deveria ter 2 componentes")
	}
}
