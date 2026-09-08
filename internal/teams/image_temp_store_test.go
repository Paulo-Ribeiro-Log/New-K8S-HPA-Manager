package teams

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsValidTeamsBroadcastImageFilename(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"img-0.png", true},
		{"img-12.jpg", true},
		{"img-0.bin", true},
		{"", false},
		{"img-0", false},               // sem extensão
		{"img-abc.png", false},         // índice não-numérico
		{"../img-0.png", false},        // path traversal
		{"img-0.png/../secret", false}, // path traversal via sufixo
		{"IMG-0.PNG", false},           // extensão maiúscula não gerada por SaveTeamsBroadcastImage
	}
	for _, c := range cases {
		if got := IsValidTeamsBroadcastImageFilename(c.name); got != c.want {
			t.Errorf("IsValidTeamsBroadcastImageFilename(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestExtensionForMimeType(t *testing.T) {
	cases := map[string]string{
		"image/png":                ".png",
		"image/jpeg":               ".jpg",
		"image/jpg":                ".jpg",
		"image/gif":                ".gif",
		"image/webp":               ".webp",
		"image/bmp":                ".bmp",
		"image/svg+xml":            ".svg",
		"image/png; charset=utf-8": ".png",
		"application/octet-stream": ".bin",
		"":                         ".bin",
		"IMAGE/PNG":                ".png",
	}
	for mimeType, want := range cases {
		if got := extensionForMimeType(mimeType); got != want {
			t.Errorf("extensionForMimeType(%q) = %q, want %q", mimeType, got, want)
		}
	}
}

func TestSaveTeamsBroadcastImage_FilenameIsValidAndReadable(t *testing.T) {
	dir := t.TempDir()
	filename, err := SaveTeamsBroadcastImage(dir, 3, "image/jpeg", []byte("fake-jpeg-bytes"))
	if err != nil {
		t.Fatalf("SaveTeamsBroadcastImage falhou: %v", err)
	}
	if filename != "img-3.jpg" {
		t.Errorf("filename = %q, want img-3.jpg", filename)
	}
	if !IsValidTeamsBroadcastImageFilename(filename) {
		t.Errorf("filename gerado %q não bate no próprio validador", filename)
	}
	data, err := os.ReadFile(filepath.Join(dir, filename))
	if err != nil {
		t.Fatalf("falha ao reler arquivo salvo: %v", err)
	}
	if string(data) != "fake-jpeg-bytes" {
		t.Errorf("conteúdo salvo = %q, want %q", data, "fake-jpeg-bytes")
	}
}

func TestResetTeamsBroadcastImagesDir_DestroysPreviousCollection(t *testing.T) {
	homeDir := t.TempDir()

	dir1, err := ResetTeamsBroadcastImagesDir(homeDir)
	if err != nil {
		t.Fatalf("1ª chamada falhou: %v", err)
	}
	if _, err := SaveTeamsBroadcastImage(dir1, 0, "image/png", []byte("old-collection-image")); err != nil {
		t.Fatalf("falha ao salvar imagem da 1ª coleta: %v", err)
	}
	if entries, _ := os.ReadDir(dir1); len(entries) != 1 {
		t.Fatalf("esperava 1 arquivo após a 1ª coleta, achou %d", len(entries))
	}

	// Nova coleta — pedido explícito do usuário: a pasta anterior deve ser destruída, mesmo que
	// a nova coleta ainda não tenha salvo nenhuma imagem própria.
	dir2, err := ResetTeamsBroadcastImagesDir(homeDir)
	if err != nil {
		t.Fatalf("2ª chamada (reset) falhou: %v", err)
	}
	if dir1 != dir2 {
		t.Fatalf("dir mudou entre coletas: %q != %q", dir1, dir2)
	}
	entries, err := os.ReadDir(dir2)
	if err != nil {
		t.Fatalf("falha ao ler pasta após reset: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("esperava pasta vazia após reset (imagem da coleta anterior destruída), achou %d arquivo(s)", len(entries))
	}
}
