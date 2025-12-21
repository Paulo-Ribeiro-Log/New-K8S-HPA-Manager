package storage

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	_ "github.com/mattn/go-sqlite3"
)

// SQLiteClient cliente SQLite para AI Diagnostics
type SQLiteClient struct {
	db   *sql.DB
	path string
	mu   sync.RWMutex
}

// NewSQLiteClient cria um novo cliente SQLite
func NewSQLiteClient(dbPath string) (*SQLiteClient, error) {
	// Criar diretório se não existir
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create db directory: %w", err)
	}

	// Abrir conexão SQLite com WAL mode (melhor concorrência)
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database: %w", err)
	}

	// Configurar pool de conexões
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)

	// Testar conexão
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	client := &SQLiteClient{
		db:   db,
		path: dbPath,
	}

	// Executar migrations
	if err := client.runMigrations(); err != nil {
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	return client, nil
}

// Close fecha a conexão com o banco de dados
func (c *SQLiteClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.db != nil {
		return c.db.Close()
	}

	return nil
}

// GetDB retorna o database handle (thread-safe)
func (c *SQLiteClient) GetDB() *sql.DB {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.db
}

// Exec executa um statement SQL
func (c *SQLiteClient) Exec(query string, args ...interface{}) (sql.Result, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.db.Exec(query, args...)
}

// Query executa uma query SQL
func (c *SQLiteClient) Query(query string, args ...interface{}) (*sql.Rows, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.db.Query(query, args...)
}

// QueryRow executa uma query que retorna uma única row
func (c *SQLiteClient) QueryRow(query string, args ...interface{}) *sql.Row {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.db.QueryRow(query, args...)
}
