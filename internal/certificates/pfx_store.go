package certificates

import (
	"crypto/x509"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// PFXExtractInfo descreve uma extração de .pfx salva por PFXExtractStore.Save — ver
// PFX-CERT-EXTRACTION-PLAN.md. "Nome" é escolhido livremente pelo usuário no momento da extração
// (não derivado do CN do certificado), diferente de ManualBackupStore (onde a "pasta" é o nome do
// Secret K8s de origem).
type PFXExtractInfo struct {
	ExtractID        string    `json:"extract_id"`
	Name             string    `json:"name"`
	ExtractedAt      time.Time `json:"extracted_at"`
	Subject          string    `json:"subject"`
	Issuer           string    `json:"issuer"`
	SerialNumber     string    `json:"serial_number"`
	NotAfter         time.Time `json:"not_after"`
	ChainLength      int       `json:"chain_length"`
	OriginalFilename string    `json:"original_filename,omitempty"`
	Comment          string    `json:"comment,omitempty"`
}

// PFXExtractStore guarda o resultado (tls.crt/tls.key) de extrações de .pfx feitas sob demanda
// pelo usuário. Estrutura em disco idêntica em espírito a ManualBackupStore — uma pasta por nome,
// organizada por timestamp — mas em base dir própria; a senha do .pfx NUNCA é persistida em
// lugar nenhum (usada uma única vez por ExtractPFX, descartada em seguida).
type PFXExtractStore struct {
	baseDir string
}

// NewPFXExtractStore cria (se necessário) ~/.k8s-hpa-manager/pfx-extracted-certs/.
func NewPFXExtractStore() (*PFXExtractStore, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("erro ao obter home dir: %w", err)
	}

	baseDir := filepath.Join(home, ".k8s-hpa-manager", "pfx-extracted-certs")
	if err := os.MkdirAll(baseDir, 0700); err != nil {
		return nil, fmt.Errorf("erro ao criar diretório de extrações .pfx %s: %w", baseDir, err)
	}

	return &PFXExtractStore{baseDir: baseDir}, nil
}

// Save salva tlsCrt/tlsKey (já extraídos por ExtractPFX) em <baseDir>/<name>/<timestamp>/, com
// nome original do arquivo .pfx e comentário opcionais. Nunca falha por causa de metadata: leaf
// já vem parseado (ExtractPFX devolve o *x509.Certificate junto), então os campos de
// subject/serial/expiração sempre vêm preenchidos aqui — ao contrário de ManualBackupStore, que
// tenta reparsear um PEM que pode estar corrompido.
func (p *PFXExtractStore) Save(name, comment, originalFilename string, tlsCrt, tlsKey []byte, leaf *x509.Certificate) (PFXExtractInfo, error) {
	return p.saveAt(name, comment, originalFilename, tlsCrt, tlsKey, leaf, time.Now())
}

func (p *PFXExtractStore) saveAt(name, comment, originalFilename string, tlsCrt, tlsKey []byte, leaf *x509.Certificate, now time.Time) (PFXExtractInfo, error) {
	if !validPathComponent(name) {
		return PFXExtractInfo{}, fmt.Errorf("nome de certificado inválido: %q", name)
	}

	dir, extractID := p.uniqueExtractDir(name, now)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return PFXExtractInfo{}, fmt.Errorf("erro ao criar diretório de extração: %w", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "tls.crt"), tlsCrt, 0600); err != nil {
		return PFXExtractInfo{}, fmt.Errorf("erro ao gravar tls.crt extraído: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tls.key"), tlsKey, 0600); err != nil {
		return PFXExtractInfo{}, fmt.Errorf("erro ao gravar tls.key extraído: %w", err)
	}

	chainLen := 1
	if certs, err := parsePEMChain(tlsCrt); err == nil {
		chainLen = len(certs)
	}

	info := PFXExtractInfo{
		ExtractID:        extractID,
		Name:             name,
		ExtractedAt:      now,
		ChainLength:      chainLen,
		OriginalFilename: originalFilename,
		Comment:          comment,
	}
	if leaf != nil {
		info.Subject = certSubjectDisplayName(leaf)
		info.Issuer = leaf.Issuer.CommonName
		info.SerialNumber = formatSerialNumber(leaf.SerialNumber)
		info.NotAfter = leaf.NotAfter
	}

	metaBytes, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return PFXExtractInfo{}, fmt.Errorf("erro ao serializar metadata da extração: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), metaBytes, 0600); err != nil {
		return PFXExtractInfo{}, fmt.Errorf("erro ao gravar metadata.json da extração: %w", err)
	}

	return info, nil
}

// uniqueExtractDir — mesma lógica de RollbackStore.uniqueBackupDir/ManualBackupStore.uniqueBackupDir
// (sufixa -2/-3/... em colisão de timestamp).
func (p *PFXExtractStore) uniqueExtractDir(name string, at time.Time) (dir, extractID string) {
	base := filepath.Join(p.baseDir, name)
	id := at.UTC().Format("2006-01-02T15-04-05")
	for i := 2; ; i++ {
		candidate := filepath.Join(base, id)
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate, id
		}
		id = fmt.Sprintf("%s-%d", at.UTC().Format("2006-01-02T15-04-05"), i)
	}
}

// ListNames lista os nomes que têm ao menos 1 extração salva — mesmo papel de
// ManualBackupStore.ListSecretsWithBackups. Ordenado alfabeticamente. Nunca retorna nil.
func (p *PFXExtractStore) ListNames() ([]string, error) {
	entries, err := os.ReadDir(p.baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("erro ao listar pastas de extração .pfx: %w", err)
	}

	result := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			result = append(result, e.Name())
		}
	}
	sort.Strings(result)
	return result, nil
}

// List retorna as extrações de name, mais recente primeiro. Nunca retorna nil.
func (p *PFXExtractStore) List(name string) ([]PFXExtractInfo, error) {
	if !validPathComponent(name) {
		return nil, fmt.Errorf("nome de certificado inválido: %q", name)
	}

	dir := filepath.Join(p.baseDir, name)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []PFXExtractInfo{}, nil
		}
		return nil, fmt.Errorf("erro ao listar extrações de %s: %w", name, err)
	}

	result := make([]PFXExtractInfo, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name(), "metadata.json"))
		if err != nil {
			continue
		}
		var info PFXExtractInfo
		if err := json.Unmarshal(data, &info); err != nil {
			continue
		}
		result = append(result, info)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].ExtractedAt.After(result[j].ExtractedAt)
	})

	return result, nil
}

// Get lê o conteúdo bruto (tls.crt/tls.key) e a metadata de uma extração específica.
func (p *PFXExtractStore) Get(name, extractID string) (tlsCrt, tlsKey []byte, meta PFXExtractInfo, err error) {
	if !validPathComponent(name) || !validPathComponent(extractID) {
		return nil, nil, PFXExtractInfo{}, fmt.Errorf("extract_id ou nome inválido")
	}

	dir := filepath.Join(p.baseDir, name, extractID)

	tlsCrt, err = os.ReadFile(filepath.Join(dir, "tls.crt"))
	if err != nil {
		return nil, nil, PFXExtractInfo{}, fmt.Errorf("erro ao ler tls.crt da extração %s: %w", extractID, err)
	}
	tlsKey, err = os.ReadFile(filepath.Join(dir, "tls.key"))
	if err != nil {
		return nil, nil, PFXExtractInfo{}, fmt.Errorf("erro ao ler tls.key da extração %s: %w", extractID, err)
	}

	if metaBytes, mErr := os.ReadFile(filepath.Join(dir, "metadata.json")); mErr == nil {
		_ = json.Unmarshal(metaBytes, &meta)
	}

	return tlsCrt, tlsKey, meta, nil
}

// UpdateComment atualiza só o comentário de uma extração já salva — não toca no PEM salvo, só na
// metadata.json. Mesmo padrão de ManualBackupStore.UpdateComment.
func (p *PFXExtractStore) UpdateComment(name, extractID, comment string) error {
	if !validPathComponent(name) || !validPathComponent(extractID) {
		return fmt.Errorf("extract_id ou nome inválido")
	}

	metaPath := filepath.Join(p.baseDir, name, extractID, "metadata.json")
	data, err := os.ReadFile(metaPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("extração não encontrada: %s/%s", name, extractID)
		}
		return fmt.Errorf("erro ao ler metadata da extração %s: %w", extractID, err)
	}

	var info PFXExtractInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return fmt.Errorf("erro ao parsear metadata da extração %s: %w", extractID, err)
	}
	info.Comment = comment

	metaBytes, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return fmt.Errorf("erro ao serializar metadata da extração: %w", err)
	}
	return os.WriteFile(metaPath, metaBytes, 0600)
}

// Delete remove por completo uma extração (tls.crt/tls.key/metadata.json) — ação destrutiva e
// irreversível, ao contrário de UpdateComment. Sempre atrás de confirmação no frontend, mesmo
// padrão de ManualBackupStore.Delete/RollbackStore.
func (p *PFXExtractStore) Delete(name, extractID string) error {
	if !validPathComponent(name) || !validPathComponent(extractID) {
		return fmt.Errorf("extract_id ou nome inválido")
	}

	dir := filepath.Join(p.baseDir, name, extractID)
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("extração não encontrada: %s/%s", name, extractID)
		}
		return fmt.Errorf("erro ao verificar extração %s: %w", extractID, err)
	}
	return os.RemoveAll(dir)
}
