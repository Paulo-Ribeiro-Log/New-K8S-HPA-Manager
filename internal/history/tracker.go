package history

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
)

// HistoryEntry representa uma entrada no histórico de alterações (Change History).
type HistoryEntry struct {
	ID          string                 `json:"id"`
	Timestamp   time.Time              `json:"timestamp"`
	UserEmail   string                 `json:"user_email,omitempty"` // Email do usuário (JWT do login)
	UserName    string                 `json:"user_name,omitempty"`  // Nome do usuário (JWT do login)
	Action      string                 `json:"action"`               // update_hpa, apply_nodepool, etc
	Resource    string                 `json:"resource"`             // namespace/name
	Cluster     string                 `json:"cluster"`
	Before      map[string]interface{} `json:"before"`
	After       map[string]interface{} `json:"after"`
	Status      string                 `json:"status"` // success, failed, partial
	ErrorMsg    string                 `json:"error_msg,omitempty"`
	Duration    int64                  `json:"duration_ms"` // Tempo de execução em ms
	SessionName string                 `json:"session_name,omitempty"`
}

// RetentionDays é por quanto tempo uma entrada fica no histórico. Antes disso ela não pode ser
// apagada nem alterada — o trigger do schema recusa (defesa contra bug, não contra quem edita o
// arquivo do banco à mão; para isso valem as permissões 0600/0700).
const RetentionDays = 365

// retentionDaysSQL é RetentionDays dentro do trigger (const string — o schema é uma const).
// Mudou um, mude o outro: o teste TestRetentionTriggerMatchesConst confere.
const retentionDaysSQL = "365"

// HistoryTracker grava e consulta o histórico em SQLite (~/.k8s-hpa-manager/history.db).
// Antes eram arquivos JSON soltos em history/AAAA-MM/ (com só as últimas 1000 entradas visíveis,
// gravação assíncrona e "Limpar" que não apagava o disco); eles são importados uma vez e a pasta
// vira history-json-backup/.
//
// Todos os métodos aceitam receiver nil (driver SQLite indisponível): não gravam e não devolvem nada.
type HistoryTracker struct {
	db *sql.DB
}

const historySchema = `
CREATE TABLE IF NOT EXISTS history_entries (
    id           TEXT    PRIMARY KEY,
    ts           INTEGER NOT NULL,
    user_email   TEXT    NOT NULL DEFAULT '',
    user_name    TEXT    NOT NULL DEFAULT '',
    action       TEXT    NOT NULL DEFAULT '',
    resource     TEXT    NOT NULL DEFAULT '',
    cluster      TEXT    NOT NULL DEFAULT '',
    status       TEXT    NOT NULL DEFAULT '',
    error_msg    TEXT    NOT NULL DEFAULT '',
    duration_ms  INTEGER NOT NULL DEFAULT 0,
    session_name TEXT    NOT NULL DEFAULT '',
    before_json  TEXT,
    after_json   TEXT
);
CREATE INDEX IF NOT EXISTS idx_history_ts      ON history_entries(ts DESC);
CREATE INDEX IF NOT EXISTS idx_history_action  ON history_entries(action, ts DESC);
CREATE INDEX IF NOT EXISTS idx_history_cluster ON history_entries(cluster, ts DESC);
CREATE INDEX IF NOT EXISTS idx_history_user    ON history_entries(user_email, ts DESC);

CREATE TABLE IF NOT EXISTS history_meta (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

CREATE TRIGGER IF NOT EXISTS history_no_update
BEFORE UPDATE ON history_entries
BEGIN
    SELECT RAISE(ABORT, 'histórico é somente-inclusão');
END;

CREATE TRIGGER IF NOT EXISTS history_no_early_delete
BEFORE DELETE ON history_entries
WHEN old.ts > (CAST(strftime('%s','now') AS INTEGER) - ` + retentionDaysSQL + ` * 86400) * 1000
BEGIN
    SELECT RAISE(ABORT, 'entrada do histórico dentro do período de retenção');
END;
`

// NewHistoryTracker abre (ou cria) o banco do histórico em baseDir/history.db e importa o
// histórico JSON antigo de baseDir/history/ na primeira execução.
func NewHistoryTracker(baseDir string) (*HistoryTracker, error) {
	if err := os.MkdirAll(baseDir, 0700); err != nil {
		return nil, fmt.Errorf("criar diretório do histórico: %w", err)
	}
	dbPath := filepath.Join(baseDir, "history.db")

	// Cria o arquivo já com 0600: o SQLite herda o modo do banco para -wal/-shm.
	if f, err := os.OpenFile(dbPath, os.O_CREATE|os.O_RDWR, 0600); err == nil {
		f.Close()
	}
	_ = os.Chmod(dbPath, 0600)

	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000&_synchronous=NORMAL")
	if err != nil {
		return nil, fmt.Errorf("abrir history.db: %w", err)
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping history.db: %w", err)
	}
	if _, err := db.Exec(historySchema); err != nil {
		db.Close()
		return nil, fmt.Errorf("criar schema do histórico: %w", err)
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		_ = os.Chmod(dbPath+suffix, 0600)
	}

	ht := &HistoryTracker{db: db}
	if err := ht.importLegacyJSON(filepath.Join(baseDir, "history"), filepath.Join(baseDir, "history-json-backup")); err != nil {
		fmt.Printf("⚠️  Histórico: falha ao importar JSON antigo: %v\n", err)
	}
	if n, err := ht.PurgeExpired(); err != nil {
		fmt.Printf("⚠️  Histórico: falha na retenção: %v\n", err)
	} else if n > 0 {
		fmt.Printf("🧹 Histórico: %d entrada(s) com mais de %d dias removida(s)\n", n, RetentionDays)
	}
	return ht, nil
}

// Close fecha o banco.
func (ht *HistoryTracker) Close() error {
	if ht == nil {
		return nil
	}
	return ht.db.Close()
}

// NormalizeStatus unifica os status gravados por handlers diferentes ("error" virou "failed").
func NormalizeStatus(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "":
		return ""
	case StatusSuccess, "ok":
		return StatusSuccess
	case StatusFailed, "error", "failure", "fail":
		return StatusFailed
	case StatusPartial:
		return StatusPartial
	default:
		return s
	}
}

// Log grava uma entrada de forma síncrona. O autor vem da entrada ou, se vazio, do ctx
// (*gin.Context da requisição, c.Request.Context() ou DetachedUserContext em goroutines).
func (ht *HistoryTracker) Log(ctx context.Context, entry HistoryEntry) error {
	if ht == nil {
		return nil
	}
	if entry.UserEmail == "" || entry.UserName == "" {
		u := UserFromContext(ctx)
		if entry.UserEmail == "" {
			entry.UserEmail = u.Email
		}
		if entry.UserName == "" {
			entry.UserName = u.Name
		}
	}
	if err := ht.insert(entry, false); err != nil {
		fmt.Printf("⚠️  Histórico: falha ao gravar %s %s: %v\n", entry.Action, entry.Resource, err)
		return err
	}
	return nil
}

func (ht *HistoryTracker) insert(entry HistoryEntry, ignoreDuplicate bool) error {
	if entry.ID == "" {
		entry.ID = uuid.New().String()
	}
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now()
	}
	before, err := marshalMap(entry.Before)
	if err != nil {
		return err
	}
	after, err := marshalMap(entry.After)
	if err != nil {
		return err
	}
	verb := "INSERT"
	if ignoreDuplicate {
		verb = "INSERT OR IGNORE"
	}
	_, err = ht.db.Exec(verb+` INTO history_entries
		(id, ts, user_email, user_name, action, resource, cluster, status, error_msg, duration_ms, session_name, before_json, after_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		entry.ID, entry.Timestamp.UnixMilli(), entry.UserEmail, entry.UserName, entry.Action, entry.Resource,
		entry.Cluster, NormalizeStatus(entry.Status), entry.ErrorMsg, entry.Duration, entry.SessionName, before, after)
	return err
}

func marshalMap(m map[string]interface{}) (interface{}, error) {
	if m == nil {
		return nil, nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	return string(b), nil
}

// HistoryFilter define filtros para busca. Datas são dias inteiros no fuso local do servidor
// (StartDate a partir de 00:00, EndDate até 23:59:59).
type HistoryFilter struct {
	Action      string
	Actions     []string // qualquer uma destas (usado pelo histórico de Cordon/Drain)
	Cluster     string
	Resource    string // busca parcial, sem diferenciar maiúsculas
	Status      string
	UserEmail   string
	StartDate   time.Time
	EndDate     time.Time
	SessionName string
	Limit       int // 0 = sem limite
	Offset      int
}

func (f HistoryFilter) where() (string, []interface{}) {
	var conds []string
	var args []interface{}
	eq := func(col, v string) {
		if v != "" {
			conds = append(conds, col+" = ?")
			args = append(args, v)
		}
	}
	eq("action", f.Action)
	eq("cluster", f.Cluster)
	eq("status", NormalizeStatus(f.Status))
	eq("user_email", f.UserEmail)
	eq("session_name", f.SessionName)
	if len(f.Actions) > 0 {
		conds = append(conds, "action IN (?"+strings.Repeat(",?", len(f.Actions)-1)+")")
		for _, a := range f.Actions {
			args = append(args, a)
		}
	}
	if f.Resource != "" {
		conds = append(conds, "instr(lower(resource), lower(?)) > 0")
		args = append(args, f.Resource)
	}
	if !f.StartDate.IsZero() {
		d := f.StartDate
		conds = append(conds, "ts >= ?")
		args = append(args, time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.Local).UnixMilli())
	}
	if !f.EndDate.IsZero() {
		d := f.EndDate
		conds = append(conds, "ts < ?")
		args = append(args, time.Date(d.Year(), d.Month(), d.Day()+1, 0, 0, 0, 0, time.Local).UnixMilli())
	}
	if len(conds) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(conds, " AND "), args
}

const selectColumns = `SELECT id, ts, user_email, user_name, action, resource, cluster, status, error_msg, duration_ms, session_name, before_json, after_json FROM history_entries`

// Query devolve as entradas que batem com o filtro (mais recentes primeiro) e o total sem paginação.
func (ht *HistoryTracker) Query(f HistoryFilter) ([]HistoryEntry, int, error) {
	if ht == nil {
		return []HistoryEntry{}, 0, nil
	}
	where, args := f.where()
	var total int
	if err := ht.db.QueryRow(`SELECT COUNT(*) FROM history_entries`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	q := selectColumns + where + ` ORDER BY ts DESC, id`
	if f.Limit > 0 {
		q += fmt.Sprintf(" LIMIT %d OFFSET %d", f.Limit, max(f.Offset, 0))
	}
	rows, err := ht.db.Query(q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	entries := []HistoryEntry{}
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			return nil, 0, err
		}
		entries = append(entries, e)
	}
	return entries, total, rows.Err()
}

type rowScanner interface{ Scan(...interface{}) error }

func scanEntry(r rowScanner) (HistoryEntry, error) {
	var e HistoryEntry
	var ts int64
	var before, after sql.NullString
	if err := r.Scan(&e.ID, &ts, &e.UserEmail, &e.UserName, &e.Action, &e.Resource, &e.Cluster, &e.Status,
		&e.ErrorMsg, &e.Duration, &e.SessionName, &before, &after); err != nil {
		return e, err
	}
	e.Timestamp = time.UnixMilli(ts)
	if before.Valid {
		_ = json.Unmarshal([]byte(before.String), &e.Before)
	}
	if after.Valid {
		_ = json.Unmarshal([]byte(after.String), &e.After)
	}
	return e, nil
}

// GetByID retorna uma entrada específica por ID.
func (ht *HistoryTracker) GetByID(id string) (*HistoryEntry, error) {
	if ht == nil {
		return nil, fmt.Errorf("history entry not found: %s", id)
	}
	e, err := scanEntry(ht.db.QueryRow(selectColumns+` WHERE id = ?`, id))
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("history entry not found: %s", id)
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// HistoryStats agrega o histórico inteiro (não só uma página).
type HistoryStats struct {
	Total     int            `json:"total"`
	Success   int            `json:"success"`
	Failed    int            `json:"failed"`
	ByAction  map[string]int `json:"by_action"`
	ByCluster map[string]int `json:"by_cluster"`
	ByUser    map[string]int `json:"by_user"`
}

// Stats conta o histórico inteiro por status, ação, cluster e usuário.
func (ht *HistoryTracker) Stats() (HistoryStats, error) {
	s := HistoryStats{ByAction: map[string]int{}, ByCluster: map[string]int{}, ByUser: map[string]int{}}
	if ht == nil {
		return s, nil
	}
	if err := ht.db.QueryRow(`SELECT COUNT(*),
		COALESCE(SUM(status = 'success'), 0), COALESCE(SUM(status = 'failed'), 0) FROM history_entries`).
		Scan(&s.Total, &s.Success, &s.Failed); err != nil {
		return s, err
	}
	for col, m := range map[string]map[string]int{"action": s.ByAction, "cluster": s.ByCluster, "user_email": s.ByUser} {
		rows, err := ht.db.Query(`SELECT ` + col + `, COUNT(*) FROM history_entries GROUP BY ` + col)
		if err != nil {
			return s, err
		}
		for rows.Next() {
			var k string
			var n int
			if err := rows.Scan(&k, &n); err != nil {
				rows.Close()
				return s, err
			}
			m[k] = n
		}
		rows.Close()
	}
	return s, nil
}

// PurgeExpired apaga as entradas com mais de RetentionDays. É o único jeito de remover algo do
// histórico — não existe "limpar tudo".
func (ht *HistoryTracker) PurgeExpired() (int64, error) {
	if ht == nil {
		return 0, nil
	}
	cutoff := time.Now().AddDate(0, 0, -RetentionDays).UnixMilli()
	res, err := ht.db.Exec(`DELETE FROM history_entries WHERE ts < ?`, cutoff)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// StartRetention roda PurgeExpired uma vez por dia até ctx terminar.
func (ht *HistoryTracker) StartRetention(ctx context.Context) {
	if ht == nil {
		return
	}
	go func() {
		t := time.NewTicker(24 * time.Hour)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if _, err := ht.PurgeExpired(); err != nil {
					fmt.Printf("⚠️  Histórico: falha na retenção: %v\n", err)
				}
			}
		}
	}()
}

// importLegacyJSON importa history/AAAA-MM/*.json (formato antigo) uma única vez e move a pasta
// para backupDir com permissões fechadas. Idempotente: INSERT OR IGNORE pelo id + marca em history_meta.
func (ht *HistoryTracker) importLegacyJSON(legacyDir, backupDir string) error {
	var done string
	_ = ht.db.QueryRow(`SELECT value FROM history_meta WHERE key = 'json_imported'`).Scan(&done)
	if done != "" {
		return nil
	}
	files, _ := filepath.Glob(filepath.Join(legacyDir, "*", "*.json"))
	root, _ := filepath.Glob(filepath.Join(legacyDir, "*.json"))
	files = append(files, root...)

	tx, err := ht.db.Begin()
	if err != nil {
		return err
	}
	imported := 0
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var e HistoryEntry
		if json.Unmarshal(data, &e) != nil || e.ID == "" {
			continue
		}
		before, _ := marshalMap(e.Before)
		after, _ := marshalMap(e.After)
		if _, err := tx.Exec(`INSERT OR IGNORE INTO history_entries
			(id, ts, user_email, user_name, action, resource, cluster, status, error_msg, duration_ms, session_name, before_json, after_json)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			e.ID, e.Timestamp.UnixMilli(), e.UserEmail, e.UserName, e.Action, e.Resource, e.Cluster,
			NormalizeStatus(e.Status), e.ErrorMsg, e.Duration, e.SessionName, before, after); err != nil {
			tx.Rollback()
			return err
		}
		imported++
	}
	if _, err := tx.Exec(`INSERT OR REPLACE INTO history_meta (key, value) VALUES ('json_imported', ?)`,
		time.Now().Format(time.RFC3339)); err != nil {
		tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}

	if len(files) > 0 {
		fmt.Printf("📦 Histórico: %d entrada(s) importada(s) do formato JSON antigo\n", imported)
		if _, err := os.Stat(backupDir); os.IsNotExist(err) {
			if err := os.Rename(legacyDir, backupDir); err == nil {
				legacyDir = backupDir
			}
		}
	}
	// Fecha as permissões do que sobrou em disco (antes era 0755/0644).
	_ = filepath.Walk(legacyDir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			return os.Chmod(p, 0700)
		}
		return os.Chmod(p, 0600)
	})
	return nil
}

// Action constants
const (
	ActionUpdateHPA          = "update_hpa"
	ActionApplyNodePool      = "apply_nodepool"
	ActionAbortNodePool      = "abort_nodepool"
	ActionSuspendCronJob     = "suspend_cronjob"
	ActionResumeCronJob      = "resume_cronjob"
	ActionRolloutPrometheus  = "rollout_prometheus"
	ActionSaveSession        = "save_session"
	ActionLoadSession        = "load_session"
	ActionDeleteSession      = "delete_session"
	ActionApplyBatch         = "apply_batch"
	ActionSnapshotCluster    = "snapshot_cluster"
	ActionCordonNode         = "cordon_node"         // Node marcado como unschedulable
	ActionDrainNode          = "drain_node"          // Pods evacuados do node
	ActionUncordonNode       = "uncordon_node"       // Node voltou a aceitar pods (aba Nodes)
	ActionApplyNode          = "apply_node"          // YAML do node aplicado (aba Nodes)
	ActionDeleteNode         = "delete_node"         // Objeto Node removido da API (aba Nodes)
	ActionNodePoolSequence   = "nodepool_sequence"   // Sequência completa PRE-DRAIN → CORDON → DRAIN → POST-DRAIN
	ActionRolloutDeployment  = "rollout_deployment"  // Rollout de Deployment executado
	ActionRolloutDaemonSet   = "rollout_daemonset"   // Rollout de DaemonSet executado
	ActionRolloutStatefulSet = "rollout_statefulset" // Rollout de StatefulSet executado
)

// Status constants
const (
	StatusSuccess = "success"
	StatusFailed  = "failed"
	StatusPartial = "partial"
)
