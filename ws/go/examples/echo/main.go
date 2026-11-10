// 一个完整的最小 Go 插件。使用 !go-ping 测试连通性，便于其他作者复制修改。
package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	lwplugin "github.com/AnxiangLemon/lw-plugins/ws/go"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	c, err := lwplugin.New(lwplugin.Config{ID: "example.go.echo", Title: "Go 连通性示例", Version: "0.1.0", Events: []string{lwplugin.MessageReceived}, OnEvent: func(ctx context.Context, c *lwplugin.Client, e lwplugin.Event) error {
		if e.Message == nil || e.Message.Direction == "outbound" || e.Message.From.ID == e.Account.ID || e.Message.Text != "!go-ping" {
			return nil
		}
		// 事件可能重放，幂等键由事件 ID 固定生成，防止重复回复。
		result, err := c.Wechat(e.Account.ID).SendText(ctx, e.Message.Conversation(), "pong（Go 插件）", lwplugin.Options{Key: "echo:" + e.ID, SourceEventID: e.ID})
		if err != nil {
			return err
		}
		slog.Info("回复已提交", "call_id", result.CallID, "status", result.Status)
		return nil
	}})
	if err == nil {
		err = c.Run(ctx)
	}
	if err != nil && !errors.Is(err, context.Canceled) {
		slog.Error("插件停止", "error", err)
		os.Exit(1)
	}
}
