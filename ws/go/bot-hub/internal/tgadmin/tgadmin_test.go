package tgadmin

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"
	"time"

	lwplugin "github.com/AnxiangLemon/lw-plugins/ws/go"
	"github.com/AnxiangLemon/lw-plugins/ws/go/bot-hub/internal/blacklist"
	"github.com/AnxiangLemon/lw-plugins/ws/go/bot-hub/internal/config"
	"github.com/AnxiangLemon/lw-plugins/ws/go/bot-hub/internal/store"
)

func TestGenerateMathChallengeUniqueAndRefresh(t *testing.T) {
	prev := 0
	for i := 0; i < 30; i++ {
		a, b, ans, opts := generateMathChallenge(prev)
		if a+b != ans {
			t.Fatalf("a+b=%d != ans=%d", a+b, ans)
		}
		if len(opts) != 4 {
			t.Fatalf("expected 4 options, got %d", len(opts))
		}
		seen := map[int]bool{}
		hasAns := false
		for _, o := range opts {
			if o <= 0 {
				t.Fatalf("option must be positive, got %d", o)
			}
			if seen[o] {
				t.Fatalf("duplicate option %d in %v", o, opts)
			}
			seen[o] = true
			if o == ans {
				hasAns = true
			}
		}
		if !hasAns {
			t.Fatalf("answer %d not in options %v", ans, opts)
		}
		prev = ans
	}
}

func TestHasExternalLink(t *testing.T) {
	cases := []struct {
		name     string
		text     string
		entities []tgEntity
		want     bool
	}{
		{"plain text", "大家好今天天气不错", nil, false},
		{"https url", "点我看 https://example.com", nil, true},
		{"t.me invite", "加群 t.me/abc123", nil, true},
		{"www link", "访问 www.test.com 了解详情", nil, true},
		{"hidden text_link", "点我领取福利", []tgEntity{{Type: "text_link"}}, true},
		{"url entity", "example.org", []tgEntity{{Type: "url"}}, true},
		{"mention only", "你好 @alice", []tgEntity{{Type: "mention"}}, false},
	}
	for _, tc := range cases {
		if got := hasExternalLink(tc.text, tc.entities); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestMyChatMemberAndPersistence(t *testing.T) {
	dir := t.TempDir()
	cfgMgr, err := config.Load(dir)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	st := store.New(dir)
	mod := New(dir, cfgMgr, st, blacklist.New(dir))

	evData, _ := json.Marshal(map[string]any{
		"my_chat_member": map[string]any{
			"chat": map[string]any{
				"id":    int64(-1001234567890),
				"title": "机器人测试群",
				"type":  "supergroup",
			},
			"new_chat_member": map[string]any{
				"status":               "administrator",
				"can_restrict_members": true,
				"can_delete_messages":  true,
				"can_invite_users":     false,
			},
		},
	})
	mod.handleMyChatMember(lwplugin.Event{
		Name:    lwplugin.TelegramMyChatMember,
		Account: lwplugin.Account{Platform: "telegram", ID: "7426534028"},
		Data:    evData,
	})
	mod.recordMuted("7426534028", "-1001234567890", "机器人测试群", 998877, "张三", "进群验证失败（保持禁言）", 0)
	mod.addWarn("7426534028", "-1001234567890", "机器人测试群", 665544, "李四", "禁止发送外部链接")

	// 重启恢复
	st2 := store.New(dir)
	mod2 := New(dir, cfgMgr, st2, blacklist.New(dir))
	snap := mod2.StateSnapshot()

	if len(snap.Chats) != 1 || snap.Chats[0].ChatID != "-1001234567890" {
		t.Fatalf("expected restored chat, got %+v", snap.Chats)
	}
	if snap.Chats[0].BotStatus != "administrator" || !snap.Chats[0].CanRestrict || !snap.Chats[0].CanDelete || snap.Chats[0].CanInvite {
		t.Fatalf("unexpected bot perms: %+v", snap.Chats[0])
	}
	if len(snap.Muted) != 1 || snap.Muted[0].UserID != 998877 {
		t.Fatalf("expected restored muted member, got %+v", snap.Muted)
	}
	if len(snap.Warnings) != 1 || snap.Warnings[0].UserID != 665544 || snap.Warnings[0].Count != 1 {
		t.Fatalf("expected restored warning, got %+v", snap.Warnings)
	}

	// 验证已过期禁言自动清理
	mod2.recordMuted("7426534028", "-1001234567890", "机器人测试群", 111222, "过期用户", "临时禁言", time.Now().Add(-10*time.Second).Unix())
	snap2 := mod2.StateSnapshot()
	if len(snap2.Muted) != 1 {
		t.Fatalf("expected expired mute to be pruned, got %+v", snap2.Muted)
	}
}

type noopCaller struct{}

func (noopCaller) Request(_ context.Context, _ string, _ any, _ any) error { return nil }

func TestWarningNoAutoResetAndBlacklist(t *testing.T) {
	dir := t.TempDir()
	cfgMgr, err := config.Load(dir)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	st := store.New(dir)
	mod := New(dir, cfgMgr, st, blacklist.New(dir))

	tg := lwplugin.NewTelegram(noopCaller{}, "bot_1")
	rule := config.TelegramChatRule{
		ChatID:           "-100999",
		Enabled:          true,
		WarnLimit:        3,
		WarnAction:       "mute",
		WarnMuteMinutes:  60,
		BlacklistEnabled: true,
	}

	ctx := context.Background()
	for i := 1; i <= 3; i++ {
		mod.applyWarning(ctx, tg, -100999, "测试群", 555666, "违规用户", "命中违禁词", rule)
	}

	// 达到 3 次上限且处罚为禁言（mute）时：警告不得自动清零，且不应自动拉黑
	snap := mod.StateSnapshot()
	if len(snap.Warnings) != 1 || snap.Warnings[0].Count != 3 {
		t.Fatalf("warning count must stay 3 after limit reached, got %+v", snap.Warnings)
	}
	if _, blocked := mod.isBlacklisted(555666); blocked {
		t.Fatalf("user must NOT be blacklisted on mute-only warning punishment")
	}

	// 若警告达上限处罚方式为踢出群聊（ban），则自动加入全局黑名单
	banRule := rule
	banRule.WarnAction = "ban"
	for i := 1; i <= 3; i++ {
		mod.applyWarning(ctx, tg, -100999, "测试群", 777888, "被踢用户", "命中违禁词", banRule)
	}
	if _, blocked := mod.isBlacklisted(777888); !blocked {
		t.Fatalf("user must be blacklisted when kicked on warn limit with BlacklistEnabled=true")
	}

	// 重启恢复后，警告与全局黑名单均保留
	bl2 := blacklist.New(dir)
	mod2 := New(dir, cfgMgr, store.New(dir), bl2)
	snap2 := mod2.StateSnapshot()
	if len(snap2.Warnings) != 2 {
		t.Fatalf("warnings must persist after restart, got %+v", snap2.Warnings)
	}
	blList := bl2.List("telegram")
	if len(blList) != 1 || blList[0].UserID != "777888" {
		t.Fatalf("blacklist must persist after restart, got %+v", blList)
	}

	// 手动移出黑名单 & 手动清零警告
	bl2.Remove("telegram", "777888")
	if _, blocked := mod2.isBlacklisted(777888); blocked {
		t.Fatal("user should be removed from blacklist")
	}
	mod2.resetWarn("-100999", 555666)
	mod2.resetWarn("-100999", 777888)
	if len(mod2.StateSnapshot().Warnings) != 0 {
		t.Fatal("warnings should be empty only after manual resetWarn")
	}
}

func TestBlacklistOnKickAndOptionalLeave(t *testing.T) {
	dir := t.TempDir()
	cfgMgr, err := config.Load(dir)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	cfg := cfgMgr.Get()
	cfg.TelegramChats = []config.TelegramChatRule{
		{
			ChatID:           "-100777",
			Enabled:          true,
			BlacklistEnabled: true,
			BlacklistOnLeave: false,
		},
	}
	if err := cfgMgr.Update(cfg); err != nil {
		t.Fatalf("cfgMgr.Update: %v", err)
	}
	bl := blacklist.New(dir)
	mod := New(dir, cfgMgr, store.New(dir), bl)
	ctx := context.Background()

	makeMemberEvent := func(userID int64, name, oldSt, newSt string) lwplugin.Event {
		raw := []byte(`{"chat_member":{"chat":{"id":-100777,"title":"黑名单测试群"},"old_chat_member":{"status":"` + oldSt + `"},"new_chat_member":{"status":"` + newSt + `","user":{"id":` + strconv.FormatInt(userID, 10) + `,"is_bot":false,"first_name":"` + name + `"}}}}`)
		return lwplugin.Event{
			Account: lwplugin.Account{ID: "bot_1", Platform: "telegram"},
			Data:    raw,
		}
	}

	// 1) 被移出群聊（kicked）自动加入全局黑名单
	mod.handleChatMember(ctx, nil, makeMemberEvent(10001, "被踢甲", "member", "kicked"))
	if _, blocked := mod.isBlacklisted(10001); !blocked {
		t.Fatal("kicked member should be automatically blacklisted")
	}

	// 2) 未开启 BlacklistOnLeave 时，主动退群（left）不拉黑
	mod.handleChatMember(ctx, nil, makeMemberEvent(10002, "退群乙", "member", "left"))
	if _, blocked := mod.isBlacklisted(10002); blocked {
		t.Fatal("left member should NOT be blacklisted when BlacklistOnLeave=false")
	}

	// 3) 开启 BlacklistOnLeave 后，主动退群（left）自动加入全局黑名单
	cfg.TelegramChats[0].BlacklistOnLeave = true
	if err := cfgMgr.Update(cfg); err != nil {
		t.Fatalf("cfgMgr.Update: %v", err)
	}
	mod.handleChatMember(ctx, nil, makeMemberEvent(10003, "退群丙", "member", "left"))
	if _, blocked := mod.isBlacklisted(10003); !blocked {
		t.Fatal("left member should be automatically blacklisted when BlacklistOnLeave=true")
	}
}

func TestTogglePanelRuleNoDeadlock(t *testing.T) {
	dir := t.TempDir()
	cfgMgr, err := config.Load(dir)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	mod := New(dir, cfgMgr, store.New(dir), blacklist.New(dir))

	// 默认仅存在 "*" 规则，首次对具体群切换策略不得发生 RWMutex 重入死锁
	done := make(chan struct{})
	var toast string
	var ok bool
	go func() {
		toast, ok = mod.togglePanelRule("-100888777", "机器人测试群", "toggle_verify")
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("togglePanelRule deadlocked on first toggle for chat without explicit rule")
	}
	if !ok || toast != "进群验证：已开启" {
		t.Fatalf("unexpected first toggle result: ok=%v toast=%q", ok, toast)
	}
	rule, found := cfgMgr.ResolveTelegramChat("-100888777")
	if !found || !rule.VerifyEnabled || rule.ChatID != "-100888777" {
		t.Fatalf("expected derived chat rule with VerifyEnabled=true, got found=%v rule=%+v", found, rule)
	}

	toast2, ok2 := mod.togglePanelRule("-100888777", "机器人测试群", "toggle_verify")
	if !ok2 || toast2 != "进群验证：已关闭" {
		t.Fatalf("unexpected second toggle result: ok=%v toast=%q", ok2, toast2)
	}
	rule2, _ := cfgMgr.ResolveTelegramChat("-100888777")
	if rule2.VerifyEnabled {
		t.Fatal("expected VerifyEnabled=false after second toggle")
	}
}

func TestResolveReplyTargetAndErrorFormat(t *testing.T) {
	dir := t.TempDir()
	cfgMgr, err := config.Load(dir)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	mod := New(dir, cfgMgr, store.New(dir), blacklist.New(dir))

	// 即使内存缓存为空，也能直接从透传的 reply_to_user_id 识别目标
	info, ok := mod.resolveReplyTarget(-100123, 999, msgSenderInfo{UserID: 778899, Name: "测试成员"})
	if !ok || info.UserID != 778899 || info.Name != "测试成员" {
		t.Fatalf("expected direct reply target, got ok=%v info=%+v", ok, info)
	}

	// 回退到内存缓存
	mod.cacheMsgSender(-100123, 888, 112233, "缓存成员")
	info2, ok2 := mod.resolveReplyTarget(-100123, 888, msgSenderInfo{})
	if !ok2 || info2.UserID != 112233 || info2.Name != "缓存成员" {
		t.Fatalf("expected cached reply target, got ok=%v info=%+v", ok2, info2)
	}

	// 错误文案翻译
	if got := formatTGAdminError(context.DeadlineExceeded); got == "" {
		t.Fatal("expected non-empty error text")
	}
}
