package storage

import (
	"bytes"
	"compress/gzip"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"k8s-hpa-manager/internal/dbfile"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// Relatórios do Relatório de Métricas que o operador salvou de propósito (só ao clicar em "Salvar": nada é
// guardado automaticamente). O resultado é JSON opaco para este pacote (quem salva monta e quem abre interpreta),
// guardado comprimido. Cada usuário só enxerga os dele.

const (
	// MaxSavedReportsPerUser limita quantos relatórios cada usuário guarda. Passou disso, o usuário apaga algum:
	// nada é descartado em silêncio.
	MaxSavedReportsPerUser = 50
	// MaxSavedReportBytes limita o JSON de UM relatório (antes de comprimir). 200 aplicações com tudo ligado
	// ficam em poucos MB; isto só barra o absurdo.
	MaxSavedReportBytes = 16 << 20
	// MaxSavedReportNameLen é o tamanho do nome, em caracteres.
	MaxSavedReportNameLen = 80
)

var (
	ErrSavedReportNotFound = errors.New("relatório salvo não encontrado")
	ErrSavedReportsLimit   = fmt.Errorf("limite de %d relatórios salvos atingido: apague algum para salvar outro", MaxSavedReportsPerUser)
	ErrSavedReportTooLarge = fmt.Errorf("relatório grande demais para salvar (máximo %d MB)", MaxSavedReportBytes>>20)
	ErrSavedReportName     = fmt.Errorf("informe um nome de até %d caracteres", MaxSavedReportNameLen)
)

// SavedReportMeta é o que a lista mostra, sem o resultado.
type SavedReportMeta struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	CreatedAt   time.Time `json:"created_at"`
	WindowStart time.Time `json:"window_start"`
	WindowEnd   time.Time `json:"window_end"`
	Apps        int       `json:"apps"`
	Clusters    int       `json:"clusters"`
	// SizeBytes é o tamanho do JSON do relatório (antes de comprimir).
	SizeBytes int `json:"size_bytes"`
}

type SavedReportsStore struct {
	db *sql.DB
	mu sync.RWMutex
}

const savedReportsSchema = `
CREATE TABLE IF NOT EXISTS saved_reports (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    user_email   TEXT     NOT NULL,
    name         TEXT     NOT NULL,
    created_at   DATETIME NOT NULL,
    window_start DATETIME NOT NULL,
    window_end   DATETIME NOT NULL,
    apps         INTEGER  NOT NULL,
    clusters     INTEGER  NOT NULL,
    size_bytes   INTEGER  NOT NULL,
    data         BLOB     NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_saved_reports_user ON saved_reports(user_email, created_at DESC);
`

func NewSavedReportsStore(dbPath string) (*SavedReportsStore, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("criar diretório saved-reports: %w", err)
	}
	dbfile.Restrict(dbPath)
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("abrir saved-reports.db: %w", err)
	}
	db.SetMaxOpenConns(3)
	db.SetMaxIdleConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping saved-reports.db: %w", err)
	}
	if _, err := db.Exec(savedReportsSchema); err != nil {
		db.Close()
		return nil, fmt.Errorf("criar schema saved_reports: %w", err)
	}
	return &SavedReportsStore{db: db}, nil
}

func (s *SavedReportsStore) Close() error { return s.db.Close() }

func gz(b []byte) ([]byte, error) {
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := w.Write(b); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func gunzip(b []byte) ([]byte, error) {
	r, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(io.LimitReader(r, MaxSavedReportBytes+1))
}

// Save guarda o relatório do usuário. meta traz o que a lista exibe (nome, janela, contagens); ID, CreatedAt e
// SizeBytes são preenchidos aqui. Devolve ErrSavedReportsLimit, ErrSavedReportTooLarge ou ErrSavedReportName.
func (s *SavedReportsStore) Save(userEmail string, meta SavedReportMeta, resultJSON []byte) (SavedReportMeta, error) {
	meta.Name = strings.TrimSpace(meta.Name)
	if n := len([]rune(meta.Name)); n == 0 || n > MaxSavedReportNameLen {
		return SavedReportMeta{}, ErrSavedReportName
	}
	if len(resultJSON) == 0 {
		return SavedReportMeta{}, fmt.Errorf("relatório vazio")
	}
	if len(resultJSON) > MaxSavedReportBytes {
		return SavedReportMeta{}, ErrSavedReportTooLarge
	}
	packed, err := gz(resultJSON)
	if err != nil {
		return SavedReportMeta{}, fmt.Errorf("comprimir relatório: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM saved_reports WHERE user_email = ?`, userEmail).Scan(&n); err != nil {
		return SavedReportMeta{}, fmt.Errorf("contar relatórios salvos: %w", err)
	}
	if n >= MaxSavedReportsPerUser {
		return SavedReportMeta{}, ErrSavedReportsLimit
	}
	meta.CreatedAt = time.Now().UTC()
	meta.SizeBytes = len(resultJSON)
	res, err := s.db.Exec(
		`INSERT INTO saved_reports (user_email, name, created_at, window_start, window_end, apps, clusters, size_bytes, data)
		 VALUES (?,?,?,?,?,?,?,?,?)`,
		userEmail, meta.Name, meta.CreatedAt, meta.WindowStart.UTC(), meta.WindowEnd.UTC(), meta.Apps, meta.Clusters, meta.SizeBytes, packed,
	)
	if err != nil {
		return SavedReportMeta{}, fmt.Errorf("inserir relatório salvo: %w", err)
	}
	meta.ID, _ = res.LastInsertId()
	return meta, nil
}

// List devolve os relatórios do usuário, do mais novo ao mais antigo, sem o resultado.
func (s *SavedReportsStore) List(userEmail string) ([]SavedReportMeta, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rows, err := s.db.Query(
		`SELECT id, name, created_at, window_start, window_end, apps, clusters, size_bytes
		 FROM saved_reports WHERE user_email = ? ORDER BY created_at DESC, id DESC`, userEmail)
	if err != nil {
		return nil, fmt.Errorf("listar relatórios salvos: %w", err)
	}
	defer rows.Close()
	out := []SavedReportMeta{}
	for rows.Next() {
		var m SavedReportMeta
		if err := rows.Scan(&m.ID, &m.Name, &m.CreatedAt, &m.WindowStart, &m.WindowEnd, &m.Apps, &m.Clusters, &m.SizeBytes); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// Get devolve o relatório (só se for do usuário) e seu JSON já descomprimido.
func (s *SavedReportsStore) Get(userEmail string, id int64) (SavedReportMeta, []byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var m SavedReportMeta
	var packed []byte
	err := s.db.QueryRow(
		`SELECT id, name, created_at, window_start, window_end, apps, clusters, size_bytes, data
		 FROM saved_reports WHERE id = ? AND user_email = ?`, id, userEmail,
	).Scan(&m.ID, &m.Name, &m.CreatedAt, &m.WindowStart, &m.WindowEnd, &m.Apps, &m.Clusters, &m.SizeBytes, &packed)
	if errors.Is(err, sql.ErrNoRows) {
		return SavedReportMeta{}, nil, ErrSavedReportNotFound
	}
	if err != nil {
		return SavedReportMeta{}, nil, fmt.Errorf("ler relatório salvo: %w", err)
	}
	raw, err := gunzip(packed)
	if err != nil {
		return SavedReportMeta{}, nil, fmt.Errorf("descomprimir relatório salvo: %w", err)
	}
	return m, raw, nil
}

// Delete apaga o relatório do usuário. O de outro usuário "não existe".
func (s *SavedReportsStore) Delete(userEmail string, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.db.Exec(`DELETE FROM saved_reports WHERE id = ? AND user_email = ?`, id, userEmail)
	if err != nil {
		return fmt.Errorf("apagar relatório salvo: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrSavedReportNotFound
	}
	return nil
}
