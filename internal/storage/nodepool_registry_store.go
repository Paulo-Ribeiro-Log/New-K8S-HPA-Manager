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

// NodePoolRegistryEntry representa um node pool descoberto em um cluster K8s
type NodePoolRegistryEntry struct {
	Cluster     string    `json:"cluster"`
	NodePool    string    `json:"nodepool"`
	NodeCount   int       `json:"node_count"`
	VMSize      string    `json:"vm_size,omitempty"`
	OSSku       string    `json:"os_sku,omitempty"`
	Mode        string    `json:"mode,omitempty"`         // System | User
	DiskSizeGB  int       `json:"disk_size_gb,omitempty"` // tamanho real do disco de boot/OS — hoje só populado pra GKE (via Container API, node K8s não expõe isso como label)
	DiskType    string    `json:"disk_type,omitempty"`    // ex: GKE "pd-balanced"/"pd-standard"/"pd-ssd"
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
	if err := migrateNodePoolRegistryDiskColumns(db); err != nil {
		return nil, fmt.Errorf("migrar colunas de disco em nodepool_registry: %w", err)
	}
	return &NodePoolRegistryStore{db: db}, nil
}

// migrateNodePoolRegistryDiskColumns adiciona disk_size_gb/disk_type em bancos criados antes
// dessas colunas existirem — CREATE TABLE IF NOT EXISTS não altera uma tabela já existente.
// SQLite não tem "ADD COLUMN IF NOT EXISTS", então checa via PRAGMA table_info antes de tentar.
func migrateNodePoolRegistryDiskColumns(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(nodepool_registry)`)
	if err != nil {
		return err
	}
	existing := make(map[string]bool)
	for rows.Next() {
		var cid int
		var name, colType string
		var notNull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dflt, &pk); err != nil {
			rows.Close()
			return err
		}
		existing[name] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	if !existing["disk_size_gb"] {
		if _, err := db.Exec(`ALTER TABLE nodepool_registry ADD COLUMN disk_size_gb INTEGER`); err != nil {
			return err
		}
	}
	if !existing["disk_type"] {
		if _, err := db.Exec(`ALTER TABLE nodepool_registry ADD COLUMN disk_type TEXT`); err != nil {
			return err
		}
	}
	return nil
}

// Upsert insere ou atualiza um entry do registry.
func (s *NodePoolRegistryStore) Upsert(e NodePoolRegistryEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`
INSERT INTO nodepool_registry (cluster, nodepool, node_count, vm_size, os_sku, mode, disk_size_gb, disk_type, last_scanned)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(cluster, nodepool) DO UPDATE SET
    node_count   = excluded.node_count,
    vm_size      = excluded.vm_size,
    os_sku       = excluded.os_sku,
    mode         = excluded.mode,
    disk_size_gb = excluded.disk_size_gb,
    disk_type    = excluded.disk_type,
    last_scanned = excluded.last_scanned`,
		e.Cluster, e.NodePool, e.NodeCount, e.VMSize, e.OSSku, e.Mode, e.DiskSizeGB, e.DiskType, e.LastScanned)
	return err
}

// GetAll retorna todos os entries (opcionalmente filtrado por cluster).
// Se o cluster informado incluir o sufixo -admin mas o registry tiver a entrada sem ele
// (ou vice-versa), aplica fallback automático para compatibilidade entre máquinas
// onde os contextos do kubeconfig têm ou não o sufixo -admin.
func (s *NodePoolRegistryStore) GetAll(cluster string) ([]NodePoolRegistryEntry, error) {
	entries, err := s.queryAll(cluster)
	if err != nil || len(entries) > 0 || cluster == "" {
		return entries, err
	}
	// Fallback: tentar sem sufixo -admin
	if without := strings.TrimSuffix(cluster, "-admin"); without != cluster {
		return s.queryAll(without)
	}
	return entries, err
}

// queryAll executa a query de listagem por cluster sem fallback.
func (s *NodePoolRegistryStore) queryAll(cluster string) ([]NodePoolRegistryEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `SELECT cluster, nodepool, node_count, vm_size, os_sku, mode, disk_size_gb, disk_type, last_scanned FROM nodepool_registry`
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

	entries, err := scanNodePoolRegistryRows(rows)
	if err != nil {
		return nil, err
	}
	return entries, rows.Err()
}

// LookupByNodePool retorna todos os entries onde o nome do nodepool coincide.
// Usado para correlacionar "aks-<nodepool>-vmss*" com clusters reais.
func (s *NodePoolRegistryStore) LookupByNodePool(nodepoolName string) ([]NodePoolRegistryEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(
		`SELECT cluster, nodepool, node_count, vm_size, os_sku, mode, disk_size_gb, disk_type, last_scanned
		 FROM nodepool_registry WHERE nodepool = ? ORDER BY cluster`,
		nodepoolName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	entries, err := scanNodePoolRegistryRows(rows)
	if err != nil {
		return nil, err
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
		`SELECT cluster, nodepool, node_count, vm_size, os_sku, mode, disk_size_gb, disk_type, last_scanned
		 FROM nodepool_registry WHERE nodepool LIKE ? ORDER BY cluster`,
		"%"+keyword+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	entries, err := scanNodePoolRegistryRows(rows)
	if err != nil {
		return nil, err
	}
	return entries, rows.Err()
}

// scanNodePoolRegistryRows lê todas as linhas de um *sql.Rows já posicionado numa das 3 queries
// acima (mesmo shape de colunas) — evita repetir o mesmo loop de Scan 3 vezes.
func scanNodePoolRegistryRows(rows *sql.Rows) ([]NodePoolRegistryEntry, error) {
	var entries []NodePoolRegistryEntry
	for rows.Next() {
		var e NodePoolRegistryEntry
		var vmSize, osSku, mode, diskType sql.NullString
		var diskSizeGB sql.NullInt64
		if err := rows.Scan(&e.Cluster, &e.NodePool, &e.NodeCount, &vmSize, &osSku, &mode, &diskSizeGB, &diskType, &e.LastScanned); err != nil {
			return nil, err
		}
		e.VMSize = vmSize.String
		e.OSSku = osSku.String
		e.Mode = mode.String
		e.DiskSizeGB = int(diskSizeGB.Int64)
		e.DiskType = diskType.String
		entries = append(entries, e)
	}
	if entries == nil {
		entries = make([]NodePoolRegistryEntry, 0)
	}
	return entries, nil
}

// DeleteByCluster remove todos os entries de um cluster (antes de re-scan).
func (s *NodePoolRegistryStore) DeleteByCluster(cluster string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`DELETE FROM nodepool_registry WHERE cluster = ?`, cluster)
	return err
}
