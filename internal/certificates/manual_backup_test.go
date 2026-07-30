package certificates

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newTestManualBackupStore(t *testing.T) *ManualBackupStore {
	t.Helper()
	dir := t.TempDir()
	return &ManualBackupStore{baseDir: dir}
}

func TestManualBackupStore_SaveListGet_RoundTrip(t *testing.T) {
	store := newTestManualBackupStore(t)
	now := time.Now()

	leaf := genCert(t, "manual.example.com", false, now.Add(-time.Hour), now.Add(24*time.Hour), nil)
	secret := testSecretFromCert(leaf)

	info, err := store.Save("meu-cluster", "meu-ns", "MEU-CERT-TLS", "backup antes de testar renovação", secret)
	if err != nil {
		t.Fatalf("Save falhou: %v", err)
	}
	if info.BackupID == "" {
		t.Fatal("esperava BackupID não-vazio")
	}
	if info.Subject != "manual.example.com" {
		t.Errorf("Subject = %q, esperado %q", info.Subject, "manual.example.com")
	}
	if info.Comment != "backup antes de testar renovação" {
		t.Errorf("Comment = %q, não persistiu corretamente", info.Comment)
	}

	list, err := store.List("MEU-CERT-TLS")
	if err != nil {
		t.Fatalf("List falhou: %v", err)
	}
	if len(list) != 1 || list[0].Comment != info.Comment {
		t.Fatalf("List não retornou o comentário esperado: %+v", list)
	}

	gotCrt, gotKey, meta, err := store.Get("MEU-CERT-TLS", info.BackupID)
	if err != nil {
		t.Fatalf("Get falhou: %v", err)
	}
	if string(gotCrt) != string(secret.Data["tls.crt"]) || string(gotKey) != string(secret.Data["tls.key"]) {
		t.Error("conteúdo recuperado não bate com o original")
	}
	if meta.Cluster != "meu-cluster" || meta.Namespace != "meu-ns" {
		t.Errorf("metadata incompleta: %+v", meta)
	}
}

func TestManualBackupStore_Save_ComentarioVazio(t *testing.T) {
	store := newTestManualBackupStore(t)
	now := time.Now()
	leaf := genCert(t, "sem-comentario.example.com", false, now.Add(-time.Hour), now.Add(24*time.Hour), nil)
	secret := testSecretFromCert(leaf)

	info, err := store.Save("c", "ns", "SECRET-SEM-COMENTARIO", "", secret)
	if err != nil {
		t.Fatalf("Save falhou: %v", err)
	}
	if info.Comment != "" {
		t.Errorf("esperava Comment vazio, obtive %q", info.Comment)
	}
}

func TestManualBackupStore_ListSecretsWithBackups(t *testing.T) {
	store := newTestManualBackupStore(t)
	now := time.Now()

	for _, name := range []string{"SECRET-B", "SECRET-A", "SECRET-C"} {
		leaf := genCert(t, name, false, now.Add(-time.Hour), now.Add(24*time.Hour), nil)
		secret := testSecretFromCert(leaf)
		if _, err := store.Save("c", "ns", name, "", secret); err != nil {
			t.Fatalf("Save(%s) falhou: %v", name, err)
		}
	}

	secrets, err := store.ListSecretsWithBackups()
	if err != nil {
		t.Fatalf("ListSecretsWithBackups falhou: %v", err)
	}
	want := []string{"SECRET-A", "SECRET-B", "SECRET-C"} // ordenado alfabeticamente
	if len(secrets) != len(want) {
		t.Fatalf("esperava %d secrets, obtive %d: %v", len(want), len(secrets), secrets)
	}
	for i, w := range want {
		if secrets[i] != w {
			t.Errorf("secrets[%d] = %q, esperado %q", i, secrets[i], w)
		}
	}
}

func TestManualBackupStore_ListSecretsWithBackups_Vazio(t *testing.T) {
	store := newTestManualBackupStore(t)
	secrets, err := store.ListSecretsWithBackups()
	if err != nil {
		t.Fatalf("não deveria falhar sem nenhum backup: %v", err)
	}
	if secrets == nil {
		t.Error("não deve retornar nil — deve retornar slice vazio")
	}
	if len(secrets) != 0 {
		t.Errorf("esperava lista vazia, obtive %d", len(secrets))
	}
}

func TestManualBackupStore_Get_RejeitaPathTraversal(t *testing.T) {
	store := newTestManualBackupStore(t)

	if _, _, _, err := store.Get("../escape", "2026-01-01T00-00-00"); err == nil {
		t.Error("Get deveria rejeitar secretName com path traversal")
	}
	if _, _, _, err := store.Get("SECRET-X", "../../escape"); err == nil {
		t.Error("Get deveria rejeitar backupID com path traversal")
	}
}

func TestManualBackupStore_FilePermissions(t *testing.T) {
	store := newTestManualBackupStore(t)
	now := time.Now()
	leaf := genCert(t, "perm.example.com", false, now.Add(-time.Hour), now.Add(24*time.Hour), nil)
	secret := testSecretFromCert(leaf)

	info, err := store.Save("c", "ns", "SECRET-PERM", "", secret)
	if err != nil {
		t.Fatalf("Save falhou: %v", err)
	}

	dir := filepath.Join(store.baseDir, "SECRET-PERM", info.BackupID)
	dirStat, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("erro ao stat do diretório: %v", err)
	}
	if perm := dirStat.Mode().Perm(); perm != 0700 {
		t.Errorf("permissão do diretório = %o, esperado 0700", perm)
	}

	for _, f := range []string{"tls.crt", "tls.key", "metadata.json"} {
		fileStat, err := os.Stat(filepath.Join(dir, f))
		if err != nil {
			t.Fatalf("erro ao stat de %s: %v", f, err)
		}
		if perm := fileStat.Mode().Perm(); perm != 0600 {
			t.Errorf("permissão de %s = %o, esperado 0600", f, perm)
		}
	}
}

func TestManualBackupStore_List_OrdenaMaisRecentePrimeiro(t *testing.T) {
	store := newTestManualBackupStore(t)
	now := time.Now()
	leaf := genCert(t, "ordem.example.com", false, now.Add(-time.Hour), now.Add(24*time.Hour), nil)
	secret := testSecretFromCert(leaf)

	var ids []string
	for i := 0; i < 3; i++ {
		info, err := store.Save("c", "ns", "SECRET-ORDEM", "", secret)
		if err != nil {
			t.Fatalf("Save #%d falhou: %v", i, err)
		}
		ids = append(ids, info.BackupID)
		time.Sleep(2 * time.Millisecond)
	}

	list, err := store.List("SECRET-ORDEM")
	if err != nil {
		t.Fatalf("List falhou: %v", err)
	}
	if len(list) != 3 || list[0].BackupID != ids[2] {
		t.Fatalf("esperava o mais recente primeiro (%s), obtive %+v", ids[2], list)
	}
}
