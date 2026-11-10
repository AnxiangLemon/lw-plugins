package manager

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	lwplugin "github.com/AnxiangLemon/lw-plugins/ws/go"
)

type scriptedHost struct {
	request func(context.Context, string, any, any) error
}

func (h *scriptedHost) Connected() bool { return true }
func (h *scriptedHost) Request(c context.Context, m string, p, o any) error {
	return h.request(c, m, p, o)
}
func reply(out, value any) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}
func dashboardManager(t *testing.T, h Host) (*Manager, *Store) {
	t.Helper()
	s := openTestStore(t)
	c := DefaultConfig()
	c.Groups = []Group{group()}
	m, err := New(c, filepath.Join(t.TempDir(), "config.json"), s, h, testLog())
	if err != nil {
		t.Fatal(err)
	}
	return m, s
}

func TestDirectoryPaginationNamesAndNoProgress(t *testing.T) {
	pages, brief := 0, 0
	h := &scriptedHost{request: func(_ context.Context, method string, p, out any) error {
		if method != "wechat.call" {
			return errors.New("unexpected method")
		}
		args := p.(map[string]any)
		var data any
		switch args["method"] {
		case "friend.init_contact":
			cursor := args["params"].(map[string]any)
			if pages == 0 {
				if cursor["currentWxContactSeq"] != int32(0) {
					t.Fatal(cursor)
				}
				data = map[string]any{"BaseResponse": map[string]int{"ret": 0}, "CurrentWxcontactSeq": 1, "CurrentChatRoomContactSeq": 3, "CountinueFlag": 1, "ContactUsernameList": []string{"wxid_friend", "room@chatroom", "corp@im.chatroom"}}
			} else {
				if cursor["currentWxContactSeq"] != int32(1) || cursor["currentChatRoomContactSeq"] != int32(3) {
					t.Fatal("未使用双游标", cursor)
				}
				data = map[string]any{"BaseResponse": map[string]int{"ret": 0}, "CountinueFlag": 0, "ContactUsernameList": []string{"room@chatroom", "second@chatroom"}}
			}
			pages++
		case "friend.batch_get_contact_brief_info":
			brief++
			ids := args["params"].(map[string]string)["toWxids"]
			if ids != "room@chatroom,second@chatroom" {
				t.Fatal("没有仅查询去重后的普通群", ids)
			}
			list := []any{}
			for _, id := range strings.Split(ids, ",") {
				list = append(list, map[string]any{"username": id, "contact": map[string]any{"userName": map[string]string{"string": id}, "nickName": map[string]string{"string": "客户交流群"}, "remark": map[string]string{"string": "售后客户"}, "chatRoomOwner": "wxid_owner", "ticket": "must-not-leak"}})
			}
			data = map[string]any{"baseResponse": map[string]int{"ret": 0}, "contactList": list}
		default:
			t.Fatal("unexpected business", args["method"])
		}
		b, _ := json.Marshal(data)
		return reply(out, lwplugin.Result{Status: "succeeded", Data: b})
	}}
	m, s := dashboardManager(t, h)
	rooms, err := m.loadDirectory(context.Background(), "wxid_bot")
	if err != nil || len(rooms) != 2 || pages != 2 || brief != 1 {
		t.Fatalf("分页失败: %+v %v", rooms, err)
	}
	raw, _ := json.Marshal(rooms)
	if strings.Contains(string(raw), "ticket") || !strings.Contains(string(raw), "客户交流群") {
		t.Fatal(string(raw))
	}
	if err = s.SaveRooms(rooms); err != nil {
		t.Fatal(err)
	}
	h.request = func(_ context.Context, _ string, _ any, out any) error {
		return reply(out, lwplugin.Result{Status: "succeeded", Data: json.RawMessage(`{"BaseResponse":{"ret":0},"CountinueFlag":1,"CurrentWxcontactSeq":0,"CurrentChatRoomContactSeq":0}`)})
	}
	if _, err = m.loadDirectory(context.Background(), "wxid_bot"); err == nil {
		t.Fatal("接受了无进展的分页")
	}
	kept, _ := s.Rooms("wxid_bot")
	if len(kept) != 2 {
		t.Fatal("失败丢失了原目录")
	}
}

func TestInviterStatisticsSeparateHistoryCurrentAndAccounts(t *testing.T) {
	s := openTestStore(t)
	g := group()
	base := snapshot(1, "wxid_bot", "old", "host")
	base.Members[1].InviterID = "host"
	base.Members[2].Nickname = "邀请人甲"
	if _, err := s.Apply(g, base, "init", true); err != nil {
		t.Fatal(err)
	}
	next := snapshot(2, "wxid_bot", "old", "host", "new")
	next.Members[1].InviterID = "host"
	next.Members[2].Nickname = "邀请人甲"
	next.Members[3].InviterID = "host"
	if _, err := s.Apply(g, next, "hint", false); err != nil {
		t.Fatal(err)
	}
	other := g
	other.AccountID = "wxid_other"
	other.RoomID = "other@chatroom"
	if _, err := s.Apply(other, next, "init", true); err != nil {
		t.Fatal(err)
	}
	d, err := s.Dashboard(g.AccountID, g.RoomID, 7)
	if err != nil || d.Joined != 1 || d.Members != 4 || d.KnownInviters != 2 || len(d.Inviters) != 1 || d.Inviters[0].Current != 2 || d.Inviters[0].Joined != 1 {
		t.Fatalf("统计错误: %+v %v", d, err)
	}
	events, _ := s.Events(g.AccountID, g.RoomID, 200)
	if events[0].InviterID != "host" || events[0].InviterName != "邀请人甲" {
		t.Fatal("新增成员缺少邀请证据", events)
	}
}

func TestOperationsWhitelistStableIDAndUnknownNeverReplays(t *testing.T) {
	calls := 0
	h := &scriptedHost{request: func(_ context.Context, method string, p, out any) error {
		if method != "wechat.call" {
			return errors.New("unexpected request")
		}
		calls++
		args := p.(map[string]any)
		if args["method"] != "group.set_name" || args["options"].(lwplugin.Options).Key != "group-operation:stable-operation-12345" {
			t.Fatal(args)
		}
		return context.DeadlineExceeded
	}}
	m, s := dashboardManager(t, h)
	o := Operation{ID: "stable-operation-12345", AccountID: group().AccountID, RoomID: group().RoomID, Action: "name", Content: "新群名称"}
	for _, action := range []string{"create_chat_room", "invite_chat_room_member", "add_chat_room_member", "approve_join"} {
		bad := o
		bad.Action = action
		if _, _, err := bad.method(); err == nil {
			t.Fatal("开放了未授权动作", action)
		}
	}
	if _, err := m.SubmitOperation(o); err != nil {
		t.Fatal(err)
	}
	if _, err := m.SubmitOperation(o); err != nil {
		t.Fatal(err)
	}
	changed := o
	changed.Content = "不同内容"
	if _, err := m.SubmitOperation(changed); err == nil {
		t.Fatal("允许同编号复用不同动作")
	}
	_, _ = s.db.Exec("UPDATE operations SET status='running' WHERE id=?", o.ID)
	m.executeOperation(context.Background(), o)
	stored, err := s.Operation(o.ID)
	if err != nil || stored.Status != "unknown" || calls != 1 {
		t.Fatalf("未知状态错误: %+v %v", stored, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 650*time.Millisecond)
	defer cancel()
	m.operations(ctx)
	if calls != 1 {
		t.Fatal("未知动作被自动重新执行")
	}
	if err = s.operationResult(o.ID, "succeeded", "call", "", json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	if err = s.operationResult(o.ID, "unknown", "", "late timeout", nil); err != nil {
		t.Fatal(err)
	}
	stored, _ = s.Operation(o.ID)
	if stored.Status != "succeeded" {
		t.Fatal("晚到超时覆盖了确定结果")
	}
}

func TestReceiptDoesNotRequeueRunningOperation(t *testing.T) {
	h := &scriptedHost{request: func(_ context.Context, _ string, _ any, out any) error {
		return reply(out, &lwplugin.Result{Status: "queued", CallID: "original"})
	}}
	m, s := dashboardManager(t, h)
	o := Operation{ID: "receipt-operation-123", AccountID: group().AccountID, RoomID: group().RoomID, Action: "info"}
	if _, err := m.SubmitOperation(o); err != nil {
		t.Fatal(err)
	}
	_, _ = s.db.Exec("UPDATE operations SET status='running' WHERE id=?", o.ID)
	if _, err := m.OperationReceipt(context.Background(), o.ID); err != nil {
		t.Fatal(err)
	}
	stored, _ := s.Operation(o.ID)
	if stored.Status != "running" {
		t.Fatal("查询把执行中的动作重新放回队列", stored.Status)
	}
}

func TestV1MigrationKeepsHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	s, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Apply(group(), snapshot(1, "wxid_bot"), "init", true); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec("ALTER TABLE events DROP COLUMN inviter_id; ALTER TABLE events DROP COLUMN inviter_name; PRAGMA user_version=1;"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	upgraded, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer upgraded.Close()
	rows, err := upgraded.Events("", "", 200)
	if err != nil || len(rows) != 1 || rows[0].Kind != "baseline" {
		t.Fatal(rows, err)
	}
}

// 逐项核对服务端接口名称与请求字段，尤其是 false、空昵称和管理员操作编号。
func TestAllGroupActionsUseDocumentedParameters(t *testing.T) {
	no := false
	for _, tc := range []struct {
		action, method string
		content        string
		members        []string
		enabled        *bool
		url            string
		field          string
		want           any
	}{
		{"info", "group.get_chat_room_info_detail", "", nil, nil, "", "chatRoomId", "room@chatroom"},
		{"qrcode", "group.get_qrcode", "", nil, nil, "", "chatRoomId", "room@chatroom"},
		{"name", "group.set_name", "客户交流群", nil, nil, "", "content", "客户交流群"},
		{"announcement", "group.set_announcement", "", nil, nil, "", "content", ""},
		{"remark", "group.set_remarks", "仅自己可见", nil, nil, "", "content", "仅自己可见"},
		{"self_name", "group.set_self_display_name", "", nil, nil, "", "displayName", ""},
		{"show_names", "group.set_show_member_display_name", "", nil, &no, "", "enabled", false},
		{"save_contact", "group.move_contact_list", "", nil, &no, "", "val", 2},
		{"remove_members", "group.del_chat_room_member", "", []string{"a", "b"}, nil, "", "toWxids", "a,b"},
		{"add_admin", "group.operate_chat_room_admin", "", []string{"a"}, nil, "", "val", 1},
		{"remove_admin", "group.operate_chat_room_admin", "", []string{"a"}, nil, "", "val", 2},
		{"transfer_owner", "group.operate_chat_room_admin", "", []string{"a"}, nil, "", "val", 3},
		{"quit", "group.quit", "", nil, nil, "", "chatRoomId", "room@chatroom"},
		{"join_scan", "group.scan_into_group", "", nil, nil, "https://weixin.qq.com/g/test", "url", "https://weixin.qq.com/g/test"},
		{"join_scan_enterprise", "group.scan_into_group_enterprise", "", nil, nil, "https://weixin.qq.com/g/test", "url", "https://weixin.qq.com/g/test"},
		{"accept_invite", "group.consent_to_join", "", nil, nil, "https://weixin.qq.com/g/test", "url", "https://weixin.qq.com/g/test"},
	} {
		t.Run(tc.action, func(t *testing.T) {
			o := Operation{ID: "documented-action-123", AccountID: "wxid_bot", RoomID: "room@chatroom", Action: tc.action, Content: tc.content, Members: tc.members, Enabled: tc.enabled, URL: tc.url}
			method, p, err := o.method()
			if err != nil || method != tc.method || p[tc.field] != tc.want {
				t.Fatalf("参数不匹配: %s %+v %v", method, p, err)
			}
		})
	}
	projected := string(projectOperation("qrcode", json.RawMessage(`{"qrcode":{"buffer":"aW1hZ2U=","token":"must-not-leak"},"token":"must-not-leak"}`)))
	if strings.Contains(projected, "token") || !strings.Contains(projected, "aW1hZ2U=") {
		t.Fatal(projected)
	}
	projected = string(projectOperation("info", json.RawMessage(`{"contact":{"chatRoom":{"announcement":{"token":"must-not-leak"}}}}`)))
	if strings.Contains(projected, "must-not-leak") {
		t.Fatal(projected)
	}
}

func TestAnnouncementEditorLoadsExistingContentWithoutTickets(t *testing.T) {
	h := &scriptedHost{request: func(_ context.Context, method string, p, out any) error {
		args := p.(map[string]any)
		if method != "wechat.call" || args["method"] != "group.get_chat_room_info_detail" {
			t.Fatal(method, args)
		}
		return reply(out, lwplugin.Result{Status: "succeeded", Data: json.RawMessage(`{"baseResponse":{"ret":0},"announcement":"原公告：欢迎交流","ticket":"must-not-leak"}`)})
	}}
	m, _ := dashboardManager(t, h)
	r := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/api/group-info", strings.NewReader(`{"account_id":"wxid_bot","room_id":"room@chatroom"}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Plugin-Request", "1")
	w := httptest.NewRecorder()
	m.Handler().ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "原公告") || strings.Contains(w.Body.String(), "ticket") {
		t.Fatal(w.Code, w.Body.String())
	}
}
