package rollbackfiles

import (
	"os"
	"path/filepath"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	return &Store{baseDir: dir}
}

func TestStore_SaveAndList(t *testing.T) {
	s := newTestStore(t)

	entry, err := s.Save("deploy.yaml", []byte("kind: Deployment"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry.Name != "deploy.yaml" {
		t.Errorf("expected name deploy.yaml, got %q", entry.Name)
	}

	list, err := s.List()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(list) != 1 || list[0].Name != "deploy.yaml" {
		t.Errorf("expected 1 file named deploy.yaml, got %+v", list)
	}
}

func TestStore_SaveDedupesOnNameCollision(t *testing.T) {
	s := newTestStore(t)

	first, err := s.Save("deploy.yaml", []byte("v1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	second, err := s.Save("deploy.yaml", []byte("v2"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if first.Name == second.Name {
		t.Fatalf("expected different names on collision, both were %q", first.Name)
	}
	if second.Name != "deploy-2.yaml" {
		t.Errorf("expected deploy-2.yaml, got %q", second.Name)
	}

	// Confirma que o conteúdo original NUNCA foi sobrescrito pela 2ª chamada de Save.
	v1, err := s.Read("deploy.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(v1) != "v1" {
		t.Errorf("expected original file content preserved, got %q", v1)
	}
}

func TestStore_RejectsPathTraversalAndBadExtension(t *testing.T) {
	s := newTestStore(t)

	cases := []string{"../escape.yaml", "sub/dir.yaml", "no-extension", "script.sh", ""}
	for _, name := range cases {
		if _, err := s.Save(name, []byte("x")); err == nil {
			t.Errorf("expected Save to reject %q, got nil error", name)
		}
		if err := s.Write(name, []byte("x")); err == nil {
			t.Errorf("expected Write to reject %q, got nil error", name)
		}
		if _, err := s.Read(name); err == nil {
			t.Errorf("expected Read to reject %q, got nil error", name)
		}
	}
}

func TestStore_WriteAndDelete(t *testing.T) {
	s := newTestStore(t)

	if _, err := s.Save("deploy.yaml", []byte("original")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := s.Write("deploy.yaml", []byte("edited")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	content, err := s.Read("deploy.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(content) != "edited" {
		t.Errorf("expected edited content, got %q", content)
	}

	if err := s.Delete("deploy.yaml"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := s.Read("deploy.yaml"); err == nil {
		t.Error("expected error reading deleted file, got nil")
	}
}

func TestStore_DeleteUnknownFileFails(t *testing.T) {
	s := newTestStore(t)
	if err := s.Delete("nao-existe.yaml"); err == nil {
		t.Error("expected error deleting a file that never existed, got nil")
	}
}

func TestBrowseDirectory_RequiresAbsolutePath(t *testing.T) {
	if _, err := BrowseDirectory("relative/path"); err == nil {
		t.Error("expected error for relative path, got nil")
	}
}

func TestBrowseDirectory_ListsOnlyYAMLFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.yaml"), []byte("a"), 0600); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.yml"), []byte("b"), 0600); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "c.txt"), []byte("c"), 0600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	entries, err := BrowseDirectory(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 yaml/yml files, got %d: %+v", len(entries), entries)
	}
}

func TestReadWriteFileAtPath_RejectsNonYAMLAndRelativePaths(t *testing.T) {
	dir := t.TempDir()
	txtPath := filepath.Join(dir, "not-yaml.txt")
	if err := os.WriteFile(txtPath, []byte("x"), 0600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	if _, err := ReadFileAtPath(txtPath); err == nil {
		t.Error("expected ReadFileAtPath to reject non-yaml file, got nil")
	}
	if err := WriteFileAtPath(txtPath, []byte("y")); err == nil {
		t.Error("expected WriteFileAtPath to reject non-yaml file, got nil")
	}
	if _, err := ReadFileAtPath("relative.yaml"); err == nil {
		t.Error("expected ReadFileAtPath to reject relative path, got nil")
	}
}

func TestWriteFileAtPath_NeverCreatesNewFile(t *testing.T) {
	dir := t.TempDir()
	neverCreated := filepath.Join(dir, "never-created.yaml")

	err := WriteFileAtPath(neverCreated, []byte("x"))
	if err == nil {
		t.Error("expected error writing to a file that doesn't exist yet, got nil")
	}
	if _, statErr := os.Stat(neverCreated); statErr == nil {
		t.Error("WriteFileAtPath must never create a new file, but one was created")
	}
}
