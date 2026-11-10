package manager

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	lwplugin "github.com/AnxiangLemon/lw-plugins/ws/go"
)

func group() Group {
	return Group{AccountID: "wxid_bot", RoomID: "room@chatroom", Label: "测试群", Enabled: true}
}
func snapshot(version uint32, ids ...string) Snapshot {
	s := Snapshot{Version: version}
	for _, id := range ids {
		s.Members = append(s.Members, Member{ID: id, Nickname: "昵称_" + id})
	}
	return s
}
func wire(version uint32, ids ...string) json.RawMessage {
	s := snapshot(version, ids...)
	b, _ := json.Marshal(map[string]any{"baseResponse": map[string]int{"ret": 0}, "chatroomUserName": "room@chatroom", "serverVersion": version, "newChatroomData": map[string]any{"memberCount": len(ids), "chatRoomMember": s.Members}})
	return b
}
func openTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := OpenStore(filepath.Join(t.TempDir(), "manager.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}
func testLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// 先验证完整性，才能把“名单消失”解释为成员净变化。
func TestSnapshotRejectsIncompleteOrWrongAccount(t *testing.T) {
	if _, err := ParseSnapshot(wire(3, "wxid_bot", "a"), "wxid_bot", "room@chatroom"); err != nil {
		t.Fatal(err)
	}
	mutations := []func(map[string]any){
		func(v map[string]any) { delete(v, "baseResponse") },
		func(v map[string]any) { v["baseResponse"] = map[string]int{"ret": -1} },
		func(v map[string]any) { v["chatroomUserName"] = "other@chatroom" },
		func(v map[string]any) { delete(v, "serverVersion") },
		func(v map[string]any) { v["newChatroomData"].(map[string]any)["infoMask"] = 1 },
		func(v map[string]any) { v["newChatroomData"].(map[string]any)["memberCount"] = 3 },
	}
	for i, change := range mutations {
		var v map[string]any
		json.Unmarshal(wire(3, "wxid_bot", "a"), &v)
		change(v)
		b, _ := json.Marshal(v)
		if _, err := ParseSnapshot(b, "wxid_bot", "room@chatroom"); err == nil {
			t.Fatalf("未拒绝第 %d 种坏响应", i)
		}
	}
	for _, ids := range [][]string{{}, {"a"}, {"wxid_bot", "wxid_bot"}} {
		if _, err := ParseSnapshot(wire(3, ids...), "wxid_bot", "room@chatroom"); err == nil {
			t.Fatalf("接受了无效名单: %v", ids)
		}
	}
}
func TestSnapshotsPersistDiffsAndNeverInventInitialJoins(t *testing.T) {
	s := openTestStore(t)
	g := group()
	e, err := s.Apply(g, snapshot(1, "wxid_bot", "a"), "reconnect", true)
	if err != nil || len(e) != 1 || e[0].Kind != "baseline" || e[0].NotifyStatus != "disabled" {
		t.Fatalf("%+v %v", e, err)
	}
	e, err = s.Apply(g, snapshot(2, "wxid_bot", "a", "b"), "mod_contact", false)
	if err != nil || len(e) != 1 || e[0].Kind != "joined" || e[0].MemberID != "b" || e[0].NotifyStatus != "disabled" {
		t.Fatalf("%+v %v", e, err)
	}
	e, err = s.Apply(g, snapshot(3, "wxid_bot", "b"), "weak_sync_hint", false)
	if err != nil || len(e) != 1 || e[0].Kind != "left" || e[0].Reason != "unknown" || e[0].Source != "member_snapshot_diff" {
		t.Fatalf("%+v %v", e, err)
	}
	if e, err = s.Apply(g, snapshot(3, "wxid_bot", "b"), "duplicate", false); err != nil || len(e) != 0 {
		t.Fatal("重复名单产生重复事件")
	}
	if _, err = s.Apply(g, snapshot(2, "wxid_bot", "a"), "stale", false); err == nil {
		t.Fatal("旧版本覆盖了基线")
	}
	if _, err = s.Apply(g, snapshot(3, "wxid_bot", "c"), "inconsistent", false); err == nil {
		t.Fatal("同版本的冲突名单被接受")
	}
	current, _ := s.Members(g.AccountID, g.RoomID)
	if current.Version != 3 || len(current.Members) != 2 {
		t.Fatal("坏响应改变了基线")
	}
	// 昵称变化不等于离群再入群；手动重建基线也不碰旧审计。
	next := snapshot(3, "wxid_bot", "b")
	next.Members[1].DisplayName = "新昵称"
	if e, err = s.Apply(g, next, "nickname", false); err != nil || len(e) != 0 {
		t.Fatal("昵称变化误报进退群")
	}
	if err = s.Reset(g.AccountID, g.RoomID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Apply(g, snapshot(1, "wxid_bot"), "reset", true); err != nil {
		t.Fatal(err)
	}
	rows, _ := s.Events("", "", 200)
	if len(rows) != 4 {
		t.Fatalf("历史审计被覆盖: %d", len(rows))
	}
}

// 同一个群 ID 在不同账号下有独立基线与日志；未确定通知不能被保留期清理。
func TestStoreAccountIsolationAndUncertainRetention(t *testing.T) {
	s := openTestStore(t)
	for _, account := range []string{"wxid_a", "wxid_b"} {
		g := group()
		g.AccountID = account
		if _, err := s.Apply(g, snapshot(1, account), "initial", true); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Apply(g, snapshot(2, account, "new_member"), "hint", false); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := s.SearchEvents("wxid_a", "room@chatroom", "new_member", "joined", 200)
	if err != nil || len(rows) != 1 || rows[0].AccountID != "wxid_a" {
		t.Fatalf("日志串用账号: %+v %v", rows, err)
	}
	id := rows[0].ID
	if _, err := s.db.Exec("UPDATE events SET observed_at='2000-01-01T00:00:00Z' WHERE id=?", id); err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"pending", "sending", "queued", "running", "unknown"} {
		if err := s.Notification(id, status, "original_call", ""); err != nil {
			t.Fatal(err)
		}
		if err := s.Cleanup(1); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Audit(id); err != nil {
			t.Fatalf("未确定通知 %s 被清理: %v", status, err)
		}
	}
	if err := s.Notification(id, "succeeded", "original_call", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.Cleanup(1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Audit(id); err == nil {
		t.Fatal("已确定的过期记录未清理")
	}
}

func TestStoreRejectsFutureSchemaWithoutChangingIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "future.db")
	s, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.db.Exec("PRAGMA user_version=99"); err != nil {
		t.Fatal(err)
	}
	if second, err := OpenStore(path); err == nil {
		second.Close()
		t.Fatal("旧程序接受了未来版本数据库")
	}
	var version int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 99 {
		t.Fatalf("未来版本数据库被修改: %d %v", version, err)
	}
}
func TestAuditAndSnapshotCommitTogether(t *testing.T) {
	s := openTestStore(t)
	g := group()
	s.Apply(g, snapshot(1, "wxid_bot"), "initial", true)
	if _, err := s.db.Exec(`CREATE TRIGGER fail_audit BEFORE INSERT ON events BEGIN SELECT RAISE(ABORT,'mock disk failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Apply(g, snapshot(2, "wxid_bot", "a"), "hint", false); err == nil {
		t.Fatal("审计写失败应回滚")
	}
	current, _ := s.Members(g.AccountID, g.RoomID)
	if current.Version != 1 {
		t.Fatal("丢日志后仍推进基线")
	}
}
func TestRestartKeepsSnapshotsAndDoesNotReplayOldNotifications(t *testing.T) {
	path := filepath.Join(t.TempDir(), "store.db")
	s, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	g := group()
	g.NotifyJoin = true
	g.NotifyLeave = true
	s.Apply(g, snapshot(1, "wxid_bot"), "initial", true)
	joined, _ := s.Apply(g, snapshot(2, "wxid_bot", "a", "b"), "hint", false)
	if claimed, err := s.Claim(joined[0].ID); err != nil || !claimed {
		t.Fatal(err)
	}
	s.Close()
	s, err = OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a, _ := s.Audit(joined[0].ID)
	b, _ := s.Audit(joined[1].ID)
	if a.NotifyStatus != "unknown" || b.NotifyStatus != "disabled" {
		t.Fatalf("重启后错误补发: %+v %+v", a, b)
	}
	e, err := s.Apply(g, snapshot(3, "wxid_bot"), "reconnect", true)
	if err != nil || len(e) != 2 || !e[0].Gap || e[0].NotifyStatus != "disabled" {
		t.Fatal("离线净变化不应补发过时提示")
	}
}

type fakeHost struct {
	mu        sync.Mutex
	calls     int
	sends     int
	data      json.RawMessage
	block     chan struct{}
	entered   chan struct{}
	sendError bool
}

func (f *fakeHost) Connected() bool { return true }
func (f *fakeHost) Request(ctx context.Context, method string, params any, out any) error {
	f.mu.Lock()
	if method == "wechat.send" {
		f.sends++
		fail := f.sendError
		f.mu.Unlock()
		if fail {
			return &lwplugin.RPCError{Code: "timeout", Message: "unknown"}
		}
		b, _ := json.Marshal(lwplugin.Result{Status: "succeeded", CallID: "notice-call"})
		return json.Unmarshal(b, out)
	}
	if method == "accounts.list" {
		f.mu.Unlock()
		return json.Unmarshal([]byte(`[{"account_id":"wxid_bot","display_name":"测试账号","platform":"wechat","online":true}]`), out)
	}
	f.calls++
	data := append(json.RawMessage(nil), f.data...)
	block, entered := f.block, f.entered
	f.mu.Unlock()
	if block != nil {
		if entered != nil {
			select {
			case entered <- struct{}{}:
			default:
			}
		}
		select {
		case <-block:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	b, _ := json.Marshal(lwplugin.Result{Status: "succeeded", Data: data})
	return json.Unmarshal(b, out)
}
func testManager(t *testing.T, host *fakeHost) (*Manager, *Store) {
	t.Helper()
	s := openTestStore(t)
	c := DefaultConfig()
	c.Groups = []Group{group()}
	m, err := New(c, filepath.Join(t.TempDir(), "config.json"), s, host, testLog())
	if err != nil {
		t.Fatal(err)
	}
	return m, s
}

func TestSeparateNotificationSwitchesAndUnknownNeverResends(t *testing.T) {
	h := &fakeHost{}
	m, s := testManager(t, h)
	g := group()
	g.NotifyJoin = true
	s.Apply(g, snapshot(1, "wxid_bot", "a"), "init", true)
	rows, _ := s.Apply(g, snapshot(2, "wxid_bot", "b"), "hint", false)
	if len(rows) != 2 {
		t.Fatal(rows)
	}
	m.config.Groups[0].NotifyJoin = true
	for _, e := range rows {
		if e.NotifyStatus == "pending" {
			m.notify(context.Background(), e)
		}
	}
	if h.sends != 1 {
		t.Fatal("离群开关关闭时仍发送了离群消息")
	}
	h.sendError = true
	rows, _ = s.Apply(g, snapshot(3, "wxid_bot", "b", "c"), "hint", false)
	m.notify(context.Background(), rows[0])
	m.notify(context.Background(), rows[0])
	if h.sends != 2 {
		t.Fatal("unknown 结果被重复发送")
	}
	a, _ := s.Audit(rows[0].ID)
	if a.NotifyStatus != "unknown" {
		t.Fatal(a)
	}
}
func TestHintCoalescingAndDirtyDuringQuery(t *testing.T) {
	h := &fakeHost{data: wire(1, "wxid_bot"), block: make(chan struct{}), entered: make(chan struct{}, 1)}
	m, _ := testManager(t, h)
	g := group()
	e := lwplugin.Event{Name: lwplugin.GroupRefreshHint, Account: lwplugin.Account{ID: g.AccountID, Platform: "wechat"}, Data: json.RawMessage(`{"room_id":"room@chatroom","reason":"mod_contact"}`)}
	for i := 0; i < 20; i++ {
		if err := m.Handle(context.Background(), e); err != nil {
			t.Fatal(err)
		}
	}
	m.mu.Lock()
	s := m.states[g.key()]
	if !s.Pending {
		t.Fatal("没有排队")
	}
	s.Pending = false
	s.Running = true
	m.active = 1
	m.mu.Unlock()
	done := make(chan struct{})
	go func() { m.check(context.Background(), g, "hint", false, 0); close(done) }()
	select {
	case <-h.entered:
	case <-time.After(time.Second):
		t.Fatal("查询未开始")
	}
	m.Handle(context.Background(), e)
	close(h.block)
	<-done
	m.mu.Lock()
	defer m.mu.Unlock()
	if !s.Pending || s.Running || m.active != 0 {
		t.Fatal("查询期间的新提示丢失")
	}
}
func TestWeakHintHasAccountScopeAndFiveGroupBudget(t *testing.T) {
	m, _ := testManager(t, &fakeHost{})
	for i := 0; i < 8; i++ {
		g := group()
		g.RoomID = string(rune('a'+i)) + "@chatroom"
		m.config.Groups = append(m.config.Groups, g)
		m.states[g.key()] = &groupState{}
	}
	e := lwplugin.Event{Name: lwplugin.SyncHint, Account: lwplugin.Account{ID: "wxid_bot", Platform: "wechat"}, Data: json.RawMessage(`{"source":"server_push","selector":4}`)}
	m.Handle(context.Background(), e)
	n := 0
	for _, s := range m.states {
		if s.Pending {
			n++
		}
	}
	if n != 5 {
		t.Fatalf("弱提示核对预算错误: %d", n)
	}
	m.Handle(context.Background(), e)
	after := 0
	for _, s := range m.states {
		if s.Pending {
			after++
		}
	}
	if after != n {
		t.Fatal("弱提示未限频")
	}
}

func TestLocalHTTPConfigurationAndCrossOriginGuard(t *testing.T) {
	m, _ := testManager(t, &fakeHost{})
	h := m.Handler()
	for _, tc := range []struct{ host, origin string }{{"evil.test:8766", ""}, {"127.0.0.1:8766", "https://evil.test"}} {
		r := httptest.NewRequest("GET", "http://"+tc.host+"/api/status", nil)
		r.Host = tc.host
		r.Header.Set("Origin", tc.origin)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusForbidden {
			t.Fatal("允许了跨来源或非法 Host")
		}
	}
	r := httptest.NewRequest("GET", "http://127.0.0.1:8766/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "微信群管家") {
		t.Fatal("内嵌页面不可用")
	}
	c := m.Config()
	c.Groups[0].NotifyJoin = true
	b, _ := json.Marshal(c)
	r = httptest.NewRequest("POST", "http://127.0.0.1:8766/api/config", strings.NewReader(string(b)))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Plugin-Request", "1")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || !m.Config().Groups[0].NotifyJoin {
		t.Fatalf("配置未生效: %s", w.Body.String())
	}
	loaded, err := LoadConfig(m.configPath)
	if err != nil || !loaded.Groups[0].NotifyJoin {
		t.Fatal("配置未持久化")
	}
	if err := ListenAddress("0.0.0.0:8766"); err == nil {
		t.Fatal("设置页不应默认暴露到外网")
	}
}
func TestBadConfigDoesNotReplaceExistingFile(t *testing.T) {
	m, _ := testManager(t, &fakeHost{})
	good := m.Config()
	if err := m.SetConfig(good); err != nil {
		t.Fatal(err)
	}
	bad := good
	bad.Groups = append([]Group(nil), good.Groups...)
	bad.Groups[0].RoomID = "room@im.chatroom"
	if err := m.SetConfig(bad); err == nil {
		t.Fatal("接受了不支持完整成员监控的企业群")
	}
	loaded, err := LoadConfig(m.configPath)
	if err != nil || loaded.Groups[0].RoomID != good.Groups[0].RoomID {
		t.Fatal(errors.New("坏配置覆盖了文件"))
	}
}
