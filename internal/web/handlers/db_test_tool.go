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
	dbTestEphemeralReadyTimeout = 30 * time.Second
	dbTestEphemeralPollInterval = 500 * time.Millisecond

	dbTestDefaultTimeoutMs = 5000
	dbTestMaxTimeoutMs     = 15000

	// dbRedisScanCap limita quantas chaves o estágio de navegação do Redis retorna — `--scan`
	// sozinho não tem limite total (só controla o tamanho do lote por iteração do cursor), então
	// SEMPRE fazemos pipe com `head` pra garantir um teto real. Nunca usar `KEYS *` (bloqueia
	// instâncias grandes).
	dbRedisScanCap = 100
)

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
	Engine     string `json:"engine"` // postgres | mysql | mongodb | redis
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
	// Truncated é true quando o resultado foi cortado por um teto de segurança (só acontece no
	// Redis — SCAN sobre um keyspace grande) — a lista exibida é uma AMOSTRA, não completa.
	Truncated bool   `json:"truncated,omitempty"`
	RawOutput string `json:"raw_output"`
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
	u.RawQuery = q.Encode()
	return u.String()
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
			if strings.TrimSpace(p.Database) != "" {
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
			if strings.TrimSpace(p.Database) == "" {
				return namesToObjects(lines), "database", false
			}
			return groupColumnsToTablesWithStats(lines), "table", false
		},
		networkErrorRegex: regexp.MustCompile(`(?i)(could not connect to server|connection refused|could not translate host name|timeout expired)`),
		authErrorRegex:    regexp.MustCompile(`(?i)(password authentication failed|role .* does not exist|no pg_hba\.conf entry)`),
		// "no pg_hba.conf entry ... no encryption" é o Postgres recusando a conexão porque ela
		// chegou sem SSL e o servidor só tem regras `hostssl` (ex: Azure Database for PostgreSQL,
		// que exige TLS por padrão) — não é senha/usuário errado, é o toggle TLS desligado no teste.
		// Checado antes de authErrorRegex (ver ordem do switch em runDBConnectivityStage).
		tlsErrorRegex: regexp.MustCompile(`(?i)(ssl error|server does not support ssl|certificate verify failed|no pg_hba\.conf entry.*no encryption)`),
	},
	"mysql": {
		label: "MySQL/MariaDB",
		// mariadb:11 — cliente mysql/mariadb compatível com servidores MySQL e MariaDB.
		image: "mariadb:11",
		buildConnectivity: func(p dbConnParams, timeoutSec int) string {
			host, port, user, pass, db := mysqlEffectiveParams(p)
			args := []string{"mysql", "-h", quoteShellArg(host), "-P", strconv.Itoa(port)}
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
			prefix := ""
			if pass != "" {
				prefix = "MYSQL_PWD=" + quoteShellArg(pass) + " "
			}
			return fmt.Sprintf("timeout %ds %s%s 2>&1", timeoutSec, prefix, strings.Join(args, " "))
		},
		// Sem Database informado: lista databases. Com Database informado: desce um nível e lista
		// as tabelas + colunas (mesmo formato "tabela|coluna|tipo|..." do Postgres) — table_schema
		// resolvido via DATABASE() (a sessão já conecta nesse banco, sem precisar reinterpolar o
		// nome na query). Estatísticas por tabela via information_schema.tables (catálogo, nunca
		// um scan): data_length ("Size"), data_length+index_length ("Storage Size" — equivalente
		// ao Compass) e table_rows (estimativa de linhas, atualizada por ANALYZE TABLE).
		buildBrowse: func(p dbConnParams, timeoutSec int) string {
			host, port, user, pass, db := mysqlEffectiveParams(p)
			args := []string{"mysql", "-h", quoteShellArg(host), "-P", strconv.Itoa(port)}
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
			prefix := ""
			if pass != "" {
				prefix = "MYSQL_PWD=" + quoteShellArg(pass) + " "
			}
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
			if db := strings.TrimSpace(p.Database); db != "" {
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
			if strings.TrimSpace(p.Database) != "" {
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
		networkErrorRegex: regexp.MustCompile(`(?i)(econnrefused|serverselectionerror|getaddrinfo enotfound|connection timed out)`),
		authErrorRegex:    regexp.MustCompile(`(?i)(authentication failed|auth error|not authorized)`),
		tlsErrorRegex:     regexp.MustCompile(`(?i)(tls|ssl).*(error|handshake|certificate)`),
	},
	"redis": {
		label: "Redis",
		// redis:7-alpine — inclui redis-cli, imagem leve (~30MB).
		image: "redis:7-alpine",
		buildConnectivity: func(p dbConnParams, timeoutSec int) string {
			cmd := redisCommand(p, "PING")
			return fmt.Sprintf("timeout %ds %s 2>&1", timeoutSec, cmd)
		},
		// Sem Database (índice) informado: visão geral por banco lógico via `INFO keyspace` —
		// preenche o "nível database" que os outros 3 engines já tinham (Postgres/MySQL/Mongo
		// listam databases quando nenhum nome é informado; o Redis antes pulava direto pra lista
		// de chaves, sempre). `INFO keyspace` é um único comando, server-wide — reporta os 16
		// bancos (0-15) independente de qual `-n` a conexão está usando.
		//
		// Com Database informado: mesmo scan de chaves de antes (`--scan` + `head`, nunca `KEYS
		// *`), mas cada chave agora roda TYPE + MEMORY USAGE num único `redis-cli` (comandos
		// enviados via stdin, uma conexão só) em vez de dois processos separados — mesmo número
		// de round-trips de antes (100 no teto), só que cada um faz 2 comandos ao invés de 1.
		// MEMORY USAGE é O(1)-ish (não escaneia a chave inteira pra tipos simples), mesmo espírito
		// de segurança do teto de 100 chaves.
		buildBrowse: func(p dbConnParams, timeoutSec int) string {
			if strings.TrimSpace(p.Database) == "" {
				cmd := redisCommand(p, "INFO", "keyspace")
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
		networkErrorRegex: regexp.MustCompile(`(?i)(could not connect|connection refused|name or service not known)`),
		authErrorRegex:    regexp.MustCompile(`(?i)(noauth|wrongpass|invalid username-password)`),
		tlsErrorRegex:     regexp.MustCompile(`(?i)(tls|ssl).*(error|handshake)`),
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
// `name` identifica o container (--name + --label dbTestDockerLabel) pra dar pra limpar depois —
// tanto na hora (se `ctx` for cancelado no meio do `docker run`) quanto pelo reaper periódico
// (ver db_test_docker.go). `--rm` já cobre o caso comum (processo termina sozinho, com ou sem o
// `timeout Ns` interno do script estourar); o gap é só quando o processo `docker` (CLI) é morto
// via SIGKILL pelo cancelamento do context — sinal que ele não consegue interceptar pra fazer sua
// própria limpeza, podendo deixar o container órfão rodando no daemon.
func execLocalDocker(ctx context.Context, image, name, script string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", "run", "--rm",
		"--name", name, "--label", dbTestDockerLabel,
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

	script := engine.buildBrowse(conn, ceilSeconds(timeoutMs))
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
	return DBBrowseResult{
		Status:     "ok",
		Message:    message,
		ObjectType: objectType,
		Objects:    objects,
		Truncated:  truncated,
		RawOutput:  stdout,
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

	req.ExecutionMode = strings.ToLower(strings.TrimSpace(req.ExecutionMode))
	if req.ExecutionMode == "" {
		req.ExecutionMode = "pod"
	}
	if req.ExecutionMode != "pod" && req.ExecutionMode != "local" {
		c.JSON(http.StatusBadRequest, errorResponse("INVALID_EXECUTION_MODE", "execution_mode deve ser pod ou local"))
		return
	}

	req.Cluster = strings.TrimSpace(req.Cluster)
	req.Namespace = strings.TrimSpace(req.Namespace)
	req.Deployment = strings.TrimSpace(req.Deployment)
	req.Engine = strings.ToLower(strings.TrimSpace(req.Engine))
	req.Host = strings.TrimSpace(req.Host)
	if req.Engine == "" {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_PARAMS", "engine é obrigatório"))
		return
	}
	if req.ExecutionMode == "pod" && (req.Cluster == "" || req.Namespace == "" || req.Deployment == "") {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_PARAMS", "cluster, namespace e deployment são obrigatórios quando execution_mode é pod"))
		return
	}

	engine, ok := dbEngines[req.Engine]
	if !ok {
		c.JSON(http.StatusBadRequest, errorResponse("INVALID_ENGINE", "engine deve ser postgres, mysql, mongodb ou redis"))
		return
	}

	req.Auth.Mode = strings.ToLower(strings.TrimSpace(req.Auth.Mode))
	if req.Auth.Mode == "" {
		req.Auth.Mode = "none"
	}
	switch req.Auth.Mode {
	case "none", "userpass", "connstring":
	default:
		c.JSON(http.StatusBadRequest, errorResponse("INVALID_AUTH_MODE", "auth.mode deve ser none, userpass ou connstring"))
		return
	}

	if req.Auth.Mode == "connstring" {
		if req.Auth.ConnStringRef != nil {
			req.Auth.ConnStringRef.Kind = strings.ToLower(strings.TrimSpace(req.Auth.ConnStringRef.Kind))
			if req.Auth.ConnStringRef.Kind == "" {
				req.Auth.ConnStringRef.Kind = "configmap"
			}
			if req.Auth.ConnStringRef.Kind != "configmap" && req.Auth.ConnStringRef.Kind != "secret" {
				c.JSON(http.StatusBadRequest, errorResponse("INVALID_CONNSTRING_REF_KIND", "connstring_ref.kind deve ser configmap ou secret"))
				return
			}
			req.Auth.ConnStringRef.Namespace = strings.TrimSpace(req.Auth.ConnStringRef.Namespace)
			req.Auth.ConnStringRef.Name = strings.TrimSpace(req.Auth.ConnStringRef.Name)
			if req.Auth.ConnStringRef.Namespace == "" || req.Auth.ConnStringRef.Name == "" {
				c.JSON(http.StatusBadRequest, errorResponse("MISSING_CONNSTRING_REF", "connstring_ref precisa de namespace e name"))
				return
			}
		} else {
			req.Auth.ConnectionString = strings.TrimSpace(req.Auth.ConnectionString)
			if req.Auth.ConnectionString == "" {
				c.JSON(http.StatusBadRequest, errorResponse("MISSING_CONNSTRING", "auth.connection_string ou auth.connstring_ref é obrigatório quando auth.mode é connstring"))
				return
			}
		}
		if req.HostConfigMapRef != nil {
			c.JSON(http.StatusBadRequest, errorResponse("INVALID_HOST_SOURCE", "host_configmap_ref não se aplica quando auth.mode é connstring — a connection string já embute host/porta"))
			return
		}
	} else if req.HostConfigMapRef != nil {
		req.HostConfigMapRef.Namespace = strings.TrimSpace(req.HostConfigMapRef.Namespace)
		req.HostConfigMapRef.Name = strings.TrimSpace(req.HostConfigMapRef.Name)
		if req.HostConfigMapRef.Namespace == "" || req.HostConfigMapRef.Name == "" {
			c.JSON(http.StatusBadRequest, errorResponse("MISSING_CONFIGMAP_REF", "host_configmap_ref precisa de namespace e name"))
			return
		}
	} else if req.Host == "" || req.Port <= 0 {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_HOST_PORT", "host e port são obrigatórios quando auth.mode não é connstring e host_configmap_ref não foi informado"))
		return
	}

	if req.Auth.Mode == "userpass" && req.Auth.SecretRef != nil {
		req.Auth.SecretRef.Namespace = strings.TrimSpace(req.Auth.SecretRef.Namespace)
		req.Auth.SecretRef.Name = strings.TrimSpace(req.Auth.SecretRef.Name)
		if req.Auth.SecretRef.Namespace == "" || req.Auth.SecretRef.Name == "" {
			c.JSON(http.StatusBadRequest, errorResponse("MISSING_SECRET_REF", "secret_ref precisa de namespace e name"))
			return
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
		return
	}

	if req.TimeoutMs <= 0 {
		req.TimeoutMs = dbTestDefaultTimeoutMs
	}
	if req.TimeoutMs > dbTestMaxTimeoutMs {
		req.TimeoutMs = dbTestMaxTimeoutMs
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
			return execLocalDocker(ctx, image, containerName, script)
		}
	} else {
		restConfig, err := h.kubeManager.GetRestConfig(req.Cluster)
		if err != nil {
			fail("falha ao obter configuração do cluster", err)
			return
		}

		send("resolve_deployment", "in_progress", fmt.Sprintf("Localizando pod Running do deployment %q...", req.Deployment), 0.15)
		resolvedPod, targetContainer, err := resolveRunningPodForDeployment(ctx, clientset, req.Namespace, req.Deployment)
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
