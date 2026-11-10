package manager

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	lwplugin "github.com/AnxiangLemon/lw-plugins/ws/go"
)

// TestRealRustHost 由 lw-sdk 的测试创建真实 Rust WS 宿主后启动。
// 普通 go test 会跳过它；其他测试仍完全自包含，不要求私有 SDK 仓库。
func TestRealRustHost(t *testing.T) {
	address := os.Getenv("LW_GO_TEST_URL")
	if address == "" {
		t.Skip("由 Rust 联调脚本提供临时宿主")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	store := openTestStore(t)
	config := DefaultConfig()
	config.DebounceMS = 100
	config.CooldownSeconds = 1
	config.WeakRecheckSeconds = 1
	config.Groups = []Group{{AccountID: "wxid_go", RoomID: "room@chatroom", Enabled: true}}
	var manager *Manager
	client, err := lwplugin.New(lwplugin.Config{ID: "test.go.group-manager", URL: address, Token: os.Getenv("LW_GO_TEST_TOKEN"), Events: []string{lwplugin.GroupRefreshHint, lwplugin.SyncHint}, Logger: testLog(), OnConnect: func(context.Context, *lwplugin.Client, lwplugin.HostInfo) error { manager.Connected(); return nil }, OnEvent: func(ctx context.Context, _ *lwplugin.Client, event lwplugin.Event) error {
		return manager.Handle(ctx, event)
	}})
	if err != nil {
		t.Fatal(err)
	}
	manager, err = New(config, filepath.Join(t.TempDir(), "config.json"), store, client, testLog())
	if err != nil {
		t.Fatal(err)
	}
	workers := make(chan struct{})
	go func() { manager.Run(ctx); close(workers) }()
	connection := make(chan error, 1)
	go func() { connection <- client.Run(ctx) }()
	defer func() { cancel(); <-workers; <-connection }()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			t.Fatal("未收到基线、入群和离群三个完整阶段")
		case <-tick.C:
			rows, err := store.Events("", "", 100)
			if err != nil {
				t.Fatal(err)
			}
			joined, left := false, false
			for _, e := range rows {
				joined = joined || (e.Kind == "joined" && e.MemberID == "b")
				left = left || (e.Kind == "left" && e.MemberID == "a")
				if e.NotifyStatus != "disabled" {
					t.Fatal("默认配置产生了群消息发送")
				}
			}
			if joined && left {
				snapshot, err := store.Members("wxid_go", "room@chatroom")
				if err != nil || len(snapshot.Members) != 2 {
					t.Fatal("最终基线错误")
				}
				t.Log("PASS: Rust 实时提示 → Go SDK → 完整名单差集 → SQLite 入群/离群日志；没有发送群消息")
				return
			}
		}
	}
}
