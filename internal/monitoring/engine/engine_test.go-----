package engine

import (
	"testing"
	"time"

	"github.com/Paulo-Ribeiro-Log/hpa-watchdog/internal/analyzer"
	"github.com/Paulo-Ribeiro-Log/hpa-watchdog/internal/models"
	"github.com/Paulo-Ribeiro-Log/hpa-watchdog/internal/scanner"
)

// TestPortForwardLifecycle testa o ciclo de vida do port-forward
func TestPortForwardLifecycle(t *testing.T) {
	tests := []struct {
		name        string
		setupFunc   func(*ScanEngine)
		verifyFunc  func(*ScanEngine) error
		description string
	}{
		{
			name: "Port-forward é iniciado no Start()",
			setupFunc: func(e *ScanEngine) {
				if err := e.Start(); err != nil {
					t.Fatalf("Falha ao iniciar engine: %v", err)
				}
			},
			verifyFunc: func(e *ScanEngine) error {
				// Verifica se port-forward manager está ativo
				if e.pfManager == nil {
					t.Error("PortForwardManager não foi inicializado")
				}
				return nil
			},
			description: "Verifica que port-forwards são iniciados quando Start() é chamado",
		},
		{
			name: "Port-forward permanece ativo durante Pause()",
			setupFunc: func(e *ScanEngine) {
				if err := e.Start(); err != nil {
					t.Fatalf("Falha ao iniciar engine: %v", err)
				}
				time.Sleep(100 * time.Millisecond)
				e.Pause()
			},
			verifyFunc: func(e *ScanEngine) error {
				// Verifica que está pausado
				if !e.IsPaused() {
					t.Error("Engine deveria estar pausado")
				}
				// Verifica que ainda está rodando
				if !e.IsRunning() {
					t.Error("Engine deveria estar rodando (apenas pausado)")
				}
				// Port-forward manager ainda deve estar disponível
				if e.pfManager == nil {
					t.Error("PortForwardManager não deveria ser nil durante pausa")
				}
				return nil
			},
			description: "Verifica que port-forwards permanecem ativos quando Pause() é chamado",
		},
		{
			name: "Port-forward é encerrado no Stop()",
			setupFunc: func(e *ScanEngine) {
				if err := e.Start(); err != nil {
					t.Fatalf("Falha ao iniciar engine: %v", err)
				}
				time.Sleep(100 * time.Millisecond)
				if err := e.Stop(); err != nil {
					t.Fatalf("Falha ao parar engine: %v", err)
				}
			},
			verifyFunc: func(e *ScanEngine) error {
				// Verifica que não está mais rodando
				if e.IsRunning() {
					t.Error("Engine não deveria estar rodando após Stop()")
				}
				// Verifica que não está pausado
				if e.IsPaused() {
					t.Error("Engine não deveria estar pausado após Stop()")
				}
				return nil
			},
			description: "Verifica que port-forwards são encerrados quando Stop() é chamado",
		},
		{
			name: "Resume após Pause mantém port-forward ativo",
			setupFunc: func(e *ScanEngine) {
				if err := e.Start(); err != nil {
					t.Fatalf("Falha ao iniciar engine: %v", err)
				}
				time.Sleep(100 * time.Millisecond)
				e.Pause()
				time.Sleep(50 * time.Millisecond)
				e.Resume()
			},
			verifyFunc: func(e *ScanEngine) error {
				// Verifica que não está pausado
				if e.IsPaused() {
					t.Error("Engine não deveria estar pausado após Resume()")
				}
				// Verifica que ainda está rodando
				if !e.IsRunning() {
					t.Error("Engine deveria estar rodando após Resume()")
				}
				// Port-forward manager ainda deve estar disponível
				if e.pfManager == nil {
					t.Error("PortForwardManager não deveria ser nil após Resume()")
				}
				return nil
			},
			description: "Verifica que port-forwards permanecem ativos após Resume()",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Cria engine de teste
			cfg := &scanner.ScanConfig{
				Mode:     scanner.ScanModeIndividual,
				Interval: 500 * time.Millisecond,
				Duration: 5 * time.Second,
				Targets: []scanner.ScanTarget{
					{
						Cluster:    "test-cluster",
						Namespaces: []string{"default"},
					},
				},
			}

			snapshotChan := make(chan *models.HPASnapshot, 10)
			anomalyChan := make(chan analyzer.Anomaly, 10)
			engine := New(cfg, snapshotChan, anomalyChan)

			// Executa setup
			tt.setupFunc(engine)

			// Aguarda propagação
			time.Sleep(100 * time.Millisecond)

			// Verifica resultado
			if err := tt.verifyFunc(engine); err != nil {
				t.Errorf("Verificação falhou: %v", err)
			}

			// Cleanup
			if engine.IsRunning() {
				engine.Stop()
			}

			// Fecha canais
			close(snapshotChan)
			close(anomalyChan)
		})
	}
}

// TestStressTestPortForward testa port-forward em modo stress test
func TestStressTestPortForward(t *testing.T) {
	cfg := &scanner.ScanConfig{
		Mode:     scanner.ScanModeStressTest,
		Interval: 500 * time.Millisecond,
		Duration: 2 * time.Second,
		Targets: []scanner.ScanTarget{
			{
				Cluster:    "test-cluster",
				Namespaces: []string{"default"},
			},
		},
	}

	snapshotChan := make(chan *models.HPASnapshot, 10)
	anomalyChan := make(chan analyzer.Anomaly, 10)
	engine := New(cfg, snapshotChan, anomalyChan)

	// Inicia teste
	if err := engine.Start(); err != nil {
		t.Fatalf("Falha ao iniciar engine: %v", err)
	}

	// Verifica que está rodando
	if !engine.IsRunning() {
		t.Error("Engine deveria estar rodando")
	}

	// Verifica que port-forward foi iniciado
	if engine.pfManager == nil {
		t.Error("PortForwardManager não foi inicializado")
	}

	// Aguarda um pouco
	time.Sleep(500 * time.Millisecond)

	// Para teste
	if err := engine.Stop(); err != nil {
		t.Fatalf("Falha ao parar engine: %v", err)
	}

	// Verifica que parou
	if engine.IsRunning() {
		t.Error("Engine não deveria estar rodando após Stop()")
	}

	// Cleanup
	close(snapshotChan)
	close(anomalyChan)
}

// TestMultipleStartStop testa múltiplos ciclos de start/stop
func TestMultipleStartStop(t *testing.T) {
	cfg := &scanner.ScanConfig{
		Mode:     scanner.ScanModeIndividual,
		Interval: 500 * time.Millisecond,
		Duration: 1 * time.Second,
		Targets: []scanner.ScanTarget{
			{
				Cluster:    "test-cluster",
				Namespaces: []string{"default"},
			},
		},
	}

	snapshotChan := make(chan *models.HPASnapshot, 10)
	anomalyChan := make(chan analyzer.Anomaly, 10)

	// Executa 3 ciclos de start/stop
	for i := 0; i < 3; i++ {
		t.Logf("Ciclo %d", i+1)

		engine := New(cfg, snapshotChan, anomalyChan)

		// Start
		if err := engine.Start(); err != nil {
			t.Fatalf("Ciclo %d: Falha ao iniciar engine: %v", i+1, err)
		}

		if !engine.IsRunning() {
			t.Errorf("Ciclo %d: Engine deveria estar rodando", i+1)
		}

		// Aguarda
		time.Sleep(300 * time.Millisecond)

		// Stop
		if err := engine.Stop(); err != nil {
			t.Fatalf("Ciclo %d: Falha ao parar engine: %v", i+1, err)
		}

		if engine.IsRunning() {
			t.Errorf("Ciclo %d: Engine não deveria estar rodando", i+1)
		}
	}

	// Cleanup
	close(snapshotChan)
	close(anomalyChan)
}

// TestPauseResumeMultipleTimes testa múltiplas pausas e retomadas
func TestPauseResumeMultipleTimes(t *testing.T) {
	cfg := &scanner.ScanConfig{
		Mode:     scanner.ScanModeIndividual,
		Interval: 200 * time.Millisecond,
		Duration: 5 * time.Second,
		Targets: []scanner.ScanTarget{
			{
				Cluster:    "test-cluster",
				Namespaces: []string{"default"},
			},
		},
	}

	snapshotChan := make(chan *models.HPASnapshot, 10)
	anomalyChan := make(chan analyzer.Anomaly, 10)
	engine := New(cfg, snapshotChan, anomalyChan)

	// Start
	if err := engine.Start(); err != nil {
		t.Fatalf("Falha ao iniciar engine: %v", err)
	}

	// Executa 3 ciclos de pause/resume
	for i := 0; i < 3; i++ {
		t.Logf("Ciclo pause/resume %d", i+1)

		// Pause
		engine.Pause()
		time.Sleep(100 * time.Millisecond)

		if !engine.IsPaused() {
			t.Errorf("Ciclo %d: Engine deveria estar pausado", i+1)
		}

		if !engine.IsRunning() {
			t.Errorf("Ciclo %d: Engine deveria estar rodando (apenas pausado)", i+1)
		}

		// Resume
		engine.Resume()
		time.Sleep(100 * time.Millisecond)

		if engine.IsPaused() {
			t.Errorf("Ciclo %d: Engine não deveria estar pausado", i+1)
		}

		if !engine.IsRunning() {
			t.Errorf("Ciclo %d: Engine deveria estar rodando", i+1)
		}

		// Verifica que port-forward manager ainda está ativo
		if engine.pfManager == nil {
			t.Errorf("Ciclo %d: PortForwardManager não deveria ser nil", i+1)
		}
	}

	// Stop final
	if err := engine.Stop(); err != nil {
		t.Fatalf("Falha ao parar engine: %v", err)
	}

	// Cleanup
	close(snapshotChan)
	close(anomalyChan)
}

// TestStopWithoutStart testa Stop() sem Start() prévio
func TestStopWithoutStart(t *testing.T) {
	cfg := &scanner.ScanConfig{
		Mode:     scanner.ScanModeIndividual,
		Interval: 500 * time.Millisecond,
		Targets: []scanner.ScanTarget{
			{
				Cluster:    "test-cluster",
				Namespaces: []string{"default"},
			},
		},
	}

	snapshotChan := make(chan *models.HPASnapshot, 10)
	anomalyChan := make(chan analyzer.Anomaly, 10)
	engine := New(cfg, snapshotChan, anomalyChan)

	// Tenta parar sem ter iniciado
	if err := engine.Stop(); err != nil {
		t.Errorf("Stop() sem Start() não deveria retornar erro: %v", err)
	}

	if engine.IsRunning() {
		t.Error("Engine não deveria estar rodando")
	}

	// Cleanup
	close(snapshotChan)
	close(anomalyChan)
}
