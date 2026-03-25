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

// NodePoolRegistryEntry representa um node pool descoberto em um cluster K8s
type NodePoolRegistryEntry struct {
	Cluster     string    `json:"cluster"`
	NodePool    string    `json:"nodepool"`
	NodeCount   int       `json:"node_count"`
	VMSize      string    `json:"vm_size,omitempty"`
	OSSku       string    `json:"os_sku,omitempty"`
	Mode        string    `json:"mode,omitempty"` // System | User
	LastScanned time.Time `json:"last_scanned"`
}

// NodePoolRegistryStore mantém um catálogo de node pools por cluster
// para correlação com entidades Dynatrace (ex: aks-<nodepool>-vmss*)
type NodePoolRegistryStore struct {
	db *sql.DB
	mu sync.RWMutex
}

const nodepoolRegistrySchema = `
CREATE TABLE IF NOT EXISTS nodepool_registry (
    cluster      TEXT NOT NULL,
    nodepool     TEXT NOT NULL,
    node_count   INTEGER NOT NULL DEFAULT 0,
    vm_size      TEXT,
    os_sku       TEXT,
    mode         TEXT,
    last_scanned DATETIME NOT NULL,
    PRIMARY KEY (cluster, nodepool)
);

CREATE INDEX IF NOT EXISTS idx_np_reg_cluster ON nodepool_registry(cluster);
`

// NewNodePoolRegistryStore abre (ou cria) o banco SQLite do registry.
func NewNodePoolRegistryStore(dbPath string) (*NodePoolRegistryStore, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("criar diretório nodepool_registry: %w", err)
	}

	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("abrir banco nodepool_registry: %w", err)
	}
	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(2)

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping banco nodepool_registry: %w", err)
	}
	if _, err := db.Exec(nodepoolRegistrySchema); err != nil {
		return nil, fmt.Errorf("criar schema nodepool_registry: %w", err)
	}
	return &NodePoolRegistryStore{db: db}, nil
}

// Upsert insere ou atualiza um entry do registry.
func (s *NodePoolRegistryStore) Upsert(e NodePoolRegistryEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`
INSERT INTO nodepool_registry (cluster, nodepool, node_count, vm_size, os_sku, mode, last_scanned)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(cluster, nodepool) DO UPDATE SET
    node_count   = excluded.node_count,
    vm_size      = excluded.vm_size,
    os_sku       = excluded.os_sku,
    mode         = excluded.mode,
    last_scanned = excluded.last_scanned`,
		e.Cluster, e.NodePool, e.NodeCount, e.VMSize, e.OSSku, e.Mode, e.LastScanned)
	return err
}

// GetAll retorna todos os entries (opcionalmente filtrado por cluster).
func (s *NodePoolRegistryStore) GetAll(cluster string) ([]NodePoolRegistryEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `SELECT cluster, nodepool, node_count, vm_size, os_sku, mode, last_scanned FROM nodepool_registry`
	args := []interface{}{}
	if cluster != "" {
		query += ` WHERE cluster = ?`
		args = append(args, cluster)
	}
	query += ` ORDER BY cluster, nodepool`

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []NodePoolRegistryEntry
	for rows.Next() {
		var e NodePoolRegistryEntry
		var vmSize, osSku, mode sql.NullString
		if err := rows.Scan(&e.Cluster, &e.NodePool, &e.NodeCount, &vmSize, &osSku, &mode, &e.LastScanned); err != nil {
			return nil, err
		}
		e.VMSize = vmSize.String
		e.OSSku = osSku.String
		e.Mode = mode.String
		entries = append(entries, e)
	}
	if entries == nil {
		entries = make([]NodePoolRegistryEntry, 0)
	}
	return entries, rows.Err()
}

// LookupByNodePool retorna todos os entries onde o nome do nodepool coincide.
// Usado para correlacionar "aks-<nodepool>-vmss*" com clusters reais.
func (s *NodePoolRegistryStore) LookupByNodePool(nodepoolName string) ([]NodePoolRegistryEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(
		`SELECT cluster, nodepool, node_count, vm_size, os_sku, mode, last_scanned
		 FROM nodepool_registry WHERE nodepool = ? ORDER BY cluster`,
		nodepoolName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []NodePoolRegistryEntry
	for rows.Next() {
		var e NodePoolRegistryEntry
		var vmSize, osSku, mode sql.NullString
		if err := rows.Scan(&e.Cluster, &e.NodePool, &e.NodeCount, &vmSize, &osSku, &mode, &e.LastScanned); err != nil {
			return nil, err
		}
		e.VMSize = vmSize.String
		e.OSSku = osSku.String
		e.Mode = mode.String
		entries = append(entries, e)
	}
	if entries == nil {
		entries = make([]NodePoolRegistryEntry, 0)
	}
	return entries, rows.Err()
}

// LookupByKeyword retorna entries onde o nome do nodepool contém a keyword (busca parcial).
// Usado para correlacionar management zones do Dynatrace com node pools K8s.
// Ex: keyword "calculo" encontra nodepool "calculofrete".
func (s *NodePoolRegistryStore) LookupByKeyword(keyword string) ([]NodePoolRegistryEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(
		`SELECT cluster, nodepool, node_count, vm_size, os_sku, mode, last_scanned
		 FROM nodepool_registry WHERE nodepool LIKE ? ORDER BY cluster`,
		"%"+keyword+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []NodePoolRegistryEntry
	for rows.Next() {
		var e NodePoolRegistryEntry
		var vmSize, osSku, mode sql.NullString
		if err := rows.Scan(&e.Cluster, &e.NodePool, &e.NodeCount, &vmSize, &osSku, &mode, &e.LastScanned); err != nil {
			return nil, err
		}
		e.VMSize = vmSize.String
		e.OSSku = osSku.String
		e.Mode = mode.String
		entries = append(entries, e)
	}
	if entries == nil {
		entries = make([]NodePoolRegistryEntry, 0)
	}
	return entries, rows.Err()
}

// DeleteByCluster remove todos os entries de um cluster (antes de re-scan).
func (s *NodePoolRegistryStore) DeleteByCluster(cluster string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`DELETE FROM nodepool_registry WHERE cluster = ?`, cluster)
	return err
}
