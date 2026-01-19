package nexus

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const (
	// ConfigFileName é o nome do arquivo de configuração
	ConfigFileName = "nexus-config.json"
	// ConfigDirName é o nome do diretório de configuração
	ConfigDirName = ".k8s-hpa-manager"
)

// FileConfigManager implementa ConfigManager usando arquivo local
type FileConfigManager struct {
	configPath string
	crypto     *Crypto
}

// NewFileConfigManager cria uma nova instância do FileConfigManager
func NewFileConfigManager() (*FileConfigManager, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	configDir := filepath.Join(homeDir, ConfigDirName)
	configPath := filepath.Join(configDir, ConfigFileName)

	// Cria o diretório se não existir
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create config directory: %w", err)
	}

	return &FileConfigManager{
		configPath: configPath,
		crypto:     NewCrypto(),
	}, nil
}

// Save salva a configuração com senha criptografada
func (m *FileConfigManager) Save(config Config) error {
	// Criptografa a senha
	encryptedPassword, err := m.crypto.Encrypt(config.Password)
	if err != nil {
		return fmt.Errorf("failed to encrypt password: %w", err)
	}

	// Cria cópia com senha criptografada
	configToSave := config
	configToSave.Password = encryptedPassword

	// Serializa para JSON
	data, err := json.MarshalIndent(configToSave, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Salva arquivo com permissões restritas
	if err := os.WriteFile(m.configPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// Load carrega a configuração com senha descriptografada
func (m *FileConfigManager) Load() (*Config, error) {
	if !m.Exists() {
		return nil, fmt.Errorf("config file not found")
	}

	// Lê arquivo
	data, err := os.ReadFile(m.configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Deserializa
	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Descriptografa senha
	if config.Password != "" {
		decryptedPassword, err := m.crypto.Decrypt(config.Password)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt password: %w", err)
		}
		config.Password = decryptedPassword
	}

	return &config, nil
}

// Exists verifica se o arquivo de configuração existe
func (m *FileConfigManager) Exists() bool {
	_, err := os.Stat(m.configPath)
	return err == nil
}

// Delete remove o arquivo de configuração
func (m *FileConfigManager) Delete() error {
	if !m.Exists() {
		return nil
	}

	if err := os.Remove(m.configPath); err != nil {
		return fmt.Errorf("failed to delete config file: %w", err)
	}

	return nil
}

// GetConfigPath retorna o caminho do arquivo de configuração
func (m *FileConfigManager) GetConfigPath() string {
	return m.configPath
}
