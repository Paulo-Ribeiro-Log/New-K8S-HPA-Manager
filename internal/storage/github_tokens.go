package storage

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// GitHubToken representa um token GitHub de um usuário
type GitHubToken struct {
	UserEmail string    `json:"user_email"`
	Token     string    `json:"-"` // Nunca serializar em JSON
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// GitHubTokenStore gerencia tokens GitHub criptografados
type GitHubTokenStore struct {
	db            *sql.DB
	encryptionKey []byte
}

// NewGitHubTokenStore cria nova instância do token store
func NewGitHubTokenStore() (*GitHubTokenStore, error) {
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
	dbPath := filepath.Join(configDir, "github-tokens.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Obter ou criar chave de criptografia
	encryptionKey, err := getOrCreateEncryptionKey(configDir)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to get encryption key: %w", err)
	}

	store := &GitHubTokenStore{
		db:            db,
		encryptionKey: encryptionKey,
	}

	// Criar schema
	if err := store.createSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create schema: %w", err)
	}

	return store, nil
}

// createSchema cria tabela de tokens
func (s *GitHubTokenStore) createSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS github_tokens (
		user_email TEXT PRIMARY KEY,
		encrypted_token TEXT NOT NULL,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_updated_at ON github_tokens(updated_at);
	`

	_, err := s.db.Exec(schema)
	return err
}

// SaveToken salva token criptografado para um usuário
func (s *GitHubTokenStore) SaveToken(userEmail, token string) error {
	if userEmail == "" {
		return fmt.Errorf("user_email é obrigatório")
	}
	if token == "" {
		return fmt.Errorf("token é obrigatório")
	}

	// Criptografar token
	encryptedToken, err := s.encrypt(token)
	if err != nil {
		return fmt.Errorf("failed to encrypt token: %w", err)
	}

	// Upsert no banco
	query := `
	INSERT INTO github_tokens (user_email, encrypted_token, updated_at)
	VALUES (?, ?, CURRENT_TIMESTAMP)
	ON CONFLICT(user_email) DO UPDATE SET
		encrypted_token = excluded.encrypted_token,
		updated_at = CURRENT_TIMESTAMP
	`

	_, err = s.db.Exec(query, userEmail, encryptedToken)
	if err != nil {
		return fmt.Errorf("failed to save token: %w", err)
	}

	return nil
}

// GetToken retorna token descriptografado de um usuário
func (s *GitHubTokenStore) GetToken(userEmail string) (string, error) {
	if userEmail == "" {
		return "", fmt.Errorf("user_email é obrigatório")
	}

	var encryptedToken string
	query := `SELECT encrypted_token FROM github_tokens WHERE user_email = ?`

	err := s.db.QueryRow(query, userEmail).Scan(&encryptedToken)
	if err == sql.ErrNoRows {
		return "", fmt.Errorf("token não encontrado para usuário: %s", userEmail)
	}
	if err != nil {
		return "", fmt.Errorf("failed to get token: %w", err)
	}

	// Descriptografar token
	token, err := s.decrypt(encryptedToken)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt token: %w", err)
	}

	return token, nil
}

// DeleteToken remove token de um usuário
func (s *GitHubTokenStore) DeleteToken(userEmail string) error {
	if userEmail == "" {
		return fmt.Errorf("user_email é obrigatório")
	}

	query := `DELETE FROM github_tokens WHERE user_email = ?`
	result, err := s.db.Exec(query, userEmail)
	if err != nil {
		return fmt.Errorf("failed to delete token: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("token não encontrado para usuário: %s", userEmail)
	}

	return nil
}

// HasToken verifica se usuário tem token configurado
func (s *GitHubTokenStore) HasToken(userEmail string) bool {
	if userEmail == "" {
		return false
	}

	var count int
	query := `SELECT COUNT(*) FROM github_tokens WHERE user_email = ?`
	s.db.QueryRow(query, userEmail).Scan(&count)

	return count > 0
}

// encrypt criptografa texto usando AES-256-GCM
func (s *GitHubTokenStore) encrypt(plaintext string) (string, error) {
	block, err := aes.NewCipher(s.encryptionKey)
	if err != nil {
		return "", err
	}

	// GCM (Galois/Counter Mode) para AEAD (Authenticated Encryption with Associated Data)
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	// Criar nonce aleatório (12 bytes para GCM)
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	// Criptografar (nonce é prepended ao ciphertext)
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)

	// Retornar como base64 para armazenamento em SQLite
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// decrypt descriptografa texto usando AES-256-GCM
func (s *GitHubTokenStore) decrypt(encryptedBase64 string) (string, error) {
	// Decodificar de base64
	ciphertext, err := base64.StdEncoding.DecodeString(encryptedBase64)
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(s.encryptionKey)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}

	// Extrair nonce e ciphertext
	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]

	// Descriptografar
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}

	return string(plaintext), nil
}

// Close fecha conexão com banco
func (s *GitHubTokenStore) Close() error {
	return s.db.Close()
}

// getOrCreateEncryptionKey obtém ou cria chave de criptografia AES-256 (32 bytes)
func getOrCreateEncryptionKey(configDir string) ([]byte, error) {
	secretPath := filepath.Join(configDir, ".secret")

	// Tentar ler chave existente
	if data, err := os.ReadFile(secretPath); err == nil {
		// Decodificar de base64
		key, err := base64.StdEncoding.DecodeString(string(data))
		if err != nil {
			return nil, fmt.Errorf("failed to decode encryption key: %w", err)
		}

		// Validar tamanho (AES-256 = 32 bytes)
		if len(key) != 32 {
			return nil, fmt.Errorf("invalid encryption key size: %d (expected 32)", len(key))
		}

		return key, nil
	}

	// Gerar nova chave aleatória (32 bytes = 256 bits)
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("failed to generate encryption key: %w", err)
	}

	// Salvar chave em arquivo (base64, permissão 600 - somente owner)
	encoded := base64.StdEncoding.EncodeToString(key)
	if err := os.WriteFile(secretPath, []byte(encoded), 0600); err != nil {
		return nil, fmt.Errorf("failed to save encryption key: %w", err)
	}

	return key, nil
}
