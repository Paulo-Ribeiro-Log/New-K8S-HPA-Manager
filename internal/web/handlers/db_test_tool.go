package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"

	"k8s-hpa-manager/internal/config"
	"k8s-hpa-manager/internal/history"
	"k8s-hpa-manager/internal/web/sse"
)

const (
	// dbTestEphemeralReadyTimeout cobre TODO o tempo até o ephemeral container ficar Running —
	// inclusive o pull da imagem, que os 30s originais não davam conta de esperar: em nós sem a
	// imagem já cacheada (primeiro uso do engine, ou node novo do cluster autoscaler), puxar
	// imagens como mongo:7/mariadb:11/mcr.microsoft.com/mssql-tools18 facilmente passa de 30s
	// dependendo da rede/registry — a operação quebrava com "timeout esperando ephemeral
	// container ficar pronto" mesmo com o pull ainda em andamento (`Waiting.Reason ==
	// "ContainerCreating"`, que `waitDBEphemeralContainerRunning` já trata como "ainda esperando",
	// não erro — só o deadline geral é que era curto demais). Bug real relatado pelo usuário.
	dbTestEphemeralReadyTimeout = 120 * time.Second
	dbTestEphemeralPollInterval = 500 * time.Millisecond

	dbTestDefaultTimeoutMs = 5000
	dbTestMaxTimeoutMs     = 15000

	// dbTestBrowseMinTimeoutMs é o piso de tempo do estágio "Explorar dados" — INDEPENDENTE do
	// timeout configurado pelo usuário (pensado pra conectividade: um único round-trip rápido).
	// Explorar dados pode fazer VÁRIOS round-trips (ex: um $collStats por collection no Mongo,
	// groupColumnsToTablesWithStats no Postgres/MySQL com JOINs) — em bancos reais com collections
	// grandes/muitas tabelas, isso facilmente estoura os 5s default ou até os 15s do teto de
	// conectividade, derrubando com "exit status 124" (timeout do `timeout` do Linux) mesmo com a
	// consulta certa. Vale só pra esse estágio; conectividade continua limitada por timeoutMs.
	dbTestBrowseMinTimeoutMs = 30000

	// dbRedisScanCap limita quantas chaves o estágio de navegação do Redis retorna — `--scan`
	// sozinho não tem limite total (só controla o tamanho do lote por iteração do cursor), então
	// SEMPRE fazemos pipe com `head` pra garantir um teto real. Nunca usar `KEYS *` (bloqueia
	// instâncias grandes).
	dbRedisScanCap = 100

	// dbTestPreviewDefaultLimit/MaxLimit limitam quantas linhas/documentos/itens a amostra de
	// dados (Preview) retorna — nunca um dump completo, mesmo espírito do dbRedisScanCap.
	dbTestPreviewDefaultLimit = 20
	dbTestPreviewMaxLimit     = 100
)

// dbTestObjectNameRegex valida o nome de tabela/collection/chave recebido no Preview antes de
// interpolar na query — identificadores reais (vindos da própria listagem do Browse) só têm
// letras, dígitos, `_`, `.` e `-` (hífen aparece com frequência em nomes de collection reais,
// ex: "permissoes-de-aplicacao-dat"). Qualquer coisa fora disso (aspas, `;`, espaço, backtick) é
// rejeitada — não é sanitização de SQL, é um allow-list que nunca deixa a query ser quebrada.
var dbTestObjectNameRegex = regexp.MustCompile(`^[A-Za-z0-9_.\-]+$`)

// quotePostgresIdentifier envolve um identificador (já validado por dbTestObjectNameRegex) em
// aspas duplas — escapa aspas internas por segurança extra, mesmo que a regex já as rejeite.
func quotePostgresIdentifier(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// quoteMySQLIdentifier — mesma ideia de quotePostgresIdentifier, mas com backtick (sintaxe do
// MySQL/MariaDB pra identificadores).
func quoteMySQLIdentifier(s string) string {
	return "`" + strings.ReplaceAll(s, "`", "``") + "`"
}

// quoteSQLServerIdentifier — mesma ideia, colchetes (sintaxe T-SQL pra identificadores).
func quoteSQLServerIdentifier(s string) string {
	return "[" + strings.ReplaceAll(s, "]", "]]") + "]"
}

// dbPreviewParams agrupa os parâmetros de uma consulta de amostra de dados paginada — evita uma
// assinatura de função com 5+ parâmetros posicionais repetida entre buildPreview e suas 4
// implementações (Postgres/MySQL/Mongo/Redis).
type dbPreviewParams struct {
	Object string
	// SortColumn vazio = sem ORDER BY/sort (ordem "natural" do banco, não garantida entre
	// páginas — só cabe ao chamador decidir se aceita esse risco).
	SortColumn string
	// SortDir já normalizado ("asc" ou "desc") antes de chegar aqui — ver validação no handler
	// Preview.
	SortDir string
	Limit   int
	Offset  int
}

// Classificação do estágio de conectividade — mesmo vocabulário do Kafka (kafkaStage*), mas
// namespaced pra DB pra não colidir.
const (
	dbStageOK            = "ok"
	dbStageTCPFailed     = "tcp_failed"
	dbStageAuthFailed    = "auth_failed"
	dbStageTLSFailed     = "tls_failed"
	dbStageUnknownFailed = "unknown_failed"
)

// DBSecretRef aponta pra um Secret K8s de onde ler username/password — mesmo padrão de
// KafkaSecretRef (kafka_test_tool.go), nunca trafega de volta pro frontend.
type DBSecretRef struct {
	Namespace   string `json:"namespace"`
	Name        string `json:"name"`
	UsernameKey string `json:"username_key"`
	PasswordKey string `json:"password_key"`
	// Base64Decode decodifica username/password mais uma vez depois de ler do Secret — mesmo
	// campo/motivo de KafkaSecretRef.Base64Decode (valor sincronizado de fonte externa, ex: Azure
	// Key Vault via external-secrets, que já é ele mesmo uma string em base64).
	Base64Decode bool `json:"base64_decode,omitempty"`
}

// DBConfigMapRef aponta pra um ConfigMap K8s de onde ler host/porta — mesmo espírito do
// DBSecretRef pra credenciais, mas ConfigMap porque host/porta não são dado sensível. Usado
// quando o endereço do banco já está parametrizado num ConfigMap existente do workload (padrão
// comum: `db-host`/`db-port` ou similar), evitando digitar de novo na ferramenta.
type DBConfigMapRef struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	HostKey   string `json:"host_key"` // default no backend: "host"
	PortKey   string `json:"port_key"` // default no backend: "port"
}

// DBConnStringRef aponta pra um Secret OU ConfigMap K8s de onde ler uma connection string
// completa (comum ter a credencial já embutida na URI, ex:
// "mongodb://user:pass@host:27017/db?..." — por isso Kind aceita as duas fontes: alguns times
// guardam isso num Secret, outros (menos seguro, mas é o que existe) num ConfigMap comum).
type DBConnStringRef struct {
	Kind      string `json:"kind"` // configmap | secret — default no backend: "configmap"
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Key       string `json:"key"` // default no backend: "connectionString"
}

// DBAuthConfig descreve autenticação opcional pro teste. Mode decide qual combinação de campos é
// usada — "none" (sem autenticação), "userpass" (Username/Password ou SecretRef, mutuamente
// exclusivos — SecretRef tem prioridade se ambos vierem preenchidos por engano) ou "connstring"
// (ConnectionString digitada OU ConnStringRef lida de Secret/ConfigMap — ConnStringRef tem
// prioridade se ambos vierem preenchidos por engano — com prioridade sobre Host/Port/Database).
type DBAuthConfig struct {
	Mode             string           `json:"mode"` // none | userpass | connstring
	Username         string           `json:"username,omitempty"`
	Password         string           `json:"password,omitempty"`
	ConnectionString string           `json:"connection_string,omitempty"`
	ConnStringRef    *DBConnStringRef `json:"connstring_ref,omitempty"`
	Database         string           `json:"database,omitempty"`
	UseTLS           bool             `json:"use_tls"`
	SkipTLSVerify    bool             `json:"skip_tls_verify"`
	SecretRef        *DBSecretRef     `json:"secret_ref,omitempty"`
	// AuthMechanism só se aplica a Mongo em modo "userpass" (embutido na própria URI como
	// query param authMechanism) — no modo "connstring" isso já vem na string, se o usuário
	// precisar. Vazio deixa o mongosh negociar automaticamente (comportamento de sempre).
	// Valores aceitos: "" | "SCRAM-SHA-1" | "SCRAM-SHA-256" (ver dbValidMongoAuthMechanisms).
	AuthMechanism string `json:"auth_mechanism,omitempty"`
}

// RunDBTestRequest é o body do POST /db-test/run.
type RunDBTestRequest struct {
	// ExecutionMode decide ONDE o teste roda: "pod" (default) — ephemeral container anexado a um
	// pod real do Deployment, reflete NetworkPolicy/Istio — ou "local" — subprocesso direto no
	// host do servidor, sem tocar o cluster K8s (útil quando o banco é alcançável direto da rede
	// do servidor e não faz sentido simular a identidade de rede de um workload específico).
	ExecutionMode string `json:"execution_mode"` // pod | local
	Cluster       string `json:"cluster"`
	Namespace     string `json:"namespace"`
	// Deployment identifica de QUAL workload o teste deve partir — mesmo motivo do teste de
	// Kafka: o ephemeral container herda a identidade de rede real desse Deployment
	// (NetworkPolicy/Istio avaliam por label/service account do pod, não por namespace inteiro).
	// Só usado/obrigatório quando ExecutionMode="pod".
	Deployment string `json:"deployment"`
	Engine     string `json:"engine"` // postgres | mysql | mongodb | redis | sqlserver
	Host       string `json:"host"`
	Port       int    `json:"port"`
	// HostConfigMapRef, quando presente, resolve Host/Port a partir de um ConfigMap em vez dos
	// campos acima — só válido quando Auth.Mode != "connstring" (a connection string já embute
	// host/porta).
	HostConfigMapRef *DBConfigMapRef `json:"host_configmap_ref,omitempty"`
	Auth             DBAuthConfig    `json:"auth"`
	// Browse lê (só leitura, nada é escrito) a lista de databases/tabelas/collections/chaves —
	// diferente do produce/consume do Kafka, não precisa de confirmação explícita porque nunca
	// muta o banco.
	Browse bool `json:"browse"`
	// RedisKeyPattern filtra o Browse via SCAN...MATCH — só usado quando Engine == "redis".
	// Vazio/omitido = sem filtro ("*").
	RedisKeyPattern string `json:"redis_key_pattern,omitempty"`
	TimeoutMs       int    `json:"timeout_ms"`
}

// DBPreviewRequest é o body do POST /db-test/preview — mesma conexão/auth de RunDBTestRequest
// (reaproveitada via embedding, validada pela mesma validateDBTestRequest), mais o objeto
// específico (tabela/collection/chave) cujo CONTEÚDO real (não só metadados) o usuário quer ver.
// Síncrono, sem SSE — mesmo padrão de ListTopics/TopicsOverview do Teste de Kafka.
type DBPreviewRequest struct {
	RunDBTestRequest
	// Database é o banco/índice onde Object vive — mesmo campo/fallback de connection string de
	// Auth.Database (ver effectiveDatabase); pro Redis é o índice 0-15 (vazio = 0).
	Database string `json:"database"`
	// Object é o nome da tabela/collection/chave a visualizar — validado contra
	// dbTestObjectNameRegex (sem aspas/espaços/`;`, protege a query montada por interpolação).
	Object string `json:"object"`
	// Limit — linhas/documentos/itens retornados por página. Default dbTestPreviewDefaultLimit,
	// teto dbTestPreviewMaxLimit.
	Limit int `json:"limit,omitempty"`
	// Offset pagina o resultado — LIMIT N OFFSET M (Postgres/MySQL) / .skip(M).limit(N) (Mongo).
	// Junto com SortColumn, é o que torna a paginação estável entre páginas (sem ordenação
	// nenhuma, o banco não garante devolver as mesmas linhas na mesma ordem em consultas
	// diferentes). Não suportado pro Redis fora de list/zset (ver buildPreview de cada engine).
	Offset int `json:"offset,omitempty"`
	// SortColumn — nome da coluna/campo pra ordenar antes de paginar. Vazio = sem ORDER BY/sort
	// (ordem "natural" do banco, não garantida entre páginas). Validado contra
	// dbTestObjectNameRegex, mesmo allow-list do Object.
	SortColumn string `json:"sort_column,omitempty"`
	// SortDir — "asc" (default) ou "desc".
	SortDir string `json:"sort_dir,omitempty"`
}

// DBPreviewResponse é o resultado do estágio de amostra de dados.
type DBPreviewResponse struct {
	Status  string `json:"status"` // ok | failed
	Message string `json:"message"`
	// Rows vem preenchido quando o engine consegue estruturar a saída (Postgres/MySQL/Mongo,
	// sempre; Redis não — tipos de chave variam demais pra um formato único). Cada mapa é uma
	// linha/documento, chave = nome da coluna/campo.
	Rows []map[string]any `json:"rows,omitempty"`
	// Truncated é true quando Limit cortou o resultado nesta página — nunca dá pra saber com
	// certeza se existem MAIS linhas sem uma contagem à parte, então é só um aviso, não garantia.
	Truncated bool `json:"truncated,omitempty"`
	// HasMore é o mesmo sinal de Truncated, mas pensado pra paginação (nome mais claro do lado do
	// botão "Próxima página" no frontend): heurística "página cheia" (len(rows) >= Limit) — sem
	// COUNT(*) à parte (caro, evitado de propósito), não é garantia de que EXISTE próxima página,
	// só que é possível.
	HasMore   bool   `json:"has_more,omitempty"`
	Offset    int    `json:"offset"`
	Limit     int    `json:"limit"`
	RawOutput string `json:"raw_output,omitempty"`
}

// DBStageResult é o resultado do estágio de conectividade/autenticação.
type DBStageResult struct {
	Status    string `json:"status"` // ok | tcp_failed | auth_failed | tls_failed | unknown_failed
	Message   string `json:"message"`
	RawOutput string `json:"raw_output"`
}

// DBBrowseObject é um objeto individual listado pelo estágio de navegação — o que ele representa
// depende do ObjectType do resultado (ver DBBrowseResult): nome de database/tabela/collection/
// chave. Type/Detail dão contexto extra quando disponível (ex: tipo de dado de chave Redis,
// colunas de uma tabela, contagem estimada de documentos de uma collection).
type DBBrowseObject struct {
	Name string `json:"name"`
	// Type: por tabela é sempre "table"; por chave Redis é o tipo real (string/hash/list/set/
	// zset/stream); databases/collections não usam este campo.
	Type string `json:"type,omitempty"`
	// Detail: resumo legível — colunas+tipos (tabela), contagem de documentos (collection),
	// tamanho em disco (database). Vazio quando não há nada relevante a mostrar.
	Detail string `json:"detail,omitempty"`
	// Count/SizeBytes/StorageSizeBytes: estatísticas estruturadas no mesmo espírito do "All Stats"
	// do MongoDB Compass (Collection/Count/Size/StorageSize) — populadas só para tabelas Postgres/
	// MySQL (reltuples/table_rows + pg_relation_size/data_length, estimativas de catálogo, nunca um
	// scan) e collections Mongo ($collStats, mesmo princípio de segurança do
	// estimatedDocumentCount já usado antes). Omitidas (0) para database/key, onde não se aplica.
	Count int64 `json:"count,omitempty"`
	// SizeBytes: tamanho "lógico" do dado (heap da tabela no Postgres, data_length no MySQL, size
	// do $collStats no Mongo — sem contar índices/overhead).
	SizeBytes int64 `json:"size_bytes,omitempty"`
	// StorageSizeBytes: tamanho total em disco incluindo índices/overhead (pg_total_relation_size,
	// data_length+index_length, storageSize do $collStats) — equivalente ao "Storage Size" do
	// MongoDB Compass.
	StorageSizeBytes int64 `json:"storage_size_bytes,omitempty"`
}

// DBBrowseResult é o resultado do estágio de navegação só-leitura. ObjectType varia por engine E
// por ter ou não um Database informado (ex: Postgres sem Database lista "database"; com Database
// informado, desce um nível e lista "table" com colunas — ver dbEngines).
type DBBrowseResult struct {
	Status     string           `json:"status"` // ok | failed | skipped
	Message    string           `json:"message"`
	ObjectType string           `json:"object_type,omitempty"` // database | table | collection | key
	Objects    []DBBrowseObject `json:"objects,omitempty"`
	// Database é o banco (ou índice, no Redis) efetivamente usado nesta listagem — resolvido via
	// dbEngine.resolveDatabaseLabel, cobre o caso do campo "Database" ter ficado vazio e o valor
	// real ter vindo do fallback de connection string (ver effectiveDatabase). Só preenchido
	// quando ObjectType != "database" (nesse nível não há um banco "escolhido" ainda, é a própria
	// lista de bancos). O frontend usa isso pra mostrar a hierarquia banco → tabelas/collections,
	// já que sem isso a lista de objetos aparecia solta, sem dizer de qual banco eles vêm.
	Database string `json:"database,omitempty"`
	// Truncated é true quando o resultado foi cortado por um teto de segurança (só acontece no
	// Redis — SCAN sobre um keyspace grande) — a lista exibida é uma AMOSTRA, não completa.
	Truncated bool `json:"truncated,omitempty"`
	// ServerInfo só é preenchido pro Redis, no nível "database" (topo) — estatísticas do próprio
	// servidor (versão, memória, clientes, hit rate), ver RedisServerInfo/parseRedisServerInfo.
	// nil e omitido no JSON pros demais engines e pros níveis mais fundos da navegação do Redis.
	ServerInfo *RedisServerInfo `json:"redis_server_info,omitempty"`
	// InfoSections é o parsing genérico e completo de `redis-cli INFO` em seções (Server, Clients,
	// Memory, CPU, Persistence, Stats, Replication, Cluster, Keyspace, etc.) — usado pelo frontend
	// pra renderizar o modal de "saída bruta" como abas organizadas por assunto em vez de um texto
	// corrido. Só pro Redis, nível "database". RawOutput continua disponível como fallback pra
	// quem quiser o texto puro original.
	InfoSections []RedisInfoSection `json:"redis_info_sections,omitempty"`
	RawOutput    string             `json:"raw_output"`
}

// RedisServerInfo resume os campos mais relevantes de `redis-cli INFO` (seções Server/Clients/
// Memory/Stats/Replication, retornadas juntas quando INFO roda sem argumento de seção) — exibido
// no nível "database" (topo) da navegação do Redis, ao lado da lista de bancos lógicos 0-15 já
// existente (parseRedisKeyspaceInfo, extraída da MESMA saída). Nil quando o parsing não encontra
// nenhum campo reconhecido (saída inesperada — ex: erro do redis-cli capturado como stdout).
type RedisServerInfo struct {
	Version          string `json:"version,omitempty"`
	Mode             string `json:"mode,omitempty"` // standalone | cluster | sentinel
	Role             string `json:"role,omitempty"` // master | slave
	UptimeDays       int64  `json:"uptime_days"`
	ConnectedClients int64  `json:"connected_clients"`
	UsedMemoryHuman  string `json:"used_memory_human,omitempty"`
	MaxMemoryHuman   string `json:"maxmemory_human,omitempty"`
	KeyspaceHits     int64  `json:"keyspace_hits"`
	KeyspaceMisses   int64  `json:"keyspace_misses"`
	// HitRatePct é calculado a partir de hits/misses — -1 (em vez de 0) quando não há dados
	// suficientes (servidor recém-iniciado, hits==misses==0) pra distinguir de uma taxa real de
	// 0% (frontend trata -1 como "sem dados ainda", não mostra o número).
	HitRatePct float64 `json:"hit_rate_pct"`
	// InstantaneousOpsPerSec é o throughput ATUAL (comandos/segundo, média de amostragem do
	// próprio Redis) — direto do campo instantaneous_ops_per_sec do INFO, sem cálculo nosso.
	InstantaneousOpsPerSec int64 `json:"instantaneous_ops_per_sec"`
	// TotalReadsProcessed/TotalWritesProcessed são os contadores ACUMULADOS (desde o start do
	// processo, não uma taxa por segundo) de total_reads_processed/total_writes_processed do
	// INFO — nível de syscall de socket, não "comando de leitura" vs "comando de escrita" (ex:
	// um único GET pode gerar mais de 1 read se a resposta vier fragmentada) — é a métrica de
	// leitura/escrita que o próprio Redis expõe, não uma classificação nossa por tipo de comando.
	TotalReadsProcessed  int64 `json:"total_reads_processed"`
	TotalWritesProcessed int64 `json:"total_writes_processed"`
	// ReadPct/WritePct somam 100 entre si — proporção de leitura vs escrita desde o start do
	// servidor. -1 quando reads+writes == 0 (servidor sem nenhuma operação registrada ainda),
	// mesmo tratamento do HitRatePct pra não confundir "sem dado" com uma proporção real de 0%.
	ReadPct  float64 `json:"read_pct"`
	WritePct float64 `json:"write_pct"`
	// AvgLatencyMs é a latência REAL média por chamada (não estimada), calculada a partir de
	// TODAS as entradas cmdstat_* da seção Commandstats do `INFO all` (soma de usec / soma de
	// calls, convertido pra ms) — só existe quando o INFO pedido inclui essa seção (ausente do
	// INFO default, presente em `INFO all`/`INFO commandstats`). -1 quando não há nenhum comando
	// registrado ainda (servidor sem tráfego desde o start).
	AvgLatencyMs float64 `json:"avg_latency_ms"`
	// SlowestCommand/SlowestCommandLatencyMs identificam o comando com maior usec_per_call entre
	// os registrados — útil pra apontar rapidamente o que está pesando na latência média (ex: um
	// KEYS ou um comando O(N) mal-usado). Vazio/-1 quando não há dados.
	SlowestCommand          string  `json:"slowest_command,omitempty"`
	SlowestCommandLatencyMs float64 `json:"slowest_command_latency_ms"`
}

// redisInfoFieldRegex casa linhas "campo:valor" da saída de `redis-cli INFO` — o formato usa
// `\r\n` como separador de linha e `#` pra cabeçalho de seção (ex: "# Server"), ambos ignorados
// aqui (a regex só captura linhas "identificador:resto-da-linha").
var redisInfoFieldRegex = regexp.MustCompile(`(?m)^([a-zA-Z_][a-zA-Z0-9_]*):(.*?)\r?$`)

// parseRedisServerInfo extrai os campos usados por RedisServerInfo da saída bruta de `INFO`
// (sem argumento — todas as seções default). Best-effort: campos ausentes/malformados ficam no
// zero-value, nunca abortam o parsing dos demais.
func parseRedisServerInfo(raw string) *RedisServerInfo {
	fields := make(map[string]string)
	for _, m := range redisInfoFieldRegex.FindAllStringSubmatch(raw, -1) {
		fields[m[1]] = strings.TrimSpace(m[2])
	}
	if len(fields) == 0 {
		return nil
	}

	info := &RedisServerInfo{
		Version:         fields["redis_version"],
		Mode:            fields["redis_mode"],
		Role:            fields["role"],
		UsedMemoryHuman: fields["used_memory_human"],
		MaxMemoryHuman:  fields["maxmemory_human"],
	}
	if v, err := strconv.ParseInt(fields["uptime_in_days"], 10, 64); err == nil {
		info.UptimeDays = v
	}
	if v, err := strconv.ParseInt(fields["connected_clients"], 10, 64); err == nil {
		info.ConnectedClients = v
	}
	hits, hitsErr := strconv.ParseInt(fields["keyspace_hits"], 10, 64)
	misses, missesErr := strconv.ParseInt(fields["keyspace_misses"], 10, 64)
	info.HitRatePct = -1
	if hitsErr == nil && missesErr == nil {
		info.KeyspaceHits = hits
		info.KeyspaceMisses = misses
		if total := hits + misses; total > 0 {
			info.HitRatePct = float64(hits) / float64(total) * 100
		}
	}
	if v, err := strconv.ParseInt(fields["instantaneous_ops_per_sec"], 10, 64); err == nil {
		info.InstantaneousOpsPerSec = v
	}
	reads, readsErr := strconv.ParseInt(fields["total_reads_processed"], 10, 64)
	writes, writesErr := strconv.ParseInt(fields["total_writes_processed"], 10, 64)
	info.ReadPct = -1
	info.WritePct = -1
	if readsErr == nil && writesErr == nil {
		info.TotalReadsProcessed = reads
		info.TotalWritesProcessed = writes
		if total := reads + writes; total > 0 {
			info.ReadPct = float64(reads) / float64(total) * 100
			info.WritePct = 100 - info.ReadPct
		}
	}
	info.AvgLatencyMs, info.SlowestCommand, info.SlowestCommandLatencyMs = parseRedisCommandStats(fields)
	return info
}

// parseRedisCommandStats calcula a latência média REAL (não estimada) a partir das entradas
// cmdstat_* da seção Commandstats do `INFO all` — cada linha tem o formato
// "cmdstat_<comando>:calls=N,usec=N,usec_per_call=F,rejected_calls=N,failed_calls=N". A média
// geral é ponderada por chamada (soma de usec / soma de calls, entre TODOS os comandos), não uma
// média simples dos usec_per_call de cada comando — isso evita que um comando raríssimo e lento
// distorça a média geral do mesmo jeito que um comando frequente e rápido. `fields` já vem do
// parsing genérico de parseRedisServerInfo (qualquer linha "chave:valor" do INFO, sem filtro de
// seção) — só filtra pelo prefixo aqui. Retorna (-1, "", -1) quando não há nenhum cmdstat_* (INFO
// sem a seção Commandstats, ou servidor sem nenhum comando executado ainda).
func parseRedisCommandStats(fields map[string]string) (avgLatencyMs float64, slowestCmd string, slowestMs float64) {
	var totalUsec, totalCalls int64
	var maxUsecPerCall float64

	for key, value := range fields {
		cmdName, ok := strings.CutPrefix(key, "cmdstat_")
		if !ok {
			continue
		}
		var calls, usec int64
		var usecPerCall float64
		for _, part := range strings.Split(value, ",") {
			kv := strings.SplitN(part, "=", 2)
			if len(kv) != 2 {
				continue
			}
			switch kv[0] {
			case "calls":
				calls, _ = strconv.ParseInt(kv[1], 10, 64)
			case "usec":
				usec, _ = strconv.ParseInt(kv[1], 10, 64)
			case "usec_per_call":
				usecPerCall, _ = strconv.ParseFloat(kv[1], 64)
			}
		}
		totalCalls += calls
		totalUsec += usec
		if usecPerCall > maxUsecPerCall {
			maxUsecPerCall = usecPerCall
			slowestCmd = cmdName
		}
	}

	if totalCalls == 0 {
		return -1, "", -1
	}
	return float64(totalUsec) / float64(totalCalls) / 1000, slowestCmd, maxUsecPerCall / 1000
}

// RedisInfoField é um par campo:valor de uma seção de `redis-cli INFO` — genérico o suficiente
// pra cobrir qualquer seção (inclusive as que a app não conhece de antemão, ex: campos novos de
// uma versão futura do Redis), sem precisar de um struct dedicado por seção.
type RedisInfoField struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// RedisInfoSection é uma seção de `redis-cli INFO` (ex: "Server", "Clients", "Memory") com seus
// campos na ordem em que aparecem na saída original — usado pelo frontend pra agrupar seções
// relacionadas em abas (ex: Server+Clients+CPU+Memory numa aba só, ver DatabaseTestTab.tsx).
type RedisInfoSection struct {
	Name   string           `json:"name"`
	Fields []RedisInfoField `json:"fields"`
}

// redisInfoSectionHeaderRegex casa o cabeçalho de seção do INFO ("# Server", "# CPU", etc — o
// nome logo após "# ", sempre no início da linha).
var redisInfoSectionHeaderRegex = regexp.MustCompile(`^#\s+(.+)$`)

// parseRedisInfoSections faz o parsing GENÉRICO e completo da saída de `redis-cli INFO` (CRLF
// entre linhas) em seções ordenadas com seus campos — ao contrário de parseRedisServerInfo (que
// extrai só um punhado de campos curados pro card de estatísticas), isso preserva TODAS as seções
// e campos, na ordem original, pra alimentar a visualização em abas do modal de saída bruta.
// Linhas em branco (separador entre seções) e linhas sem ":" (nunca acontece numa saída válida do
// INFO, mas ignorado por segurança) são puladas. Best-effort: uma seção sem nenhum campo
// reconhecido simplesmente não aparece no resultado.
func parseRedisInfoSections(raw string) []RedisInfoSection {
	var sections []RedisInfoSection
	var current *RedisInfoSection

	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimRight(line, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if m := redisInfoSectionHeaderRegex.FindStringSubmatch(trimmed); m != nil {
			sections = append(sections, RedisInfoSection{Name: m[1]})
			current = &sections[len(sections)-1]
			continue
		}
		if current == nil {
			continue
		}
		idx := strings.Index(trimmed, ":")
		if idx < 0 {
			continue
		}
		current.Fields = append(current.Fields, RedisInfoField{
			Key:   trimmed[:idx],
			Value: trimmed[idx+1:],
		})
	}
	return sections
}

// DBTestResult é o resultado completo de uma execução do teste de banco de dados.
type DBTestResult struct {
	// TargetPod/EphemeralContainer: mesma transparência do teste de Kafka — ephemeral containers
	// não podem ser removidos via API do K8s, ficam no pod até ele reiniciar.
	TargetPod          string         `json:"target_pod"`
	EphemeralContainer string         `json:"ephemeral_container"`
	Connectivity       DBStageResult  `json:"connectivity"`
	Browse             DBBrowseResult `json:"browse"`
}

// dbConnParams é a forma já resolvida (credenciais incluídas) passada pros construtores de
// comando de cada engine.
type dbConnParams struct {
	Mode     string // none | userpass | connstring
	Host     string
	Port     int
	Username string
	Password string
	// Database: nome do banco (Postgres/MySQL/Mongo) OU índice numérico 0-15 do banco lógico do
	// Redis (Redis não tem nomes de banco, só um índice via `SELECT`/`-n` — ver redisEffectiveTarget).
	Database      string
	ConnStr       string
	UseTLS        bool
	SkipTLSVerify bool
	// AuthMechanism — ver DBAuthConfig.AuthMechanism. Só usado por buildMongoURI.
	AuthMechanism string
	// RedisKeyPattern filtra o estágio de navegação do Redis via `SCAN ... MATCH <pattern>`
	// (`redis-cli --scan --pattern <pattern>`) — só usado quando o engine é Redis. Vazio = sem
	// filtro (equivalente a MATCH "*").
	RedisKeyPattern string
}

// dbEngine descreve um motor de banco suportado: imagem do ephemeral container, como montar os
// scripts de conectividade/navegação, e como classificar erros. Registry — adicionar um novo
// engine é só um novo item no map dbEngines, sem mexer no resto do handler.
type dbEngine struct {
	label             string
	image             string // tag fixada, nunca "latest"
	buildConnectivity func(p dbConnParams, timeoutSec int) string
	// buildBrowse é nil quando o engine não suporta navegação (nenhum caso na v1). O script
	// retornado varia com p.Database — sem Database informado lista o nível "database"; com
	// Database informado, desce um nível (tabelas/collections) — ver cada engine no registry.
	buildBrowse func(p dbConnParams, timeoutSec int) string
	// parseBrowseOutput devolve os objetos parseados, o ObjectType que rotula o resultado
	// (varia com p.Database, mesma lógica de buildBrowse) e se o resultado foi truncado.
	parseBrowseOutput func(raw string, p dbConnParams) (objects []DBBrowseObject, objectType string, truncated bool)
	// resolveDatabaseLabel devolve o banco (ou índice, no Redis) EFETIVAMENTE usado — cobre o
	// fallback de connection string (effectiveDatabase) e a extração própria do MySQL
	// (mysqlEffectiveParams) — usado só pra popular DBBrowseResult.Database, exibido no frontend
	// como contexto de hierarquia acima da lista de tabelas/collections/chaves.
	resolveDatabaseLabel func(p dbConnParams) string
	// buildPreview monta o comando pra buscar uma amostra dos dados REAIS de um objeto específico
	// (tabela/collection/chave) — diferente de buildBrowse (metadados/estatísticas via catálogo),
	// isso lê o conteúdo de verdade, sempre paginado (LIMIT/OFFSET ou skip/limit — nunca um dump
	// completo de uma vez).
	buildPreview func(p dbConnParams, pv dbPreviewParams, timeoutSec int) string
	// parsePreviewOutput estrutura a saída em linhas/documentos genéricos (chave=nome da coluna/
	// campo). Nil quando o engine não consegue estruturar de forma confiável (Redis: tipo da
	// chave só é conhecido em runtime, formatos de saída incompatíveis entre si) — nesse caso o
	// handler devolve só RawOutput e o frontend cai pra exibição de texto puro.
	parsePreviewOutput func(raw string) ([]map[string]any, error)
	// parseServerInfo extrai estatísticas do próprio servidor (versão, memória, clientes
	// conectados, hit rate) da MESMA saída de buildBrowse no nível "database" — nil pros engines
	// que não expõem isso nesse ponto (Postgres/MySQL/Mongo listam só nomes de bancos ali, sem
	// seção de estatísticas embutida na mesma chamada). Só o Redis usa isso hoje (ver
	// parseRedisServerInfo) — tipo `*RedisServerInfo` mesmo sendo genérico na struct porque é o
	// único consumidor; DBBrowseResult.ServerInfo fica nil e omitido no JSON pros demais engines.
	parseServerInfo func(raw string) *RedisServerInfo
	// parseInfoSections faz o parsing genérico e completo (todas as seções/campos) da MESMA saída
	// usada por parseServerInfo — alimenta a visualização em abas do modal de saída bruta. Só o
	// Redis usa isso hoje (ver parseRedisInfoSections).
	parseInfoSections func(raw string) []RedisInfoSection
	networkErrorRegex *regexp.Regexp
	authErrorRegex    *regexp.Regexp
	tlsErrorRegex     *regexp.Regexp
}

// namesToObjects converte uma lista simples de nomes (sem type/detail) em DBBrowseObject — usado
// pelos níveis "database" (Postgres/MySQL/Mongo), onde não há informação extra a mostrar.
func namesToObjects(names []string) []DBBrowseObject {
	objects := make([]DBBrowseObject, 0, len(names))
	for _, n := range names {
		objects = append(objects, DBBrowseObject{Name: n})
	}
	return objects
}

// dbBrowseMaxColumnsShown limita quantas colunas aparecem no Detail de uma tabela antes de
// resumir em "(+N mais)" — tabelas largas (dezenas de colunas) ficariam ilegíveis sem isso.
const dbBrowseMaxColumnsShown = 6

// groupColumnsToTablesWithStats agrupa linhas "tabela|coluna|tipo|..." em um DBBrowseObject por
// tabela, com Detail resumindo as colunas. Espera 6 campos por linha —
// "tabela|coluna|tipo|size_bytes|storage_size_bytes|row_estimate" — formato comum
// usado tanto pela query do Postgres (pg_relation_size/pg_total_relation_size/reltuples) quanto
// pela do MySQL (data_length/data_length+index_length/table_rows), todos estimativas de catálogo
// (nunca um COUNT(*) ou scan completo — seguro mesmo em tabelas grandes, mesmo espírito do
// estimatedDocumentCount do Mongo). size_bytes/storage_size_bytes/row_estimate são repetidos em
// toda linha da mesma tabela (uma por coluna) — só a primeira ocorrência é usada.
func groupColumnsToTablesWithStats(lines []string) []DBBrowseObject {
	var order []string
	cols := map[string][]string{}
	sizeBytes := map[string]int64{}
	storageSizeBytes := map[string]int64{}
	rowCount := map[string]int64{}
	for _, l := range lines {
		parts := strings.SplitN(l, "|", 6)
		if len(parts) != 6 {
			continue
		}
		table, col, typ := parts[0], parts[1], parts[2]
		if _, ok := cols[table]; !ok {
			order = append(order, table)
			sizeBytes[table], _ = strconv.ParseInt(parts[3], 10, 64)
			storageSizeBytes[table], _ = strconv.ParseInt(parts[4], 10, 64)
			rowCount[table], _ = strconv.ParseInt(parts[5], 10, 64)
		}
		cols[table] = append(cols[table], col+" "+typ)
	}
	objects := make([]DBBrowseObject, 0, len(order))
	for _, table := range order {
		colList := cols[table]
		shown := colList[:min(len(colList), dbBrowseMaxColumnsShown)]
		detail := strings.Join(shown, ", ")
		if len(colList) > dbBrowseMaxColumnsShown {
			detail += fmt.Sprintf(" (+%d mais)", len(colList)-dbBrowseMaxColumnsShown)
		}
		objects = append(objects, DBBrowseObject{
			Name: table, Type: "table", Detail: detail,
			Count: rowCount[table], SizeBytes: sizeBytes[table], StorageSizeBytes: storageSizeBytes[table],
		})
	}
	return objects
}

// jsStringLiteral escapa uma string Go pra uso seguro como literal dentro de um script mongosh
// (`--eval`) — literais JSON são um subconjunto válido de literais JS, então json.Marshal já
// resolve aspas/backslashes corretamente sem precisar de um escapador JS dedicado.
func jsStringLiteral(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// formatBytesShort formata um tamanho em bytes de forma legível (KB/MB/GB) — usado no Detail de
// databases do Mongo (sizeOnDisk).
func formatBytesShort(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

// connTargetOrURI devolve a ConnectionString informada pelo usuário quando Mode=="connstring",
// senão delega em `build` pra montar a URI a partir dos campos discretos — helper compartilhado
// pelos engines que aceitam URI nativa (Postgres/Mongo).
func connTargetOrURI(p dbConnParams, build func(dbConnParams) string) string {
	if p.Mode == "connstring" {
		return p.ConnStr
	}
	return build(p)
}

// connStringDatabase extrai o nome do banco do path de uma connection string genérica
// (postgresql://.../dbname, mongodb://.../dbname, mongodb+srv://.../dbname) — muitas connection
// strings JÁ trazem o banco embutido (ex: copiadas direto de um app real), sem exigir que o
// usuário digite de novo no campo separado "Database". `url.Parse` funciona igual pra qualquer
// esquema `scheme://...`, incluindo `mongodb+srv://`.
func connStringDatabase(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(u.Path, "/")
}

// uriSchemeRegex casa o "esquema://" no início de uma string, se houver — usado só pra montar
// mensagens de erro mais específicas (ver isValidPostgresConnString/isValidMongoConnString
// abaixo), nunca pra decidir validade por si só.
var uriSchemeRegex = regexp.MustCompile(`^([a-zA-Z][a-zA-Z0-9+.-]*)://`)

// extractURIScheme devolve o esquema (sem "://") de uma string no formato genérico de URI, ou
// "" se não bater o padrão.
func extractURIScheme(raw string) string {
	m := uriSchemeRegex.FindStringSubmatch(strings.TrimSpace(raw))
	if m == nil {
		return ""
	}
	return m[1]
}

// effectiveDatabase resolve o banco alvo do estágio de navegação (Postgres/Mongo): o campo
// Database explícito tem prioridade — permite sobrescrever ou navegar num banco diferente do que
// está na connection string (Mongo: getSiblingDB troca de banco livremente dentro do mesmo
// cluster/replica set autenticado, então isso é um caso real). Se vazio e Mode=="connstring", cai
// pro banco embutido no path da própria connection string, se houver (comum quando ela já foi
// copiada de uma configuração real de app). Fora do modo connstring, não há de onde mais tirar o
// banco — devolve vazio mesmo (mantém o nível "database" do Explorar dados).
func effectiveDatabase(p dbConnParams) string {
	if db := strings.TrimSpace(p.Database); db != "" {
		return db
	}
	if p.Mode == "connstring" {
		return connStringDatabase(p.ConnStr)
	}
	return ""
}

func buildPostgresURI(p dbConnParams) string {
	u := url.URL{Scheme: "postgresql", Host: fmt.Sprintf("%s:%d", p.Host, p.Port)}
	if p.Username != "" {
		if p.Password != "" {
			u.User = url.UserPassword(p.Username, p.Password)
		} else {
			u.User = url.User(p.Username)
		}
	}
	db := p.Database
	if db == "" {
		db = "postgres"
	}
	u.Path = "/" + db
	q := url.Values{}
	if p.UseTLS {
		if p.SkipTLSVerify {
			q.Set("sslmode", "require")
		} else {
			q.Set("sslmode", "verify-full")
		}
	} else {
		q.Set("sslmode", "disable")
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// postgresConnStringSchemes são os únicos esquemas que `psql` reconhece como URI de conexão —
// fora deles ele produz erros nativos do libpq difíceis de entender sem contexto nenhum. Ex real
// relatado: usuário colou (ou leu de um Secret) uma connection string com esquema `sqlserver://`
// (comum quando o host reaproveita o domínio *.database.windows.net do Azure, usado tanto por
// Azure SQL Database quanto — via private endpoint com DNS customizado — por Azure Database for
// PostgreSQL, fácil de confundir os dois na hora de copiar a string certa) — psql tentou
// interpretar a URI inteira só como valor do host (buildPostgresURI embute p.Host cru dentro de
// outra URI `postgresql://<p.Host>:<porta>`) e devolveu o cru
// `psql: error: invalid integer value "sqlserver://host:5432" for connection option "port"`,
// sem nenhuma pista de que o problema era o esquema errado. Validado ANTES de rodar o teste
// (validateDBTestRequest/runTest), mesmo padrão de isValidRedisConnString/redisConnStringHint.
var postgresConnStringSchemes = []string{"postgresql://", "postgres://"}

func isValidPostgresConnString(s string) bool {
	lower := strings.ToLower(strings.TrimSpace(s))
	for _, scheme := range postgresConnStringSchemes {
		if strings.HasPrefix(lower, scheme) {
			return true
		}
	}
	return false
}

// postgresConnStringHint nomeia o esquema errado quando reconhecível (ex: "sqlserver://") —
// muito mais acionável do que deixar o erro cru do libpq chegar ao usuário sem contexto.
func postgresConnStringHint(raw string) string {
	if scheme := extractURIScheme(raw); scheme != "" {
		return fmt.Sprintf("Essa connection string usa o esquema %q, que não é PostgreSQL — confira se copiou a string do banco certo. Esperado: postgresql:// ou postgres://", scheme+"://")
	}
	return "Connection string do PostgreSQL deve começar com postgresql:// ou postgres:// (ex: postgresql://usuario:senha@host:porta/banco)"
}

func buildMongoURI(p dbConnParams) string {
	u := url.URL{Scheme: "mongodb", Host: fmt.Sprintf("%s:%d", p.Host, p.Port)}
	if p.Username != "" {
		if p.Password != "" {
			u.User = url.UserPassword(p.Username, p.Password)
		} else {
			u.User = url.User(p.Username)
		}
	}
	if p.Database != "" {
		u.Path = "/" + p.Database
	}
	q := url.Values{}
	if p.UseTLS {
		q.Set("tls", "true")
		if p.SkipTLSVerify {
			q.Set("tlsAllowInvalidCertificates", "true")
		}
	}
	if p.AuthMechanism != "" {
		q.Set("authMechanism", p.AuthMechanism)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// mongoConnStringSchemes são os únicos esquemas que `mongosh` aceita como URI de conexão — mesmo
// racional de postgresConnStringSchemes acima (evita repassar um esquema errado cru pro cliente
// nativo e receber de volta um erro sem contexto).
var mongoConnStringSchemes = []string{"mongodb://", "mongodb+srv://"}

func isValidMongoConnString(s string) bool {
	lower := strings.ToLower(strings.TrimSpace(s))
	for _, scheme := range mongoConnStringSchemes {
		if strings.HasPrefix(lower, scheme) {
			return true
		}
	}
	return false
}

func mongoConnStringHint(raw string) string {
	if scheme := extractURIScheme(raw); scheme != "" {
		return fmt.Sprintf("Essa connection string usa o esquema %q, que não é MongoDB — confira se copiou a string do banco certo. Esperado: mongodb:// ou mongodb+srv://", scheme+"://")
	}
	return "Connection string do MongoDB deve começar com mongodb:// ou mongodb+srv:// (ex: mongodb://usuario:senha@host:porta/banco)"
}

// dbValidMongoAuthMechanisms — mecanismos aceitos no campo AuthMechanism (modo "userpass" do
// Mongo). Escopo deliberadamente restrito aos dois mecanismos de usuário/senha comuns — MONGODB-
// X509/GSSAPI/MONGODB-AWS exigiriam certificado/config adicional que o teste não coleta hoje.
var dbValidMongoAuthMechanisms = map[string]bool{
	"SCRAM-SHA-1":   true,
	"SCRAM-SHA-256": true,
}

// parseMySQLConnString extrai host/port/user/pass/db de uma connection string estilo
// "mysql://user:pass@host:port/db" — o cliente `mysql`/`mariadb` NÃO aceita URI diretamente
// (diferente de psql/mongosh), então isso é feito no Go antes de montar os flags discretos.
func parseMySQLConnString(raw string) (host string, port int, username, password, database string) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", 0, "", "", ""
	}
	host = u.Hostname()
	if p := u.Port(); p != "" {
		port, _ = strconv.Atoi(p)
	}
	if u.User != nil {
		username = u.User.Username()
		password, _ = u.User.Password()
	}
	database = strings.TrimPrefix(u.Path, "/")
	return
}

// mysqlEffectiveParams resolve os campos discretos efetivos — da connection string quando
// Mode=="connstring", senão dos campos já presentes em `p`.
func mysqlEffectiveParams(p dbConnParams) (host string, port int, username, password, database string) {
	if p.Mode == "connstring" {
		return parseMySQLConnString(p.ConnStr)
	}
	return p.Host, p.Port, p.Username, p.Password, p.Database
}

// mysqlPasswordPrefix monta o prefixo de ambiente pra senha do mysql/mariadb (MYSQL_PWD) — ver
// envVarPrefix pro porquê do `env` explícito (bug real: sem ele, a senha nunca era aplicada).
func mysqlPasswordPrefix(p dbConnParams) string {
	_, _, _, pass, _ := mysqlEffectiveParams(p)
	return envVarPrefix("MYSQL_PWD", pass)
}

// mysqlConnStringSchemes: diferente de psql/mongosh, o cliente `mysql`/`mariadb` nunca recebe a
// URI crua (parseMySQLConnString já decompõe em host/porta/usuário/senha antes de montar os
// flags discretos, e `url.Parse` não valida o nome do esquema) — então um esquema errado aqui
// não quebra com um erro cru de driver, só conecta silenciosamente usando host/porta extraídos
// de uma string que pode não ser MySQL de verdade. Validado mesmo assim, mesmo racional de
// postgresConnStringSchemes/mongoConnStringSchemes: falha cedo e com mensagem clara em vez de
// tentar conectar num banco errado sem avisar.
var mysqlConnStringSchemes = []string{"mysql://", "mariadb://"}

func isValidMySQLConnString(s string) bool {
	lower := strings.ToLower(strings.TrimSpace(s))
	for _, scheme := range mysqlConnStringSchemes {
		if strings.HasPrefix(lower, scheme) {
			return true
		}
	}
	return false
}

func mysqlConnStringHint(raw string) string {
	if scheme := extractURIScheme(raw); scheme != "" {
		return fmt.Sprintf("Essa connection string usa o esquema %q, que não é MySQL/MariaDB — confira se copiou a string do banco certo. Esperado: mysql:// ou mariadb://", scheme+"://")
	}
	return "Connection string do MySQL/MariaDB deve começar com mysql:// ou mariadb:// (ex: mysql://usuario:senha@host:porta/banco)"
}

// sqlserverDefaultPort — porta padrão do SQL Server/Azure SQL, usada quando a connection string
// não especifica porta explicitamente (`sqlserver://host/db`, sem ":porta").
const sqlserverDefaultPort = 1433

// sqlserverCmdPath — caminho completo do binário, NÃO o nome bare "sqlcmd". BUG REAL confirmado
// ao vivo: a imagem mcr.microsoft.com/mssql-tools NÃO tem `/opt/mssql-tools/bin` no PATH padrão
// (`PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin`, confirmado via `docker
// inspect`) — chamar só `sqlcmd` (como os outros engines chamam `psql`/`mariadb`/`mongosh`/
// `redis-cli`, todos de fato no PATH das respectivas imagens) falha com
// `env: 'sqlcmd': No such file or directory`, reproduzido rodando exatamente o `docker run`
// que `execLocalDocker` monta (sem `--entrypoint`, sem override de PATH).
const sqlserverCmdPath = "/opt/mssql-tools/bin/sqlcmd"

// jdbcSQLServerPrefix — prefixo do formato de connection string do driver JDBC da Microsoft
// (Java), MUITO comum em configs reais de aplicação (Java/Spring, ferramentas corporativas,
// secrets copiados de docs internas) — descoberto ao vivo quando um usuário colou exatamente
// esse formato (`jdbc:sqlserver://host:port;database=x;user=y;password=z;encrypt=true;
// trustServerCertificate=false;...`) esperando que funcionasse. `strings.ToLower` na comparação,
// mas o prefixo em si já é lowercase (o formato real do driver é sempre minúsculo).
const jdbcSQLServerPrefix = "jdbc:sqlserver://"

// parseJDBCSQLServerParams faz o parse do formato `jdbc:sqlserver://host[:porta][;chave=valor]*`
// — bem diferente do formato URI simples (sqlserver://user:pass@host/db) que parseSQLServerConnString
// cobre: aqui host/porta vêm antes do primeiro `;`, e credenciais/banco/TLS vêm como pares
// chave=valor separados por `;` (não há um jeito de usar `url.Parse` pra isso — `;` e `=` livres
// no meio da string não formam uma URI válida). Chaves reconhecidas (nomes reais do driver JDBC
// da Microsoft, case-insensitive): database/databaseName, user, password, encrypt,
// trustServerCertificate — outras chaves (hostNameInCertificate, loginTimeout, applicationName,
// etc.) são ignoradas silenciosamente, não têm equivalente direto nos flags do sqlcmd usados
// aqui. `ok=false` quando `raw` não começa com o prefixo JDBC (deixa o chamador cair pro parser
// de URI simples).
func parseJDBCSQLServerParams(raw string) (host string, port int, username, password, database string, useTLS, skipTLSVerify, ok bool) {
	trimmed := strings.TrimSpace(raw)
	if !strings.HasPrefix(strings.ToLower(trimmed), jdbcSQLServerPrefix) {
		return "", 0, "", "", "", false, false, false
	}
	rest := trimmed[len(jdbcSQLServerPrefix):]
	parts := strings.Split(rest, ";")

	hostPort := parts[0]
	port = sqlserverDefaultPort
	if idx := strings.LastIndex(hostPort, ":"); idx >= 0 {
		host = hostPort[:idx]
		if p, err := strconv.Atoi(hostPort[idx+1:]); err == nil {
			port = p
		}
	} else {
		host = hostPort
	}

	for _, kv := range parts[1:] {
		kv = strings.TrimSpace(kv)
		if kv == "" {
			continue
		}
		eq := strings.Index(kv, "=")
		if eq < 0 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(kv[:eq]))
		val := strings.TrimSpace(kv[eq+1:])
		switch key {
		case "database", "databasename":
			database = val
		case "user", "username":
			username = val
		case "password":
			password = val
		case "encrypt":
			useTLS = strings.EqualFold(val, "true")
		case "trustservercertificate":
			skipTLSVerify = strings.EqualFold(val, "true")
		}
	}
	return host, port, username, password, database, useTLS, skipTLSVerify, true
}

// parseSQLServerConnString extrai host/port/user/pass/db de uma connection string no formato URI
// "sqlserver://user:pass@host:port/db" (também aceita mssql://, mesmo alias) — mesmo mecanismo de
// parseMySQLConnString (o cliente `sqlcmd` também não aceita URI diretamente, precisa dos campos
// discretos). Não cobre o formato ADO.NET (`Server=...;Database=...;`, sem prefixo reconhecível
// que distinga de um erro de digitação qualquer) — só URI simples e JDBC (ver
// parseJDBCSQLServerParams), os dois formatos reais já vistos em uso.
func parseSQLServerConnString(raw string) (host string, port int, username, password, database string) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", 0, "", "", ""
	}
	host = u.Hostname()
	port = sqlserverDefaultPort
	if p := u.Port(); p != "" {
		port, _ = strconv.Atoi(p)
	}
	if u.User != nil {
		username = u.User.Username()
		password, _ = u.User.Password()
	}
	database = strings.TrimPrefix(u.Path, "/")
	return
}

// sqlserverConnStringParams resolve host/port/user/pass/db + dicas de TLS a partir de uma
// connection string, tentando o formato JDBC primeiro (é o único dos dois que consegue expressar
// TLS explicitamente via encrypt=/trustServerCertificate=) e caindo pro formato URI simples se
// não bater (que não tem conceito de TLS embutido — useTLS/skipTLSVerify sempre false nesse caso,
// mesmo comportamento de antes desta função existir).
func sqlserverConnStringParams(raw string) (host string, port int, username, password, database string, useTLS, skipTLSVerify bool) {
	if h, p, u, pw, db, tls, trust, ok := parseJDBCSQLServerParams(raw); ok {
		return h, p, u, pw, db, tls, trust
	}
	h, p, u, pw, db := parseSQLServerConnString(raw)
	return h, p, u, pw, db, false, false
}

// sqlserverEffectiveParams resolve os campos discretos efetivos (+ dicas de TLS, só relevantes em
// Mode=="connstring" — fora desse modo, TLS vem sempre dos toggles discretos p.UseTLS/
// p.SkipTLSVerify, sem mudança de comportamento).
func sqlserverEffectiveParams(p dbConnParams) (host string, port int, username, password, database string, useTLS, skipTLSVerify bool) {
	if p.Mode == "connstring" {
		return sqlserverConnStringParams(p.ConnStr)
	}
	return p.Host, p.Port, p.Username, p.Password, p.Database, p.UseTLS, p.SkipTLSVerify
}

// envVarPrefix monta um prefixo `env VAR='valor' ` pra injetar uma variável de ambiente na frente
// de um comando já embrulhado em `timeout Ns ...` (usado pra passar senha sem expô-la em
// `ps`/histórico de shell — MYSQL_PWD, SQLCMDPASSWORD).
//
// BUG REAL corrigido — `timeout Ns VAR=valor cmd` (sem o `env`) NÃO funciona como atribuição de
// variável de ambiente: a sintaxe `VAR=valor cmd` só é reconhecida pelo shell como atribuição
// temporária quando é a PRIMEIRA palavra de um comando simples. Com `timeout` e a duração já
// ocupando essa posição, `VAR=valor` vira só mais um argumento posicional pro próprio `timeout`,
// que tenta executá-lo como se fosse o nome do programa a rodar — falha com
// `timeout: failed to run command 'VAR=valor': No such file or directory`, confirmado ao vivo
// rodando exatamente esse padrão. `env VAR=valor cmd` resolve isso (`env` lê pares VAR=valor
// como seus próprios argumentos, em qualquer posição antes do comando real). Esse bug já existia
// desde a implementação original do engine MySQL (`MYSQL_PWD=...` sem `env`) — nunca detectado
// porque nenhum teste anterior exercitou autenticação com senha nesse caminho específico
// (conectividade sem senha, ou com credenciais lidas de outro lugar, sempre "funcionava" porque
// o prefixo vazio nunca acionava o bug). Achado ao validar o SQL Server (SQLCMDPASSWORD) contra
// um servidor real — corrigido nos dois engines de uma vez.
func envVarPrefix(name, value string) string {
	if value == "" {
		return ""
	}
	return "env " + name + "=" + quoteShellArg(value) + " "
}

// sqlserverPasswordPrefix monta o prefixo de ambiente pra senha do sqlcmd (SQLCMDPASSWORD).
func sqlserverPasswordPrefix(p dbConnParams) string {
	_, _, _, pass, _, _, _ := sqlserverEffectiveParams(p)
	return envVarPrefix("SQLCMDPASSWORD", pass)
}

// sqlserverConnStringSchemes: mesmo racional de postgresConnStringSchemes/mongoConnStringSchemes
// — evita repassar um esquema errado pro parser e conectar (ou falhar) sem contexto nenhum.
// jdbc:sqlserver:// adicionado depois de um caso real: usuário colou a connection string exata
// que usa no dia a dia (formato do driver JDBC da Microsoft, muito comum em config de app Java) e
// esperava que funcionasse — só o formato URI simples era aceito antes disso.
var sqlserverConnStringSchemes = []string{"sqlserver://", "mssql://", jdbcSQLServerPrefix}

func isValidSQLServerConnString(s string) bool {
	lower := strings.ToLower(strings.TrimSpace(s))
	for _, scheme := range sqlserverConnStringSchemes {
		if strings.HasPrefix(lower, scheme) {
			return true
		}
	}
	return false
}

func sqlserverConnStringHint(raw string) string {
	if scheme := extractURIScheme(raw); scheme != "" {
		return fmt.Sprintf("Essa connection string usa o esquema %q, que não é SQL Server — confira se copiou a string do banco certo. Esperado: sqlserver://, mssql:// ou jdbc:sqlserver://", scheme+"://")
	}
	return "Connection string do SQL Server deve começar com sqlserver://, mssql:// ou jdbc:sqlserver:// (ex: sqlserver://usuario:senha@host:1433/banco, ou jdbc:sqlserver://host:1433;database=banco;user=usuario;password=senha;encrypt=true) — formato ADO.NET (Server=...;) não é suportado aqui."
}

// redisEffectiveTarget devolve (true, uri, nil) quando dá pra usar `redis-cli -u <uri>`
// diretamente (Mode=="connstring", redis-cli 6+ aceita isso nativamente), ou (false, "", args)
// com os flags discretos -h/-p/-a/--tls pros demais modos.
func redisEffectiveTarget(p dbConnParams) (useURI bool, uri string, args []string) {
	if p.Mode == "connstring" {
		return true, p.ConnStr, nil
	}
	args = append(args, "-h", quoteShellArg(p.Host), "-p", strconv.Itoa(p.Port))
	if p.Password != "" {
		args = append(args, "-a", quoteShellArg(p.Password), "--no-auth-warning")
	}
	// --user: só faz sentido com Redis 6+ ACL (usuários nomeados). Redis pré-ACL usa só senha —
	// por isso Username é opcional aqui, diferente dos outros engines onde é o campo principal.
	if p.Username != "" {
		args = append(args, "--user", quoteShellArg(p.Username))
	}
	// Redis não tem nomes de banco — só um índice numérico 0-15 (default 0), selecionado via `-n`.
	if db, convErr := strconv.Atoi(strings.TrimSpace(p.Database)); convErr == nil {
		args = append(args, "-n", strconv.Itoa(db))
	}
	if p.UseTLS {
		args = append(args, "--tls")
		if p.SkipTLSVerify {
			args = append(args, "--insecure")
		}
	}
	return false, "", args
}

// redisConnStringSchemes são os únicos esquemas que `redis-cli -u` aceita — fora deles ele falha
// com "Invalid URI scheme", um erro cru sem nenhuma explicação do porquê. Validado ANTES de
// rodar o teste (validateDBTestRequest / runTest) pra nunca deixar esse erro cru chegar ao
// usuário sem contexto.
var redisConnStringSchemes = []string{"redis://", "rediss://"}

func isValidRedisConnString(s string) bool {
	lower := strings.ToLower(strings.TrimSpace(s))
	for _, scheme := range redisConnStringSchemes {
		if strings.HasPrefix(lower, scheme) {
			return true
		}
	}
	return false
}

// redisConnStringHint monta uma mensagem acionável pra connection string Redis inválida —
// detecta especificamente o caso de o usuário colar o comando `redis-cli -h ... -p ... -a ...
// --tls` (formato que a própria Azure Portal sugere na aba "Console"/documentação como comando
// de teste) no campo de connection string, esperando que funcionasse como se estivesse rodando
// redis-cli manualmente. Sem essa detecção, o único sinal que o usuário via era o "Invalid URI
// scheme" cru devolvido pelo próprio redis-cli.
// validateDBConnStringScheme checa se `raw` usa um esquema de URI reconhecido pelo cliente
// nativo do engine selecionado — chamado nos dois pontos onde uma connection string chega
// (digitada direto no formulário, ou lida de Secret/ConfigMap via ConnStringRef), pra nunca
// deixar um esquema errado (ex: connection string copiada do banco/engine errado) chegar sem
// aviso no psql/mongosh/mysql e produzir um erro cru de driver sem contexto nenhum. Devolve
// (código, mensagem) não-vazios quando inválida; ("", "") quando ok.
func validateDBConnStringScheme(engine, raw string) (code, hint string) {
	switch engine {
	case "redis":
		if !isValidRedisConnString(raw) {
			return "INVALID_REDIS_CONNSTRING", redisConnStringHint(raw)
		}
	case "postgres":
		if !isValidPostgresConnString(raw) {
			return "INVALID_POSTGRES_CONNSTRING", postgresConnStringHint(raw)
		}
	case "mongodb":
		if !isValidMongoConnString(raw) {
			return "INVALID_MONGO_CONNSTRING", mongoConnStringHint(raw)
		}
	case "mysql":
		if !isValidMySQLConnString(raw) {
			return "INVALID_MYSQL_CONNSTRING", mysqlConnStringHint(raw)
		}
	case "sqlserver":
		if !isValidSQLServerConnString(raw) {
			return "INVALID_SQLSERVER_CONNSTRING", sqlserverConnStringHint(raw)
		}
	}
	return "", ""
}

func redisConnStringHint(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if strings.HasPrefix(trimmed, "-") || strings.Contains(trimmed, " -h ") || strings.Contains(trimmed, "--tls") {
		return "Isso parece ser um comando redis-cli (-h/-p/-a/--tls), não uma connection string. " +
			"Use o modo \"Usuário e senha\" preenchendo Host/Porta/Senha e marcando TLS, ou informe uma URI no formato rediss://:senha@host:porta/0"
	}
	return "Connection string do Redis deve começar com redis:// ou rediss:// (ex: rediss://:senha@host:porta/0)"
}

// redisBaseCommand monta "redis-cli <flags de conexão>" sem subcomando — reusado tanto por
// redisCommand (um subcomando) quanto pelo estágio de navegação, que precisa reemitir a mesma
// base de conexão várias vezes (um TYPE por chave, ver dbEngines["redis"].buildBrowse).
func redisBaseCommand(p dbConnParams) string {
	useURI, uri, args := redisEffectiveTarget(p)
	parts := []string{"redis-cli"}
	if useURI {
		parts = append(parts, "-u", quoteShellArg(uri))
	} else {
		parts = append(parts, args...)
	}
	return strings.Join(parts, " ")
}

func redisCommand(p dbConnParams, extraArgs ...string) string {
	parts := []string{redisBaseCommand(p)}
	for _, a := range extraArgs {
		parts = append(parts, quoteShellArg(a))
	}
	return strings.Join(parts, " ")
}

// redisKeyspaceLineRegex casa linhas do "INFO keyspace" no formato
// "dbN:keys=X,expires=Y,avg_ttl=Z[,subexpiry=W]" — o campo subexpiry (Redis 7.4+) é ignorado, não
// muda a extração dos 3 campos usados aqui.
var redisKeyspaceLineRegex = regexp.MustCompile(`(?m)^db(\d+):keys=(\d+),expires=(\d+),avg_ttl=(\d+)`)

// parseRedisKeyspaceInfo extrai um DBBrowseObject por banco lógico não-vazio (0-15) a partir da
// saída de "INFO keyspace" — bancos sem nenhuma chave simplesmente não aparecem na saída do
// Redis, não precisam ser filtrados aqui.
func parseRedisKeyspaceInfo(raw string) []DBBrowseObject {
	matches := redisKeyspaceLineRegex.FindAllStringSubmatch(raw, -1)
	objects := make([]DBBrowseObject, 0, len(matches))
	for _, m := range matches {
		keys, _ := strconv.ParseInt(m[2], 10, 64)
		expires, _ := strconv.ParseInt(m[3], 10, 64)
		avgTTL, _ := strconv.ParseInt(m[4], 10, 64)
		detail := fmt.Sprintf("%d expirando(s)", expires)
		if avgTTL > 0 {
			detail += fmt.Sprintf(", avg TTL %dms", avgTTL)
		}
		objects = append(objects, DBBrowseObject{Name: "db" + m[1], Detail: detail, Count: keys})
	}
	return objects
}

// parseLineListOutput é o parser genérico "uma linha = um objeto" usado por Postgres (via `psql
// -t -A`), MySQL (`SHOW DATABASES` menos o cabeçalho) e Redis (`--scan`).
func parseLineListOutput(raw string, skipFirstLine bool) []string {
	lines := strings.Split(strings.TrimSpace(raw), "\n")
	if skipFirstLine && len(lines) > 0 {
		lines = lines[1:]
	}
	var out []string
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		out = append(out, l)
	}
	return out
}

// parseJSONLinesPreview parseia uma saída onde cada linha é um objeto JSON independente (formato
// do `row_to_json` do Postgres no Preview: uma linha de saída por linha de tabela). Linhas
// malformadas são ignoradas (best-effort — uma linha ruim não deve derrubar a amostra inteira).
func parseJSONLinesPreview(raw string) ([]map[string]any, error) {
	var rows []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var row map[string]any
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			continue
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// parseTSVWithHeaderPreview parseia a saída `--batch` do cliente mysql/mariadb: primeira linha =
// nomes das colunas (separados por tab), linhas seguintes = valores — dá pra montar os mapas
// coluna→valor sem precisar saber o schema de antemão (equivalente ao row_to_json do Postgres,
// só que via formato tabular em vez de JSON nativo).
func parseTSVWithHeaderPreview(raw string) ([]map[string]any, error) {
	lines := strings.Split(strings.TrimSpace(raw), "\n")
	if len(lines) == 0 {
		return nil, nil
	}
	headers := strings.Split(lines[0], "\t")
	var rows []map[string]any
	for _, line := range lines[1:] {
		if strings.TrimSpace(line) == "" {
			continue
		}
		cols := strings.Split(line, "\t")
		row := make(map[string]any, len(headers))
		for i, h := range headers {
			if i < len(cols) {
				row[h] = cols[i]
			} else {
				row[h] = ""
			}
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// redisKeyTypeMarkerRegex casa a linha `__DBTEST_KEYTYPE__:<tipo>` emitida pelo buildPreview do
// Redis — é um detalhe de implementação (não faz parte da resposta real do redis-cli), removido
// do RawOutput antes de mostrar pro usuário.
var redisKeyTypeMarkerRegex = regexp.MustCompile(`^__DBTEST_KEYTYPE__:(\w+)\n?`)

// parseRedisPreviewMeta separa a marca de TYPE (ver redisKeyTypeMarkerRegex) do resto da saída e
// decide se HasMore se aplica: só list/zset têm paginação real por índice
// (LRANGE/ZRANGE — ver buildPreview do Redis); os demais tipos (string/hash/set/stream) sempre
// trazem tudo de uma vez, então HasMore fica sempre false pra eles, mesmo que a saída seja grande.
// Chamado incondicionalmente pra qualquer engine no handler Preview — pra Postgres/MySQL/Mongo, a
// regex simplesmente não casa (não emitem essa marca), devolvendo a saída inalterada.
func parseRedisPreviewMeta(raw string, limit int) (cleaned string, hasMore bool) {
	m := redisKeyTypeMarkerRegex.FindStringSubmatch(raw)
	if m == nil {
		return raw, false
	}
	keyType := m[1]
	cleaned = raw[len(m[0]):]

	lines := 0
	for _, l := range strings.Split(strings.TrimRight(cleaned, "\n"), "\n") {
		if strings.TrimSpace(l) != "" {
			lines++
		}
	}
	switch keyType {
	case "list":
		hasMore = lines >= limit
	case "zset":
		// ZRANGE ... WITHSCORES intercala membro/score — 2 linhas por item.
		hasMore = lines >= limit*2
	}
	return cleaned, hasMore
}

// extractJSONArray isola o primeiro array JSON `[...]` numa saída que pode ter texto/banner do
// cliente antes ou depois (ex: avisos de versão do mongosh) — parser tolerante, não exige que a
// saída inteira seja só o JSON.
func extractJSONArray(raw string) string {
	start := strings.Index(raw, "[")
	end := strings.LastIndex(raw, "]")
	if start == -1 || end == -1 || end < start {
		return ""
	}
	return raw[start : end+1]
}

var dbEngines = map[string]dbEngine{
	"postgres": {
		label: "PostgreSQL",
		// postgres:16-alpine — imagem oficial, garante psql compatível com o servidor.
		image: "postgres:16-alpine",
		buildConnectivity: func(p dbConnParams, timeoutSec int) string {
			target := connTargetOrURI(p, buildPostgresURI)
			cmd := fmt.Sprintf("psql %s -c %s", quoteShellArg(target), quoteShellArg("SELECT 1;"))
			return fmt.Sprintf("timeout %ds %s 2>&1", timeoutSec, cmd)
		},
		// Sem Database informado: lista databases. Com Database informado: desce um nível e lista
		// as tabelas do schema public + suas colunas, junto com estatísticas de catálogo por
		// tabela (nunca um scan/COUNT(*) real): pg_relation_size (heap, "Size"),
		// pg_total_relation_size (heap+índices+toast, "Storage Size" — equivalente ao Compass) e
		// reltuples (estimativa de linhas, atualizada por VACUUM/ANALYZE) — repetidos em toda linha
		// da mesma tabela, agrupados no Go (ver groupColumnsToTablesWithStats).
		buildBrowse: func(p dbConnParams, timeoutSec int) string {
			target := connTargetOrURI(p, buildPostgresURI)
			query := "SELECT datname FROM pg_database WHERE NOT datistemplate ORDER BY datname;"
			if effectiveDatabase(p) != "" {
				query = "SELECT col.table_name || '|' || col.column_name || '|' || col.data_type || '|' || " +
					"pg_relation_size(c.oid) || '|' || pg_total_relation_size(c.oid) || '|' || c.reltuples::bigint " +
					"FROM information_schema.columns col " +
					"JOIN pg_class c ON c.relname = col.table_name AND c.relkind = 'r' " +
					"JOIN pg_namespace n ON n.oid = c.relnamespace AND n.nspname = col.table_schema " +
					"WHERE col.table_schema = 'public' ORDER BY col.table_name, col.ordinal_position;"
			}
			cmd := fmt.Sprintf("psql %s -t -A -c %s", quoteShellArg(target), quoteShellArg(query))
			return fmt.Sprintf("timeout %ds %s 2>&1", timeoutSec, cmd)
		},
		parseBrowseOutput: func(raw string, p dbConnParams) ([]DBBrowseObject, string, bool) {
			lines := parseLineListOutput(raw, false)
			if effectiveDatabase(p) == "" {
				return namesToObjects(lines), "database", false
			}
			return groupColumnsToTablesWithStats(lines), "table", false
		},
		// row_to_json embute cada linha como um objeto JSON (uma linha de saída por linha da
		// tabela) — auto-descritivo, não precisa saber os nomes das colunas de antemão (diferente
		// do MySQL, que usa o cabeçalho do --batch pro mesmo efeito).
		buildPreview: func(p dbConnParams, pv dbPreviewParams, timeoutSec int) string {
			target := connTargetOrURI(p, buildPostgresURI)
			orderBy := ""
			if pv.SortColumn != "" {
				orderBy = fmt.Sprintf(" ORDER BY %s %s", quotePostgresIdentifier(pv.SortColumn), strings.ToUpper(pv.SortDir))
			}
			query := fmt.Sprintf("SELECT row_to_json(t) FROM (SELECT * FROM %s%s LIMIT %d OFFSET %d) t;",
				quotePostgresIdentifier(pv.Object), orderBy, pv.Limit, pv.Offset)
			cmd := fmt.Sprintf("psql %s -t -A -c %s", quoteShellArg(target), quoteShellArg(query))
			return fmt.Sprintf("timeout %ds %s 2>&1", timeoutSec, cmd)
		},
		parsePreviewOutput:   parseJSONLinesPreview,
		resolveDatabaseLabel: effectiveDatabase,
		networkErrorRegex:    regexp.MustCompile(`(?i)(could not connect to server|connection refused|could not translate host name|timeout expired)`),
		authErrorRegex:       regexp.MustCompile(`(?i)(password authentication failed|role .* does not exist|no pg_hba\.conf entry)`),
		// "no pg_hba.conf entry ... no encryption" é o Postgres recusando a conexão porque ela
		// chegou sem SSL e o servidor só tem regras `hostssl` (ex: Azure Database for PostgreSQL,
		// que exige TLS por padrão) — não é senha/usuário errado, é o toggle TLS desligado no teste.
		// Checado antes de authErrorRegex (ver ordem do switch em runDBConnectivityStage).
		tlsErrorRegex: regexp.MustCompile(`(?i)(ssl error|server does not support ssl|certificate verify failed|no pg_hba\.conf entry.*no encryption)`),
	},
	"mysql": {
		label: "MySQL/MariaDB",
		// mariadb:11 — cliente compatível com servidores MySQL e MariaDB.
		//
		// BUG REAL corrigido — binário chamado como `mysql`, que não existe mais nessa imagem: o
		// pacote `mariadb-client` (MariaDB 10.6+, incluindo o 11.x usado aqui) renomeou o binário
		// pra `mariadb` e NÃO mantém mais o symlink de compatibilidade `mysql` (confirmado ao vivo
		// — `find / -iname mysql` na imagem `mariadb:11` não acha nenhum executável, só diretórios
		// de dados/config com esse nome). Toda invocação (`docker run ... mysql ...`, tanto no
		// modo local quanto no modo pod) falhava com "executable file not found" — o engine
		// MySQL/MariaDB estava quebrado por completo, não só num caso de borda. Trocado `mysql`
		// por `mariadb` (mesmo binário, mesma compatibilidade de protocolo com servidores MySQL de
		// verdade — é justamente o propósito do fork) nos 3 pontos que montam o comando
		// (buildConnectivity/buildBrowse/buildPreview).
		image: "mariadb:11",
		buildConnectivity: func(p dbConnParams, timeoutSec int) string {
			host, port, user, _, db := mysqlEffectiveParams(p)
			args := []string{"mariadb", "-h", quoteShellArg(host), "-P", strconv.Itoa(port)}
			if user != "" {
				args = append(args, "-u", quoteShellArg(user))
			}
			if p.UseTLS {
				if p.SkipTLSVerify {
					args = append(args, "--ssl-mode=REQUIRED")
				} else {
					args = append(args, "--ssl-mode=VERIFY_IDENTITY")
				}
			}
			if db != "" {
				args = append(args, quoteShellArg(db))
			}
			args = append(args, "-e", quoteShellArg("SELECT 1;"))
			prefix := mysqlPasswordPrefix(p)
			return fmt.Sprintf("timeout %ds %s%s 2>&1", timeoutSec, prefix, strings.Join(args, " "))
		},
		// Sem Database informado: lista databases. Com Database informado: desce um nível e lista
		// as tabelas + colunas (mesmo formato "tabela|coluna|tipo|..." do Postgres) — table_schema
		// resolvido via DATABASE() (a sessão já conecta nesse banco, sem precisar reinterpolar o
		// nome na query). Estatísticas por tabela via information_schema.tables (catálogo, nunca
		// um scan): data_length ("Size"), data_length+index_length ("Storage Size" — equivalente
		// ao Compass) e table_rows (estimativa de linhas, atualizada por ANALYZE TABLE).
		buildBrowse: func(p dbConnParams, timeoutSec int) string {
			host, port, user, _, db := mysqlEffectiveParams(p)
			args := []string{"mariadb", "-h", quoteShellArg(host), "-P", strconv.Itoa(port)}
			if user != "" {
				args = append(args, "-u", quoteShellArg(user))
			}
			if p.UseTLS {
				if p.SkipTLSVerify {
					args = append(args, "--ssl-mode=REQUIRED")
				} else {
					args = append(args, "--ssl-mode=VERIFY_IDENTITY")
				}
			}
			query := "SHOW DATABASES;"
			if db != "" {
				args = append(args, quoteShellArg(db))
				query = "SELECT CONCAT(col.table_name,'|',col.column_name,'|',col.column_type,'|'," +
					"IFNULL(t.data_length,0),'|',IFNULL(t.data_length,0)+IFNULL(t.index_length,0),'|',IFNULL(t.table_rows,0)) " +
					"FROM information_schema.columns col " +
					"JOIN information_schema.tables t ON t.table_name = col.table_name AND t.table_schema = col.table_schema " +
					"WHERE col.table_schema = DATABASE() ORDER BY col.table_name, col.ordinal_position;"
			}
			// -N (--skip-column-names) evita ter que descartar a linha de cabeçalho no Go.
			args = append(args, "-N", "-e", quoteShellArg(query))
			prefix := mysqlPasswordPrefix(p)
			return fmt.Sprintf("timeout %ds %s%s 2>&1", timeoutSec, prefix, strings.Join(args, " "))
		},
		parseBrowseOutput: func(raw string, p dbConnParams) ([]DBBrowseObject, string, bool) {
			lines := parseLineListOutput(raw, false)
			_, _, _, _, db := mysqlEffectiveParams(p)
			if db == "" {
				return namesToObjects(lines), "database", false
			}
			return groupColumnsToTablesWithStats(lines), "table", false
		},
		// --batch (implícito quando stdout não é um TTY, explícito aqui por clareza) gera saída
		// separada por tab COM linha de cabeçalho (sem o -N usado em buildBrowse) — dá pra montar
		// os mapas coluna→valor sem precisar saber o schema de antemão.
		buildPreview: func(p dbConnParams, pv dbPreviewParams, timeoutSec int) string {
			host, port, user, _, db := mysqlEffectiveParams(p)
			args := []string{"mariadb", "-h", quoteShellArg(host), "-P", strconv.Itoa(port)}
			if user != "" {
				args = append(args, "-u", quoteShellArg(user))
			}
			if p.UseTLS {
				if p.SkipTLSVerify {
					args = append(args, "--ssl-mode=REQUIRED")
				} else {
					args = append(args, "--ssl-mode=VERIFY_IDENTITY")
				}
			}
			if db != "" {
				args = append(args, quoteShellArg(db))
			}
			orderBy := ""
			if pv.SortColumn != "" {
				orderBy = fmt.Sprintf(" ORDER BY %s %s", quoteMySQLIdentifier(pv.SortColumn), strings.ToUpper(pv.SortDir))
			}
			query := fmt.Sprintf("SELECT * FROM %s%s LIMIT %d OFFSET %d;", quoteMySQLIdentifier(pv.Object), orderBy, pv.Limit, pv.Offset)
			args = append(args, "--batch", "-e", quoteShellArg(query))
			prefix := mysqlPasswordPrefix(p)
			return fmt.Sprintf("timeout %ds %s%s 2>&1", timeoutSec, prefix, strings.Join(args, " "))
		},
		parsePreviewOutput: parseTSVWithHeaderPreview,
		resolveDatabaseLabel: func(p dbConnParams) string {
			_, _, _, _, db := mysqlEffectiveParams(p)
			return db
		},
		networkErrorRegex: regexp.MustCompile(`(?i)(can't connect to mysql server|connection refused|unknown mysql server host)`),
		authErrorRegex:    regexp.MustCompile(`(?i)access denied for user`),
		tlsErrorRegex:     regexp.MustCompile(`(?i)(ssl connection error|tls.*handshake|certificate verify failed)`),
	},
	"mongodb": {
		label: "MongoDB",
		// mongo:7 — imagem oficial; validar em produção se `mongosh` vem embutido nessa tag
		// específica (Mongo migrou do shell legado `mongo` pro `mongosh` distribuído separado em
		// algumas versões). Se não vier, trocar para uma imagem que garanta o binário.
		image: "mongo:7",
		buildConnectivity: func(p dbConnParams, timeoutSec int) string {
			uri := connTargetOrURI(p, buildMongoURI)
			cmd := fmt.Sprintf("mongosh %s --quiet --eval %s", quoteShellArg(uri), quoteShellArg("db.runCommand({ping:1})"))
			return fmt.Sprintf("timeout %ds %s 2>&1", timeoutSec, cmd)
		},
		// Sem Database informado: lista databases (nome + tamanho em disco). Com Database
		// informado: desce um nível e lista as collections desse banco + estatísticas via
		// $collStats (count/size/storageSize — mesmo dado do "All Stats" do MongoDB Compass),
		// que lê metadata do storage engine sem escanear a collection inteira, seguro mesmo em
		// collections grandes. try/catch por collection: views e alguns tipos especiais não
		// suportam $collStats — cai para estimatedDocumentCount (só count, sem size) em vez de
		// derrubar a listagem inteira por causa de uma collection.
		buildBrowse: func(p dbConnParams, timeoutSec int) string {
			uri := connTargetOrURI(p, buildMongoURI)
			var script string
			if db := effectiveDatabase(p); db != "" {
				dbLit := jsStringLiteral(db)
				script = fmt.Sprintf(
					"JSON.stringify(db.getSiblingDB(%s).getCollectionNames().map(function(c){"+
						"try{"+
						"var s=db.getSiblingDB(%s).getCollection(c).aggregate([{$collStats:{storageStats:{}}}]).toArray()[0];"+
						"var ss=(s&&s.storageStats)||{};"+
						"return {name:c, count:ss.count||0, size:ss.size||0, storageSize:ss.storageSize||0};"+
						"}catch(e){"+
						"return {name:c, count:db.getSiblingDB(%s).getCollection(c).estimatedDocumentCount(), size:0, storageSize:0};"+
						"}"+
						"}))",
					dbLit, dbLit, dbLit,
				)
			} else {
				script = "JSON.stringify(db.adminCommand({listDatabases:1}).databases.map(function(d){return {name:d.name, sizeOnDisk:d.sizeOnDisk};}))"
			}
			cmd := fmt.Sprintf("mongosh %s --quiet --eval %s", quoteShellArg(uri), quoteShellArg(script))
			return fmt.Sprintf("timeout %ds %s 2>&1", timeoutSec, cmd)
		},
		parseBrowseOutput: func(raw string, p dbConnParams) ([]DBBrowseObject, string, bool) {
			jsonArr := extractJSONArray(raw)
			if jsonArr == "" {
				return nil, "", false
			}
			if effectiveDatabase(p) != "" {
				var collections []struct {
					Name        string `json:"name"`
					Count       int64  `json:"count"`
					Size        int64  `json:"size"`
					StorageSize int64  `json:"storageSize"`
				}
				if err := json.Unmarshal([]byte(jsonArr), &collections); err != nil {
					return nil, "", false
				}
				objects := make([]DBBrowseObject, 0, len(collections))
				for _, c := range collections {
					detail := fmt.Sprintf("~%d documento(s)", c.Count)
					if c.StorageSize > 0 {
						detail += fmt.Sprintf(", %s (dados) / %s (storage)", formatBytesShort(c.Size), formatBytesShort(c.StorageSize))
					}
					objects = append(objects, DBBrowseObject{
						Name: c.Name, Type: "collection", Detail: detail,
						Count: c.Count, SizeBytes: c.Size, StorageSizeBytes: c.StorageSize,
					})
				}
				return objects, "collection", false
			}
			var dbs []struct {
				Name       string `json:"name"`
				SizeOnDisk int64  `json:"sizeOnDisk"`
			}
			if err := json.Unmarshal([]byte(jsonArr), &dbs); err != nil {
				return nil, "", false
			}
			objects := make([]DBBrowseObject, 0, len(dbs))
			for _, d := range dbs {
				objects = append(objects, DBBrowseObject{Name: d.Name, Type: "database", Detail: formatBytesShort(d.SizeOnDisk)})
			}
			return objects, "database", false
		},
		// EJSON.stringify (não JSON.stringify) — mongosh expõe EJSON globalmente pra serializar
		// tipos BSON especiais (ObjectId, Date, Long, Binary) de forma legível
		// ({"$oid":"..."}, etc.), em vez do JSON.stringify simples quebrar silenciosamente esses
		// campos (mesmo bug já visto no sizeOnDisk do nível "database" — aqui evitado de propósito).
		buildPreview: func(p dbConnParams, pv dbPreviewParams, timeoutSec int) string {
			uri := connTargetOrURI(p, buildMongoURI)
			// sort via Object.fromEntries (não objeto literal {campo: 1}) — nomes de campo podem
			// ter caracteres inválidos como chave de identificador JS solto (ex: "-" seria lido
			// como subtração); construir a chave dinamicamente evita esse problema por completo,
			// sem precisar validar se o nome "parece" um identificador JS válido.
			sortExpr := ""
			if pv.SortColumn != "" {
				dir := 1
				if pv.SortDir == "desc" {
					dir = -1
				}
				sortExpr = fmt.Sprintf(".sort(Object.fromEntries([[%s,%d]]))", jsStringLiteral(pv.SortColumn), dir)
			}
			script := fmt.Sprintf(
				"EJSON.stringify(db.getSiblingDB(%s).getCollection(%s).find()%s.skip(%d).limit(%d).toArray())",
				jsStringLiteral(effectiveDatabase(p)), jsStringLiteral(pv.Object), sortExpr, pv.Offset, pv.Limit,
			)
			cmd := fmt.Sprintf("mongosh %s --quiet --eval %s", quoteShellArg(uri), quoteShellArg(script))
			return fmt.Sprintf("timeout %ds %s 2>&1", timeoutSec, cmd)
		},
		parsePreviewOutput: func(raw string) ([]map[string]any, error) {
			arr := extractJSONArray(raw)
			if arr == "" {
				return nil, fmt.Errorf("saída inesperada do mongosh — ver saída bruta")
			}
			var rows []map[string]any
			if err := json.Unmarshal([]byte(arr), &rows); err != nil {
				return nil, err
			}
			return rows, nil
		},
		resolveDatabaseLabel: effectiveDatabase,
		networkErrorRegex:    regexp.MustCompile(`(?i)(econnrefused|serverselectionerror|getaddrinfo enotfound|connection timed out)`),
		authErrorRegex:       regexp.MustCompile(`(?i)(authentication failed|auth error|not authorized)`),
		tlsErrorRegex:        regexp.MustCompile(`(?i)(tls|ssl).*(error|handshake|certificate)`),
	},
	"redis": {
		label: "Redis",
		// redis:7-alpine — inclui redis-cli, imagem leve (~30MB).
		image: "redis:7-alpine",
		buildConnectivity: func(p dbConnParams, timeoutSec int) string {
			cmd := redisCommand(p, "PING")
			return fmt.Sprintf("timeout %ds %s 2>&1", timeoutSec, cmd)
		},
		// Sem Database (índice) informado: visão geral por banco lógico + estatísticas do servidor
		// via `INFO` sem seção específica — preenche o "nível database" que os outros 3 engines já
		// tinham (Postgres/MySQL/Mongo listam databases quando nenhum nome é informado; o Redis
		// antes pulava direto pra lista de chaves, sempre). `INFO` sem argumento devolve as seções
		// default (Server/Clients/Memory/Stats/Replication/Keyspace, entre outras) num único
		// comando, server-wide — tanto os 16 bancos (0-15, parseRedisKeyspaceInfo) quanto os campos
		// usados por parseRedisServerInfo (versão, memória, clientes, hit rate, latência real) vêm
		// da mesma chamada, sem round-trip extra. `all` (em vez de sem argumento) inclui também as
		// seções Commandstats/Latencystats — ausentes do INFO default —, necessárias pra calcular
		// a latência média real por comando (parseRedisCommandStats). Pedir todas as seções juntas
		// não custa mais que pedir uma só.
		//
		// Com Database informado: mesmo scan de chaves de antes (`--scan` + `head`, nunca `KEYS
		// *`), mas cada chave agora roda TYPE + MEMORY USAGE num único `redis-cli` (comandos
		// enviados via stdin, uma conexão só) em vez de dois processos separados — mesmo número
		// de round-trips de antes (100 no teto), só que cada um faz 2 comandos ao invés de 1.
		// MEMORY USAGE é O(1)-ish (não escaneia a chave inteira pra tipos simples), mesmo espírito
		// de segurança do teto de 100 chaves.
		buildBrowse: func(p dbConnParams, timeoutSec int) string {
			if strings.TrimSpace(p.Database) == "" {
				cmd := redisCommand(p, "INFO", "all")
				return fmt.Sprintf("timeout %ds %s 2>&1", timeoutSec, cmd)
			}
			scanArgs := []string{"--scan"}
			if pattern := strings.TrimSpace(p.RedisKeyPattern); pattern != "" {
				scanArgs = append(scanArgs, "--pattern", pattern)
			}
			scanCmd := redisCommand(p, scanArgs...)
			baseCmd := redisBaseCommand(p)
			pipeline := fmt.Sprintf(
				`%s | head -n %d | while IFS= read -r k; do `+
					`r=$(printf 'TYPE %%s\nMEMORY USAGE %%s\n' "$k" "$k" | %s 2>/dev/null); `+
					`t=$(printf '%%s\n' "$r" | head -n1); m=$(printf '%%s\n' "$r" | tail -n1); `+
					`printf '%%s|%%s|%%s\n' "$k" "$t" "$m"; done`,
				scanCmd, dbRedisScanCap, baseCmd,
			)
			return fmt.Sprintf("timeout %ds sh -c %s 2>&1", timeoutSec, quoteShellArg(pipeline))
		},
		parseBrowseOutput: func(raw string, p dbConnParams) ([]DBBrowseObject, string, bool) {
			if strings.TrimSpace(p.Database) == "" {
				return parseRedisKeyspaceInfo(raw), "database", false
			}
			lines := parseLineListOutput(raw, false)
			objects := make([]DBBrowseObject, 0, len(lines))
			for _, l := range lines {
				parts := strings.SplitN(l, "|", 3)
				name := parts[0]
				typ := "unknown"
				if len(parts) >= 2 && strings.TrimSpace(parts[1]) != "" {
					typ = strings.TrimSpace(parts[1])
				}
				var memBytes int64
				if len(parts) == 3 {
					memBytes, _ = strconv.ParseInt(strings.TrimSpace(parts[2]), 10, 64)
				}
				objects = append(objects, DBBrowseObject{Name: name, Type: typ, SizeBytes: memBytes})
			}
			truncated := len(objects) >= dbRedisScanCap
			return objects, "key", truncated
		},
		// O comando certo pra ler o VALOR de uma chave depende do TYPE, só conhecido em runtime —
		// GET (string), HGETALL (hash), LRANGE (list), SRANDMEMBER (set, evita SMEMBERS sem teto
		// numa chave gigante), ZRANGE ... WITHSCORES (zset), XRANGE (stream). Sem
		// parsePreviewOutput: os 5 formatos de saída são incompatíveis demais entre si pra
		// estruturar num único formato de linha/documento — o handler devolve só RawOutput, o
		// frontend mostra como texto puro (ainda assim é o valor real, só sem tabela).
		// list/zset têm índice explícito — Offset pagina de verdade (LRANGE/ZRANGE offset
		// offset+limit-1). string/hash/set/stream não têm noção de "página" (GET/HGETALL sempre
		// trazem tudo de uma vez; SRANDMEMBER é amostra aleatória, não paginável de forma estável)
		// — Offset é ignorado nesses casos, mesmo limitação já documentada antes.
		buildPreview: func(p dbConnParams, pv dbPreviewParams, timeoutSec int) string {
			base := redisBaseCommand(p)
			key := quoteShellArg(pv.Object)
			rangeEnd := pv.Offset + pv.Limit - 1
			// __DBTEST_KEYTYPE__ é uma marca interna (removida do RawOutput antes de mostrar pro
			// usuário, ver parseRedisPreviewMeta) — o handler Go não sabe em runtime qual case do
			// shell rodou, e precisa saber o TYPE pra decidir se HasMore se aplica (só list/zset
			// têm paginação real por índice).
			script := fmt.Sprintf(
				`t=$(%s TYPE %s); printf '__DBTEST_KEYTYPE__:%%s\n' "$t"; case "$t" in `+
					`string) %s GET %s ;; `+
					`hash) %s HGETALL %s ;; `+
					`list) %s LRANGE %s %d %d ;; `+
					`set) %s SRANDMEMBER %s %d ;; `+
					`zset) %s ZRANGE %s %d %d WITHSCORES ;; `+
					`stream) %s XRANGE %s - + COUNT %d ;; `+
					`*) echo "tipo não suportado ou chave inexistente: $t" ;; esac`,
				base, key,
				base, key,
				base, key,
				base, key, pv.Offset, rangeEnd,
				base, key, pv.Limit,
				base, key, pv.Offset, rangeEnd,
				base, key, pv.Limit,
			)
			return fmt.Sprintf("timeout %ds sh -c %s 2>&1", timeoutSec, quoteShellArg(script))
		},
		resolveDatabaseLabel: func(p dbConnParams) string {
			db := strings.TrimSpace(p.Database)
			if db == "" {
				return "0"
			}
			return db
		},
		parseServerInfo:   parseRedisServerInfo,
		parseInfoSections: parseRedisInfoSections,
		networkErrorRegex: regexp.MustCompile(`(?i)(could not connect|connection refused|name or service not known)`),
		authErrorRegex:    regexp.MustCompile(`(?i)(noauth|wrongpass|invalid username-password)`),
		tlsErrorRegex:     regexp.MustCompile(`(?i)(tls|ssl).*(error|handshake)`),
	},
	"sqlserver": {
		label: "SQL Server (Azure SQL)",
		// mcr.microsoft.com/mssql-tools18 — usada na primeira versão desta feature — NÃO EXISTE
		// como imagem standalone (bug real: `docker run` falhava com "not found" no modo local, e
		// o mesmo aconteceria no modo pod). `mssql-tools18` é só um PACOTE (`/opt/mssql-tools18/
		// bin/sqlcmd`, baseado no driver ODBC 18) embutido dentro da imagem completa do motor
		// (`mcr.microsoft.com/mssql/server`, GBs, roda o SQL Server inteiro — não faz sentido só
		// pra rodar um cliente) — não existe como imagem "só as ferramentas" nesse pacote mais
		// novo. `mcr.microsoft.com/mssql-tools` (sem "18", confirmado pull público real) é a
		// distribuição standalone oficial ainda publicada — sqlcmd (driver ODBC 17) já no PATH,
		// mesma imagem usada por anos em CI/scripts pra conectar em Azure SQL antes do ODBC 18
		// existir; sem exigência de flag de encriptação explícita pra funcionar contra Azure SQL
		// (a própria Azure já negocia TLS no handshake TDS independente do client). `-C` (trust
		// server certificate) continua condicionado a SkipTLSVerify, pro caso de a imagem não
		// confiar na cadeia de CA do servidor (comum em containers mínimos).
		image: "mcr.microsoft.com/mssql-tools:latest",
		buildConnectivity: func(p dbConnParams, timeoutSec int) string {
			host, port, user, _, db, useTLS, skipTLSVerify := sqlserverEffectiveParams(p)
			args := []string{sqlserverCmdPath, "-S", quoteShellArg(fmt.Sprintf("%s,%d", host, port))}
			if user != "" {
				args = append(args, "-U", quoteShellArg(user))
			}
			if db != "" {
				args = append(args, "-d", quoteShellArg(db))
			}
			if useTLS {
				args = append(args, "-N")
			}
			if skipTLSVerify {
				args = append(args, "-C")
			}
			args = append(args, "-Q", quoteShellArg("SET NOCOUNT ON; SELECT 1;"))
			return fmt.Sprintf("timeout %ds %s%s 2>&1", timeoutSec, sqlserverPasswordPrefix(p), strings.Join(args, " "))
		},
		// Sem Database informado: lista databases (exclui as 4 de sistema: master/tempdb/model/
		// msdb, database_id 1-4 — mesmo espírito do filtro NOT datistemplate do Postgres). Com
		// Database informado: desce um nível e lista tabelas do schema dbo + colunas, no mesmo
		// formato "tabela|coluna|tipo|size|storage_size|rowcount" do Postgres/MySQL (reaproveita
		// groupColumnsToTablesWithStats sem precisar de parser novo). Estatísticas via
		// sys.partitions + sys.allocation_units (VIEWS DE CATÁLOGO, não uma DMV — ver "Bug real
		// corrigido" abaixo) — used_pages("Size"), total_pages ("Storage Size", heap+índices) e
		// rows (contagem), ambos em páginas de 8KB. `-h -1` suprime cabeçalho; `SET NOCOUNT ON;`
		// suprime a mensagem "(N rows affected)" que o sqlcmd imprime no stdout mesmo em modo
		// não-interativo (sem isso, essa linha extra contaminaria a lista de databases — o parser
		// de tabela já tolera linhas malformadas, mas a lista simples de nomes não). `-h -1` e
		// `-y 0` (usado no Preview) são MUTUAMENTE EXCLUSIVOS no sqlcmd (`Sqlcmd: The -h and the
		// -y 0 options are mutually exclusive.`, confirmado rodando de verdade) — Browse usa só
		// `-h -1`; o risco residual de truncar uma linha >256 chars (o motivo de existir `-y 0`) é
		// baixo aqui, já que cada linha é só tabela+coluna+tipo+3 números, não um valor de dado
		// arbitrário como no Preview.
		//
		// BUG REAL corrigido — a versão original usava `sys.dm_db_partition_stats` (uma Dynamic
		// Management View), que exige a permissão `VIEW DATABASE STATE`/`VIEW DATABASE
		// PERFORMANCE STATE` (o nome mudou entre versões do SQL Server, mesma categoria de
		// permissão) — NÃO concedida por padrão nem pela role `db_datareader`, a mais comum pra
		// service accounts de leitura. Relatado ao vivo contra um Azure SQL real: `Msg 262 ...
		// VIEW DATABASE PERFORMANCE STATE permission denied`. `sys.partitions`/
		// `sys.allocation_units` são VIEWS DE CATÁLOGO (não DMVs) — visibilidade segue o modelo de
		// permissão em nível de objeto (se o login pode ler a tabela, vê os metadados dela nessas
		// views), sem exigir nenhuma permissão especial de "estado do servidor/banco". Reproduzido
		// e confirmado ao vivo: criei um login só com `db_datareader` (sem VIEW DATABASE STATE) —
		// a query antiga falhou com o EXATO erro relatado; a nova, com as views de catálogo, retornou
		// os dados corretamente com esse mesmo login restrito. `au.type IN (1,3)` filtra
		// IN_ROW_DATA/ROW_OVERFLOW_DATA (dados "normais"), evitando somar allocation units de
		// LOB_DATA (type=2, semântica de container_id diferente) por engano.
		buildBrowse: func(p dbConnParams, timeoutSec int) string {
			host, port, user, _, db, useTLS, skipTLSVerify := sqlserverEffectiveParams(p)
			args := []string{sqlserverCmdPath, "-S", quoteShellArg(fmt.Sprintf("%s,%d", host, port))}
			if user != "" {
				args = append(args, "-U", quoteShellArg(user))
			}
			if useTLS {
				args = append(args, "-N")
			}
			if skipTLSVerify {
				args = append(args, "-C")
			}
			query := "SELECT name FROM sys.databases WHERE database_id > 4 ORDER BY name;"
			if db != "" {
				args = append(args, "-d", quoteShellArg(db))
				query = "SELECT c.TABLE_NAME + '|' + c.COLUMN_NAME + '|' + c.DATA_TYPE + '|' + " +
					"CAST(ISNULL(SUM(DISTINCT au.used_pages) * 8 * 1024, 0) AS VARCHAR(20)) + '|' + " +
					"CAST(ISNULL(SUM(DISTINCT au.total_pages) * 8 * 1024, 0) AS VARCHAR(20)) + '|' + " +
					"CAST(ISNULL(MAX(p2.rows), 0) AS VARCHAR(20)) " +
					"FROM INFORMATION_SCHEMA.COLUMNS c " +
					"JOIN sys.tables t ON t.name = c.TABLE_NAME AND SCHEMA_NAME(t.schema_id) = c.TABLE_SCHEMA " +
					"LEFT JOIN sys.partitions p2 ON p2.object_id = t.object_id AND p2.index_id IN (0,1) " +
					"LEFT JOIN sys.allocation_units au ON au.container_id = p2.partition_id AND au.type IN (1,3) " +
					"WHERE t.is_ms_shipped = 0 AND c.TABLE_SCHEMA = 'dbo' " +
					"GROUP BY c.TABLE_NAME, c.COLUMN_NAME, c.DATA_TYPE, c.ORDINAL_POSITION " +
					"ORDER BY c.TABLE_NAME, c.ORDINAL_POSITION;"
			}
			args = append(args, "-h", "-1", "-Q", quoteShellArg("SET NOCOUNT ON; "+query))
			return fmt.Sprintf("timeout %ds %s%s 2>&1", timeoutSec, sqlserverPasswordPrefix(p), strings.Join(args, " "))
		},
		parseBrowseOutput: func(raw string, p dbConnParams) ([]DBBrowseObject, string, bool) {
			lines := parseLineListOutput(raw, false)
			_, _, _, _, db, _, _ := sqlserverEffectiveParams(p)
			if db == "" {
				return namesToObjects(lines), "database", false
			}
			return groupColumnsToTablesWithStats(lines), "table", false
		},
		// FOR JSON PATH, WITHOUT_ARRAY_WRAPPER numa subquery correlacionada (`(SELECT t.* FOR
		// JSON ...) FROM (<query paginada>) t`) — o jeito documentado de obter "uma linha de
		// output = um objeto JSON" no SQL Server, equivalente ao row_to_json do Postgres. `-y 0`
		// desliga o truncamento de colunas de texto (default 256 chars) do sqlcmd — sem isso, um
		// JSON longo sai quebrado em múltiplas linhas e corrompe o parse (cada linha vira um
		// `json.Unmarshal` independente em parseJSONLinesPreview) — CONFIRMADO ao vivo contra um
		// SQL Server real: um valor de 500 chars saiu em 3 linhas sem `-y 0`, 1 linha só com
		// `-y 0`. Sem `-h -1` de propósito — mutuamente exclusivo com `-y 0`
		// (`Sqlcmd: The -h and the -y 0 options are mutually exclusive.`) — mas testado ao vivo:
		// essa query específica (uma única coluna sem nome, resultado de FOR JSON) não imprime
		// nenhuma linha de cabeçalho/separador de qualquer forma, então a ausência de `-h -1` não
		// contamina o parseJSONLinesPreview (que também tolera e descarta linhas não-JSON, rede de
		// segurança extra caso esse comportamento mude em outra versão do SQL Server/sqlcmd).
		// `ORDER BY (SELECT NULL)` é o truque padrão pra satisfazer a exigência de ORDER BY do
		// OFFSET/FETCH quando nenhuma coluna de ordenação foi escolhida pelo usuário.
		buildPreview: func(p dbConnParams, pv dbPreviewParams, timeoutSec int) string {
			host, port, user, _, db, useTLS, skipTLSVerify := sqlserverEffectiveParams(p)
			args := []string{sqlserverCmdPath, "-S", quoteShellArg(fmt.Sprintf("%s,%d", host, port))}
			if user != "" {
				args = append(args, "-U", quoteShellArg(user))
			}
			if db != "" {
				args = append(args, "-d", quoteShellArg(db))
			}
			if useTLS {
				args = append(args, "-N")
			}
			if skipTLSVerify {
				args = append(args, "-C")
			}
			orderBy := "(SELECT NULL)"
			if pv.SortColumn != "" {
				orderBy = fmt.Sprintf("%s %s", quoteSQLServerIdentifier(pv.SortColumn), strings.ToUpper(pv.SortDir))
			}
			query := fmt.Sprintf(
				"SET NOCOUNT ON; SELECT (SELECT t.* FOR JSON PATH, WITHOUT_ARRAY_WRAPPER, INCLUDE_NULL_VALUES) "+
					"FROM (SELECT * FROM %s ORDER BY %s OFFSET %d ROWS FETCH NEXT %d ROWS ONLY) AS t;",
				quoteSQLServerIdentifier(pv.Object), orderBy, pv.Offset, pv.Limit)
			args = append(args, "-y", "0", "-Q", quoteShellArg(query))
			return fmt.Sprintf("timeout %ds %s%s 2>&1", timeoutSec, sqlserverPasswordPrefix(p), strings.Join(args, " "))
		},
		parsePreviewOutput: parseJSONLinesPreview,
		resolveDatabaseLabel: func(p dbConnParams) string {
			_, _, _, _, db, _, _ := sqlserverEffectiveParams(p)
			return db
		},
		networkErrorRegex: regexp.MustCompile(`(?i)(a network-related or instance-specific error|tcp provider|no such host is known|login timeout expired|connection timeout expired)`),
		authErrorRegex:    regexp.MustCompile(`(?i)(login failed for user|cannot open server .* requested by the login)`),
		tlsErrorRegex:     regexp.MustCompile(`(?i)(ssl provider|certificate verify failed|certificate chain was issued by an authority that is not trusted|encryption\(ssl/tls\) handshake failed)`),
	},
}

// getOrCreateDBEphemeralContainer anexa um ephemeral container com a imagem do engine no pod real
// informado — réplica de getOrCreateKafkaEphemeralContainer (kafka_test_tool.go), parametrizada
// pela imagem. Reusa um container existente com a mesma imagem+alvo se ainda estiver Running.
func getOrCreateDBEphemeralContainer(ctx context.Context, clientset kubernetes.Interface, namespace, podName, targetContainer, image string) (containerName string, err error) {
	pod, err := clientset.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("falha ao buscar pod %s: %w", podName, err)
	}

	for _, ec := range pod.Spec.EphemeralContainers {
		if ec.Image != image || ec.TargetContainerName != targetContainer {
			continue
		}
		for _, status := range pod.Status.EphemeralContainerStatuses {
			if status.Name == ec.Name && status.State.Running != nil {
				return ec.Name, nil
			}
		}
	}

	containerName = fmt.Sprintf("db-test-%d", time.Now().Unix())

	patchData, err := json.Marshal(map[string]interface{}{
		"spec": map[string]interface{}{
			"ephemeralContainers": []map[string]interface{}{
				{
					"name":                containerName,
					"image":               image,
					"command":             []string{"sleep", "300"},
					"imagePullPolicy":     "IfNotPresent",
					"targetContainerName": targetContainer,
				},
			},
		},
	})
	if err != nil {
		return "", fmt.Errorf("falha ao montar patch do ephemeral container: %w", err)
	}

	_, err = clientset.CoreV1().Pods(namespace).Patch(
		ctx, podName, types.StrategicMergePatchType, patchData, metav1.PatchOptions{}, "ephemeralcontainers",
	)
	if err != nil {
		return "", fmt.Errorf("falha ao anexar ephemeral container: %w", err)
	}

	return containerName, nil
}

// waitDBEphemeralContainerRunning espera o ephemeral container ficar Running — réplica direta de
// waitKafkaEphemeralContainerRunning (kafka_test_tool.go).
func waitDBEphemeralContainerRunning(ctx context.Context, clientset kubernetes.Interface, namespace, podName, containerName string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		pod, err := clientset.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("falha ao consultar status do pod: %w", err)
		}
		for _, status := range pod.Status.EphemeralContainerStatuses {
			if status.Name != containerName {
				continue
			}
			if status.State.Running != nil {
				return nil
			}
			if status.State.Terminated != nil {
				return fmt.Errorf("ephemeral container terminou inesperadamente: %s", status.State.Terminated.Reason)
			}
			if status.State.Waiting != nil && status.State.Waiting.Reason != "ContainerCreating" {
				return fmt.Errorf("ephemeral container aguardando: %s", status.State.Waiting.Reason)
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(dbTestEphemeralPollInterval):
		}
	}
	return fmt.Errorf("timeout esperando ephemeral container ficar pronto (%s)", timeout)
}

// resolveDBCredentials devolve (username, password) — da fonte manual ou de um Secret K8s. Réplica
// de resolveKafkaCredentials (kafka_test_tool.go). Nunca expõe a senha de volta pro chamador HTTP.
func resolveDBCredentials(ctx context.Context, clientset kubernetes.Interface, auth *DBAuthConfig) (username, password string, err error) {
	if auth.SecretRef != nil {
		ref := auth.SecretRef
		secret, getErr := clientset.CoreV1().Secrets(ref.Namespace).Get(ctx, ref.Name, metav1.GetOptions{})
		if getErr != nil {
			return "", "", fmt.Errorf("falha ao ler secret %s/%s: %w", ref.Namespace, ref.Name, getErr)
		}
		userKey := ref.UsernameKey
		if userKey == "" {
			userKey = "username"
		}
		passKey := ref.PasswordKey
		if passKey == "" {
			passKey = "password"
		}
		userBytes, ok := secret.Data[userKey]
		if !ok {
			return "", "", fmt.Errorf("chave %q não encontrada no secret %s/%s", userKey, ref.Namespace, ref.Name)
		}
		passBytes, ok := secret.Data[passKey]
		if !ok {
			return "", "", fmt.Errorf("chave %q não encontrada no secret %s/%s", passKey, ref.Namespace, ref.Name)
		}
		username, password = string(userBytes), string(passBytes)
		if ref.Base64Decode {
			username, err = decodeSecretValueBase64(username)
			if err != nil {
				return "", "", fmt.Errorf("valor da chave %q não é base64 válido (Base64Decode marcado): %w", userKey, err)
			}
			password, err = decodeSecretValueBase64(password)
			if err != nil {
				return "", "", fmt.Errorf("valor da chave %q não é base64 válido (Base64Decode marcado): %w", passKey, err)
			}
		}
		return username, password, nil
	}
	return auth.Username, auth.Password, nil
}

// resolveDBHostPort devolve (host, port) lidos de um ConfigMap K8s — mesmo padrão de
// resolveDBCredentials, mas ConfigMap porque host/porta não são dado sensível.
func resolveDBHostPort(ctx context.Context, clientset kubernetes.Interface, ref *DBConfigMapRef) (host string, port int, err error) {
	cm, getErr := clientset.CoreV1().ConfigMaps(ref.Namespace).Get(ctx, ref.Name, metav1.GetOptions{})
	if getErr != nil {
		return "", 0, fmt.Errorf("falha ao ler configmap %s/%s: %w", ref.Namespace, ref.Name, getErr)
	}
	hostKey := ref.HostKey
	if hostKey == "" {
		hostKey = "host"
	}
	portKey := ref.PortKey
	if portKey == "" {
		portKey = "port"
	}
	hostVal, ok := cm.Data[hostKey]
	if !ok {
		return "", 0, fmt.Errorf("chave %q não encontrada no configmap %s/%s", hostKey, ref.Namespace, ref.Name)
	}
	portVal, ok := cm.Data[portKey]
	if !ok {
		return "", 0, fmt.Errorf("chave %q não encontrada no configmap %s/%s", portKey, ref.Namespace, ref.Name)
	}
	portInt, convErr := strconv.Atoi(strings.TrimSpace(portVal))
	if convErr != nil {
		return "", 0, fmt.Errorf("valor da chave %q no configmap %s/%s não é um número válido: %q", portKey, ref.Namespace, ref.Name, portVal)
	}
	return strings.TrimSpace(hostVal), portInt, nil
}

// resolveDBConnString devolve a connection string — da fonte manual ou de um Secret/ConfigMap K8s
// (Kind decide qual). Mesmo padrão de resolveDBCredentials/resolveDBHostPort. A string lida pode
// já ter a credencial embutida na URI (comum em ConfigMaps de app legado) — resolvida aqui e
// usada só server-side, nunca trafega de volta pro chamador HTTP.
func resolveDBConnString(ctx context.Context, clientset kubernetes.Interface, auth *DBAuthConfig) (string, error) {
	ref := auth.ConnStringRef
	if ref == nil {
		return auth.ConnectionString, nil
	}

	key := ref.Key
	if key == "" {
		key = "connectionString"
	}

	if ref.Kind == "secret" {
		secret, err := clientset.CoreV1().Secrets(ref.Namespace).Get(ctx, ref.Name, metav1.GetOptions{})
		if err != nil {
			return "", fmt.Errorf("falha ao ler secret %s/%s: %w", ref.Namespace, ref.Name, err)
		}
		val, ok := secret.Data[key]
		if !ok {
			return "", fmt.Errorf("chave %q não encontrada no secret %s/%s", key, ref.Namespace, ref.Name)
		}
		return strings.TrimSpace(string(val)), nil
	}

	cm, err := clientset.CoreV1().ConfigMaps(ref.Namespace).Get(ctx, ref.Name, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("falha ao ler configmap %s/%s: %w", ref.Namespace, ref.Name, err)
	}
	val, ok := cm.Data[key]
	if !ok {
		return "", fmt.Errorf("chave %q não encontrada no configmap %s/%s", key, ref.Namespace, ref.Name)
	}
	return strings.TrimSpace(val), nil
}

func ceilSeconds(ms int) int {
	sec := (ms + 999) / 1000
	if sec < 1 {
		return 1
	}
	return sec
}

// dbExecFunc abstrai ONDE o script de cada estágio roda — dentro de um ephemeral container
// (execCmdInPod, modo "pod") ou dentro de um container Docker local no host do servidor
// (execLocalDocker, modo "local"). runDBConnectivityStage/runDBBrowseStage não sabem qual dos
// dois é — só chamam a função, o que evita duplicar a lógica de classificação de erro entre os
// dois modos.
type dbExecFunc func(ctx context.Context, script string) (string, error)

// execLocalDocker roda o script dentro de um container Docker local (`docker run --rm`), no host
// onde o servidor da aplicação está rodando — sem tocar o cluster K8s. Modo "terminal": útil
// quando o banco é alcançável diretamente da rede do servidor (VPN, endpoint público,
// LoadBalancer) e não faz sentido/não é possível refletir a identidade de rede de um pod
// específico. Reusa a MESMA imagem do engine já usada no modo "pod" (mesmo princípio do `kcat`
// no Teste de Kafka) — nunca exige os clientes (psql/mysql/mongosh/redis-cli) instalados
// nativamente no servidor, só o binário `docker` + daemon rodando lá.
//
// `--network host` dá ao container a mesma visibilidade de rede que o processo do servidor teria
// rodando nativamente — funciona em Linux/WSL2 (ambiente alvo desta app, ver CLAUDE.md); Docker
// Desktop no Mac/Windows tem semântica de host networking diferente e não foi validado aqui.
//
// Mesmo formato de erro de execCmdInPod ("... (stderr: ...)") pra reusar extractStderr sem
// duplicar a classificação de erro entre os dois modos de execução.
//
// `name` identifica o container (--name) e `label` marca de qual ferramenta ele é (--label,
// dbTestDockerLabel ou kafkaTestDockerLabel) pra dar pra limpar depois — tanto na hora (se `ctx`
// for cancelado no meio do `docker run`) quanto pelo reaper periódico (ver db_test_docker.go /
// kafka_test_docker.go, mesmo reapOrphanedContainersByLabel compartilhado). `--rm` já cobre o
// caso comum (processo termina sozinho, com ou sem o `timeout Ns` interno do script estourar); o
// gap é só quando o processo `docker` (CLI) é morto via SIGKILL pelo cancelamento do context —
// sinal que ele não consegue interceptar pra fazer sua própria limpeza, podendo deixar o
// container órfão rodando no daemon.
func execLocalDocker(ctx context.Context, image, name, label, script string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", "run", "--rm",
		"--name", name, "--label", label,
		"--network", "host", image, "sh", "-c", script)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			cleanupCancelledDockerContainer(name)
		}
		// stdout.String() é devolvido mesmo no erro — mesmo motivo de execCmdInPod: os scripts
		// dos engines fazem `... 2>&1`, então a mensagem de erro real do cliente (psql/mysql/
		// mongosh/redis-cli) está em stdout, não no stderr do processo `docker run` em si.
		return stdout.String(), fmt.Errorf("exec: %v (stderr: %s)", err, stderr.String())
	}
	return stdout.String(), nil
}

// runDBConnectivityStage executa o script de conectividade/autenticação do engine — um único
// exec, classificando o resultado pelas regexes do próprio engine.
func runDBConnectivityStage(ctx context.Context, run dbExecFunc, engine dbEngine, conn dbConnParams, timeoutMs int) DBStageResult {
	script := engine.buildConnectivity(conn, ceilSeconds(timeoutMs))
	stdout, err := run(ctx, script)
	if err != nil {
		// A saída real do cliente (psql/mysql/mongosh/redis-cli) vem em `stdout` — o script roda
		// com `2>&1`, então stderr do processo já foi mesclado ali. `extractStderr(err)` só serve de
		// fallback pra falhas que nunca chegaram a rodar o script (ex: erro do SPDY executor).
		raw := strings.TrimSpace(stdout)
		if raw == "" {
			raw = extractStderr(err)
		}
		switch {
		case engine.networkErrorRegex.MatchString(raw):
			return DBStageResult{Status: dbStageTCPFailed, Message: "Não conseguiu conectar no host (rede/DNS)", RawOutput: raw}
		// tlsErrorRegex checado antes de authErrorRegex: no Postgres, "no pg_hba.conf entry ...
		// no encryption" bate também na regra genérica de auth, mas é o servidor exigindo TLS
		// (ex: Azure Database for PostgreSQL), não credencial errada — checar TLS primeiro evita
		// que esse caso seja mascarado como falha de autenticação.
		case engine.tlsErrorRegex.MatchString(raw):
			return DBStageResult{Status: dbStageTLSFailed, Message: "Falha de handshake TLS/SSL — servidor pode exigir conexão criptografada (habilite o TLS neste teste)", RawOutput: raw}
		case engine.authErrorRegex.MatchString(raw):
			return DBStageResult{Status: dbStageAuthFailed, Message: "Falha de autenticação", RawOutput: raw}
		default:
			return DBStageResult{Status: dbStageUnknownFailed, Message: "Falha não classificada — ver saída bruta", RawOutput: raw}
		}
	}
	return DBStageResult{Status: dbStageOK, Message: "Conectividade e autenticação OK", RawOutput: stdout}
}

// runDBBrowseStage executa o script de navegação só-leitura do engine (databases/tabelas/
// collections/chaves) — nunca escreve nada, por isso não exige confirmação.
func runDBBrowseStage(ctx context.Context, run dbExecFunc, engine dbEngine, conn dbConnParams, timeoutMs int) DBBrowseResult {
	if engine.buildBrowse == nil {
		return DBBrowseResult{Status: "skipped", Message: "Navegação não suportada para este engine"}
	}

	browseTimeoutMs := timeoutMs
	if browseTimeoutMs < dbTestBrowseMinTimeoutMs {
		browseTimeoutMs = dbTestBrowseMinTimeoutMs
	}
	script := engine.buildBrowse(conn, ceilSeconds(browseTimeoutMs))
	stdout, err := run(ctx, script)
	if err != nil {
		raw := strings.TrimSpace(stdout)
		if raw == "" {
			raw = extractStderr(err)
		}
		return DBBrowseResult{Status: "failed", Message: "Falha ao listar objetos", RawOutput: raw}
	}

	objects, objectType, truncated := engine.parseBrowseOutput(stdout, conn)
	message := fmt.Sprintf("%d objeto(s) encontrado(s)", len(objects))
	if truncated {
		message += fmt.Sprintf(" — amostra limitada a %d, pode haver mais", dbRedisScanCap)
	}
	if len(objects) == 0 {
		message = "Nenhum objeto encontrado"
	}
	// Database só é relevante (e preenchido) quando já descemos um nível — no nível "database" a
	// lista JÁ É a lista de bancos, não há um banco "escolhido" ainda pra mostrar como contexto.
	var databaseLabel string
	if objectType != "database" && engine.resolveDatabaseLabel != nil {
		databaseLabel = engine.resolveDatabaseLabel(conn)
	}
	// parseServerInfo/parseInfoSections só existem pro Redis, e só no nível "database" (topo) — a
	// mesma chamada de INFO que lista os bancos 0-15 já traz as estatísticas do servidor e todas
	// as seções, ver RedisServerInfo/RedisInfoSection.
	var serverInfo *RedisServerInfo
	var infoSections []RedisInfoSection
	if objectType == "database" {
		if engine.parseServerInfo != nil {
			serverInfo = engine.parseServerInfo(stdout)
		}
		if engine.parseInfoSections != nil {
			infoSections = engine.parseInfoSections(stdout)
		}
	}
	return DBBrowseResult{
		Status:       "ok",
		Message:      message,
		ObjectType:   objectType,
		Objects:      objects,
		Database:     databaseLabel,
		Truncated:    truncated,
		ServerInfo:   serverInfo,
		InfoSections: infoSections,
		RawOutput:    stdout,
	}
}

// ─── Handler: endpoint SSE + rotas ─────────────────────────────────────────────

// DBTestHandler orquestra o teste de banco de dados sob demanda — mesmo esqueleto do
// KafkaTestHandler (SSE, lock de 1-teste-por-usuário), sem estágio de escrita: o teste nunca muta
// o banco, só conecta/autentica e opcionalmente navega (só leitura).
type DBTestHandler struct {
	kubeManager    *config.KubeConfigManager
	tracker        *sse.ProgressTracker
	historyTracker *history.HistoryTracker
	cancelFuncs    sync.Map // sessionID -> context.CancelFunc
	runningUsers   sync.Map // userEmail -> struct{} — "um teste por vez por usuário"
}

// NewDBTestHandler cria o handler do teste de banco de dados.
func NewDBTestHandler(km *config.KubeConfigManager, tracker *sse.ProgressTracker, ht *history.HistoryTracker) *DBTestHandler {
	go startDBTestContainerReaper()
	return &DBTestHandler{kubeManager: km, tracker: tracker, historyTracker: ht}
}

// Run inicia o teste de banco de dados e retorna um session_id para streaming SSE.
// POST /api/v1/db-test/run
func (h *DBTestHandler) Run(c *gin.Context) {
	var req RunDBTestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse("INVALID_REQUEST", err.Error()))
		return
	}

	engine, ok := validateDBTestRequest(c, &req)
	if !ok {
		return
	}

	userInfo := GetUserInfoForHistory(c)

	lockKey := userInfo.Email
	if lockKey == "" {
		lockKey = "unknown"
	}
	if _, alreadyRunning := h.runningUsers.LoadOrStore(lockKey, struct{}{}); alreadyRunning {
		c.JSON(http.StatusConflict, errorResponse("TEST_ALREADY_RUNNING",
			"você já tem um teste de banco de dados em andamento — aguarde terminar ou cancele antes de iniciar outro"))
		return
	}

	sessionID := uuid.New().String()
	ctx, cancel := context.WithCancel(context.Background())
	h.cancelFuncs.Store(sessionID, cancel)

	go func() {
		defer h.cancelFuncs.Delete(sessionID)
		defer h.runningUsers.Delete(lockKey)
		h.runTest(ctx, sessionID, req, engine, userInfo)
	}()

	c.JSON(http.StatusOK, gin.H{"session_id": sessionID})
}

// validateDBTestRequest valida e normaliza os campos de RunDBTestRequest compartilhados entre
// Run (assíncrono, via SSE) e Preview (síncrono, amostra de dados de uma tabela/collection/chave
// específica) — evita duplicar ~100 linhas de validação entre os dois. Escreve a resposta de erro
// e devolve ok=false na primeira falha; devolve o dbEngine resolvido quando tudo é válido.
func validateDBTestRequest(c *gin.Context, req *RunDBTestRequest) (dbEngine, bool) {
	req.ExecutionMode = strings.ToLower(strings.TrimSpace(req.ExecutionMode))
	if req.ExecutionMode == "" {
		req.ExecutionMode = "pod"
	}
	if req.ExecutionMode != "pod" && req.ExecutionMode != "local" {
		c.JSON(http.StatusBadRequest, errorResponse("INVALID_EXECUTION_MODE", "execution_mode deve ser pod ou local"))
		return dbEngine{}, false
	}

	req.Cluster = strings.TrimSpace(req.Cluster)
	req.Namespace = strings.TrimSpace(req.Namespace)
	req.Deployment = strings.TrimSpace(req.Deployment)
	req.Engine = strings.ToLower(strings.TrimSpace(req.Engine))
	req.Host = strings.TrimSpace(req.Host)
	if req.Engine == "" {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_PARAMS", "engine é obrigatório"))
		return dbEngine{}, false
	}
	if req.ExecutionMode == "pod" && (req.Cluster == "" || req.Namespace == "" || req.Deployment == "") {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_PARAMS", "cluster, namespace e deployment são obrigatórios quando execution_mode é pod"))
		return dbEngine{}, false
	}

	engine, engineOk := dbEngines[req.Engine]
	if !engineOk {
		c.JSON(http.StatusBadRequest, errorResponse("INVALID_ENGINE", "engine deve ser postgres, mysql, mongodb, redis ou sqlserver"))
		return dbEngine{}, false
	}

	req.Auth.Mode = strings.ToLower(strings.TrimSpace(req.Auth.Mode))
	if req.Auth.Mode == "" {
		req.Auth.Mode = "none"
	}
	switch req.Auth.Mode {
	case "none", "userpass", "connstring":
	default:
		c.JSON(http.StatusBadRequest, errorResponse("INVALID_AUTH_MODE", "auth.mode deve ser none, userpass ou connstring"))
		return dbEngine{}, false
	}

	req.Auth.AuthMechanism = strings.ToUpper(strings.TrimSpace(req.Auth.AuthMechanism))
	if req.Auth.AuthMechanism != "" {
		if req.Engine != "mongodb" || req.Auth.Mode != "userpass" {
			c.JSON(http.StatusBadRequest, errorResponse("INVALID_AUTH_MECHANISM", "auth_mechanism só se aplica ao engine mongodb com auth.mode userpass"))
			return dbEngine{}, false
		}
		if !dbValidMongoAuthMechanisms[req.Auth.AuthMechanism] {
			c.JSON(http.StatusBadRequest, errorResponse("INVALID_AUTH_MECHANISM", "auth_mechanism deve ser SCRAM-SHA-1 ou SCRAM-SHA-256"))
			return dbEngine{}, false
		}
	}

	if req.Auth.Mode == "connstring" {
		if req.Auth.ConnStringRef != nil {
			req.Auth.ConnStringRef.Kind = strings.ToLower(strings.TrimSpace(req.Auth.ConnStringRef.Kind))
			if req.Auth.ConnStringRef.Kind == "" {
				req.Auth.ConnStringRef.Kind = "configmap"
			}
			if req.Auth.ConnStringRef.Kind != "configmap" && req.Auth.ConnStringRef.Kind != "secret" {
				c.JSON(http.StatusBadRequest, errorResponse("INVALID_CONNSTRING_REF_KIND", "connstring_ref.kind deve ser configmap ou secret"))
				return dbEngine{}, false
			}
			req.Auth.ConnStringRef.Namespace = strings.TrimSpace(req.Auth.ConnStringRef.Namespace)
			req.Auth.ConnStringRef.Name = strings.TrimSpace(req.Auth.ConnStringRef.Name)
			if req.Auth.ConnStringRef.Namespace == "" || req.Auth.ConnStringRef.Name == "" {
				c.JSON(http.StatusBadRequest, errorResponse("MISSING_CONNSTRING_REF", "connstring_ref precisa de namespace e name"))
				return dbEngine{}, false
			}
		} else {
			req.Auth.ConnectionString = strings.TrimSpace(req.Auth.ConnectionString)
			if req.Auth.ConnectionString == "" {
				c.JSON(http.StatusBadRequest, errorResponse("MISSING_CONNSTRING", "auth.connection_string ou auth.connstring_ref é obrigatório quando auth.mode é connstring"))
				return dbEngine{}, false
			}
			if code, hint := validateDBConnStringScheme(req.Engine, req.Auth.ConnectionString); code != "" {
				c.JSON(http.StatusBadRequest, errorResponse(code, hint))
				return dbEngine{}, false
			}
		}
		if req.HostConfigMapRef != nil {
			c.JSON(http.StatusBadRequest, errorResponse("INVALID_HOST_SOURCE", "host_configmap_ref não se aplica quando auth.mode é connstring — a connection string já embute host/porta"))
			return dbEngine{}, false
		}
	} else if req.HostConfigMapRef != nil {
		req.HostConfigMapRef.Namespace = strings.TrimSpace(req.HostConfigMapRef.Namespace)
		req.HostConfigMapRef.Name = strings.TrimSpace(req.HostConfigMapRef.Name)
		if req.HostConfigMapRef.Namespace == "" || req.HostConfigMapRef.Name == "" {
			c.JSON(http.StatusBadRequest, errorResponse("MISSING_CONFIGMAP_REF", "host_configmap_ref precisa de namespace e name"))
			return dbEngine{}, false
		}
	} else if req.Host == "" || req.Port <= 0 {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_HOST_PORT", "host e port são obrigatórios quando auth.mode não é connstring e host_configmap_ref não foi informado"))
		return dbEngine{}, false
	} else if strings.Contains(req.Host, "://") {
		// Proteção contra colar uma connection string inteira no campo "Host" (em vez de trocar
		// pro modo "Connection String") — sem essa checagem, buildPostgresURI/buildMongoURI
		// embutem o valor cru dentro de outra URI (`Host: fmt.Sprintf("%s:%d", p.Host, p.Port)`),
		// produzindo uma URI duplamente aninhada (ex: "postgresql://sqlserver://host:5432/postgres")
		// que psql/mongosh não conseguem parsear de forma sensata — o erro nativo do libpq nesse
		// caso não menciona nada sobre o campo Host ou URI aninhada, só um "invalid integer value
		// ... for connection option port" totalmente desconexo do problema real (bug real relatado
		// pelo usuário, ver CLAUDE.md).
		c.JSON(http.StatusBadRequest, errorResponse("HOST_LOOKS_LIKE_CONNSTRING",
			fmt.Sprintf("O campo Host não deve conter uma connection string inteira (recebido: %q) — troque para o modo \"Connection String\" e cole ali, ou preencha aqui só o hostname/IP.", req.Host)))
		return dbEngine{}, false
	}

	if req.Auth.Mode == "userpass" && req.Auth.SecretRef != nil {
		req.Auth.SecretRef.Namespace = strings.TrimSpace(req.Auth.SecretRef.Namespace)
		req.Auth.SecretRef.Name = strings.TrimSpace(req.Auth.SecretRef.Name)
		if req.Auth.SecretRef.Namespace == "" || req.Auth.SecretRef.Name == "" {
			c.JSON(http.StatusBadRequest, errorResponse("MISSING_SECRET_REF", "secret_ref precisa de namespace e name"))
			return dbEngine{}, false
		}
	}

	// No modo "local" cluster/namespace/deployment não são necessários pro teste em si (roda
	// direto no host do servidor), MAS qualquer referência de Secret/ConfigMap ainda precisa de
	// um cluster pra ser lida via API do K8s.
	usesK8sRef := (req.Auth.Mode == "userpass" && req.Auth.SecretRef != nil) ||
		(req.Auth.Mode == "connstring" && req.Auth.ConnStringRef != nil) ||
		req.HostConfigMapRef != nil
	if req.Cluster == "" && usesK8sRef {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_CLUSTER", "cluster é obrigatório quando secret_ref/host_configmap_ref/connstring_ref é usado"))
		return dbEngine{}, false
	}

	if req.TimeoutMs <= 0 {
		req.TimeoutMs = dbTestDefaultTimeoutMs
	}
	if req.TimeoutMs > dbTestMaxTimeoutMs {
		req.TimeoutMs = dbTestMaxTimeoutMs
	}

	return engine, true
}

// Stream conecta o cliente ao fluxo SSE de um teste em andamento.
// GET /api/v1/db-test/stream/:sessionId
func (h *DBTestHandler) Stream(c *gin.Context) {
	sessionID := c.Param("sessionId")
	if sessionID == "" {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_SESSION", "sessionId obrigatório"))
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	for _, evt := range h.tracker.GetReplayEvents(sessionID) {
		if _, err := c.Writer.WriteString(sse.FormatSSE(evt)); err != nil {
			return
		}
		c.Writer.Flush()
	}

	client := sse.NewClient(sessionID)
	h.tracker.AddClient(client)
	defer h.tracker.RemoveClient(sessionID)

	ctx := c.Request.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-client.Channel:
			if !ok {
				return
			}
			if _, err := c.Writer.WriteString(sse.FormatSSE(event)); err != nil {
				return
			}
			c.Writer.Flush()
			if event.Type == "complete" || event.Type == "error" {
				return
			}
		}
	}
}

// Cancel força a parada de um teste em andamento.
// POST /api/v1/db-test/cancel/:sessionId
func (h *DBTestHandler) Cancel(c *gin.Context) {
	sessionID := c.Param("sessionId")
	if sessionID == "" {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_SESSION", "sessionId obrigatório"))
		return
	}

	if val, ok := h.cancelFuncs.Load(sessionID); ok {
		val.(context.CancelFunc)()
		h.cancelFuncs.Delete(sessionID)
		c.JSON(http.StatusOK, gin.H{"cancelled": true})
	} else {
		c.JSON(http.StatusNotFound, gin.H{"cancelled": false, "message": "sessão não encontrada ou já finalizada"})
	}
}

// runTest executa o fluxo completo (resolver pod → anexar ephemeral container → conectividade →
// navegação opcional), reportando progresso via SSE a cada etapa.
func (h *DBTestHandler) runTest(ctx context.Context, sessionID string, req RunDBTestRequest, engine dbEngine, userInfo history.UserInfo) {
	start := time.Now()

	send := func(evtType, phase, message string, progress float64) {
		h.tracker.SendToClient(sessionID, sse.ProgressEvent{
			ID:        sessionID,
			Type:      evtType,
			Phase:     phase,
			Message:   message,
			Progress:  progress,
			Timestamp: time.Now(),
			Cluster:   req.Cluster,
		})
	}

	fail := func(stage string, err error) {
		send("error", "failed", fmt.Sprintf("%s: %v", stage, err), 1.0)
		h.logHistory(req, userInfo, start, nil, fmt.Errorf("%s: %w", stage, err))
	}

	send("init", "started", "Iniciando teste de banco de dados...", 0.05)

	// clientset só é resolvido quando necessário — modo "local" sem nenhuma referência de Secret/
	// ConfigMap não precisa tocar o cluster K8s em nenhum momento.
	var clientset kubernetes.Interface
	if req.Cluster != "" {
		var err error
		clientset, err = h.kubeManager.GetClient(req.Cluster)
		if err != nil {
			fail("falha ao conectar no cluster", err)
			return
		}
	}

	conn := dbConnParams{
		Mode:            req.Auth.Mode,
		Host:            req.Host,
		Port:            req.Port,
		Database:        req.Auth.Database,
		ConnStr:         req.Auth.ConnectionString,
		UseTLS:          req.Auth.UseTLS,
		SkipTLSVerify:   req.Auth.SkipTLSVerify,
		AuthMechanism:   req.Auth.AuthMechanism,
		RedisKeyPattern: req.RedisKeyPattern,
	}
	if req.Auth.Mode == "userpass" {
		username, password, err := resolveDBCredentials(ctx, clientset, &req.Auth)
		if err != nil {
			fail("falha ao resolver credenciais", err)
			return
		}
		conn.Username = username
		conn.Password = password
	}
	if req.Auth.Mode == "connstring" && req.Auth.ConnStringRef != nil {
		connStr, err := resolveDBConnString(ctx, clientset, &req.Auth)
		if err != nil {
			fail("falha ao resolver connection string", err)
			return
		}
		if code, hint := validateDBConnStringScheme(req.Engine, connStr); code != "" {
			fail("connection string inválida", fmt.Errorf("%s", hint))
			return
		}
		conn.ConnStr = connStr
	}
	if req.HostConfigMapRef != nil {
		host, port, err := resolveDBHostPort(ctx, clientset, req.HostConfigMapRef)
		if err != nil {
			fail("falha ao resolver host/porta do configmap", err)
			return
		}
		conn.Host = host
		conn.Port = port
		req.Host = host // usado no logHistory — auditoria reflete o host resolvido, não a ref
		req.Port = port
	}

	var run dbExecFunc
	var podName, containerName string

	if req.ExecutionMode == "local" {
		send("local_exec", "in_progress", fmt.Sprintf("Executando localmente via Docker (%s)...", engine.image), 0.3)
		image := engine.image
		containerName := "k8s-hpa-dbtest-" + sessionID
		run = func(ctx context.Context, script string) (string, error) {
			return execLocalDocker(ctx, image, containerName, dbTestDockerLabel, script)
		}
	} else {
		restConfig, err := h.kubeManager.GetRestConfig(req.Cluster)
		if err != nil {
			fail("falha ao obter configuração do cluster", err)
			return
		}

		send("resolve_deployment", "in_progress", fmt.Sprintf("Localizando pod Running do deployment %q...", req.Deployment), 0.15)
		resolvedPod, targetContainer, err := resolvePodForDeployment(ctx, clientset, req.Namespace, req.Deployment, "", "")
		if err != nil {
			fail("falha ao localizar pod do deployment", err)
			return
		}
		podName = resolvedPod

		send("ephemeral_container", "in_progress", fmt.Sprintf("Anexando container de teste (%s) no pod %s...", engine.label, podName), 0.3)
		containerName, err = getOrCreateDBEphemeralContainer(ctx, clientset, req.Namespace, podName, targetContainer, engine.image)
		if err != nil {
			fail("falha ao anexar ephemeral container", err)
			return
		}
		if err := waitDBEphemeralContainerRunning(ctx, clientset, req.Namespace, podName, containerName, dbTestEphemeralReadyTimeout); err != nil {
			fail("ephemeral container não ficou pronto", err)
			return
		}

		ns, pod, container := req.Namespace, podName, containerName
		run = func(ctx context.Context, script string) (string, error) {
			return execCmdInPod(ctx, clientset, restConfig, ns, pod, container, []string{"sh", "-c", script})
		}
	}

	send("connectivity", "in_progress", fmt.Sprintf("Testando conectividade e autenticação (%s)...", engine.label), 0.5)
	connectivity := runDBConnectivityStage(ctx, run, engine, conn, req.TimeoutMs)

	result := DBTestResult{
		TargetPod:          podName,
		EphemeralContainer: containerName,
		Connectivity:       connectivity,
		Browse:             DBBrowseResult{Status: "skipped"},
	}

	if req.Browse {
		if connectivity.Status != dbStageOK {
			result.Browse = DBBrowseResult{Status: "skipped", Message: "Pulado — conectividade falhou antes de tentar navegar"}
		} else {
			send("browse", "in_progress", "Listando objetos do banco...", 0.8)
			result.Browse = runDBBrowseStage(ctx, run, engine, conn, req.TimeoutMs)
		}
	}

	h.tracker.SendToClient(sessionID, sse.ProgressEvent{
		ID:        sessionID,
		Type:      "complete",
		Phase:     "completed",
		Message:   "Teste de banco de dados concluído",
		Progress:  1.0,
		Timestamp: time.Now(),
		Cluster:   req.Cluster,
		Result:    result,
	})
	h.logHistory(req, userInfo, start, &result, nil)
}

// logHistory registra a execução no HistoryTracker. Nunca inclui username/password/connection
// string — só engine/host/namespace/resultado resumido, mesmo quando a credencial foi manual.
func (h *DBTestHandler) logHistory(req RunDBTestRequest, userInfo history.UserInfo, start time.Time, result *DBTestResult, opErr error) {
	if h.historyTracker == nil {
		return
	}

	status := "success"
	errMsg := ""
	if opErr != nil {
		status = "failed"
		errMsg = opErr.Error()
	}

	after := map[string]interface{}{
		"execution_mode": req.ExecutionMode,
		"namespace":      req.Namespace,
		"deployment":     req.Deployment,
		"engine":         req.Engine,
		"auth_mode":      req.Auth.Mode,
		"use_tls":        req.Auth.UseTLS,
		"browse":         req.Browse,
	}
	if req.Host != "" {
		after["host"] = req.Host
	}
	if req.Auth.Database != "" {
		after["database"] = req.Auth.Database
	}
	if req.RedisKeyPattern != "" {
		after["redis_key_pattern"] = req.RedisKeyPattern
	}
	if result != nil {
		after["target_pod"] = result.TargetPod
		after["ephemeral_container"] = result.EphemeralContainer
		after["connectivity_status"] = result.Connectivity.Status
		after["browse_status"] = result.Browse.Status
	}

	resource := req.Host
	if resource == "" {
		resource = req.Engine + " (connection string)"
	}

	h.historyTracker.Log(history.HistoryEntry{
		UserEmail: userInfo.Email,
		UserName:  userInfo.Name,
		Action:    "db_test",
		Resource:  resource,
		Cluster:   req.Cluster,
		Status:    status,
		After:     after,
		Duration:  time.Since(start).Milliseconds(),
		ErrorMsg:  errMsg,
	})
}
