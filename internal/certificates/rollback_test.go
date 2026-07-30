package certificates

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
)

// newTestRollbackStore cria um RollbackStore isolado num diretório temporário — não usa
// NewRollbackStore() de propósito, pra não depender de ~/.k8s-hpa-manager real nem disparar o
// cleanupLoop em background durante os testes.
func newTestRollbackStore(t *testing.T) *RollbackStore {
	t.Helper()
	dir := t.TempDir()
	return &RollbackStore{baseDir: dir}
}

func testSecretFromCert(leaf *testCert) *corev1.Secret {
	return &corev1.Secret{
		Data: map[string][]byte{
			"tls.crt": concatPEM(leaf),
			"tls.key": leaf.keyPEM,
		},
	}
}

func TestRollbackStore_BackupListGet_RoundTrip(t *testing.T) {
	store := newTestRollbackStore(t)
	now := time.Now()

	leaf := genCert(t, "cert.example.com", false, now.Add(-time.Hour), now.Add(24*time.Hour), nil)
	secret := testSecretFromCert(leaf)

	info, err := store.Backup("meu-cluster", "meu-ns", "MEU-CERT-TLS", secret)
	if err != nil {
		t.Fatalf("Backup falhou: %v", err)
	}
	if info.BackupID == "" {
		t.Fatal("esperava BackupID não-vazio")
	}
	if info.Subject != "cert.example.com" {
		t.Errorf("Subject = %q, esperado %q", info.Subject, "cert.example.com")
	}
	if info.SerialNumber == "" {
		t.Error("esperava SerialNumber não-vazio")
	}

	list, err := store.List("MEU-CERT-TLS")
	if err != nil {
		t.Fatalf("List falhou: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("esperava 1 backup, obtive %d", len(list))
	}
	if list[0].BackupID != info.BackupID {
		t.Errorf("BackupID da lista = %q, esperado %q", list[0].BackupID, info.BackupID)
	}

	gotCrt, gotKey, meta, err := store.Get("MEU-CERT-TLS", info.BackupID)
	if err != nil {
		t.Fatalf("Get falhou: %v", err)
	}
	if string(gotCrt) != string(secret.Data["tls.crt"]) {
		t.Error("tls.crt recuperado não bate com o original")
	}
	if string(gotKey) != string(secret.Data["tls.key"]) {
		t.Error("tls.key recuperado não bate com o original")
	}
	if meta.Cluster != "meu-cluster" || meta.Namespace != "meu-ns" {
		t.Errorf("metadata incompleta: %+v", meta)
	}
}

func TestRollbackStore_List_SecretSemBackups(t *testing.T) {
	store := newTestRollbackStore(t)

	list, err := store.List("NUNCA-EXISTIU")
	if err != nil {
		t.Fatalf("List não deveria falhar para secret sem backups: %v", err)
	}
	if list == nil {
		t.Error("List não deve retornar nil — deve retornar slice vazio (evita null no JSON)")
	}
	if len(list) != 0 {
		t.Errorf("esperava lista vazia, obtive %d itens", len(list))
	}
}

func TestRollbackStore_List_OrdenaMaisRecentePrimeiro(t *testing.T) {
	store := newTestRollbackStore(t)
	now := time.Now()

	leaf := genCert(t, "a.example.com", false, now.Add(-time.Hour), now.Add(24*time.Hour), nil)
	secret := testSecretFromCert(leaf)

	var ids []string
	for i := 0; i < 3; i++ {
		info, err := store.Backup("cluster", "ns", "SECRET-X", secret)
		if err != nil {
			t.Fatalf("Backup #%d falhou: %v", i, err)
		}
		ids = append(ids, info.BackupID)
		time.Sleep(2 * time.Millisecond) // garante BackedUpAt estritamente crescente
	}

	list, err := store.List("SECRET-X")
	if err != nil {
		t.Fatalf("List falhou: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("esperava 3 backups, obtive %d", len(list))
	}
	if list[0].BackupID != ids[2] {
		t.Errorf("primeiro item da lista deveria ser o mais recente (%s), obtive %s", ids[2], list[0].BackupID)
	}
}

func TestRollbackStore_Backup_ColisaoDeID(t *testing.T) {
	store := newTestRollbackStore(t)
	now := time.Now()

	leaf := genCert(t, "b.example.com", false, now.Add(-time.Hour), now.Add(24*time.Hour), nil)
	secret := testSecretFromCert(leaf)

	dir1, id1 := store.uniqueBackupDir("SECRET-COLLIDE", now)
	if err := os.MkdirAll(dir1, 0700); err != nil {
		t.Fatalf("erro ao preparar colisão: %v", err)
	}

	info, err := store.backupAt("cluster", "ns", "SECRET-COLLIDE", secret, now)
	if err != nil {
		t.Fatalf("Backup com colisão de timestamp falhou: %v", err)
	}
	if info.BackupID == id1 {
		t.Errorf("esperava um BackupID diferente de %q após colisão, obtive o mesmo", id1)
	}
}

func TestRollbackStore_Get_RejeitaPathTraversal(t *testing.T) {
	store := newTestRollbackStore(t)

	if _, _, _, err := store.Get("../escape", "2026-01-01T00-00-00"); err == nil {
		t.Error("Get deveria rejeitar secretName com path traversal")
	}
	if _, _, _, err := store.Get("SECRET-X", "../../escape"); err == nil {
		t.Error("Get deveria rejeitar backupID com path traversal")
	}
}

func TestRollbackStore_FilePermissions(t *testing.T) {
	store := newTestRollbackStore(t)
	now := time.Now()

	leaf := genCert(t, "perm.example.com", false, now.Add(-time.Hour), now.Add(24*time.Hour), nil)
	secret := testSecretFromCert(leaf)

	info, err := store.Backup("cluster", "ns", "SECRET-PERM", secret)
	if err != nil {
		t.Fatalf("Backup falhou: %v", err)
	}

	dir := filepath.Join(store.baseDir, "SECRET-PERM", info.BackupID)

	dirStat, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("erro ao stat do diretório de backup: %v", err)
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

func TestRollbackStore_Prune_MantemSoMaisRecentes(t *testing.T) {
	store := newTestRollbackStore(t)
	now := time.Now()

	leaf := genCert(t, "prune.example.com", false, now.Add(-time.Hour), now.Add(24*time.Hour), nil)
	secret := testSecretFromCert(leaf)

	var ids []string
	for i := 0; i < 5; i++ {
		info, err := store.Backup("cluster", "ns", "SECRET-PRUNE", secret)
		if err != nil {
			t.Fatalf("Backup #%d falhou: %v", i, err)
		}
		ids = append(ids, info.BackupID)
		time.Sleep(2 * time.Millisecond)
	}

	if err := store.Prune(2); err != nil {
		t.Fatalf("Prune falhou: %v", err)
	}

	list, err := store.List("SECRET-PRUNE")
	if err != nil {
		t.Fatalf("List falhou: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("esperava 2 backups após Prune(2), obtive %d", len(list))
	}
	// Os 2 mantidos devem ser os 2 mais recentes (últimos da lista `ids`).
	kept := map[string]bool{list[0].BackupID: true, list[1].BackupID: true}
	if !kept[ids[3]] || !kept[ids[4]] {
		t.Errorf("Prune não manteve os backups mais recentes: mantidos=%v, esperado incluir %v e %v", kept, ids[3], ids[4])
	}
	if kept[ids[0]] {
		t.Error("Prune deveria ter removido o backup mais antigo")
	}
}
