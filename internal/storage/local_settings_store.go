package storage

import (
	"database/sql"
	"fmt"
	"time"
)

// LocalSettingsStore gerencia configurações locais da instalação
type LocalSettingsStore struct {
	client *SQLiteClient
}

// NewLocalSettingsStore cria nova instância
func NewLocalSettingsStore(client *SQLiteClient) *LocalSettingsStore {
	return &LocalSettingsStore{client: client}
}

// LocalSetting representa uma configuração local
type LocalSetting struct {
	Key       string    `json:"key"`
	Value     string    `json:"value"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Constantes para chaves conhecidas
const (
	SettingLastAIEmail = "last_ai_email"
)

// CreateTable cria tabela de configurações locais
func (s *LocalSettingsStore) CreateTable() error {
	query := `
	CREATE TABLE IF NOT EXISTS local_settings (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	`

	_, err := s.client.db.Exec(query)
	return err
}

// Set define uma configuração
func (s *LocalSettingsStore) Set(key, value string) error {
	if key == "" {
		return fmt.Errorf("key is required")
	}

	query := `
	INSERT INTO local_settings (key, value, updated_at)
	VALUES (?, ?, ?)
	ON CONFLICT(key) DO UPDATE SET
		value = excluded.value,
		updated_at = excluded.updated_at
	`

	_, err := s.client.db.Exec(query, key, value, time.Now())
	if err != nil {
		return fmt.Errorf("failed to set setting: %w", err)
	}

	return nil
}

// Get obtém uma configuração
func (s *LocalSettingsStore) Get(key string) (string, error) {
	if key == "" {
		return "", fmt.Errorf("key is required")
	}

	query := `SELECT value FROM local_settings WHERE key = ?`
	row := s.client.db.QueryRow(query, key)

	var value string
	err := row.Scan(&value)

	if err == sql.ErrNoRows {
		return "", nil // Configuração não existe
	}

	if err != nil {
		return "", fmt.Errorf("failed to get setting: %w", err)
	}

	return value, nil
}

// Delete remove uma configuração
func (s *LocalSettingsStore) Delete(key string) error {
	if key == "" {
		return fmt.Errorf("key is required")
	}

	query := `DELETE FROM local_settings WHERE key = ?`
	_, err := s.client.db.Exec(query, key)
	return err
}

// GetAll retorna todas as configurações
func (s *LocalSettingsStore) GetAll() ([]LocalSetting, error) {
	query := `SELECT key, value, updated_at FROM local_settings ORDER BY key`

	rows, err := s.client.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to get settings: %w", err)
	}
	defer rows.Close()

	var settings []LocalSetting
	for rows.Next() {
		var setting LocalSetting
		if err := rows.Scan(&setting.Key, &setting.Value, &setting.UpdatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan setting: %w", err)
		}
		settings = append(settings, setting)
	}

	return settings, nil
}

// SetLastAIEmail define o último email AI usado (atalho)
func (s *LocalSettingsStore) SetLastAIEmail(email string) error {
	return s.Set(SettingLastAIEmail, email)
}

// GetLastAIEmail obtém o último email AI usado (atalho)
func (s *LocalSettingsStore) GetLastAIEmail() (string, error) {
	return s.Get(SettingLastAIEmail)
}
