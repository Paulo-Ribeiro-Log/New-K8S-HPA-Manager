// Package storage fornece persistência para o registry de dependências externas
package storage

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	_ "github.com/mattn/go-sqlite3"
)

// DependencyRecord representa uma dependência externa persistida
type DependencyRecord struct {
	ID          int       `json:"id"`
	ServiceName string    `json:"service_name"` // ex: rdsh-regional01.dc.nova
	ServiceType string    `json:"service_type"` // ex: rds, kafka, eventhub
	Cluster     string    `json:"cluster"`
	Namespace   string    `json:"namespace"`
	Deployment  string    `json:"deployment"`   // Nome do deployment (se encontrado via env var)
	SourceType  string    `json:"source_type"`  // configmap, secret, env
	SourceName  string    `json:"source_name"`  // nome do configmap/secret/container
	SourceKey   string    `json:"source_key"`   // chave onde foi encontrado
	FirstSeen   time.Time `json:"first_seen"`
	LastSeen    time.Time `json:"last_seen"`
}

// DependencyRegistry gerencia o banco de dados de dependências
type DependencyRegistry struct {
	db *sql.DB
}

// NewDependencyRegistry cria uma nova instância do registry
func NewDependencyRegistry() (*DependencyRegistry, error) {
	// Determinar caminho do banco de dados
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("erro ao obter home directory: %w", err)
	}

	dbDir := filepath.Join(homeDir, ".k8s-hpa-manager")
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		return nil, fmt.Errorf("erro ao criar diretório: %w", err)
	}

	dbPath := filepath.Join(dbDir, "dependency-registry.db")

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("erro ao abrir banco de dados: %w", err)
	}

	registry := &DependencyRegistry{db: db}
	if err := registry.initSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("erro ao inicializar schema: %w", err)
	}

	log.Info().Str("path", dbPath).Msg("Dependency registry initialized")
	return registry, nil
}

// initSchema cria as tabelas necessárias
func (r *DependencyRegistry) initSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS dependencies (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		service_name TEXT NOT NULL,
		service_type TEXT NOT NULL,
		cluster TEXT NOT NULL,
		namespace TEXT NOT NULL,
		deployment TEXT DEFAULT '',
		source_type TEXT NOT NULL,
		source_name TEXT NOT NULL,
		source_key TEXT DEFAULT '',
		first_seen TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		last_seen TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(cluster, namespace, service_name, source_type, source_name, source_key)
	);

	CREATE INDEX IF NOT EXISTS idx_dependencies_service_name ON dependencies(service_name);
	CREATE INDEX IF NOT EXISTS idx_dependencies_service_type ON dependencies(service_type);
	CREATE INDEX IF NOT EXISTS idx_dependencies_cluster ON dependencies(cluster);
	CREATE INDEX IF NOT EXISTS idx_dependencies_namespace ON dependencies(namespace);
	CREATE INDEX IF NOT EXISTS idx_dependencies_last_seen ON dependencies(last_seen);

	CREATE TABLE IF NOT EXISTS scan_history (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		cluster TEXT NOT NULL,
		namespaces_scanned INTEGER DEFAULT 0,
		dependencies_found INTEGER DEFAULT 0,
		dependencies_saved INTEGER DEFAULT 0,
		duration_ms INTEGER DEFAULT 0,
		scanned_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_scan_history_cluster ON scan_history(cluster);
	CREATE INDEX IF NOT EXISTS idx_scan_history_scanned_at ON scan_history(scanned_at);
	`

	_, err := r.db.Exec(schema)
	return err
}

// UpsertDependency insere ou atualiza uma dependência
func (r *DependencyRegistry) UpsertDependency(dep *DependencyRecord) error {
	query := `
	INSERT INTO dependencies (service_name, service_type, cluster, namespace, deployment, source_type, source_name, source_key, first_seen, last_seen)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	ON CONFLICT(cluster, namespace, service_name, source_type, source_name, source_key)
	DO UPDATE SET
		deployment = excluded.deployment,
		last_seen = CURRENT_TIMESTAMP
	`

	_, err := r.db.Exec(query,
		dep.ServiceName,
		dep.ServiceType,
		dep.Cluster,
		dep.Namespace,
		dep.Deployment,
		dep.SourceType,
		dep.SourceName,
		dep.SourceKey,
	)

	return err
}

// SearchByServiceName busca dependências por nome do serviço, tipo, source ou deployment (busca parcial)
// Suporta wildcards: * ou % (ex: "rds*", "rds%", "*kafka*")
func (r *DependencyRegistry) SearchByServiceName(query string) ([]DependencyRecord, error) {
	lower := strings.ToLower(query)

	// Suporte a wildcards: * ou % do usuário
	var pattern string
	if strings.ContainsAny(lower, "*%") {
		// Usuário forneceu wildcard explícito: converter * para % do SQLite
		pattern = strings.ReplaceAll(lower, "*", "%")
	} else {
		// Busca padrão: contém o termo em qualquer posição
		pattern = "%" + lower + "%"
	}
	sqlQuery := `
	SELECT id, service_name, service_type, cluster, namespace, deployment, source_type, source_name, source_key, first_seen, last_seen
	FROM dependencies
	WHERE service_name LIKE ? OR service_type LIKE ? OR source_name LIKE ? OR source_key LIKE ? OR deployment LIKE ? OR namespace LIKE ?
	ORDER BY service_name, cluster, namespace
	`

	rows, err := r.db.Query(sqlQuery, pattern, pattern, pattern, pattern, pattern, pattern)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return r.scanRows(rows)
}

// GetAll retorna todas as dependências com filtros opcionais
func (r *DependencyRegistry) GetAll(cluster, namespace, serviceType string) ([]DependencyRecord, error) {
	query := `
	SELECT id, service_name, service_type, cluster, namespace, deployment, source_type, source_name, source_key, first_seen, last_seen
	FROM dependencies
	WHERE 1=1
	`
	args := []interface{}{}

	if cluster != "" {
		query += " AND cluster = ?"
		args = append(args, cluster)
	}
	if namespace != "" {
		query += " AND namespace = ?"
		args = append(args, namespace)
	}
	if serviceType != "" {
		query += " AND service_type = ?"
		args = append(args, serviceType)
	}

	query += " ORDER BY service_name, cluster, namespace"

	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return r.scanRows(rows)
}

// GetUniqueServices retorna lista de serviços únicos
func (r *DependencyRegistry) GetUniqueServices() ([]string, error) {
	query := `SELECT DISTINCT service_name FROM dependencies ORDER BY service_name`

	rows, err := r.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var services []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		services = append(services, name)
	}

	return services, nil
}

// GetUniqueClusters retorna lista de clusters únicos
func (r *DependencyRegistry) GetUniqueClusters() ([]string, error) {
	query := `SELECT DISTINCT cluster FROM dependencies ORDER BY cluster`

	rows, err := r.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var clusters []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		clusters = append(clusters, name)
	}

	return clusters, nil
}

// GetServiceUsage retorna onde um serviço específico é usado
func (r *DependencyRegistry) GetServiceUsage(serviceName string) ([]DependencyRecord, error) {
	query := `
	SELECT id, service_name, service_type, cluster, namespace, deployment, source_type, source_name, source_key, first_seen, last_seen
	FROM dependencies
	WHERE LOWER(service_name) = LOWER(?)
	ORDER BY cluster, namespace
	`

	rows, err := r.db.Query(query, serviceName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return r.scanRows(rows)
}

// GetStats retorna estatísticas do registry
func (r *DependencyRegistry) GetStats() (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	// Total de dependências
	var total int
	if err := r.db.QueryRow("SELECT COUNT(*) FROM dependencies").Scan(&total); err != nil {
		return nil, err
	}
	stats["total_dependencies"] = total

	// Serviços únicos
	var uniqueServices int
	if err := r.db.QueryRow("SELECT COUNT(DISTINCT service_name) FROM dependencies").Scan(&uniqueServices); err != nil {
		return nil, err
	}
	stats["unique_services"] = uniqueServices

	// Clusters únicos
	var uniqueClusters int
	if err := r.db.QueryRow("SELECT COUNT(DISTINCT cluster) FROM dependencies").Scan(&uniqueClusters); err != nil {
		return nil, err
	}
	stats["unique_clusters"] = uniqueClusters

	// Namespaces únicos
	var uniqueNamespaces int
	if err := r.db.QueryRow("SELECT COUNT(DISTINCT namespace) FROM dependencies").Scan(&uniqueNamespaces); err != nil {
		return nil, err
	}
	stats["unique_namespaces"] = uniqueNamespaces

	// Por tipo
	byType := make(map[string]int)
	rows, err := r.db.Query("SELECT service_type, COUNT(*) FROM dependencies GROUP BY service_type")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var serviceType string
		var count int
		if err := rows.Scan(&serviceType, &count); err != nil {
			return nil, err
		}
		byType[serviceType] = count
	}
	stats["by_type"] = byType

	// Último scan
	var lastScanStr sql.NullString
	if err := r.db.QueryRow("SELECT MAX(scanned_at) FROM scan_history").Scan(&lastScanStr); err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	if lastScanStr.Valid && lastScanStr.String != "" {
		// Parse timestamp string do SQLite
		if t, err := time.Parse("2006-01-02 15:04:05", lastScanStr.String); err == nil {
			stats["last_scan"] = t
		} else if t, err := time.Parse(time.RFC3339, lastScanStr.String); err == nil {
			stats["last_scan"] = t
		}
	}

	return stats, nil
}

// RecordScan registra uma execução de scan
func (r *DependencyRegistry) RecordScan(cluster string, namespacesScanned, depsFound, depsSaved int, durationMs int64) error {
	query := `
	INSERT INTO scan_history (cluster, namespaces_scanned, dependencies_found, dependencies_saved, duration_ms)
	VALUES (?, ?, ?, ?, ?)
	`

	_, err := r.db.Exec(query, cluster, namespacesScanned, depsFound, depsSaved, durationMs)
	return err
}

// GetScanHistory retorna histórico de scans
func (r *DependencyRegistry) GetScanHistory(limit int) ([]map[string]interface{}, error) {
	query := `
	SELECT cluster, namespaces_scanned, dependencies_found, dependencies_saved, duration_ms, scanned_at
	FROM scan_history
	ORDER BY scanned_at DESC
	LIMIT ?
	`

	rows, err := r.db.Query(query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var history []map[string]interface{}
	for rows.Next() {
		var cluster string
		var nsScanned, depsFound, depsSaved int
		var durationMs int64
		var scannedAt time.Time

		if err := rows.Scan(&cluster, &nsScanned, &depsFound, &depsSaved, &durationMs, &scannedAt); err != nil {
			return nil, err
		}

		history = append(history, map[string]interface{}{
			"cluster":             cluster,
			"namespaces_scanned":  nsScanned,
			"dependencies_found":  depsFound,
			"dependencies_saved":  depsSaved,
			"duration_ms":         durationMs,
			"scanned_at":          scannedAt,
		})
	}

	return history, nil
}

// CleanOldRecords remove dependências não vistas há mais de X dias
func (r *DependencyRegistry) CleanOldRecords(days int) (int64, error) {
	query := `DELETE FROM dependencies WHERE last_seen < datetime('now', ?)`
	result, err := r.db.Exec(query, fmt.Sprintf("-%d days", days))
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// Close fecha a conexão com o banco
func (r *DependencyRegistry) Close() error {
	return r.db.Close()
}

// scanRows converte rows em slice de DependencyRecord
func (r *DependencyRegistry) scanRows(rows *sql.Rows) ([]DependencyRecord, error) {
	var deps []DependencyRecord

	for rows.Next() {
		var dep DependencyRecord
		var firstSeen, lastSeen string

		err := rows.Scan(
			&dep.ID,
			&dep.ServiceName,
			&dep.ServiceType,
			&dep.Cluster,
			&dep.Namespace,
			&dep.Deployment,
			&dep.SourceType,
			&dep.SourceName,
			&dep.SourceKey,
			&firstSeen,
			&lastSeen,
		)
		if err != nil {
			return nil, err
		}

		// Parse timestamps
		if t, err := time.Parse("2006-01-02 15:04:05", firstSeen); err == nil {
			dep.FirstSeen = t
		} else if t, err := time.Parse(time.RFC3339, firstSeen); err == nil {
			dep.FirstSeen = t
		}

		if t, err := time.Parse("2006-01-02 15:04:05", lastSeen); err == nil {
			dep.LastSeen = t
		} else if t, err := time.Parse(time.RFC3339, lastSeen); err == nil {
			dep.LastSeen = t
		}

		deps = append(deps, dep)
	}

	return deps, nil
}
