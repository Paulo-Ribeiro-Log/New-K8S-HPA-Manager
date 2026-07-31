package certificates

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	corev1 "k8s.io/api/core/v1"
)

// ManualBackupInfo descreve uma cópia salva manualmente por ManualBackupStore.Save — mecanismo
// separado do RollbackStore (Fase 2): disparado sob demanda pelo usuário (botão "Copiar para
// Backup"), não automaticamente antes de uma sobrescrita, e aceita um comentário livre.
type ManualBackupInfo struct {
	BackupID     string    `json:"backup_id"`
	Cluster      string    `json:"cluster"`
	Namespace    string    `json:"namespace"`
	SecretName   string    `json:"secret_name"`
	BackedUpAt   time.Time `json:"backed_up_at"`
	Subject      string    `json:"subject"`
	SerialNumber string    `json:"serial_number"`
	NotAfter     time.Time `json:"not_after"`
	Comment      string    `json:"comment,omitempty"`
}

// ManualBackupStore guarda cópias de certificados TLS escolhidas manualmente pelo usuário, à parte
// do RollbackStore (que só guarda o estado ANTERIOR automaticamente antes de uma sobrescrita).
// Estrutura em disco idêntica em espírito ao RollbackStore — uma pasta por secretName, organizada
// automaticamente — mas em base dir própria e sem Prune automático: são cópias deliberadas do
// usuário, não um buffer técnico rotativo, então não são apagadas sozinhas.
type ManualBackupStore struct {
	baseDir string
}

// NewManualBackupStore cria (se necessário) ~/.k8s-hpa-manager/manual-cert-backups/.
func NewManualBackupStore() (*ManualBackupStore, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("erro ao obter home dir: %w", err)
	}

	baseDir := filepath.Join(home, ".k8s-hpa-manager", "manual-cert-backups")
	if err := os.MkdirAll(baseDir, 0700); err != nil {
		return nil, fmt.Errorf("erro ao criar diretório de backup manual %s: %w", baseDir, err)
	}

	return &ManualBackupStore{baseDir: baseDir}, nil
}

// Save salva o conteúdo ATUAL de secret em <baseDir>/<secretName>/<timestamp>/, com um comentário
// opcional. Nunca falha por causa de metadata: se o cert estiver corrompido/ilegível, os bytes
// brutos ainda são salvos e os campos de metadata (subject/serial/expiração) ficam vazios.
func (m *ManualBackupStore) Save(cluster, namespace, secretName, comment string, secret *corev1.Secret) (ManualBackupInfo, error) {
	return m.saveAt(cluster, namespace, secretName, comment, secret, time.Now())
}

// saveAt é o corpo real de Save, parametrizado por `now` — separado só para os testes forçarem uma
// colisão de timestamp de propósito (mesmo padrão de RollbackStore.backupAt).
func (m *ManualBackupStore) saveAt(cluster, namespace, secretName, comment string, secret *corev1.Secret, now time.Time) (ManualBackupInfo, error) {
	if !validPathComponent(secretName) {
		return ManualBackupInfo{}, fmt.Errorf("nome de secret inválido: %q", secretName)
	}

	dir, backupID := m.uniqueBackupDir(secretName, now)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return ManualBackupInfo{}, fmt.Errorf("erro ao criar diretório de backup: %w", err)
	}

	tlsCrt := secret.Data["tls.crt"]
	tlsKey := secret.Data["tls.key"]

	if err := os.WriteFile(filepath.Join(dir, "tls.crt"), tlsCrt, 0600); err != nil {
		return ManualBackupInfo{}, fmt.Errorf("erro ao gravar tls.crt do backup: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tls.key"), tlsKey, 0600); err != nil {
		return ManualBackupInfo{}, fmt.Errorf("erro ao gravar tls.key do backup: %w", err)
	}

	info := ManualBackupInfo{
		BackupID:   backupID,
		Cluster:    cluster,
		Namespace:  namespace,
		SecretName: secretName,
		BackedUpAt: now,
		Comment:    comment,
	}

	if certs, err := parsePEMChain(tlsCrt); err == nil && len(certs) > 0 {
		leaf := certs[0]
		info.Subject = leaf.Subject.CommonName
		info.SerialNumber = formatSerialNumber(leaf.SerialNumber)
		info.NotAfter = leaf.NotAfter
	}

	metaBytes, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return ManualBackupInfo{}, fmt.Errorf("erro ao serializar metadata do backup: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), metaBytes, 0600); err != nil {
		return ManualBackupInfo{}, fmt.Errorf("erro ao gravar metadata.json do backup: %w", err)
	}

	return info, nil
}

// uniqueBackupDir — mesma lógica de RollbackStore.uniqueBackupDir (sufixa -2/-3/... em colisão).
func (m *ManualBackupStore) uniqueBackupDir(secretName string, at time.Time) (dir, backupID string) {
	base := filepath.Join(m.baseDir, secretName)
	id := at.UTC().Format("2006-01-02T15-04-05")
	for i := 2; ; i++ {
		candidate := filepath.Join(base, id)
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate, id
		}
		id = fmt.Sprintf("%s-%d", at.UTC().Format("2006-01-02T15-04-05"), i)
	}
}

// ListSecretsWithBackups lista os nomes de secret que têm ao menos 1 backup manual salvo —
// alimenta a navegação "entre pastas" na instalação manual, que não fica restrita ao secret
// atualmente selecionado (diferente do RollbackStore, cujo picker só lista backups do secret em
// foco). Ordenado alfabeticamente. Nunca retorna nil.
func (m *ManualBackupStore) ListSecretsWithBackups() ([]string, error) {
	entries, err := os.ReadDir(m.baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("erro ao listar pastas de backup manual: %w", err)
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

// List retorna os backups manuais de secretName, mais recente primeiro. Nunca retorna nil.
func (m *ManualBackupStore) List(secretName string) ([]ManualBackupInfo, error) {
	if !validPathComponent(secretName) {
		return nil, fmt.Errorf("nome de secret inválido: %q", secretName)
	}

	dir := filepath.Join(m.baseDir, secretName)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []ManualBackupInfo{}, nil
		}
		return nil, fmt.Errorf("erro ao listar backups de %s: %w", secretName, err)
	}

	result := make([]ManualBackupInfo, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name(), "metadata.json"))
		if err != nil {
			continue
		}
		var info ManualBackupInfo
		if err := json.Unmarshal(data, &info); err != nil {
			continue
		}
		result = append(result, info)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].BackedUpAt.After(result[j].BackedUpAt)
	})

	return result, nil
}

// Get lê o conteúdo bruto (tls.crt/tls.key) e a metadata de um backup manual específico.
func (m *ManualBackupStore) Get(secretName, backupID string) (tlsCrt, tlsKey []byte, meta ManualBackupInfo, err error) {
	if !validPathComponent(secretName) || !validPathComponent(backupID) {
		return nil, nil, ManualBackupInfo{}, fmt.Errorf("backup_id ou secret_name inválido")
	}

	dir := filepath.Join(m.baseDir, secretName, backupID)

	tlsCrt, err = os.ReadFile(filepath.Join(dir, "tls.crt"))
	if err != nil {
		return nil, nil, ManualBackupInfo{}, fmt.Errorf("erro ao ler tls.crt do backup %s: %w", backupID, err)
	}
	tlsKey, err = os.ReadFile(filepath.Join(dir, "tls.key"))
	if err != nil {
		return nil, nil, ManualBackupInfo{}, fmt.Errorf("erro ao ler tls.key do backup %s: %w", backupID, err)
	}

	if metaBytes, mErr := os.ReadFile(filepath.Join(dir, "metadata.json")); mErr == nil {
		_ = json.Unmarshal(metaBytes, &meta)
	}

	return tlsCrt, tlsKey, meta, nil
}

// UpdateComment atualiza só o comentário de um backup manual já salvo — não toca no PEM salvo, só
// na metadata.json. Usado pela ação "Editar nota" do picker (CertificateSourcePickerModal.tsx).
func (m *ManualBackupStore) UpdateComment(secretName, backupID, comment string) error {
	if !validPathComponent(secretName) || !validPathComponent(backupID) {
		return fmt.Errorf("backup_id ou secret_name inválido")
	}

	metaPath := filepath.Join(m.baseDir, secretName, backupID, "metadata.json")
	data, err := os.ReadFile(metaPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("backup não encontrado: %s/%s", secretName, backupID)
		}
		return fmt.Errorf("erro ao ler metadata do backup %s: %w", backupID, err)
	}

	var info ManualBackupInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return fmt.Errorf("erro ao parsear metadata do backup %s: %w", backupID, err)
	}
	info.Comment = comment

	metaBytes, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return fmt.Errorf("erro ao serializar metadata do backup: %w", err)
	}
	return os.WriteFile(metaPath, metaBytes, 0600)
}

// Delete remove por completo um backup manual (tls.crt/tls.key/metadata.json) — ação destrutiva e
// irreversível, ao contrário de UpdateComment. Usada pela ação "Remover" do picker, sempre atrás
// de confirmação no frontend (CertificateSourcePickerModal.tsx).
func (m *ManualBackupStore) Delete(secretName, backupID string) error {
	if !validPathComponent(secretName) || !validPathComponent(backupID) {
		return fmt.Errorf("backup_id ou secret_name inválido")
	}

	dir := filepath.Join(m.baseDir, secretName, backupID)
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("backup não encontrado: %s/%s", secretName, backupID)
		}
		return fmt.Errorf("erro ao verificar backup %s: %w", backupID, err)
	}
	return os.RemoveAll(dir)
}
