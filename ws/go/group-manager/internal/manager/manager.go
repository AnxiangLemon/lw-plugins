package manager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	lwplugin "github.com/AnxiangLemon/lw-plugins/ws/go"
)

type Host interface {
	lwplugin.Caller
	Connected() bool
}
type groupState struct {
	Pending, Running, Gap, Urgent bool
	Due, LastStart, LastSuccess   time.Time
	Source, Error                 string
	Failures, Count               int
	Version                       uint32
}
type GroupStatus struct {
	Group
	Pending     bool      `json:"pending"`
	Running     bool      `json:"running"`
	LastChecked time.Time `json:"last_checked"`
	Error       string    `json:"error"`
	MemberCount int       `json:"member_count"`
	Version     uint32    `json:"version"`
}

// Manager 的锁只保护配置和调度状态，网络调用放在锁外。
// 同群仅一个在途查询，全局最多两个；查询中新增提示会留下 Pending 再核对。
type Manager struct {
	mu               sync.Mutex
	config           Config
	configPath       string
	generation       uint64
	states           map[string]*groupState
	weakAt           map[string]time.Time
	weakNext         map[string]int
	store            *Store
	host             Host
	logger           *slog.Logger
	wg               sync.WaitGroup
	active           int
	connectionError  string
	directories      map[string]*DirectoryStatus
	discoveryPending map[string]bool
	autoDiscover     bool
}

func New(config Config, path string, store *Store, host Host, logger *slog.Logger) (*Manager, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if logger == nil {
		logger = slog.Default()
	}
	m := &Manager{config: config, configPath: path, store: store, host: host, logger: logger, states: map[string]*groupState{}, weakAt: map[string]time.Time{}, weakNext: map[string]int{}}
	m.directories = map[string]*DirectoryStatus{}
	m.discoveryPending = map[string]bool{}
	for _, g := range config.Groups {
		snapshot, err := store.Members(g.AccountID, g.RoomID)
		if err != nil {
			return nil, err
		}
		m.states[g.key()] = &groupState{Count: len(snapshot.Members), Version: snapshot.Version}
	}
	return m, nil
}
func (m *Manager) Config() Config {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := m.config
	c.Groups = append([]Group{}, c.Groups...)
	return c
}
func (m *Manager) SetConfig(config Config) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := SaveConfig(m.configPath, config); err != nil {
		return err
	}
	m.generation++
	m.config = config
	for _, g := range config.Groups {
		if m.states[g.key()] == nil {
			m.states[g.key()] = &groupState{}
		}
		if g.Enabled {
			m.queue(g, "settings_changed", true, true)
		}
	}
	return nil
}
func (m *Manager) group(key string) (Group, bool) {
	for _, g := range m.config.Groups {
		if g.key() == key {
			return g, true
		}
	}
	return Group{}, false
}
func (m *Manager) queue(g Group, source string, gap, urgent bool) {
	if !g.Enabled {
		return
	}
	s := m.states[g.key()]
	if s == nil {
		s = &groupState{}
		m.states[g.key()] = s
	}
	if !s.Pending {
		s.Due = time.Now().Add(time.Duration(m.config.DebounceMS) * time.Millisecond)
	}
	s.Pending = true
	s.Gap = s.Gap || gap
	s.Urgent = s.Urgent || urgent
	s.Source = source
	if urgent {
		s.Due = time.Now()
		s.Failures = 0
	}
}

// Connected 在每次 WS 重连完成后调用。服务器不保证刷新提示断线重放，
// 因此对关注群做一次基线核对，记录离线净变化并抑制过时的群消息。
func (m *Manager) Connected() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.connectionError = ""
	m.autoDiscover = true
	for _, g := range m.config.Groups {
		m.queue(g, "reconnect", true, true)
	}
}

func (m *Manager) Disconnected(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err != nil {
		m.connectionError = err.Error()
	}
}
func (m *Manager) ConnectionError() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.connectionError
}
func (m *Manager) Handle(_ context.Context, event lwplugin.Event) error {
	if event.Account.Platform != "wechat" {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	switch event.Name {
	case lwplugin.GroupRefreshHint:
		var h lwplugin.GroupHint
		if err := json.Unmarshal(event.Data, &h); err != nil {
			return err
		}
		for _, g := range m.config.Groups {
			if g.AccountID == event.Account.ID && g.RoomID == h.RoomID {
				m.queue(g, h.Reason, false, false)
			}
		}
	case lwplugin.AccountOnline:
		if m.directories[event.Account.ID] == nil {
			m.directories[event.Account.ID] = &DirectoryStatus{AccountID: event.Account.ID}
		}
		m.discoveryPending[event.Account.ID] = true
		for _, g := range m.config.Groups {
			if g.AccountID == event.Account.ID {
				m.queue(g, "account_online", true, true)
			}
		}
	case lwplugin.SyncHint:
		var h lwplugin.SyncMetadata
		if err := json.Unmarshal(event.Data, &h); err != nil {
			return err
		}
		if h.Source != "server_push" || h.Selector == nil || (*h.Selector&4) == 0 || m.config.WeakRecheckSeconds == 0 {
			return nil
		}
		if time.Since(m.weakAt[event.Account.ID]) < time.Duration(m.config.WeakRecheckSeconds)*time.Second {
			return nil
		}
		// selector 没有群 ID。每轮最多核对五个关注群，轮转且限频，不能伪造退群。
		var groups []Group
		for _, g := range m.config.Groups {
			if g.Enabled && g.AccountID == event.Account.ID {
				groups = append(groups, g)
			}
		}
		if len(groups) == 0 {
			return nil
		}
		sort.Slice(groups, func(i, j int) bool { return groups[i].RoomID < groups[j].RoomID })
		n := len(groups)
		if n > 5 {
			n = 5
		}
		start := m.weakNext[event.Account.ID] % len(groups)
		for i := 0; i < n; i++ {
			m.queue(groups[(start+i)%len(groups)], "weak_sync_hint", false, false)
		}
		m.weakNext[event.Account.ID] = (start + n) % len(groups)
		m.weakAt[event.Account.ID] = time.Now()
	}
	return nil
}
func (m *Manager) Recheck(account, room string, reset bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	g, ok := m.group((Group{AccountID: account, RoomID: room}).key())
	if !ok || !g.Enabled {
		return errors.New("该群未启用监控")
	}
	if m.states[g.key()].Running && reset {
		return errors.New("成员同步正在执行，请完成后重新开始记录")
	}
	if reset {
		if err := m.store.Reset(account, room); err != nil {
			return err
		}
		m.generation++
	}
	m.queue(g, "manual_check", reset, true)
	return nil
}
func (m *Manager) Status() []GroupStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []GroupStatus{}
	for _, g := range m.config.Groups {
		s := m.states[g.key()]
		if s == nil {
			s = &groupState{}
		}
		out = append(out, GroupStatus{Group: g, Pending: s.Pending, Running: s.Running, LastChecked: s.LastSuccess, Error: s.Error, MemberCount: s.Count, Version: s.Version})
	}
	return out
}

// Run 只调度已关注的群。普通事件只排队，不在 WS 回调中等待业务 RPC。
func (m *Manager) Run(ctx context.Context) {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	notifyCtx, cancel := context.WithCancel(ctx)
	defer func() { cancel(); m.wg.Wait() }()
	m.wg.Add(1)
	go func() { defer m.wg.Done(); m.notifications(notifyCtx) }()
	m.wg.Add(1)
	go func() { defer m.wg.Done(); m.operations(notifyCtx) }()
	var lastPoll, lastCleanup time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			m.mu.Lock()
			auto := m.autoDiscover && m.host.Connected()
			if auto {
				m.autoDiscover = false
			}
			if m.host.Connected() {
				// 整个账号只允许一个群目录任务，避免按钮连点启动重复分页。
				for account := range m.discoveryPending {
					s := m.directories[account]
					if s.Running {
						continue
					}
					s.Running = true
					delete(m.discoveryPending, account)
					m.wg.Add(1)
					go func(a string) { defer m.wg.Done(); m.discoverRooms(ctx, a) }(account)
					break
				}
			}
			if m.config.PollSeconds > 0 && now.Sub(lastPoll) >= time.Duration(m.config.PollSeconds)*time.Second {
				for _, g := range m.config.Groups {
					m.queue(g, "periodic_check", false, false)
				}
				lastPoll = now
			}
			if m.host.Connected() {
				for _, g := range m.config.Groups {
					s := m.states[g.key()]
					if s == nil || !g.Enabled || !s.Pending || s.Running || m.active >= 2 || now.Before(s.Due) {
						continue
					}
					if !s.Urgent && now.Sub(s.LastStart) < time.Duration(m.config.CooldownSeconds)*time.Second {
						continue
					}
					s.Pending = false
					s.Running = true
					s.LastStart = now
					s.Urgent = false
					m.active++
					source, gap, generation := s.Source, s.Gap, m.generation
					m.wg.Add(1)
					go func(g Group) { defer m.wg.Done(); m.check(ctx, g, source, gap, generation) }(g)
				}
			}
			days := m.config.RetentionDays
			m.mu.Unlock()
			if auto {
				m.wg.Add(1)
				go func() { defer m.wg.Done(); m.discoverAccounts(ctx) }()
			}
			if now.Sub(lastCleanup) >= time.Hour {
				if err := m.store.Cleanup(days); err != nil {
					m.logger.Error("清理历史日志失败", "error", err)
				}
				lastCleanup = now
			}
		}
	}
}
func (m *Manager) check(parent context.Context, g Group, source string, gap bool, generation uint64) {
	ctx, cancel := context.WithTimeout(parent, 45*time.Second)
	defer cancel()
	wx := lwplugin.NewWechat(m.host, g.AccountID)
	result, err := wx.Call(ctx, "group.get_chat_room_member_detail", map[string]string{"chatRoomId": g.RoomID}, lwplugin.Options{})
	if err == nil {
		result, err = wx.Await(ctx, result)
	}
	if err == nil && !result.Succeeded() {
		err = statusError(result)
	}
	var snapshot Snapshot
	if err == nil {
		snapshot, err = ParseSnapshot(result.Data, g.AccountID, g.RoomID)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.states[g.key()]
	s.Running = false
	m.active--
	current, ok := m.group(g.key())
	if !ok || !current.Enabled || generation != m.generation {
		if ok && current.Enabled {
			m.queue(current, "settings_changed", true, true)
		}
		return
	}
	var events []Audit
	if err == nil {
		events, err = m.store.Apply(current, snapshot, source, gap)
	}
	if err != nil {
		if parent.Err() != nil {
			return
		}
		s.Error = err.Error()
		s.Failures++
		m.logger.Warn("成员核对失败，保留原基线", "account_id", g.AccountID, "room_id", g.RoomID, "error", err)
		// 仅对只读查询做三次有限恢复；失败后等待新提示或手动核对。
		if s.Failures < 3 {
			s.Pending = true
			s.Due = time.Now().Add(time.Duration(s.Failures) * 30 * time.Second)
		}
		return
	}
	s.Error = ""
	s.Failures = 0
	if !s.Pending {
		s.Gap = false
	}
	s.LastSuccess = time.Now()
	s.Count = len(snapshot.Members)
	s.Version = snapshot.Version
	for _, event := range events {
		kind := map[string]string{"baseline": "建立成员基线", "joined": "成员新增", "left": "成员离开（名单对比）"}[event.Kind]
		m.logger.Info(kind, "account_id", g.AccountID, "room_id", g.RoomID, "member_id", event.MemberID, "name", event.Name, "before", event.Before, "after", event.After, "source", "member_snapshot_diff", "trigger", event.Trigger, "reason", event.Reason, "offline_gap", event.Gap, "audit_id", event.ID)
	}
}

func (m *Manager) notifications(ctx context.Context) {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if !m.host.Connected() {
				continue
			}
			events, err := m.store.Pending()
			if err != nil {
				m.logger.Error("读取待发送提示失败", "error", err)
				continue
			}
			for _, e := range events {
				if ctx.Err() != nil {
					return
				}
				m.notify(ctx, e)
			}
		}
	}
}
func (m *Manager) notify(parent context.Context, e Audit) {
	// 发送前重新读取开关，保存日志后关闭通知仍可阻止尚未开始的消息。
	// Claim 之后视为已开始，后续只追踪原调用，不用新幂等键重试。
	m.mu.Lock()
	g, ok := m.group((Group{AccountID: e.AccountID, RoomID: e.RoomID}).key())
	enabled := ok && g.Enabled && ((e.Kind == "joined" && g.NotifyJoin) || (e.Kind == "left" && g.NotifyLeave))
	m.mu.Unlock()
	if !enabled {
		if err := m.store.Notification(e.ID, "disabled", "", ""); err != nil {
			m.logger.Error("更新提示状态失败", "error", err)
		}
		return
	}
	claimed, err := m.store.Claim(e.ID)
	if err != nil || !claimed {
		if err != nil {
			m.logger.Error("领取提示任务失败", "error", err)
		}
		return
	}
	template := g.JoinTemplate
	if e.Kind == "left" {
		template = g.LeaveTemplate
	}
	if template == "" {
		if e.Kind == "joined" {
			template = "欢迎 {name} 加入本群。"
		} else {
			template = "成员 {name} 已不在当前群名单中。"
		}
	}
	groupName := g.Label
	if groupName == "" {
		groupName = g.RoomID
	}
	text := strings.NewReplacer("{name}", e.Name, "{wxid}", e.MemberID, "{group}", groupName, "{count}", fmt.Sprint(e.After)).Replace(template)
	ctx, cancel := context.WithTimeout(parent, 60*time.Second)
	defer cancel()
	wx := lwplugin.NewWechat(m.host, e.AccountID)
	result, err := wx.SendText(ctx, e.RoomID, text, lwplugin.Options{Key: e.NotifyKey})
	if err == nil {
		result, err = wx.Await(ctx, result)
	}
	status, detail := "unknown", ""
	if err != nil {
		detail = err.Error()
	} else {
		switch result.Status {
		case "succeeded", "failed", "rejected", "cancelled":
			status = result.Status
		default:
			detail = "结果未确定；请查询原回执"
		}
		if result.Error != nil {
			detail = result.Error.Message
		}
	}
	if err = m.store.Notification(e.ID, status, result.CallID, detail); err != nil {
		m.logger.Error("提示状态保存失败，不自动重发", "audit_id", e.ID, "error", err)
	}
}

// Receipt 只核对原幂等键，不重新发送消息；用于断线或崩溃后的 unknown 状态。
func (m *Manager) Receipt(ctx context.Context, id int64) (Audit, error) {
	e, err := m.store.Audit(id)
	if err != nil {
		return e, err
	}
	if e.NotifyStatus != "unknown" && e.NotifyStatus != "queued" && e.NotifyStatus != "running" {
		return e, nil
	}
	var result *lwplugin.Result
	err = lwplugin.NewWechat(m.host, e.AccountID).Query(ctx, "find", map[string]any{"idempotency_key": e.NotifyKey}, &result)
	if err != nil {
		return e, err
	}
	if result == nil {
		return e, errors.New("宿主未找到原调用；不自动重发")
	}
	detail := ""
	if result.Error != nil {
		detail = result.Error.Message
	}
	if err = m.store.Notification(id, result.Status, result.CallID, detail); err != nil {
		return e, err
	}
	return m.store.Audit(id)
}
