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

// Note representa uma anotação em Markdown escopada por cluster+aba.
// Cada Save insere uma NOVA entrada — o histórico é um diário, nunca um documento sobrescrito.
type Note struct {
	ID        int64     `json:"id"`
	Cluster   string    `json:"cluster"`
	Tab       string    `json:"tab"`
	Content   string    `json:"content"`
	UserEmail string    `json:"user_email"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// NotesStore persiste anotações por cluster+aba em um banco SQLite próprio.
type NotesStore struct {
	db *sql.DB
	mu sync.RWMutex
}

const notesSchema = `
CREATE TABLE IF NOT EXISTS notes (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    cluster    TEXT     NOT NULL,
    tab        TEXT     NOT NULL,
    content    TEXT     NOT NULL,
    user_email TEXT     NOT NULL,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_notes_scope ON notes(cluster, tab, created_at DESC);
`

// generalNotesTab/generalNotesCluster espelham as constantes GENERAL_NOTES_TAB/GENERAL_NOTES_CLUSTER
// do frontend (hooks/useNotes.ts) — precisam bater exatamente com os mesmos literais.
const (
	generalNotesTab     = "__general__"
	generalNotesCluster = "__all_clusters__"
)

func NewNotesStore(dbPath string) (*NotesStore, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("criar diretório notes: %w", err)
	}
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("abrir notes.db: %w", err)
	}
	db.SetMaxOpenConns(3)
	db.SetMaxIdleConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping notes.db: %w", err)
	}
	if _, err := db.Exec(notesSchema); err != nil {
		db.Close()
		return nil, fmt.Errorf("criar schema notes: %w", err)
	}
	// Migração idempotente: "Lembretes gerais" (tab=__general__) passaram a ser globais
	// (independentes do cluster ativo no momento em que foram escritos) em vez de escopados
	// por cluster+aba como as notas normais — sem isso, um lembrete geral criado sob um
	// cluster específico sumia da visão assim que o usuário trocava de cluster (e o app não
	// lembrava o último cluster selecionado entre reinícios), parecendo perda de dado mesmo
	// com a linha intacta no SQLite. Consolida qualquer entrada antiga de __general__ ainda
	// presa a um cluster real no novo cluster-sentinela; roda em toda inicialização mas só
	// afeta linhas que ainda não migraram (WHERE cluster != sentinela).
	if _, err := db.Exec(
		`UPDATE notes SET cluster = ? WHERE tab = ? AND cluster != ?`,
		generalNotesCluster, generalNotesTab, generalNotesCluster,
	); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrar lembretes gerais: %w", err)
	}
	return &NotesStore{db: db}, nil
}

func (s *NotesStore) Close() error {
	return s.db.Close()
}

// Save insere uma NOVA entrada (modelo diário — nunca sobrescreve uma nota existente).
func (s *NotesStore) Save(n Note) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	res, err := s.db.Exec(
		`INSERT INTO notes (cluster, tab, content, user_email, created_at, updated_at)
		 VALUES (?,?,?,?,?,?)`,
		n.Cluster, n.Tab, n.Content, n.UserEmail, now, now,
	)
	if err != nil {
		return 0, fmt.Errorf("inserir note: %w", err)
	}
	return res.LastInsertId()
}

// List retorna as notas de um escopo (cluster+tab), mais recente primeiro.
// Nunca retorna nil — slice nil vira null no JSON, o frontend espera array.
func (s *NotesStore) List(cluster, tab string) ([]Note, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(
		`SELECT id, cluster, tab, content, user_email, created_at, updated_at
		 FROM notes WHERE cluster=? AND tab=? ORDER BY created_at DESC`,
		cluster, tab,
	)
	if err != nil {
		return nil, fmt.Errorf("listar notes: %w", err)
	}
	defer rows.Close()

	notes := []Note{}
	for rows.Next() {
		var n Note
		if err := rows.Scan(&n.ID, &n.Cluster, &n.Tab, &n.Content, &n.UserEmail, &n.CreatedAt, &n.UpdatedAt); err != nil {
			continue
		}
		notes = append(notes, n)
	}
	return notes, nil
}

// Search busca notas por substring do conteúdo em TODOS os clusters/abas (não escopado),
// mais recente primeiro. Usado para achar uma nota quando não se lembra em qual cluster/aba foi criada.
func (s *NotesStore) Search(query string, limit int) ([]Note, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(
		`SELECT id, cluster, tab, content, user_email, created_at, updated_at
		 FROM notes WHERE content LIKE ? ESCAPE '\' ORDER BY created_at DESC LIMIT ?`,
		"%"+escapeLike(query)+"%", limit,
	)
	if err != nil {
		return nil, fmt.Errorf("buscar notes: %w", err)
	}
	defer rows.Close()

	notes := []Note{}
	for rows.Next() {
		var n Note
		if err := rows.Scan(&n.ID, &n.Cluster, &n.Tab, &n.Content, &n.UserEmail, &n.CreatedAt, &n.UpdatedAt); err != nil {
			continue
		}
		notes = append(notes, n)
	}
	return notes, nil
}

// escapeLike escapa os wildcards do LIKE (% e _) presentes no termo de busca do usuário.
func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

// GetByID busca uma nota específica — usado para checar autoria antes de editar/excluir.
func (s *NotesStore) GetByID(id int64) (*Note, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var n Note
	err := s.db.QueryRow(
		`SELECT id, cluster, tab, content, user_email, created_at, updated_at FROM notes WHERE id=?`,
		id,
	).Scan(&n.ID, &n.Cluster, &n.Tab, &n.Content, &n.UserEmail, &n.CreatedAt, &n.UpdatedAt)
	if err != nil {
		return nil, err // inclui sql.ErrNoRows
	}
	return &n, nil
}

// Update sobrescreve o conteúdo de uma entrada existente e atualiza updated_at.
func (s *NotesStore) Update(id int64, content string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(
		`UPDATE notes SET content=?, updated_at=? WHERE id=?`,
		content, time.Now().UTC(), id,
	)
	if err != nil {
		return fmt.Errorf("atualizar note: %w", err)
	}
	return nil
}

// Delete remove uma entrada específica.
func (s *NotesStore) Delete(id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`DELETE FROM notes WHERE id=?`, id)
	if err != nil {
		return fmt.Errorf("excluir note: %w", err)
	}
	return nil
}
