// Package rollbackfiles implementa a "biblioteca de arquivos de rollback" — pedido explícito do
// usuário depois de usar o Modo Nexus do Rollback de Deployment: (1) baixar (persistir no
// servidor) artefatos encontrados no Nexus pra reuso futuro, (2) um modo "Rollback Manual" que
// lista/seleciona arquivos dessa pasta gerenciada OU de qualquer outro diretório do servidor
// ("arquivos que já temos salvo de outras ações de rollback"), e (3) editar esses YAMLs.
//
// Estrutura em disco: ~/.k8s-hpa-manager/rollback-artifacts/<arquivo>.yaml — pasta ÚNICA e plana
// (sem subpastas por deployment/cluster — os próprios nomes de arquivo, herdados do Nexus ou
// escolhidos pelo usuário ao salvar manualmente, já carregam essa identificação, mesma convenção
// de nomes já usada em "continuousdeploy-history"). Sem metadata.json — ModTime do próprio arquivo
// já é informação suficiente (nome + data de modificação), diferente de ManualBackupStore (certs),
// que precisa de metadata estruturada (subject/serial/expiração) que não existe pra um YAML de
// Deployment solto.
package rollbackfiles

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// FileEntry descreve um arquivo YAML encontrado — tanto na pasta gerenciada quanto num diretório
// externo navegado sob demanda.
type FileEntry struct {
	Name       string    `json:"name"`
	Path       string    `json:"path"` // caminho absoluto real no disco do servidor
	Size       int64     `json:"size"`
	ModifiedAt time.Time `json:"modifiedAt"`
}

// Store gerencia a pasta padrão de artefatos de rollback.
type Store struct {
	baseDir string
}

// NewStore cria (se necessário) ~/.k8s-hpa-manager/rollback-artifacts/.
func NewStore() (*Store, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("erro ao obter home dir: %w", err)
	}
	baseDir := filepath.Join(home, ".k8s-hpa-manager", "rollback-artifacts")
	if err := os.MkdirAll(baseDir, 0700); err != nil {
		return nil, fmt.Errorf("erro ao criar diretório de artefatos de rollback %s: %w", baseDir, err)
	}
	return &Store{baseDir: baseDir}, nil
}

// BaseDir expõe o caminho absoluto da pasta padrão — usado só pra exibição na UI (mostrar ao
// usuário onde os arquivos ficam salvos no servidor).
func (s *Store) BaseDir() string {
	return s.baseDir
}

// validFileName rejeita nomes que poderiam escapar de baseDir via path traversal, e exige
// extensão .yaml/.yml — mesmo princípio de validPathComponent já usado em internal/certificates,
// reaplicado aqui (pacote novo, sem import cruzado entre os dois por conveniência de um helper tão
// pequeno).
func validFileName(name string) bool {
	if name == "" || strings.ContainsAny(name, "/\\") || strings.Contains(name, "..") {
		return false
	}
	lower := strings.ToLower(name)
	return strings.HasSuffix(lower, ".yaml") || strings.HasSuffix(lower, ".yml")
}

func listYAMLFiles(dir string) ([]FileEntry, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []FileEntry{}, nil
		}
		return nil, fmt.Errorf("erro ao listar diretório %s: %w", dir, err)
	}

	result := make([]FileEntry, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !validFileName(e.Name()) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		result = append(result, FileEntry{
			Name:       e.Name(),
			Path:       filepath.Join(dir, e.Name()),
			Size:       info.Size(),
			ModifiedAt: info.ModTime(),
		})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ModifiedAt.After(result[j].ModifiedAt) })
	return result, nil
}

// List retorna os arquivos .yaml/.yml da pasta padrão, mais recente primeiro. Nunca retorna nil.
func (s *Store) List() ([]FileEntry, error) {
	return listYAMLFiles(s.baseDir)
}

// Save persiste um novo arquivo na pasta padrão — usado pelo botão "Baixar" do Modo Nexus (o
// conteúdo já vem do Nexus, aqui só persiste) e por um eventual "Salvar como" manual. Dedupe por
// sufixo numérico (-2/-3/...) quando o nome já existe — mesmo padrão de uniqueBackupDir em
// ManualBackupStore, mas aplicado ao NOME do arquivo em vez de um diretório de timestamp (não faz
// sentido versionar por timestamp aqui — o nome já é uma identidade única o bastante na convenção
// do Nexus, então uma colisão real normalmente significa "mesmo arquivo baixado de novo").
func (s *Store) Save(name string, content []byte) (FileEntry, error) {
	if !validFileName(name) {
		return FileEntry{}, fmt.Errorf("nome de arquivo inválido (precisa terminar em .yaml/.yml, sem separadores de caminho): %q", name)
	}

	finalName := name
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	for i := 2; ; i++ {
		candidate := filepath.Join(s.baseDir, finalName)
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			break
		}
		finalName = fmt.Sprintf("%s-%d%s", base, i, ext)
	}

	path := filepath.Join(s.baseDir, finalName)
	if err := os.WriteFile(path, content, 0600); err != nil {
		return FileEntry{}, fmt.Errorf("erro ao salvar %s: %w", finalName, err)
	}

	info, err := os.Stat(path)
	if err != nil {
		return FileEntry{}, fmt.Errorf("erro ao ler metadata do arquivo salvo: %w", err)
	}
	return FileEntry{Name: finalName, Path: path, Size: info.Size(), ModifiedAt: info.ModTime()}, nil
}

// Read lê o conteúdo de um arquivo da pasta padrão.
func (s *Store) Read(name string) ([]byte, error) {
	if !validFileName(name) {
		return nil, fmt.Errorf("nome de arquivo inválido: %q", name)
	}
	return os.ReadFile(filepath.Join(s.baseDir, name))
}

// Write sobrescreve o conteúdo de um arquivo já existente na pasta padrão — usado pelo "Salvar"
// da edição via Monaco.
func (s *Store) Write(name string, content []byte) error {
	if !validFileName(name) {
		return fmt.Errorf("nome de arquivo inválido: %q", name)
	}
	return os.WriteFile(filepath.Join(s.baseDir, name), content, 0600)
}

// Delete remove um arquivo da pasta padrão.
func (s *Store) Delete(name string) error {
	if !validFileName(name) {
		return fmt.Errorf("nome de arquivo inválido: %q", name)
	}
	path := filepath.Join(s.baseDir, name)
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("arquivo não encontrado: %s", name)
		}
		return fmt.Errorf("erro ao verificar arquivo %s: %w", name, err)
	}
	return os.Remove(path)
}

// ─── Diretório externo (arbitrário) — "arquivos que já temos salvo de outras ações de rollback" ──
//
// Deliberadamente FORA da pasta gerenciada — pedido explícito do usuário. Sempre exige path
// absoluto; só lista/lê/escreve arquivos .yaml/.yml (nunca qualquer outro arquivo do servidor,
// mesmo com acesso de SRE — reduz o raio de exposição desta funcionalidade, que já é
// intencionalmente ampla). Rotas HTTP que expõem essas 3 funções ficam atrás de RequireSREGroup(),
// mesmo nível de confiança já dado a outras ferramentas desta app com acesso ao host do servidor
// (ex: Command Runner).

// BrowseDirectory lista os arquivos .yaml/.yml de um diretório absoluto arbitrário do servidor
// (não recursivo).
func BrowseDirectory(dirPath string) ([]FileEntry, error) {
	if !filepath.IsAbs(dirPath) {
		return nil, fmt.Errorf("caminho precisa ser absoluto: %q", dirPath)
	}
	info, err := os.Stat(dirPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("diretório não encontrado: %s", dirPath)
		}
		return nil, fmt.Errorf("erro ao acessar diretório %s: %w", dirPath, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s não é um diretório", dirPath)
	}
	return listYAMLFiles(dirPath)
}

// ReadFileAtPath lê o conteúdo de um arquivo .yaml/.yml em qualquer caminho absoluto do servidor.
func ReadFileAtPath(path string) ([]byte, error) {
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("caminho precisa ser absoluto: %q", path)
	}
	if !validFileName(filepath.Base(path)) {
		return nil, fmt.Errorf("só é permitido ler arquivos .yaml/.yml: %q", path)
	}
	return os.ReadFile(path)
}

// WriteFileAtPath sobrescreve o conteúdo de um arquivo .yaml/.yml já existente em qualquer
// caminho absoluto do servidor — usado pelo "Salvar" da edição via Monaco quando a origem é um
// diretório externo. Nunca CRIA um arquivo novo fora da pasta gerenciada (só sobrescreve um que já
// existia e já foi listado via BrowseDirectory) — reduz a superfície de "gravar em qualquer lugar
// que eu quiser" pra "só onde o usuário já apontou e confirmou que existe".
func WriteFileAtPath(path string, content []byte) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("caminho precisa ser absoluto: %q", path)
	}
	if !validFileName(filepath.Base(path)) {
		return fmt.Errorf("só é permitido escrever em arquivos .yaml/.yml: %q", path)
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("arquivo não encontrado: %s", path)
		}
		return fmt.Errorf("erro ao verificar arquivo %s: %w", path, err)
	}
	return os.WriteFile(path, content, 0600)
}
