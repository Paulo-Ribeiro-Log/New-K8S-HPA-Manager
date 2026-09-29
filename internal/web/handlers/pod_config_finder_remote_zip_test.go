package handlers

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// buildTestJar monta um zip parecido com um fat jar do Spring Boot: milhares de entradas (índice
// grande, várias leituras), um application.yml comprimido, uma entrada grande sem compressão
// (atravessa vários blocos de podZipChunkSize) e uma acima do limite de exibição.
func buildTestJar(t *testing.T) (path string, appYml string) {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for i := 0; i < 3000; i++ {
		w, _ := zw.Create(fmt.Sprintf("BOOT-INF/classes/com/empresa/app/Classe%04d.class", i))
		w.Write([]byte{0xCA, 0xFE, 0xBA, 0xBE, byte(i)})
	}
	appYml = "server:\n  port: 8080\nspring:\n  application:\n    name: abastecimento # ção\n"
	w, _ := zw.Create("BOOT-INF/classes/application.yml")
	w.Write([]byte(appYml))

	big := make([]byte, 3*podZipChunkSize+123)
	rand.New(rand.NewSource(1)).Read(big)
	w, _ = zw.CreateHeader(&zip.FileHeader{Name: "BOOT-INF/lib/grande.bin", Method: zip.Store})
	w.Write(big)

	w, _ = zw.Create("BOOT-INF/classes/enorme.properties")
	w.Write(bytes.Repeat([]byte("a=b\n"), archiveExtractMaxContentBytes/4+10))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	path = filepath.Join(t.TempDir(), "app com espaço.jar")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path, appYml
}

func localShRunner(calls *int) func(string) (string, error) {
	return func(script string) (string, error) {
		*calls++
		out, err := exec.Command("sh", "-c", script).Output()
		return string(out), err
	}
}

func TestRemoteZipListaEExtraiPorTrechos(t *testing.T) {
	path, appYml := buildTestJar(t)
	info, _ := os.Stat(path)

	calls := 0
	zr, err := openZipWithRunner(localShRunner(&calls), path)
	if err != nil {
		t.Fatal(err)
	}
	entries := listZipEntries(zr)
	if len(entries) != 3003 {
		t.Fatalf("esperava 3003 entradas, veio %d", len(entries))
	}
	listCalls := calls

	got, err := readZipEntry(zr, "BOOT-INF/classes/application.yml")
	if err != nil {
		t.Fatal(err)
	}
	if got != appYml {
		t.Errorf("conteúdo diferente:\n%q\n%q", got, appYml)
	}
	t.Logf("jar de %d KB: listar = %d exec(s), total com a extração = %d", info.Size()/1024, listCalls, calls)
	if listCalls > 4 {
		t.Errorf("listar deveria usar poucos execs (tamanho + blocos do fim), usou %d", listCalls)
	}

	if _, err := readZipEntry(zr, "BOOT-INF/classes/enorme.properties"); !errors.As(err, new(errZipEntryTooLarge)) {
		t.Errorf("esperava errZipEntryTooLarge, veio %v", err)
	}
	if _, err := readZipEntry(zr, "nao/existe.yml"); err == nil {
		t.Error("esperava erro para entrada inexistente")
	}
}

func TestPodFileRangeReaderAtravessaBlocos(t *testing.T) {
	path, _ := buildTestJar(t)
	want, _ := os.ReadFile(path)

	calls := 0
	ra, err := newPodFileRangeReader(localShRunner(&calls), path)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ off, n int64 }{
		{0, 10}, {podZipChunkSize - 5, 10}, {podZipChunkSize - 1, 2*podZipChunkSize + 2}, {ra.size - 7, 7},
	} {
		p := make([]byte, c.n)
		n, err := ra.ReadAt(p, c.off)
		if err != nil || int64(n) != c.n || !bytes.Equal(p, want[c.off:c.off+c.n]) {
			t.Errorf("ReadAt(off=%d, n=%d) = %d, %v (bytes iguais: %v)", c.off, c.n, n, err, bytes.Equal(p[:n], want[c.off:c.off+int64(n)]))
		}
	}
	p := make([]byte, 20)
	if n, err := ra.ReadAt(p, ra.size-5); n != 5 || err == nil {
		t.Errorf("leitura passando do fim deveria devolver 5 e io.EOF, veio %d, %v", n, err)
	}
}

func TestPodFileRangeReaderArquivoInexistente(t *testing.T) {
	calls := 0
	if _, err := openZipWithRunner(localShRunner(&calls), "/nao/existe.jar"); err == nil {
		t.Error("esperava erro")
	}
}
