package lwplugin

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }
func endpointServer(t *testing.T, serve func(*websocket.Conn)) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" || r.URL.Query().Get("token") != "" {
			t.Error("token 应只通过握手 Header 发送")
			http.Error(w, "auth", http.StatusUnauthorized)
			return
		}
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		var hello map[string]any
		if conn.ReadJSON(&hello) != nil {
			return
		}
		if hello["type"] != "hello" || hello["plugin_id"] != "test.go" {
			t.Error("错误的 hello")
		}
		conn.WriteJSON(map[string]any{"v": 2, "type": "hello.ok", "host_version": "test", "events": AllEvents(), "capabilities": []string{"wechat.v2"}})
		var subscribe map[string]any
		if conn.ReadJSON(&subscribe) != nil {
			return
		}
		if subscribe["type"] != "subscribe" {
			t.Error("错误的订阅帧")
		}
		conn.WriteJSON(map[string]any{"v": 2, "type": "subscribe.ok"})
		serve(conn)
	}))
	t.Cleanup(s.Close)
	return s
}

// 回调发 RPC 时读取循环必须继续工作，否则会出现 Python SDK 曾明确规避的死锁。
func TestCallbacksRPCDurableAckAndIntegerPrecision(t *testing.T) {
	acked := make(chan struct{}, 2)
	nested := make(chan struct{}, 1)
	var handlers atomic.Int32
	server := endpointServer(t, func(conn *websocket.Conn) {
		for {
			var f struct {
				Type, Method string
				ReqID        string `json:"req_id"`
			}
			if conn.ReadJSON(&f) != nil {
				return
			}
			switch f.Method {
			case "init":
				conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"api.result","ok":true,"req_id":"`+f.ReqID+`","data":{"id":18446744073709551615}}`))
				e := map[string]any{"type": "event", "event": CallUpdated, "event_id": "durable-1", "cursor": 7, "account": map[string]string{"id": "wxid_a", "platform": "wechat"}}
				conn.WriteJSON(e)
				conn.WriteJSON(e)
			case "nested":
				conn.WriteJSON(map[string]any{"type": "api.result", "ok": true, "req_id": f.ReqID, "data": map[string]any{}})
				nested <- struct{}{}
			case "wechat.events.ack":
				conn.WriteJSON(map[string]any{"type": "api.result", "ok": true, "req_id": f.ReqID, "data": nil})
				acked <- struct{}{}
			}
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, err := New(Config{ID: "test.go", URL: "ws" + strings.TrimPrefix(server.URL, "http"), Token: "test-token", Events: []string{CallUpdated}, Logger: quiet(), OnConnect: func(ctx context.Context, c *Client, _ HostInfo) error {
		var v map[string]any
		if err := c.Request(ctx, "init", map[string]any{}, &v); err != nil {
			return err
		}
		n, ok := v["id"].(json.Number)
		if !ok || n.String() != "18446744073709551615" {
			t.Error("64 位 ID 丢失精度")
		}
		return nil
	}, OnEvent: func(ctx context.Context, c *Client, e Event) error {
		handlers.Add(1)
		return c.Request(ctx, "nested", map[string]any{}, nil)
	}})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- client.Run(ctx) }()
	for i := 0; i < 2; i++ {
		select {
		case <-acked:
		case <-ctx.Done():
			t.Fatal("回调或 ACK 超时")
		}
	}
	select {
	case <-nested:
	default:
		t.Fatal("回调没有收到 RPC 结果")
	}
	if handlers.Load() != 1 {
		t.Fatal("重复事件不应重复处理")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("取消后未退出")
	}
}

func TestMutationTimeoutKeepsKeyAndNeverResends(t *testing.T) {
	ready := make(chan struct{})
	var requests atomic.Int32
	server := endpointServer(t, func(conn *websocket.Conn) {
		for {
			var f map[string]any
			if conn.ReadJSON(&f) != nil {
				return
			}
			if f["method"] == "wechat.call" {
				requests.Add(1)
			}
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, _ := New(Config{ID: "test.go", URL: "ws" + strings.TrimPrefix(server.URL, "http"), Token: "test-token", RequestTimeout: 40 * time.Millisecond, Logger: quiet(), OnConnect: func(context.Context, *Client, HostInfo) error { close(ready); return nil }})
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()
	select {
	case <-ready:
	case <-time.After(3 * time.Second):
		t.Fatal("连接超时")
	}
	_, err := c.Wechat("wxid_a").Call(ctx, "msg.send_text_message", map[string]string{"toWxid": "friend", "content": "hello"}, Options{Key: "stable-key"})
	var rpc *RPCError
	if !errors.As(err, &rpc) || rpc.Key != "stable-key" || rpc.AccountID != "wxid_a" || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("丢失超时上下文: %#v", err)
	}
	time.Sleep(70 * time.Millisecond)
	if requests.Load() != 1 {
		t.Fatal("超时调用被自动重发")
	}
	cancel()
	<-done
}

func TestEndpointPrecedenceIPv6AndSecretFreeURL(t *testing.T) {
	t.Setenv("LW_PLUGIN_URL", "")
	t.Setenv("LW_PLUGIN_TOKEN", "")
	t.Setenv("LW_PLUGIN_ENDPOINT_FILE", "")
	path := filepath.Join(t.TempDir(), "plugin-endpoint.json")
	if err := os.WriteFile(path, []byte(`{"host":"::1","port":12345,"token":"file-token"}`), 0600); err != nil {
		t.Fatal(err)
	}
	e, err := ResolveEndpoint("", "", path)
	if err != nil || e.URL != "ws://[::1]:12345/" || e.Token != "file-token" {
		t.Fatalf("%+v %v", e, err)
	}
	e, err = ResolveEndpoint("wss://example.test/plugin?token=url-secret", "explicit-token", path)
	if err != nil || strings.Contains(e.URL, "secret") || e.Token != "explicit-token" {
		t.Fatal("显式配置或 token 清理失败")
	}
	for _, u := range []string{"http://example.test", "ws://user:pass@example.test", "ws://"} {
		if _, err := checkedEndpoint(u, "secret"); err == nil {
			t.Fatal("接受了无效地址")
		}
	}
}

func TestOutOfOrderConcurrentRPC(t *testing.T) {
	ready := make(chan struct{})
	s := endpointServer(t, func(conn *websocket.Conn) {
		type request struct {
			ReqID  string `json:"req_id"`
			Method string
		}
		var a, b request
		if conn.ReadJSON(&a) != nil || conn.ReadJSON(&b) != nil {
			return
		}
		for _, r := range []request{b, a} {
			conn.WriteJSON(map[string]any{"type": "api.result", "ok": true, "req_id": r.ReqID, "data": r.Method})
		}
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, _ := New(Config{ID: "test.go", URL: "ws" + strings.TrimPrefix(s.URL, "http"), Token: "test-token", Logger: quiet(), OnConnect: func(context.Context, *Client, HostInfo) error { close(ready); return nil }})
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()
	select {
	case <-ready:
	case <-time.After(3 * time.Second):
		t.Fatal("连接超时")
	}
	results := make(chan error, 2)
	for _, method := range []string{"first", "second"} {
		go func(method string) {
			var out string
			err := c.Request(ctx, method, map[string]any{}, &out)
			if err == nil && out != method {
				err = errors.New("请求与响应错配")
			}
			results <- err
		}(method)
	}
	for i := 0; i < 2; i++ {
		select {
		case err := <-results:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(time.Second):
			t.Fatal("RPC 超时")
		}
	}
	cancel()
	<-done
}

// 已经写出的业务帧遇到断线时只报不确定，不因为恢复连接而重新发出。
func TestReconnectNeverReplaysInflightMutation(t *testing.T) {
	var calls, connections atomic.Int32
	restored := make(chan struct{})
	s := endpointServer(t, func(conn *websocket.Conn) {
		for {
			var f map[string]any
			if conn.ReadJSON(&f) != nil {
				return
			}
			if f["method"] == "wechat.call" {
				calls.Add(1)
				return
			}
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _ := New(Config{ID: "test.go", URL: "ws" + strings.TrimPrefix(s.URL, "http"), Token: "test-token", Logger: quiet(), OnConnect: func(ctx context.Context, c *Client, _ HostInfo) error {
		if connections.Add(1) == 1 {
			_, err := c.Wechat("wxid_a").Call(ctx, "msg.send_text_message", map[string]string{"toWxid": "friend", "content": "hello"}, Options{Key: "lost-result"})
			return err
		}
		close(restored)
		return nil
	}})
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()
	select {
	case <-restored:
	case <-ctx.Done():
		t.Fatal("没有恢复连接")
	}
	if calls.Load() != 1 {
		t.Fatal("重连重复发送了业务帧")
	}
	cancel()
	<-done
}

func TestWriteLockWaitCanBeCancelled(t *testing.T) {
	c, _ := New(Config{ID: "test.go"})
	c.writeMu <- struct{}{}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	started := time.Now()
	err := c.write(ctx, nil, map[string]any{})
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(started) > time.Second {
		t.Fatal("等待写锁没有尊重 context")
	}
}

// 宿主总线与订阅确认有竞态，合法的早到事件必须保留。
func TestEventBeforeSubscriptionAck(t *testing.T) {
	upgrader := websocket.Upgrader{}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		var raw map[string]any
		if conn.ReadJSON(&raw) != nil {
			return
		}
		conn.WriteJSON(map[string]any{"type": "hello.ok", "events": []string{GroupRefreshHint}})
		if conn.ReadJSON(&raw) != nil {
			return
		}
		conn.WriteJSON(map[string]any{"type": "event", "event": GroupRefreshHint, "event_id": "early", "account": map[string]string{"id": "wxid_a", "platform": "wechat"}, "data": map[string]string{"room_id": "room@chatroom"}})
		conn.WriteJSON(map[string]any{"type": "subscribe.ok"})
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer s.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	received := make(chan struct{}, 1)
	c, _ := New(Config{ID: "test.go", URL: "ws" + strings.TrimPrefix(s.URL, "http"), Token: "test-token", Events: []string{GroupRefreshHint}, Logger: quiet(), OnEvent: func(context.Context, *Client, Event) error { received <- struct{}{}; return nil }})
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()
	select {
	case <-received:
	case <-ctx.Done():
		t.Fatal("早到提示被丢失")
	}
	cancel()
	<-done
}
