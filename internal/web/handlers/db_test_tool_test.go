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
