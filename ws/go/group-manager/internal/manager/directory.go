package manager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	lwplugin "github.com/AnxiangLemon/lw-plugins/ws/go"
)

type DirectoryStatus struct {
	AccountID string    `json:"account_id"`
	Running   bool      `json:"running"`
	UpdatedAt time.Time `json:"updated_at"`
	Error     string    `json:"error"`
	Count     int       `json:"count"`
}

func (m *Manager) discoverAccounts(parent context.Context) {
	ctx, cancel := context.WithTimeout(parent, 15*time.Second)
	defer cancel()
	var accounts []struct {
		ID       string `json:"account_id"`
		Platform string `json:"platform"`
		Online   bool   `json:"online"`
	}
	if err := m.host.Request(ctx, "accounts.list", map[string]any{}, &accounts); err != nil {
		m.logger.Warn("加载微信账号失败", "error", err)
		return
	}
	for _, a := range accounts {
		if a.Platform == "wechat" && a.Online {
			_ = m.Discover(a.ID)
		}
	}
}

// requestData 的等待只查询同一调用；读失败由调用方决定下一次刷新，不无限重试。
func (m *Manager) requestData(ctx context.Context, account, method string, params any) (json.RawMessage, error) {
	wx := lwplugin.NewWechat(m.host, account)
	r, err := wx.Call(ctx, method, params, lwplugin.Options{})
	if err == nil {
		r, err = wx.Await(ctx, r)
	}
	if err != nil {
		return nil, err
	}
	if !r.Succeeded() {
		return nil, statusError(r)
	}
	return r.Data, nil
}
func checkBusiness(raw json.RawMessage) error {
	var v struct {
		Base *struct {
			Ret *int `json:"ret"`
		} `json:"baseResponse"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return err
	}
	if v.Base == nil || v.Base.Ret == nil || *v.Base.Ret != 0 {
		return errors.New("微信未返回成功业务状态，保留已有资料")
	}
	return nil
}
func validRoom(id string) bool {
	return len(id) <= 256 && len(id) > len("@chatroom") && (strings.HasSuffix(id, "@chatroom") || strings.HasSuffix(id, "@im.chatroom")) && !strings.ContainsAny(id, " \t\r\n\x00,")
}

// Discover 只排队。分页及资料查询在 Run 中执行，页面无需等待整个通讯录加载。
func (m *Manager) Discover(account string) error {
	if account == "" || len(account) > 128 || strings.ContainsAny(account, " \t\r\n\x00") {
		return errors.New("请选择微信账号")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.directories[account]
	if s == nil {
		s = &DirectoryStatus{AccountID: account}
		m.directories[account] = s
	}
	if s.Running {
		return nil
	}
	m.discoveryPending[account] = true
	return nil
}
func (m *Manager) DirectoryStatuses() []DirectoryStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []DirectoryStatus{}
	for _, s := range m.directories {
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].AccountID < out[j].AccountID })
	return out
}

// discoverRooms 串行推进 InitContact 的两个游标，先收全 ID 再批量补群名称。
// 任何分页失败、重复游标或超限都保留旧列表，不把不完整结果显示成“已全部同步”。
func (m *Manager) discoverRooms(parent context.Context, account string) {
	ctx, cancel := context.WithTimeout(parent, 3*time.Minute)
	defer cancel()
	rooms, err := m.loadDirectory(ctx, account)
	if err == nil {
		err = m.store.ReplaceRooms(account, rooms)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.directories[account]
	s.Running = false
	if err != nil {
		s.Error = err.Error()
		m.logger.Warn("同步群列表失败，保留已有群列表", "account_id", account, "error", err)
		return
	}
	s.Error = ""
	s.UpdatedAt = time.Now()
	s.Count = len(rooms)
	m.logger.Info("群列表已同步", "account_id", account, "count", len(rooms))
}
func (m *Manager) loadDirectory(ctx context.Context, account string) ([]Room, error) {
	var wxSeq, roomSeq int32
	seen := map[string]bool{}
	complete := false
	for page := 0; page < 100; page++ {
		raw, err := m.requestData(ctx, account, "friend.init_contact", map[string]any{"currentWxContactSeq": wxSeq, "currentChatRoomContactSeq": roomSeq})
		if err != nil {
			return nil, err
		}
		if err = checkBusiness(raw); err != nil {
			return nil, err
		}
		var batch struct {
			WX       *int32   `json:"CurrentWxcontactSeq"`
			Room     *int32   `json:"CurrentChatRoomContactSeq"`
			Continue *int32   `json:"CountinueFlag"`
			Names    []string `json:"ContactUsernameList"`
		}
		if err = json.Unmarshal(raw, &batch); err != nil {
			return nil, err
		}
		for _, id := range batch.Names {
			// InitContact / 普通联系人摘要不覆盖企业群协议，避免混入后整批查询失败。
			if validRoom(id) && !strings.HasSuffix(id, "@im.chatroom") {
				seen[id] = true
			}
		}
		if len(seen) > 5000 {
			return nil, errors.New("群数量超过单次同步上限 5000，请分账号管理")
		}
		if batch.Continue == nil {
			return nil, errors.New("通讯录分页缺少结束标志，保留原群列表")
		}
		if *batch.Continue == 0 {
			complete = true
			break
		}
		if batch.WX == nil || batch.Room == nil || *batch.WX < 0 || *batch.Room < 0 || (*batch.WX == wxSeq && *batch.Room == roomSeq) {
			return nil, errors.New("通讯录分页没有进展，已停止刷新")
		}
		wxSeq, roomSeq = *batch.WX, *batch.Room
	}
	if !complete {
		return nil, errors.New("通讯录分页超过 100 页，已停止刷新")
	}
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := []Room{}
	for start := 0; start < len(ids); start += 20 {
		end := start + 20
		if end > len(ids) {
			end = len(ids)
		}
		raw, err := m.requestData(ctx, account, "friend.batch_get_contact_brief_info", map[string]string{"toWxids": strings.Join(ids[start:end], ",")})
		if err != nil {
			return nil, err
		}
		if err = checkBusiness(raw); err != nil {
			return nil, err
		}
		var response struct {
			Contacts []struct {
				ID      string `json:"username"`
				Ret     *int   `json:"ret"`
				Contact struct {
					ID struct {
						String string `json:"string"`
					} `json:"userName"`
					Name struct {
						String string `json:"string"`
					} `json:"nickName"`
					Remark struct {
						String string `json:"string"`
					} `json:"remark"`
					Owner string `json:"chatRoomOwner"`
				} `json:"contact"`
			} `json:"contactList"`
		}
		if err = json.Unmarshal(raw, &response); err != nil {
			return nil, err
		}
		found := map[string]Room{}
		for _, c := range response.Contacts {
			if c.Ret != nil && *c.Ret != 0 {
				continue
			}
			id := c.ID
			if id == "" {
				id = c.Contact.ID.String
			}
			if !seen[id] || (c.Contact.ID.String != "" && c.Contact.ID.String != id) {
				continue
			}
			found[id] = Room{AccountID: account, RoomID: id, Name: c.Contact.Name.String, Remark: c.Contact.Remark.String, Owner: c.Contact.Owner, UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
		}
		for _, id := range ids[start:end] {
			r, ok := found[id]
			if !ok {
				return nil, fmt.Errorf("群 %s 的资料缺失或失败，请稍后刷新", id)
			}
			out = append(out, r)
		}
	}
	return out, nil
}
