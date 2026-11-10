package manager

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	lwplugin "github.com/AnxiangLemon/lw-plugins/ws/go"
)

// Operation 持久保存一个客户主动提交的动作。浏览器生成 ID，重复请求使用同一个 ID，
// 插件与宿主都只执行一次；超时、重启只核对原回执，不换幂等键重做写操作。
type Operation struct {
	ID        string          `json:"id"`
	AccountID string          `json:"account_id"`
	RoomID    string          `json:"room_id"`
	Action    string          `json:"action"`
	Content   string          `json:"content,omitempty"`
	Members   []string        `json:"members,omitempty"`
	Enabled   *bool           `json:"enabled,omitempty"`
	URL       string          `json:"url,omitempty"`
	Status    string          `json:"status"`
	CallID    string          `json:"call_id"`
	Detail    string          `json:"detail"`
	Result    json.RawMessage `json:"result"`
	CreatedAt string          `json:"created_at"`
}

// operationRequest 只序列化业务输入，防止客户端伪造执行状态、回执或返回值。
func (o Operation) request() string {
	b, _ := json.Marshal(struct {
		Content string
		Members []string
		Enabled *bool
		URL     string
	}{o.Content, o.Members, o.Enabled, o.URL})
	return string(b)
}

// operationMethod 是明确允许的群操作表。创建群、直接拉人、邀请成员均无入口。
// ConsentToJoin 是本人确认邀请，不能作为群主审批申请使用。
func (o Operation) method() (string, map[string]any, error) {
	if o.AccountID == "" || len(o.AccountID) > 128 || strings.ContainsAny(o.AccountID, " \t\r\n\x00") {
		return "", nil, errors.New("请选择微信账号")
	}
	if len(o.ID) < 16 || len(o.ID) > 80 || strings.ContainsAny(o.ID, " /\\\t\r\n\x00") {
		return "", nil, errors.New("操作编号无效，请重新打开操作窗口")
	}
	p := map[string]any{"chatRoomId": o.RoomID}
	method := ""
	if o.Action == "join_scan" || o.Action == "join_scan_enterprise" || o.Action == "accept_invite" {
		if len(o.URL) < 8 || len(o.URL) > 8192 || (!strings.HasPrefix(o.URL, "https://") && !strings.HasPrefix(o.URL, "http://")) {
			return "", nil, errors.New("请填写完整的微信入群链接")
		}
		p = map[string]any{"url": o.URL}
		method = map[string]string{"join_scan": "group.scan_into_group", "join_scan_enterprise": "group.scan_into_group_enterprise", "accept_invite": "group.consent_to_join"}[o.Action]
		return method, p, nil
	}
	if !validRoom(o.RoomID) {
		return "", nil, errors.New("请选择有效群聊")
	}
	enterprise := strings.HasSuffix(o.RoomID, "@im.chatroom")
	switch o.Action {
	case "info":
		method = "group.get_chat_room_info_detail"
	case "qrcode":
		method = "group.get_qrcode"
	case "name", "announcement", "remark":
		if len(o.Content) > 12000 || (o.Action == "name" && (strings.TrimSpace(o.Content) == "" || len(o.Content) > 256)) {
			return "", nil, errors.New("群名称不能为空且最多 256 字节，公告或备注最多 12000 字节")
		}
		method = map[string]string{"name": "group.set_name", "announcement": "group.set_announcement", "remark": "group.set_remarks"}[o.Action]
		p["content"] = o.Content
	case "self_name":
		if len(o.Content) > 256 {
			return "", nil, errors.New("群昵称过长")
		}
		method = "group.set_self_display_name"
		p["displayName"] = o.Content
	case "show_names", "save_contact":
		if o.Enabled == nil {
			return "", nil, errors.New("请明确选择开启或关闭")
		}
		if o.Action == "show_names" {
			method = "group.set_show_member_display_name"
			p["enabled"] = *o.Enabled
		} else {
			method = "group.move_contact_list"
			p["val"] = 2
			if *o.Enabled {
				p["val"] = 3
			}
		}
	case "remove_members", "add_admin", "remove_admin", "transfer_owner":
		if len(o.Members) == 0 || len(o.Members) > 100 || (o.Action == "transfer_owner" && len(o.Members) != 1) {
			return "", nil, errors.New("请选择成员；转让群主只能选择一人")
		}
		seen := map[string]bool{}
		for _, id := range o.Members {
			if id == "" || len(id) > 256 || strings.ContainsAny(id, ", \t\r\n\x00") || seen[id] {
				return "", nil, errors.New("成员 ID 为空、重复或格式无效")
			}
			seen[id] = true
			if id == o.AccountID {
				return "", nil, errors.New("此操作请选择其他群成员")
			}
		}
		p["toWxids"] = strings.Join(o.Members, ",")
		if o.Action == "remove_members" {
			method = "group.del_chat_room_member"
		} else {
			method = "group.operate_chat_room_admin"
			p["val"] = map[string]int{"add_admin": 1, "remove_admin": 2, "transfer_owner": 3}[o.Action]
		}
	case "quit":
		method = "group.quit"
	default:
		return "", nil, errors.New("该群操作尚未开放；入群审批需要服务端提供专用接口")
	}
	if enterprise && (o.Action == "qrcode" || o.Action == "remark" || o.Action == "remove_members" || o.Action == "add_admin" || o.Action == "remove_admin" || o.Action == "transfer_owner") {
		return "", nil, errors.New("服务端暂不支持企业互通群的此项操作")
	}
	return method, p, nil
}

func (m *Manager) SubmitOperation(o Operation) (Operation, error) {
	if _, _, err := o.method(); err != nil {
		return o, err
	}
	// 操作目标必须来自已经加载的群或客户关注列表，避免页面传入无关群。
	if validRoom(o.RoomID) {
		rooms, err := m.store.Rooms(o.AccountID)
		if err != nil {
			return o, err
		}
		known := false
		for _, r := range rooms {
			known = known || r.RoomID == o.RoomID
		}
		for _, g := range m.Config().Groups {
			known = known || (g.AccountID == o.AccountID && g.RoomID == o.RoomID)
		}
		if !known {
			return o, errors.New("请先同步群列表或添加该群")
		}
	}
	tx, err := m.store.db.Begin()
	if err != nil {
		return o, err
	}
	defer tx.Rollback()
	var account, room, action, params string
	err = tx.QueryRow("SELECT account_id,room_id,action,params FROM operations WHERE id=?", o.ID).Scan(&account, &room, &action, &params)
	if err == nil {
		if account != o.AccountID || room != o.RoomID || action != o.Action || params != o.request() {
			return o, errors.New("同一操作编号不能用于不同操作")
		}
		tx.Rollback()
		return m.store.Operation(o.ID)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return o, err
	}
	var pending int
	if err = tx.QueryRow("SELECT COUNT(*) FROM operations WHERE status='queued'").Scan(&pending); err != nil {
		return o, err
	}
	if pending >= 100 {
		return o, errors.New("待执行操作较多，请等待完成")
	}
	o.Status = "queued"
	o.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	_, err = tx.Exec("INSERT INTO operations(id,account_id,room_id,action,params,status,created_at) VALUES(?,?,?,?,?,'queued',?)", o.ID, o.AccountID, o.RoomID, o.Action, o.request(), o.CreatedAt)
	if err != nil {
		return o, err
	}
	if err = tx.Commit(); err != nil {
		return o, err
	}
	return m.store.Operation(o.ID)
}

const operationColumns = `id,account_id,room_id,action,params,status,call_id,detail,result,created_at`

func scanOperation(row interface{ Scan(...any) error }) (Operation, error) {
	var o Operation
	var raw, result string
	err := row.Scan(&o.ID, &o.AccountID, &o.RoomID, &o.Action, &raw, &o.Status, &o.CallID, &o.Detail, &result, &o.CreatedAt)
	if err != nil {
		return o, err
	}
	var p struct {
		Content string
		Members []string
		Enabled *bool
		URL     string
	}
	if err = json.Unmarshal([]byte(raw), &p); err != nil {
		return o, err
	}
	o.Content, o.Members, o.Enabled, o.URL = p.Content, p.Members, p.Enabled, p.URL
	o.Result = json.RawMessage(result)
	return o, nil
}
func (s *Store) Operation(id string) (Operation, error) {
	return scanOperation(s.db.QueryRow("SELECT "+operationColumns+" FROM operations WHERE id=?", id))
}
func (s *Store) Operations(account string) ([]Operation, error) {
	rows, err := s.db.Query("SELECT "+operationColumns+" FROM operations WHERE (?='' OR account_id=?) ORDER BY created_at DESC LIMIT 100", account, account)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Operation{}
	for rows.Next() {
		o, e := scanOperation(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, o)
	}
	return out, rows.Err()
}
func (s *Store) operationResult(id, status, call, detail string, data json.RawMessage) error {
	if len(data) == 0 {
		data = json.RawMessage(`{}`)
	}
	// 查询结果与执行协程可能同时返回，已确定的终态不能被稍后的网络超时覆盖。
	_, err := s.db.Exec("UPDATE operations SET status=?,call_id=?,detail=?,result=? WHERE id=? AND status IN ('queued','running','unknown')", status, call, detail, string(data), id)
	return err
}

func (m *Manager) operations(ctx context.Context) {
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if !m.host.Connected() {
				continue
			}
			o, err := scanOperation(m.store.db.QueryRow("SELECT " + operationColumns + " FROM operations WHERE status='queued' ORDER BY created_at LIMIT 1"))
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			if err != nil {
				m.logger.Error("读取群操作失败", "error", err)
				continue
			}
			claimed, err := m.store.db.Exec("UPDATE operations SET status='running' WHERE id=? AND status='queued'", o.ID)
			if err != nil {
				continue
			}
			n, _ := claimed.RowsAffected()
			if n != 1 {
				continue
			}
			m.executeOperation(ctx, o)
		}
	}
}
func (m *Manager) executeOperation(parent context.Context, o Operation) {
	ctx, cancel := context.WithTimeout(parent, 70*time.Second)
	defer cancel()
	method, params, err := o.method()
	var result lwplugin.Result
	wx := lwplugin.NewWechat(m.host, o.AccountID)
	if err == nil {
		result, err = wx.Call(ctx, method, params, lwplugin.Options{Key: "group-operation:" + o.ID})
	}
	if err == nil {
		result, err = wx.Await(ctx, result)
	}
	status, detail, data := "unknown", "", json.RawMessage(`{}`)
	if err != nil {
		detail = err.Error()
	} else {
		status = result.Status
		if result.Error != nil {
			detail = result.Error.Message
		}
		if result.Succeeded() {
			data = projectOperation(o.Action, result.Data)
		}
	}
	if err = m.store.operationResult(o.ID, status, result.CallID, detail, data); err != nil {
		m.logger.Error("保存群操作结果失败", "operation_id", o.ID, "error", err)
	}
	m.logger.Info("群操作已处理", "operation_id", o.ID, "account_id", o.AccountID, "room_id", o.RoomID, "action", o.Action, "status", status)
	if status == "succeeded" {
		m.afterOperation(o)
	}
}

func (m *Manager) afterOperation(o Operation) {
	if o.Action == "name" || o.Action == "remark" || o.Action == "join_scan" || o.Action == "join_scan_enterprise" || o.Action == "accept_invite" || o.Action == "quit" {
		_ = m.Discover(o.AccountID)
	}
	if o.Action == "remove_members" {
		_ = m.Recheck(o.AccountID, o.RoomID, false)
	}
	if o.Action == "quit" {
		if err := m.pauseGroup(o.AccountID, o.RoomID); err != nil {
			m.logger.Error("退群后暂停记录失败", "error", err)
		}
	}
}

// pauseGroup 在同一把配置锁内修改、落盘，避免退群回执覆盖客户刚保存的其他群设置。
func (m *Manager) pauseGroup(account, room string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := m.config
	c.Groups = append([]Group{}, c.Groups...)
	for i := range c.Groups {
		if c.Groups[i].AccountID == account && c.Groups[i].RoomID == room {
			c.Groups[i].Enabled = false
		}
	}
	if err := SaveConfig(m.configPath, c); err != nil {
		return err
	}
	m.config = c
	m.generation++
	return nil
}

// 返回资料采用白名单：二维码图像、公告与群 ID 足够页面显示，不透传票据或内部 XML。
func projectOperation(action string, data json.RawMessage) json.RawMessage {
	var v map[string]json.RawMessage
	if json.Unmarshal(data, &v) != nil {
		if action == "join_scan" || action == "join_scan_enterprise" || action == "accept_invite" {
			var room string
			if json.Unmarshal(data, &room) == nil && validRoom(room) {
				b, _ := json.Marshal(map[string]string{"room_id": room})
				return b
			}
		}
		return json.RawMessage(`{}`)
	}
	out := map[string]json.RawMessage{}
	keys := []string{}
	if action == "info" {
		keys = []string{"announcement", "announcementEditor", "announcementPublishTime", "chatRoomStatus", "topic"}
		if contact := v["contact"]; len(contact) > 0 {
			var c map[string]json.RawMessage
			_ = json.Unmarshal(contact, &c)
			var room map[string]json.RawMessage
			_ = json.Unmarshal(c["chatRoom"], &room)
			for _, key := range []string{"announcement", "announcementEditor", "announcementPublishTime", "topic"} {
				if value := room[key]; len(value) > 0 {
					v[key] = value
				}
			}
		}
	}
	if action == "qrcode" {
		// qrcode 是 PB Buffer，只复制经过 Base64 校验的图像，不透传整个嵌套对象。
		var qr struct {
			Buffer string `json:"buffer"`
		}
		if json.Unmarshal(v["qrcode"], &qr) == nil && len(qr.Buffer) <= 2<<20 {
			if bytes, err := base64.StdEncoding.DecodeString(qr.Buffer); err == nil && len(bytes) > 0 {
				clean, _ := json.Marshal(map[string]any{"buffer": qr.Buffer, "iLen": len(bytes)})
				out["qrcode"] = clean
			}
		}
	}
	for _, key := range keys {
		if value := v[key]; len(value) > 0 {
			// 公告字段应为字符串或 uint32，异常对象不能借用白名单字段透传。
			var text string
			var number uint32
			if strings.Contains(key, "Time") || key == "chatRoomStatus" {
				if json.Unmarshal(value, &number) == nil {
					out[key] = value
				}
			} else if json.Unmarshal(value, &text) == nil {
				out[key] = value
			}
		}
	}
	b, err := json.Marshal(out)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return b
}

// OperationReceipt 查询原动作；unknown 状态绝不重新执行。成功后再次投影返回资料。
func (m *Manager) OperationReceipt(ctx context.Context, id string) (Operation, error) {
	o, err := m.store.Operation(id)
	if err != nil {
		return o, err
	}
	if o.Status != "unknown" && o.Status != "running" && o.Status != "queued" {
		return o, nil
	}
	var result *lwplugin.Result
	err = lwplugin.NewWechat(m.host, o.AccountID).Query(ctx, "find", map[string]any{"idempotency_key": "group-operation:" + o.ID}, &result)
	if err != nil {
		return o, err
	}
	if result == nil {
		return o, fmt.Errorf("宿主未找到原操作，保留当前状态；请先核对微信实际结果")
	}
	if result.Status == "queued" || result.Status == "running" {
		// 原调用仍执行中，不能把本地 running/unknown 改回 queued 再次领取。
		return o, nil
	}
	detail := ""
	if result.Error != nil {
		detail = result.Error.Message
	}
	data := json.RawMessage(`{}`)
	if result.Succeeded() {
		data = projectOperation(o.Action, result.Data)
	}
	if err = m.store.operationResult(id, result.Status, result.CallID, detail, data); err != nil {
		return o, err
	}
	if result.Succeeded() {
		m.afterOperation(o)
	}
	return m.store.Operation(id)
}
