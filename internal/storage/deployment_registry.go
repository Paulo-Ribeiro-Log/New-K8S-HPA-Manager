package storage

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// DeploymentRecord representa um registro de deployment na base de conhecimento
type DeploymentRecord struct {
	ID              int       `json:"id"`
	DeploymentName  string    `json:"deployment_name"`
	Namespace       string    `json:"namespace"`
	Cluster         string    `json:"cluster"`
	Version         string    `json:"version"`          // app.kubernetes.io/version
	ImageTag        string    `json:"image_tag"`        // Extraído da imagem
	FullImage       string    `json:"full_image"`       // Imagem completa
	ReplicasCurrent int       `json:"replicas_current"` // Réplicas atuais
	ReplicasDesired int       `json:"replicas_desired"` // Réplicas desejadas
	AppName         string    `json:"app_name"`         // app.kubernetes.io/name
	Status          string    `json:"status"`           // healthy, warning, critical
	Squad           string    `json:"squad"`            // devops.k8s.io/squad
	ServiceNowTask  string    `json:"servicenow_task"`  // devops.k8s.io/servicenow-task-number (CHG prefix)
	CreatedAt       time.Time `json:"created_at"`       // ✅ Data de criação real do deployment (metadata.creationTimestamp)
	FirstSeen       time.Time `json:"first_seen"`
	LastSeen        time.Time `json:"last_seen"`
	LastHealthCheck time.Time `json:"last_health_check"`
}

// DeploymentRegistry gerencia a base de conhecimento de deployments
type DeploymentRegistry struct {
	db *sql.DB
}

// NewDeploymentRegistry cria nova instância do registry
func NewDeploymentRegistry() (*DeploymentRegistry, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	// Criar diretório .k8s-hpa-manager se não existir
	configDir := filepath.Join(homeDir, ".k8s-hpa-manager")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create config directory: %w", err)
	}

	// Abrir/criar banco SQLite
	dbPath := filepath.Join(configDir, "deployment-registry.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	registry := &DeploymentRegistry{db: db}

	// Criar schema
	if err := registry.createSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create schema: %w", err)
	}

	return registry, nil
}

// createSchema cria as tabelas necessárias
func (r *DeploymentRegistry) createSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS deployments (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		deployment_name TEXT NOT NULL,
		namespace TEXT NOT NULL,
		cluster TEXT NOT NULL,
		version TEXT,
		image_tag TEXT,
		full_image TEXT,
		replicas_current INTEGER DEFAULT 0,
		replicas_desired INTEGER DEFAULT 0,
		app_name TEXT,
		status TEXT,
		squad TEXT,
		servicenow_task TEXT,
		created_at TIMESTAMP,
		first_seen TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		last_seen TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		last_health_check TIMESTAMP,
		UNIQUE(cluster, namespace, deployment_name)
	);

	CREATE INDEX IF NOT EXISTS idx_app_name ON deployments(app_name);
	CREATE INDEX IF NOT EXISTS idx_version ON deployments(version);
	CREATE INDEX IF NOT EXISTS idx_cluster ON deployments(cluster);
	CREATE INDEX IF NOT EXISTS idx_created_at ON deployments(created_at);
	CREATE INDEX IF NOT EXISTS idx_last_seen ON deployments(last_seen);
	CREATE INDEX IF NOT EXISTS idx_squad ON deployments(squad);
	CREATE INDEX IF NOT EXISTS idx_servicenow_task ON deployments(servicenow_task);

	CREATE TABLE IF NOT EXISTS version_history (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		deployment_id INTEGER,
		old_version TEXT,
		new_version TEXT,
		changed_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (deployment_id) REFERENCES deployments(id)
	);
	`

	if _, err := r.db.Exec(schema); err != nil {
		return err
	}

	// ✅ Migração: Adicionar coluna created_at se não existir (para bancos existentes)
	migration := `
	ALTER TABLE deployments ADD COLUMN created_at TIMESTAMP;
	`
	// Ignorar erro se coluna já existir
	r.db.Exec(migration)

	return nil
}

// UpsertDeployment insere ou atualiza um registro de deployment
func (r *DeploymentRegistry) UpsertDeployment(record DeploymentRecord) error {
	now := time.Now()

	// Buscar versão anterior (se existir) para rastrear mudanças
	var oldVersion string
	var deploymentID int
	queryOld := `SELECT id, version FROM deployments WHERE cluster = ? AND namespace = ? AND deployment_name = ?`
	r.db.QueryRow(queryOld, record.Cluster, record.Namespace, record.DeploymentName).Scan(&deploymentID, &oldVersion)

	// Insert ou Update
	query := `
	INSERT INTO deployments (
		deployment_name, namespace, cluster, version, image_tag, full_image,
		replicas_current, replicas_desired, app_name, status, squad, servicenow_task,
		created_at, last_seen, last_health_check
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(cluster, namespace, deployment_name) DO UPDATE SET
		version = excluded.version,
		image_tag = excluded.image_tag,
		full_image = excluded.full_image,
		replicas_current = excluded.replicas_current,
		replicas_desired = excluded.replicas_desired,
		app_name = excluded.app_name,
		status = excluded.status,
		squad = excluded.squad,
		servicenow_task = excluded.servicenow_task,
		last_seen = excluded.last_seen,
		last_health_check = excluded.last_health_check
		-- ⚠️ NÃO atualizar created_at - manter data original de criação
	`

	_, err := r.db.Exec(query,
		record.DeploymentName, record.Namespace, record.Cluster,
		record.Version, record.ImageTag, record.FullImage,
		record.ReplicasCurrent, record.ReplicasDesired, record.AppName,
		record.Status, record.Squad, record.ServiceNowTask,
		record.CreatedAt, now, now,
	)

	if err != nil {
		return fmt.Errorf("failed to upsert deployment: %w", err)
	}

	// Rastrear mudança de versão
	if oldVersion != "" && record.Version != "" && oldVersion != record.Version {
		r.trackVersionChange(deploymentID, oldVersion, record.Version)
	}

	return nil
}

// trackVersionChange registra mudança de versão no histórico
func (r *DeploymentRegistry) trackVersionChange(deploymentID int, oldVersion, newVersion string) error {
	query := `INSERT INTO version_history (deployment_id, old_version, new_version) VALUES (?, ?, ?)`
	_, err := r.db.Exec(query, deploymentID, oldVersion, newVersion)
	return err
}

// SearchByAppName busca deployments por nome da aplicação
func (r *DeploymentRegistry) SearchByAppName(appName string) ([]DeploymentRecord, error) {
	query := `
	SELECT id, deployment_name, namespace, cluster, version, image_tag, full_image,
	       replicas_current, replicas_desired, app_name, status, squad, servicenow_task,
	       created_at, first_seen, last_seen, last_health_check
	FROM deployments
	WHERE app_name = ? OR deployment_name LIKE ? OR deployment_name = ?
	ORDER BY last_seen DESC
	`

	rows, err := r.db.Query(query, appName, "%"+appName+"%", appName)
	if err != nil {
		return nil, fmt.Errorf("failed to search deployments: %w", err)
	}
	defer rows.Close()

	var records []DeploymentRecord
	for rows.Next() {
		var r DeploymentRecord
		var createdAt, firstSeen, lastSeen, lastHealthCheck sql.NullTime

		err := rows.Scan(
			&r.ID, &r.DeploymentName, &r.Namespace, &r.Cluster,
			&r.Version, &r.ImageTag, &r.FullImage,
			&r.ReplicasCurrent, &r.ReplicasDesired, &r.AppName,
			&r.Status, &r.Squad, &r.ServiceNowTask,
			&createdAt, &firstSeen, &lastSeen, &lastHealthCheck,
		)

		if err != nil {
			continue
		}

		if createdAt.Valid {
			r.CreatedAt = createdAt.Time
		}
		if firstSeen.Valid {
			r.FirstSeen = firstSeen.Time
		}
		if lastSeen.Valid {
			r.LastSeen = lastSeen.Time
		}
		if lastHealthCheck.Valid {
			r.LastHealthCheck = lastHealthCheck.Time
		}

		records = append(records, r)
	}

	return records, nil
}

// GetProductionVersion busca a versão mais provável em produção
// Prioriza: 1) Match exato de deployment_name/app_name, 2) LIKE como fallback
// Prioriza clusters com nome contendo "prod"
func (r *DeploymentRegistry) GetProductionVersion(appName string) (*DeploymentRecord, error) {
	query := `
	SELECT id, deployment_name, namespace, cluster, version, image_tag, full_image,
	       replicas_current, replicas_desired, app_name, status, squad, servicenow_task,
	       created_at, first_seen, last_seen, last_health_check
	FROM deployments
	WHERE (app_name = ? OR deployment_name LIKE ? OR deployment_name = ?)
	  AND (cluster LIKE '%prod%' OR cluster LIKE '%prd%')
	ORDER BY
	  CASE
	    WHEN deployment_name = ? OR app_name = ? THEN 0
	    ELSE 1
	  END,
	  last_seen DESC
	LIMIT 1
	`

	var record DeploymentRecord
	var createdAt, firstSeen, lastSeen, lastHealthCheck sql.NullTime

	// Parâmetros: WHERE(app_name=, LIKE, deployment_name=), ORDER BY CASE(deployment_name=, app_name=)
	err := r.db.QueryRow(query, appName, "%"+appName+"%", appName, appName, appName).Scan(
		&record.ID, &record.DeploymentName, &record.Namespace, &record.Cluster,
		&record.Version, &record.ImageTag, &record.FullImage,
		&record.ReplicasCurrent, &record.ReplicasDesired, &record.AppName,
		&record.Status, &record.Squad, &record.ServiceNowTask,
		&createdAt, &firstSeen, &lastSeen, &lastHealthCheck,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("deployment não encontrado em clusters de produção")
	}

	if err != nil {
		return nil, fmt.Errorf("failed to get production version: %w", err)
	}

	if createdAt.Valid {
		record.CreatedAt = createdAt.Time
	}
	if firstSeen.Valid {
		record.FirstSeen = firstSeen.Time
	}
	if lastSeen.Valid {
		record.LastSeen = lastSeen.Time
	}
	if lastHealthCheck.Valid {
		record.LastHealthCheck = lastHealthCheck.Time
	}

	return &record, nil
}

// GetAllVersions retorna todas as versões de um app agrupadas
func (r *DeploymentRegistry) GetAllVersions(appName string) (map[string][]DeploymentRecord, error) {
	records, err := r.SearchByAppName(appName)
	if err != nil {
		return nil, err
	}

	// Agrupar por versão
	versionMap := make(map[string][]DeploymentRecord)
	for _, rec := range records {
		version := rec.Version
		if version == "" {
			version = rec.ImageTag
		}
		if version == "" {
			version = "unknown"
		}

		versionMap[version] = append(versionMap[version], rec)
	}

	return versionMap, nil
}

// CleanOldRecords remove registros não vistos há mais de X dias
func (r *DeploymentRegistry) CleanOldRecords(daysOld int) (int, error) {
	cutoffDate := time.Now().AddDate(0, 0, -daysOld)

	result, err := r.db.Exec(`
		DELETE FROM deployments
		WHERE last_seen < ?
	`, cutoffDate)

	if err != nil {
		return 0, fmt.Errorf("failed to clean old records: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	return int(rowsAffected), nil
}

// GetStats retorna estatísticas da base de conhecimento
func (r *DeploymentRegistry) GetStats() (map[string]interface{}, error) {
	var totalDeployments, totalClusters, totalNamespaces int

	r.db.QueryRow(`SELECT COUNT(*) FROM deployments`).Scan(&totalDeployments)
	r.db.QueryRow(`SELECT COUNT(DISTINCT cluster) FROM deployments`).Scan(&totalClusters)
	r.db.QueryRow(`SELECT COUNT(DISTINCT namespace) FROM deployments`).Scan(&totalNamespaces)

	var oldestSeen, newestSeen sql.NullTime
	r.db.QueryRow(`SELECT MIN(first_seen), MAX(last_seen) FROM deployments`).Scan(&oldestSeen, &newestSeen)

	stats := map[string]interface{}{
		"total_deployments": totalDeployments,
		"total_clusters":    totalClusters,
		"total_namespaces":  totalNamespaces,
	}

	if oldestSeen.Valid {
		stats["oldest_record"] = oldestSeen.Time
	}
	if newestSeen.Valid {
		stats["newest_record"] = newestSeen.Time
	}

	return stats, nil
}

// GetAll retorna todos os deployments do registry com filtros opcionais
func (r *DeploymentRegistry) GetAll(cluster, namespace string, onlyValidVersions bool) ([]DeploymentRecord, error) {
	query := `
	SELECT id, deployment_name, namespace, cluster, version, image_tag, full_image,
	       replicas_current, replicas_desired, app_name, status, squad, servicenow_task,
	       first_seen, last_seen, last_health_check
	FROM deployments
	WHERE 1=1
	`

	args := []interface{}{}

	// Filtro por cluster
	if cluster != "" {
		query += " AND cluster = ?"
		args = append(args, cluster)
	}

	// Filtro por namespace
	if namespace != "" {
		query += " AND namespace = ?"
		args = append(args, namespace)
	}

	// Filtro por versões válidas (padrão semver x.x.x ou x.x.x-x)
	if onlyValidVersions {
		// Regex SQLite para validar padrão: dígitos.dígitos.dígitos ou dígitos.dígitos.dígitos-dígitos
		query += ` AND (
			version GLOB '[0-9]*.[0-9]*.[0-9]*' OR
			version GLOB '[0-9]*.[0-9]*.[0-9]*-[0-9]*'
		)`
	}

	query += " ORDER BY last_seen DESC"

	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to get all deployments: %w", err)
	}
	defer rows.Close()

	var records []DeploymentRecord
	for rows.Next() {
		var r DeploymentRecord
		var firstSeen, lastSeen, lastHealthCheck sql.NullTime

		err := rows.Scan(
			&r.ID, &r.DeploymentName, &r.Namespace, &r.Cluster,
			&r.Version, &r.ImageTag, &r.FullImage,
			&r.ReplicasCurrent, &r.ReplicasDesired, &r.AppName,
			&r.Status, &r.Squad, &r.ServiceNowTask,
			&firstSeen, &lastSeen, &lastHealthCheck,
		)

		if err != nil {
			continue
		}

		if firstSeen.Valid {
			r.FirstSeen = firstSeen.Time
		}
		if lastSeen.Valid {
			r.LastSeen = lastSeen.Time
		}
		if lastHealthCheck.Valid {
			r.LastHealthCheck = lastHealthCheck.Time
		}

		records = append(records, r)
	}

	return records, nil
}

// Close fecha a conexão com o banco
func (r *DeploymentRegistry) Close() error {
	return r.db.Close()
}

// ExtractVersionFromImage extrai a tag de versão de uma imagem Docker
// Exemplo: gcr.io/project/app:2.5.5-2 → 2.5.5-2
func ExtractVersionFromImage(image string) string {
	parts := strings.Split(image, ":")
	if len(parts) > 1 {
		return parts[len(parts)-1] // Última parte após ":"
	}
	return ""
}
