package storage

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"k8s-hpa-manager/internal/dbfile"

	_ "github.com/mattn/go-sqlite3"
)

// HTTPClientExecutionsKeep é quantas execuções cada usuário mantém; as mais antigas saem a cada
// inclusão (é uma lista de "últimas requisições", não auditoria — essa fica no Change History).
const HTTPClientExecutionsKeep = 200

// HTTPClientExecution é uma execução do Cliente HTTP. Request guarda a requisição inteira (JSON do
// body do /send, com headers e corpo) para poder re-executar; da resposta, só o resumo — o corpo
// pode ser grande e é refeito ao re-executar.
type HTTPClientExecution struct {
	ID            int64     `json:"id"`
	UserEmail     string    `json:"user_email"`
	CreatedAt     time.Time `json:"created_at"`
	Request       string    `json:"request"`
	Method        string    `json:"method"`
	URL           string    `json:"url"`
	ExecutionMode string    `json:"execution_mode"`
	Cluster       string    `json:"cluster,omitempty"`
	Status        int       `json:"status,omitempty"`
	StatusText    string    `json:"status_text,omitempty"`
	DurationMs    int64     `json:"duration_ms"`
	SizeBytes     int64     `json:"size_bytes"`
	ExecutedFrom  string    `json:"executed_from,omitempty"`
	Error         string    `json:"error,omitempty"`
}

// HTTPClientExecutionStore persiste as execuções do Cliente HTTP em
// ~/.k8s-hpa-manager/http_client_executions.db (0600: headers podem ter tokens).
type HTTPClientExecutionStore struct {
	db  *sql.DB
	key []byte // AES-256: criptografa a coluna request (headers e corpo, que carregam tokens)
}

// httpClientRequestPrefix marca a coluna request já criptografada. Linhas sem o prefixo são do
// formato antigo (JSON em texto puro) e são migradas ao abrir o banco.
const httpClientRequestPrefix = "enc:v1:"

// httpClientURLRe acha URLs dentro de um texto (mensagens de erro de rede do Go incluem a URL inteira,
// com a query string).
var httpClientURLRe = regexp.MustCompile(`https?://[^\s"'<>)]+`)

// redactURL devolve a URL sem credenciais (user:senha@), sem query string e sem fragmento: query
// costuma carregar token (?api_key=...). Com query, termina em "?…" para deixar claro que existia.
// O texto que não parece uma URL volta igual.
func redactURL(raw string) string {
	if !strings.Contains(raw, "://") {
		return raw
	}
	rest := raw
	if i := strings.IndexByte(rest, '#'); i >= 0 {
		rest = rest[:i]
	}
	hadQuery := false
	if i := strings.IndexByte(rest, '?'); i >= 0 {
		rest, hadQuery = rest[:i], true
	}
	if i := strings.Index(rest, "://"); i >= 0 {
		scheme, after := rest[:i+3], rest[i+3:]
		hostEnd := strings.IndexByte(after, '/')
		authority, tail := after, ""
		if hostEnd >= 0 {
			authority, tail = after[:hostEnd], after[hostEnd:]
		}
		if at := strings.LastIndexByte(authority, '@'); at >= 0 {
			authority = authority[at+1:]
		}
		rest = scheme + authority + tail
	}
	if hadQuery {
		rest += "?…"
	}
	return rest
}

// redactURLsInText aplica redactURL a cada URL encontrada em um texto livre.
func redactURLsInText(s string) string {
	return httpClientURLRe.ReplaceAllStringFunc(s, redactURL)
}

func (s *HTTPClientExecutionStore) encryptRequest(plain string) (string, error) {
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	return httpClientRequestPrefix + base64.StdEncoding.EncodeToString(gcm.Seal(nonce, nonce, []byte(plain), nil)), nil
}

// decryptRequest decifra a coluna request. Texto sem o prefixo é o formato antigo e volta igual.
func (s *HTTPClientExecutionStore) decryptRequest(stored string) (string, error) {
	if !strings.HasPrefix(stored, httpClientRequestPrefix) {
		return stored, nil
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(stored, httpClientRequestPrefix))
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", fmt.Errorf("conteúdo cifrado curto demais")
	}
	plain, err := gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

// migrateLegacy criptografa as linhas gravadas em texto puro por versões anteriores e tira a query
// string e as credenciais da URL e do erro. Idempotente: linhas já migradas não são tocadas.
func (s *HTTPClientExecutionStore) migrateLegacy() error {
	rows, err := s.db.Query(`SELECT id, request, url, error FROM http_client_executions
		WHERE request NOT LIKE ? OR url LIKE '%?%' OR url LIKE '%@%' OR error LIKE '%?%'`, httpClientRequestPrefix+"%")
	if err != nil {
		return err
	}
	type linha struct {
		id                 int64
		req, urlCol, errTx string
	}
	var pend []linha
	for rows.Next() {
		var l linha
		if err := rows.Scan(&l.id, &l.req, &l.urlCol, &l.errTx); err != nil {
			rows.Close()
			return err
		}
		pend = append(pend, l)
	}
	rows.Close()
	if len(pend) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	for _, l := range pend {
		req := l.req
		if !strings.HasPrefix(req, httpClientRequestPrefix) {
			if req, err = s.encryptRequest(req); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(`UPDATE http_client_executions SET request = ?, url = ?, error = ? WHERE id = ?`,
			req, redactURL(l.urlCol), redactURLsInText(l.errTx), l.id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

const httpClientExecutionsSchema = `
CREATE TABLE IF NOT EXISTS http_client_executions (
    id             INTEGER  PRIMARY KEY AUTOINCREMENT,
    user_email     TEXT     NOT NULL DEFAULT '',
    created_at     DATETIME NOT NULL,
    request        TEXT     NOT NULL,
    method         TEXT     NOT NULL,
    url            TEXT     NOT NULL,
    execution_mode TEXT     NOT NULL DEFAULT 'server',
    cluster        TEXT     NOT NULL DEFAULT '',
    status         INTEGER  NOT NULL DEFAULT 0,
    status_text    TEXT     NOT NULL DEFAULT '',
    duration_ms    INTEGER  NOT NULL DEFAULT 0,
    size_bytes     INTEGER  NOT NULL DEFAULT 0,
    executed_from  TEXT     NOT NULL DEFAULT '',
    error          TEXT     NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_http_client_executions_user ON http_client_executions(user_email, id DESC);
`

func NewHTTPClientExecutionStore(dbPath string) (*HTTPClientExecutionStore, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0700); err != nil {
		return nil, fmt.Errorf("criar diretório http client executions: %w", err)
	}
	dbfile.Restrict(dbPath) // 0600: headers podem ter tokens
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("abrir http_client_executions.db: %w", err)
	}
	db.SetMaxOpenConns(3)
	db.SetMaxIdleConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping http_client_executions.db: %w", err)
	}
	if _, err := db.Exec(httpClientExecutionsSchema); err != nil {
		db.Close()
		return nil, fmt.Errorf("criar schema http_client_executions: %w", err)
	}
	key, err := getOrCreateEncryptionKey(filepath.Dir(dbPath))
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("chave de criptografia do cliente http: %w", err)
	}
	store := &HTTPClientExecutionStore{db: db, key: key}
	if err := store.migrateLegacy(); err != nil {
		// A migração é melhor esforço: o store continua funcionando (linhas antigas ficam legíveis).
		fmt.Printf("⚠️  Cliente HTTP: não foi possível proteger execuções antigas: %v\n", err)
	}
	return store, nil
}

func (s *HTTPClientExecutionStore) Close() error {
	return s.db.Close()
}

// Add grava a execução e descarta as do mesmo usuário além das HTTPClientExecutionsKeep mais recentes.
func (s *HTTPClientExecutionStore) Add(e *HTTPClientExecution) error {
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now()
	}
	enc, err := s.encryptRequest(e.Request)
	if err != nil {
		return fmt.Errorf("proteger execução do cliente http: %w", err)
	}
	res, err := s.db.Exec(
		`INSERT INTO http_client_executions (user_email, created_at, request, method, url, execution_mode, cluster,
			status, status_text, duration_ms, size_bytes, executed_from, error) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		e.UserEmail, e.CreatedAt.UTC(), enc, e.Method, redactURL(e.URL), e.ExecutionMode, e.Cluster,
		e.Status, e.StatusText, e.DurationMs, e.SizeBytes, e.ExecutedFrom, redactURLsInText(e.Error),
	)
	if err != nil {
		return fmt.Errorf("gravar execução do cliente http: %w", err)
	}
	e.ID, _ = res.LastInsertId()
	_, err = s.db.Exec(
		`DELETE FROM http_client_executions WHERE user_email = ? AND id NOT IN (
			SELECT id FROM http_client_executions WHERE user_email = ? ORDER BY id DESC LIMIT ?)`,
		e.UserEmail, e.UserEmail, HTTPClientExecutionsKeep,
	)
	return err
}

// List devolve as execuções do usuário, mais recentes primeiro.
func (s *HTTPClientExecutionStore) List(userEmail string, limit int) ([]HTTPClientExecution, error) {
	if limit <= 0 || limit > HTTPClientExecutionsKeep {
		limit = HTTPClientExecutionsKeep
	}
	rows, err := s.db.Query(
		`SELECT id, user_email, created_at, request, method, url, execution_mode, cluster,
			status, status_text, duration_ms, size_bytes, executed_from, error
		 FROM http_client_executions WHERE user_email = ? ORDER BY id DESC LIMIT ?`,
		userEmail, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("listar execuções do cliente http: %w", err)
	}
	defer rows.Close()

	out := []HTTPClientExecution{}
	for rows.Next() {
		var e HTTPClientExecution
		if err := rows.Scan(&e.ID, &e.UserEmail, &e.CreatedAt, &e.Request, &e.Method, &e.URL, &e.ExecutionMode, &e.Cluster,
			&e.Status, &e.StatusText, &e.DurationMs, &e.SizeBytes, &e.ExecutedFrom, &e.Error); err != nil {
			return nil, err
		}
		// Sem chave que decifre (ex.: .secret trocado), a linha sai com request vazio e o handler a omite.
		if plain, derr := s.decryptRequest(e.Request); derr == nil {
			e.Request = plain
		} else {
			e.Request = ""
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// Delete remove uma execução do próprio usuário. Devolve false se não existia (ou é de outro usuário).
func (s *HTTPClientExecutionStore) Delete(userEmail string, id int64) (bool, error) {
	res, err := s.db.Exec(`DELETE FROM http_client_executions WHERE id = ? AND user_email = ?`, id, userEmail)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}
