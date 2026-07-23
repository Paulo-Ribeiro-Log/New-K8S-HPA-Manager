package config

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

const fakeKubeconfigYAML = `apiVersion: v1
kind: Config
clusters:
- name: fake-cluster
  cluster:
    server: https://fake.example.com
contexts:
- name: fake-context
  context:
    cluster: fake-cluster
current-context: fake-context
`

// TestSnapshotKubeconfig_SetsKubeconfigEnvVar cobre o motivo real de existir: chamadas
// exec.Command("kubectl", ...) espalhadas pelo código que não recebem --kubeconfig explícito
// (ex: describe de node, VPA, gateway, secrets, port-forward) dependem da variável de
// ambiente KUBECONFIG do processo pra resolver a cópia privada em vez do arquivo
// compartilhado. t.Setenv restaura o valor original ao final do teste automaticamente.
func TestSnapshotKubeconfig_SetsKubeconfigEnvVar(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("KUBECONFIG", "")

	sourcePath := filepath.Join(home, "source-kubeconfig.yaml")
	if err := os.WriteFile(sourcePath, []byte(fakeKubeconfigYAML), 0600); err != nil {
		t.Fatalf("failed to write source kubeconfig: %v", err)
	}

	dest, err := snapshotKubeconfig(sourcePath)
	if err != nil {
		t.Fatalf("snapshotKubeconfig() error = %v", err)
	}

	if got := os.Getenv("KUBECONFIG"); got != dest {
		t.Errorf("KUBECONFIG env var = %q, want %q", got, dest)
	}
}

func TestSnapshotKubeconfig_CopiesToAppDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	sourcePath := filepath.Join(home, "source-kubeconfig.yaml")
	if err := os.WriteFile(sourcePath, []byte(fakeKubeconfigYAML), 0600); err != nil {
		t.Fatalf("failed to write source kubeconfig: %v", err)
	}

	dest, err := snapshotKubeconfig(sourcePath)
	if err != nil {
		t.Fatalf("snapshotKubeconfig() error = %v", err)
	}

	wantDest := filepath.Join(home, ".k8s-hpa-manager", "kubeconfig")
	if dest != wantDest {
		t.Errorf("dest = %q, want %q", dest, wantDest)
	}

	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("failed to read snapshot: %v", err)
	}
	if string(got) != fakeKubeconfigYAML {
		t.Errorf("snapshot content mismatch:\ngot:  %q\nwant: %q", got, fakeKubeconfigYAML)
	}

	info, err := os.Stat(dest)
	if err != nil {
		t.Fatalf("failed to stat snapshot: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("snapshot permissions = %o, want 0600", perm)
	}
}

func TestSnapshotKubeconfig_NoLeftoverTempFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	sourcePath := filepath.Join(home, "source-kubeconfig.yaml")
	if err := os.WriteFile(sourcePath, []byte(fakeKubeconfigYAML), 0600); err != nil {
		t.Fatalf("failed to write source kubeconfig: %v", err)
	}

	if _, err := snapshotKubeconfig(sourcePath); err != nil {
		t.Fatalf("snapshotKubeconfig() error = %v", err)
	}

	appDir := filepath.Join(home, ".k8s-hpa-manager")
	entries, err := os.ReadDir(appDir)
	if err != nil {
		t.Fatalf("failed to read app dir: %v", err)
	}
	for _, e := range entries {
		if e.Name() != "kubeconfig" {
			t.Errorf("unexpected leftover file in app dir: %s", e.Name())
		}
	}
}

func TestSnapshotKubeconfig_EmptySourceFallsBackToDefaultKubeconfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	defaultPath := filepath.Join(home, ".kube", "config")
	if err := os.MkdirAll(filepath.Dir(defaultPath), 0700); err != nil {
		t.Fatalf("failed to create .kube dir: %v", err)
	}
	if err := os.WriteFile(defaultPath, []byte(fakeKubeconfigYAML), 0600); err != nil {
		t.Fatalf("failed to write default kubeconfig: %v", err)
	}

	dest, err := snapshotKubeconfig("")
	if err != nil {
		t.Fatalf("snapshotKubeconfig(\"\") error = %v", err)
	}

	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("failed to read snapshot: %v", err)
	}
	if string(got) != fakeKubeconfigYAML {
		t.Errorf("snapshot content mismatch when falling back to default kubeconfig path")
	}
}

func TestSnapshotKubeconfig_MissingSourceReturnsError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	_, err := snapshotKubeconfig(filepath.Join(home, "does-not-exist.yaml"))
	if err == nil {
		t.Fatal("snapshotKubeconfig() error = nil, want error for missing source file")
	}
}

func TestSnapshotKubeconfig_RecreatedOnEachCall(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	sourcePath := filepath.Join(home, "source-kubeconfig.yaml")
	if err := os.WriteFile(sourcePath, []byte(fakeKubeconfigYAML), 0600); err != nil {
		t.Fatalf("failed to write source kubeconfig: %v", err)
	}
	if _, err := snapshotKubeconfig(sourcePath); err != nil {
		t.Fatalf("first snapshotKubeconfig() error = %v", err)
	}

	updated := fakeKubeconfigYAML + "# updated\n"
	if err := os.WriteFile(sourcePath, []byte(updated), 0600); err != nil {
		t.Fatalf("failed to update source kubeconfig: %v", err)
	}

	dest, err := snapshotKubeconfig(sourcePath)
	if err != nil {
		t.Fatalf("second snapshotKubeconfig() error = %v", err)
	}

	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("failed to read snapshot: %v", err)
	}
	if string(got) != updated {
		t.Errorf("snapshot was not recreated with updated source content")
	}
}

// TestSnapshotKubeconfig_ConcurrentCallsDoNotCorrupt simula o cenário documentado em
// snapshotKubeconfig: mais de um KubeConfigManager criado ao mesmo tempo no mesmo processo
// (ex: cordon/drain instanciando um manager próprio por request). Nenhuma chamada concorrente
// deve falhar, e o arquivo final deve sempre estar íntegro (nunca um YAML parcial/misturado).
func TestSnapshotKubeconfig_ConcurrentCallsDoNotCorrupt(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	sourcePath := filepath.Join(home, "source-kubeconfig.yaml")
	if err := os.WriteFile(sourcePath, []byte(fakeKubeconfigYAML), 0600); err != nil {
		t.Fatalf("failed to write source kubeconfig: %v", err)
	}

	const goroutines = 20
	var wg sync.WaitGroup
	errs := make([]error, goroutines)
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, err := snapshotKubeconfig(sourcePath)
			errs[idx] = err
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: snapshotKubeconfig() error = %v", i, err)
		}
	}

	dest := filepath.Join(home, ".k8s-hpa-manager", "kubeconfig")
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("failed to read snapshot after concurrent calls: %v", err)
	}
	if string(got) != fakeKubeconfigYAML {
		t.Errorf("snapshot content corrupted after concurrent calls: %q", got)
	}

	appDir := filepath.Join(home, ".k8s-hpa-manager")
	entries, err := os.ReadDir(appDir)
	if err != nil {
		t.Fatalf("failed to read app dir: %v", err)
	}
	for _, e := range entries {
		if e.Name() != "kubeconfig" {
			t.Errorf("unexpected leftover file after concurrent calls: %s", e.Name())
		}
	}
}
