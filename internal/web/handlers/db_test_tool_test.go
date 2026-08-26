package handlers

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestResolveDBCredentials_Manual(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	auth := &DBAuthConfig{Username: "u", Password: "p"}

	username, password, err := resolveDBCredentials(context.Background(), clientset, auth)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if username != "u" || password != "p" {
		t.Errorf("got (%q, %q), want (u, p)", username, password)
	}
}

func TestResolveDBCredentials_SecretRaw(t *testing.T) {
	clientset := fake.NewSimpleClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "db-creds", Namespace: "ns"},
		Data: map[string][]byte{
			"username": []byte("meu-usuario"),
			"password": []byte("minha-senha"),
		},
	})
	auth := &DBAuthConfig{
		SecretRef: &DBSecretRef{Namespace: "ns", Name: "db-creds", UsernameKey: "username", PasswordKey: "password"},
	}

	username, password, err := resolveDBCredentials(context.Background(), clientset, auth)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if username != "meu-usuario" || password != "minha-senha" {
		t.Errorf("got (%q, %q), want (meu-usuario, minha-senha)", username, password)
	}
}

func TestResolveDBCredentials_SecretBase64Decode(t *testing.T) {
	// "meu-usuario" e "minha-senha" em base64 — simula valor sincronizado via AKV já
	// codificado em base64 (além do base64 "de transporte" do próprio Secret, que o
	// client-go já decodifica antes de chegar em secret.Data).
	clientset := fake.NewSimpleClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "db-creds", Namespace: "ns"},
		Data: map[string][]byte{
			"username": []byte("bWV1LXVzdWFyaW8="),
			"password": []byte("bWluaGEtc2VuaGE="),
		},
	})
	auth := &DBAuthConfig{
		SecretRef: &DBSecretRef{Namespace: "ns", Name: "db-creds", UsernameKey: "username", PasswordKey: "password", Base64Decode: true},
	}

	username, password, err := resolveDBCredentials(context.Background(), clientset, auth)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if username != "meu-usuario" || password != "minha-senha" {
		t.Errorf("got (%q, %q), want (meu-usuario, minha-senha)", username, password)
	}
}

func TestResolveDBCredentials_SecretBase64DecodeInvalid(t *testing.T) {
	clientset := fake.NewSimpleClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "db-creds", Namespace: "ns"},
		Data: map[string][]byte{
			"username": []byte("não é base64 válido!!"),
			"password": []byte("minha-senha"),
		},
	})
	auth := &DBAuthConfig{
		SecretRef: &DBSecretRef{Namespace: "ns", Name: "db-creds", UsernameKey: "username", PasswordKey: "password", Base64Decode: true},
	}

	_, _, err := resolveDBCredentials(context.Background(), clientset, auth)
	if err == nil {
		t.Fatal("esperava erro pra valor não-base64 com Base64Decode marcado, veio nil")
	}
}

func TestResolveDBCredentials_SecretKeyMissing(t *testing.T) {
	clientset := fake.NewSimpleClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "db-creds", Namespace: "ns"},
		Data: map[string][]byte{
			"username": []byte("u"),
		},
	})
	auth := &DBAuthConfig{
		SecretRef: &DBSecretRef{Namespace: "ns", Name: "db-creds", UsernameKey: "username", PasswordKey: "senha"},
	}

	_, _, err := resolveDBCredentials(context.Background(), clientset, auth)
	if err == nil {
		t.Fatal("esperava erro de chave ausente, veio nil")
	}
}

func TestConnStringDatabase(t *testing.T) {
	cases := map[string]string{
		"postgresql://user:pass@host:5432/mydb?sslmode=require":                    "mydb",
		"mongodb://user:pass@host1,host2,host3/mydb?replicaSet=x&authSource=admin": "mydb",
		"mongodb+srv://user:pass@cluster.mongodb.net/mydb":                         "mydb",
		"mongodb://user:pass@host/?replicaSet=x&authSource=admin":                  "",
		"mongodb://user:pass@host":                                                 "",
		"not a url \x7f":                                                           "",
	}
	for raw, want := range cases {
		if got := connStringDatabase(raw); got != want {
			t.Errorf("connStringDatabase(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestEffectiveDatabase(t *testing.T) {
	t.Run("explicit field wins over connstring", func(t *testing.T) {
		p := dbConnParams{Mode: "connstring", ConnStr: "mongodb://h/embedded", Database: "override"}
		if got := effectiveDatabase(p); got != "override" {
			t.Errorf("got %q, want %q", got, "override")
		}
	})
	t.Run("falls back to connstring database when field empty", func(t *testing.T) {
		p := dbConnParams{Mode: "connstring", ConnStr: "mongodb://h/embedded"}
		if got := effectiveDatabase(p); got != "embedded" {
			t.Errorf("got %q, want %q", got, "embedded")
		}
	})
	t.Run("empty when connstring has no database", func(t *testing.T) {
		p := dbConnParams{Mode: "connstring", ConnStr: "mongodb://h/?authSource=admin"}
		if got := effectiveDatabase(p); got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})
	t.Run("non-connstring mode uses field only", func(t *testing.T) {
		p := dbConnParams{Mode: "userpass", Database: "mydb"}
		if got := effectiveDatabase(p); got != "mydb" {
			t.Errorf("got %q, want %q", got, "mydb")
		}
	})
	t.Run("non-connstring mode with empty field stays empty", func(t *testing.T) {
		p := dbConnParams{Mode: "userpass"}
		if got := effectiveDatabase(p); got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})
}

func TestDbTestObjectNameRegex(t *testing.T) {
	valid := []string{"orders", "permissoes-de-aplicacao-dat", "db_prd_registro_defeitos", "table.name", "Table123"}
	invalid := []string{`tab"le`, "tab le", "tab;le", "tab`le", "tab)le", "", "tab'le"}
	for _, v := range valid {
		if !dbTestObjectNameRegex.MatchString(v) {
			t.Errorf("expected %q to be valid", v)
		}
	}
	for _, v := range invalid {
		if dbTestObjectNameRegex.MatchString(v) {
			t.Errorf("expected %q to be invalid", v)
		}
	}
}

func TestQuoteIdentifiers(t *testing.T) {
	if got := quotePostgresIdentifier(`my"table`); got != `"my""table"` {
		t.Errorf("quotePostgresIdentifier: got %q", got)
	}
	if got := quoteMySQLIdentifier("my`table"); got != "`my``table`" {
		t.Errorf("quoteMySQLIdentifier: got %q", got)
	}
}

func TestParseJSONLinesPreview(t *testing.T) {
	raw := `{"id":1,"name":"a"}
{"id":2,"name":"b"}
malformed line
{"id":3,"name":"c"}`
	rows, err := parseJSONLinesPreview(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("expected 3 rows, got %d: %v", len(rows), rows)
	}
	if rows[1]["name"] != "b" {
		t.Errorf("row[1].name = %v, want b", rows[1]["name"])
	}
}

func TestParseTSVWithHeaderPreview(t *testing.T) {
	raw := "id\tname\temail\n1\talice\ta@x.com\n2\tbob\tb@x.com\n"
	rows, err := parseTSVWithHeaderPreview(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	if rows[0]["name"] != "alice" || rows[1]["email"] != "b@x.com" {
		t.Errorf("unexpected rows: %v", rows)
	}
}

func TestParseTSVWithHeaderPreviewMissingTrailingColumns(t *testing.T) {
	raw := "id\tname\temail\n1\talice\n"
	rows, err := parseTSVWithHeaderPreview(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 1 || rows[0]["email"] != "" {
		t.Errorf("expected missing trailing column to default to empty string, got %v", rows)
	}
}

func TestBuildPreviewPagination(t *testing.T) {
	timeoutSec := 30

	t.Run("postgres with sort", func(t *testing.T) {
		script := dbEngines["postgres"].buildPreview(dbConnParams{Host: "h", Port: 5432, Database: "db"},
			dbPreviewParams{Object: "orders", SortColumn: "created_at", SortDir: "desc", Limit: 20, Offset: 40}, timeoutSec)
		if !strings.Contains(script, `ORDER BY "created_at" DESC`) {
			t.Errorf("missing ORDER BY: %q", script)
		}
		if !strings.Contains(script, "LIMIT 20 OFFSET 40") {
			t.Errorf("missing LIMIT/OFFSET: %q", script)
		}
	})

	t.Run("postgres without sort", func(t *testing.T) {
		script := dbEngines["postgres"].buildPreview(dbConnParams{Host: "h", Port: 5432, Database: "db"},
			dbPreviewParams{Object: "orders", Limit: 20, Offset: 0}, timeoutSec)
		if strings.Contains(script, "ORDER BY") {
			t.Errorf("unexpected ORDER BY: %q", script)
		}
	})

	t.Run("mysql with sort", func(t *testing.T) {
		script := dbEngines["mysql"].buildPreview(dbConnParams{Host: "h", Port: 3306, Database: "db"},
			dbPreviewParams{Object: "orders", SortColumn: "id", SortDir: "asc", Limit: 10, Offset: 5}, timeoutSec)
		if !strings.Contains(script, "ORDER BY `id` ASC") {
			t.Errorf("missing ORDER BY: %q", script)
		}
		if !strings.Contains(script, "LIMIT 10 OFFSET 5") {
			t.Errorf("missing LIMIT/OFFSET: %q", script)
		}
	})

	t.Run("mongo with sort", func(t *testing.T) {
		script := dbEngines["mongodb"].buildPreview(dbConnParams{Host: "h", Port: 27017, Database: "db"},
			dbPreviewParams{Object: "orders", SortColumn: "createdAt", SortDir: "desc", Limit: 20, Offset: 40}, timeoutSec)
		if !strings.Contains(script, `.sort(Object.fromEntries([["createdAt",-1]]))`) {
			t.Errorf("missing sort: %q", script)
		}
		if !strings.Contains(script, ".skip(40).limit(20)") {
			t.Errorf("missing skip/limit: %q", script)
		}
	})

	t.Run("mongo without sort", func(t *testing.T) {
		script := dbEngines["mongodb"].buildPreview(dbConnParams{Host: "h", Port: 27017, Database: "db"},
			dbPreviewParams{Object: "orders", Limit: 20, Offset: 0}, timeoutSec)
		if strings.Contains(script, ".sort(") {
			t.Errorf("unexpected sort: %q", script)
		}
	})

	t.Run("redis list with offset", func(t *testing.T) {
		script := dbEngines["redis"].buildPreview(dbConnParams{Host: "h", Port: 6379},
			dbPreviewParams{Object: "mylist", Limit: 10, Offset: 5}, timeoutSec)
		if !strings.Contains(script, "LRANGE") || !strings.Contains(script, " 5 14 ") {
			t.Errorf("missing paginated LRANGE range: %q", script)
		}
	})
}

func TestParseRedisPreviewMeta(t *testing.T) {
	t.Run("list full page has more", func(t *testing.T) {
		raw := "__DBTEST_KEYTYPE__:list\na\nb\nc\n"
		cleaned, hasMore := parseRedisPreviewMeta(raw, 3)
		if cleaned != "a\nb\nc\n" {
			t.Errorf("cleaned = %q", cleaned)
		}
		if !hasMore {
			t.Error("expected hasMore=true for full page")
		}
	})

	t.Run("list partial page no more", func(t *testing.T) {
		raw := "__DBTEST_KEYTYPE__:list\na\nb\n"
		_, hasMore := parseRedisPreviewMeta(raw, 5)
		if hasMore {
			t.Error("expected hasMore=false for partial page")
		}
	})

	t.Run("zset withscores doubles line count", func(t *testing.T) {
		raw := "__DBTEST_KEYTYPE__:zset\nmember1\n1.5\nmember2\n2.5\n"
		_, hasMore := parseRedisPreviewMeta(raw, 2)
		if !hasMore {
			t.Error("expected hasMore=true for 2 members (4 lines) at limit=2")
		}
	})

	t.Run("hash never has more even if long", func(t *testing.T) {
		raw := "__DBTEST_KEYTYPE__:hash\nf1\nv1\nf2\nv2\nf3\nv3\n"
		_, hasMore := parseRedisPreviewMeta(raw, 1)
		if hasMore {
			t.Error("expected hasMore=false for hash regardless of line count")
		}
	})

	t.Run("no marker leaves raw untouched", func(t *testing.T) {
		raw := `{"a":1}`
		cleaned, hasMore := parseRedisPreviewMeta(raw, 10)
		if cleaned != raw || hasMore {
			t.Errorf("expected passthrough, got cleaned=%q hasMore=%v", cleaned, hasMore)
		}
	})
}

func TestIsValidRedisConnString(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"redis scheme", "redis://:pass@host:6379/0", true},
		{"rediss scheme", "rediss://:pass@host:10000/0", true},
		{"uppercase scheme", "REDISS://:pass@host:10000/0", true},
		{"empty", "", false},
		{"azure redis-cli snippet", "-p 10000 -h mycache.redis.azure.net -a MyAccessKey --tls", false},
		{"bare host no scheme", "mycache.redis.azure.net:10000", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isValidRedisConnString(tc.in); got != tc.want {
				t.Errorf("isValidRedisConnString(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestRedisConnStringHint(t *testing.T) {
	t.Run("redis-cli snippet gets specific hint", func(t *testing.T) {
		hint := redisConnStringHint("-p 10000 -h mycache.redis.azure.net -a MyAccessKey --tls")
		if !strings.Contains(hint, "redis-cli") {
			t.Errorf("expected hint to mention redis-cli, got %q", hint)
		}
	})

	t.Run("generic invalid string gets generic hint", func(t *testing.T) {
		hint := redisConnStringHint("mycache.redis.azure.net:10000")
		if !strings.Contains(hint, "redis://") || !strings.Contains(hint, "rediss://") {
			t.Errorf("expected hint to mention redis://  and rediss://, got %q", hint)
		}
	})
}

// Regressão: usuário relatou `psql: error: invalid integer value
// "sqlserver://sqlp-cargadireta.database.windows.net:5432" for connection option "port"` ao
// testar um banco Postgres — connection string com esquema sqlserver:// (copiada por engano do
// banco/engine errado) sendo repassada crua pro psql, sem nenhuma validação prévia de esquema
// (diferente do Redis, que já tinha essa proteção). validateDBConnStringScheme cobre agora
// postgres/mongodb/mysql com o mesmo padrão.
func TestValidateDBConnStringScheme(t *testing.T) {
	cases := []struct {
		name       string
		engine     string
		in         string
		wantCode   string
		hintSubstr string
	}{
		{"postgres valid postgresql://", "postgres", "postgresql://user:pass@host:5432/db", "", ""},
		{"postgres valid postgres://", "postgres", "postgres://user:pass@host:5432/db", "", ""},
		{"postgres wrong scheme sqlserver:// (bug real relatado)", "postgres",
			"sqlserver://sqlp-cargadireta.database.windows.net:5432", "INVALID_POSTGRES_CONNSTRING", `"sqlserver://"`},
		{"mongodb valid", "mongodb", "mongodb://user:pass@host:27017/db", "", ""},
		{"mongodb valid +srv", "mongodb", "mongodb+srv://user:pass@host/db", "", ""},
		{"mongodb wrong scheme", "mongodb", "postgresql://host:5432/db", "INVALID_MONGO_CONNSTRING", `"postgresql://"`},
		{"mysql valid", "mysql", "mysql://user:pass@host:3306/db", "", ""},
		{"mysql wrong scheme", "mysql", "sqlserver://host:1433", "INVALID_MYSQL_CONNSTRING", `"sqlserver://"`},
		{"redis still works via shared dispatcher", "redis", "redis://:pass@host:6379/0", "", ""},
		{"engine sem checagem de esquema não bloqueia", "unknown-engine", "qualquer coisa", "", ""},
		// Engine sqlserver adicionado depois do fix acima — a mesma string do relato do usuário
		// (que corretamente falhava sob o engine "postgres") agora é VÁLIDA sob o engine
		// "sqlserver", que é o que o usuário realmente precisava usar.
		{"sqlserver valid sqlserver:// — string real do usuário", "sqlserver",
			"sqlserver://sqlp-cargadireta.database.windows.net:5432", "", ""},
		{"sqlserver valid mssql://", "sqlserver", "mssql://user:pass@host:1433/db", "", ""},
		{"sqlserver valid jdbc:sqlserver:// (relatado ao vivo pelo usuário)", "sqlserver",
			"jdbc:sqlserver://host.database.windows.net:1433;database=db;user=u;password=p;encrypt=true", "", ""},
		{"sqlserver wrong scheme", "sqlserver", "postgresql://host:5432/db", "INVALID_SQLSERVER_CONNSTRING", `"postgresql://"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, hint := validateDBConnStringScheme(tc.engine, tc.in)
			if code != tc.wantCode {
				t.Errorf("code = %q, want %q (hint=%q)", code, tc.wantCode, hint)
			}
			if tc.hintSubstr != "" && !strings.Contains(hint, tc.hintSubstr) {
				t.Errorf("hint = %q, want it to contain %q", hint, tc.hintSubstr)
			}
		})
	}
}

// Regressão: `timeout Ns VAR=valor cmd` NÃO funciona como atribuição de variável de ambiente —
// confirmado ao vivo contra um SQL Server/MariaDB reais (`timeout: failed to run command
// 'VAR=valor': No such file or directory`). O prefixo de senha (MYSQL_PWD/SQLCMDPASSWORD)
// precisa vir depois de um `env` explícito pra funcionar nessa posição.
func TestEnvVarPrefix(t *testing.T) {
	if got := envVarPrefix("MYSQL_PWD", ""); got != "" {
		t.Errorf("prefixo vazio esperado sem senha, got %q", got)
	}
	got := envVarPrefix("MYSQL_PWD", "s3cret")
	if !strings.HasPrefix(got, "env ") {
		t.Errorf("prefixo deve começar com \"env \" — sem isso, `timeout Ns VAR=val cmd` trata "+
			"VAR=val como o próprio comando a executar, não como atribuição de ambiente; got %q", got)
	}
	if !strings.Contains(got, "MYSQL_PWD=") {
		t.Errorf("prefixo deve conter a variável, got %q", got)
	}
}

func TestParseSQLServerConnString(t *testing.T) {
	t.Run("string real relatada pelo usuário — sem usuário/senha/database", func(t *testing.T) {
		host, port, user, pass, db := parseSQLServerConnString("sqlserver://sqlp-cargadireta.database.windows.net:5432")
		if host != "sqlp-cargadireta.database.windows.net" || port != 5432 || user != "" || pass != "" || db != "" {
			t.Errorf("got (%q, %d, %q, %q, %q)", host, port, user, pass, db)
		}
	})

	t.Run("completa com usuário/senha/database", func(t *testing.T) {
		host, port, user, pass, db := parseSQLServerConnString("sqlserver://sa:MyP%40ss@myserver.database.windows.net:1433/mydb")
		if host != "myserver.database.windows.net" || port != 1433 || user != "sa" || pass != "MyP@ss" || db != "mydb" {
			t.Errorf("got (%q, %d, %q, %q, %q)", host, port, user, pass, db)
		}
	})

	t.Run("sem porta explícita cai no default 1433", func(t *testing.T) {
		_, port, _, _, _ := parseSQLServerConnString("sqlserver://myserver.database.windows.net/mydb")
		if port != sqlserverDefaultPort {
			t.Errorf("port = %d, want %d", port, sqlserverDefaultPort)
		}
	})
}

// Regressão: usuário relatou usar exatamente esse formato (driver JDBC da Microsoft, comum em
// config de app Java) — connection string em formato URI simples (o único suportado até então)
// não cobria isso, gerando confusão. Formato/shape reproduzido a partir do relato real (valores
// sintéticos, não os originais).
func TestParseJDBCSQLServerParams(t *testing.T) {
	raw := "jdbc:sqlserver://sqlp-cargadireta.database.windows.net:1433;database=db_prd_plano;" +
		"user=svc-cabecalho-i-01@sqlp-cargadireta;password=Qc1234#56(ntJabcw123t;encrypt=true;" +
		"trustServerCertificate=false;hostNameInCertificate=*.database.windows.net;loginTimeout=30"

	host, port, user, pass, db, useTLS, skipTLSVerify, ok := parseJDBCSQLServerParams(raw)
	if !ok {
		t.Fatal("esperava ok=true pra uma string jdbc:sqlserver:// válida")
	}
	if host != "sqlp-cargadireta.database.windows.net" {
		t.Errorf("host = %q", host)
	}
	if port != 1433 {
		t.Errorf("port = %d, want 1433", port)
	}
	if user != "svc-cabecalho-i-01@sqlp-cargadireta" {
		t.Errorf("user = %q — o '@' faz parte do login (formato user@servername do Azure SQL), não deve ser cortado", user)
	}
	if pass != "Qc1234#56(ntJabcw123t" {
		t.Errorf("pass = %q — caracteres especiais (#, () não devem quebrar o parse", pass)
	}
	if db != "db_prd_plano" {
		t.Errorf("db = %q", db)
	}
	if !useTLS {
		t.Error("useTLS = false, want true (encrypt=true na string)")
	}
	if skipTLSVerify {
		t.Error("skipTLSVerify = true, want false (trustServerCertificate=false na string)")
	}

	t.Run("chave case-insensitive e alias databaseName/username", func(t *testing.T) {
		host, _, user, _, db, _, _, ok := parseJDBCSQLServerParams(
			"jdbc:sqlserver://host:1433;DatabaseName=xyz;UserName=abc;Encrypt=TRUE")
		if !ok || host != "host" || db != "xyz" || user != "abc" {
			t.Errorf("got host=%q db=%q user=%q ok=%v", host, db, user, ok)
		}
	})

	t.Run("sem porta explícita cai no default", func(t *testing.T) {
		_, port, _, _, _, _, _, ok := parseJDBCSQLServerParams("jdbc:sqlserver://host;database=x")
		if !ok || port != sqlserverDefaultPort {
			t.Errorf("port = %d, ok=%v", port, ok)
		}
	})

	t.Run("string não-jdbc devolve ok=false", func(t *testing.T) {
		if _, _, _, _, _, _, _, ok := parseJDBCSQLServerParams("sqlserver://host:1433/db"); ok {
			t.Error("esperava ok=false pra uma string que não começa com jdbc:sqlserver://")
		}
	})

	t.Run("sqlserverEffectiveParams usa o parser certo em Mode=connstring", func(t *testing.T) {
		p := dbConnParams{Mode: "connstring", ConnStr: raw}
		h, po, u, pw, d, tls, trust := sqlserverEffectiveParams(p)
		if h != host || po != port || u != user || pw != pass || d != db || tls != useTLS || trust != skipTLSVerify {
			t.Errorf("sqlserverEffectiveParams não bateu com parseJDBCSQLServerParams direto")
		}
	})
}

// redisInfoFixture reproduz o formato REAL de `redis-cli INFO` (CRLF entre linhas, cabeçalhos de
// seção "# Nome"), capturado contra um redis:7-alpine real — só os campos usados por
// parseRedisServerInfo, pra manter o teste legível sem as ~200 linhas da saída completa.
const redisInfoFixture = "# Server\r\n" +
	"redis_version:7.4.10\r\n" +
	"redis_mode:standalone\r\n" +
	"os:Linux 6.18.33.2\r\n" +
	"\r\n" +
	"# Clients\r\n" +
	"connected_clients:3\r\n" +
	"\r\n" +
	"# Memory\r\n" +
	"used_memory_human:965.91K\r\n" +
	"maxmemory_human:0B\r\n" +
	"\r\n" +
	"# Stats\r\n" +
	"keyspace_hits:842\r\n" +
	"keyspace_misses:158\r\n" +
	"instantaneous_ops_per_sec:5\r\n" +
	"total_reads_processed:2000\r\n" +
	"total_writes_processed:500\r\n" +
	"\r\n" +
	"# Replication\r\n" +
	"role:master\r\n" +
	"\r\n" +
	"# Commandstats\r\n" +
	"cmdstat_set:calls=5,usec=100,usec_per_call=20.00,rejected_calls=0,failed_calls=0\r\n" +
	"cmdstat_get:calls=20,usec=100,usec_per_call=5.00,rejected_calls=0,failed_calls=0\r\n" +
	"\r\n" +
	"# Keyspace\r\n" +
	"db0:keys=12,expires=3,avg_ttl=0\r\n"

func TestParseRedisInfoSections(t *testing.T) {
	t.Run("agrupa por seção na ordem original (CRLF)", func(t *testing.T) {
		sections := parseRedisInfoSections(redisInfoFixture)
		wantNames := []string{"Server", "Clients", "Memory", "Stats", "Replication", "Commandstats", "Keyspace"}
		if len(sections) != len(wantNames) {
			t.Fatalf("got %d seções, want %d (%+v)", len(sections), len(wantNames), sections)
		}
		for i, name := range wantNames {
			if sections[i].Name != name {
				t.Errorf("sections[%d].Name = %q, want %q", i, sections[i].Name, name)
			}
		}
	})

	t.Run("campos preservam key e value crus", func(t *testing.T) {
		sections := parseRedisInfoSections(redisInfoFixture)
		server := sections[0]
		if server.Name != "Server" {
			t.Fatalf("esperava seção Server primeiro, got %q", server.Name)
		}
		found := false
		for _, f := range server.Fields {
			if f.Key == "redis_version" {
				found = true
				if f.Value != "7.4.10" {
					t.Errorf("redis_version = %q, want 7.4.10", f.Value)
				}
			}
		}
		if !found {
			t.Error("campo redis_version não encontrado na seção Server")
		}
	})

	t.Run("saida vazia ou sem cabecalho de secao devolve nil", func(t *testing.T) {
		if got := parseRedisInfoSections(""); got != nil {
			t.Errorf("esperava nil, got %+v", got)
		}
		if got := parseRedisInfoSections("NOAUTH Authentication required.\r\n"); got != nil {
			t.Errorf("esperava nil pra saída de erro sem cabeçalho de seção, got %+v", got)
		}
	})
}

func TestParseRedisServerInfo(t *testing.T) {
	t.Run("extrai campos reais do INFO (CRLF)", func(t *testing.T) {
		info := parseRedisServerInfo(redisInfoFixture)
		if info == nil {
			t.Fatal("esperava *RedisServerInfo não-nil")
		}
		if info.Version != "7.4.10" {
			t.Errorf("Version = %q, want 7.4.10", info.Version)
		}
		if info.Mode != "standalone" {
			t.Errorf("Mode = %q, want standalone", info.Mode)
		}
		if info.Role != "master" {
			t.Errorf("Role = %q, want master", info.Role)
		}
		if info.ConnectedClients != 3 {
			t.Errorf("ConnectedClients = %d, want 3", info.ConnectedClients)
		}
		if info.UsedMemoryHuman != "965.91K" {
			t.Errorf("UsedMemoryHuman = %q, want 965.91K", info.UsedMemoryHuman)
		}
		if info.KeyspaceHits != 842 || info.KeyspaceMisses != 158 {
			t.Errorf("hits/misses = %d/%d, want 842/158", info.KeyspaceHits, info.KeyspaceMisses)
		}
		wantHitRate := 842.0 / (842.0 + 158.0) * 100
		if info.HitRatePct != wantHitRate {
			t.Errorf("HitRatePct = %v, want %v", info.HitRatePct, wantHitRate)
		}
		if info.InstantaneousOpsPerSec != 5 {
			t.Errorf("InstantaneousOpsPerSec = %d, want 5", info.InstantaneousOpsPerSec)
		}
		if info.TotalReadsProcessed != 2000 || info.TotalWritesProcessed != 500 {
			t.Errorf("reads/writes = %d/%d, want 2000/500", info.TotalReadsProcessed, info.TotalWritesProcessed)
		}
		wantReadPct := 2000.0 / (2000.0 + 500.0) * 100
		if info.ReadPct != wantReadPct {
			t.Errorf("ReadPct = %v, want %v", info.ReadPct, wantReadPct)
		}
		if info.ReadPct+info.WritePct != 100 {
			t.Errorf("ReadPct+WritePct = %v, want 100", info.ReadPct+info.WritePct)
		}
		wantAvgLatency := (100.0 + 100.0) / (5.0 + 20.0) / 1000
		if info.AvgLatencyMs != wantAvgLatency {
			t.Errorf("AvgLatencyMs = %v, want %v", info.AvgLatencyMs, wantAvgLatency)
		}
		if info.SlowestCommand != "set" {
			t.Errorf("SlowestCommand = %q, want %q", info.SlowestCommand, "set")
		}
		if info.SlowestCommandLatencyMs != 0.02 {
			t.Errorf("SlowestCommandLatencyMs = %v, want 0.02", info.SlowestCommandLatencyMs)
		}
	})

	t.Run("hits e misses zerados vira -1 (sem dados), não 0%", func(t *testing.T) {
		raw := "redis_version:7.4.10\r\nkeyspace_hits:0\r\nkeyspace_misses:0\r\n"
		info := parseRedisServerInfo(raw)
		if info.HitRatePct != -1 {
			t.Errorf("HitRatePct = %v, want -1 (sem dados)", info.HitRatePct)
		}
	})

	t.Run("reads e writes zerados vira -1 (sem dados), não 0%", func(t *testing.T) {
		raw := "redis_version:7.4.10\r\ntotal_reads_processed:0\r\ntotal_writes_processed:0\r\n"
		info := parseRedisServerInfo(raw)
		if info.ReadPct != -1 || info.WritePct != -1 {
			t.Errorf("ReadPct/WritePct = %v/%v, want -1/-1 (sem dados)", info.ReadPct, info.WritePct)
		}
	})

	t.Run("saída vazia ou sem campos reconhecidos devolve nil", func(t *testing.T) {
		if info := parseRedisServerInfo(""); info != nil {
			t.Errorf("esperava nil pra saída vazia, got %+v", info)
		}
		if info := parseRedisServerInfo("NOAUTH Authentication required.\r\n"); info != nil {
			t.Errorf("esperava nil pra saída de erro, got %+v", info)
		}
	})
}

func TestParseRedisCommandStats(t *testing.T) {
	t.Run("media ponderada por chamada, nao media simples dos usec_per_call", func(t *testing.T) {
		// set: 5 chamadas, 100 usec total (20 usec/call) — get: 20 chamadas, 400 usec total (20
		// usec/call também, mas MUITO mais volume). Uma média simples dos usec_per_call (20 e 20)
		// daria 20 de qualquer forma aqui — troca por valores diferentes pra realmente provar que
		// é ponderado: set MUITO mais lento (usec_per_call alto) mas raro não deve dominar a média
		// geral, que é por CHAMADA, não por comando.
		fields := map[string]string{
			"cmdstat_set": "calls=1,usec=1000,usec_per_call=1000.00,rejected_calls=0,failed_calls=0",
			"cmdstat_get": "calls=999,usec=999,usec_per_call=1.00,rejected_calls=0,failed_calls=0",
		}
		avg, slowest, slowestMs := parseRedisCommandStats(fields)
		wantAvg := (1000.0 + 999.0) / (1.0 + 999.0) / 1000 // ~0.002ms — dominado pelo volume do get
		if avg != wantAvg {
			t.Errorf("avg = %v, want %v", avg, wantAvg)
		}
		if slowest != "set" {
			t.Errorf("slowest = %q, want %q (maior usec_per_call, mesmo sendo raro)", slowest, "set")
		}
		if slowestMs != 1.0 {
			t.Errorf("slowestMs = %v, want 1.0", slowestMs)
		}
	})

	t.Run("sem nenhum cmdstat_ devolve -1", func(t *testing.T) {
		avg, slowest, slowestMs := parseRedisCommandStats(map[string]string{"redis_version": "7.4.10"})
		if avg != -1 || slowest != "" || slowestMs != -1 {
			t.Errorf("got avg=%v slowest=%q slowestMs=%v, want -1/\"\"/-1", avg, slowest, slowestMs)
		}
	})

	t.Run("mapa vazio devolve -1", func(t *testing.T) {
		avg, _, slowestMs := parseRedisCommandStats(map[string]string{})
		if avg != -1 || slowestMs != -1 {
			t.Errorf("got avg=%v slowestMs=%v, want -1/-1", avg, slowestMs)
		}
	})
}

func TestBuildMongoURIAuthMechanism(t *testing.T) {
	t.Run("no mechanism omits query param", func(t *testing.T) {
		got := buildMongoURI(dbConnParams{Host: "h", Port: 27017, Username: "u", Password: "p"})
		if strings.Contains(got, "authMechanism") {
			t.Errorf("expected no authMechanism param, got %q", got)
		}
	})
	t.Run("mechanism set adds query param", func(t *testing.T) {
		got := buildMongoURI(dbConnParams{Host: "h", Port: 27017, Username: "u", Password: "p", AuthMechanism: "SCRAM-SHA-256"})
		if !strings.Contains(got, "authMechanism=SCRAM-SHA-256") {
			t.Errorf("expected authMechanism=SCRAM-SHA-256 in URI, got %q", got)
		}
	})
}
