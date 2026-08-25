package storage

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// ─── "Descoberta de Rede" — Fase 5 (IP-ROUTE-DISCOVERY-PLAN.md, seção 10.1): Histórico de
// Descobertas. Persiste cada execução concluída pra que, ao buscar de novo o MESMO alvo, a
// ferramenta mostre o que já se sabe em vez de reinvestigar do zero — resolve diretamente a dor
// observada ao vivo numa sessão real: reinvestigar o mesmo host atrás de um cofre PAM (Delinea) do
// zero, múltiplas vezes, em conversas diferentes.
//
// Diferente do `NetDiscoveryRegistryStore` da Fase 4 (cache-on-read de "esse IP é um recurso K8s
// conhecido?", uma linha por IP, sempre sobrescrita) — este store é um LOG (uma linha por
// execução, nunca sobrescrita, mesmo modelo de diário já usado em `notes_store.go`) — o objetivo
// aqui é "o que descobrimos sobre este alvo ao longo do tempo", não "qual é o estado atual de um
// IP". Também diferente do `HistoryTracker` genérico da app (auditoria "quem fez o quê") — este é
// específico pra consulta de volta por alvo, não uma trilha de auditoria genérica.

// NetDiscoveryHistoryRecord é uma execução completa persistida. `ResultJSON` guarda o
// `NetDiscoveryResult` inteiro serializado (marshal/unmarshal é responsabilidade do chamador —
// `internal/web/handlers`, que já define o tipo; este pacote nunca importa `handlers`, evita
// ciclo de import) — mais simples que normalizar hops/fingerprint em colunas/tabelas próprias, já
// que o dado só precisa ser consultado por alvo/data, nunca por campo interno do resultado.
type NetDiscoveryHistoryRecord struct {
	ID          int64
	TargetInput string // normalizado (trim+lowercase) — o texto que o usuário digitou
	TargetIP    string // IP resolvido/alcançado por essa execução
	Mode        string // "pod" | "local"
	Reached     bool
	HopsCount   int
	ResultJSON  string
	CreatedAt   time.Time
	CreatedBy   string
}

// NetDiscoveryHistoryStore persiste o histórico num SQLite próprio (WAL, mesmo padrão de
// `snat_history_store.go`/`notes_store.go`).
type NetDiscoveryHistoryStore struct {
	db *sql.DB
	mu sync.RWMutex
}

const netDiscoveryHistorySchema = `
CREATE TABLE IF NOT EXISTS net_discovery_history (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    target_input TEXT     NOT NULL,
    target_ip    TEXT     NOT NULL,
    mode         TEXT     NOT NULL,
    reached      INTEGER  NOT NULL,
    hops_count   INTEGER  NOT NULL,
    result_json  TEXT     NOT NULL,
    created_at   DATETIME NOT NULL,
    created_by   TEXT
);

CREATE INDEX IF NOT EXISTS idx_net_discovery_history_target_input ON net_discovery_history(target_input, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_net_discovery_history_target_ip ON net_discovery_history(target_ip, created_at DESC);
`

func NewNetDiscoveryHistoryStore(dbPath string) (*NetDiscoveryHistoryStore, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("criar diretório net-discovery-history: %w", err)
	}
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("abrir net-discovery-history.db: %w", err)
	}
	db.SetMaxOpenConns(3)
	db.SetMaxIdleConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping net-discovery-history.db: %w", err)
	}
	if _, err := db.Exec(netDiscoveryHistorySchema); err != nil {
		db.Close()
		return nil, fmt.Errorf("criar schema net-discovery-history: %w", err)
	}
	return &NetDiscoveryHistoryStore{db: db}, nil
}

func (s *NetDiscoveryHistoryStore) Close() error {
	return s.db.Close()
}

// normalizeTarget — trim+lowercase, mesma normalização usada tanto ao salvar quanto ao consultar,
// pra "Example.COM" e "example.com" caírem na mesma chave de histórico.
func normalizeTarget(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

// Save insere uma nova execução (nunca sobrescreve — modelo de diário) e poda entradas com mais de
// 90 dias PRO MESMO ALVO (mesmo padrão de retenção já usado em `snat_history_store.go`, escopado
// por chave em vez de global — mantém o histórico de cada alvo individualmente limitado, sem
// deixar um alvo investigado com muita frequência inchar o banco).
func (s *NetDiscoveryHistoryStore) Save(r NetDiscoveryHistoryRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	targetInput := normalizeTarget(r.TargetInput)
	reached := 0
	if r.Reached {
		reached = 1
	}

	_, err := s.db.Exec(
		`INSERT INTO net_discovery_history
		 (target_input, target_ip, mode, reached, hops_count, result_json, created_at, created_by)
		 VALUES (?,?,?,?,?,?,?,?)`,
		targetInput, r.TargetIP, r.Mode, reached, r.HopsCount, r.ResultJSON, r.CreatedAt.UTC(), r.CreatedBy,
	)
	if err != nil {
		return fmt.Errorf("inserir net_discovery_history: %w", err)
	}

	_, _ = s.db.Exec(
		`DELETE FROM net_discovery_history WHERE target_input=? AND created_at < ?`,
		targetInput, r.CreatedAt.Add(-90*24*time.Hour).UTC(),
	)
	return nil
}

// GetRecentByTarget devolve as últimas `limit` execuções pra um alvo — casando por
// `target_input` normalizado OU `target_ip` (cobre o usuário alternando entre digitar o hostname e
// o IP resolvido do mesmo host), mais recente primeiro. Alvo nunca visto devolve slice vazio
// (nunca nil — evita o `null` do JSON documentado noutro lugar desta app), nunca erro.
func (s *NetDiscoveryHistoryStore) GetRecentByTarget(targetInputOrIP string, limit int) ([]NetDiscoveryHistoryRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	normalized := normalizeTarget(targetInputOrIP)
	rows, err := s.db.Query(
		`SELECT id, target_input, target_ip, mode, reached, hops_count, result_json, created_at, created_by
		 FROM net_discovery_history
		 WHERE target_input = ? OR target_ip = ?
		 ORDER BY created_at DESC
		 LIMIT ?`,
		normalized, targetInputOrIP, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("consultar net_discovery_history: %w", err)
	}
	defer rows.Close()

	records := []NetDiscoveryHistoryRecord{}
	for rows.Next() {
		var r NetDiscoveryHistoryRecord
		var reached int
		var createdBy sql.NullString
		if err := rows.Scan(&r.ID, &r.TargetInput, &r.TargetIP, &r.Mode, &reached, &r.HopsCount, &r.ResultJSON, &r.CreatedAt, &createdBy); err != nil {
			return nil, fmt.Errorf("ler linha net_discovery_history: %w", err)
		}
		r.Reached = reached != 0
		r.CreatedBy = createdBy.String
		records = append(records, r)
	}
	return records, rows.Err()
}
