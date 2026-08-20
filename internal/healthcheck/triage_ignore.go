package healthcheck

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/rs/zerolog/log"
)

// TriageIgnoreSource identifica a fonte externa a que um TriageIgnoreEntry se aplica.
//
// Diferente de FilterRule (filters.go), que suprime ACHADOS de postura K8s depois de encontrados
// (ConfigMap vazio, Secret de sistema, etc.), um TriageIgnoreEntry suprime um sinal externo ANTES
// — na hora de decidir o escopo de namespaces do Modo Triagem (ver HEALTHCHECK-TRIAGE-MODE-PLAN.md
// seção 2.5). Um alerta/problem/trigger "conhecido e aceito" não deve forçar um namespace inteiro
// pra dentro do escopo triado.
type TriageIgnoreSource string

const (
	TriageIgnoreSourcePrometheusAlert  TriageIgnoreSource = "prometheus_alert"
	TriageIgnoreSourceDynatraceProblem TriageIgnoreSource = "dynatrace_problem"
	TriageIgnoreSourceZabbixTrigger    TriageIgnoreSource = "zabbix_trigger"        // Fase 5 (ainda não implementada)
	TriageIgnoreSourceElasticsearchApp TriageIgnoreSource = "elasticsearch_pattern" // Fase 3 (ainda não implementada)
)

// TriageIgnoreEntry é uma regra de supressão: um sinal externo específico, identificado por nome
// exato (alertname do Prometheus, título ou displayId do problem Dynatrace, nome do trigger
// Zabbix), que nunca deve contribuir namespace nenhum pro escopo do Modo Triagem — mesmo com a
// fonte disponível e o sinal genuinamente ativo.
type TriageIgnoreEntry struct {
	ID        string             `json:"id"`
	Source    TriageIgnoreSource `json:"source"`
	Value     string             `json:"value"` // nome exato do sinal a ignorar
	Reason    string             `json:"reason,omitempty"`
	CreatedAt string             `json:"created_at"`
	CreatedBy string             `json:"created_by"`
}

// TriageIgnoreFile é o formato persistido em disco.
type TriageIgnoreFile struct {
	Version string              `json:"version"`
	Entries []TriageIgnoreEntry `json:"entries"`
}

// TriageIgnoreManager gerencia a lista de supressão de sinal externo do Modo Triagem. Mesmo
// padrão de persistência do FilterManager (filters.go) — arquivo JSON local, mutex, sem banco.
type TriageIgnoreManager struct {
	configPath string
	config     TriageIgnoreFile
	mu         sync.RWMutex
}

// NewTriageIgnoreManager cria o gerenciador, criando um arquivo vazio se ainda não existir.
func NewTriageIgnoreManager(configPath string) (*TriageIgnoreManager, error) {
	m := &TriageIgnoreManager{
		configPath: configPath,
		config: TriageIgnoreFile{
			Version: "1.0",
			Entries: []TriageIgnoreEntry{},
		},
	}

	if err := m.Load(); err != nil {
		if os.IsNotExist(err) {
			if err := m.Save(); err != nil {
				return nil, fmt.Errorf("failed to save default triage ignore config: %w", err)
			}
		} else {
			return nil, fmt.Errorf("failed to load triage ignore config: %w", err)
		}
	}

	log.Info().
		Str("config_path", configPath).
		Int("entries_count", len(m.config.Entries)).
		Msg("Triage ignore manager initialized")

	return m, nil
}

// Load carrega a configuração do arquivo.
func (m *TriageIgnoreManager) Load() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	data, err := os.ReadFile(m.configPath)
	if err != nil {
		return err
	}

	if err := json.Unmarshal(data, &m.config); err != nil {
		return fmt.Errorf("failed to parse triage ignore config: %w", err)
	}

	return nil
}

// Save salva a configuração no arquivo.
func (m *TriageIgnoreManager) Save() error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	dir := filepath.Dir(m.configPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	data, err := json.MarshalIndent(m.config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal triage ignore config: %w", err)
	}

	if err := os.WriteFile(m.configPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write triage ignore config: %w", err)
	}

	log.Debug().
		Str("config_path", m.configPath).
		Int("entries_count", len(m.config.Entries)).
		Msg("Triage ignore config saved")

	return nil
}

// AddEntry adiciona uma nova entrada de supressão. Rejeita duplicatas exatas (mesma Source+Value).
func (m *TriageIgnoreManager) AddEntry(entry TriageIgnoreEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	switch entry.Source {
	case TriageIgnoreSourcePrometheusAlert, TriageIgnoreSourceDynatraceProblem,
		TriageIgnoreSourceZabbixTrigger, TriageIgnoreSourceElasticsearchApp:
		// válido
	default:
		return fmt.Errorf("fonte inválida: %s", entry.Source)
	}
	if entry.Value == "" {
		return fmt.Errorf("value é obrigatório")
	}

	for _, existing := range m.config.Entries {
		if existing.Source == entry.Source && existing.Value == entry.Value {
			return fmt.Errorf("entrada duplicada para %s: %s", entry.Source, entry.Value)
		}
	}

	m.config.Entries = append(m.config.Entries, entry)

	log.Info().
		Str("source", string(entry.Source)).
		Str("value", entry.Value).
		Msg("Triage ignore entry added")

	return nil
}

// RemoveEntry remove uma entrada por ID.
func (m *TriageIgnoreManager) RemoveEntry(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	newEntries := make([]TriageIgnoreEntry, 0, len(m.config.Entries))
	found := false

	for _, entry := range m.config.Entries {
		if entry.ID == id {
			found = true
			log.Info().Str("id", id).Str("source", string(entry.Source)).Str("value", entry.Value).
				Msg("Triage ignore entry removed")
			continue
		}
		newEntries = append(newEntries, entry)
	}

	if !found {
		return fmt.Errorf("entrada não encontrada: %s", id)
	}

	m.config.Entries = newEntries
	return nil
}

// GetEntries retorna todas as entradas (cópia, evita race condition no chamador).
func (m *TriageIgnoreManager) GetEntries() []TriageIgnoreEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()

	entries := make([]TriageIgnoreEntry, len(m.config.Entries))
	copy(entries, m.config.Entries)
	return entries
}

// IgnoredValues retorna o conjunto de valores ignorados para uma fonte específica — usado pelos
// TargetSource concretos (target_source_dynatrace.go, target_source_prometheus.go) pra filtrar
// ANTES de popular Namespaces/Reasons. Nunca retorna nil (map vazio quando não há entradas), mas
// chamadores com um *TriageIgnoreManager nil devem tratar isso separadamente — ver
// Orchestrator.buildTriageSources.
func (m *TriageIgnoreManager) IgnoredValues(source TriageIgnoreSource) map[string]struct{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make(map[string]struct{})
	for _, entry := range m.config.Entries {
		if entry.Source == source {
			out[entry.Value] = struct{}{}
		}
	}
	return out
}
