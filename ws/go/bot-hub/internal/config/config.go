package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

// Config 是 bot-hub 的完整配置（持久化为 dataDir/config.json）。
type Config struct {
	MentionGroups []MentionGroup     `json:"mention_groups"`
	MonitorRules  []MonitorRule      `json:"monitor_rules"`
	TelegramChats []TelegramChatRule `json:"telegram_chats"`
	WechatRooms   []WechatRoomRule   `json:"wechat_rooms"`
}

// WechatRoomRule 单个微信群的入群/退群监控与提示策略。
type WechatRoomRule struct {
	AccountID       string `json:"account_id"`        // 所属微信账号 wxid
	RoomID          string `json:"room_id"`           // 群 ID（xxx@chatroom）
	Title           string `json:"title"`             // 群名称备注
	Enabled         bool   `json:"enabled"`           // 是否开启入/退群监控
	WelcomeEnabled  bool   `json:"welcome_enabled"`   // 新人入群群内提示
	WelcomeTemplate string `json:"welcome_template"`  // 支持 {name} {wxid} {inviter} {group} {count}
	GoodbyeEnabled   bool   `json:"goodbye_enabled"`    // 成员离群群内提示
	GoodbyeTemplate  string `json:"goodbye_template"`   // 支持 {name} {wxid} {group} {count}
	PollIntervalSec  int    `json:"poll_interval_sec"`  // 定时兜底核对间隔（秒，0 表示仅靠 hint 触发，最低 60）
	BlacklistEnabled bool   `json:"blacklist_enabled"`  // 是否开启全平台黑名单拦截（命中黑名单入群时管理员静默踢出，仅记日志）
	BlacklistOnLeave bool   `json:"blacklist_on_leave"` // 可选：成员离群（退群/被移出）时自动加入全局黑名单
}

// MentionGroup 人员分组（例如“运维值班组”、“客服组”），纯粹用于复用一组被 @ 的成员名单。
type MentionGroup struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Members     []string `json:"members"` // 支持 TG @username / user_id、微信 wxid / 昵称
}

// NotifyTarget 消息转发目标（支持微信、Telegram，同平台或跨平台均可）。
type NotifyTarget struct {
	Type      string `json:"type"`       // "telegram" | "wechat"
	AccountID string `json:"account_id"` // TG bot_id 或 微信 wxid
	To        string `json:"to"`         // TG chat_id 或 微信 wxid/群ID
}

// MonitorRule 消息监控与防刷屏转发规则（支持微信↔微信、TG↔TG 及微信↔TG）。
type MonitorRule struct {
	ID               string         `json:"id"`
	Name             string         `json:"name"`
	Enabled          bool           `json:"enabled"`
	Platforms        []string       `json:"platforms"`          // 空表示全部平台；可填 ["telegram", "wechat"]
	AccountIDs       []string       `json:"account_ids"`        // 空表示全部账号
	ChatIDs          []string       `json:"chat_ids"`           // 空表示全部群聊；填具体群 ID 仅监控指定群
	IgnoreSenders    []string       `json:"ignore_senders"`          // 忽略的发送者（username / user_id / wxid）
	IgnoreCommands   bool           `json:"ignore_commands"`         // 忽略以 / 开头的机器人命令消息（如 /panel、/warn@Bot）
	IgnoreBots       bool           `json:"ignore_bots"`             // 忽略其他机器人账号发出的消息
	ExcludeKeywords  []string       `json:"exclude_keywords"`        // 排除过滤词：消息包含任意词即不转发（支持 ^前缀 表示开头匹配，不区分大小写）
	MatchAllMessages bool           `json:"match_all_messages"`      // true=转发来源群全部消息（受消息类型过滤）；false=按 @/分组/关键词 触发
	MessageTypes     []string       `json:"message_types"`      // 允许转发的消息类型：text/image/video/voice/file/link/other，空表示全部
	WatchMentions    []string       `json:"watch_mentions"`     // 直接监控被 @ 的人（@username / user_id / wxid）
	WatchGroupIDs    []string       `json:"watch_group_ids"`    // 监控的 MentionGroup ID 列表
	WatchAllMention  bool           `json:"watch_all"`          // 是否监控 @all / @所有人
	Keywords         []string       `json:"keywords"`           // 可选：同时监控关键词（不区分大小写）
	CooldownSeconds  int            `json:"cooldown_seconds"`        // 冷却秒数（如 300=5分钟, 600=10分钟；0 表示不冷却）
	CooldownScope    string         `json:"cooldown_scope"`          // "target"（按群+目标，默认）| "chat"（按群）| "rule"（按规则全局）
	ForwardMedia     *bool          `json:"forward_media,omitempty"` // 是否尝试转发原图/原视频/原文件/语音（默认 true）
	MaxMediaMB       int            `json:"max_media_mb"`            // 跨平台媒体下载转发大小上限（MB，默认 10；微信同平台 XML 直转不受此限）
	RawForward       bool           `json:"raw_forward"`             // true=纯原样转发（仅发原文/原媒体，不套文本模板）；false=按模板发送通知
	Template         string         `json:"template"`                // 自定义消息模板（空则用默认模板）
	Targets          []NotifyTarget `json:"targets"`                 // 转发目标列表
}

// ShouldForwardMedia 返回该规则是否开启原媒体转发（旧配置未设置时默认开启）。
func (r MonitorRule) ShouldForwardMedia() bool {
	if r.ForwardMedia == nil {
		return true
	}
	return *r.ForwardMedia
}

// EffectiveMaxMediaMB 返回跨平台媒体转发的有效大小上限（MB），默认 10 MB，最大 32 MB。
func (r MonitorRule) EffectiveMaxMediaMB() int {
	if r.MaxMediaMB <= 0 {
		return 10
	}
	if r.MaxMediaMB > 32 {
		return 32
	}
	return r.MaxMediaMB
}

// TelegramChatRule 单个 Telegram 群（或全局默认 "*"）的群管策略。
type TelegramChatRule struct {
	ChatID             string   `json:"chat_id"` // 具体 chat_id（如 "-1001234567890"），或 "*" 表示默认模板
	Title              string   `json:"title"`
	Enabled            bool     `json:"enabled"`
	VerifyEnabled      bool     `json:"verify_enabled"`       // 进群按钮验证（先禁言，点击按钮通过后解禁）
	VerifyTimeoutSec   int      `json:"verify_timeout_sec"`   // 验证超时秒数（默认 120）
	VerifyMaxAttempts  int      `json:"verify_max_attempts"`  // 最大答错次数（默认 3，答错自动换题）
	VerifyKickOnFail   bool     `json:"verify_kick_on_fail"`  // 超时或答错达上限是否踢出
	WelcomeEnabled     bool     `json:"welcome_enabled"`      // 入群欢迎
	WelcomeTemplate    string   `json:"welcome_template"`     // 欢迎文案（支持 {name} {group}）
	WelcomeDeleteSec   int      `json:"welcome_delete_sec"`   // 欢迎消息自动撤回秒数（0 不撤回）
	GoodbyeEnabled     bool     `json:"goodbye_enabled"`      // 退群提示（发在群内）
	GoodbyeTemplate    string   `json:"goodbye_template"`     // 退群文案（支持 {name} {group}）
	GoodbyeDeleteSec   int      `json:"goodbye_delete_sec"`   // 退群提示自动撤回秒数（0 不撤回）
	AutoApproveJoin    bool     `json:"auto_approve_join"`    // 自动通过 chat_join_request
	BlockLinks         bool     `json:"block_links"`          // 拦截外部链接（非管理员）
	BannedKeywords     []string `json:"banned_keywords"`      // 违禁词列表
	DeleteBannedMsg    bool     `json:"delete_banned_msg"`    // 命中违禁词/链接是否自动删消息
	WarnLimit          int      `json:"warn_limit"`           // 警告上限（默认 3）
	WarnAction         string   `json:"warn_action"`          // "mute" | "ban"
	WarnMuteMinutes    int      `json:"warn_mute_minutes"`    // 警告满后禁言分钟数（默认 60）
	BlacklistEnabled   bool     `json:"blacklist_enabled"`    // 是否开启黑名单系统（被移出群聊后禁止再加群，申请拒绝/入群即踢，需手动移除）
	BlacklistOnLeave   bool     `json:"blacklist_on_leave"`   // 可选：成员主动退出群聊时是否也自动加入黑名单
}

// DefaultConfig 返回开箱可用的默认配置。
func DefaultConfig() Config {
	return Config{
		MentionGroups: []MentionGroup{
			{
				ID:          "ops",
				Name:        "运维值班组",
				Description: "示例分组：在转发规则中引用后，群里 @ 组内任意成员即触发转发",
				Members:     []string{},
			},
		},
		MonitorRules:  []MonitorRule{},
		WechatRooms:   []WechatRoomRule{},
		TelegramChats: []TelegramChatRule{
			{
				ChatID:           "*",
				Title:            "全局默认策略",
				Enabled:          true,
				VerifyEnabled:     false,
				VerifyTimeoutSec:  120,
				VerifyMaxAttempts: 3,
				VerifyKickOnFail:  false,
				WelcomeEnabled:    true,
				WelcomeTemplate:   "欢迎 {name} 加入 {group}！请遵守群规，文明交流。",
				WelcomeDeleteSec:  60,
				GoodbyeEnabled:    false,
				GoodbyeTemplate:   "{name} 离开了群聊。",
				GoodbyeDeleteSec:  60,
				BlockLinks:       false,
				BannedKeywords:   []string{},
				DeleteBannedMsg:  true,
				WarnLimit:        3,
				WarnAction:       "mute",
				WarnMuteMinutes:  60,
			},
		},
	}
}

// Manager 负责线程安全的配置读写与原子落盘。
type Manager struct {
	mu   sync.RWMutex
	path string
	cfg  Config
}

// Load 加载或初始化配置。
func Load(dataDir string) (*Manager, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(dataDir, "config.json")
	m := &Manager{
		path: path,
		cfg:  DefaultConfig(),
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if saveErr := m.saveLocked(); saveErr != nil {
				return nil, saveErr
			}
			return m, nil
		}
		return nil, err
	}
	var loaded Config
	if err := json.Unmarshal(data, &loaded); err != nil {
		return nil, err
	}
	if loaded.MentionGroups == nil {
		loaded.MentionGroups = []MentionGroup{}
	}
	if loaded.MonitorRules == nil {
		loaded.MonitorRules = []MonitorRule{}
	}
	if loaded.TelegramChats == nil {
		loaded.TelegramChats = []TelegramChatRule{}
	}
	if loaded.WechatRooms == nil {
		loaded.WechatRooms = []WechatRoomRule{}
	}
	m.cfg = loaded
	return m, nil
}

// Get 返回配置的深拷贝快照。
func (m *Manager) Get() Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	raw, _ := json.Marshal(m.cfg)
	var copyCfg Config
	_ = json.Unmarshal(raw, &copyCfg)
	return copyCfg
}

// Update 替换整个配置并原子落盘。
func (m *Manager) Update(next Config) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if next.MentionGroups == nil {
		next.MentionGroups = []MentionGroup{}
	}
	if next.MonitorRules == nil {
		next.MonitorRules = []MonitorRule{}
	}
	if next.TelegramChats == nil {
		next.TelegramChats = []TelegramChatRule{}
	}
	if next.WechatRooms == nil {
		next.WechatRooms = []WechatRoomRule{}
	}
	m.cfg = next
	return m.saveLocked()
}

// Mutate 在锁内修改配置并落盘。
func (m *Manager) Mutate(fn func(*Config)) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	fn(&m.cfg)
	return m.saveLocked()
}

// ResolveTelegramChat 查找指定群的策略，未单独配置时回退到 "*" 默认策略。
func (m *Manager) ResolveTelegramChat(chatID string) (TelegramChatRule, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var fallback *TelegramChatRule
	for i := range m.cfg.TelegramChats {
		r := &m.cfg.TelegramChats[i]
		if r.ChatID == chatID {
			return *r, true
		}
		if r.ChatID == "*" {
			fallback = r
		}
	}
	if fallback != nil {
		return *fallback, true
	}
	return TelegramChatRule{}, false
}

// FindWechatRoom 查找指定微信账号与群 ID 的监控规则。
func (m *Manager) FindWechatRoom(accountID, roomID string) (WechatRoomRule, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for i := range m.cfg.WechatRooms {
		r := &m.cfg.WechatRooms[i]
		if r.AccountID == accountID && r.RoomID == roomID {
			return *r, true
		}
	}
	return WechatRoomRule{}, false
}

func (m *Manager) saveLocked() error {
	raw, err := json.MarshalIndent(m.cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := m.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, m.path)
}
