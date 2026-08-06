package certificates

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	corev1 "k8s.io/api/core/v1"
)

// rollbackDefaultKeepPerSecret é quantos backups mais recentes o Prune mantém por secretName.
const rollbackDefaultKeepPerSecret = 20

// RollbackBackupInfo descreve um backup salvo por RollbackStore.Backup — o conteúdo (tls.crt/
// tls.key) fica nos arquivos irmãos de metadata.json, nunca embutido aqui.
type RollbackBackupInfo struct {
	BackupID     string    `json:"backup_id"`
	Cluster      string    `json:"cluster"`
	Namespace    string    `json:"namespace"`
	SecretName   string    `json:"secret_name"`
	BackedUpAt   time.Time `json:"backed_up_at"`
	Subject      string    `json:"subject"`       // vazio se o cert anterior estava corrompido
	SerialNumber string    `json:"serial_number"` // hex, mesmo formato de CertificateInfo.SerialNumber
	NotAfter     time.Time `json:"not_after"`
}

// RollbackStore guarda cópias do conteúdo ANTERIOR de um Secret TLS antes de ele ser sobrescrito
// (upload manual ou renovação via AWX), permitindo reverter uma atualização com problema.
// Estrutura em disco: <baseDir>/<secretName>/<backupID>/{tls.crt,tls.key,metadata.json} — mesma
// convenção de permissão restrita (0700 dirs / 0600 arquivos) já usada para dado sensível como
// jwt.secret/awx_credentials.json.
type RollbackStore struct {
	baseDir string
}

// NewRollbackStore cria (se necessário) ~/.k8s-hpa-manager/rollback-certs/ e inicia a limpeza
// periódica de backups antigos em background.
func NewRollbackStore() (*RollbackStore, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("erro ao obter home dir: %w", err)
	}

	baseDir := filepath.Join(home, ".k8s-hpa-manager", "rollback-certs")
	if err := os.MkdirAll(baseDir, 0700); err != nil {
		return nil, fmt.Errorf("erro ao criar diretório de rollback %s: %w", baseDir, err)
	}

	s := &RollbackStore{baseDir: baseDir}
	go s.cleanupLoop()
	return s, nil
}

// cleanupLoop roda Prune uma vez ao iniciar e depois a cada 24h — mesmo padrão de
// internal/storage/ai_history_store.go (cleanupLoop/runCleanup).
func (r *RollbackStore) cleanupLoop() {
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	if err := r.Prune(rollbackDefaultKeepPerSecret); err != nil {
		log.Warn().Err(err).Msg("erro na limpeza periódica de backups de certificado")
	}
	for range ticker.C {
		if err := r.Prune(rollbackDefaultKeepPerSecret); err != nil {
			log.Warn().Err(err).Msg("erro na limpeza periódica de backups de certificado")
		}
	}
}

// validPathComponent rejeita nomes que poderiam escapar de baseDir/secretName via path traversal
// — secretName e backupID chegam de parâmetros de URL, nunca confiar neles direto num filepath.Join.
func validPathComponent(s string) bool {
	return s != "" && !strings.ContainsAny(s, "/\\") && !strings.Contains(s, "..")
}

// Backup salva o conteúdo ATUAL de secret (antes de ser sobrescrito) em
// <baseDir>/<secretName>/<timestamp>/. Nunca falha por causa de metadata: se o cert anterior
// estiver corrompido/ilegível, os bytes brutos ainda são salvos e os campos de metadata (subject/
// serial/expiração) ficam vazios.
func (r *RollbackStore) Backup(cluster, namespace, secretName string, secret *corev1.Secret) (RollbackBackupInfo, error) {
	return r.backupAt(cluster, namespace, secretName, secret, time.Now())
}

// backupAt é o corpo real de Backup, parametrizado por `now` — separado só para permitir que os
// testes forcem uma colisão de timestamp de propósito (ver TestRollbackStore_Backup_ColisaoDeID).
func (r *RollbackStore) backupAt(cluster, namespace, secretName string, secret *corev1.Secret, now time.Time) (RollbackBackupInfo, error) {
	if !validPathComponent(secretName) {
		return RollbackBackupInfo{}, fmt.Errorf("nome de secret inválido: %q", secretName)
	}

	dir, backupID := r.uniqueBackupDir(secretName, now)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return RollbackBackupInfo{}, fmt.Errorf("erro ao criar diretório de backup: %w", err)
	}

	tlsCrt := secret.Data["tls.crt"]
	tlsKey := secret.Data["tls.key"]

	if err := os.WriteFile(filepath.Join(dir, "tls.crt"), tlsCrt, 0600); err != nil {
		return RollbackBackupInfo{}, fmt.Errorf("erro ao gravar tls.crt do backup: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tls.key"), tlsKey, 0600); err != nil {
		return RollbackBackupInfo{}, fmt.Errorf("erro ao gravar tls.key do backup: %w", err)
	}

	info := RollbackBackupInfo{
		BackupID:   backupID,
		Cluster:    cluster,
		Namespace:  namespace,
		SecretName: secretName,
		BackedUpAt: now,
	}

	if certs, err := parsePEMChain(tlsCrt); err == nil && len(certs) > 0 {
		leaf := certs[0]
		info.Subject = certSubjectDisplayName(leaf)
		info.SerialNumber = formatSerialNumber(leaf.SerialNumber)
		info.NotAfter = leaf.NotAfter
	}

	metaBytes, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return RollbackBackupInfo{}, fmt.Errorf("erro ao serializar metadata do backup: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), metaBytes, 0600); err != nil {
		return RollbackBackupInfo{}, fmt.Errorf("erro ao gravar metadata.json do backup: %w", err)
	}

	return info, nil
}

// uniqueBackupDir monta o diretório de destino a partir do timestamp; em caso de colisão (2
// backups do mesmo secret no mesmo segundo), sufixa "-2", "-3", ... até achar um nome livre, em
// vez de sobrescrever silenciosamente um backup anterior.
func (r *RollbackStore) uniqueBackupDir(secretName string, at time.Time) (dir, backupID string) {
	base := filepath.Join(r.baseDir, secretName)
	id := at.UTC().Format("2006-01-02T15-04-05")
	for i := 2; ; i++ {
		candidate := filepath.Join(base, id)
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate, id
		}
		id = fmt.Sprintf("%s-%d", at.UTC().Format("2006-01-02T15-04-05"), i)
	}
}

// List retorna os backups de secretName, mais recente primeiro. Nunca retorna nil — slice vazio
// vira "[]" no JSON, não "null" (bug já documentado no projeto: frontend checa `campo &&`, que não
// cobre null).
func (r *RollbackStore) List(secretName string) ([]RollbackBackupInfo, error) {
	if !validPathComponent(secretName) {
		return nil, fmt.Errorf("nome de secret inválido: %q", secretName)
	}

	dir := filepath.Join(r.baseDir, secretName)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []RollbackBackupInfo{}, nil
		}
		return nil, fmt.Errorf("erro ao listar backups de %s: %w", secretName, err)
	}

	result := make([]RollbackBackupInfo, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name(), "metadata.json"))
		if err != nil {
			continue // backup sem metadata legível — pula, não quebra a listagem inteira
		}
		var info RollbackBackupInfo
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

// Get lê o conteúdo bruto (tls.crt/tls.key) e a metadata de um backup específico.
func (r *RollbackStore) Get(secretName, backupID string) (tlsCrt, tlsKey []byte, meta RollbackBackupInfo, err error) {
	if !validPathComponent(secretName) || !validPathComponent(backupID) {
		return nil, nil, RollbackBackupInfo{}, fmt.Errorf("backup_id ou secret_name inválido")
	}

	dir := filepath.Join(r.baseDir, secretName, backupID)

	tlsCrt, err = os.ReadFile(filepath.Join(dir, "tls.crt"))
	if err != nil {
		return nil, nil, RollbackBackupInfo{}, fmt.Errorf("erro ao ler tls.crt do backup %s: %w", backupID, err)
	}
	tlsKey, err = os.ReadFile(filepath.Join(dir, "tls.key"))
	if err != nil {
		return nil, nil, RollbackBackupInfo{}, fmt.Errorf("erro ao ler tls.key do backup %s: %w", backupID, err)
	}

	if metaBytes, mErr := os.ReadFile(filepath.Join(dir, "metadata.json")); mErr == nil {
		_ = json.Unmarshal(metaBytes, &meta)
	}

	return tlsCrt, tlsKey, meta, nil
}

// Prune mantém só os keepPerSecret backups mais recentes de cada secret, apagando o resto — chamado
// periodicamente por cleanupLoop, mas também exposto pra uso direto em testes.
func (r *RollbackStore) Prune(keepPerSecret int) error {
	entries, err := os.ReadDir(r.baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("erro ao listar diretório de rollback: %w", err)
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		secretName := e.Name()
		backups, err := r.List(secretName)
		if err != nil {
			log.Warn().Err(err).Str("secret", secretName).Msg("erro ao listar backups para limpeza")
			continue
		}
		if len(backups) <= keepPerSecret {
			continue
		}
		for _, old := range backups[keepPerSecret:] {
			oldDir := filepath.Join(r.baseDir, secretName, old.BackupID)
			if err := os.RemoveAll(oldDir); err != nil {
				log.Warn().Err(err).Str("dir", oldDir).Msg("erro ao remover backup antigo de certificado")
			}
		}
	}

	return nil
}
