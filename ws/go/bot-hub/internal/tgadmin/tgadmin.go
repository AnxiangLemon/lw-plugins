package tgadmin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
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

const (
	maxCachedMsgSenders = 2000
	defaultMaxAttempts  = 3
)

type pendingVerify struct {
	accountID   string
	chatID      int64
	chatTitle   string
	userID      int64
	name        string
	msgID       int64
	answer      int
	attempts    int
	maxAttempts int
	timeoutSec  int
	kickOnFail  bool
	cancel      context.CancelFunc
}

type adminCacheEntry struct {
	admins  map[int64]bool
	expires time.Time
}

type msgSenderInfo struct {
	UserID int64
	Name   string
	Time   int64
}

// ChatState 记录已发现的 Telegram 群聊及机器人在该群的管理员权限状态。
type ChatState struct {
	ChatID         string `json:"chat_id"`
	Title          string `json:"title"`
	AccountID      string `json:"account_id"`
	BotStatus      string `json:"bot_status"` // "administrator" | "creator" | "member" | "restricted" | "left" | "kicked" | ""
	CanRestrict    bool   `json:"can_restrict"`
	CanDelete      bool   `json:"can_delete"`
	CanInvite      bool   `json:"can_invite"`
	LastSeen       int64  `json:"last_seen"`
	PermsCheckedAt int64  `json:"perms_checked_at"`
}

// MutedMember 记录当前处于禁言/受限状态的群成员，便于在 Web 面板一键解禁。
type MutedMember struct {
	AccountID string `json:"account_id"`
	ChatID    string `json:"chat_id"`
	ChatTitle string `json:"chat_title"`
	UserID    int64  `json:"user_id"`
	Name      string `json:"name"`
	Reason    string `json:"reason"`
	MutedAt   int64  `json:"muted_at"`
	Until     int64  `json:"until"` // 0 表示不限时（如验证失败或手动永久禁言）
}

// WarnRecord 记录群成员的累计警告状态。
type WarnRecord struct {
	AccountID  string `json:"account_id"`
	ChatID     string `json:"chat_id"`
	ChatTitle  string `json:"chat_title"`
	UserID     int64  `json:"user_id"`
	Name       string `json:"name"`
	Count      int    `json:"count"`
	LastReason string `json:"last_reason"`
	UpdatedAt  int64  `json:"updated_at"`
}

// legacyBlacklistEntry 仅用于兼容迁移旧版 tg_cache.json 中的黑名单记录。
type legacyBlacklistEntry struct {
	AccountID string `json:"account_id"`
	ChatID    string `json:"chat_id"`
	ChatTitle string `json:"chat_title"`
	UserID    int64  `json:"user_id"`
	Name      string `json:"name"`
	Reason    string `json:"reason"`
	AddedAt   int64  `json:"added_at"`
}

// StateSnapshot 提供给 Web 面板的 TG 群管运行态快照。
type StateSnapshot struct {
	Chats    []ChatState   `json:"chats"`
	Muted    []MutedMember `json:"muted"`
	Warnings []WarnRecord  `json:"warnings"`
}

type tgDiskCache struct {
	Chats     map[string]ChatState            `json:"chats"`
	Muted     map[string]MutedMember          `json:"muted"`
	Warnings  map[string]WarnRecord           `json:"warnings"`
	Blacklist map[string]legacyBlacklistEntry `json:"blacklist,omitempty"`
}

type tgEntity struct {
	Type string `json:"type"`
}

// Module Telegram 群管模块：
// - 进群人机验证（先禁言 → 算术内联按钮 → 答错立即换题并限制次数 → 通过解禁 / 失败或超时踢出或保持禁言）
// - 入群欢迎 & 退群提示（群内发送，支持自定义模板 + 自动撤回防刷屏）
// - 进群审批（chat_join_request）
// - 外链（含隐藏超链接）与违禁词拦截 + 自动删消息 + 警告累计禁言/踢人
// - 三重解禁：Web 面板一键解禁、群内禁言消息内联解禁按钮、管理员回复消息 /unmute
// - 自动监听 my_chat_member 发现群聊并记录机器人管理员权限
type Module struct {
	cfgMgr    *config.Manager
	store     *store.Store
	bl        *blacklist.Store
	cachePath string

	mu                 sync.Mutex
	client             *lwplugin.Client
	verifies           map[string]*pendingVerify // key: chatID:userID
	adminCache         map[string]adminCacheEntry
	msgSenders         map[string]msgSenderInfo // key: chatID:messageID
	chats              map[string]ChatState     // key: chatID
	muted              map[string]MutedMember   // key: chatID:userID
	warnings           map[string]WarnRecord    // key: chatID:userID
	skipBlacklistUntil map[string]int64         // key: chatID:userID -> 跳过自动拉黑截止时间戳（用于验证超时临时踢出）
}

func New(dataDir string, cfgMgr *config.Manager, st *store.Store, bl *blacklist.Store) *Module {
	_ = os.MkdirAll(dataDir, 0o755)
	if bl == nil {
		bl = blacklist.New(dataDir)
	}
	m := &Module{
		cfgMgr:             cfgMgr,
		store:              st,
		bl:                 bl,
		cachePath:          filepath.Join(dataDir, "tg_cache.json"),
		verifies:           make(map[string]*pendingVerify),
		adminCache:         make(map[string]adminCacheEntry),
		msgSenders:         make(map[string]msgSenderInfo),
		chats:              make(map[string]ChatState),
		muted:              make(map[string]MutedMember),
		warnings:           make(map[string]WarnRecord),
		skipBlacklistUntil: make(map[string]int64),
	}
	m.loadCache()
	return m
}

func (m *Module) SetClient(c *lwplugin.Client) {
	m.mu.Lock()
	m.client = c
	m.mu.Unlock()
}

func (m *Module) getClient() *lwplugin.Client {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.client
}

func (m *Module) loadCache() {
	raw, err := os.ReadFile(m.cachePath)
	if err != nil {
		return
	}
	var disk tgDiskCache
	if json.Unmarshal(raw, &disk) != nil {
		return
	}
	if disk.Chats != nil {
		m.chats = disk.Chats
		for _, ch := range disk.Chats {
			if ch.BotStatus != "left" && ch.BotStatus != "kicked" {
				m.store.TouchChat("telegram", ch.AccountID, ch.ChatID, ch.Title)
			}
		}
	}
	if disk.Muted != nil {
		m.muted = disk.Muted
	}
	if disk.Warnings != nil {
		m.warnings = disk.Warnings
	}
	// 自动平滑迁移旧版 tg_cache.json 中的黑名单到全平台公共 blacklist.Store
	if len(disk.Blacklist) > 0 {
		for _, old := range disk.Blacklist {
			if old.UserID <= 0 {
				continue
			}
			m.bl.Add(blacklist.Entry{
				Platform:        "telegram",
				UserID:          strconv.FormatInt(old.UserID, 10),
				UserName:        old.Name,
				Reason:          old.Reason,
				SourceAccountID: old.AccountID,
				SourceChatID:    old.ChatID,
				SourceChatTitle: old.ChatTitle,
				AddedAt:         old.AddedAt,
			})
		}
		m.saveCacheLocked()
	}
}

func (m *Module) saveCacheLocked() {
	disk := tgDiskCache{
		Chats:    m.chats,
		Muted:    m.muted,
		Warnings: m.warnings,
	}
	raw, err := json.MarshalIndent(disk, "", "  ")
	if err != nil {
		return
	}
	tmp := m.cachePath + ".tmp"
	if os.WriteFile(tmp, raw, 0o600) == nil {
		_ = os.Rename(tmp, m.cachePath)
	}
}

// StateSnapshot 返回供 Web 面板渲染的 TG 群管运行态快照。
func (m *Module) StateSnapshot() StateSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now().Unix()
	chats := make([]ChatState, 0, len(m.chats))
	for _, c := range m.chats {
		if c.BotStatus == "left" || c.BotStatus == "kicked" {
			continue
		}
		chats = append(chats, c)
	}
	sort.Slice(chats, func(i, j int) bool {
		aAdmin := chats[i].BotStatus == "administrator" || chats[i].BotStatus == "creator"
		bAdmin := chats[j].BotStatus == "administrator" || chats[j].BotStatus == "creator"
		if aAdmin != bAdmin {
			return aAdmin
		}
		if chats[i].LastSeen != chats[j].LastSeen {
			return chats[i].LastSeen > chats[j].LastSeen
		}
		return chats[i].ChatID < chats[j].ChatID
	})

	muted := make([]MutedMember, 0, len(m.muted))
	pruned := false
	for k, item := range m.muted {
		if item.Until > 0 && item.Until <= now {
			delete(m.muted, k)
			pruned = true
			continue
		}
		muted = append(muted, item)
	}
	if pruned {
		m.saveCacheLocked()
	}
	sort.Slice(muted, func(i, j int) bool {
		return muted[i].MutedAt > muted[j].MutedAt
	})

	warnings := make([]WarnRecord, 0, len(m.warnings))
	for _, w := range m.warnings {
		if w.Count > 0 {
			warnings = append(warnings, w)
		}
	}
	sort.Slice(warnings, func(i, j int) bool {
		return warnings[i].UpdatedAt > warnings[j].UpdatedAt
	})

	return StateSnapshot{
		Chats:    chats,
		Muted:    muted,
		Warnings: warnings,
	}
}

// RegisterCommands 为在线 TG Bot 注册左下角斜杠命令菜单。
func (m *Module) RegisterCommands(ctx context.Context, c *lwplugin.Client, accountID string) {
	commands := []map[string]string{
		{"command": "panel", "description": "打开群管内联控制面板（管理员）"},
		{"command": "warn", "description": "回复某人消息并警告一次"},
		{"command": "unwarn", "description": "回复某人消息并清零警告"},
		{"command": "mute", "description": "回复某人消息并禁言（如 /mute 30）"},
		{"command": "unmute", "description": "回复某人消息并解除禁言"},
		{"command": "ban", "description": "回复某人消息并移出群聊"},
	}
	_ = c.Telegram(accountID).SetMyCommands(ctx, commands, nil)
}

// HandleEvent 统一分发 Telegram 群管相关事件。
func (m *Module) HandleEvent(ctx context.Context, c *lwplugin.Client, ev lwplugin.Event) {
	switch ev.Name {
	case lwplugin.GenericMessageReceived:
		if ev.Message != nil && ev.Message.Platform == "telegram" {
			m.handleMessage(ctx, c, ev)
		}
	case lwplugin.TelegramCallbackQuery:
		m.handleCallbackQuery(ctx, c, ev)
	case lwplugin.TelegramChatMember:
		m.handleChatMember(ctx, c, ev)
	case lwplugin.TelegramMyChatMember:
		m.handleMyChatMember(ev)
	case lwplugin.TelegramChatJoinRequest:
		m.handleJoinRequest(ctx, c, ev)
	}
}

// CheckBotPermissions 主动查询指定 TG 群的群名及机器人在该群的管理员权限。
func (m *Module) CheckBotPermissions(ctx context.Context, accountID, chatIDStr string) (ChatState, error) {
	c := m.getClient()
	if c == nil {
		return ChatState{}, errors.New("宿主尚未连接")
	}
	chatID, err := strconv.ParseInt(strings.TrimSpace(chatIDStr), 10, 64)
	if err != nil || chatID == 0 {
		return ChatState{}, errors.New("无效的 Telegram 群 ID")
	}
	accID := m.resolveBotAccount(accountID, chatIDStr)
	if accID == "" {
		return ChatState{}, errors.New("没有可用的在线 Telegram 机器人账号")
	}
	tg := c.Telegram(accID)

	var me struct {
		ID int64 `json:"id"`
	}
	if err := tg.Call(ctx, "getMe", nil, &me); err != nil || me.ID == 0 {
		return ChatState{}, fmt.Errorf("获取机器人身份失败: %v", err)
	}

	var chatInfo struct {
		ID    int64  `json:"id"`
		Title string `json:"title"`
	}
	_ = tg.Call(ctx, "getChat", map[string]any{"chat_id": chatID}, &chatInfo)

	var member struct {
		Status              string `json:"status"`
		CanRestrictMembers  bool   `json:"can_restrict_members"`
		CanDeleteMessages   bool   `json:"can_delete_messages"`
		CanInviteUsers      bool   `json:"can_invite_users"`
	}
	if err := tg.Call(ctx, "getChatMember", map[string]any{
		"chat_id": chatID,
		"user_id": me.ID,
	}, &member); err != nil {
		return ChatState{}, fmt.Errorf("查询机器人群权限失败（请确认机器人已加入该群）: %v", err)
	}

	now := time.Now().Unix()
	m.mu.Lock()
	cs := m.chats[chatIDStr]
	cs.ChatID = chatIDStr
	cs.AccountID = accID
	if strings.TrimSpace(chatInfo.Title) != "" {
		cs.Title = strings.TrimSpace(chatInfo.Title)
	}
	cs.BotStatus = member.Status
	cs.CanRestrict = member.Status == "creator" || member.CanRestrictMembers
	cs.CanDelete = member.Status == "creator" || member.CanDeleteMessages
	cs.CanInvite = member.Status == "creator" || member.CanInviteUsers
	cs.LastSeen = now
	cs.PermsCheckedAt = now
	m.chats[chatIDStr] = cs
	m.saveCacheLocked()
	m.mu.Unlock()

	if cs.Title != "" {
		m.store.TouchChat("telegram", accID, chatIDStr, cs.Title)
	}
	return cs, nil
}

// WebAction 从 Web 面板对指定 TG 群成员执行解禁、禁言、移出或清零警告。
func (m *Module) WebAction(ctx context.Context, accountID, chatIDStr string, userID int64, action string, minutes int) error {
	c := m.getClient()
	if c == nil {
		return errors.New("宿主尚未连接")
	}
	chatID, err := strconv.ParseInt(strings.TrimSpace(chatIDStr), 10, 64)
	if err != nil || chatID == 0 {
		return errors.New("无效的 Telegram 群 ID")
	}
	if userID <= 0 {
		return errors.New("请提供有效的 Telegram 用户 ID（纯数字）")
	}
	accID := m.resolveBotAccount(accountID, chatIDStr)
	if accID == "" {
		return errors.New("没有可用的在线 Telegram 机器人账号")
	}
	tg := c.Telegram(accID)
	chatTitle := m.chatTitleOf(chatIDStr)

	switch action {
	case "unmute":
		if err := tg.RestrictChatMember(ctx, chatID, userID, fullPermissions(), 0); err != nil {
			return fmt.Errorf("解禁失败（请确认机器人是管理员且具备限制成员权限）: %v", err)
		}
		m.clearVerifyAndMuted(ctx, tg, chatID, userID)
		m.store.IncAdminAction()
		m.store.AddLog(store.LogEntry{
			Category: "tg_admin", Platform: "telegram", AccountID: accID, ChatID: chatIDStr, ChatTitle: chatTitle,
			Summary: fmt.Sprintf("通过 Web 面板解除用户 %d 禁言", userID),
		})
		return nil

	case "mute":
		if minutes <= 0 {
			minutes = 60
		}
		until := time.Now().Add(time.Duration(minutes) * time.Minute).Unix()
		if err := tg.RestrictChatMember(ctx, chatID, userID, mutedPermissions(), until); err != nil {
			return fmt.Errorf("禁言失败（请确认机器人是管理员且具备限制成员权限）: %v", err)
		}
		m.recordMuted(accID, chatIDStr, chatTitle, userID, strconv.FormatInt(userID, 10), fmt.Sprintf("Web 面板禁言 %d 分钟", minutes), until)
		m.store.IncAdminAction()
		m.store.AddLog(store.LogEntry{
			Category: "tg_admin", Platform: "telegram", AccountID: accID, ChatID: chatIDStr, ChatTitle: chatTitle,
			Summary: fmt.Sprintf("通过 Web 面板禁言用户 %d（%d 分钟）", userID, minutes),
		})
		return nil

	case "ban":
		if err := tg.BanChatMember(ctx, chatID, userID, 0, false); err != nil {
			return fmt.Errorf("移出失败（请确认机器人是管理员且具备封禁权限）: %v", err)
		}
		m.clearVerifyAndMuted(ctx, tg, chatID, userID)
		if rule, ok := m.cfgMgr.ResolveTelegramChat(chatIDStr); ok && rule.BlacklistEnabled {
			m.addBlacklist(accID, chatIDStr, chatTitle, userID, strconv.FormatInt(userID, 10), "管理员通过 Web 面板移出并拉黑")
		}
		m.store.IncAdminAction()
		m.store.AddLog(store.LogEntry{
			Category: "tg_admin", Platform: "telegram", AccountID: accID, ChatID: chatIDStr, ChatTitle: chatTitle,
			Summary: fmt.Sprintf("通过 Web 面板移出用户 %d", userID),
		})
		return nil

	case "unwarn":
		m.resetWarn(chatIDStr, userID)
		m.store.AddLog(store.LogEntry{
			Category: "tg_admin", Platform: "telegram", AccountID: accID, ChatID: chatIDStr, ChatTitle: chatTitle,
			Summary: fmt.Sprintf("通过 Web 面板清零用户 %d 的警告记录", userID),
		})
		return nil

	default:
		return errors.New("不支持的操作类型: " + action)
	}
}

// BanUserAcrossKnownChats 当管理员在公共黑名单页手动拉黑 TG 用户时，尝试在所有已管理的 TG 群中静默移出该成员。
func (m *Module) BanUserAcrossKnownChats(ctx context.Context, userID int64) int {
	c := m.getClient()
	if c == nil || userID <= 0 {
		return 0
	}
	m.mu.Lock()
	snapshot := make([]ChatState, 0, len(m.chats))
	for _, cs := range m.chats {
		if cs.BotStatus == "administrator" || cs.BotStatus == "creator" || cs.BotStatus == "" {
			snapshot = append(snapshot, cs)
		}
	}
	m.mu.Unlock()

	userIDStr := strconv.FormatInt(userID, 10)
	kicked := 0
	for _, cs := range snapshot {
		cid, err := strconv.ParseInt(cs.ChatID, 10, 64)
		if err != nil || cid == 0 {
			continue
		}
		accID := m.resolveBotAccount(cs.AccountID, cs.ChatID)
		if accID == "" {
			continue
		}
		tg := c.Telegram(accID)
		if err := tg.BanChatMember(ctx, cid, userID, 0, false); err == nil {
			m.bl.RecordBannedChat("telegram", userIDStr, cs.ChatID)
			m.clearVerifyAndMuted(ctx, tg, cid, userID)
			kicked++
		}
	}
	return kicked
}

// UnbanUserAcrossChats 移出全局黑名单时，对记录中曾执行过封禁的 TG 群逐一解除封禁，使其可以重新加群。
func (m *Module) UnbanUserAcrossChats(ctx context.Context, userID int64, bannedChats []string) {
	c := m.getClient()
	if c == nil || userID <= 0 {
		return
	}
	seen := make(map[string]bool, len(bannedChats))
	for _, cidStr := range bannedChats {
		cidStr = strings.TrimSpace(cidStr)
		if cidStr == "" || seen[cidStr] {
			continue
		}
		seen[cidStr] = true
		cid, err := strconv.ParseInt(cidStr, 10, 64)
		if err != nil || cid == 0 {
			continue
		}
		accID := m.resolveBotAccount("", cidStr)
		if accID == "" {
			continue
		}
		_ = c.Telegram(accID).UnbanChatMember(ctx, cid, userID, true)
	}
}

func (m *Module) resolveBotAccount(explicitAccountID, chatIDStr string) string {
	if strings.TrimSpace(explicitAccountID) != "" {
		return strings.TrimSpace(explicitAccountID)
	}
	m.mu.Lock()
	if cs, ok := m.chats[chatIDStr]; ok && cs.AccountID != "" {
		acc := cs.AccountID
		m.mu.Unlock()
		return acc
	}
	m.mu.Unlock()

	for _, acc := range m.store.Accounts() {
		if acc.Platform == "telegram" && acc.Online {
			return acc.ID
		}
	}
	return ""
}

func (m *Module) chatTitleOf(chatIDStr string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if cs, ok := m.chats[chatIDStr]; ok && cs.Title != "" {
		return cs.Title
	}
	return chatIDStr
}

func (m *Module) touchChat(accountID, chatIDStr, title string) {
	if chatIDStr == "" {
		return
	}
	m.store.TouchChat("telegram", accountID, chatIDStr, title)
	m.mu.Lock()
	cs := m.chats[chatIDStr]
	changed := cs.ChatID == "" || (title != "" && title != cs.Title) || (accountID != "" && accountID != cs.AccountID)
	cs.ChatID = chatIDStr
	if accountID != "" {
		cs.AccountID = accountID
	}
	if strings.TrimSpace(title) != "" {
		cs.Title = strings.TrimSpace(title)
	}
	cs.LastSeen = time.Now().Unix()
	m.chats[chatIDStr] = cs
	if changed {
		m.saveCacheLocked()
	}
	m.mu.Unlock()
}

func (m *Module) handleMyChatMember(ev lwplugin.Event) {
	var payload struct {
		MyChatMember struct {
			Chat struct {
				ID    int64  `json:"id"`
				Title string `json:"title"`
				Type  string `json:"type"`
			} `json:"chat"`
			NewChatMember struct {
				Status             string `json:"status"`
				CanRestrictMembers bool   `json:"can_restrict_members"`
				CanDeleteMessages  bool   `json:"can_delete_messages"`
				CanInviteUsers     bool   `json:"can_invite_users"`
			} `json:"new_chat_member"`
		} `json:"my_chat_member"`
	}
	if err := json.Unmarshal(ev.Data, &payload); err != nil || payload.MyChatMember.Chat.ID == 0 {
		return
	}
	mcm := payload.MyChatMember
	if mcm.Chat.Type != "group" && mcm.Chat.Type != "supergroup" {
		return
	}
	chatIDStr := strconv.FormatInt(mcm.Chat.ID, 10)
	now := time.Now().Unix()
	st := mcm.NewChatMember

	m.mu.Lock()
	cs := m.chats[chatIDStr]
	cs.ChatID = chatIDStr
	cs.AccountID = ev.Account.ID
	if strings.TrimSpace(mcm.Chat.Title) != "" {
		cs.Title = strings.TrimSpace(mcm.Chat.Title)
	}
	cs.BotStatus = st.Status
	cs.CanRestrict = st.Status == "creator" || st.CanRestrictMembers
	cs.CanDelete = st.Status == "creator" || st.CanDeleteMessages
	cs.CanInvite = st.Status == "creator" || st.CanInviteUsers
	cs.LastSeen = now
	cs.PermsCheckedAt = now
	m.chats[chatIDStr] = cs
	m.saveCacheLocked()
	m.mu.Unlock()

	if st.Status != "left" && st.Status != "kicked" {
		m.store.TouchChat("telegram", ev.Account.ID, chatIDStr, cs.Title)
	}
	roleLabel := map[string]string{
		"administrator": "管理员",
		"creator":       "群主",
		"member":        "普通成员",
		"restricted":    "受限成员",
		"left":          "已退出群聊",
		"kicked":        "已被移出群聊",
	}[st.Status]
	if roleLabel == "" {
		roleLabel = st.Status
	}
	m.store.AddLog(store.LogEntry{
		Category: "tg_admin", Platform: "telegram", AccountID: ev.Account.ID,
		ChatID: chatIDStr, ChatTitle: cs.Title,
		Summary: fmt.Sprintf("机器人群内身份变更：%s", roleLabel),
	})
}

func (m *Module) cacheMsgSender(chatID, msgID, userID int64, name string) {
	if chatID == 0 || msgID <= 0 || userID <= 0 {
		return
	}
	key := fmt.Sprintf("%d:%d", chatID, msgID)
	m.mu.Lock()
	if len(m.msgSenders) >= maxCachedMsgSenders {
		cutoff := time.Now().Add(-30 * time.Minute).Unix()
		for k, v := range m.msgSenders {
			if v.Time < cutoff {
				delete(m.msgSenders, k)
			}
		}
		if len(m.msgSenders) >= maxCachedMsgSenders {
			m.msgSenders = make(map[string]msgSenderInfo, 256)
		}
	}
	m.msgSenders[key] = msgSenderInfo{UserID: userID, Name: name, Time: time.Now().Unix()}
	m.mu.Unlock()
}

func (m *Module) lookupMsgSender(chatID, msgID int64) (msgSenderInfo, bool) {
	if chatID == 0 || msgID <= 0 {
		return msgSenderInfo{}, false
	}
	key := fmt.Sprintf("%d:%d", chatID, msgID)
	m.mu.Lock()
	defer m.mu.Unlock()
	info, ok := m.msgSenders[key]
	return info, ok
}

func (m *Module) handleMessage(ctx context.Context, c *lwplugin.Client, ev lwplugin.Event) {
	msg := ev.Message
	if !msg.IsGroup {
		return
	}
	var raw struct {
		TG struct {
			MessageID          int64      `json:"message_id"`
			ChatID             int64      `json:"chat_id"`
			UserID             int64      `json:"user_id"`
			Username           string     `json:"username"`
			DisplayName        string     `json:"display_name"`
			ChatTitle          string     `json:"chat_title"`
			ReplyToMessageID   int64      `json:"reply_to_message_id"`
			ReplyToUserID      int64      `json:"reply_to_user_id"`
			ReplyToUsername    string     `json:"reply_to_username"`
			ReplyToDisplayName string     `json:"reply_to_display_name"`
			BotUserID          int64      `json:"bot_user_id"`
			Entities           []tgEntity `json:"entities"`
		} `json:"tg"`
	}
	if err := json.Unmarshal(msg.Raw, &raw); err != nil || raw.TG.ChatID == 0 {
		return
	}
	chatID := raw.TG.ChatID
	chatIDStr := strconv.FormatInt(chatID, 10)
	senderName := displayName(raw.TG.DisplayName, raw.TG.Username, raw.TG.UserID)

	m.touchChat(ev.Account.ID, chatIDStr, raw.TG.ChatTitle)
	m.cacheMsgSender(chatID, raw.TG.MessageID, raw.TG.UserID, senderName)

	text := strings.TrimSpace(msg.Text)
	if text == "" && len(raw.TG.Entities) == 0 {
		return
	}

	// 1. 管理员斜杠命令
	if strings.HasPrefix(text, "/") {
		replySender := msgSenderInfo{
			UserID: raw.TG.ReplyToUserID,
			Name:   displayName(raw.TG.ReplyToDisplayName, raw.TG.ReplyToUsername, raw.TG.ReplyToUserID),
		}
		m.handleAdminCommand(ctx, c, ev.Account.ID, chatID, raw.TG.ChatTitle, raw.TG.UserID, raw.TG.BotUserID, raw.TG.MessageID, raw.TG.ReplyToMessageID, replySender, text)
		return
	}

	// 2. 群内容拦截（违禁词 / 外链）
	rule, ok := m.cfgMgr.ResolveTelegramChat(chatIDStr)
	if !ok || !rule.Enabled {
		return
	}
	if m.isAdmin(ctx, c, ev.Account.ID, chatID, raw.TG.UserID) {
		return
	}

	violation := ""
	lower := strings.ToLower(text)
	if rule.BlockLinks && hasExternalLink(text, raw.TG.Entities) {
		violation = "禁止发送外部链接"
	}
	if violation == "" && text != "" {
		for _, kw := range rule.BannedKeywords {
			kw = strings.TrimSpace(kw)
			if kw != "" && strings.Contains(lower, strings.ToLower(kw)) {
				violation = "命中违禁词: " + kw
				break
			}
		}
	}
	if violation == "" {
		return
	}

	tg := c.Telegram(ev.Account.ID)
	if rule.DeleteBannedMsg && raw.TG.MessageID > 0 {
		_ = tg.DeleteMessage(ctx, chatID, raw.TG.MessageID)
	}
	m.applyWarning(ctx, tg, chatID, raw.TG.ChatTitle, raw.TG.UserID, senderName, violation, rule)
}

// hasExternalLink 检测纯文本链接及 Telegram 隐藏超链接实体（url / text_link）。
func hasExternalLink(text string, entities []tgEntity) bool {
	for _, ent := range entities {
		if ent.Type == "url" || ent.Type == "text_link" {
			return true
		}
	}
	lower := strings.ToLower(text)
	for _, needle := range []string{"http://", "https://", "t.me/", "telegram.me/", "www."} {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	return false
}

const minTelegramUserID = 10000

func (m *Module) resolveReplyTarget(chatID, replyToMsgID int64, replySender msgSenderInfo) (msgSenderInfo, bool) {
	if replySender.UserID > 0 {
		return replySender, true
	}
	if replyToMsgID > 0 {
		return m.lookupMsgSender(chatID, replyToMsgID)
	}
	return msgSenderInfo{}, false
}

func formatTGAdminError(err error) string {
	if err == nil {
		return ""
	}
	raw := err.Error()
	lower := strings.ToLower(raw)
	switch {
	case strings.Contains(lower, "not enough rights") || strings.Contains(lower, "chat_admin_required"):
		return "机器人权限不足（请确认机器人是群管理员，且开启了限制/封禁成员权限）"
	case strings.Contains(lower, "administrator") || strings.Contains(lower, "chat_owner") || strings.Contains(lower, "creator"):
		return "目标成员是群主或管理员，无法执行该操作"
	case strings.Contains(lower, "user_not_participant") || strings.Contains(lower, "participant_id_invalid") || strings.Contains(lower, "user not found"):
		return "目标用户不在本群或用户 ID 无效"
	case strings.Contains(lower, "can't restrict self") || strings.Contains(lower, "can't remove chat owner"):
		return "无法对机器人自身或群主执行该操作"
	default:
		return raw
	}
}

func (m *Module) handleAdminCommand(
	ctx context.Context,
	c *lwplugin.Client,
	accountID string,
	chatID int64,
	chatTitle string,
	senderID int64,
	botUserID int64,
	msgID int64,
	replyToMsgID int64,
	replySender msgSenderInfo,
	text string,
) {
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return
	}
	cmd := strings.ToLower(fields[0])
	if idx := strings.Index(cmd, "@"); idx > 0 {
		cmd = cmd[:idx]
	}
	if cmd != "/panel" && cmd != "/settings" && cmd != "/mute" && cmd != "/unmute" && cmd != "/ban" && cmd != "/warn" && cmd != "/unwarn" {
		return
	}
	if !m.isAdmin(ctx, c, accountID, chatID, senderID) {
		return
	}
	tg := c.Telegram(accountID)

	if cmd == "/panel" || cmd == "/settings" {
		m.sendAdminPanel(ctx, tg, chatID, chatTitle, 0)
		if msgID > 0 {
			_ = tg.DeleteMessage(ctx, chatID, msgID)
		}
		return
	}

	// 支持两种用法：
	// 1) 直接回复对方消息发命令：/unmute、/mute 30、/warn 发广告、/ban
	// 2) 显式传数字 user_id：/unmute 123456、/mute 123456 30
	var targetID int64
	var targetName string
	var extraArgs []string

	if len(fields) >= 2 {
		if v, err := strconv.ParseInt(fields[1], 10, 64); err == nil && v > 0 {
			// 如果是回复消息且命令为 /mute 30（第一个参数是较小分钟数而非 user_id），优先识别为回复目标 + 分钟数
			if replyToMsgID > 0 && cmd == "/mute" && v <= 43200 {
				if info, ok := m.resolveReplyTarget(chatID, replyToMsgID, replySender); ok {
					targetID = info.UserID
					targetName = info.Name
					extraArgs = fields[1:]
				}
			}
			// 未回复消息时，过小的数字（如 /mute 30）通常是管理员漏掉回复而误填的分钟数，避免当成 user_id 误调用
			if targetID == 0 && v >= minTelegramUserID {
				targetID = v
				targetName = strconv.FormatInt(v, 10)
				extraArgs = fields[2:]
			}
		}
	}
	if targetID == 0 && (replyToMsgID > 0 || replySender.UserID > 0) {
		if info, ok := m.resolveReplyTarget(chatID, replyToMsgID, replySender); ok {
			targetID = info.UserID
			targetName = info.Name
			extraArgs = fields[1:]
		} else {
			_, _ = tg.SendMessage(ctx, chatID, "未能识别被回复消息的发送者（可能是重启前的旧消息或频道匿名消息），请使用 <code>"+cmd+" &lt;user_id&gt;</code> 或在 Web 面板一键操作。", map[string]any{"parse_mode": "HTML"})
			return
		}
	}
	if targetID <= 0 {
		_, _ = tg.SendMessage(ctx, chatID, "用法：<b>回复某人消息</b>发送 <code>/mute 30</code> / <code>/unmute</code> / <code>/warn 原因</code> / <code>/unwarn</code> / <code>/ban</code>，或附带数字用户 ID：<code>"+cmd+" 123456789</code>。", map[string]any{"parse_mode": "HTML"})
		return
	}

	if cmd == "/mute" || cmd == "/ban" || cmd == "/warn" {
		if botUserID > 0 && targetID == botUserID {
			_, _ = tg.SendMessage(ctx, chatID, "⚠️ 不能对机器人自身执行该操作。", map[string]any{"parse_mode": "HTML"})
			return
		}
		if m.isAdmin(ctx, c, accountID, chatID, targetID) {
			_, _ = tg.SendMessage(ctx, chatID, "⚠️ 目标成员是群管理员，无法执行该处罚操作。", map[string]any{"parse_mode": "HTML"})
			return
		}
	}

	chatIDStr := strconv.FormatInt(chatID, 10)

	switch cmd {
	case "/mute":
		mins := 60
		if len(extraArgs) >= 1 {
			if v, err := strconv.Atoi(extraArgs[0]); err == nil && v > 0 {
				mins = v
			}
		}
		until := time.Now().Add(time.Duration(mins) * time.Minute).Unix()
		if err := tg.RestrictChatMember(ctx, chatID, targetID, mutedPermissions(), until); err != nil {
			_, _ = tg.SendMessage(ctx, chatID, "❌ 禁言失败："+escapeHTML(formatTGAdminError(err)), map[string]any{"parse_mode": "HTML"})
			return
		}
		m.recordMuted(accountID, chatIDStr, chatTitle, targetID, targetName, fmt.Sprintf("管理员手动禁言 %d 分钟", mins), until)
		m.store.IncAdminAction()
		m.store.AddLog(store.LogEntry{
			Category: "tg_admin", Platform: "telegram", AccountID: accountID, ChatID: chatIDStr, ChatTitle: chatTitle,
			Summary: fmt.Sprintf("管理员禁言用户 %s (%d)（%d 分钟）", targetName, targetID, mins),
		})
		_, _ = tg.SendMessage(ctx, chatID, fmt.Sprintf("🔇 已禁言 <b>%s</b>（<code>%d</code>，%d 分钟）。", escapeHTML(targetName), targetID, mins), map[string]any{
			"parse_mode":   "HTML",
			"reply_markup": unmuteInlineMarkup(targetID),
		})
	case "/unmute":
		if err := tg.RestrictChatMember(ctx, chatID, targetID, fullPermissions(), 0); err != nil {
			_, _ = tg.SendMessage(ctx, chatID, "❌ 解禁失败："+escapeHTML(formatTGAdminError(err)), map[string]any{"parse_mode": "HTML"})
			return
		}
		m.clearVerifyAndMuted(ctx, tg, chatID, targetID)
		m.store.IncAdminAction()
		m.store.AddLog(store.LogEntry{
			Category: "tg_admin", Platform: "telegram", AccountID: accountID, ChatID: chatIDStr, ChatTitle: chatTitle,
			Summary: fmt.Sprintf("管理员解除用户 %s (%d) 禁言", targetName, targetID),
		})
		_, _ = tg.SendMessage(ctx, chatID, fmt.Sprintf("🔊 已解除 <b>%s</b>（<code>%d</code>）禁言。", escapeHTML(targetName), targetID), map[string]any{"parse_mode": "HTML"})
	case "/ban":
		if err := tg.BanChatMember(ctx, chatID, targetID, 0, false); err != nil {
			_, _ = tg.SendMessage(ctx, chatID, "❌ 移出失败："+escapeHTML(formatTGAdminError(err)), map[string]any{"parse_mode": "HTML"})
			return
		}
		m.clearVerifyAndMuted(ctx, tg, chatID, targetID)
		blSuffix := ""
		if rule, ok := m.cfgMgr.ResolveTelegramChat(chatIDStr); ok && rule.BlacklistEnabled {
			m.addBlacklist(accountID, chatIDStr, chatTitle, targetID, targetName, "管理员通过 /ban 移出并拉黑")
			blSuffix = "（已加入黑名单）"
		}
		m.store.IncAdminAction()
		m.store.AddLog(store.LogEntry{
			Category: "tg_admin", Platform: "telegram", AccountID: accountID, ChatID: chatIDStr, ChatTitle: chatTitle,
			Summary: fmt.Sprintf("管理员移出用户 %s (%d)%s", targetName, targetID, blSuffix),
		})
		_, _ = tg.SendMessage(ctx, chatID, fmt.Sprintf("🚫 已移出 <b>%s</b>（<code>%d</code>）%s。", escapeHTML(targetName), targetID, blSuffix), map[string]any{"parse_mode": "HTML"})
	case "/warn":
		rule, _ := m.cfgMgr.ResolveTelegramChat(chatIDStr)
		reason := "管理员手动警告"
		if len(extraArgs) >= 1 {
			reason = strings.Join(extraArgs, " ")
		}
		m.applyWarning(ctx, tg, chatID, chatTitle, targetID, targetName, reason, rule)
	case "/unwarn":
		m.resetWarn(chatIDStr, targetID)
		_, _ = tg.SendMessage(ctx, chatID, fmt.Sprintf("✅ 已清零 <b>%s</b>（<code>%d</code>）的警告记录。", escapeHTML(targetName), targetID), map[string]any{"parse_mode": "HTML"})
	}
}

func (m *Module) handleChatMember(ctx context.Context, c *lwplugin.Client, ev lwplugin.Event) {
	var payload struct {
		ChatMember struct {
			Chat struct {
				ID    int64  `json:"id"`
				Title string `json:"title"`
			} `json:"chat"`
			OldChatMember struct {
				Status string `json:"status"`
			} `json:"old_chat_member"`
			NewChatMember struct {
				Status string `json:"status"`
				User   struct {
					ID        int64  `json:"id"`
					IsBot     bool   `json:"is_bot"`
					FirstName string `json:"first_name"`
					Username  string `json:"username"`
				} `json:"user"`
			} `json:"new_chat_member"`
		} `json:"chat_member"`
	}
	if err := json.Unmarshal(ev.Data, &payload); err != nil || payload.ChatMember.Chat.ID == 0 {
		return
	}
	cm := payload.ChatMember
	if cm.NewChatMember.User.IsBot {
		return
	}
	chatID := cm.Chat.ID
	chatIDStr := strconv.FormatInt(chatID, 10)
	m.touchChat(ev.Account.ID, chatIDStr, cm.Chat.Title)

	rule, ok := m.cfgMgr.ResolveTelegramChat(chatIDStr)
	if !ok || !rule.Enabled {
		return
	}

	oldSt := cm.OldChatMember.Status
	newSt := cm.NewChatMember.Status
	user := cm.NewChatMember.User
	name := displayName(user.FirstName, user.Username, user.ID)
	tg := c.Telegram(ev.Account.ID)

	joined := (oldSt == "left" || oldSt == "kicked") && (newSt == "member" || newSt == "restricted")
	left := (oldSt == "member" || oldSt == "restricted" || oldSt == "administrator") && (newSt == "left" || newSt == "kicked")

	if joined {
		// 全平台黑名单静默兜底拦截：一处拉黑、处处受阻；静默移出不发群消息，仅记日志
		if rule.BlacklistEnabled {
			if bl, blocked := m.isBlacklisted(user.ID); blocked {
				_ = tg.BanChatMember(ctx, chatID, user.ID, 0, false)
				m.bl.RecordBannedChat("telegram", strconv.FormatInt(user.ID, 10), chatIDStr)
				m.clearVerifyAndMuted(ctx, tg, chatID, user.ID)
				m.store.IncAdminAction()
				m.store.AddLog(store.LogEntry{
					Category: "tg_admin", Platform: "telegram", AccountID: ev.Account.ID,
					ChatID: chatIDStr, ChatTitle: cm.Chat.Title,
					Summary: fmt.Sprintf("黑名单静默拦截：已自动移出再次入群成员 %s (%d)", name, user.ID),
					Detail:  fmt.Sprintf("拉黑原因：%s", bl.Reason),
				})
				return
			}
		}
		m.store.AddLog(store.LogEntry{
			Category: "tg_admin", Platform: "telegram", AccountID: ev.Account.ID,
			ChatID: chatIDStr, ChatTitle: cm.Chat.Title,
			Summary: fmt.Sprintf("新成员入群: %s (%d)", name, user.ID),
		})
		if rule.VerifyEnabled {
			m.startVerification(ctx, c, ev.Account.ID, chatID, cm.Chat.Title, user.ID, name, rule)
			return
		}
		if rule.WelcomeEnabled {
			m.sendWelcome(ctx, tg, chatID, cm.Chat.Title, name, rule)
		}
	} else if left {
		// 成员离群时自动清理其未完成的验证与受限记录
		m.clearVerifyAndMuted(ctx, tg, chatID, user.ID)
		if rule.BlacklistEnabled && oldSt != "administrator" && oldSt != "creator" {
			if newSt == "kicked" {
				if !m.shouldSkipBlacklist(chatIDStr, user.ID) {
					if _, exists := m.isBlacklisted(user.ID); !exists {
						m.addBlacklist(ev.Account.ID, chatIDStr, cm.Chat.Title, user.ID, name, "被移出群聊自动拉黑")
						m.store.AddLog(store.LogEntry{
							Category: "tg_admin", Platform: "telegram", AccountID: ev.Account.ID,
							ChatID: chatIDStr, ChatTitle: cm.Chat.Title,
							Summary: fmt.Sprintf("成员 %s (%d) 被移出群聊，已自动加入全局黑名单", name, user.ID),
						})
					} else {
						m.bl.RecordBannedChat("telegram", strconv.FormatInt(user.ID, 10), chatIDStr)
					}
				}
			} else if newSt == "left" && rule.BlacklistOnLeave {
				if _, exists := m.isBlacklisted(user.ID); !exists {
					m.addBlacklist(ev.Account.ID, chatIDStr, cm.Chat.Title, user.ID, name, "主动退出群聊自动拉黑")
					m.store.AddLog(store.LogEntry{
						Category: "tg_admin", Platform: "telegram", AccountID: ev.Account.ID,
						ChatID: chatIDStr, ChatTitle: cm.Chat.Title,
						Summary: fmt.Sprintf("成员 %s (%d) 主动退出群聊，已自动加入全局黑名单", name, user.ID),
					})
				}
			}
		}
		if rule.GoodbyeEnabled {
			m.sendGoodbye(ctx, tg, chatID, cm.Chat.Title, name, rule)
		}
	}
}

func (m *Module) handleJoinRequest(ctx context.Context, c *lwplugin.Client, ev lwplugin.Event) {
	var payload struct {
		Req struct {
			Chat struct {
				ID    int64  `json:"id"`
				Title string `json:"title"`
			} `json:"chat"`
			From struct {
				ID        int64  `json:"id"`
				FirstName string `json:"first_name"`
				Username  string `json:"username"`
			} `json:"from"`
		} `json:"chat_join_request"`
	}
	if err := json.Unmarshal(ev.Data, &payload); err != nil || payload.Req.Chat.ID == 0 {
		return
	}
	chatID := payload.Req.Chat.ID
	chatIDStr := strconv.FormatInt(chatID, 10)
	m.touchChat(ev.Account.ID, chatIDStr, payload.Req.Chat.Title)

	rule, ok := m.cfgMgr.ResolveTelegramChat(chatIDStr)
	if !ok || !rule.Enabled {
		return
	}
	reqName := displayName(payload.Req.From.FirstName, payload.Req.From.Username, payload.Req.From.ID)

	// 黑名单优先于自动审群：全群通用黑名单成员申请入群直接静默拒绝，仅记日志
	if rule.BlacklistEnabled {
		if bl, blocked := m.isBlacklisted(payload.Req.From.ID); blocked {
			if err := c.Telegram(ev.Account.ID).DeclineChatJoinRequest(ctx, chatID, payload.Req.From.ID); err == nil {
				m.store.IncAdminAction()
				m.store.AddLog(store.LogEntry{
					Category: "tg_admin", Platform: "telegram", AccountID: ev.Account.ID,
					ChatID: chatIDStr, ChatTitle: payload.Req.Chat.Title,
					Summary: fmt.Sprintf("黑名单静默拦截：已自动拒绝入群申请 %s (%d)", reqName, payload.Req.From.ID),
					Detail:  fmt.Sprintf("拉黑原因：%s", bl.Reason),
				})
			}
			return
		}
	}

	if !rule.AutoApproveJoin {
		return
	}
	if err := c.Telegram(ev.Account.ID).ApproveChatJoinRequest(ctx, chatID, payload.Req.From.ID); err == nil {
		m.store.IncAdminAction()
		m.store.AddLog(store.LogEntry{
			Category: "tg_admin", Platform: "telegram", AccountID: ev.Account.ID,
			ChatID: chatIDStr, ChatTitle: payload.Req.Chat.Title,
			Summary: fmt.Sprintf("自动通过进群申请: %s (%d)", reqName, payload.Req.From.ID),
		})
	}
}

// generateMathChallenge 生成算术题与 4 个互不重复的随机选项，尽量避开 prevAnswer。
func generateMathChallenge(prevAnswer int) (a, b, ans int, options []int) {
	for range 8 {
		a = rand.Intn(12) + 2
		b = rand.Intn(12) + 2
		ans = a + b
		if ans != prevAnswer {
			break
		}
	}
	seen := map[int]bool{ans: true}
	options = []int{ans}
	offsets := []int{-3, -2, -1, 1, 2, 3, 4, -4}
	rand.Shuffle(len(offsets), func(i, j int) { offsets[i], offsets[j] = offsets[j], offsets[i] })
	for _, off := range offsets {
		if len(options) >= 4 {
			break
		}
		cand := ans + off
		if cand > 0 && !seen[cand] {
			seen[cand] = true
			options = append(options, cand)
		}
	}
	rand.Shuffle(len(options), func(i, j int) { options[i], options[j] = options[j], options[i] })
	return a, b, ans, options
}

func buildVerifyPromptAndMarkup(name string, userID int64, timeoutSec, a, b, remainAttempts, maxAttempts int, options []int) (string, map[string]any) {
	var row []map[string]any
	for _, opt := range options {
		row = append(row, map[string]any{
			"text":          strconv.Itoa(opt),
			"callback_data": fmt.Sprintf("vfy:%d:%d", userID, opt),
		})
	}
	prompt := fmt.Sprintf(
		"👋 欢迎 <b>%s</b>！\n请在 <b>%d 秒</b>内点击下方正确答案解除禁言（答错会自动换题，剩余 <b>%d/%d</b> 次机会）：\n\n<code>%d + %d = ?</code>",
		escapeHTML(name), timeoutSec, remainAttempts, maxAttempts, a, b,
	)
	markup := map[string]any{
		"inline_keyboard": [][]map[string]any{row},
	}
	return prompt, markup
}

func (m *Module) startVerification(
	ctx context.Context,
	c *lwplugin.Client,
	accountID string,
	chatID int64,
	chatTitle string,
	userID int64,
	name string,
	rule config.TelegramChatRule,
) {
	tg := c.Telegram(accountID)
	_ = tg.RestrictChatMember(ctx, chatID, userID, mutedPermissions(), 0)

	chatIDStr := strconv.FormatInt(chatID, 10)
	m.recordMuted(accountID, chatIDStr, chatTitle, userID, name, "进群人机验证中", 0)

	timeoutSec := rule.VerifyTimeoutSec
	if timeoutSec <= 0 {
		timeoutSec = 120
	}
	maxAttempts := rule.VerifyMaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = defaultMaxAttempts
	}

	a, b, ans, options := generateMathChallenge(0)
	prompt, markup := buildVerifyPromptAndMarkup(name, userID, timeoutSec, a, b, maxAttempts, maxAttempts, options)

	rawResp, err := tg.SendMessage(ctx, chatID, prompt, map[string]any{
		"parse_mode":   "HTML",
		"reply_markup": markup,
	})
	if err != nil {
		return
	}
	var sent struct {
		MessageID int64 `json:"message_id"`
	}
	_ = json.Unmarshal(rawResp, &sent)

	vCtx, cancel := context.WithCancel(context.Background())
	key := fmt.Sprintf("%d:%d", chatID, userID)
	m.mu.Lock()
	if prev, ok := m.verifies[key]; ok {
		prev.cancel()
	}
	m.verifies[key] = &pendingVerify{
		accountID:   accountID,
		chatID:      chatID,
		chatTitle:   chatTitle,
		userID:      userID,
		name:        name,
		msgID:       sent.MessageID,
		answer:      ans,
		attempts:    0,
		maxAttempts: maxAttempts,
		timeoutSec:  timeoutSec,
		kickOnFail:  rule.VerifyKickOnFail,
		cancel:      cancel,
	}
	m.mu.Unlock()

	go func() {
		timer := time.NewTimer(time.Duration(timeoutSec) * time.Second)
		defer timer.Stop()
		select {
		case <-vCtx.Done():
			return
		case <-timer.C:
			m.mu.Lock()
			pv, exists := m.verifies[key]
			if exists {
				delete(m.verifies, key)
			}
			m.mu.Unlock()
			if !exists {
				return
			}
			tCtx, tCancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer tCancel()
			m.handleVerifyFailure(tCtx, tg, pv, "验证超时")
		}
	}()
}

func (m *Module) handleVerifyFailure(ctx context.Context, tg lwplugin.Telegram, pv *pendingVerify, reason string) {
	if pv.msgID > 0 {
		_ = tg.DeleteMessage(ctx, pv.chatID, pv.msgID)
	}
	chatIDStr := strconv.FormatInt(pv.chatID, 10)
	if pv.kickOnFail {
		// 验证失败临时移出允许用户后续重新验证，不触发“被移出自动拉黑”
		m.markSkipBlacklist(chatIDStr, pv.userID, 15*time.Second)
		_ = tg.BanChatMember(ctx, pv.chatID, pv.userID, 0, false)
		_ = tg.UnbanChatMember(ctx, pv.chatID, pv.userID, true)
		m.removeMuted(chatIDStr, pv.userID)
		m.store.IncAdminAction()
		m.store.AddLog(store.LogEntry{
			Category: "tg_admin", Platform: "telegram", AccountID: pv.accountID,
			ChatID: chatIDStr, ChatTitle: pv.chatTitle,
			Summary: fmt.Sprintf("进群%s，已移出 %s (%d)", reason, pv.name, pv.userID),
		})
		return
	}
	// 不踢出时保持禁言，并更新受限原因，方便管理员在群内或 Web 面板一键解禁
	m.recordMuted(pv.accountID, chatIDStr, pv.chatTitle, pv.userID, pv.name, "进群"+reason+"（保持禁言）", 0)
	m.store.IncAdminAction()
	m.store.AddLog(store.LogEntry{
		Category: "tg_admin", Platform: "telegram", AccountID: pv.accountID,
		ChatID: chatIDStr, ChatTitle: pv.chatTitle,
		Summary: fmt.Sprintf("进群%s，%s (%d) 保持禁言", reason, pv.name, pv.userID),
	})
	_, _ = tg.SendMessage(ctx, pv.chatID, fmt.Sprintf("🔇 <b>%s</b> 进群%s，当前保持禁言状态。", escapeHTML(pv.name), escapeHTML(reason)), map[string]any{
		"parse_mode":   "HTML",
		"reply_markup": unmuteInlineMarkup(pv.userID),
	})
}

func (m *Module) handleCallbackQuery(ctx context.Context, c *lwplugin.Client, ev lwplugin.Event) {
	var payload struct {
		CQ struct {
			ID   string `json:"id"`
			Data string `json:"data"`
			From struct {
				ID        int64  `json:"id"`
				FirstName string `json:"first_name"`
				Username  string `json:"username"`
			} `json:"from"`
			Message struct {
				MessageID int64 `json:"message_id"`
				Chat      struct {
					ID    int64  `json:"id"`
					Title string `json:"title"`
				} `json:"chat"`
			} `json:"message"`
		} `json:"callback_query"`
	}
	if err := json.Unmarshal(ev.Data, &payload); err != nil || payload.CQ.ID == "" {
		return
	}
	cq := payload.CQ
	tg := c.Telegram(ev.Account.ID)
	chatID := cq.Message.Chat.ID
	chatIDStr := strconv.FormatInt(chatID, 10)

	// 1. 进群算术按钮验证（答错立即换题 + 限制尝试次数）
	if strings.HasPrefix(cq.Data, "vfy:") {
		parts := strings.Split(cq.Data, ":")
		if len(parts) != 3 {
			return
		}
		targetUser, _ := strconv.ParseInt(parts[1], 10, 64)
		choice, _ := strconv.Atoi(parts[2])
		if cq.From.ID != targetUser {
			_ = tg.AnswerCallbackQuery(ctx, cq.ID, "这是其他新成员的验证题哦", true)
			return
		}
		key := fmt.Sprintf("%d:%d", chatID, targetUser)
		m.mu.Lock()
		pv, ok := m.verifies[key]
		if !ok {
			m.mu.Unlock()
			_ = tg.AnswerCallbackQuery(ctx, cq.ID, "验证已过期或已完成", false)
			return
		}
		if choice == pv.answer {
			delete(m.verifies, key)
			pv.cancel()
			m.mu.Unlock()

			_ = tg.RestrictChatMember(ctx, chatID, targetUser, fullPermissions(), 0)
			m.removeMuted(chatIDStr, targetUser)
			_ = tg.AnswerCallbackQuery(ctx, cq.ID, "验证通过，已解除禁言！", false)
			if cq.Message.MessageID > 0 {
				_ = tg.DeleteMessage(ctx, chatID, cq.Message.MessageID)
			}
			m.store.IncAdminAction()
			rule, ok := m.cfgMgr.ResolveTelegramChat(chatIDStr)
			if ok && rule.WelcomeEnabled {
				m.sendWelcome(ctx, tg, chatID, cq.Message.Chat.Title, displayName(cq.From.FirstName, cq.From.Username, cq.From.ID), rule)
			}
			return
		}

		// 答错：累计次数；未超限立即刷新新题目，超限则判定失败
		pv.attempts++
		if pv.attempts >= pv.maxAttempts {
			delete(m.verifies, key)
			pv.cancel()
			m.mu.Unlock()

			_ = tg.AnswerCallbackQuery(ctx, cq.ID, fmt.Sprintf("连续答错 %d 次，验证失败", pv.maxAttempts), true)
			m.handleVerifyFailure(ctx, tg, pv, fmt.Sprintf("答错达到上限（%d 次）", pv.maxAttempts))
			return
		}

		remain := pv.maxAttempts - pv.attempts
		a, b, newAns, newOpts := generateMathChallenge(pv.answer)
		pv.answer = newAns
		name := pv.name
		timeoutSec := pv.timeoutSec
		maxAttempts := pv.maxAttempts
		msgID := pv.msgID
		m.mu.Unlock()

		prompt, markup := buildVerifyPromptAndMarkup(name, targetUser, timeoutSec, a, b, remain, maxAttempts, newOpts)
		if msgID > 0 {
			_, _ = tg.EditMessageText(ctx, chatID, msgID, prompt, map[string]any{
				"parse_mode":   "HTML",
				"reply_markup": markup,
			})
		}
		_ = tg.AnswerCallbackQuery(ctx, cq.ID, fmt.Sprintf("答案不对，题目已刷新（还剩 %d 次机会）", remain), true)
		return
	}

	// 2. 群内禁言消息的「管理员一键解禁」按钮
	if strings.HasPrefix(cq.Data, "unmute:") {
		if !m.isAdmin(ctx, c, ev.Account.ID, chatID, cq.From.ID) {
			_ = tg.AnswerCallbackQuery(ctx, cq.ID, "仅群管理员可解除禁言", true)
			return
		}
		targetUser, _ := strconv.ParseInt(strings.TrimPrefix(cq.Data, "unmute:"), 10, 64)
		if targetUser <= 0 {
			return
		}
		if err := tg.RestrictChatMember(ctx, chatID, targetUser, fullPermissions(), 0); err != nil {
			_ = tg.AnswerCallbackQuery(ctx, cq.ID, "解禁失败，请检查机器人管理员权限", true)
			return
		}
		m.clearVerifyAndMuted(ctx, tg, chatID, targetUser)
		adminName := displayName(cq.From.FirstName, cq.From.Username, cq.From.ID)
		m.store.IncAdminAction()
		m.store.AddLog(store.LogEntry{
			Category: "tg_admin", Platform: "telegram", AccountID: ev.Account.ID, ChatID: chatIDStr, ChatTitle: cq.Message.Chat.Title,
			Summary: fmt.Sprintf("管理员 %s 通过群内按钮解除用户 %d 禁言", adminName, targetUser),
		})
		_ = tg.AnswerCallbackQuery(ctx, cq.ID, "已解除该成员禁言", false)
		if cq.Message.MessageID > 0 {
			_, _ = tg.EditMessageText(ctx, chatID, cq.Message.MessageID, fmt.Sprintf("🔊 管理员 <b>%s</b> 已解除用户 <code>%d</code> 的禁言。", escapeHTML(adminName), targetUser), map[string]any{"parse_mode": "HTML"})
		}
		return
	}

	// 3. 管理员内联控制面板开关
	if strings.HasPrefix(cq.Data, "panel:") {
		if !m.isAdmin(ctx, c, ev.Account.ID, chatID, cq.From.ID) {
			_ = tg.AnswerCallbackQuery(ctx, cq.ID, "仅群管理员可操作", true)
			return
		}
		action := strings.TrimPrefix(cq.Data, "panel:")
		if action == "close" {
			_ = tg.AnswerCallbackQuery(ctx, cq.ID, "面板已关闭", false)
			_ = tg.DeleteMessage(ctx, chatID, cq.Message.MessageID)
			return
		}
		toast, updated := m.togglePanelRule(chatIDStr, cq.Message.Chat.Title, action)
		if !updated {
			_ = tg.AnswerCallbackQuery(ctx, cq.ID, "未知操作", false)
			return
		}
		_ = tg.AnswerCallbackQuery(ctx, cq.ID, toast, false)
		m.sendAdminPanel(ctx, tg, chatID, cq.Message.Chat.Title, cq.Message.MessageID)
	}
}

func resolveTelegramChatInConfig(cfg *config.Config, chatID string) config.TelegramChatRule {
	for i := range cfg.TelegramChats {
		if cfg.TelegramChats[i].ChatID == chatID {
			return cfg.TelegramChats[i]
		}
	}
	for i := range cfg.TelegramChats {
		if cfg.TelegramChats[i].ChatID == "*" {
			return cfg.TelegramChats[i]
		}
	}
	return config.TelegramChatRule{
		Enabled:           true,
		VerifyTimeoutSec:  120,
		VerifyMaxAttempts: defaultMaxAttempts,
		DeleteBannedMsg:   true,
		WarnLimit:         3,
		WarnAction:        "mute",
		WarnMuteMinutes:   60,
	}
}

func (m *Module) togglePanelRule(chatIDStr, chatTitle, action string) (string, bool) {
	var toast string
	updated := false
	statusText := func(v bool) string {
		if v {
			return "已开启"
		}
		return "已关闭"
	}
	_ = m.cfgMgr.Mutate(func(cfg *config.Config) {
		idx := -1
		for i := range cfg.TelegramChats {
			if cfg.TelegramChats[i].ChatID == chatIDStr {
				idx = i
				break
			}
		}
		if idx == -1 {
			base := resolveTelegramChatInConfig(cfg, chatIDStr)
			base.ChatID = chatIDStr
			base.Title = chatTitle
			cfg.TelegramChats = append(cfg.TelegramChats, base)
			idx = len(cfg.TelegramChats) - 1
		}
		r := &cfg.TelegramChats[idx]
		if strings.TrimSpace(chatTitle) != "" {
			r.Title = strings.TrimSpace(chatTitle)
		}
		switch action {
		case "toggle_verify":
			r.VerifyEnabled = !r.VerifyEnabled
			toast = "进群验证：" + statusText(r.VerifyEnabled)
			updated = true
		case "toggle_welcome":
			r.WelcomeEnabled = !r.WelcomeEnabled
			toast = "入群欢迎：" + statusText(r.WelcomeEnabled)
			updated = true
		case "toggle_links":
			r.BlockLinks = !r.BlockLinks
			toast = "拦截链接：" + statusText(r.BlockLinks)
			updated = true
		case "toggle_approve":
			r.AutoApproveJoin = !r.AutoApproveJoin
			toast = "自动审群：" + statusText(r.AutoApproveJoin)
			updated = true
		case "toggle_blacklist":
			r.BlacklistEnabled = !r.BlacklistEnabled
			toast = "黑名单系统：" + statusText(r.BlacklistEnabled)
			updated = true
		}
	})
	return toast, updated
}

func unmuteInlineMarkup(userID int64) map[string]any {
	return map[string]any{
		"inline_keyboard": [][]map[string]any{
			{
				{"text": "🔊 管理员一键解禁", "callback_data": fmt.Sprintf("unmute:%d", userID)},
			},
		},
	}
}

func (m *Module) botPermSummary(chatIDStr string) string {
	m.mu.Lock()
	cs, ok := m.chats[chatIDStr]
	m.mu.Unlock()
	if !ok || cs.BotStatus == "" {
		return ""
	}
	if cs.BotStatus != "administrator" && cs.BotStatus != "creator" {
		return "\n⚠️ 机器人当前非群管理员，请先设为管理员以启用群管功能"
	}
	var missing []string
	if !cs.CanRestrict {
		missing = append(missing, "限制/封禁成员")
	}
	if !cs.CanDelete {
		missing = append(missing, "删除消息")
	}
	if !cs.CanInvite {
		missing = append(missing, "邀请/审群")
	}
	if len(missing) > 0 {
		return "\n⚠️ 缺少权限：" + strings.Join(missing, "、")
	}
	return "\n🛡️ 机器人权限：正常（限制 / 删消息 / 审群）"
}

func (m *Module) sendAdminPanel(ctx context.Context, tg lwplugin.Telegram, chatID int64, chatTitle string, editMsgID int64) {
	chatIDStr := strconv.FormatInt(chatID, 10)
	rule, _ := m.cfgMgr.ResolveTelegramChat(chatIDStr)
	onOff := func(v bool) string {
		if v {
			return "✅ 已开启"
		}
		return "⚪ 已关闭"
	}
	permLine := m.botPermSummary(chatIDStr)
	text := fmt.Sprintf(
		"⚙️ <b>Bot Hub 群管面板</b>\n群聊：%s%s\n\n点击下方按钮可即时切换本群策略（与 Web 面板实时同步）：",
		escapeHTML(firstNonEmpty(chatTitle, chatIDStr)),
		escapeHTML(permLine),
	)
	markup := map[string]any{
		"inline_keyboard": [][]map[string]any{
			{
				{"text": "进群验证: " + onOff(rule.VerifyEnabled), "callback_data": "panel:toggle_verify"},
				{"text": "入群欢迎: " + onOff(rule.WelcomeEnabled), "callback_data": "panel:toggle_welcome"},
			},
			{
				{"text": "拦截链接: " + onOff(rule.BlockLinks), "callback_data": "panel:toggle_links"},
				{"text": "自动审群: " + onOff(rule.AutoApproveJoin), "callback_data": "panel:toggle_approve"},
			},
			{
				{"text": "黑名单系统: " + onOff(rule.BlacklistEnabled), "callback_data": "panel:toggle_blacklist"},
				{"text": "✖ 关闭面板", "callback_data": "panel:close"},
			},
		},
	}
	extra := map[string]any{"parse_mode": "HTML", "reply_markup": markup}
	if editMsgID > 0 {
		_, _ = tg.EditMessageText(ctx, chatID, editMsgID, text, extra)
	} else {
		_, _ = tg.SendMessage(ctx, chatID, text, extra)
	}
}

func (m *Module) sendWelcome(ctx context.Context, tg lwplugin.Telegram, chatID int64, chatTitle, name string, rule config.TelegramChatRule) {
	text := rule.WelcomeTemplate
	if strings.TrimSpace(text) == "" {
		text = "欢迎 {name} 加入 {group}！"
	}
	text = strings.ReplaceAll(text, "{name}", name)
	text = strings.ReplaceAll(text, "{group}", chatTitle)
	m.sendAutoDeleteMessage(ctx, tg, chatID, text, rule.WelcomeDeleteSec)
}

func (m *Module) sendGoodbye(ctx context.Context, tg lwplugin.Telegram, chatID int64, chatTitle, name string, rule config.TelegramChatRule) {
	text := rule.GoodbyeTemplate
	if strings.TrimSpace(text) == "" {
		text = "{name} 离开了群聊。"
	}
	text = strings.ReplaceAll(text, "{name}", name)
	text = strings.ReplaceAll(text, "{group}", chatTitle)
	if strings.TrimSpace(text) == "" {
		return
	}
	m.sendAutoDeleteMessage(ctx, tg, chatID, text, rule.GoodbyeDeleteSec)
}

func (m *Module) sendAutoDeleteMessage(ctx context.Context, tg lwplugin.Telegram, chatID int64, text string, deleteSec int) {
	rawResp, err := tg.SendMessage(ctx, chatID, text, nil)
	if err != nil || deleteSec <= 0 {
		return
	}
	var sent struct {
		MessageID int64 `json:"message_id"`
	}
	if json.Unmarshal(rawResp, &sent) == nil && sent.MessageID > 0 {
		go func(mid int64, delay int) {
			time.Sleep(time.Duration(delay) * time.Second)
			dCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_ = tg.DeleteMessage(dCtx, chatID, mid)
		}(sent.MessageID, deleteSec)
	}
}

func (m *Module) recordMuted(accountID, chatIDStr, chatTitle string, userID int64, name, reason string, until int64) {
	key := fmt.Sprintf("%s:%d", chatIDStr, userID)
	m.mu.Lock()
	m.muted[key] = MutedMember{
		AccountID: accountID,
		ChatID:    chatIDStr,
		ChatTitle: chatTitle,
		UserID:    userID,
		Name:      name,
		Reason:    reason,
		MutedAt:   time.Now().Unix(),
		Until:     until,
	}
	m.saveCacheLocked()
	m.mu.Unlock()
}

func (m *Module) removeMuted(chatIDStr string, userID int64) {
	key := fmt.Sprintf("%s:%d", chatIDStr, userID)
	m.mu.Lock()
	if _, ok := m.muted[key]; ok {
		delete(m.muted, key)
		m.saveCacheLocked()
	}
	m.mu.Unlock()
}

func (m *Module) clearVerifyAndMuted(ctx context.Context, tg lwplugin.Telegram, chatID, userID int64) {
	key := fmt.Sprintf("%d:%d", chatID, userID)
	var verifyMsgID int64
	m.mu.Lock()
	if pv, ok := m.verifies[key]; ok {
		pv.cancel()
		verifyMsgID = pv.msgID
		delete(m.verifies, key)
	}
	if _, ok := m.muted[key]; ok {
		delete(m.muted, key)
		m.saveCacheLocked()
	}
	m.mu.Unlock()
	if verifyMsgID > 0 {
		_ = tg.DeleteMessage(ctx, chatID, verifyMsgID)
	}
}

func (m *Module) addWarn(accountID, chatIDStr, chatTitle string, userID int64, name, reason string) int {
	key := fmt.Sprintf("%s:%d", chatIDStr, userID)
	m.mu.Lock()
	rec := m.warnings[key]
	rec.AccountID = accountID
	rec.ChatID = chatIDStr
	rec.ChatTitle = chatTitle
	rec.UserID = userID
	if name != "" {
		rec.Name = name
	}
	rec.Count++
	rec.LastReason = reason
	rec.UpdatedAt = time.Now().Unix()
	m.warnings[key] = rec
	m.saveCacheLocked()
	count := rec.Count
	m.mu.Unlock()
	return count
}

func (m *Module) resetWarn(chatIDStr string, userID int64) {
	key := fmt.Sprintf("%s:%d", chatIDStr, userID)
	m.mu.Lock()
	if _, ok := m.warnings[key]; ok {
		delete(m.warnings, key)
		m.saveCacheLocked()
	}
	m.mu.Unlock()
}

func (m *Module) addBlacklist(accountID, chatIDStr, chatTitle string, userID int64, name, reason string) {
	m.bl.Add(blacklist.Entry{
		Platform:        "telegram",
		UserID:          strconv.FormatInt(userID, 10),
		UserName:        firstNonEmpty(name, strconv.FormatInt(userID, 10)),
		Reason:          reason,
		SourceAccountID: accountID,
		SourceChatID:    chatIDStr,
		SourceChatTitle: chatTitle,
	})
}

func (m *Module) isBlacklisted(userID int64) (blacklist.Entry, bool) {
	return m.bl.Contains("telegram", strconv.FormatInt(userID, 10))
}

func (m *Module) markSkipBlacklist(chatIDStr string, userID int64, ttl time.Duration) {
	key := fmt.Sprintf("%s:%d", chatIDStr, userID)
	m.mu.Lock()
	m.skipBlacklistUntil[key] = time.Now().Add(ttl).Unix()
	m.mu.Unlock()
}

func (m *Module) shouldSkipBlacklist(chatIDStr string, userID int64) bool {
	key := fmt.Sprintf("%s:%d", chatIDStr, userID)
	now := time.Now().Unix()
	m.mu.Lock()
	defer m.mu.Unlock()
	exp, ok := m.skipBlacklistUntil[key]
	if !ok {
		return false
	}
	if exp < now {
		delete(m.skipBlacklistUntil, key)
		return false
	}
	delete(m.skipBlacklistUntil, key)
	return true
}

func (m *Module) applyWarning(
	ctx context.Context,
	tg lwplugin.Telegram,
	chatID int64,
	chatTitle string,
	userID int64,
	name string,
	reason string,
	rule config.TelegramChatRule,
) {
	limit := rule.WarnLimit
	if limit <= 0 {
		limit = 3
	}
	chatIDStr := strconv.FormatInt(chatID, 10)
	count := m.addWarn(tg.AccountID, chatIDStr, chatTitle, userID, name, reason)
	m.store.IncAdminAction()
	m.store.AddLog(store.LogEntry{
		Category: "tg_admin", Platform: "telegram", AccountID: tg.AccountID,
		ChatID: chatIDStr, ChatTitle: chatTitle,
		Summary: fmt.Sprintf("警告 %s (%d/%d): %s", name, count, limit, reason),
	})

	// 警告达上限后不自动清零，除非管理员在面板手动清零；
	// 黑名单仅在触发“移出群聊 (ban)”时自动拉黑，单纯禁言 (mute) 不拉黑。
	if count >= limit {
		if rule.WarnAction == "ban" {
			blNote := ""
			if rule.BlacklistEnabled {
				m.addBlacklist(tg.AccountID, chatIDStr, chatTitle, userID, name, fmt.Sprintf("警告达上限被移出(%d/%d): %s", count, limit, reason))
				blNote = "并已加入黑名单"
			}
			_ = tg.BanChatMember(ctx, chatID, userID, 0, false)
			_, _ = tg.SendMessage(ctx, chatID, fmt.Sprintf("🚫 <b>%s</b> 警告已达上限（%d/%d），已移出群聊%s。", escapeHTML(name), count, limit, blNote), map[string]any{"parse_mode": "HTML"})
		} else {
			mins := rule.WarnMuteMinutes
			if mins <= 0 {
				mins = 60
			}
			until := time.Now().Add(time.Duration(mins) * time.Minute).Unix()
			_ = tg.RestrictChatMember(ctx, chatID, userID, mutedPermissions(), until)
			m.recordMuted(tg.AccountID, chatIDStr, chatTitle, userID, name, fmt.Sprintf("警告达上限（%s）", reason), until)
			_, _ = tg.SendMessage(ctx, chatID, fmt.Sprintf("🔇 <b>%s</b> 警告已达上限（%d/%d），已禁言 %d 分钟。", escapeHTML(name), count, limit, mins), map[string]any{
				"parse_mode":   "HTML",
				"reply_markup": unmuteInlineMarkup(userID),
			})
		}
		return
	}
	_, _ = tg.SendMessage(ctx, chatID, fmt.Sprintf("⚠️ <b>%s</b> 收到警告（%d/%d）：%s", escapeHTML(name), count, limit, escapeHTML(reason)), map[string]any{"parse_mode": "HTML"})
}

func (m *Module) isAdmin(ctx context.Context, c *lwplugin.Client, accountID string, chatID, userID int64) bool {
	key := fmt.Sprintf("%s:%d", accountID, chatID)
	m.mu.Lock()
	if entry, ok := m.adminCache[key]; ok && time.Now().Before(entry.expires) {
		is := entry.admins[userID]
		m.mu.Unlock()
		return is
	}
	m.mu.Unlock()

	raw, err := c.Telegram(accountID).GetChatAdministrators(ctx, chatID)
	if err != nil {
		return false
	}
	var members []struct {
		User struct {
			ID int64 `json:"id"`
		} `json:"user"`
	}
	if err := json.Unmarshal(raw, &members); err != nil {
		return false
	}
	set := make(map[int64]bool, len(members))
	for _, mem := range members {
		set[mem.User.ID] = true
	}
	m.mu.Lock()
	m.adminCache[key] = adminCacheEntry{admins: set, expires: time.Now().Add(3 * time.Minute)}
	m.mu.Unlock()
	return set[userID]
}

func mutedPermissions() map[string]any {
	return map[string]any{
		"can_send_messages":         false,
		"can_send_audios":           false,
		"can_send_documents":        false,
		"can_send_photos":           false,
		"can_send_videos":           false,
		"can_send_video_notes":      false,
		"can_send_voice_notes":      false,
		"can_send_polls":            false,
		"can_send_other_messages":   false,
		"can_add_web_page_previews": false,
	}
}

func fullPermissions() map[string]any {
	return map[string]any{
		"can_send_messages":         true,
		"can_send_audios":           true,
		"can_send_documents":        true,
		"can_send_photos":           true,
		"can_send_videos":           true,
		"can_send_video_notes":      true,
		"can_send_voice_notes":      true,
		"can_send_polls":            true,
		"can_send_other_messages":   true,
		"can_add_web_page_previews": true,
	}
}

func displayName(first, username string, id int64) string {
	if strings.TrimSpace(first) != "" {
		return strings.TrimSpace(first)
	}
	if strings.TrimSpace(username) != "" {
		return "@" + strings.TrimSpace(username)
	}
	return strconv.FormatInt(id, 10)
}

func escapeHTML(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	return r.Replace(s)
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}
