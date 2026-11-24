package store

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

const maxMemoryLogs = 400

// LogEntry 统一审计与告警记录（监控转发、冷却抑制、TG 群管操作）。
type LogEntry struct {
	ID         string `json:"id"`
	Time       int64  `json:"time"`
	Category   string `json:"category"` // "monitor" | "cooldown" | "tg_admin" | "system"
	Level      string `json:"level"`    // "info" | "warn" | "error"
	Platform   string `json:"platform"`
	AccountID  string `json:"account_id"`
	ChatID     string `json:"chat_id"`
	ChatTitle  string `json:"chat_title,omitempty"`
	RuleName   string `json:"rule_name,omitempty"`
	Summary    string `json:"summary"`
	Detail     string `json:"detail,omitempty"`
}

// AccountInfo 宿主账号快照（展示平台真实昵称与头像，不使用宿主备注）。
type AccountInfo struct {
	Platform string `json:"platform"`
	ID       string `json:"id"`
	Nickname string `json:"nickname,omitempty"`
	Username string `json:"username,omitempty"`
	Avatar   string `json:"avatar,omitempty"`
	Online   bool   `json:"online"`
}

type cachedProfile struct {
	Nickname string `json:"nickname,omitempty"`
	Username string `json:"username,omitempty"`
	Avatar   string `json:"avatar,omitempty"`
}

// KnownChat 运行过程中观察到的群聊（方便在 Web 面板一键填入群 ID）。
type KnownChat struct {
	Platform  string `json:"platform"`
	AccountID string `json:"account_id"`
	ChatID    string `json:"chat_id"`
	Title     string `json:"title"`
	LastSeen  int64  `json:"last_seen"`
}

// Stats 仪表盘核心统计。
type Stats struct {
	Connected       bool  `json:"connected"`
	HostVersion     string `json:"host_version"`
	UptimeSec       int64 `json:"uptime_sec"`
	MessagesSeen    int64 `json:"messages_seen"`
	AlertsSent      int64 `json:"alerts_sent"`
	AlertsSuppressed int64 `json:"alerts_suppressed"`
	AdminActions    int64 `json:"admin_actions"`
}

// Store 维护运行态指标、日志与已知群/账号缓存。
type Store struct {
	mu          sync.RWMutex
	logPath     string
	profilePath string
	consoleLog  bool
	startedAt   time.Time
	connected   bool
	hostVersion string
	stats       Stats
	logs        []LogEntry
	accounts    map[string]AccountInfo   // key: platform:id
	profiles    map[string]cachedProfile // key: platform:id（持久化真实昵称与头像，离线仍可展示）
	chats       map[string]KnownChat     // key: platform:chat_id
}

func New(dataDir string) *Store {
	_ = os.MkdirAll(dataDir, 0o755)
	s := &Store{
		logPath:     filepath.Join(dataDir, "events.jsonl"),
		profilePath: filepath.Join(dataDir, "profiles.json"),
		startedAt:   time.Now(),
		logs:        make([]LogEntry, 0, 128),
		accounts:    make(map[string]AccountInfo),
		profiles:    make(map[string]cachedProfile),
		chats:       make(map[string]KnownChat),
	}
	s.loadRecentLogs()
	s.loadProfiles()
	return s
}

func (s *Store) loadProfiles() {
	raw, err := os.ReadFile(s.profilePath)
	if err != nil {
		return
	}
	var m map[string]cachedProfile
	if json.Unmarshal(raw, &m) == nil && m != nil {
		s.profiles = m
	}
}

func (s *Store) saveProfilesLocked() {
	raw, err := json.MarshalIndent(s.profiles, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(s.profilePath, raw, 0o600)
}

func (s *Store) loadRecentLogs() {
	f, err := os.Open(s.logPath)
	if err != nil {
		return
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	var all []LogEntry
	for scanner.Scan() {
		var entry LogEntry
		if json.Unmarshal(scanner.Bytes(), &entry) == nil {
			all = append(all, entry)
		}
	}
	if len(all) > maxMemoryLogs {
		all = all[len(all)-maxMemoryLogs:]
	}
	s.logs = all
}

func (s *Store) SetConnection(connected bool, hostVersion string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.connected = connected
	if hostVersion != "" {
		s.hostVersion = hostVersion
	}
}

func (s *Store) IncMessages() {
	s.mu.Lock()
	s.stats.MessagesSeen++
	s.mu.Unlock()
}

func (s *Store) IncAlertSent() {
	s.mu.Lock()
	s.stats.AlertsSent++
	s.mu.Unlock()
}

func (s *Store) IncAlertSuppressed() {
	s.mu.Lock()
	s.stats.AlertsSuppressed++
	s.mu.Unlock()
}

func (s *Store) IncAdminAction() {
	s.mu.Lock()
	s.stats.AdminActions++
	s.mu.Unlock()
}

func (s *Store) Stats() Stats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st := s.stats
	st.Connected = s.connected
	st.HostVersion = s.hostVersion
	st.UptimeSec = int64(time.Since(s.startedAt).Seconds())
	return st
}

func (s *Store) SetAccounts(list []AccountInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := make(map[string]AccountInfo, len(list))
	for _, a := range list {
		key := a.Platform + ":" + a.ID
		if prev, ok := s.accounts[key]; ok {
			if a.Nickname == "" {
				a.Nickname = prev.Nickname
			}
			if a.Username == "" {
				a.Username = prev.Username
			}
			if a.Avatar == "" {
				a.Avatar = prev.Avatar
			}
		}
		if prof, ok := s.profiles[key]; ok {
			if a.Nickname == "" {
				a.Nickname = prof.Nickname
			}
			if a.Username == "" {
				a.Username = prof.Username
			}
			if a.Avatar == "" {
				a.Avatar = prof.Avatar
			}
		}
		next[key] = a
	}
	s.accounts = next
}

func (s *Store) UpsertAccount(a AccountInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := a.Platform + ":" + a.ID
	if prev, ok := s.accounts[key]; ok {
		if a.Nickname == "" {
			a.Nickname = prev.Nickname
		}
		if a.Username == "" {
			a.Username = prev.Username
		}
		if a.Avatar == "" {
			a.Avatar = prev.Avatar
		}
	}
	if prof, ok := s.profiles[key]; ok {
		if a.Nickname == "" {
			a.Nickname = prof.Nickname
		}
		if a.Username == "" {
			a.Username = prof.Username
		}
		if a.Avatar == "" {
			a.Avatar = prof.Avatar
		}
	}
	s.accounts[key] = a
}

// UpdateAccountProfile 更新并持久化账号的真实平台昵称与头像。
func (s *Store) UpdateAccountProfile(platform, id, nickname, username, avatar string) {
	if platform == "" || id == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := platform + ":" + id
	prof := s.profiles[key]
	changed := false
	if nickname != "" && nickname != prof.Nickname {
		prof.Nickname = nickname
		changed = true
	}
	if username != "" && username != prof.Username {
		prof.Username = username
		changed = true
	}
	if avatar != "" && avatar != prof.Avatar {
		prof.Avatar = avatar
		changed = true
	}
	if changed {
		s.profiles[key] = prof
		s.saveProfilesLocked()
	}
	if acc, ok := s.accounts[key]; ok {
		if prof.Nickname != "" {
			acc.Nickname = prof.Nickname
		}
		if prof.Username != "" {
			acc.Username = prof.Username
		}
		if prof.Avatar != "" {
			acc.Avatar = prof.Avatar
		}
		s.accounts[key] = acc
	}
}

func (s *Store) Accounts() []AccountInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]AccountInfo, 0, len(s.accounts))
	for _, a := range s.accounts {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Online != out[j].Online {
			return out[i].Online
		}
		if out[i].Platform != out[j].Platform {
			return out[i].Platform < out[j].Platform
		}
		if out[i].Nickname != out[j].Nickname {
			return out[i].Nickname < out[j].Nickname
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func (s *Store) TouchChat(platform, accountID, chatID, title string) {
	if chatID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := platform + ":" + chatID
	prev := s.chats[key]
	if title == "" {
		title = prev.Title
	}
	s.chats[key] = KnownChat{
		Platform:  platform,
		AccountID: accountID,
		ChatID:    chatID,
		Title:     title,
		LastSeen:  time.Now().Unix(),
	}
}

func (s *Store) Chats() []KnownChat {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]KnownChat, 0, len(s.chats))
	for _, c := range s.chats {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Platform != out[j].Platform {
			return out[i].Platform < out[j].Platform
		}
		if out[i].LastSeen != out[j].LastSeen {
			return out[i].LastSeen > out[j].LastSeen
		}
		return out[i].ChatID < out[j].ChatID
	})
	return out
}

// EnableConsoleLog 控制 AddLog 时是否同步向终端打印格式化日志。
func (s *Store) EnableConsoleLog(on bool) {
	s.mu.Lock()
	s.consoleLog = on
	s.mu.Unlock()
}

func (s *Store) AddLog(entry LogEntry) {
	if entry.Time == 0 {
		entry.Time = time.Now().Unix()
	}
	if entry.ID == "" {
		entry.ID = time.Now().Format("20060102150405.000000000")
	}
	if entry.Level == "" {
		entry.Level = "info"
	}

	s.mu.Lock()
	s.logs = append(s.logs, entry)
	if len(s.logs) > maxMemoryLogs {
		s.logs = s.logs[len(s.logs)-maxMemoryLogs:]
	}
	printConsole := s.consoleLog
	s.mu.Unlock()

	if printConsole {
		scope := ""
		if entry.RuleName != "" && entry.ChatTitle != "" {
			scope = entry.RuleName + " · " + entry.ChatTitle
		} else if entry.RuleName != "" {
			scope = entry.RuleName
		} else if entry.ChatTitle != "" {
			scope = entry.ChatTitle
		} else if entry.ChatID != "" {
			scope = entry.ChatID
		}
		PrintConsoleLine(entry.Level, entry.Category, scope, entry.Summary, entry.Detail)
	}

	if f, err := os.OpenFile(s.logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600); err == nil {
		if raw, err := json.Marshal(entry); err == nil {
			_, _ = f.Write(append(raw, '\n'))
		}
		_ = f.Close()
	}
}

// Logs 按倒序（最新在前）返回日志，可按 category 过滤。
func (s *Store) Logs(category string, limit int) []LogEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if limit <= 0 || limit > maxMemoryLogs {
		limit = 100
	}
	out := make([]LogEntry, 0, limit)
	for i := len(s.logs) - 1; i >= 0 && len(out) < limit; i-- {
		e := s.logs[i]
		if category != "" && category != "all" && e.Category != category {
			continue
		}
		out = append(out, e)
	}
	return out
}

// ClearLogs 清空内存与磁盘持久化日志；当 category 为空或 "all" 时清空全部，否则仅清除指定分类。
func (s *Store) ClearLogs(category string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if category == "" || category == "all" {
		s.logs = s.logs[:0]
		return os.WriteFile(s.logPath, nil, 0o600)
	}

	kept := make([]LogEntry, 0, len(s.logs))
	for _, e := range s.logs {
		if e.Category != category {
			kept = append(kept, e)
		}
	}
	s.logs = kept

	var buf []byte
	for _, e := range kept {
		if raw, err := json.Marshal(e); err == nil {
			buf = append(buf, raw...)
			buf = append(buf, '\n')
		}
	}
	return os.WriteFile(s.logPath, buf, 0o600)
}
