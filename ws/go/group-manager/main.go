// 微信群管家：本地 WS 插件 + SQLite 审计 + 内嵌设置页面。
// 程序只使用宿主授予的业务能力，不直接连接微信或自行建立 /ws/sync。
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	lwplugin "github.com/AnxiangLemon/lw-plugins/ws/go"
	"github.com/AnxiangLemon/lw-plugins/ws/go/group-manager/internal/manager"
)

const version = "0.3.2"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	data := flag.String("data", "data/group-manager", "本地数据目录，保存配置、SQLite 和日志")
	listen := flag.String("listen", "127.0.0.1:8766", "本地设置页面地址")
	endpoint := flag.String("endpoint-file", "", "宿主 plugin-endpoint.json 路径，也可用 LW_PLUGIN_ENDPOINT_FILE")
	printVersion := flag.Bool("version", false, "显示程序版本并退出")
	flag.Parse()
	if *printVersion {
		fmt.Println("group-manager " + version)
		return nil
	}
	if err := manager.ListenAddress(*listen); err != nil {
		return err
	}
	configPath := filepath.Join(*data, "config.json")
	config, err := manager.LoadConfig(configPath)
	if err != nil {
		return err
	}
	logFile, err := manager.NewDailyLog(filepath.Join(*data, "logs"), config.RetentionDays)
	if err != nil {
		return err
	}
	defer logFile.Close()
	logger := slog.New(slog.NewJSONHandler(io.MultiWriter(os.Stderr, logFile), nil))
	store, err := manager.OpenStore(filepath.Join(*data, "group-manager.db"))
	if err != nil {
		return err
	}
	defer store.Close()
	var app *manager.Manager
	client, err := lwplugin.New(lwplugin.Config{ID: "wechat.group-manager", Title: "微信群管家", Version: version, EndpointFile: *endpoint, Logger: logger,
		Events: []string{lwplugin.GroupRefreshHint, lwplugin.SyncHint, lwplugin.AccountOnline, lwplugin.AccountOffline},
		Filter: lwplugin.Filter{Platforms: []string{"wechat"}},
		OnConnect: func(_ context.Context, _ *lwplugin.Client, h lwplugin.HostInfo) error {
			logger.Info("已连接宿主，开始同步群列表和已记录群成员", "host_version", h.Version)
			app.Connected()
			return nil
		},
		OnEvent:      func(ctx context.Context, _ *lwplugin.Client, e lwplugin.Event) error { return app.Handle(ctx, e) },
		OnDisconnect: func(err error) { app.Disconnected(err) },
	})
	if err != nil {
		return err
	}
	app, err = manager.New(config, configPath, store, client, logger)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", *listen)
	if err != nil {
		return err
	}
	server := &http.Server{Handler: app.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 20 * time.Second, WriteTimeout: 20 * time.Second, IdleTimeout: 60 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// 各工作循环使用同一个退出上下文；全部结束后才关闭 SQLite。
	done := make(chan struct{})
	go func() { app.Run(ctx); close(done) }()
	clientDone := make(chan struct{})
	go func() {
		defer close(clientDone)
		if err := client.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			logger.Error("插件连接循环结束", "error", err)
		}
	}()
	serverDone := make(chan error, 1)
	go func() { serverDone <- server.Serve(listener) }()
	logger.Info("微信群管家已启动", "version", version, "settings_url", "http://"+listener.Addr().String(), "data_dir", *data)
	select {
	case <-ctx.Done():
	case err = <-serverDone:
		if !errors.Is(err, http.ErrServerClosed) {
			stop()
		}
	}
	stop()
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	closeErr := server.Shutdown(shutdown)
	<-done
	<-clientDone
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return closeErr
}
