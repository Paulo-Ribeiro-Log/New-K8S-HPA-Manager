package handlers

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// pod_config_finder.go — buscador de arquivos de configuração num container de pod (não expõe
// porta nenhuma, mesmo princípio do SFTP embutido: cada chamada abre um `kubectl exec` via SPDY,
// roda UM comando, fecha). Motivado por dois casos reais, cada stack com sua própria convenção:
//   - Spring Boot (chart `convair-helm` desta empresa): o `application.yml` frequentemente NÃO é
//     exposto via ConfigMap — vem compilado dentro do próprio jar (`BOOT-INF/classes/application.yaml`),
//     só visível abrindo o pacote (`.jar/.war/.zip`) — Kind="archive" abaixo.
//   - .NET: o `appsettings.json`/`web.config` tipicamente NÃO vem empacotado — é um arquivo solto
//     copiado direto na imagem (`Dockerfile COPY`), sem passo de extração nenhum — Kind="file"
//     abaixo. NuGet (`.nupkg`) é zip de verdade e também é reconhecido como Kind="archive" pro caso
//     raro de deploy via pacote.
// ListConfigCandidates busca os dois tipos numa passada só; o front-end decide o fluxo (extrair
// entrada vs ler direto) pelo campo Kind. Deliberadamente genérico por nome de arquivo/extensão —
// não hardcoded pra um único framework — reaproveitável pra outras stacks empacotadas ou não.
//
// Reaproveita execCmdInPod (nodepools_conntrack.go, já usado por Conntrack/DB Test/Kafka Test) —
// nenhum mecanismo de exec novo. Todas as rotas são só LEITURA (nunca escreve nada persistente no
// pod — a extração via `jar xf` usa um diretório temporário dentro do próprio container, sempre
// apagado no fim do comando), mesmo padrão de RBAC do list/download do SFTP (sem RequireSREGroup).
//
// clientset/restConfig são sempre passados como VALOR de retorno de resolvePodExecTarget — nunca
// guardados em campo do PodHandler, que é um singleton compartilhado por todas as requisições HTTP
// concorrentes (guardar estado por-request ali seria uma race condition real entre requests
// simultâneos, ex: dois usuários usando a ferramenta ao mesmo tempo).

const (
	archiveExtractCmdTimeout = 25 * time.Second
	// archiveExtractMaxContentBytes — teto de exibição do conteúdo extraído. Um arquivo de
	// configuração de verdade (application.yml, properties, etc.) nunca chega perto disso; existe
	// só como rede de segurança contra escolher por engano uma entrada grande (ex: uma classe
	// .class binária de várias centenas de KB) e travar o Monaco Editor no frontend.
	archiveExtractMaxContentBytes = 2 * 1024 * 1024
	// archiveExtractMaxCandidates — teto de candidatos devolvidos por ListArchives. Uma imagem
	// JDK/Node pode ter dezenas de .jar de dependências além do artefato da aplicação em si — sem
	// teto, a lista ficaria grande o bastante pra atrapalhar mais que ajudar.
	archiveExtractMaxCandidates = 200
)

// ArchiveCandidate é um candidato encontrado no container: um arquivo de config SOLTO (Kind
// "file" — lido direto via GetConfigFileContent, sem passo de extração) ou um pacote .jar/.war/
// .zip/.nupkg (Kind "archive" — precisa listar entradas via ListArchiveEntries antes de extrair
// uma via GetArchiveEntryContent).
type ArchiveCandidate struct {
	Path      string `json:"path"`
	SizeBytes int64  `json:"size_bytes"`
	Kind      string `json:"kind"`
}

// ArchiveEntry é uma entrada de arquivo dentro do .jar/.war/.zip. SizeBytes é -1 quando a
// ferramenta usada pra listar não expõe tamanho (caso do `jar tf`, que só lista nomes).
type ArchiveEntry struct {
	Name      string `json:"name"`
	SizeBytes int64  `json:"size_bytes"`
}

// resolvePodExecTarget resolve cluster/namespace/pod/container + clientset/restConfig comuns às
// 3 rotas deste arquivo. Retorna ok=false já com a resposta de erro escrita.
func (h *PodHandler) resolvePodExecTarget(c *gin.Context) (namespace, podName, container string, clientset kubernetes.Interface, restConfig *rest.Config, ctx context.Context, cancel context.CancelFunc, ok bool) {
	cluster := c.Param("cluster")
	namespace = c.Param("namespace")
	podName = c.Param("name")
	container = c.Query("container")
	if container == "" {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_CONTAINER", "parâmetro \"container\" é obrigatório"))
		return "", "", "", nil, nil, nil, nil, false
	}

	var err error
	clientset, err = h.kubeManager.GetClient(cluster)
	if err != nil {
		c.JSON(http.StatusBadGateway, errorResponse("CLUSTER_ERROR", err.Error()))
		return "", "", "", nil, nil, nil, nil, false
	}
	restConfig, err = h.kubeManager.GetRestConfig(cluster)
	if err != nil {
		c.JSON(http.StatusBadGateway, errorResponse("CLUSTER_ERROR", err.Error()))
		return "", "", "", nil, nil, nil, nil, false
	}

	ctx, cancel = context.WithTimeout(c.Request.Context(), archiveExtractCmdTimeout)
	return namespace, podName, container, clientset, restConfig, ctx, cancel, true
}

// archiveDetectToolScript imprime em stdout qual ferramenta usar (a primeira disponível, nessa
// ordem de preferência: unzip é o mais universal e o único que extrai direto pro stdout sem
// precisar de diretório temporário; jar vem de brinde em toda imagem com JDK — comum nesta
// frota, Spring Boot; python3 como último recurso, presente em boa parte das imagens Debian/
// Alpine mesmo sem unzip/jar). "none" quando nenhuma das 3 existe.
const archiveDetectToolScript = `if command -v unzip >/dev/null 2>&1; then echo unzip; ` +
	`elif command -v jar >/dev/null 2>&1; then echo jar; ` +
	`elif command -v python3 >/dev/null 2>&1; then echo python3; ` +
	`else echo none; fi`

// archiveFindCandidatesScript localiza .jar/.war/.zip/.nupkg no container (.nupkg é o formato de
// pacote NuGet — zip de verdade, mesmo caso raro de deploy .NET via pacote em vez de arquivo
// solto). -xdev evita cruzar pontos de montagem (mantém o find fora de /proc, /sys e volumes
// montados na prática); -prune nesses 3 caminhos é defesa em profundidade caso não sejam mount
// points próprios nesta imagem específica.
const archiveFindCandidatesScript = `find / -xdev -maxdepth 6 ` +
	`\( -path /proc -o -path /sys -o -path /dev \) -prune -o ` +
	`-type f \( -iname '*.jar' -o -iname '*.war' -o -iname '*.zip' -o -iname '*.nupkg' \) -printf '%s\t%p\n' 2>/dev/null`

// archiveFindCandidatesScriptNoPrintf é o fallback pra `find` do BusyBox, que não tem `-printf`
// (só o GNU find tem) — sem tamanho, só o caminho.
const archiveFindCandidatesScriptNoPrintf = `find / -xdev -maxdepth 6 ` +
	`\( -path /proc -o -path /sys -o -path /dev \) -prune -o ` +
	`-type f \( -iname '*.jar' -o -iname '*.war' -o -iname '*.zip' -o -iname '*.nupkg' \) -print 2>/dev/null`

// configFileFindScript localiza arquivos de configuração SOLTOS reconhecidos por convenção de
// nome — cobre .NET (appsettings*.json/.yml/.yaml, web.config — normalmente copiados soltos na
// imagem via Dockerfile COPY, nunca empacotados) e Spring Boot fora do jar (application.yml/
// .properties, bootstrap.yml, quando não vêm compilados no jar). Lista fechada de nomes/
// convenções reais — não usa um wildcard genérico tipo "*.json"/"*.yml" pra não trazer ruído
// (qualquer outro json/yaml do container que não seja config de aplicação).
const configFileFindScript = `find / -xdev -maxdepth 6 ` +
	`\( -path /proc -o -path /sys -o -path /dev \) -prune -o ` +
	`-type f \( -iname 'appsettings*.json' -o -iname 'appsettings*.yml' -o -iname 'appsettings*.yaml' ` +
	`-o -iname 'web.config' -o -iname 'application.yml' -o -iname 'application.yaml' ` +
	`-o -iname 'application*.properties' -o -iname 'bootstrap.yml' -o -iname 'bootstrap.yaml' \) ` +
	`-printf '%s\t%p\n' 2>/dev/null`

// configFileFindScriptNoPrintf é o fallback BusyBox de configFileFindScript (sem tamanho).
const configFileFindScriptNoPrintf = `find / -xdev -maxdepth 6 ` +
	`\( -path /proc -o -path /sys -o -path /dev \) -prune -o ` +
	`-type f \( -iname 'appsettings*.json' -o -iname 'appsettings*.yml' -o -iname 'appsettings*.yaml' ` +
	`-o -iname 'web.config' -o -iname 'application.yml' -o -iname 'application.yaml' ` +
	`-o -iname 'application*.properties' -o -iname 'bootstrap.yml' -o -iname 'bootstrap.yaml' \) ` +
	`-print 2>/dev/null`

// isNoShellExecError detecta o exec que falhou porque a imagem não tem `sh` (distroless, .NET
// chiseled, etc.) — achado real: o runtime devolve `exec: "sh": executable file not found in
// $PATH`. Nesse caso nenhum dos comandos desta ferramenta roda (find/cat/unzip dependem de sh),
// então não adianta tentar os fallbacks: responde direto com NO_SHELL e uma mensagem legível.
func isNoShellExecError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "executable file not found") && strings.Contains(msg, `"sh"`)
}

// respondNoShell escreve a resposta NO_SHELL. Devolve false se err não for desse tipo.
func respondNoShell(c *gin.Context, container string, err error) bool {
	if !isNoShellExecError(err) {
		return false
	}
	c.JSON(http.StatusUnprocessableEntity, errorResponse("NO_SHELL", fmt.Sprintf(
		"o container %q não tem shell (sh) — imagem mínima/distroless. Esta ferramenta depende de "+
			"sh/find/cat dentro do container e não consegue buscar arquivos nele. Tente outro "+
			"container do pod ou consulte a imagem/ConfigMap de origem.", container)))
	return true
}

// findCandidatesWithFallback roda o script GNU (`withPrintf`, com tamanho via -printf); se falhar
// ou não achar nada, tenta o fallback BusyBox (`noPrintf`, sem -printf) antes de desistir — mesmo
// padrão de retry que ListConfigCandidates aplica nas duas buscas (arquivo solto e pacote).
//
// BUG REAL corrigido — reproduzido ao vivo: `find / ...` varrendo a raiz inteira do container
// esbarra com frequência em "Permission denied" de algum subdiretório ilegível (comum: container
// rodando como non-root) — isso faz o `find` sair com status 1 mesmo tendo impresso candidatos
// válidos em stdout ANTES de bater no diretório proibido (`2>/dev/null` só suprime a MENSAGEM de
// erro, não muda o exit code). A versão antiga só aceitava o resultado com `err == nil`, então
// descartava candidatos de verdade e caía no retry, que batia na mesma parede de permissão e
// devolvia erro cru ("stream: command terminated with exit code 1") pro usuário em vez da lista
// que o find já tinha achado. Corrigido: candidatos não-vazios em stdout sempre valem, com erro
// (`err`/`err2`) só decidindo o resultado quando as DUAS tentativas vierem realmente vazias.
func findCandidatesWithFallback(ctx context.Context, clientset kubernetes.Interface, restConfig *rest.Config, namespace, podName, container, withPrintf, noPrintf string) ([]ArchiveCandidate, error) {
	out, err := execCmdInPod(ctx, clientset, restConfig, namespace, podName, container, []string{"sh", "-c", withPrintf})
	candidates := parseArchiveCandidatesWithSize(out)
	if len(candidates) > 0 {
		return candidates, nil
	}
	if isNoShellExecError(err) {
		return nil, err // sem sh o fallback falharia igual
	}
	out2, err2 := execCmdInPod(ctx, clientset, restConfig, namespace, podName, container, []string{"sh", "-c", noPrintf})
	candidates2 := parseArchiveCandidatesNoSize(out2)
	if len(candidates2) > 0 {
		return candidates2, nil
	}
	if err != nil {
		return nil, err
	}
	if err2 != nil {
		return nil, err2
	}
	return nil, nil // as duas tentativas rodaram limpo, só não acharam nada — não é erro.
}

// ListConfigCandidates — GET /api/v1/pods/:cluster/:namespace/:name/config-candidates?container=
// Busca unificada: arquivos de config soltos (Kind="file") + pacotes .jar/.war/.zip/.nupkg
// (Kind="archive"). Um erro de exec só derruba a resposta se as DUAS buscas falharem — uma
// stack .NET tipicamente não tem nenhum pacote no container, o que não é erro, só resultado vazio
// daquele lado.
func (h *PodHandler) ListConfigCandidates(c *gin.Context) {
	namespace, podName, container, clientset, restConfig, ctx, cancel, ok := h.resolvePodExecTarget(c)
	if !ok {
		return
	}
	defer cancel()

	fileCandidates, fileErr := findCandidatesWithFallback(ctx, clientset, restConfig, namespace, podName, container,
		configFileFindScript, configFileFindScriptNoPrintf)
	if respondNoShell(c, container, fileErr) {
		return
	}
	for i := range fileCandidates {
		fileCandidates[i].Kind = "file"
	}

	archiveCandidates, archiveErr := findCandidatesWithFallback(ctx, clientset, restConfig, namespace, podName, container,
		archiveFindCandidatesScript, archiveFindCandidatesScriptNoPrintf)
	for i := range archiveCandidates {
		archiveCandidates[i].Kind = "archive"
	}

	if fileErr != nil && archiveErr != nil {
		c.JSON(http.StatusBadGateway, errorResponse("CONFIG_FIND_ERROR", fileErr.Error()))
		return
	}

	candidates := append(fileCandidates, archiveCandidates...)
	sortArchiveCandidates(candidates)
	if len(candidates) > archiveExtractMaxCandidates {
		candidates = candidates[:archiveExtractMaxCandidates]
	}
	c.JSON(http.StatusOK, gin.H{"candidates": candidates})
}

// GetConfigFileContent — GET /api/v1/pods/:cluster/:namespace/:name/config-file-content?container=&path=
// Lê um arquivo de config SOLTO (candidato Kind="file" de ListConfigCandidates) direto via `cat`
// — sem passo de extração, já que não está empacotado. Mesmas checagens de tamanho/UTF-8 de
// GetArchiveEntryContent, pro mesmo motivo (trava o Monaco Editor do front-end).
func (h *PodHandler) GetConfigFileContent(c *gin.Context) {
	path := c.Query("path")
	if path == "" {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_PATH", "parâmetro \"path\" é obrigatório"))
		return
	}
	namespace, podName, container, clientset, restConfig, ctx, cancel, ok := h.resolvePodExecTarget(c)
	if !ok {
		return
	}
	defer cancel()

	out, err := execCmdInPod(ctx, clientset, restConfig, namespace, podName, container,
		[]string{"sh", "-c", "cat " + quoteShellArg(path)})
	if respondNoShell(c, container, err) {
		return
	}
	if err != nil {
		c.JSON(http.StatusBadGateway, errorResponse("CONFIG_FILE_READ_ERROR", err.Error()+": "+out))
		return
	}
	if len(out) > archiveExtractMaxContentBytes {
		c.JSON(http.StatusUnprocessableEntity, errorResponse("CONFIG_FILE_TOO_LARGE",
			fmt.Sprintf("arquivo tem %d bytes, maior que o limite de exibição (%d bytes)", len(out), archiveExtractMaxContentBytes)))
		return
	}
	if !utf8.ValidString(out) {
		c.JSON(http.StatusUnprocessableEntity, errorResponse("CONFIG_FILE_BINARY",
			"este arquivo não parece ser texto (binário/encoding não reconhecido) — não é possível exibir o conteúdo"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"content": out})
}

// archiveSystemPathPrefixes — diretórios tipicamente cheios de jars de RUNTIME/FERRAMENTA (JDK,
// Maven, libs do SO), não do artefato da própria aplicação. Achado real testando ao vivo contra
// um pod Spring Boot real: a imagem tinha Maven instalado, o que sozinho gerou ~50 jars de
// dependência (`/usr/share/maven/lib/*.jar`) — ruído que enterrava o único jar relevante
// (`/app/app.jar`) no meio da lista. Nunca usado pra EXCLUIR um candidato (informação nunca
// escondida, só reordenada) — só rebaixa a prioridade de exibição via sortArchiveCandidates.
var archiveSystemPathPrefixes = []string{"/usr/", "/opt/", "/lib/", "/lib64/", "/var/", "/root/.m2/"}

func isLikelyApplicationArchivePath(path string) bool {
	for _, prefix := range archiveSystemPathPrefixes {
		if strings.HasPrefix(path, prefix) {
			return false
		}
	}
	return true
}

// sortArchiveCandidates prioriza candidatos fora de diretórios de sistema/ferramenta (mais
// prováveis de ser o artefato da própria aplicação), e dentro de cada grupo ordena por tamanho
// decrescente (o jar da aplicação tende a ser bem maior que uma lib de dependência isolada).
func sortArchiveCandidates(candidates []ArchiveCandidate) {
	sort.SliceStable(candidates, func(i, j int) bool {
		li, lj := isLikelyApplicationArchivePath(candidates[i].Path), isLikelyApplicationArchivePath(candidates[j].Path)
		if li != lj {
			return li // prováveis (true) vêm antes dos de sistema (false)
		}
		return candidates[i].SizeBytes > candidates[j].SizeBytes
	})
}

// parseArchiveCandidatesWithSize parseia a saída de `find ... -printf '%s\t%p\n'` (GNU find).
func parseArchiveCandidatesWithSize(out string) []ArchiveCandidate {
	result := []ArchiveCandidate{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			continue
		}
		size, _ := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
		result = append(result, ArchiveCandidate{Path: parts[1], SizeBytes: size})
	}
	return result
}

// parseArchiveCandidatesNoSize parseia a saída de `find ... -print` (fallback BusyBox, sem tamanho).
func parseArchiveCandidatesNoSize(out string) []ArchiveCandidate {
	result := []ArchiveCandidate{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		result = append(result, ArchiveCandidate{Path: line, SizeBytes: -1})
	}
	return result
}

// detectArchiveTool roda archiveDetectToolScript e devolve "unzip"/"jar"/"python3"/"none".
func detectArchiveTool(ctx context.Context, clientset kubernetes.Interface, restConfig *rest.Config, namespace, podName, container string) (string, error) {
	out, err := execCmdInPod(ctx, clientset, restConfig, namespace, podName, container,
		[]string{"sh", "-c", archiveDetectToolScript})
	if err != nil {
		return "", err
	}
	tool := strings.TrimSpace(out)
	switch tool {
	case "unzip", "jar", "python3":
		return tool, nil
	default:
		return "none", nil
	}
}

// ListArchiveEntries — GET /api/v1/pods/:cluster/:namespace/:name/archive-entries?container=&path=
func (h *PodHandler) ListArchiveEntries(c *gin.Context) {
	archivePath := c.Query("path")
	if archivePath == "" {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_PATH", "parâmetro \"path\" é obrigatório"))
		return
	}
	namespace, podName, container, clientset, restConfig, ctx, cancel, ok := h.resolvePodExecTarget(c)
	if !ok {
		return
	}
	defer cancel()

	tool, err := detectArchiveTool(ctx, clientset, restConfig, namespace, podName, container)
	if respondNoShell(c, container, err) {
		return
	}
	if err != nil {
		c.JSON(http.StatusBadGateway, errorResponse("ARCHIVE_TOOL_DETECT_ERROR", err.Error()))
		return
	}
	if tool == "none" {
		c.JSON(http.StatusUnprocessableEntity, errorResponse("NO_ARCHIVE_TOOL",
			"nenhuma ferramenta de extração encontrada no container (unzip, jar ou python3)"))
		return
	}

	var cmd []string
	switch tool {
	case "unzip":
		cmd = []string{"sh", "-c", "unzip -l " + quoteShellArg(archivePath) + " 2>&1"}
	case "jar":
		cmd = []string{"sh", "-c", "jar tf " + quoteShellArg(archivePath) + " 2>&1"}
	case "python3":
		cmd = []string{"python3", "-c", archivePythonListScript, archivePath}
	}
	out, err := execCmdInPod(ctx, clientset, restConfig, namespace, podName, container, cmd)
	if err != nil {
		c.JSON(http.StatusBadGateway, errorResponse("ARCHIVE_LIST_ERROR", err.Error()+": "+out))
		return
	}

	var entries []ArchiveEntry
	switch tool {
	case "unzip":
		entries = parseUnzipListOutput(out)
	case "jar":
		entries = parseJarTfOutput(out)
	case "python3":
		entries = parsePythonListOutput(out)
	}
	c.JSON(http.StatusOK, gin.H{"entries": entries, "tool": tool})
}

// archivePythonListScript imprime "tamanho\tnome" por linha, uma entrada por arquivo do zip —
// formato próprio (não precisa parsear texto de terceiro), só entradas de arquivo (is_dir=False).
const archivePythonListScript = `
import sys, zipfile
with zipfile.ZipFile(sys.argv[1]) as zf:
    for info in zf.infolist():
        if not info.is_dir():
            sys.stdout.write(str(info.file_size) + "\t" + info.filename + "\n")
`

// unzipListEntryRegex casa uma linha de dados de `unzip -l` (Info-ZIP e BusyBox usam o mesmo
// layout de colunas: tamanho, data, hora, nome) — ignora as colunas de data/hora do meio, só
// captura tamanho (grupo 1) e nome (grupo 2, resto da linha).
var unzipListEntryRegex = regexp.MustCompile(`^\s*(\d+)\s+\S+\s+\S+\s+(.+?)\s*$`)

// parseUnzipListOutput parseia a saída de `unzip -l` — pula cabeçalho/rodapé (linhas de "---" e
// a linha final "N files") automaticamente, já que só linhas que batem no regex viram entrada;
// diretórios (nome terminando em "/") são descartados, só arquivos fazem sentido pra extrair.
func parseUnzipListOutput(out string) []ArchiveEntry {
	entries := []ArchiveEntry{}
	for _, line := range strings.Split(out, "\n") {
		m := unzipListEntryRegex.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		name := m[2]
		if strings.HasSuffix(name, "/") || strings.EqualFold(name, "files") {
			continue
		}
		size, _ := strconv.ParseInt(m[1], 10, 64)
		entries = append(entries, ArchiveEntry{Name: name, SizeBytes: size})
	}
	return entries
}

// parseJarTfOutput parseia `jar tf` — uma entrada por linha, sem tamanho (SizeBytes: -1).
func parseJarTfOutput(out string) []ArchiveEntry {
	entries := []ArchiveEntry{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		line = strings.TrimSpace(line)
		if line == "" || strings.HasSuffix(line, "/") {
			continue
		}
		entries = append(entries, ArchiveEntry{Name: line, SizeBytes: -1})
	}
	return entries
}

// parsePythonListOutput parseia a saída de archivePythonListScript ("tamanho\tnome" por linha).
func parsePythonListOutput(out string) []ArchiveEntry {
	entries := []ArchiveEntry{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			continue
		}
		size, _ := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
		entries = append(entries, ArchiveEntry{Name: parts[1], SizeBytes: size})
	}
	return entries
}

// GetArchiveEntryContent — GET /api/v1/pods/:cluster/:namespace/:name/archive-content?container=&path=&entry=
func (h *PodHandler) GetArchiveEntryContent(c *gin.Context) {
	archivePath := c.Query("path")
	entry := c.Query("entry")
	if archivePath == "" || entry == "" {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_PARAMS", "parâmetros \"path\" e \"entry\" são obrigatórios"))
		return
	}
	namespace, podName, container, clientset, restConfig, ctx, cancel, ok := h.resolvePodExecTarget(c)
	if !ok {
		return
	}
	defer cancel()

	tool, err := detectArchiveTool(ctx, clientset, restConfig, namespace, podName, container)
	if respondNoShell(c, container, err) {
		return
	}
	if err != nil {
		c.JSON(http.StatusBadGateway, errorResponse("ARCHIVE_TOOL_DETECT_ERROR", err.Error()))
		return
	}
	if tool == "none" {
		c.JSON(http.StatusUnprocessableEntity, errorResponse("NO_ARCHIVE_TOOL",
			"nenhuma ferramenta de extração encontrada no container (unzip, jar ou python3)"))
		return
	}

	var cmd []string
	switch tool {
	case "unzip":
		// -p extrai direto pro stdout, sem tocar em disco — o mais simples dos 3 caminhos.
		cmd = []string{"sh", "-c", "unzip -p " + quoteShellArg(archivePath) + " " + quoteShellArg(entry)}
	case "jar":
		// `jar` não tem extração pra stdout — usa um diretório temporário próprio (mktemp -d),
		// sempre apagado no final (inclusive em caso de falha do jar/cat, via `; rm -rf` fora do &&).
		script := "d=$(mktemp -d) && cd \"$d\" && jar xf " + quoteShellArg(archivePath) + " " +
			quoteShellArg(entry) + " && cat \"$d/" + entry + "\"; rc=$?; rm -rf \"$d\"; exit $rc"
		cmd = []string{"sh", "-c", script}
	case "python3":
		cmd = []string{"python3", "-c", archivePythonExtractScript, archivePath, entry}
	}

	out, err := execCmdInPod(ctx, clientset, restConfig, namespace, podName, container, cmd)
	if err != nil {
		c.JSON(http.StatusBadGateway, errorResponse("ARCHIVE_EXTRACT_ERROR", err.Error()+": "+out))
		return
	}

	if len(out) > archiveExtractMaxContentBytes {
		c.JSON(http.StatusUnprocessableEntity, errorResponse("ARCHIVE_ENTRY_TOO_LARGE",
			fmt.Sprintf("entrada tem %d bytes, maior que o limite de exibição (%d bytes)", len(out), archiveExtractMaxContentBytes)))
		return
	}
	if !utf8.ValidString(out) {
		c.JSON(http.StatusUnprocessableEntity, errorResponse("ARCHIVE_ENTRY_BINARY",
			"esta entrada não parece ser texto (binário/encoding não reconhecido) — não é possível exibir o conteúdo"))
		return
	}

	c.JSON(http.StatusOK, gin.H{"content": out, "tool": tool})
}

// archivePythonExtractScript escreve os bytes crus da entrada em stdout (sys.stdout.buffer,
// nunca sys.stdout.write — evita reencoding acidental caso o conteúdo real não seja UTF-8, a
// checagem de validade acontece no lado Go depois, sobre os bytes exatos que saíram do zip).
const archivePythonExtractScript = `
import sys, zipfile
with zipfile.ZipFile(sys.argv[1]) as zf:
    sys.stdout.buffer.write(zf.read(sys.argv[2]))
`
