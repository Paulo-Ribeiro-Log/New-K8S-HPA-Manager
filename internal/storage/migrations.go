package storage

const (
	// Schema SQL para tabela de histórico de análises AI
	createHistoryTableSQL = `
CREATE TABLE IF NOT EXISTS ai_analysis_history (
    id TEXT PRIMARY KEY,
    resource_type TEXT NOT NULL,
    cluster TEXT NOT NULL,
    namespace TEXT NOT NULL,
    resource_name TEXT NOT NULL,

    provider TEXT NOT NULL,
    model TEXT,
    analysis TEXT NOT NULL,
    suggestions TEXT,

    tokens_used INTEGER,
    response_time REAL,
    analyzed_at DATETIME NOT NULL,
    user_email TEXT,

    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
`

	// Índices para melhorar performance de queries
	createIndexesSQL = `
CREATE INDEX IF NOT EXISTS idx_resource ON ai_analysis_history(cluster, namespace, resource_name);
CREATE INDEX IF NOT EXISTS idx_analyzed_at ON ai_analysis_history(analyzed_at DESC);
CREATE INDEX IF NOT EXISTS idx_provider ON ai_analysis_history(provider);
CREATE INDEX IF NOT EXISTS idx_user_email ON ai_analysis_history(user_email);
CREATE INDEX IF NOT EXISTS idx_resource_type ON ai_analysis_history(resource_type);
`

	// Schema SQL para tabela de histórico de análises preditivas
	createPredictionsTableSQL = `
CREATE TABLE IF NOT EXISTS predictions_history (
    id TEXT PRIMARY KEY,
    cluster TEXT NOT NULL,
    namespace TEXT NOT NULL,
    deployment TEXT NOT NULL,
    
    health_score INTEGER NOT NULL,
    risk_level TEXT NOT NULL,
    executive_summary TEXT NOT NULL,
    predictions TEXT NOT NULL,
    recommendations TEXT NOT NULL,
    raw_metrics TEXT NOT NULL,
    
    provider TEXT NOT NULL,
    model TEXT,
    duration_ms INTEGER NOT NULL,
    user_email TEXT,
    analyzed_at DATETIME NOT NULL,
    
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
`

	// Índices para predictions
	createPredictionsIndexesSQL = `
CREATE INDEX IF NOT EXISTS idx_pred_deployment ON predictions_history(cluster, namespace, deployment);
CREATE INDEX IF NOT EXISTS idx_pred_analyzed_at ON predictions_history(analyzed_at DESC);
CREATE INDEX IF NOT EXISTS idx_pred_risk_level ON predictions_history(risk_level);
CREATE INDEX IF NOT EXISTS idx_pred_user_email ON predictions_history(user_email);
CREATE INDEX IF NOT EXISTS idx_pred_health_score ON predictions_history(health_score);
`

	// Migration: Adicionar coluna prometheus_metrics (FASE 2.1 - 06/01/2026)
	addPrometheusMetricsColumnSQL = `
ALTER TABLE ai_analysis_history
ADD COLUMN prometheus_metrics TEXT;
`
)

// runMigrations executa migrations do banco de dados
func (c *SQLiteClient) runMigrations() error {
	// Criar tabela de histórico
	if _, err := c.db.Exec(createHistoryTableSQL); err != nil {
		return err
	}

	// Criar índices
	if _, err := c.db.Exec(createIndexesSQL); err != nil {
		return err
	}

	// Criar tabela de predictions
	if _, err := c.db.Exec(createPredictionsTableSQL); err != nil {
		return err
	}

	// Criar índices de predictions
	if _, err := c.db.Exec(createPredictionsIndexesSQL); err != nil {
		return err
	}

	// Migration: Adicionar coluna prometheus_metrics (se não existir)
	// Verificar se coluna já existe antes de tentar adicionar
	var columnExists int
	err := c.db.QueryRow(`
		SELECT COUNT(*)
		FROM pragma_table_info('ai_analysis_history')
		WHERE name='prometheus_metrics'
	`).Scan(&columnExists)

	if err != nil {
		return err
	}

	// Adicionar coluna apenas se não existir
	if columnExists == 0 {
		if _, err := c.db.Exec(addPrometheusMetricsColumnSQL); err != nil {
			return err
		}
	}

	return nil
}
