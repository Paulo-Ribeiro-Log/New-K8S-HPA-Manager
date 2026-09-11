package browser

import (
	"testing"
	"time"

	"github.com/rs/zerolog"
)

// TestManager_IdleReap_ClosesRealBrowser valida o mecanismo de fechamento por ociosidade contra
// um Chromium REAL (não um mock) — a origem do problema que motivou esta mudança era memória/CPU
// de um processo de SO de verdade, então só um teste que lança um processo de verdade confirma a
// correção. idleTimeout/idleCheckInterval são reduzidos pra segundos só durante este teste.
func TestManager_IdleReap_ClosesRealBrowser(t *testing.T) {
	if testing.Short() {
		t.Skip("lança um Chromium real — pulado em -short")
	}

	origTimeout, origInterval := idleTimeout, idleCheckInterval
	idleTimeout = 2 * time.Second
	idleCheckInterval = 300 * time.Millisecond
	defer func() {
		idleTimeout, idleCheckInterval = origTimeout, origInterval
	}()

	sessionDir := t.TempDir()
	logger := zerolog.Nop()
	// Mesmas flags de estabilidade já usadas em produção (internal/servicenow/rod_extractor.go's
	// rodLaunchFlags) — sem "no-sandbox", o Chromium se recusa a iniciar em ambientes sem sandbox
	// utilizável do kernel (confirmado ao vivo: CI do GitHub Actions falha com "FATAL:
	// zygote_host_impl_linux.cc(126)] No usable sandbox!" sem essa flag; WSL2 local não expôs o
	// problema porque o sandbox funciona normalmente lá).
	opts := LaunchOptions{
		SessionDir: sessionDir,
		Headless:   true,
		Flags: map[string]string{
			"no-sandbox":             "",
			"disable-setuid-sandbox": "",
			"disable-dev-shm-usage":  "",
			"disable-gpu":            "",
		},
	}

	var m Manager
	b, err := m.Get(opts, nil, nil, &logger)
	if err != nil {
		t.Fatalf("Get() falhou: %v", err)
	}
	if b == nil {
		t.Fatal("browser retornado é nil")
	}

	// Enquanto ainda não passou o idleTimeout, Get() deve reaproveitar o MESMO processo.
	b2, err := m.Get(opts, nil, nil, &logger)
	if err != nil {
		t.Fatalf("2ª Get() falhou: %v", err)
	}
	if b2 != b {
		t.Fatal("esperava reaproveitar o mesmo *rod.Browser antes do idle timeout vencer")
	}

	// Espera passar da janela de ociosidade + margem para pelo menos um ciclo do reaper rodar.
	time.Sleep(idleTimeout + idleCheckInterval*3)

	m.mu.Lock()
	closedByReaper := m.browser == nil
	m.mu.Unlock()
	if !closedByReaper {
		t.Fatal("esperava que idleReapLoop tivesse fechado o browser sozinho após idleTimeout")
	}

	// Uma chamada nova depois do reap deve relançar um processo novo, não reaproveitar o antigo
	// (que já foi fechado/matado pelo reaper).
	b3, err := m.Get(opts, nil, nil, &logger)
	if err != nil {
		t.Fatalf("Get() pós-reap falhou: %v", err)
	}
	if b3 == b {
		t.Fatal("esperava um *rod.Browser novo depois do reap, não a mesma referência de antes")
	}

	// Fecha explicitamente ANTES do fim do teste — t.TempDir() tenta remover sessionDir logo em
	// seguida, e um Chrome ainda vivo com arquivos abertos ali dentro faz esse cleanup falhar
	// ("directory not empty"). Ordem entre defer e t.Cleanup() não é garantida, por isso fechar
	// aqui em vez de via defer. browser.Close() envia o comando via CDP mas o processo do SO leva
	// um instante a mais pra soltar de fato os arquivos do perfil (SingletonLock etc.) — uma
	// pequena espera evita corrida com o RemoveAll do t.TempDir().
	m.Close()
	time.Sleep(500 * time.Millisecond)
}
