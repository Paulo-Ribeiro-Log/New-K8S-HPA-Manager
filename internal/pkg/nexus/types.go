package nexus

import "time"

// Config representa a configuração de conexão com o Nexus
type Config struct {
	BaseURL    string `json:"baseUrl"`    // Ex: https://nexus.viavarejo.com.br
	Repository string `json:"repository"` // Ex: workspace
	Username   string `json:"username"`   // Usuário SSO
	Password   string `json:"password"`   // Senha (será criptografada)
	TempDir    string `json:"tempDir"`    // Diretório temporário para downloads
	URLPattern string `json:"urlPattern"` // Padrão de URL customizado (opcional)
	// Placeholders disponíveis: {baseUrl}, {repository}, {release}, {version}, {environment}, {type}
	// Padrão: {baseUrl}/repository/{repository}/{release}/{version}/{environment}/values/{type}-values.yaml
}

// ValuesFileRequest representa uma requisição para baixar um arquivo de values
type ValuesFileRequest struct {
	Release     string `json:"release" binding:"required"`     // Nome do release
	Version     string `json:"version" binding:"required"`     // Versão (ex: v1.0.0)
	Environment string `json:"environment" binding:"required"` // dev, sit, uat, hlg, prd
	Type        string `json:"type" binding:"required"`        // base, sit, prd, hlg
}

// ValuesFileResponse representa a resposta com o conteúdo de um arquivo
type ValuesFileResponse struct {
	Content  string `json:"content"`            // Conteúdo YAML do arquivo
	FilePath string `json:"filePath"`           // Path relativo no Nexus
	FullURL  string `json:"fullUrl"`            // URL completa
	Size     int64  `json:"size"`               // Tamanho em bytes
	Error    string `json:"error,omitempty"`    // Mensagem de erro, se houver
}

// CompareValuesRequest representa requisição para comparar dois arquivos
type CompareValuesRequest struct {
	File1 ValuesFileRequest `json:"file1" binding:"required"`
	File2 ValuesFileRequest `json:"file2" binding:"required"`
}

// CompareValuesResponse representa a resposta da comparação
type CompareValuesResponse struct {
	File1    ValuesFileResponse `json:"file1"`
	File2    ValuesFileResponse `json:"file2"`
	Error    string             `json:"error,omitempty"`
}

// TestConnectionResponse representa a resposta do teste de conexão
type TestConnectionResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Version string `json:"version,omitempty"` // Versão do Nexus, se disponível
}

// VersionInfo representa informações de uma versão
type VersionInfo struct {
	Version     string    `json:"version"`
	ReleaseDate time.Time `json:"releaseDate,omitempty"`
}

// EnvironmentInfo representa informações de um ambiente
type EnvironmentInfo struct {
	Name      string   `json:"name"`
	Available []string `json:"available"` // Tipos de values disponíveis
}

// DownloadedFile representa um arquivo baixado temporariamente
type DownloadedFile struct {
	LocalPath  string    `json:"localPath"`
	RemotePath string    `json:"remotePath"`
	Size       int64     `json:"size"`
	Downloaded time.Time `json:"downloaded"`
}

// Client interface define as operações disponíveis para o Nexus
type Client interface {
	// TestConnection verifica se a conexão e autenticação estão funcionando
	TestConnection() (*TestConnectionResponse, error)

	// DownloadValues baixa um arquivo de values do Nexus
	DownloadValues(req ValuesFileRequest) (*ValuesFileResponse, error)

	// DownloadMultipleValues baixa múltiplos arquivos
	DownloadMultipleValues(reqs []ValuesFileRequest) ([]ValuesFileResponse, error)

	// BuildURL constrói a URL completa para um arquivo
	BuildURL(req ValuesFileRequest) string

	// CleanupTempFiles limpa arquivos temporários antigos
	CleanupTempFiles(olderThan time.Duration) error
}

// ConfigManager interface define operações de gerenciamento de configuração
type ConfigManager interface {
	// Save salva a configuração (com senha criptografada)
	Save(config Config) error

	// Load carrega a configuração (com senha descriptografada)
	Load() (*Config, error)

	// Exists verifica se existe configuração salva
	Exists() bool

	// Delete remove a configuração salva
	Delete() error
}

// ValidEnvironments lista de ambientes válidos
var ValidEnvironments = []string{"default", "dev", "sit", "uat", "hlg", "prd"}

// ValidTypes lista de tipos válidos
var ValidTypes = []string{"base", "sit", "prd", "hlg", "dev"}

// IsValidEnvironment verifica se um ambiente é válido
func IsValidEnvironment(env string) bool {
	for _, valid := range ValidEnvironments {
		if valid == env {
			return true
		}
	}
	return false
}

// IsValidType verifica se um tipo é válido
func IsValidType(t string) bool {
	for _, valid := range ValidTypes {
		if valid == t {
			return true
		}
	}
	return false
}
