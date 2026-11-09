package storage

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"k8s-hpa-manager/internal/dbfile"

	_ "github.com/mattn/go-sqlite3"
)

// TeamsSentStore guarda os envios do Teams Broadcast com o identificador que o Teams deu a cada
// mensagem (message_id), para poder apagar ou editar uma mensagem a qualquer momento, mesmo depois
// de outros envios e de reiniciar o servidor. Cada lote tem um resumo curto do texto (primeira
// linha, para reconhecê-lo na lista) — o texto completo da mensagem NÃO é gravado.
type TeamsSentStore struct {
	db *sql.DB
	mu sync.Mutex
}

// Estados de uma mensagem enviada.
const (
	TeamsSentStateSent    = "sent"
	TeamsSentStateEdited  = "edited"
	TeamsSentStateDeleted = "deleted"
)

// TeamsSentBatch é um envio em lote (uma ação de "Enviar").
type TeamsSentBatch struct {
	ID      string    `json:"id"`
	SentAt  time.Time `json:"sent_at"`
	SentBy  string    `json:"sent_by"`
	Preview string    `json:"preview"`
	Total   int       `json:"total"`   // destinatários do envio
	OK      int       `json:"ok"`      // enviados com sucesso
	Active  int       `json:"active"`  // mensagens ainda existentes (enviadas ou editadas)
	Edited  int       `json:"edited"`  // das ativas, quantas já foram editadas
	Deleted int       `json:"deleted"` // apagadas pela ferramenta
}

// TeamsSentMessage é a mensagem enviada a um destinatário.
type TeamsSentMessage struct {
	BatchID     string    `json:"batch_id"`
	ThreadID    string    `json:"thread_id"`
	DisplayName string    `json:"display_name"`
	MessageID   string    `json:"message_id"`
	State       string    `json:"state"`
	UpdatedAt   time.Time `json:"updated_at"`
}

const teamsSentSchema = `
CREATE TABLE IF NOT EXISTS teams_sent_batches (
    id      TEXT     PRIMARY KEY,
    sent_at DATETIME NOT NULL,
    sent_by TEXT     NOT NULL DEFAULT '',
    preview TEXT     NOT NULL DEFAULT '',
    total   INTEGER  NOT NULL DEFAULT 0,
    ok      INTEGER  NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS teams_sent_messages (
    batch_id     TEXT     NOT NULL,
    thread_id    TEXT     NOT NULL,
    display_name TEXT     NOT NULL DEFAULT '',
    message_id   TEXT     NOT NULL,
    state        TEXT     NOT NULL DEFAULT 'sent',
    updated_at   DATETIME NOT NULL,
    PRIMARY KEY (thread_id, message_id)
);
CREATE INDEX IF NOT EXISTS idx_teams_sent_messages_batch ON teams_sent_messages(batch_id);
CREATE INDEX IF NOT EXISTS idx_teams_sent_batches_at ON teams_sent_batches(sent_at);`

func NewTeamsSentStore(dbPath string) (*TeamsSentStore, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("criar diretório teams_sent: %w", err)
	}
	dbfile.Restrict(dbPath)
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("abrir teams_sent.db: %w", err)
	}
	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping teams_sent.db: %w", err)
	}
	if _, err := db.Exec(teamsSentSchema); err != nil {
		db.Close()
		return nil, fmt.Errorf("criar schema teams_sent: %w", err)
	}
	return &TeamsSentStore{db: db}, nil
}

// SaveBatch grava o lote e as mensagens enviadas com sucesso (message_id não vazio).
func (s *TeamsSentStore) SaveBatch(b TeamsSentBatch, msgs []TeamsSentMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	at := b.SentAt.UTC()
	if _, err := tx.Exec(`INSERT OR REPLACE INTO teams_sent_batches (id, sent_at, sent_by, preview, total, ok) VALUES (?,?,?,?,?,?)`,
		b.ID, at, b.SentBy, b.Preview, b.Total, b.OK); err != nil {
		return fmt.Errorf("gravar lote: %w", err)
	}
	for _, m := range msgs {
		if m.MessageID == "" {
			continue
		}
		if _, err := tx.Exec(`INSERT OR REPLACE INTO teams_sent_messages (batch_id, thread_id, display_name, message_id, state, updated_at) VALUES (?,?,?,?,?,?)`,
			b.ID, m.ThreadID, m.DisplayName, m.MessageID, TeamsSentStateSent, at); err != nil {
			return fmt.Errorf("gravar mensagem: %w", err)
		}
	}
	return tx.Commit()
}

// ListBatches devolve os lotes mais recentes primeiro, com as contagens por estado.
func (s *TeamsSentStore) ListBatches(limit int) ([]TeamsSentBatch, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.Query(`
		SELECT b.id, b.sent_at, b.sent_by, b.preview, b.total, b.ok,
		       COALESCE(SUM(CASE WHEN m.state IN ('sent','edited') THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN m.state = 'edited' THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN m.state = 'deleted' THEN 1 ELSE 0 END), 0)
		FROM teams_sent_batches b
		LEFT JOIN teams_sent_messages m ON m.batch_id = b.id
		GROUP BY b.id
		ORDER BY b.sent_at DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TeamsSentBatch{}
	for rows.Next() {
		var b TeamsSentBatch
		if err := rows.Scan(&b.ID, &b.SentAt, &b.SentBy, &b.Preview, &b.Total, &b.OK, &b.Active, &b.Edited, &b.Deleted); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// ListMessages devolve as mensagens de um lote.
func (s *TeamsSentStore) ListMessages(batchID string) ([]TeamsSentMessage, error) {
	rows, err := s.db.Query(`SELECT batch_id, thread_id, display_name, message_id, state, updated_at
		FROM teams_sent_messages WHERE batch_id = ? ORDER BY display_name COLLATE NOCASE, thread_id`, batchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TeamsSentMessage{}
	for rows.Next() {
		var m TeamsSentMessage
		if err := rows.Scan(&m.BatchID, &m.ThreadID, &m.DisplayName, &m.MessageID, &m.State, &m.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// MarkState muda o estado das mensagens pelo par (thread_id, message_id).
func (s *TeamsSentStore) MarkState(state string, refs [][2]string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	now := time.Now().UTC()
	for _, r := range refs {
		if _, err := tx.Exec(`UPDATE teams_sent_messages SET state = ?, updated_at = ? WHERE thread_id = ? AND message_id = ?`,
			state, now, r[0], r[1]); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// DeleteBatch esquece o lote (só da lista local; não apaga nada no Teams).
func (s *TeamsSentStore) DeleteBatch(batchID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.Exec(`DELETE FROM teams_sent_messages WHERE batch_id = ?`, batchID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM teams_sent_batches WHERE id = ?`, batchID); err != nil {
		return err
	}
	return tx.Commit()
}

// PruneOlderThan remove lotes (e mensagens) enviados antes de cutoff. Devolve quantos lotes saíram.
func (s *TeamsSentStore) PruneOlderThan(cutoff time.Time) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback() //nolint:errcheck
	c := cutoff.UTC()
	if _, err := tx.Exec(`DELETE FROM teams_sent_messages WHERE batch_id IN (SELECT id FROM teams_sent_batches WHERE sent_at < ?)`, c); err != nil {
		return 0, err
	}
	res, err := tx.Exec(`DELETE FROM teams_sent_batches WHERE sent_at < ?`, c)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, tx.Commit()
}

func (s *TeamsSentStore) Close() error { return s.db.Close() }
