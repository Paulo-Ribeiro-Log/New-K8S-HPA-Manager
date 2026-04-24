package teams

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/rs/zerolog"
)

const mrViaBotThreadID = "19:eab1be93-5589-4a3f-9f47-d6cfcbc50a0c_61740f97-9be2-4459-b054-5230364585a7@unq.gbl.spaces"

// Extractor extrai aprovações SRE do Mr.ViaBot no Microsoft Teams via automação de browser.
type Extractor struct {
	SessionDir string
	Logger     *zerolog.Logger
}

// NewExtractor cria um Extractor usando o diretório de sessão exclusivo do Teams.
// Diretório separado do ServiceNow (rod-session) pois usa o Chrome do sistema,
// não o Chromium do Rod — perfis incompatíveis corrompem a sessão um do outro.
func NewExtractor(homeDir string, logger *zerolog.Logger) *Extractor {
	sessionDir := filepath.Join(homeDir, ".k8s-hpa-manager", "teams-session")
	os.MkdirAll(sessionDir, 0700) //nolint:errcheck
	return &Extractor{
		SessionDir: sessionDir,
		Logger:     logger,
	}
}

// Extract abre o Teams, navega até o Mr.ViaBot e extrai as aprovações do dia atual.
// Reutiliza a sessão Azure AD existente (compartilhada com ServiceNow).
func (e *Extractor) Extract() (*ExtractionResult, error) {
	// Garante que o diretório existe (Chrome cria a sessão na primeira execução)
	if err := os.MkdirAll(e.SessionDir, 0700); err != nil {
		return nil, fmt.Errorf("erro ao criar diretório de sessão: %v", err)
	}

	tmpDir, err := os.MkdirTemp("", "teams-extract-*")
	if err != nil {
		return nil, fmt.Errorf("erro ao criar diretório temporário: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	e.Logger.Info().Msg("[Teams] Iniciando extração de aprovações do Mr.ViaBot...")

	_, err = RunDiscovery(e.SessionDir, tmpDir, e.Logger, 10*time.Minute)
	if err != nil {
		return nil, fmt.Errorf("erro na descoberta Teams: %v", err)
	}

	domFile := filepath.Join(tmpDir, "viabot-dom-messages.json")
	data, err := os.ReadFile(domFile)
	if err != nil {
		return nil, fmt.Errorf("arquivo DOM não encontrado — conversa não foi carregada: %v", err)
	}

	var domData struct {
		Messages []string `json:"messages"`
	}
	if err := json.Unmarshal(data, &domData); err != nil {
		return nil, fmt.Errorf("erro ao parsear DOM messages: %v", err)
	}

	items := ParseDOMMessages(domData.Messages)
	e.Logger.Info().Int("count", len(items)).Msg("[Teams] Aprovações extraídas")

	return &ExtractionResult{
		Items:       items,
		ExtractedAt: time.Now(),
		Source:      "dom",
	}, nil
}
