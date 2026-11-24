package blacklist

import "testing"

func TestStoreGlobalPlatformBlacklistAndPersistence(t *testing.T) {
	dir := t.TempDir()
	st := New(dir)

	// 1. 添加并验证同一平台全群/全账号通用命中
	e, created := st.Add(Entry{
		Platform:        "Telegram",
		UserID:          "123456",
		UserName:        "测试用户",
		Reason:          "被移出群聊自动拉黑",
		SourceAccountID: "bot_1",
		SourceChatID:    "-100111",
		SourceChatTitle: "A群",
	})
	if !created || e.Platform != "telegram" || len(e.BannedChats) != 1 || e.BannedChats[0] != "-100111" {
		t.Fatalf("unexpected Add result: created=%v entry=%+v", created, e)
	}

	if got, ok := st.Contains("telegram", "123456"); !ok || got.UserName != "测试用户" {
		t.Fatalf("expected telegram user to be blacklisted globally, got ok=%v entry=%+v", ok, got)
	}
	// 不同平台互不干扰
	if _, ok := st.Contains("wechat", "123456"); ok {
		t.Fatal("different platform should not match")
	}

	// 2. 在另一个群再次被拦截后记录 BannedChats
	st.RecordBannedChat("telegram", "123456", "-100222")
	st.Add(Entry{
		Platform:        "wechat",
		UserID:          "wxid_badguy",
		UserName:        "广告号",
		Reason:          "管理员移出群聊自动拉黑",
		SourceAccountID: "wxid_bot1",
		SourceChatID:    "43358773958@chatroom",
		SourceChatTitle: "微信测试群",
	})

	// 3. 重启重新加载持久化数据
	st2 := New(dir)
	if st2.Count() != 2 {
		t.Fatalf("expected 2 entries after reload, got %d", st2.Count())
	}
	gotTG, ok := st2.Contains("telegram", "123456")
	if !ok || len(gotTG.BannedChats) != 2 {
		t.Fatalf("expected 2 banned chats persisted, got ok=%v entry=%+v", ok, gotTG)
	}
	if len(st2.List("wechat")) != 1 || len(st2.List("telegram")) != 1 || len(st2.List("all")) != 2 {
		t.Fatalf("unexpected platform filter counts")
	}

	// 4. 移除记录
	removed, ok := st2.Remove("telegram", "123456")
	if !ok || removed.UserID != "123456" {
		t.Fatalf("expected removal to succeed, got ok=%v entry=%+v", ok, removed)
	}
	if _, still := st2.Contains("telegram", "123456"); still {
		t.Fatal("user should no longer be blacklisted")
	}
}
