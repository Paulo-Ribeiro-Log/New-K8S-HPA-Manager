package handlers

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// pod_config_finder_remote_zip.go — quarta "ferramenta" de ListArchiveEntries/GetArchiveEntryContent
// (archiveToolRemote): quando o container não tem unzip, jar nem python3, o zip é aberto AQUI no
// servidor (archive/zip) lendo só os trechos necessários do arquivo remoto — o índice (central
// directory) fica no fim do zip, então listar e extrair uma entrada trafega alguns KB, não o .jar
// inteiro. Do container só precisa de `sh`, `wc`, `tail` e `head` (coreutils ou BusyBox).

const archiveToolRemote = "remote"

// podZipChunkSize: cada leitura remota busca um bloco alinhado desse tamanho (um exec por bloco).
// O archive/zip faz muitas leituras pequenas (bufio de 4 KB no índice) — sem os blocos, um jar com
// milhares de entradas viraria centenas de execs.
const podZipChunkSize = 512 * 1024

// podZipMaxCachedChunks limita a memória do cache de blocos por requisição (8 × 512 KB = 4 MB).
const podZipMaxCachedChunks = 8

// podFileRangeReader implementa io.ReaderAt sobre um arquivo dentro do container, via exec de
// `tail -c +N arquivo | head -c M`. Vive só durante uma requisição (nunca compartilhado).
type podFileRangeReader struct {
	run    func(script string) (string, error) // `sh -c script` no container (execCmdInPod em produção)
	path   string
	size   int64
	chunks map[int64][]byte
}

func newPodFileRangeReader(run func(script string) (string, error), path string) (*podFileRangeReader, error) {
	out, err := run("wc -c < " + quoteShellArg(path))
	if err != nil {
		return nil, fmt.Errorf("falha ao obter o tamanho de %s: %v: %s", path, err, strings.TrimSpace(out))
	}
	size, convErr := strconv.ParseInt(strings.TrimSpace(out), 10, 64)
	if convErr != nil || size <= 0 {
		return nil, fmt.Errorf("tamanho inválido para %s: %q", path, strings.TrimSpace(out))
	}
	return &podFileRangeReader{run: run, path: path, size: size, chunks: make(map[int64][]byte)}, nil
}

func (r *podFileRangeReader) chunk(idx int64) ([]byte, error) {
	if data, ok := r.chunks[idx]; ok {
		return data, nil
	}
	start := idx * podZipChunkSize
	length := int64(podZipChunkSize)
	if start+length > r.size {
		length = r.size - start
	}
	script := fmt.Sprintf("tail -c +%d %s | head -c %d", start+1, quoteShellArg(r.path), length)
	out, err := r.run(script)
	if err != nil {
		return nil, fmt.Errorf("falha ao ler %s (offset %d): %v", r.path, start, err)
	}
	if int64(len(out)) != length {
		return nil, fmt.Errorf("leitura parcial de %s (offset %d): esperava %d bytes, veio %d", r.path, start, length, len(out))
	}
	if len(r.chunks) >= podZipMaxCachedChunks {
		r.chunks = make(map[int64][]byte)
	}
	data := []byte(out)
	r.chunks[idx] = data
	return data, nil
}

// ReadAt segue o contrato de io.ReaderAt: devolve io.EOF quando o trecho pedido passa do fim.
func (r *podFileRangeReader) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 {
		return 0, fmt.Errorf("offset negativo: %d", off)
	}
	n := 0
	for n < len(p) {
		pos := off + int64(n)
		if pos >= r.size {
			return n, io.EOF
		}
		idx := pos / podZipChunkSize
		data, err := r.chunk(idx)
		if err != nil {
			return n, err
		}
		n += copy(p[n:], data[pos-idx*podZipChunkSize:])
	}
	return n, nil
}

// openRemoteZip abre o .jar/.war/.zip/.nupkg do container sem trazê-lo inteiro.
func openRemoteZip(ctx context.Context, clientset kubernetes.Interface, restConfig *rest.Config, namespace, podName, container, path string) (*zip.Reader, error) {
	run := func(script string) (string, error) {
		return execCmdInPod(ctx, clientset, restConfig, namespace, podName, container, []string{"sh", "-c", script})
	}
	return openZipWithRunner(run, path)
}

// openZipWithRunner é openRemoteZip com o executor injetado (testável sem cluster).
func openZipWithRunner(run func(script string) (string, error), path string) (*zip.Reader, error) {
	ra, err := newPodFileRangeReader(run, path)
	if err != nil {
		return nil, err
	}
	zr, err := zip.NewReader(ra, ra.size)
	if err != nil {
		return nil, fmt.Errorf("%s não parece ser um zip/jar válido: %v", path, err)
	}
	return zr, nil
}

// listZipEntries converte o índice do zip no formato de ListArchiveEntries (só arquivos).
func listZipEntries(zr *zip.Reader) []ArchiveEntry {
	entries := []ArchiveEntry{}
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		entries = append(entries, ArchiveEntry{Name: f.Name, SizeBytes: int64(f.UncompressedSize64)})
	}
	return entries
}

// errZipEntryTooLarge sinaliza entrada maior que archiveExtractMaxContentBytes — checado pelo
// tamanho declarado no índice, ANTES de baixar a entrada.
type errZipEntryTooLarge struct{ size uint64 }

func (e errZipEntryTooLarge) Error() string {
	return fmt.Sprintf("entrada tem %d bytes, maior que o limite de exibição (%d bytes)", e.size, archiveExtractMaxContentBytes)
}

// readZipEntry extrai uma entrada pelo nome exato.
func readZipEntry(zr *zip.Reader, name string) (string, error) {
	for _, f := range zr.File {
		if f.Name != name {
			continue
		}
		if f.UncompressedSize64 > archiveExtractMaxContentBytes {
			return "", errZipEntryTooLarge{size: f.UncompressedSize64}
		}
		rc, err := f.Open()
		if err != nil {
			return "", fmt.Errorf("falha ao abrir a entrada %s: %v", name, err)
		}
		defer rc.Close()
		data, err := io.ReadAll(io.LimitReader(rc, archiveExtractMaxContentBytes+1))
		if err != nil {
			return "", fmt.Errorf("falha ao extrair a entrada %s: %v", name, err)
		}
		return string(data), nil
	}
	return "", fmt.Errorf("entrada %q não encontrada no arquivo", name)
}
