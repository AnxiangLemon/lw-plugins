package blacklist

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Entry 表示一条跨群、跨账号通用的平台黑名单记录。
// 同一平台下按 (platform, user_id) 全局生效：一处拉黑，处处受阻。
type Entry struct {
	Platform        string   `json:"platform"`                    // 平台标识，如 "wechat" / "telegram" / "qq"
	UserID          string   `json:"user_id"`                     // 平台内用户唯一标识（wxid / TG 数字 user_id / QQ 号等）
	UserName        string   `json:"user_name"`                   // 展示名称
	Reason          string   `json:"reason"`                      // 拉黑原因
	SourceAccountID string   `json:"source_account_id,omitempty"` // 触发拉黑的账号 ID（仅溯源）
	SourceChatID    string   `json:"source_chat_id,omitempty"`    // 触发拉黑的来源群 ID（仅溯源）
	SourceChatTitle string   `json:"source_chat_title,omitempty"` // 触发拉黑的来源群名称（仅溯源）
	BannedChats     []string `json:"banned_chats,omitempty"`      // 曾执行过平台级封禁的群 ID 列表（移出黑名单时用于逐一解封）
	AddedAt         int64    `json:"added_at"`                    // 加入黑名单的时间戳（秒）
}

type diskData struct {
	Entries []Entry `json:"entries"`
}

// Store 线程安全的全平台公共黑名单存储，持久化到 dataDir/blacklist.json。
type Store struct {
	path string

	mu      sync.RWMutex
	entries map[string]Entry // key: normKey(platform, userID)
}

func normKey(platform, userID string) string {
	return strings.ToLower(strings.TrimSpace(platform)) + ":" + strings.TrimSpace(userID)
}

// New 初始化公共黑名单存储并从磁盘加载已有记录。
func New(dataDir string) *Store {
	_ = os.MkdirAll(dataDir, 0o755)
	s := &Store{
		path:    filepath.Join(dataDir, "blacklist.json"),
		entries: make(map[string]Entry),
	}
	s.load()
	return s
}

func (s *Store) load() {
	raw, err := os.ReadFile(s.path)
	if err != nil {
		return
	}
	var d diskData
	if json.Unmarshal(raw, &d) != nil {
		return
	}
	for _, e := range d.Entries {
		p := strings.ToLower(strings.TrimSpace(e.Platform))
		u := strings.TrimSpace(e.UserID)
		if p == "" || u == "" {
			continue
		}
		e.Platform = p
		e.UserID = u
		s.entries[normKey(p, u)] = e
	}
}

func (s *Store) saveLocked() {
	list := make([]Entry, 0, len(s.entries))
	for _, e := range s.entries {
		list = append(list, e)
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].AddedAt != list[j].AddedAt {
			return list[i].AddedAt > list[j].AddedAt
		}
		return normKey(list[i].Platform, list[i].UserID) < normKey(list[j].Platform, list[j].UserID)
	})
	raw, err := json.MarshalIndent(diskData{Entries: list}, "", "  ")
	if err != nil {
		return
	}
	tmp := s.path + ".tmp"
	if os.WriteFile(tmp, raw, 0o644) == nil {
		_ = os.Rename(tmp, s.path)
	}
}

func mergeChats(existing []string, add ...string) []string {
	seen := make(map[string]bool, len(existing)+len(add))
	out := make([]string, 0, len(existing)+len(add))
	for _, c := range existing {
		c = strings.TrimSpace(c)
		if c != "" && !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	for _, c := range add {
		c = strings.TrimSpace(c)
		if c != "" && !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	return out
}

// Add 将成员加入平台全局黑名单；若已存在则合并来源/已封禁群列表并返回 created=false。
func (s *Store) Add(e Entry) (Entry, bool) {
	e.Platform = strings.ToLower(strings.TrimSpace(e.Platform))
	e.UserID = strings.TrimSpace(e.UserID)
	if e.Platform == "" || e.UserID == "" {
		return Entry{}, false
	}
	if strings.TrimSpace(e.UserName) == "" {
		e.UserName = e.UserID
	}
	if e.AddedAt <= 0 {
		e.AddedAt = time.Now().Unix()
	}
	key := normKey(e.Platform, e.UserID)

	s.mu.Lock()
	defer s.mu.Unlock()
	if prev, exists := s.entries[key]; exists {
		if prev.UserName == "" || prev.UserName == prev.UserID {
			if e.UserName != "" {
				prev.UserName = e.UserName
			}
		}
		if prev.Reason == "" && e.Reason != "" {
			prev.Reason = e.Reason
		}
		if prev.SourceAccountID == "" {
			prev.SourceAccountID = e.SourceAccountID
		}
		if prev.SourceChatID == "" {
			prev.SourceChatID = e.SourceChatID
			prev.SourceChatTitle = e.SourceChatTitle
		}
		prev.BannedChats = mergeChats(prev.BannedChats, e.BannedChats...)
		if e.SourceChatID != "" {
			prev.BannedChats = mergeChats(prev.BannedChats, e.SourceChatID)
		}
		s.entries[key] = prev
		s.saveLocked()
		return prev, false
	}
	if e.SourceChatID != "" {
		e.BannedChats = mergeChats(e.BannedChats, e.SourceChatID)
	}
	s.entries[key] = e
	s.saveLocked()
	return e, true
}

// RecordBannedChat 记录该黑名单成员在某个群执行过平台级封禁，便于移除黑名单时统一解封。
func (s *Store) RecordBannedChat(platform, userID, chatID string) {
	chatID = strings.TrimSpace(chatID)
	if chatID == "" {
		return
	}
	key := normKey(platform, userID)
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[key]
	if !ok {
		return
	}
	next := mergeChats(e.BannedChats, chatID)
	if len(next) != len(e.BannedChats) {
		e.BannedChats = next
		s.entries[key] = e
		s.saveLocked()
	}
}

// Remove 从黑名单移除指定平台成员，并返回被移除的原始记录。
func (s *Store) Remove(platform, userID string) (Entry, bool) {
	key := normKey(platform, userID)
	s.mu.Lock()
	defer s.mu.Unlock()
	prev, ok := s.entries[key]
	if !ok {
		return Entry{}, false
	}
	delete(s.entries, key)
	s.saveLocked()
	return prev, true
}

// Contains 检查该用户在指定平台是否处于黑名单（全群通用、全账号通用）。
func (s *Store) Contains(platform, userID string) (Entry, bool) {
	key := normKey(platform, userID)
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.entries[key]
	return e, ok
}

// List 返回黑名单列表（按加入时间倒序），platform 为空或 "all" 时返回全平台记录。
func (s *Store) List(platform string) []Entry {
	pf := strings.ToLower(strings.TrimSpace(platform))
	s.mu.RLock()
	out := make([]Entry, 0, len(s.entries))
	for _, e := range s.entries {
		if pf != "" && pf != "all" && e.Platform != pf {
			continue
		}
		out = append(out, e)
	}
	s.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool {
		if out[i].AddedAt != out[j].AddedAt {
			return out[i].AddedAt > out[j].AddedAt
		}
		return normKey(out[i].Platform, out[i].UserID) < normKey(out[j].Platform, out[j].UserID)
	})
	return out
}

// Count 返回当前黑名单总人数。
func (s *Store) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.entries)
}
