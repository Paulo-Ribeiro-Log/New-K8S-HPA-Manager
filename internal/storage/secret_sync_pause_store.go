package storage

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// secret_sync_pause_store.go — registro de "sincronização pausada" pra Secrets gerenciados por um
// ExternalSecret (external-secrets), ver internal/web/handlers/secret_sync_pause.go. Motivado por
// um caso real: pra testar aplicações em HLG às vezes é preciso digitar manualmente um valor num
// Secret que o AKV não está sincronizando de verdade (falha na origem ou impedimento técnico), mas
// editar e aplicar não sobrevive nem alguns segundos — o operador vigia o Secret (ownerReference
// com controller:true) e reconcilia (reverte) qualquer drift quase instantaneamente, não só no
// refreshInterval. "Pausar" apaga o ExternalSecret (só quando spec.target.deletionPolicy=Retain,
// que garante que apagar o ExternalSecret NÃO apaga o Secret) depois de guardar o manifesto
// completo aqui, pra "Retomar" poder recriar exatamente como estava.
//
// Um registro por (cluster, namespace, secret_name) — pausar de novo com uma sessão já pausada
// simplesmente substitui o registro (não deveria acontecer na prática, já que o botão de pausar
// fica desabilitado durante a pausa, mas evita erro de UNIQUE se dois usuários tentarem ao mesmo
// tempo). Resume (bem-sucedido) remove o registro.
type SecretSyncPause struct {
	Cluster                string    `json:"cluster"`
	Namespace              string    `json:"namespace"`
	SecretName             string    `json:"secret_name"`
	ExternalSecretName     string    `json:"external_secret_name"`
	ExternalSecretManifest string    `json:"-"` // YAML completo — nunca precisa ir pro frontend
	PausedBy               string    `json:"paused_by"`
	PausedAt               time.Time `json:"paused_at"`
	Reason                 string    `json:"reason,omitempty"`
}

type SecretSyncPauseStore struct {
	db *sql.DB
	mu sync.RWMutex
}

const secretSyncPauseSchema = `
CREATE TABLE IF NOT EXISTS secret_sync_pauses (
    id                        INTEGER PRIMARY KEY AUTOINCREMENT,
    cluster                   TEXT     NOT NULL,
    namespace                 TEXT     NOT NULL,
    secret_name               TEXT     NOT NULL,
    external_secret_name      TEXT     NOT NULL,
    external_secret_manifest  TEXT     NOT NULL,
    paused_by                 TEXT     NOT NULL,
    paused_at                 DATETIME NOT NULL,
    reason                    TEXT,
    UNIQUE(cluster, namespace, secret_name)
);
`

func NewSecretSyncPauseStore(dbPath string) (*SecretSyncPauseStore, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("criar diretório secret-sync-pauses: %w", err)
	}
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("abrir secret-sync-pauses.db: %w", err)
	}
	db.SetMaxOpenConns(3)
	db.SetMaxIdleConns(1)
	if err := db.Ping(); err != nil {
		db.Close() //nolint:errcheck
		return nil, fmt.Errorf("ping secret-sync-pauses.db: %w", err)
	}
	if _, err := db.Exec(secretSyncPauseSchema); err != nil {
		db.Close() //nolint:errcheck
		return nil, fmt.Errorf("criar schema secret-sync-pauses: %w", err)
	}
	return &SecretSyncPauseStore{db: db}, nil
}

func (s *SecretSyncPauseStore) Close() error {
	return s.db.Close()
}

// Pause grava o registro de pausa (INSERT OR REPLACE — ver comentário do tipo acima).
func (s *SecretSyncPauseStore) Pause(rec SecretSyncPause) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if rec.PausedAt.IsZero() {
		rec.PausedAt = time.Now().UTC()
	}
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO secret_sync_pauses
		 (cluster, namespace, secret_name, external_secret_name, external_secret_manifest, paused_by, paused_at, reason)
		 VALUES (?,?,?,?,?,?,?,?)`,
		rec.Cluster, rec.Namespace, rec.SecretName, rec.ExternalSecretName, rec.ExternalSecretManifest,
		rec.PausedBy, rec.PausedAt, nullableString(rec.Reason),
	)
	if err != nil {
		return fmt.Errorf("gravar pausa de sync: %w", err)
	}
	return nil
}

// Get retorna o registro de pausa ativo de um Secret, ou nil se ele não estiver pausado (estado
// normal — não é erro).
func (s *SecretSyncPauseStore) Get(cluster, namespace, secretName string) (*SecretSyncPause, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var rec SecretSyncPause
	var reason sql.NullString
	err := s.db.QueryRow(
		`SELECT cluster, namespace, secret_name, external_secret_name, external_secret_manifest, paused_by, paused_at, reason
		 FROM secret_sync_pauses WHERE cluster=? AND namespace=? AND secret_name=?`,
		cluster, namespace, secretName,
	).Scan(&rec.Cluster, &rec.Namespace, &rec.SecretName, &rec.ExternalSecretName, &rec.ExternalSecretManifest,
		&rec.PausedBy, &rec.PausedAt, &reason)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("buscar pausa de sync: %w", err)
	}
	rec.Reason = reason.String
	return &rec, nil
}

// Resume remove o registro de pausa — chamado depois que o ExternalSecret já foi recriado com
// sucesso no cluster (ver ResumeSync no handler). Idempotente: remover um registro inexistente não
// é erro.
func (s *SecretSyncPauseStore) Resume(cluster, namespace, secretName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(
		`DELETE FROM secret_sync_pauses WHERE cluster=? AND namespace=? AND secret_name=?`,
		cluster, namespace, secretName,
	)
	if err != nil {
		return fmt.Errorf("remover pausa de sync: %w", err)
	}
	return nil
}
