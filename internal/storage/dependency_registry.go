// Package storage fornece persistência para o registry de dependências externas
package storage

import (
	"database/sql"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/rs/zerolog/log"
)

// DependencyRecord representa uma dependência externa persistida
type DependencyRecord struct {
	ID          int       `json:"id"`
	ServiceName string    `json:"service_name"` // ex: rdsh-regional01.dc.nova
	ServiceType string    `json:"service_type"` // ex: rds, kafka, eventhub
	TopicName   string    `json:"topic_name"`   // ex: pedidos-criados, events-hub (para Kafka/EventHub)
	Cluster     string    `json:"cluster"`
	Namespace   string    `json:"namespace"`
	Deployment  string    `json:"deployment"`  // Nome do deployment (se encontrado via env var)
	SourceType  string    `json:"source_type"` // configmap, secret, env
	SourceName  string    `json:"source_name"` // nome do configmap/secret/container
	SourceKey   string    `json:"source_key"`  // chave onde foi encontrado
	FirstSeen   time.Time `json:"first_seen"`
	LastSeen    time.Time `json:"last_seen"`
}

// SecretDataRecord representa uma entrada de chave/valor de um Secret OU ConfigMap indexada para
// busca (ResourceKind distingue os dois — mesma convenção de source_type/source_name já usada em
// DependencyRecord). Ao contrário de DependencyRecord (que só guarda nomes de host extraídos por
// regex), aqui o CONTEÚDO do recurso é persistido (chave, valor decodificado e valor base64) — ver
// nota de segurança em SECRETS-DATA-SEARCH no CLAUDE.md/plano: única persistência de conteúdo bruto
// de Secret em toda a aplicação (ConfigMaps não são sensíveis por padrão, mas entram na mesma
// tabela/mesma proteção por simplicidade e porque já é comum guardar configuração "quase secreta"
// em ConfigMap por engano), feita de forma deliberada e com escopo restrito (busca-only, sem
// endpoint de "listar tudo", snapshot por cluster em vez de histórico crescente, botão de limpeza
// dedicado).
type SecretDataRecord struct {
	ID              int       `json:"id"`
	ResourceKind    string    `json:"resource_kind"` // "secret" ou "configmap"
	Cluster         string    `json:"cluster"`
	Namespace       string    `json:"namespace"`
	ResourceName    string    `json:"resource_name"`    // nome do Secret ou ConfigMap
	ResourceSubtype string    `json:"resource_subtype"` // Type do Secret (Opaque, kubernetes.io/tls...); vazio para ConfigMap
	DataKey         string    `json:"data_key"`
	ValueBase64     string    `json:"value_base64"`
	ValueDecoded    string    `json:"value_decoded"` // vazio quando IsBinary
	IsBinary        bool      `json:"is_binary"`
	Truncated       bool      `json:"truncated"`
	LastSeen        time.Time `json:"last_seen"`
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

// hasLegacySecretDataSchema detecta se secret_data_entries existe com o schema anterior à
// generalização pra ConfigMaps (coluna secret_name TEXT NOT NULL sem DEFAULT). Retorna false tanto
// quando a tabela não existe (instalação nova) quanto quando já foi recriada com o schema atual.
func (r *DependencyRegistry) hasLegacySecretDataSchema() bool {
	rows, err := r.db.Query(`PRAGMA table_info(secret_data_entries)`)
	if err != nil {
		return false
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			continue
		}
		if name == "secret_name" {
			return true
		}
	}
	return false
}

// initSchema cria as tabelas necessárias
func (r *DependencyRegistry) initSchema() error {
	// Passo 1: criar tabelas base (sem topic_name para compatibilidade)
	base := `
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
		last_seen TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS scan_history (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		cluster TEXT NOT NULL,
		namespaces_scanned INTEGER DEFAULT 0,
		dependencies_found INTEGER DEFAULT 0,
		dependencies_saved INTEGER DEFAULT 0,
		duration_ms INTEGER DEFAULT 0,
		scanned_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	`
	if _, err := r.db.Exec(base); err != nil {
		return err
	}

	// Passo 2: migrações — adiciona colunas se não existirem (ignora erro se já existir)
	r.db.Exec(`ALTER TABLE dependencies ADD COLUMN topic_name TEXT DEFAULT ''`)

	// Passo 3: criar índices (após garantir que as colunas existem)
	indexes := `
	CREATE INDEX IF NOT EXISTS idx_dependencies_service_name ON dependencies(service_name);
	CREATE INDEX IF NOT EXISTS idx_dependencies_service_type ON dependencies(service_type);
	CREATE INDEX IF NOT EXISTS idx_dependencies_topic_name ON dependencies(topic_name);
	CREATE INDEX IF NOT EXISTS idx_dependencies_cluster ON dependencies(cluster);
	CREATE INDEX IF NOT EXISTS idx_dependencies_namespace ON dependencies(namespace);
	CREATE INDEX IF NOT EXISTS idx_dependencies_last_seen ON dependencies(last_seen);
	CREATE INDEX IF NOT EXISTS idx_scan_history_cluster ON scan_history(cluster);
	CREATE INDEX IF NOT EXISTS idx_scan_history_scanned_at ON scan_history(scanned_at);
	`
	if _, err := r.db.Exec(indexes); err != nil {
		return err
	}

	// Passo 4: garantir constraint UNIQUE via índice único (SQLite não suporta ADD CONSTRAINT)
	r.db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_dependencies_unique ON dependencies(cluster, namespace, service_name, source_type, source_name, source_key, topic_name)`)

	// Passo 5: tabela de índice de conteúdo de Secrets + ConfigMaps (chave/valor) — ver
	// SecretDataRecord. Semântica de SNAPSHOT por cluster (ReplaceSecretDataForCluster faz
	// delete-then-insert), não upsert histórico — por isso não há first_seen/last_seen com
	// significado de "desde quando existe", só last_seen = quando o scan mais recente daquele
	// cluster rodou. resource_kind ("secret"/"configmap") já nasce na definição da tabela; as
	// colunas resource_name/resource_subtype também — só existem via ALTER TABLE abaixo pra quem
	// já tinha a tabela da versão anterior.
	//
	// Bug real corrigido: bancos criados ANTES da generalização pra ConfigMaps tinham
	// `secret_name TEXT NOT NULL` (sem DEFAULT) — o INSERT novo (via ResourceName) não populava
	// mais essa coluna, então TODO scan passou a falhar com "NOT NULL constraint failed:
	// secret_data_entries.secret_name", silenciosamente (só logado como Warn em
	// DependenciesHandler.Scan/ScanCluster, sem falhar a requisição). Como
	// ReplaceSecretDataForCluster faz DELETE+INSERT numa única transação e retorna no primeiro erro,
	// o Rollback desfazia o DELETE também — a tabela ficava CONGELADA no snapshot de antes da
	// mudança de schema, dando a falsa impressão de que só Secrets "funcionavam" (sobras de um scan
	// anterior bem-sucedido) enquanto ConfigMaps apareciam vazios (nunca existiram num scan
	// anterior, já que a extração é nova). Corrigido detectando a coluna legada via PRAGMA
	// table_info e recriando a tabela do zero nesse caso — sem custo real, a tabela é sempre um
	// snapshot reconstruído no próximo scan de cada cluster.
	if r.hasLegacySecretDataSchema() {
		log.Warn().Msg("secret_data_entries com schema legado (secret_name NOT NULL) detectado — recriando tabela")
		if _, err := r.db.Exec(`DROP TABLE IF EXISTS secret_data_entries`); err != nil {
			return err
		}
	}

	secretDataTable := `
	CREATE TABLE IF NOT EXISTS secret_data_entries (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		resource_kind TEXT NOT NULL DEFAULT 'secret',
		cluster TEXT NOT NULL,
		namespace TEXT NOT NULL,
		resource_name TEXT NOT NULL DEFAULT '',
		resource_subtype TEXT DEFAULT '',
		data_key TEXT NOT NULL,
		value_base64 TEXT NOT NULL,
		value_decoded TEXT DEFAULT '',
		is_binary INTEGER DEFAULT 0,
		truncated INTEGER DEFAULT 0,
		last_seen TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	`
	if _, err := r.db.Exec(secretDataTable); err != nil {
		return err
	}

	// Migração: bancos criados antes da generalização pra ConfigMaps não têm essas colunas.
	r.db.Exec(`ALTER TABLE secret_data_entries ADD COLUMN resource_kind TEXT NOT NULL DEFAULT 'secret'`)
	r.db.Exec(`ALTER TABLE secret_data_entries ADD COLUMN resource_name TEXT NOT NULL DEFAULT ''`)
	r.db.Exec(`ALTER TABLE secret_data_entries ADD COLUMN resource_subtype TEXT DEFAULT ''`)

	secretDataIndexes := `
	CREATE INDEX IF NOT EXISTS idx_secret_data_cluster ON secret_data_entries(cluster);
	CREATE INDEX IF NOT EXISTS idx_secret_data_kind ON secret_data_entries(resource_kind);
	CREATE INDEX IF NOT EXISTS idx_secret_data_key ON secret_data_entries(data_key);
	CREATE INDEX IF NOT EXISTS idx_secret_data_value_decoded ON secret_data_entries(value_decoded);
	CREATE INDEX IF NOT EXISTS idx_secret_data_value_b64 ON secret_data_entries(value_base64);
	`
	if _, err := r.db.Exec(secretDataIndexes); err != nil {
		return err
	}

	return nil
}

// UpsertDependency insere ou atualiza uma dependência
func (r *DependencyRegistry) UpsertDependency(dep *DependencyRecord) error {
	query := `
	INSERT INTO dependencies (service_name, service_type, topic_name, cluster, namespace, deployment, source_type, source_name, source_key, first_seen, last_seen)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	ON CONFLICT(cluster, namespace, service_name, source_type, source_name, source_key, topic_name)
	DO UPDATE SET
		deployment = excluded.deployment,
		last_seen = CURRENT_TIMESTAMP
	`

	_, err := r.db.Exec(query,
		dep.ServiceName,
		dep.ServiceType,
		dep.TopicName,
		dep.Cluster,
		dep.Namespace,
		dep.Deployment,
		dep.SourceType,
		dep.SourceName,
		dep.SourceKey,
	)

	return err
}

// ReplaceSecretDataForCluster substitui todas as entradas de secret_data_entries de UM cluster
// pelas entries do scan mais recente (delete-then-insert transacional — semântica de snapshot,
// nunca acumula chaves/secrets já removidos do cluster, ao contrário de UpsertDependency).
func (r *DependencyRegistry) ReplaceSecretDataForCluster(cluster string, entries []SecretDataRecord) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM secret_data_entries WHERE cluster = ?`, cluster); err != nil {
		return err
	}
	if err := insertSecretDataEntries(tx, entries); err != nil {
		return err
	}

	return tx.Commit()
}

// ReplaceSecretDataForResource é o equivalente de ReplaceSecretDataForCluster, mas escopado a UM
// recurso (cluster+namespace+resource_kind+resource_name) — usado pelo refresh pontual disparado
// depois de um Resync AKV (aba Dependencies), quando não faz sentido re-escanear o cluster inteiro
// só pra atualizar uma secret. `entries` normalmente compartilham a mesma chave de escopo (mesmo
// recurso), mas isso não é validado aqui — quem chama é responsável por montar `entries`
// coerentemente com os parâmetros de escopo (ver DependencyScanner.RefreshResourceData).
func (r *DependencyRegistry) ReplaceSecretDataForResource(cluster, namespace, resourceKind, resourceName string, entries []SecretDataRecord) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(
		`DELETE FROM secret_data_entries WHERE cluster = ? AND namespace = ? AND resource_kind = ? AND resource_name = ?`,
		cluster, namespace, resourceKind, resourceName,
	); err != nil {
		return err
	}
	if err := insertSecretDataEntries(tx, entries); err != nil {
		return err
	}

	return tx.Commit()
}

// insertSecretDataEntries insere um lote de SecretDataRecord na transação já aberta pelo chamador
// (ReplaceSecretDataForCluster/ReplaceSecretDataForResource) — extraído pra evitar duplicar o
// INSERT entre as duas variantes de escopo (cluster inteiro vs. um recurso só).
func insertSecretDataEntries(tx *sql.Tx, entries []SecretDataRecord) error {
	if len(entries) == 0 {
		return nil
	}

	stmt, err := tx.Prepare(`
	INSERT INTO secret_data_entries (resource_kind, cluster, namespace, resource_name, resource_subtype, data_key, value_base64, value_decoded, is_binary, truncated, last_seen)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, e := range entries {
		if _, err := stmt.Exec(
			e.ResourceKind, e.Cluster, e.Namespace, e.ResourceName, e.ResourceSubtype, e.DataKey,
			e.ValueBase64, e.ValueDecoded, boolToInt(e.IsBinary), boolToInt(e.Truncated),
		); err != nil {
			return err
		}
	}

	return nil
}

// ClearSecretData apaga TODAS as entradas de secret_data_entries (todos os clusters) — botão
// dedicado de limpeza, separado da limpeza do registry de dependências (que não é sensível).
func (r *DependencyRegistry) ClearSecretData() error {
	_, err := r.db.Exec(`DELETE FROM secret_data_entries`)
	return err
}

// SearchSecretData busca em secret_data_entries (Secrets E ConfigMaps juntos, resource_kind
// distingue os dois no resultado) por chave OU por valor — nunca os dois ao mesmo tempo, já que a
// conversão automática para base64 só se aplica ao modo "value" (a chave nunca é base64; o VALOR
// de um ConfigMap.Data também não é base64 no manifesto — só de Secret e ConfigMap.BinaryData —,
// mas o valor_base64 é recalculado pra todas as entradas por uniformidade, então a condição de
// base64 continua funcionando igual independente da origem). mode: "key" ou "value" (default
// "value" se vazio/inválido). Suporta os mesmos coringas * / % de SearchByServiceName.
func (r *DependencyRegistry) SearchSecretData(query, mode string) ([]SecretDataRecord, error) {
	if mode != "key" {
		mode = "value"
	}

	lower := strings.ToLower(query)
	hasWildcard := strings.ContainsAny(lower, "*%")

	var pattern string
	if hasWildcard {
		pattern = strings.ReplaceAll(lower, "*", "%")
	} else {
		pattern = "%" + lower + "%"
	}

	var sqlQuery string
	var args []interface{}

	if mode == "key" {
		sqlQuery = `
		SELECT id, resource_kind, cluster, namespace, resource_name, resource_subtype, data_key, value_base64, value_decoded, is_binary, truncated, last_seen
		FROM secret_data_entries
		WHERE LOWER(data_key) LIKE ?
		ORDER BY cluster, namespace, resource_name, data_key
		LIMIT 500
		`
		args = []interface{}{pattern}
	} else {
		// Modo valor, três condições unidas com OR:
		//   1. substring no valor JÁ decodificado — sempre confiável, é quem sustenta a busca por
		//      substring geral (base64 não é substring-estável: codificar um trecho isolado
		//      raramente bate com a codificação do valor completo, por causa do alinhamento de
		//      3 bytes do base64).
		//   2. o termo, como TEXTO PURO, convertido para base64 automaticamente — atende
		//      literalmente "vai fazer o encode para base64 automaticamente" do pedido original.
		//   3. o termo, já digitado/colado EM base64 pelo usuário, comparado direto (sem
		//      recodificar) — atende o "podem ser em base64, ou convertidas antes" do pedido
		//      original: sem isso, colar um base64 já pronto (ex: "dGVzdGU=") faria um
		//      double-encode e nunca daria match.
		// Coringa desativa as condições 2 e 3 (não faz sentido codificar um padrão de wildcard).
		if hasWildcard {
			sqlQuery = `
			SELECT id, resource_kind, cluster, namespace, resource_name, resource_subtype, data_key, value_base64, value_decoded, is_binary, truncated, last_seen
			FROM secret_data_entries
			WHERE LOWER(value_decoded) LIKE ?
			ORDER BY cluster, namespace, resource_name, data_key
			LIMIT 500
			`
			args = []interface{}{pattern}
		} else {
			encoded := base64.StdEncoding.EncodeToString([]byte(query))
			sqlQuery = `
			SELECT id, resource_kind, cluster, namespace, resource_name, resource_subtype, data_key, value_base64, value_decoded, is_binary, truncated, last_seen
			FROM secret_data_entries
			WHERE LOWER(value_decoded) LIKE ? OR value_base64 LIKE ? OR value_base64 LIKE ?
			ORDER BY cluster, namespace, resource_name, data_key
			LIMIT 500
			`
			args = []interface{}{pattern, "%" + encoded + "%", "%" + query + "%"}
		}
	}

	rows, err := r.db.Query(sqlQuery, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return r.scanSecretDataRows(rows)
}

func (r *DependencyRegistry) scanSecretDataRows(rows *sql.Rows) ([]SecretDataRecord, error) {
	var records []SecretDataRecord

	for rows.Next() {
		var rec SecretDataRecord
		var isBinary, truncated int
		var lastSeen string

		err := rows.Scan(
			&rec.ID, &rec.ResourceKind, &rec.Cluster, &rec.Namespace, &rec.ResourceName, &rec.ResourceSubtype,
			&rec.DataKey, &rec.ValueBase64, &rec.ValueDecoded, &isBinary, &truncated, &lastSeen,
		)
		if err != nil {
			return nil, err
		}

		rec.IsBinary = isBinary != 0
		rec.Truncated = truncated != 0

		if t, err := time.Parse("2006-01-02 15:04:05", lastSeen); err == nil {
			rec.LastSeen = t
		} else if t, err := time.Parse(time.RFC3339, lastSeen); err == nil {
			rec.LastSeen = t
		}

		records = append(records, rec)
	}

	return records, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
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
	SELECT id, service_name, service_type, topic_name, cluster, namespace, deployment, source_type, source_name, source_key, first_seen, last_seen
	FROM dependencies
	WHERE service_name LIKE ? OR service_type LIKE ? OR topic_name LIKE ? OR source_name LIKE ? OR source_key LIKE ? OR deployment LIKE ? OR namespace LIKE ?
	ORDER BY service_name, cluster, namespace
	`

	rows, err := r.db.Query(sqlQuery, pattern, pattern, pattern, pattern, pattern, pattern, pattern)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return r.scanRows(rows)
}

// GetAll retorna todas as dependências com filtros opcionais
func (r *DependencyRegistry) GetAll(cluster, namespace, serviceType string) ([]DependencyRecord, error) {
	query := `
	SELECT id, service_name, service_type, topic_name, cluster, namespace, deployment, source_type, source_name, source_key, first_seen, last_seen
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
	SELECT id, service_name, service_type, topic_name, cluster, namespace, deployment, source_type, source_name, source_key, first_seen, last_seen
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

	// Entradas de secret_data_entries (índice de chave/valor de Secrets)
	var secretDataEntries int
	if err := r.db.QueryRow("SELECT COUNT(*) FROM secret_data_entries").Scan(&secretDataEntries); err != nil {
		return nil, err
	}
	stats["secret_data_entries"] = secretDataEntries

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
			"cluster":            cluster,
			"namespaces_scanned": nsScanned,
			"dependencies_found": depsFound,
			"dependencies_saved": depsSaved,
			"duration_ms":        durationMs,
			"scanned_at":         scannedAt,
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
			&dep.TopicName,
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
