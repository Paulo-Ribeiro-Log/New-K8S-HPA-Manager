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

	return nil
}
