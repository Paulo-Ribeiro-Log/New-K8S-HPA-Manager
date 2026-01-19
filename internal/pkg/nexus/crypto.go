package nexus

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"os"
)

const (
	// EnvEncryptionKey é o nome da variável de ambiente para a chave de criptografia
	EnvEncryptionKey = "K8S_HPA_ENCRYPTION_KEY"
	// DefaultEncryptionKey é uma chave padrão (deve ser substituída em produção)
	DefaultEncryptionKey = "k8s-hpa-manager-default-key-2026"
)

// Crypto gerencia criptografia/descriptografia de dados sensíveis
type Crypto struct {
	key []byte
}

// NewCrypto cria uma nova instância do Crypto
func NewCrypto() *Crypto {
	key := getEncryptionKey()
	// Gera hash SHA-256 da chave para ter exatamente 32 bytes
	hash := sha256.Sum256([]byte(key))
	return &Crypto{
		key: hash[:],
	}
}

// getEncryptionKey obtém a chave de criptografia da variável de ambiente ou usa a padrão
func getEncryptionKey() string {
	if key := os.Getenv(EnvEncryptionKey); key != "" {
		return key
	}
	return DefaultEncryptionKey
}

// Encrypt criptografa um texto usando AES-256-GCM
func (c *Crypto) Encrypt(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}

	block, err := aes.NewCipher(c.key)
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

// Decrypt descriptografa um texto usando AES-256-GCM
func (c *Crypto) Decrypt(ciphertext string) (string, error) {
	if ciphertext == "" {
		return "", nil
	}

	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(c.key)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", errors.New("ciphertext too short")
	}

	nonce, cipherText := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, cipherText, nil)
	if err != nil {
		return "", err
	}

	return string(plaintext), nil
}

// EncryptPassword é um helper para criptografar senhas
func EncryptPassword(password string) (string, error) {
	crypto := NewCrypto()
	return crypto.Encrypt(password)
}

// DecryptPassword é um helper para descriptografar senhas
func DecryptPassword(encryptedPassword string) (string, error) {
	crypto := NewCrypto()
	return crypto.Decrypt(encryptedPassword)
}
