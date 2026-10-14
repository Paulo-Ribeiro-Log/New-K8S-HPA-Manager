package storage

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// HTTPClientExecutionsKeep é quantas execuções cada usuário mantém; as mais antigas saem a cada
// inclusão (é uma lista de "últimas requisições", não auditoria — essa fica no Change History).
const HTTPClientExecutionsKeep = 200

// HTTPClientExecution é uma execução do Cliente HTTP. Request guarda a requisição inteira (JSON do
// body do /send, com headers e corpo) para poder re-executar; da resposta, só o resumo — o corpo
// pode ser grande e é refeito ao re-executar.
type HTTPClientExecution struct {
	ID            int64     `json:"id"`
	UserEmail     string    `json:"user_email"`
	CreatedAt     time.Time `json:"created_at"`
	Request       string    `json:"request"`
	Method        string    `json:"method"`
	URL           string    `json:"url"`
	ExecutionMode string    `json:"execution_mode"`
	Cluster       string    `json:"cluster,omitempty"`
	Status        int       `json:"status,omitempty"`
	StatusText    string    `json:"status_text,omitempty"`
	DurationMs    int64     `json:"duration_ms"`
	SizeBytes     int64     `json:"size_bytes"`
	ExecutedFrom  string    `json:"executed_from,omitempty"`
	Error         string    `json:"error,omitempty"`
}

// HTTPClientExecutionStore persiste as execuções do Cliente HTTP em
// ~/.k8s-hpa-manager/http_client_executions.db (0600: headers podem ter tokens).
type HTTPClientExecutionStore struct {
	db *sql.DB
}

const httpClientExecutionsSchema = `
CREATE TABLE IF NOT EXISTS http_client_executions (
    id             INTEGER  PRIMARY KEY AUTOINCREMENT,
    user_email     TEXT     NOT NULL DEFAULT '',
    created_at     DATETIME NOT NULL,
    request        TEXT     NOT NULL,
    method         TEXT     NOT NULL,
    url            TEXT     NOT NULL,
    execution_mode TEXT     NOT NULL DEFAULT 'server',
    cluster        TEXT     NOT NULL DEFAULT '',
    status         INTEGER  NOT NULL DEFAULT 0,
    status_text    TEXT     NOT NULL DEFAULT '',
    duration_ms    INTEGER  NOT NULL DEFAULT 0,
    size_bytes     INTEGER  NOT NULL DEFAULT 0,
    executed_from  TEXT     NOT NULL DEFAULT '',
    error          TEXT     NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_http_client_executions_user ON http_client_executions(user_email, id DESC);
`

func NewHTTPClientExecutionStore(dbPath string) (*HTTPClientExecutionStore, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0700); err != nil {
		return nil, fmt.Errorf("criar diretório http client executions: %w", err)
	}
	// O SQLite cria -wal/-shm com a permissão do banco: criar já 0600 cobre os três.
	if f, err := os.OpenFile(dbPath, os.O_CREATE|os.O_RDWR, 0600); err == nil {
		f.Close()
	}
	_ = os.Chmod(dbPath, 0600)
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("abrir http_client_executions.db: %w", err)
	}
	db.SetMaxOpenConns(3)
	db.SetMaxIdleConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping http_client_executions.db: %w", err)
	}
	if _, err := db.Exec(httpClientExecutionsSchema); err != nil {
		db.Close()
		return nil, fmt.Errorf("criar schema http_client_executions: %w", err)
	}
	return &HTTPClientExecutionStore{db: db}, nil
}

func (s *HTTPClientExecutionStore) Close() error {
	return s.db.Close()
}

// Add grava a execução e descarta as do mesmo usuário além das HTTPClientExecutionsKeep mais recentes.
func (s *HTTPClientExecutionStore) Add(e *HTTPClientExecution) error {
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now()
	}
	res, err := s.db.Exec(
		`INSERT INTO http_client_executions (user_email, created_at, request, method, url, execution_mode, cluster,
			status, status_text, duration_ms, size_bytes, executed_from, error) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		e.UserEmail, e.CreatedAt.UTC(), e.Request, e.Method, e.URL, e.ExecutionMode, e.Cluster,
		e.Status, e.StatusText, e.DurationMs, e.SizeBytes, e.ExecutedFrom, e.Error,
	)
	if err != nil {
		return fmt.Errorf("gravar execução do cliente http: %w", err)
	}
	e.ID, _ = res.LastInsertId()
	_, err = s.db.Exec(
		`DELETE FROM http_client_executions WHERE user_email = ? AND id NOT IN (
			SELECT id FROM http_client_executions WHERE user_email = ? ORDER BY id DESC LIMIT ?)`,
		e.UserEmail, e.UserEmail, HTTPClientExecutionsKeep,
	)
	return err
}

// List devolve as execuções do usuário, mais recentes primeiro.
func (s *HTTPClientExecutionStore) List(userEmail string, limit int) ([]HTTPClientExecution, error) {
	if limit <= 0 || limit > HTTPClientExecutionsKeep {
		limit = HTTPClientExecutionsKeep
	}
	rows, err := s.db.Query(
		`SELECT id, user_email, created_at, request, method, url, execution_mode, cluster,
			status, status_text, duration_ms, size_bytes, executed_from, error
		 FROM http_client_executions WHERE user_email = ? ORDER BY id DESC LIMIT ?`,
		userEmail, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("listar execuções do cliente http: %w", err)
	}
	defer rows.Close()

	out := []HTTPClientExecution{}
	for rows.Next() {
		var e HTTPClientExecution
		if err := rows.Scan(&e.ID, &e.UserEmail, &e.CreatedAt, &e.Request, &e.Method, &e.URL, &e.ExecutionMode, &e.Cluster,
			&e.Status, &e.StatusText, &e.DurationMs, &e.SizeBytes, &e.ExecutedFrom, &e.Error); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// Delete remove uma execução do próprio usuário. Devolve false se não existia (ou é de outro usuário).
func (s *HTTPClientExecutionStore) Delete(userEmail string, id int64) (bool, error) {
	res, err := s.db.Exec(`DELETE FROM http_client_executions WHERE id = ? AND user_email = ?`, id, userEmail)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}
