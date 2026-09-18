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

	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
)

// SSHCredentialProfile representa um perfil SSH nomeado de um usuário — chave privada ou senha,
// SEMPRE criptografadas em repouso (AES-256-GCM, mesmo mecanismo de github_tokens.go — diferente
// de user_tokens_store.go, que grava API keys em texto puro; uma chave privada SSH nunca deveria
// entrar num store sem criptografia).
type SSHCredentialProfile struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Username   string    `json:"username"`
	AuthMethod string    `json:"authMethod"` // "key" | "password"
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

// SSHCredentialProfileSecrets carrega os segredos DESCRIPTOGRAFADOS — nunca serializado em JSON,
// nunca exposto por nenhum handler HTTP diretamente (só consumido internamente por
// internal/vmssh ao abrir uma conexão).
type SSHCredentialProfileSecrets struct {
	SSHCredentialProfile
	PrivateKeyPEM []byte
	Passphrase    string
	Password      string
}

// SaveSSHCredentialProfileInput é o que o usuário fornece ao criar/editar um perfil.
type SaveSSHCredentialProfileInput struct {
	ID            string // vazio = criar novo
	Name          string
	Username      string
	AuthMethod    string // "key" | "password"
	PrivateKeyPEM string
	Passphrase    string
	Password      string
}

// VMCredentialStore gerencia perfis de credencial SSH criptografados, por usuário — mesmo padrão
// de GitHubTokenStore (banco próprio, chave de criptografia compartilhada via
// getOrCreateEncryptionKey), mas com MÚLTIPLOS perfis nomeados por usuário (não um slot único),
// já que um analista tipicamente tem chaves diferentes pra frotas/ambientes diferentes.
type VMCredentialStore struct {
	db            *sql.DB
	encryptionKey []byte
}

// NewVMCredentialStore cria/abre o store — banco próprio (vm-credentials.db), separado de
// github-tokens.db e do user_ai_tokens da UserTokensStore.
func NewVMCredentialStore() (*VMCredentialStore, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	configDir := filepath.Join(homeDir, ".k8s-hpa-manager")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create config directory: %w", err)
	}

	dbPath := filepath.Join(configDir, "vm-credentials.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	encryptionKey, err := getOrCreateEncryptionKey(configDir)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to get encryption key: %w", err)
	}

	store := &VMCredentialStore{db: db, encryptionKey: encryptionKey}
	if err := store.createSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create schema: %w", err)
	}
	return store, nil
}

func (s *VMCredentialStore) createSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS vm_ssh_credentials (
		id TEXT PRIMARY KEY,
		user_email TEXT NOT NULL,
		name TEXT NOT NULL,
		username TEXT NOT NULL,
		auth_method TEXT NOT NULL,
		encrypted_private_key TEXT,
		encrypted_passphrase TEXT,
		encrypted_password TEXT,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_vm_ssh_credentials_user ON vm_ssh_credentials(user_email);
	`
	_, err := s.db.Exec(schema)
	return err
}

// SaveProfile cria (ID vazio) ou atualiza (ID preenchido) um perfil — sempre escopado ao
// user_email do chamador, nunca permite editar o perfil de outro usuário mesmo sabendo o ID
// (WHERE id=? AND user_email=? no UPDATE).
func (s *VMCredentialStore) SaveProfile(userEmail string, input SaveSSHCredentialProfileInput) (string, error) {
	if userEmail == "" {
		return "", fmt.Errorf("user_email é obrigatório")
	}
	if input.Name == "" || input.Username == "" {
		return "", fmt.Errorf("name e username são obrigatórios")
	}
	if input.AuthMethod != "key" && input.AuthMethod != "password" {
		return "", fmt.Errorf("authMethod deve ser 'key' ou 'password'")
	}

	var encPrivateKey, encPassphrase, encPassword sql.NullString
	if input.AuthMethod == "key" {
		if input.PrivateKeyPEM == "" {
			return "", fmt.Errorf("privateKeyPEM é obrigatório para authMethod=key")
		}
		enc, err := s.encrypt(input.PrivateKeyPEM)
		if err != nil {
			return "", fmt.Errorf("failed to encrypt private key: %w", err)
		}
		encPrivateKey = sql.NullString{String: enc, Valid: true}
		if input.Passphrase != "" {
			enc, err := s.encrypt(input.Passphrase)
			if err != nil {
				return "", fmt.Errorf("failed to encrypt passphrase: %w", err)
			}
			encPassphrase = sql.NullString{String: enc, Valid: true}
		}
	} else {
		if input.Password == "" {
			return "", fmt.Errorf("password é obrigatório para authMethod=password")
		}
		enc, err := s.encrypt(input.Password)
		if err != nil {
			return "", fmt.Errorf("failed to encrypt password: %w", err)
		}
		encPassword = sql.NullString{String: enc, Valid: true}
	}

	id := input.ID
	if id == "" {
		id = uuid.New().String()
		_, err := s.db.Exec(
			`INSERT INTO vm_ssh_credentials (id, user_email, name, username, auth_method, encrypted_private_key, encrypted_passphrase, encrypted_password)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			id, userEmail, input.Name, input.Username, input.AuthMethod, encPrivateKey, encPassphrase, encPassword,
		)
		if err != nil {
			return "", fmt.Errorf("failed to create credential profile: %w", err)
		}
		return id, nil
	}

	result, err := s.db.Exec(
		`UPDATE vm_ssh_credentials SET name=?, username=?, auth_method=?, encrypted_private_key=?, encrypted_passphrase=?, encrypted_password=?, updated_at=CURRENT_TIMESTAMP
		 WHERE id=? AND user_email=?`,
		input.Name, input.Username, input.AuthMethod, encPrivateKey, encPassphrase, encPassword, id, userEmail,
	)
	if err != nil {
		return "", fmt.Errorf("failed to update credential profile: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return "", fmt.Errorf("perfil não encontrado (ou pertence a outro usuário): %s", id)
	}
	return id, nil
}

// ListProfiles lista os perfis de um usuário — NUNCA inclui os segredos, só metadados de
// exibição (mesmo espírito de maskGitHubToken/GetGitHubProfiles: o segredo nunca sai do backend
// em texto puro).
func (s *VMCredentialStore) ListProfiles(userEmail string) ([]SSHCredentialProfile, error) {
	rows, err := s.db.Query(
		`SELECT id, name, username, auth_method, created_at, updated_at FROM vm_ssh_credentials WHERE user_email = ? ORDER BY name`,
		userEmail,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list credential profiles: %w", err)
	}
	defer rows.Close()

	profiles := []SSHCredentialProfile{}
	for rows.Next() {
		var p SSHCredentialProfile
		if err := rows.Scan(&p.ID, &p.Name, &p.Username, &p.AuthMethod, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan credential profile: %w", err)
		}
		profiles = append(profiles, p)
	}
	return profiles, nil
}

// GetDecrypted resolve um perfil com os segredos DESCRIPTOGRAFADOS — uso interno apenas (ver
// internal/web/handlers/vm_terminal.go), nunca deve ser serializado numa resposta HTTP. Escopado
// ao user_email do chamador — um usuário nunca consegue ler o perfil de outro mesmo sabendo o ID.
func (s *VMCredentialStore) GetDecrypted(userEmail, id string) (*SSHCredentialProfileSecrets, error) {
	var p SSHCredentialProfileSecrets
	var encPrivateKey, encPassphrase, encPassword sql.NullString

	err := s.db.QueryRow(
		`SELECT id, name, username, auth_method, encrypted_private_key, encrypted_passphrase, encrypted_password, created_at, updated_at
		 FROM vm_ssh_credentials WHERE id = ? AND user_email = ?`,
		id, userEmail,
	).Scan(&p.ID, &p.Name, &p.Username, &p.AuthMethod, &encPrivateKey, &encPassphrase, &encPassword, &p.CreatedAt, &p.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("perfil de credencial não encontrado: %s", id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get credential profile: %w", err)
	}

	if encPrivateKey.Valid {
		dec, err := s.decrypt(encPrivateKey.String)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt private key: %w", err)
		}
		p.PrivateKeyPEM = []byte(dec)
	}
	if encPassphrase.Valid {
		dec, err := s.decrypt(encPassphrase.String)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt passphrase: %w", err)
		}
		p.Passphrase = dec
	}
	if encPassword.Valid {
		dec, err := s.decrypt(encPassword.String)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt password: %w", err)
		}
		p.Password = dec
	}

	return &p, nil
}

// DeleteProfile remove um perfil — escopado ao user_email do chamador.
func (s *VMCredentialStore) DeleteProfile(userEmail, id string) error {
	result, err := s.db.Exec(`DELETE FROM vm_ssh_credentials WHERE id = ? AND user_email = ?`, id, userEmail)
	if err != nil {
		return fmt.Errorf("failed to delete credential profile: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("perfil não encontrado (ou pertence a outro usuário): %s", id)
	}
	return nil
}

func (s *VMCredentialStore) Close() error {
	return s.db.Close()
}

// encrypt/decrypt — AES-256-GCM, mesma implementação exata de GitHubTokenStore (copiada em vez de
// extraída para um helper compartilhado: são só ~30 linhas cada, e duplicar aqui evita acoplar os
// dois stores um ao outro por um detalhe de implementação que pode divergir no futuro).

func (s *VMCredentialStore) encrypt(plaintext string) (string, error) {
	block, err := aes.NewCipher(s.encryptionKey)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

func (s *VMCredentialStore) decrypt(encryptedBase64 string) (string, error) {
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
	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}
