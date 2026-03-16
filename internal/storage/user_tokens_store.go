package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// UserTokensStore gerencia tokens AI dos usuários
type UserTokensStore struct {
	client *SQLiteClient
}

// NewUserTokensStore cria nova instância
func NewUserTokensStore(client *SQLiteClient) *UserTokensStore {
	return &UserTokensStore{client: client}
}

// UserTokens representa tokens de um usuário
type UserTokens struct {
	UserEmail            string            `json:"user_email"`
	GeminiAPIKey         string            `json:"gemini_api_key,omitempty"`
	GeminiModel          string            `json:"gemini_model,omitempty"`
	GeminiAuthMode       string            `json:"gemini_auth_mode,omitempty"`       // "apikey" ou "vertex"
	GeminiVertexProject  string            `json:"gemini_vertex_project,omitempty"`  // projeto GCP para Vertex AI
	GeminiVertexLocation     string            `json:"gemini_vertex_location,omitempty"`      // região GCP (ex: us-central1)
	GeminiServiceAccountJSON string            `json:"gemini_service_account_json,omitempty"` // JSON do service account (sem gcloud)
	GeminiRefreshToken       string            `json:"gemini_refresh_token,omitempty"`         // OAuth refresh token (Device Auth flow)
	OpenAIAPIKey             string            `json:"openai_api_key,omitempty"`
	OpenAIModel         string            `json:"openai_model,omitempty"`
	ClaudeAPIKey        string            `json:"claude_api_key,omitempty"`
	ClaudeModel         string            `json:"claude_model,omitempty"`
	CopilotAPIKey       string            `json:"copilot_api_key,omitempty"`
	CopilotEndpoint     string            `json:"copilot_endpoint,omitempty"`
	CopilotDeployment   string            `json:"copilot_deployment,omitempty"`
	OllamaModel         string            `json:"ollama_model,omitempty"`
	PreferredProvider   string            `json:"preferred_provider"`
	DynatraceURL        string            `json:"dynatrace_url,omitempty"`
	DynatraceToken      string            `json:"dynatrace_token,omitempty"`
	DynatraceTagFilter  string            `json:"dynatrace_tag_filter,omitempty"` // filtro de tag: mostrar apenas problems com essa tag
	Metadata            map[string]string `json:"metadata,omitempty"`
	UpdatedAt           time.Time         `json:"updated_at"`
	CreatedAt           time.Time         `json:"created_at"`
}

// CreateTable cria tabela de tokens de usuários
func (s *UserTokensStore) CreateTable() error {
	query := `
	CREATE TABLE IF NOT EXISTS user_ai_tokens (
		user_email TEXT PRIMARY KEY,
		gemini_api_key TEXT,
		gemini_model TEXT,
		openai_api_key TEXT,
		openai_model TEXT,
		claude_api_key TEXT,
		claude_model TEXT,
		copilot_api_key TEXT,
		copilot_endpoint TEXT,
		copilot_deployment TEXT,
		ollama_model TEXT,
		preferred_provider TEXT DEFAULT 'ollama',
		metadata TEXT,
		updated_at DATETIME NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_user_tokens_email ON user_ai_tokens(user_email);
	CREATE INDEX IF NOT EXISTS idx_user_tokens_updated ON user_ai_tokens(updated_at DESC);
	`

	_, err := s.client.db.Exec(query)
	if err != nil {
		return err
	}

	// Migration: adicionar colunas se não existirem (para DBs existentes)
	migrations := []string{
		`ALTER TABLE user_ai_tokens ADD COLUMN gemini_model TEXT`,
		`ALTER TABLE user_ai_tokens ADD COLUMN openai_model TEXT`,
		`ALTER TABLE user_ai_tokens ADD COLUMN claude_model TEXT`,
		`ALTER TABLE user_ai_tokens ADD COLUMN ollama_model TEXT`,
		`ALTER TABLE user_ai_tokens ADD COLUMN gemini_auth_mode TEXT DEFAULT 'apikey'`,
		`ALTER TABLE user_ai_tokens ADD COLUMN gemini_vertex_project TEXT`,
		`ALTER TABLE user_ai_tokens ADD COLUMN gemini_vertex_location TEXT`,
		`ALTER TABLE user_ai_tokens ADD COLUMN gemini_service_account_json TEXT`,
		`ALTER TABLE user_ai_tokens ADD COLUMN gemini_refresh_token TEXT`,
		`ALTER TABLE user_ai_tokens ADD COLUMN copilot_api_key TEXT`,
		`ALTER TABLE user_ai_tokens ADD COLUMN copilot_endpoint TEXT`,
		`ALTER TABLE user_ai_tokens ADD COLUMN copilot_deployment TEXT`,
		`ALTER TABLE user_ai_tokens ADD COLUMN dynatrace_url TEXT`,
		`ALTER TABLE user_ai_tokens ADD COLUMN dynatrace_token TEXT`,
		`ALTER TABLE user_ai_tokens ADD COLUMN dynatrace_tag_filter TEXT`,
	}

	for _, migration := range migrations {
		// Ignorar erros — "duplicate column name" é esperado se coluna já existe
		s.client.Exec(migration) //nolint:errcheck
	}

	return nil
}

// SaveTokens salva/atualiza tokens de um usuário
func (s *UserTokensStore) SaveTokens(userEmail string, tokens *UserTokens) error {
	if userEmail == "" {
		return fmt.Errorf("user_email is required")
	}

	tokens.UserEmail = userEmail
	tokens.UpdatedAt = time.Now()

	// Serializar metadata
	metadataJSON := "{}"
	if tokens.Metadata != nil {
		data, err := json.Marshal(tokens.Metadata)
		if err != nil {
			return fmt.Errorf("failed to marshal metadata: %w", err)
		}
		metadataJSON = string(data)
	}

	query := `
	INSERT INTO user_ai_tokens (
		user_email, gemini_api_key, gemini_model, gemini_auth_mode, gemini_vertex_project, gemini_vertex_location,
		gemini_service_account_json, gemini_refresh_token, openai_api_key, openai_model, claude_api_key, claude_model,
		copilot_api_key, copilot_endpoint, copilot_deployment, ollama_model, preferred_provider,
		dynatrace_url, dynatrace_token, dynatrace_tag_filter, metadata, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(user_email) DO UPDATE SET
		gemini_api_key = excluded.gemini_api_key,
		gemini_model = excluded.gemini_model,
		gemini_auth_mode = excluded.gemini_auth_mode,
		gemini_vertex_project = excluded.gemini_vertex_project,
		gemini_vertex_location = excluded.gemini_vertex_location,
		gemini_service_account_json = excluded.gemini_service_account_json,
		gemini_refresh_token = excluded.gemini_refresh_token,
		openai_api_key = excluded.openai_api_key,
		openai_model = excluded.openai_model,
		claude_api_key = excluded.claude_api_key,
		claude_model = excluded.claude_model,
		copilot_api_key = excluded.copilot_api_key,
		copilot_endpoint = excluded.copilot_endpoint,
		copilot_deployment = excluded.copilot_deployment,
		ollama_model = excluded.ollama_model,
		preferred_provider = excluded.preferred_provider,
		dynatrace_url = excluded.dynatrace_url,
		dynatrace_token = excluded.dynatrace_token,
		dynatrace_tag_filter = excluded.dynatrace_tag_filter,
		metadata = excluded.metadata,
		updated_at = excluded.updated_at
	`

	_, err := s.client.db.Exec(query,
		userEmail,
		tokens.GeminiAPIKey,
		tokens.GeminiModel,
		tokens.GeminiAuthMode,
		tokens.GeminiVertexProject,
		tokens.GeminiVertexLocation,
		tokens.GeminiServiceAccountJSON,
		tokens.GeminiRefreshToken,
		tokens.OpenAIAPIKey,
		tokens.OpenAIModel,
		tokens.ClaudeAPIKey,
		tokens.ClaudeModel,
		tokens.CopilotAPIKey,
		tokens.CopilotEndpoint,
		tokens.CopilotDeployment,
		tokens.OllamaModel,
		tokens.PreferredProvider,
		tokens.DynatraceURL,
		tokens.DynatraceToken,
		tokens.DynatraceTagFilter,
		metadataJSON,
		tokens.UpdatedAt,
	)

	if err != nil {
		return fmt.Errorf("failed to save tokens: %w", err)
	}

	return nil
}

// GetTokens obtém tokens de um usuário
func (s *UserTokensStore) GetTokens(userEmail string) (*UserTokens, error) {
	if userEmail == "" {
		return nil, fmt.Errorf("user_email is required")
	}

	query := `
	SELECT user_email, gemini_api_key, gemini_model, gemini_auth_mode, gemini_vertex_project, gemini_vertex_location,
	       gemini_service_account_json, gemini_refresh_token, openai_api_key, openai_model, claude_api_key, claude_model,
	       copilot_api_key, copilot_endpoint, copilot_deployment, ollama_model, preferred_provider,
	       dynatrace_url, dynatrace_token, dynatrace_tag_filter, metadata, updated_at, created_at
	FROM user_ai_tokens
	WHERE user_email = ?
	`

	row := s.client.db.QueryRow(query, userEmail)

	var tokens UserTokens
	var metadataJSON string
	var geminiAuthMode, geminiVertexProject, geminiVertexLocation, geminiServiceAccountJSON, geminiRefreshToken sql.NullString
	var dynatraceURL, dynatraceToken, dynatraceTagFilter sql.NullString

	err := row.Scan(
		&tokens.UserEmail,
		&tokens.GeminiAPIKey,
		&tokens.GeminiModel,
		&geminiAuthMode,
		&geminiVertexProject,
		&geminiVertexLocation,
		&geminiServiceAccountJSON,
		&geminiRefreshToken,
		&tokens.OpenAIAPIKey,
		&tokens.OpenAIModel,
		&tokens.ClaudeAPIKey,
		&tokens.ClaudeModel,
		&tokens.CopilotAPIKey,
		&tokens.CopilotEndpoint,
		&tokens.CopilotDeployment,
		&tokens.OllamaModel,
		&tokens.PreferredProvider,
		&dynatraceURL,
		&dynatraceToken,
		&dynatraceTagFilter,
		&metadataJSON,
		&tokens.UpdatedAt,
		&tokens.CreatedAt,
	)
	tokens.GeminiAuthMode = geminiAuthMode.String
	tokens.GeminiVertexProject = geminiVertexProject.String
	tokens.GeminiVertexLocation = geminiVertexLocation.String
	tokens.GeminiServiceAccountJSON = geminiServiceAccountJSON.String
	tokens.GeminiRefreshToken = geminiRefreshToken.String
	tokens.DynatraceURL = dynatraceURL.String
	tokens.DynatraceToken = dynatraceToken.String
	tokens.DynatraceTagFilter = dynatraceTagFilter.String

	if err == sql.ErrNoRows {
		return nil, nil // Usuário não tem tokens configurados
	}

	if err != nil {
		return nil, fmt.Errorf("failed to get tokens: %w", err)
	}

	// Deserializar metadata
	if metadataJSON != "" && metadataJSON != "{}" {
		if err := json.Unmarshal([]byte(metadataJSON), &tokens.Metadata); err != nil {
			// Ignorar erro de deserialização de metadata
			tokens.Metadata = nil
		}
	}

	return &tokens, nil
}

// DeleteTokens remove tokens de um usuário
func (s *UserTokensStore) DeleteTokens(userEmail string) error {
	if userEmail == "" {
		return fmt.Errorf("user_email is required")
	}

	query := `DELETE FROM user_ai_tokens WHERE user_email = ?`
	_, err := s.client.db.Exec(query, userEmail)
	return err
}

// HasTokens verifica se usuário tem pelo menos um token configurado
func (s *UserTokensStore) HasTokens(userEmail string) (bool, error) {
	tokens, err := s.GetTokens(userEmail)
	if err != nil {
		return false, err
	}

	if tokens == nil {
		return false, nil
	}

	hasGemini := tokens.GeminiAPIKey != "" ||
		(tokens.GeminiAuthMode == "vertex" && tokens.GeminiVertexProject != "") ||
		tokens.GeminiRefreshToken != ""
	return hasGemini || tokens.OpenAIAPIKey != "" || tokens.ClaudeAPIKey != "" || tokens.CopilotAPIKey != "", nil
}
