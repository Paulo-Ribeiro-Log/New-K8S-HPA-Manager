package history

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
)

// HistoryEntry representa uma entrada no histórico de alterações
// NOTA: Isto é um histórico PESSOAL, não um audit log para compliance
type HistoryEntry struct {
	ID          string                 `json:"id"`
	Timestamp   time.Time              `json:"timestamp"`
	Action      string                 `json:"action"`      // update_hpa, apply_nodepool, etc
	Resource    string                 `json:"resource"`    // namespace/name
	Cluster     string                 `json:"cluster"`
	Before      map[string]interface{} `json:"before"`
	After       map[string]interface{} `json:"after"`
	Status      string                 `json:"status"`       // success, failed
	ErrorMsg    string                 `json:"error_msg,omitempty"`
	Duration    int64                  `json:"duration_ms"`  // Tempo de execução em ms
	SessionName string                 `json:"session_name,omitempty"`
}

// HistoryTracker gerencia o histórico de alterações
type HistoryTracker struct {
	entries      []HistoryEntry
	mutex        sync.RWMutex
	historyDir   string
	maxEntries   int // Limite de entradas em memória
}

// NewHistoryTracker cria um novo tracker
func NewHistoryTracker(baseDir string) (*HistoryTracker, error) {
	historyDir := filepath.Join(baseDir, "history")

	// Criar diretório se não existir
	if err := os.MkdirAll(historyDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create history directory: %w", err)
	}

	tracker := &HistoryTracker{
		entries:    make([]HistoryEntry, 0),
		historyDir: historyDir,
		maxEntries: 1000, // Manter últimas 1000 entradas em memória
	}

	// Carregar histórico existente
	if err := tracker.loadFromDisk(); err != nil {
		// Não é erro fatal, apenas log
		fmt.Printf("Warning: could not load history: %v\n", err)
	}

	return tracker, nil
}

// Log adiciona uma entrada ao histórico
func (ht *HistoryTracker) Log(entry HistoryEntry) error {
	ht.mutex.Lock()
	defer ht.mutex.Unlock()

	// Gerar ID se não fornecido
	if entry.ID == "" {
		entry.ID = uuid.New().String()
	}

	// Timestamp se não fornecido
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now()
	}

	// Adicionar à memória
	ht.entries = append(ht.entries, entry)

	// Limitar tamanho em memória
	if len(ht.entries) > ht.maxEntries {
		ht.entries = ht.entries[len(ht.entries)-ht.maxEntries:]
	}

	// Persistir em disco (async para não bloquear)
	go func() {
		if err := ht.saveToDisk(entry); err != nil {
			fmt.Printf("Warning: failed to save history entry: %v\n", err)
		}
	}()

	return nil
}

// GetAll retorna todas as entradas do histórico
func (ht *HistoryTracker) GetAll() []HistoryEntry {
	ht.mutex.RLock()
	defer ht.mutex.RUnlock()

	// Retornar cópia para evitar race conditions
	entries := make([]HistoryEntry, len(ht.entries))
	copy(entries, ht.entries)

	// Ordenar por timestamp (mais recente primeiro)
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Timestamp.After(entries[j].Timestamp)
	})

	return entries
}

// GetFiltered retorna entradas filtradas
func (ht *HistoryTracker) GetFiltered(filter HistoryFilter) []HistoryEntry {
	ht.mutex.RLock()
	defer ht.mutex.RUnlock()

	filtered := make([]HistoryEntry, 0)

	for _, entry := range ht.entries {
		if filter.Matches(entry) {
			filtered = append(filtered, entry)
		}
	}

	// Ordenar por timestamp (mais recente primeiro)
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].Timestamp.After(filtered[j].Timestamp)
	})

	return filtered
}

// GetByID retorna uma entrada específica por ID
func (ht *HistoryTracker) GetByID(id string) (*HistoryEntry, error) {
	ht.mutex.RLock()
	defer ht.mutex.RUnlock()

	for _, entry := range ht.entries {
		if entry.ID == id {
			return &entry, nil
		}
	}

	return nil, fmt.Errorf("history entry not found: %s", id)
}

// Clear limpa todo o histórico
func (ht *HistoryTracker) Clear() error {
	ht.mutex.Lock()
	defer ht.mutex.Unlock()

	ht.entries = make([]HistoryEntry, 0)

	// Remover arquivos do disco
	files, err := filepath.Glob(filepath.Join(ht.historyDir, "*.json"))
	if err != nil {
		return err
	}

	for _, file := range files {
		if err := os.Remove(file); err != nil {
			return err
		}
	}

	return nil
}

// saveToDisk persiste uma entrada em arquivo JSON
func (ht *HistoryTracker) saveToDisk(entry HistoryEntry) error {
	// Organizar por ano/mês
	yearMonth := entry.Timestamp.Format("2006-01")
	monthDir := filepath.Join(ht.historyDir, yearMonth)

	if err := os.MkdirAll(monthDir, 0755); err != nil {
		return err
	}

	// Nome do arquivo: YYYY-MM-DD-UUID.json
	filename := fmt.Sprintf("%s-%s.json",
		entry.Timestamp.Format("2006-01-02"),
		entry.ID)
	filepath := filepath.Join(monthDir, filename)

	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filepath, data, 0644)
}

// loadFromDisk carrega histórico existente do disco
func (ht *HistoryTracker) loadFromDisk() error {
	// Carregar apenas últimos 3 meses para evitar sobrecarga
	now := time.Now()
	for i := 0; i < 3; i++ {
		yearMonth := now.AddDate(0, -i, 0).Format("2006-01")
		monthDir := filepath.Join(ht.historyDir, yearMonth)

		files, err := filepath.Glob(filepath.Join(monthDir, "*.json"))
		if err != nil {
			continue
		}

		for _, file := range files {
			data, err := os.ReadFile(file)
			if err != nil {
				continue
			}

			var entry HistoryEntry
			if err := json.Unmarshal(data, &entry); err != nil {
				continue
			}

			ht.entries = append(ht.entries, entry)
		}
	}

	// Ordenar por timestamp
	sort.Slice(ht.entries, func(i, j int) bool {
		return ht.entries[i].Timestamp.After(ht.entries[j].Timestamp)
	})

	// Limitar tamanho
	if len(ht.entries) > ht.maxEntries {
		ht.entries = ht.entries[:ht.maxEntries]
	}

	return nil
}

// HistoryFilter define filtros para busca
type HistoryFilter struct {
	Action      string    // Filtrar por action
	Cluster     string    // Filtrar por cluster
	Resource    string    // Filtrar por resource
	Status      string    // Filtrar por status
	StartDate   time.Time // Data inicial
	EndDate     time.Time // Data final
	SessionName string    // Filtrar por sessão
}

// Matches verifica se uma entrada corresponde ao filtro
func (f HistoryFilter) Matches(entry HistoryEntry) bool {
	if f.Action != "" && entry.Action != f.Action {
		return false
	}

	if f.Cluster != "" && entry.Cluster != f.Cluster {
		return false
	}

	if f.Resource != "" && entry.Resource != f.Resource {
		return false
	}

	if f.Status != "" && entry.Status != f.Status {
		return false
	}

	if !f.StartDate.IsZero() && entry.Timestamp.Before(f.StartDate) {
		return false
	}

	if !f.EndDate.IsZero() && entry.Timestamp.After(f.EndDate) {
		return false
	}

	if f.SessionName != "" && entry.SessionName != f.SessionName {
		return false
	}

	return true
}

// Action constants
const (
	ActionUpdateHPA         = "update_hpa"
	ActionApplyNodePool     = "apply_nodepool"
	ActionSuspendCronJob    = "suspend_cronjob"
	ActionResumeCronJob     = "resume_cronjob"
	ActionRolloutPrometheus = "rollout_prometheus"
	ActionSaveSession       = "save_session"
	ActionLoadSession       = "load_session"
	ActionDeleteSession     = "delete_session"
	ActionApplyBatch        = "apply_batch"
	ActionSnapshotCluster   = "snapshot_cluster"
)

// Status constants
const (
	StatusSuccess = "success"
	StatusFailed  = "failed"
	StatusPartial = "partial"
)
