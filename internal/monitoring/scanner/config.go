package scanner

import (
	"fmt"
	"time"
)

// ScanMode define os modos de scan disponíveis
type ScanMode int

const (
	ScanModeFull       ScanMode = iota // Todos os clusters de um ambiente (PRD ou HLG)
	ScanModeIndividual                 // Seleção customizada (clusters, namespaces, deployments)
	ScanModeStressTest                 // Múltiplos alvos para teste de carga
)

func (s ScanMode) String() string {
	switch s {
	case ScanModeFull:
		return "Full"
	case ScanModeIndividual:
		return "Individual"
	case ScanModeStressTest:
		return "Stress Test"
	default:
		return "Unknown"
	}
}

// Environment define o ambiente a ser escaneado
type Environment string

const (
	EnvironmentPRD Environment = "PRD"
	EnvironmentHLG Environment = "HLG"
)

// ScanConfig configuração de um scan
type ScanConfig struct {
	// Modo de scan
	Mode ScanMode

	// Ambiente (para modo Full)
	Environment Environment

	// Targets customizados (para modo Individual e StressTest)
	Targets []ScanTarget

	// Configurações de execução
	Interval      time.Duration // Intervalo entre scans (default: 5min)
	Duration      time.Duration // Duração total do teste (0 = infinito, max: 3h)
	AutoStart     bool          // Iniciar automaticamente após configuração
	StartTime     time.Time     // Hora de início (0 = agora)
	EndTime       time.Time     // Hora de término (calculado a partir de Duration)
	MaxIterations int           // Número máximo de iterações (0 = infinito)

	// Metadados
	Name        string    // Nome descritivo do scan
	Description string    // Descrição
	CreatedAt   time.Time // Quando foi criado
}

// ScanTarget define um alvo específico de scan
type ScanTarget struct {
	Cluster     string   // Nome do cluster (obrigatório)
	Namespaces  []string // Namespaces específicos (vazio = todos)
	Deployments []string // Deployments específicos (vazio = todos)
	HPAs        []string // HPAs específicos (vazio = todos)
}

// DefaultScanConfig retorna configuração padrão
func DefaultScanConfig() *ScanConfig {
	return &ScanConfig{
		Mode:        ScanModeFull,
		Environment: EnvironmentPRD,
		Interval:    5 * time.Minute,
		Duration:    0, // Infinito
		AutoStart:   false,
		CreatedAt:   time.Now(),
	}
}

// Validate valida a configuração
func (c *ScanConfig) Validate() error {
	// Valida intervalo
	if c.Interval < 1*time.Minute {
		return fmt.Errorf("intervalo mínimo é 1 minuto, recebido: %v", c.Interval)
	}
	if c.Interval > 60*time.Minute {
		return fmt.Errorf("intervalo máximo é 60 minutos, recebido: %v", c.Interval)
	}

	// Valida duração
	if c.Duration > 3*time.Hour {
		return fmt.Errorf("duração máxima é 3 horas, recebido: %v", c.Duration)
	}

	// Valida modo
	switch c.Mode {
	case ScanModeFull:
		if c.Environment != EnvironmentPRD && c.Environment != EnvironmentHLG {
			return fmt.Errorf("ambiente inválido para modo Full: %s", c.Environment)
		}

	case ScanModeIndividual, ScanModeStressTest:
		if len(c.Targets) == 0 {
			return fmt.Errorf("modo %s requer pelo menos 1 target", c.Mode)
		}
		for i, target := range c.Targets {
			if target.Cluster == "" {
				return fmt.Errorf("target %d: cluster é obrigatório", i)
			}
		}
	}

	return nil
}

// CalculateEndTime calcula hora de término baseado em Duration
func (c *ScanConfig) CalculateEndTime() {
	if c.Duration > 0 {
		if c.StartTime.IsZero() {
			c.StartTime = time.Now()
		}
		c.EndTime = c.StartTime.Add(c.Duration)
	}
}

// EstimateScans estima quantos scans serão executados
func (c *ScanConfig) EstimateScans() int {
	if c.Duration == 0 && c.MaxIterations == 0 {
		return -1 // Infinito
	}

	if c.MaxIterations > 0 {
		return c.MaxIterations
	}

	if c.Duration > 0 {
		return int(c.Duration / c.Interval)
	}

	return -1
}

// Summary retorna resumo da configuração
func (c *ScanConfig) Summary() string {
	var summary string

	summary += fmt.Sprintf("Modo: %s\n", c.Mode)

	switch c.Mode {
	case ScanModeFull:
		summary += fmt.Sprintf("Ambiente: %s\n", c.Environment)

	case ScanModeIndividual:
		summary += fmt.Sprintf("Targets: %d cluster(s) selecionado(s)\n", len(c.Targets))
		for i, target := range c.Targets {
			summary += fmt.Sprintf("  %d. %s", i+1, target.Cluster)
			if len(target.Namespaces) > 0 {
				summary += fmt.Sprintf(" (NS: %v)", target.Namespaces)
			}
			if len(target.Deployments) > 0 {
				summary += fmt.Sprintf(" (Deploy: %v)", target.Deployments)
			}
			summary += "\n"
		}

	case ScanModeStressTest:
		totalTargets := 0
		for _, target := range c.Targets {
			if len(target.Deployments) > 0 {
				totalTargets += len(target.Deployments)
			} else if len(target.Namespaces) > 0 {
				totalTargets += len(target.Namespaces)
			} else {
				totalTargets++
			}
		}
		summary += fmt.Sprintf("Targets: %d alvo(s) simultâneo(s)\n", totalTargets)
	}

	summary += fmt.Sprintf("Intervalo: %v\n", c.Interval)

	if c.Duration > 0 {
		summary += fmt.Sprintf("Duração: %v (%d scans estimados)\n", c.Duration, c.EstimateScans())
		if !c.EndTime.IsZero() {
			summary += fmt.Sprintf("Término: %s\n", c.EndTime.Format("15:04:05"))
		}
	} else {
		summary += "Duração: Infinito (Ctrl+C para parar)\n"
	}

	return summary
}

// ToTarget converte string "cluster/namespace/deployment" para ScanTarget
func ToTarget(s string) (ScanTarget, error) {
	// Parse formato: cluster[/namespace[/deployment]]
	// Exemplos:
	//   "akspriv-api-prd-admin"
	//   "akspriv-api-prd-admin/default"
	//   "akspriv-api-prd-admin/default/nginx"

	target := ScanTarget{}

	// Parse simples por enquanto
	// TODO: Implementar parsing robusto
	target.Cluster = s

	return target, nil
}
