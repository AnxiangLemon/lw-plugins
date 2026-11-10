package manager

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	lwplugin "github.com/AnxiangLemon/lw-plugins/ws/go"
	_ "modernc.org/sqlite" // 纯 Go SQLite；CGO_ENABLED=0 时仍可构建。
)

// Store 独占插件自己的 SQLite 文件，与宿主数据库没有写入关系。
// 单连接使事务、通知领取和配置操作的先后关系更容易检查。
type Store struct{ db *sql.DB }

// Audit 是一次已确认的名单净变化。NotifyKey 不传到页面，防止页面误建新调用。
type Audit struct {
	ID           int64  `json:"id"`
	AccountID    string `json:"account_id"`
	RoomID       string `json:"room_id"`
	Revision     int64  `json:"revision"`
	Kind         string `json:"kind"`
	MemberID     string `json:"member_id"`
	Name         string `json:"name"`
	ObservedAt   string `json:"observed_at"`
	Source       string `json:"source"`
	Trigger      string `json:"trigger"`
	Reason       string `json:"reason"`
	Before       int    `json:"before_count"`
	After        int    `json:"after_count"`
	Gap          bool   `json:"gap"`
	NotifyStatus string `json:"notify_status"`
	NotifyKey    string `json:"-"`
	NotifyCallID string `json:"notify_call_id"`
	NotifyError  string `json:"notify_error"`
	InviterID    string `json:"inviter_id"`
	InviterName  string `json:"inviter_name"`
}

// OpenStore 创建初始表，并恢复上次崩溃留下的通知状态；不会自动补发旧通知。
// user_version 留作后续增量迁移入口，新程序的数据不得交由旧版本改写。
func OpenStore(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	var schemaVersion int
	if err = db.QueryRow("PRAGMA user_version").Scan(&schemaVersion); err != nil {
		db.Close()
		return nil, err
	}
	if schemaVersion > 2 {
		db.Close()
		return nil, fmt.Errorf("数据库版本 %d 高于当前程序支持版本，保留原文件", schemaVersion)
	}
	_, err = db.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000;
CREATE TABLE IF NOT EXISTS snapshots(account_id TEXT NOT NULL,room_id TEXT NOT NULL,revision INTEGER NOT NULL,version INTEGER NOT NULL,members TEXT NOT NULL,observed_at TEXT NOT NULL,PRIMARY KEY(account_id,room_id));
CREATE TABLE IF NOT EXISTS events(id INTEGER PRIMARY KEY AUTOINCREMENT,account_id TEXT NOT NULL,room_id TEXT NOT NULL,revision INTEGER NOT NULL,kind TEXT NOT NULL,member_id TEXT NOT NULL,name TEXT NOT NULL,observed_at TEXT NOT NULL,source TEXT NOT NULL,trigger_kind TEXT NOT NULL,reason TEXT NOT NULL,before_count INTEGER NOT NULL,after_count INTEGER NOT NULL,gap INTEGER NOT NULL,notify_status TEXT NOT NULL,notify_key TEXT NOT NULL,notify_call_id TEXT NOT NULL DEFAULT '',notify_error TEXT NOT NULL DEFAULT '',UNIQUE(account_id,room_id,revision,kind,member_id));
CREATE INDEX IF NOT EXISTS event_room ON events(account_id,room_id,id DESC);
UPDATE events SET notify_status='unknown',notify_error='插件重启时通知仍在执行；需查询原回执' WHERE notify_status='sending';
UPDATE events SET notify_status='disabled',notify_error='重启前的待发送提示不自动补发' WHERE notify_status='pending';`)
	if err != nil {
		db.Close()
		return nil, err
	}
	// v2 为邀请人审计和管理面板新增独立数据表。事务升级允许旧数据原地迁移。
	if err = migrateDashboard(db, schemaVersion); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}
func (s *Store) Close() error { return s.db.Close() }

// Apply 用一个事务提交快照和审计。写入失败就回滚，绝不先更新基线再丢日志。
// gap 表示启动/重连核对：保留净变化，但不补发可能已过时的欢迎或离群消息。
func (s *Store) Apply(g Group, next Snapshot, source string, gap bool) ([]Audit, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var revision int64
	var version uint32
	var raw string
	err = tx.QueryRow("SELECT revision,version,members FROM snapshots WHERE account_id=? AND room_id=?", g.AccountID, g.RoomID).Scan(&revision, &version, &raw)
	first := errors.Is(err, sql.ErrNoRows)
	if err != nil && !first {
		return nil, err
	}
	// 手动重建基线后仍延续历史修订号，不能与旧日志的唯一键冲突。
	if first {
		if err = tx.QueryRow("SELECT COALESCE(MAX(revision),0) FROM events WHERE account_id=? AND room_id=?", g.AccountID, g.RoomID).Scan(&revision); err != nil {
			return nil, err
		}
	}
	var previous []Member
	if !first {
		if err = json.Unmarshal([]byte(raw), &previous); err != nil {
			return nil, err
		}
		if next.Version < version {
			return nil, errors.New("收到旧版本成员名单，保留原基线；必要时手动重建基线")
		}
	}
	old := map[string]Member{}
	fresh := map[string]Member{}
	for _, m := range previous {
		old[m.ID] = m
	}
	for _, m := range next.Members {
		fresh[m.ID] = m
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	revision++
	var events []Audit
	add := func(kind string, m Member, notify bool) {
		status := "disabled"
		if notify && !gap {
			status = "pending"
		}
		events = append(events, Audit{AccountID: g.AccountID, RoomID: g.RoomID, Revision: revision, Kind: kind, MemberID: m.ID, Name: m.Name(), ObservedAt: now, Source: "member_snapshot_diff", Trigger: source, Reason: "unknown", Before: len(previous), After: len(next.Members), Gap: gap, NotifyStatus: status, NotifyKey: "group-manager:" + lwplugin.NewKey()})
		e := &events[len(events)-1]
		e.InviterID = m.InviterID
		if inviter, ok := fresh[m.InviterID]; ok {
			e.InviterName = inviter.Name()
		}
	}
	if first {
		add("baseline", Member{}, false)
	} else {
		for _, m := range next.Members {
			if _, ok := old[m.ID]; !ok {
				add("joined", m, g.NotifyJoin)
			}
		}
		for _, m := range previous {
			if _, ok := fresh[m.ID]; !ok {
				add("left", m, g.NotifyLeave)
			}
		}
		if len(events) > 0 && next.Version == version {
			return nil, errors.New("版本未变化但成员名单不同，保留原基线")
		}
	}
	for i := range events {
		e := &events[i]
		result, err := tx.Exec(`INSERT INTO events(account_id,room_id,revision,kind,member_id,name,observed_at,source,trigger_kind,reason,before_count,after_count,gap,notify_status,notify_key,inviter_id,inviter_name) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, e.AccountID, e.RoomID, e.Revision, e.Kind, e.MemberID, e.Name, e.ObservedAt, e.Source, e.Trigger, e.Reason, e.Before, e.After, e.Gap, e.NotifyStatus, e.NotifyKey, e.InviterID, e.InviterName)
		if err != nil {
			return nil, err
		}
		e.ID, err = result.LastInsertId()
		if err != nil {
			return nil, err
		}
	}
	b, err := json.Marshal(next.Members)
	if err != nil {
		return nil, err
	}
	_, err = tx.Exec(`INSERT INTO snapshots(account_id,room_id,revision,version,members,observed_at) VALUES(?,?,?,?,?,?) ON CONFLICT(account_id,room_id) DO UPDATE SET revision=excluded.revision,version=excluded.version,members=excluded.members,observed_at=excluded.observed_at`, g.AccountID, g.RoomID, revision, next.Version, string(b), now)
	if err != nil {
		return nil, err
	}
	return events, tx.Commit()
}
func (s *Store) Members(account, room string) (Snapshot, error) {
	var out Snapshot
	var raw string
	err := s.db.QueryRow("SELECT version,members FROM snapshots WHERE account_id=? AND room_id=?", account, room).Scan(&out.Version, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return Snapshot{Members: []Member{}}, nil
	}
	if err == nil {
		err = json.Unmarshal([]byte(raw), &out.Members)
	}
	return out, err
}

const auditColumns = `id,account_id,room_id,revision,kind,member_id,name,observed_at,source,trigger_kind,reason,before_count,after_count,gap,notify_status,notify_key,notify_call_id,notify_error,inviter_id,inviter_name`

func scanAudit(row interface{ Scan(...any) error }) (Audit, error) {
	var a Audit
	err := row.Scan(&a.ID, &a.AccountID, &a.RoomID, &a.Revision, &a.Kind, &a.MemberID, &a.Name, &a.ObservedAt, &a.Source, &a.Trigger, &a.Reason, &a.Before, &a.After, &a.Gap, &a.NotifyStatus, &a.NotifyKey, &a.NotifyCallID, &a.NotifyError, &a.InviterID, &a.InviterName)
	return a, err
}
func (s *Store) Events(account, room string, limit int) ([]Audit, error) {
	return s.SearchEvents(account, room, "", "", limit)
}

// SearchEvents 在数据库中筛选，而不是先截取最近 200 条再由页面过滤。
func (s *Store) SearchEvents(account, room, query, kind string, limit int) ([]Audit, error) {
	if limit < 1 || limit > 200 {
		limit = 100
	}
	rows, err := s.db.Query(`SELECT `+auditColumns+` FROM events WHERE (?='' OR account_id=?) AND (?='' OR room_id=?) AND (?='' OR instr(member_id,?)>0 OR instr(name,?)>0) AND (?='' OR kind=?) ORDER BY id DESC LIMIT ?`, account, account, room, room, query, query, query, kind, kind, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Audit{}
	for rows.Next() {
		a, err := scanAudit(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
func (s *Store) Audit(id int64) (Audit, error) {
	return scanAudit(s.db.QueryRow(`SELECT `+auditColumns+` FROM events WHERE id=?`, id))
}
func (s *Store) Pending() ([]Audit, error) {
	rows, err := s.db.Query(`SELECT ` + auditColumns + ` FROM events WHERE notify_status='pending' ORDER BY id LIMIT 20`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Audit
	for rows.Next() {
		a, err := scanAudit(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// Claim 以条件更新领取通知，避免两个处理路径重复提交同一条消息。
func (s *Store) Claim(id int64) (bool, error) {
	r, err := s.db.Exec("UPDATE events SET notify_status='sending' WHERE id=? AND notify_status='pending'", id)
	if err != nil {
		return false, err
	}
	n, err := r.RowsAffected()
	return n == 1, err
}
func (s *Store) Notification(id int64, status, callID, detail string) error {
	_, err := s.db.Exec("UPDATE events SET notify_status=?,notify_call_id=?,notify_error=? WHERE id=?", status, callID, detail, id)
	return err
}
func (s *Store) Reset(account, room string) error {
	_, err := s.db.Exec("DELETE FROM snapshots WHERE account_id=? AND room_id=?", account, room)
	return err
}
func (s *Store) Cleanup(days int) error {
	// 查询原回执后可能仍是 queued/running，同样属于未确定结果，不能自动删除。
	_, err := s.db.Exec("DELETE FROM events WHERE observed_at<? AND notify_status NOT IN ('pending','sending','queued','running','unknown')", time.Now().UTC().AddDate(0, 0, -days).Format(time.RFC3339Nano))
	return err
}
func statusError(r lwplugin.Result) error {
	if r.Error != nil {
		return fmt.Errorf("%s: %s", r.Status, r.Error.Message)
	}
	return fmt.Errorf("调用未成功: %s", r.Status)
}
