package wxadmin

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	lwplugin "github.com/AnxiangLemon/lw-plugins/ws/go"
	"github.com/AnxiangLemon/lw-plugins/ws/go/bot-hub/internal/blacklist"
	"github.com/AnxiangLemon/lw-plugins/ws/go/bot-hub/internal/config"
	"github.com/AnxiangLemon/lw-plugins/ws/go/bot-hub/internal/store"
)

const wxAdminFlagBit uint32 = 2048 // 微信协议 chatroomMemberFlag 管理员标记位 (0x800)

// Member 微信群成员信息（保留真实 inviterUserName 邀请来源、头像与管理员标志）。
type Member struct {
	ID              string `json:"userName"`
	Nickname        string `json:"nickName"`
	DisplayName     string `json:"displayName"`
	InviterID       string `json:"inviterUserName,omitempty"`
	SmallHeadImgUrl string `json:"smallHeadImgUrl,omitempty"`
	BigHeadImgUrl   string `json:"bigHeadImgUrl,omitempty"`
	Avatar          string `json:"avatar,omitempty"` // 本地缓存头像 URL（/api/avatar/<hash>.jpg）
	MemberFlag      uint32 `json:"chatroomMemberFlag,omitempty"`
	Role            string `json:"role,omitempty"` // "owner" | "admin" | "member"
}

func (m Member) Name() string {
	if strings.TrimSpace(m.DisplayName) != "" {
		return strings.TrimSpace(m.DisplayName)
	}
	if strings.TrimSpace(m.Nickname) != "" {
		return strings.TrimSpace(m.Nickname)
	}
	return m.ID
}

// RoomMeta 微信群实时元信息（群名称、群备注、群公告、群主、当前账号角色权限）。
type RoomMeta struct {
	AccountID             string `json:"account_id"`
	RoomID                string `json:"room_id"`
	Title                 string `json:"title,omitempty"`
	Remark                string `json:"remark,omitempty"` // 群聊备注（仅自己可见）
	Announcement          string `json:"announcement,omitempty"`
	AnnouncementEditor    string `json:"announcement_editor,omitempty"`
	AnnouncementUpdatedAt int64  `json:"announcement_updated_at,omitempty"`
	OwnerID               string `json:"owner_id,omitempty"`
	SelfRole              string `json:"self_role,omitempty"` // "owner" | "admin" | "member" | ""(未知)
	CanManage             bool   `json:"can_manage"`          // 是否群主或群管理员（可改群公告/移出成员）
	UpdatedAt             int64  `json:"updated_at,omitempty"`
}

// RoomSnapshot 单个微信群的最新有效完整名单快照。
type RoomSnapshot struct {
	AccountID  string   `json:"account_id"`
	RoomID     string   `json:"room_id"`
	Version    uint32   `json:"server_version"`
	UpdatedAt  int64    `json:"updated_at"`
	Members    []Member `json:"members"`
	MemberCnt  int      `json:"member_count"`
	Meta       RoomMeta `json:"meta"`
}

// MemberChange 入群 / 退群事件记录。
type MemberChange struct {
	Time        int64  `json:"time"`
	AccountID   string `json:"account_id"`
	RoomID      string `json:"room_id"`
	RoomTitle   string `json:"room_title"`
	Kind        string `json:"kind"` // "joined" | "left"
	MemberID    string `json:"member_id"`
	MemberName  string `json:"member_name"`
	InviterID   string `json:"inviter_id,omitempty"`
	InviterName string `json:"inviter_name,omitempty"`
	MemberCount int    `json:"member_count"`
}

// Module 微信群管模块：
// - 自动同步群列表 / 手动添加群
// - 监听 wechat.group.refresh_hint 与 wechat.sync.hint 触发完整名单核对（含 30s 节流）
// - 严格验证完整名单（baseResponse.ret==0、infoMask==0、memberCount 一致、本人在群内），杜绝误报全员退群
// - 首次建立基线，后续精确计算入群（含邀请人）与退群差集，可选发送群内欢迎/离群提示
// - 提供群管理操作（查看成员名单、设置群公告、修改群名称、移出成员）
type Module struct {
	cfgMgr *config.Manager
	store  *store.Store
	bl     *blacklist.Store

	cachePath   string
	changesPath string
	avatarDir   string
	httpClient  *http.Client

	mu         sync.RWMutex
	client     *lwplugin.Client
	snapshots  map[string]RoomSnapshot     // key: account_id|room_id
	roomMeta   map[string]RoomMeta         // key: account_id|room_id
	lastCheck  map[string]time.Time
	changes    []MemberChange
	discovered map[string][]DiscoveredRoom // key: account_id
}

// DiscoveredRoom 从微信通讯录同步出的普通群目录。
type DiscoveredRoom struct {
	AccountID string `json:"account_id"`
	RoomID    string `json:"room_id"`
	Title     string `json:"title"`
}

func New(dataDir string, cfgMgr *config.Manager, st *store.Store, bl *blacklist.Store) *Module {
	_ = os.MkdirAll(dataDir, 0o755)
	if bl == nil {
		bl = blacklist.New(dataDir)
	}
	avatarDir := filepath.Join(dataDir, "cache", "img")
	_ = os.MkdirAll(avatarDir, 0o755)
	m := &Module{
		cfgMgr:      cfgMgr,
		store:       st,
		bl:          bl,
		cachePath:   filepath.Join(dataDir, "wx_cache.json"),
		changesPath: filepath.Join(dataDir, "wx_changes.jsonl"),
		avatarDir:   avatarDir,
		httpClient:  &http.Client{Timeout: 10 * time.Second},
		snapshots:   make(map[string]RoomSnapshot),
		roomMeta:    make(map[string]RoomMeta),
		lastCheck:   make(map[string]time.Time),
		changes:     make([]MemberChange, 0, 128),
		discovered:  make(map[string][]DiscoveredRoom),
	}
	m.loadPersistentState()
	return m
}

// AvatarDir 返回本地群成员头像缓存目录。
func (m *Module) AvatarDir() string {
	return m.avatarDir
}

// HasSnapshot 判断指定群是否已有持久化或内存成员基线。
func (m *Module) HasSnapshot(accountID, roomID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.snapshots[accountID+"|"+roomID]
	return ok
}

func (m *Module) SetClient(c *lwplugin.Client) {
	m.mu.Lock()
	m.client = c
	m.mu.Unlock()
}

func (m *Module) getClient() *lwplugin.Client {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.client
}

// HandleEvent 接收微信 Hint 与消息事件（实时捕捉群公告与群名称变更）。
func (m *Module) HandleEvent(ctx context.Context, c *lwplugin.Client, ev lwplugin.Event) {
	m.SetClient(c)
	switch ev.Name {
	case lwplugin.GroupRefreshHint:
		var hint struct {
			RoomID string `json:"room_id"`
		}
		if json.Unmarshal(ev.Data, &hint) == nil && strings.HasSuffix(hint.RoomID, "@chatroom") {
			go m.CheckRoom(context.Background(), ev.Account.ID, hint.RoomID, false)
		}
	case lwplugin.SyncHint:
		// 账号级弱提示：轮询该账号已开启监控的群（受 30s 节流保护）
		cfg := m.cfgMgr.Get()
		for _, r := range cfg.WechatRooms {
			if r.Enabled && r.AccountID == ev.Account.ID {
				go m.CheckRoom(context.Background(), r.AccountID, r.RoomID, false)
			}
		}
	case lwplugin.WechatMessageReceived, lwplugin.GenericMessageReceived:
		if ev.Message == nil {
			return
		}
		platform := ev.Message.Platform
		if platform == "" {
			platform = ev.Account.Platform
		}
		if platform != "wechat" {
			return
		}
		roomID := ev.Message.Conversation()
		if !strings.HasSuffix(roomID, "@chatroom") {
			return
		}
		m.handleWechatRoomMessage(ev.Account.ID, roomID, ev.Message)
	}
}

var (
	reRenameCN = regexp.MustCompile(`修改群名为[“"]([^”"\r\n]+)[”"]`)
	reRenameEN = regexp.MustCompile(`changed the group name to ["“]([^"”\r\n]+)["”]`)
	reAnnTag   = regexp.MustCompile(`(?s)<textannouncement>\s*(?:<!\[CDATA\[(.*?)\]\]>|([^<]*))\s*</textannouncement>`)
	reSysAnn   = regexp.MustCompile(`(?s)<mmchatroombarannouncememt>.*?<content>\s*(?:<!\[CDATA\[(.*?)\]\]>|([^<]*))\s*</content>`)
	reAnnDesc  = regexp.MustCompile(`(?s)<datadesc>\s*(?:<!\[CDATA\[(.*?)\]\]>|([^<]*))\s*</datadesc>`)
)

func extractRegexGroup(re *regexp.Regexp, s string) (string, bool) {
	match := re.FindStringSubmatch(s)
	if len(match) < 2 {
		return "", false
	}
	for _, g := range match[1:] {
		if g != "" {
			return g, true
		}
	}
	return "", true
}

// handleWechatRoomMessage 从实时微信群消息中提取群名称变更与群公告变更。
func (m *Module) handleWechatRoomMessage(accountID, roomID string, msg *lwplugin.Message) {
	text := msg.Text
	if text == "" && len(msg.Raw) > 0 {
		text = string(msg.Raw)
	}
	if text == "" {
		return
	}

	// 1. 群公告更新（appmsg type=87 或 sysmsg mmchatroombarannouncememt）
	if strings.Contains(text, "mmchatroombarannouncememt") || strings.Contains(text, "<textannouncement>") || strings.Contains(text, "group_notice_item") {
		var ann string
		var ok bool
		if ann, ok = extractRegexGroup(reSysAnn, text); !ok {
			if ann, ok = extractRegexGroup(reAnnTag, text); !ok {
				ann, ok = extractRegexGroup(reAnnDesc, text)
			}
		}
		if ok {
			ann = strings.TrimSpace(ann)
			changed := m.updateRoomAnnouncement(accountID, roomID, ann, msg.From.ID, time.Now().Unix())
			if changed {
				title := m.resolveRoomTitle(accountID, roomID)
				preview := ann
				if preview == "" {
					preview = "（公告已清空）"
				}
				m.store.AddLog(store.LogEntry{
					Category: "wx_admin", Platform: "wechat", AccountID: accountID,
					ChatID: roomID, ChatTitle: title,
					Summary: "微信群公告已更新",
					Detail:  preview,
				})
			}
		}
		return
	}

	// 2. 群名称变更（如：你修改群名为“微信测试群1124”）
	if newTitle, ok := extractRegexGroup(reRenameCN, text); ok && strings.TrimSpace(newTitle) != "" {
		m.syncRoomTitle(accountID, roomID, strings.TrimSpace(newTitle), true)
		return
	}
	if newTitle, ok := extractRegexGroup(reRenameEN, text); ok && strings.TrimSpace(newTitle) != "" {
		m.syncRoomTitle(accountID, roomID, strings.TrimSpace(newTitle), true)
	}
}

// StartTicker 定时兜底核对已配置 PollIntervalSec 的微信群。
func (m *Module) StartTicker(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if m.getClient() == nil {
				continue
			}
			cfg := m.cfgMgr.Get()
			now := time.Now()
			for _, r := range cfg.WechatRooms {
				if !r.Enabled || r.PollIntervalSec < 60 {
					continue
				}
				key := r.AccountID + "|" + r.RoomID
				m.mu.RLock()
				last := m.lastCheck[key]
				m.mu.RUnlock()
				if now.Sub(last) >= time.Duration(r.PollIntervalSec)*time.Second {
					go m.CheckRoom(ctx, r.AccountID, r.RoomID, false)
				}
			}
		}
	}
}

// CheckRoom 拉取微信群完整成员名单并与上次基线做差集。
func (m *Module) CheckRoom(parent context.Context, accountID, roomID string, force bool) (RoomSnapshot, error) {
	c := m.getClient()
	if c == nil {
		return RoomSnapshot{}, errors.New("宿主尚未连接")
	}
	if !strings.HasSuffix(roomID, "@chatroom") {
		return RoomSnapshot{}, errors.New("仅普通微信群（@chatroom）支持完整成员监控")
	}
	key := accountID + "|" + roomID
	m.mu.Lock()
	if !force && time.Since(m.lastCheck[key]) < 25*time.Second {
		snap := m.snapshots[key]
		m.mu.Unlock()
		return snap, nil
	}
	m.lastCheck[key] = time.Now()
	m.mu.Unlock()

	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()

	raw, err := callWechat(ctx, c, accountID, "group.get_chat_room_member_detail", map[string]any{
		"chatRoomId": roomID,
	})
	if err != nil {
		return RoomSnapshot{}, err
	}
	snap, err := parseCompleteSnapshot(raw, accountID, roomID)
	if err != nil {
		return RoomSnapshot{}, err
	}

	// 同步拉取群详情（最新群名、群备注、群主、当前群公告）并计算成员及当前账号权限角色
	meta := m.fetchRoomMeta(ctx, c, accountID, roomID, snap.Members)
	assignMemberRoles(snap.Members, meta.OwnerID)
	m.assignAvatarURLs(snap.Members)
	snap.Meta = meta
	if meta.Title != "" {
		m.syncRoomTitle(accountID, roomID, meta.Title, false)
	}

	m.mu.Lock()
	prev, hadBaseline := m.snapshots[key]
	m.snapshots[key] = snap
	m.roomMeta[key] = meta
	m.mu.Unlock()
	m.saveCache()
	go m.cacheMemberAvatars(snap.Members)

	rule, hasRule := m.findRule(accountID, roomID)
	roomTitle := firstNonEmptyStr(meta.Title, rule.Title, roomID)
	m.store.TouchChat("wechat", accountID, roomID, roomTitle)

	if !hadBaseline {
		m.store.AddLog(store.LogEntry{
			Category: "wx_admin", Platform: "wechat", AccountID: accountID,
			ChatID: roomID, ChatTitle: roomTitle,
			Summary: fmt.Sprintf("已建立群成员基线（共 %d 人）", snap.MemberCnt),
		})
		return snap, nil
	}

	// 计算差集
	prevMap := make(map[string]Member, len(prev.Members))
	for _, mem := range prev.Members {
		prevMap[mem.ID] = mem
	}
	currMap := make(map[string]Member, len(snap.Members))
	for _, mem := range snap.Members {
		currMap[mem.ID] = mem
	}

	var joined []Member
	var left []Member
	for _, mem := range snap.Members {
		if _, ok := prevMap[mem.ID]; !ok {
			joined = append(joined, mem)
		}
	}
	for _, mem := range prev.Members {
		if _, ok := currMap[mem.ID]; !ok {
			left = append(left, mem)
		}
	}

	nowTs := time.Now().Unix()
	for _, mem := range joined {
		inviterName := ""
		if mem.InviterID != "" {
			if inv, ok := currMap[mem.InviterID]; ok {
				inviterName = inv.Name()
			} else {
				inviterName = mem.InviterID
			}
		}
		chg := MemberChange{
			Time: nowTs, AccountID: accountID, RoomID: roomID, RoomTitle: roomTitle,
			Kind: "joined", MemberID: mem.ID, MemberName: mem.Name(),
			InviterID: mem.InviterID, InviterName: inviterName, MemberCount: snap.MemberCnt,
		}
		m.recordChange(chg)
		m.store.IncAdminAction()
		summary := fmt.Sprintf("微信入群: %s", mem.Name())
		if inviterName != "" {
			summary += fmt.Sprintf("（邀请人: %s）", inviterName)
		}
		m.store.AddLog(store.LogEntry{
			Category: "wx_admin", Platform: "wechat", AccountID: accountID,
			ChatID: roomID, ChatTitle: roomTitle, Summary: summary,
			Detail: fmt.Sprintf("wxid=%s · 当前群人数 %d", mem.ID, snap.MemberCnt),
		})

		// 全平台黑名单静默拦截：一处拉黑、处处受阻；静默移出不发群消息，仅记日志
		if hasRule && rule.Enabled && rule.BlacklistEnabled {
			if blEntry, blocked := m.bl.Contains("wechat", mem.ID); blocked {
				invInfo := ""
				if inviterName != "" {
					invInfo = fmt.Sprintf(" · 邀请人：%s", inviterName)
				}
				if meta.CanManage {
					if _, err := callWechat(ctx, c, accountID, "group.del_chat_room_member", map[string]any{
						"chatRoomId": roomID,
						"toWxids":    mem.ID,
					}); err == nil {
						m.bl.RecordBannedChat("wechat", mem.ID, roomID)
						m.store.IncAdminAction()
						m.store.AddLog(store.LogEntry{
							Category: "wx_admin", Platform: "wechat", AccountID: accountID,
							ChatID: roomID, ChatTitle: roomTitle,
							Summary: fmt.Sprintf("黑名单静默拦截：已自动移出再次入群成员 %s (%s)", mem.Name(), mem.ID),
							Detail:  fmt.Sprintf("拉黑原因：%s%s", blEntry.Reason, invInfo),
						})
					} else {
						m.store.AddLog(store.LogEntry{
							Category: "wx_admin", Level: "warn", Platform: "wechat", AccountID: accountID,
							ChatID: roomID, ChatTitle: roomTitle,
							Summary: fmt.Sprintf("黑名单拦截移出失败：%s (%s)", mem.Name(), mem.ID),
							Detail:  fmt.Sprintf("错误：%v · 拉黑原因：%s%s", err, blEntry.Reason, invInfo),
						})
					}
				} else {
					m.store.AddLog(store.LogEntry{
						Category: "wx_admin", Level: "warn", Platform: "wechat", AccountID: accountID,
						ChatID: roomID, ChatTitle: roomTitle,
						Summary: fmt.Sprintf("黑名单告警：成员 %s (%s) 再次入群（当前账号非管理员，无法自动移出）", mem.Name(), mem.ID),
						Detail:  fmt.Sprintf("拉黑原因：%s%s", blEntry.Reason, invInfo),
					})
				}
				continue
			}
		}

		if hasRule && rule.Enabled && rule.WelcomeEnabled {
			text := renderWxTemplate(rule.WelcomeTemplate, "欢迎 {name} 加入 {group}！当前群人数：{count}", chg)
			_, _ = c.Wechat(accountID).SendText(ctx, roomID, text, lwplugin.Options{Key: lwplugin.NewKey()})
		}
	}

	for _, mem := range left {
		chg := MemberChange{
			Time: nowTs, AccountID: accountID, RoomID: roomID, RoomTitle: roomTitle,
			Kind: "left", MemberID: mem.ID, MemberName: mem.Name(), MemberCount: snap.MemberCnt,
		}
		m.recordChange(chg)
		m.store.IncAdminAction()
		m.store.AddLog(store.LogEntry{
			Category: "wx_admin", Platform: "wechat", AccountID: accountID,
			ChatID: roomID, ChatTitle: roomTitle,
			Summary: fmt.Sprintf("微信离群: %s", mem.Name()),
			Detail:  fmt.Sprintf("wxid=%s · 当前群人数 %d", mem.ID, snap.MemberCnt),
		})
		if hasRule && rule.Enabled && rule.BlacklistEnabled && rule.BlacklistOnLeave && mem.Role != "owner" && mem.Role != "admin" {
			if _, exists := m.bl.Contains("wechat", mem.ID); !exists {
				m.bl.Add(blacklist.Entry{
					Platform:        "wechat",
					UserID:          mem.ID,
					UserName:        mem.Name(),
					Reason:          "微信离群自动拉黑",
					SourceAccountID: accountID,
					SourceChatID:    roomID,
					SourceChatTitle: roomTitle,
				})
				m.store.AddLog(store.LogEntry{
					Category: "wx_admin", Platform: "wechat", AccountID: accountID,
					ChatID: roomID, ChatTitle: roomTitle,
					Summary: fmt.Sprintf("成员 %s (%s) 离群，已自动加入全局黑名单", mem.Name(), mem.ID),
				})
			}
		}
		if hasRule && rule.Enabled && rule.GoodbyeEnabled {
			text := renderWxTemplate(rule.GoodbyeTemplate, "{name} 离开了 {group}，当前群人数：{count}", chg)
			_, _ = c.Wechat(accountID).SendText(ctx, roomID, text, lwplugin.Options{Key: lwplugin.NewKey()})
		}
	}
	return snap, nil
}

// DiscoverRooms 从微信通讯录分页拉取普通群列表，并补充群名称。
func (m *Module) DiscoverRooms(ctx context.Context, accountID string) ([]DiscoveredRoom, error) {
	c := m.getClient()
	if c == nil {
		return nil, errors.New("宿主尚未连接")
	}
	if strings.TrimSpace(accountID) == "" {
		return nil, errors.New("请选择微信账号")
	}
	var roomIDs []string
	seen := map[string]bool{}
	wxSeq, roomSeq := 0, 0
	for page := 0; page < 30; page++ {
		raw, err := callWechat(ctx, c, accountID, "friend.init_contact", map[string]any{
			"currentWxContactSeq":       wxSeq,
			"currentChatRoomContactSeq": roomSeq,
		})
		if err != nil {
			return nil, err
		}
		var generic map[string]any
		_ = json.Unmarshal(raw, &generic)
		for _, listKey := range []string{"contactUsernameList", "contactUserNameList"} {
			if arr, ok := generic[listKey].([]any); ok {
				for _, item := range arr {
					if s, ok := item.(string); ok && strings.HasSuffix(s, "@chatroom") && !seen[s] {
						seen[s] = true
						roomIDs = append(roomIDs, s)
					}
				}
			}
		}
		nextWx := firstNonZeroInt(generic, "currentWxContactSeq", "currentWxcontactSeq")
		nextRoom := firstNonZeroInt(generic, "currentChatRoomContactSeq", "currentChatroomContactSeq")
		if nextWx == wxSeq && nextRoom == roomSeq {
			break
		}
		wxSeq, roomSeq = nextWx, nextRoom
		if nextWx == 0 && nextRoom == 0 {
			break
		}
	}

	// 批量补群名（lwsapi /Friend/GetContactDetail 要求 toWxids 逗号分隔字符串）
	titles := map[string]string{}
	for i := 0; i < len(roomIDs); i += 20 {
		end := min(i+20, len(roomIDs))
		batch := roomIDs[i:end]
		raw, err := callWechat(ctx, c, accountID, "friend.get_contact_detail", map[string]any{
			"toWxids": strings.Join(batch, ","),
		})
		if err != nil {
			continue
		}
		var detail struct {
			ContactList []struct {
				UserName struct {
					String string `json:"string"`
				} `json:"userName"`
				NickName struct {
					String string `json:"string"`
				} `json:"nickName"`
			} `json:"contactList"`
		}
		if json.Unmarshal(raw, &detail) == nil {
			for _, item := range detail.ContactList {
				if item.UserName.String != "" && item.NickName.String != "" {
					titles[item.UserName.String] = item.NickName.String
				}
			}
		}
	}

	out := make([]DiscoveredRoom, 0, len(roomIDs))
	for _, id := range roomIDs {
		title := titles[id]
		if title == "" {
			title = id
		}
		out = append(out, DiscoveredRoom{AccountID: accountID, RoomID: id, Title: title})
		m.store.TouchChat("wechat", accountID, id, title)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Title < out[j].Title })

	m.mu.Lock()
	m.discovered[accountID] = out
	m.mu.Unlock()
	m.saveCache()
	return out, nil
}

// RoomAction 执行微信群管理写操作（改公告、改仅自己可见的群备注、移出成员）。
func (m *Module) RoomAction(ctx context.Context, accountID, roomID, action, value string) error {
	c := m.getClient()
	if c == nil {
		return errors.New("宿主尚未连接")
	}

	// 改群公告和移出成员仅群主或群管理员可执行
	if action == "set_announcement" || action == "kick" || action == "kick_blacklist" {
		m.mu.RLock()
		meta, hasMeta := m.roomMeta[accountID+"|"+roomID]
		m.mu.RUnlock()
		if hasMeta && meta.SelfRole == "member" {
			return errors.New("当前微信账号不是该群的群主或管理员，无法执行此管理操作（仅可用于监控）")
		}
	}

	var method string
	var params map[string]any
	var summary string
	switch action {
	case "set_announcement":
		method = "group.set_announcement"
		params = map[string]any{"chatRoomId": roomID, "content": value}
		summary = "修改微信群公告"
	case "set_remarks":
		method = "group.set_remarks"
		params = map[string]any{"chatRoomId": roomID, "content": value}
		summary = "修改微信群备注（仅自己可见）: " + value
	case "kick", "kick_blacklist":
		method = "group.del_chat_room_member"
		params = map[string]any{"chatRoomId": roomID, "toWxids": value}
		summary = "移出微信群成员: " + value
	default:
		return fmt.Errorf("不支持的群管操作: %s", action)
	}
	if _, err := callWechat(ctx, c, accountID, method, params); err != nil {
		return err
	}

	roomTitle := m.resolveRoomTitle(accountID, roomID)
	// 写操作成功后立即刷新本地状态与缓存
	switch action {
	case "set_remarks":
		m.updateRoomRemark(accountID, roomID, strings.TrimSpace(value))
	case "set_announcement":
		m.updateRoomAnnouncement(accountID, roomID, value, accountID, time.Now().Unix())
	case "kick", "kick_blacklist":
		shouldBlacklist := action == "kick_blacklist"
		if !shouldBlacklist {
			if rule, ok := m.cfgMgr.FindWechatRoom(accountID, roomID); ok && rule.BlacklistEnabled {
				shouldBlacklist = true
			}
		}
		if shouldBlacklist {
			memName := m.resolveMemberName(accountID, roomID, value)
			reason := "管理员移出微信群自动拉黑"
			if action == "kick_blacklist" {
				reason = "管理员移出并拉黑"
			}
			m.bl.Add(blacklist.Entry{
				Platform:        "wechat",
				UserID:          value,
				UserName:        memName,
				Reason:          reason,
				SourceAccountID: accountID,
				SourceChatID:    roomID,
				SourceChatTitle: roomTitle,
			})
			summary += "（已加入全局黑名单）"
		}
	}

	m.store.IncAdminAction()
	m.store.AddLog(store.LogEntry{
		Category: "wx_admin", Platform: "wechat", AccountID: accountID,
		ChatID: roomID, ChatTitle: roomTitle, Summary: summary,
	})
	return nil
}

func (m *Module) resolveMemberName(accountID, roomID, wxid string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if snap, ok := m.snapshots[accountID+"|"+roomID]; ok {
		for _, mem := range snap.Members {
			if mem.ID == wxid {
				return mem.Name()
			}
		}
	}
	return wxid
}

// KickUserAcrossManagedRooms 手动拉黑微信用户时，静默扫描当前所有拥有管理权限且包含该成员的监控群并将其移出。
func (m *Module) KickUserAcrossManagedRooms(ctx context.Context, wxid string) int {
	c := m.getClient()
	wxid = strings.TrimSpace(wxid)
	if c == nil || wxid == "" {
		return 0
	}
	type target struct {
		accountID string
		roomID    string
		roomTitle string
	}
	var targets []target
	m.mu.RLock()
	for key, snap := range m.snapshots {
		meta := m.roomMeta[key]
		if !meta.CanManage {
			continue
		}
		for _, mem := range snap.Members {
			if mem.ID == wxid && mem.Role != "owner" {
				targets = append(targets, target{
					accountID: snap.AccountID,
					roomID:    snap.RoomID,
					roomTitle: m.resolveRoomTitleLocked(snap.AccountID, snap.RoomID),
				})
				break
			}
		}
	}
	m.mu.RUnlock()

	kicked := 0
	for _, t := range targets {
		if _, err := callWechat(ctx, c, t.accountID, "group.del_chat_room_member", map[string]any{
			"chatRoomId": t.roomID,
			"toWxids":    wxid,
		}); err == nil {
			m.bl.RecordBannedChat("wechat", wxid, t.roomID)
			m.store.IncAdminAction()
			m.store.AddLog(store.LogEntry{
				Category: "wx_admin", Platform: "wechat", AccountID: t.accountID,
				ChatID: t.roomID, ChatTitle: t.roomTitle,
				Summary: fmt.Sprintf("黑名单联动静默移出成员：%s", wxid),
			})
			kicked++
		}
	}
	return kicked
}

func (m *Module) resolveRoomTitleLocked(accountID, roomID string) string {
	if meta, ok := m.roomMeta[accountID+"|"+roomID]; ok {
		if meta.Remark != "" {
			return meta.Remark
		}
		if meta.Title != "" {
			return meta.Title
		}
	}
	return roomID
}

// StateSnapshot 返回微信群管页所需的快照（群成员数、群元信息与权限、最近入退群动态、已发现群目录）。
func (m *Module) StateSnapshot() map[string]any {
	m.mu.RLock()
	defer m.mu.RUnlock()
	counts := make(map[string]int, len(m.snapshots))
	for k, v := range m.snapshots {
		counts[k] = v.MemberCnt
	}
	metas := make(map[string]RoomMeta, len(m.roomMeta))
	for k, v := range m.roomMeta {
		metas[k] = v
	}
	changes := make([]MemberChange, len(m.changes))
	for i := range m.changes {
		changes[i] = m.changes[len(m.changes)-1-i]
	}
	var disc []DiscoveredRoom
	for _, list := range m.discovered {
		disc = append(disc, list...)
	}
	sort.Slice(disc, func(i, j int) bool {
		if disc[i].AccountID != disc[j].AccountID {
			return disc[i].AccountID < disc[j].AccountID
		}
		return disc[i].Title < disc[j].Title
	})
	return map[string]any{
		"member_counts": counts,
		"room_meta":     metas,
		"changes":       changes,
		"discovered":    disc,
	}
}

// SnapshotOf 返回指定群的完整成员列表（附带最新群元信息与权限）。
func (m *Module) SnapshotOf(accountID, roomID string) (RoomSnapshot, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	key := accountID + "|" + roomID
	s, ok := m.snapshots[key]
	if ok {
		if meta, hasMeta := m.roomMeta[key]; hasMeta {
			s.Meta = meta
		}
	}
	return s, ok
}

// fetchRoomMeta 拉取群名称、群主、最新群公告，并计算当前账号在该群的权限角色。
func (m *Module) fetchRoomMeta(ctx context.Context, c *lwplugin.Client, accountID, roomID string, members []Member) RoomMeta {
	key := accountID + "|" + roomID
	m.mu.RLock()
	meta := m.roomMeta[key]
	m.mu.RUnlock()
	meta.AccountID = accountID
	meta.RoomID = roomID
	meta.UpdatedAt = time.Now().Unix()

	// 1. 通过 friend.get_contact_detail 获取最新群名称、群备注与群主 chatRoomOwner
	if raw, err := callWechat(ctx, c, accountID, "friend.get_contact_detail", map[string]any{
		"toWxids": roomID,
	}); err == nil && len(raw) > 0 {
		var detail struct {
			ContactList []struct {
				NickName struct {
					String string `json:"string"`
				} `json:"nickName"`
				Remark struct {
					String string `json:"string"`
				} `json:"remark"`
				ChatRoomOwner string `json:"chatRoomOwner"`
			} `json:"contactList"`
		}
		if json.Unmarshal(raw, &detail) == nil && len(detail.ContactList) > 0 {
			if t := strings.TrimSpace(detail.ContactList[0].NickName.String); t != "" {
				meta.Title = t
			}
			meta.Remark = strings.TrimSpace(detail.ContactList[0].Remark.String)
			if owner := strings.TrimSpace(detail.ContactList[0].ChatRoomOwner); owner != "" {
				meta.OwnerID = owner
			}
		}
	}

	// 2. 通过 group.get_chat_room_info_detail 获取最新群公告
	if raw, err := callWechat(ctx, c, accountID, "group.get_chat_room_info_detail", map[string]any{
		"chatRoomId": roomID,
	}); err == nil && len(raw) > 0 {
		var info struct {
			Announcement            string `json:"announcement"`
			AnnouncementEditor      string `json:"announcementEditor"`
			AnnouncementPublishTime int64  `json:"announcementPublishTime"`
		}
		if json.Unmarshal(raw, &info) == nil {
			ann := strings.TrimSpace(info.Announcement)
			if strings.Contains(ann, "<") {
				if extracted, ok := extractRegexGroup(reAnnTag, ann); ok {
					ann = strings.TrimSpace(extracted)
				} else if extracted, ok := extractRegexGroup(reAnnDesc, ann); ok {
					ann = strings.TrimSpace(extracted)
				}
			}
			meta.Announcement = ann
			if info.AnnouncementEditor != "" {
				meta.AnnouncementEditor = info.AnnouncementEditor
			}
			if info.AnnouncementPublishTime > 0 {
				meta.AnnouncementUpdatedAt = info.AnnouncementPublishTime
			}
		}
	}

	// 3. 结合群主 ID 与成员列表 chatroomMemberFlag 判断当前账号角色
	role := "member"
	if meta.OwnerID != "" && meta.OwnerID == accountID {
		role = "owner"
	} else {
		for _, mem := range members {
			if mem.ID == accountID {
				if mem.MemberFlag&wxAdminFlagBit != 0 {
					role = "admin"
				}
				break
			}
		}
	}
	meta.SelfRole = role
	meta.CanManage = role == "owner" || role == "admin"
	return meta
}

func assignMemberRoles(members []Member, ownerID string) {
	for i := range members {
		switch {
		case ownerID != "" && members[i].ID == ownerID:
			members[i].Role = "owner"
		case members[i].MemberFlag&wxAdminFlagBit != 0:
			members[i].Role = "admin"
		default:
			members[i].Role = "member"
		}
	}
}

func (m *Module) syncRoomTitle(accountID, roomID, newTitle string, logChange bool) {
	newTitle = strings.TrimSpace(newTitle)
	if newTitle == "" {
		return
	}
	key := accountID + "|" + roomID

	m.mu.Lock()
	meta := m.roomMeta[key]
	prevTitle := meta.Title
	meta.AccountID = accountID
	meta.RoomID = roomID
	meta.Title = newTitle
	meta.UpdatedAt = time.Now().Unix()
	m.roomMeta[key] = meta
	if snap, ok := m.snapshots[key]; ok {
		snap.Meta = meta
		m.snapshots[key] = snap
	}
	if list, ok := m.discovered[accountID]; ok {
		for i := range list {
			if list[i].RoomID == roomID {
				list[i].Title = newTitle
			}
		}
		m.discovered[accountID] = list
	}
	m.mu.Unlock()
	m.saveCache()

	m.store.TouchChat("wechat", accountID, roomID, newTitle)

	// 同步持久化更新已监控群配置的群名称，保证概览与群卡片立即一致
	configChanged := false
	_ = m.cfgMgr.Mutate(func(cfg *config.Config) {
		for i := range cfg.WechatRooms {
			if cfg.WechatRooms[i].AccountID == accountID && cfg.WechatRooms[i].RoomID == roomID {
				if cfg.WechatRooms[i].Title != newTitle {
					cfg.WechatRooms[i].Title = newTitle
					configChanged = true
				}
			}
		}
	})

	if logChange && (prevTitle != newTitle || configChanged) {
		m.store.AddLog(store.LogEntry{
			Category: "wx_admin", Platform: "wechat", AccountID: accountID,
			ChatID: roomID, ChatTitle: newTitle,
			Summary: fmt.Sprintf("微信群名称已更新为「%s」", newTitle),
		})
	}
}

func (m *Module) updateRoomAnnouncement(accountID, roomID, ann, editor string, ts int64) bool {
	key := accountID + "|" + roomID
	m.mu.Lock()
	meta := m.roomMeta[key]
	changed := meta.Announcement != ann
	meta.AccountID = accountID
	meta.RoomID = roomID
	meta.Announcement = ann
	if editor != "" {
		meta.AnnouncementEditor = editor
	}
	if ts > 0 {
		meta.AnnouncementUpdatedAt = ts
	}
	meta.UpdatedAt = time.Now().Unix()
	m.roomMeta[key] = meta
	if snap, ok := m.snapshots[key]; ok {
		snap.Meta = meta
		m.snapshots[key] = snap
	}
	m.mu.Unlock()
	if changed {
		m.saveCache()
	}
	return changed
}

func (m *Module) resolveRoomTitle(accountID, roomID string) string {
	m.mu.RLock()
	if meta, ok := m.roomMeta[accountID+"|"+roomID]; ok && meta.Title != "" {
		m.mu.RUnlock()
		return meta.Title
	}
	m.mu.RUnlock()
	if rule, ok := m.findRule(accountID, roomID); ok && strings.TrimSpace(rule.Title) != "" {
		return rule.Title
	}
	return roomID
}

func firstNonEmptyStr(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func (m *Module) recordChange(chg MemberChange) {
	m.mu.Lock()
	m.changes = append(m.changes, chg)
	if len(m.changes) > 200 {
		m.changes = m.changes[len(m.changes)-200:]
	}
	m.mu.Unlock()

	if f, err := os.OpenFile(m.changesPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600); err == nil {
		if raw, err := json.Marshal(chg); err == nil {
			_, _ = f.Write(append(raw, '\n'))
		}
		_ = f.Close()
	}
}

// ClearChanges 清空内存与磁盘持久化的微信入退群变动记录。
func (m *Module) ClearChanges() {
	m.mu.Lock()
	m.changes = m.changes[:0]
	m.mu.Unlock()
	_ = os.WriteFile(m.changesPath, nil, 0o600)
}

func (m *Module) updateRoomRemark(accountID, roomID, remark string) {
	key := accountID + "|" + roomID
	m.mu.Lock()
	meta := m.roomMeta[key]
	meta.AccountID = accountID
	meta.RoomID = roomID
	meta.Remark = remark
	meta.UpdatedAt = time.Now().Unix()
	m.roomMeta[key] = meta
	if snap, ok := m.snapshots[key]; ok {
		snap.Meta = meta
		m.snapshots[key] = snap
	}
	m.mu.Unlock()
	m.saveCache()
}

type persistentCache struct {
	Snapshots  map[string]RoomSnapshot     `json:"snapshots"`
	RoomMeta   map[string]RoomMeta         `json:"room_meta"`
	Discovered map[string][]DiscoveredRoom `json:"discovered"`
}

func (m *Module) loadPersistentState() {
	if raw, err := os.ReadFile(m.cachePath); err == nil && len(raw) > 0 {
		var pc persistentCache
		if json.Unmarshal(raw, &pc) == nil {
			if pc.Snapshots != nil {
				for k, s := range pc.Snapshots {
					m.assignAvatarURLs(s.Members)
					pc.Snapshots[k] = s
				}
				m.snapshots = pc.Snapshots
			}
			if pc.RoomMeta != nil {
				m.roomMeta = pc.RoomMeta
			}
			if pc.Discovered != nil {
				m.discovered = pc.Discovered
			}
		}
	}

	if f, err := os.Open(m.changesPath); err == nil {
		defer f.Close()
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 32*1024), 512*1024)
		var loaded []MemberChange
		for scanner.Scan() {
			var ch MemberChange
			if json.Unmarshal(scanner.Bytes(), &ch) == nil {
				loaded = append(loaded, ch)
			}
		}
		if len(loaded) > 200 {
			loaded = loaded[len(loaded)-200:]
		}
		m.changes = loaded
	}
}

func (m *Module) saveCache() {
	m.mu.RLock()
	pc := persistentCache{
		Snapshots:  m.snapshots,
		RoomMeta:   m.roomMeta,
		Discovered: m.discovered,
	}
	raw, err := json.Marshal(pc)
	m.mu.RUnlock()
	if err != nil {
		return
	}
	tmp := m.cachePath + ".tmp"
	if os.WriteFile(tmp, raw, 0o600) == nil {
		_ = os.Rename(tmp, m.cachePath)
	}
}

func avatarCacheKey(rawURL string) string {
	sum := sha1.Sum([]byte(rawURL))
	return hex.EncodeToString(sum[:]) + ".jpg"
}

func (m *Module) assignAvatarURLs(members []Member) {
	for i := range members {
		src := strings.TrimSpace(members[i].SmallHeadImgUrl)
		if src == "" {
			src = strings.TrimSpace(members[i].BigHeadImgUrl)
		}
		if src == "" {
			continue
		}
		name := avatarCacheKey(src)
		if _, err := os.Stat(filepath.Join(m.avatarDir, name)); err == nil {
			members[i].Avatar = "/api/avatar/" + name
		} else {
			members[i].Avatar = src
		}
	}
}

// cacheMemberAvatars 后台下载缺失的群成员头像到 data/bot-hub/cache/img，已缓存文件自动跳过。
func (m *Module) cacheMemberAvatars(members []Member) {
	type task struct {
		url      string
		filename string
	}
	seen := map[string]bool{}
	var tasks []task
	for _, mem := range members {
		src := strings.TrimSpace(mem.SmallHeadImgUrl)
		if src == "" {
			src = strings.TrimSpace(mem.BigHeadImgUrl)
		}
		if src == "" || !strings.HasPrefix(src, "http") {
			continue
		}
		fn := avatarCacheKey(src)
		if seen[fn] {
			continue
		}
		seen[fn] = true
		if _, err := os.Stat(filepath.Join(m.avatarDir, fn)); err == nil {
			continue
		}
		tasks = append(tasks, task{url: src, filename: fn})
	}
	if len(tasks) == 0 {
		return
	}

	sem := make(chan struct{}, 6)
	var wg sync.WaitGroup
	var downloaded bool
	var dlMu sync.Mutex

	for _, t := range tasks {
		wg.Add(1)
		sem <- struct{}{}
		go func(tk task) {
			defer wg.Done()
			defer func() { <-sem }()
			req, err := http.NewRequest(http.MethodGet, tk.url, nil)
			if err != nil {
				return
			}
			resp, err := m.httpClient.Do(req)
			if err != nil {
				return
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				return
			}
			data, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
			if err != nil || len(data) < 64 {
				return
			}
			dst := filepath.Join(m.avatarDir, tk.filename)
			tmp := dst + ".tmp"
			if os.WriteFile(tmp, data, 0o600) == nil {
				if os.Rename(tmp, dst) == nil {
					dlMu.Lock()
					downloaded = true
					dlMu.Unlock()
				}
			}
		}(t)
	}
	wg.Wait()

	if downloaded {
		m.mu.Lock()
		for k, s := range m.snapshots {
			m.assignAvatarURLs(s.Members)
			m.snapshots[k] = s
		}
		m.mu.Unlock()
		m.saveCache()
	}
}

func (m *Module) findRule(accountID, roomID string) (config.WechatRoomRule, bool) {
	cfg := m.cfgMgr.Get()
	for _, r := range cfg.WechatRooms {
		if r.AccountID == accountID && r.RoomID == roomID {
			return r, true
		}
	}
	return config.WechatRoomRule{}, false
}

func callWechat(ctx context.Context, c *lwplugin.Client, accountID, method string, params any) (json.RawMessage, error) {
	wx := c.Wechat(accountID)
	r, err := wx.Call(ctx, method, params, lwplugin.Options{Key: lwplugin.NewKey()})
	if err == nil {
		r, err = wx.Await(ctx, r)
	}
	if err != nil {
		return nil, err
	}
	if !r.Succeeded() {
		if r.Error != nil {
			return nil, fmt.Errorf("%s: %s", r.Error.Code, r.Error.Message)
		}
		return nil, fmt.Errorf("微信调用未成功 (status=%s)", r.Status)
	}
	return r.Data, nil
}

func parseCompleteSnapshot(data json.RawMessage, accountID, roomID string) (RoomSnapshot, error) {
	var wire struct {
		Base *struct {
			Ret *int `json:"ret"`
		} `json:"baseResponse"`
		Room    string  `json:"chatroomUserName"`
		Version *uint32 `json:"serverVersion"`
		Members *struct {
			Count *uint32 `json:"memberCount"`
			Mask  uint32  `json:"infoMask"`
			Room  *struct {
				String string `json:"string"`
			} `json:"chatRoomUserName"`
			List []Member `json:"chatRoomMember"`
		} `json:"newChatroomData"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return RoomSnapshot{}, fmt.Errorf("成员详情 JSON 无效: %w", err)
	}
	if wire.Base == nil || wire.Base.Ret == nil || *wire.Base.Ret != 0 {
		return RoomSnapshot{}, errors.New("微信成员详情业务状态异常，保留原基线")
	}
	if wire.Members == nil || wire.Version == nil || wire.Members.Count == nil || wire.Members.Mask != 0 {
		return RoomSnapshot{}, errors.New("群成员资料不完整，保留原基线")
	}
	if *wire.Members.Count == 0 || uint32(len(wire.Members.List)) != *wire.Members.Count {
		return RoomSnapshot{}, errors.New("成员数量与名单长度不一致")
	}
	self := false
	for _, mem := range wire.Members.List {
		if mem.ID == accountID {
			self = true
			break
		}
	}
	if !self {
		return RoomSnapshot{}, errors.New("当前账号不在名单内，不触发全员离群")
	}
	sort.Slice(wire.Members.List, func(i, j int) bool { return wire.Members.List[i].ID < wire.Members.List[j].ID })
	return RoomSnapshot{
		AccountID: accountID,
		RoomID:    roomID,
		Version:   *wire.Version,
		UpdatedAt: time.Now().Unix(),
		Members:   wire.Members.List,
		MemberCnt: len(wire.Members.List),
	}, nil
}

func renderWxTemplate(tpl, fallback string, chg MemberChange) string {
	if strings.TrimSpace(tpl) == "" {
		tpl = fallback
	}
	r := strings.NewReplacer(
		"{name}", chg.MemberName,
		"{wxid}", chg.MemberID,
		"{inviter}", chg.InviterName,
		"{group}", chg.RoomTitle,
		"{count}", strconv.Itoa(chg.MemberCount),
	)
	return r.Replace(tpl)
}

func firstNonZeroInt(m map[string]any, keys ...string) int {
	for _, k := range keys {
		if v := intField(m, k); v != 0 {
			return v
		}
	}
	return 0
}

func intField(m map[string]any, key string) int {
	v, ok := m[key]
	if !ok {
		return 0
	}
	if f, ok := v.(float64); ok {
		return int(f)
	}
	return 0
}
