package lwplugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

var ErrNotConnected = errors.New("插件尚未连接宿主")

// Config 使用标准库类型，不依赖应用框架。OnEvent 按接收顺序串行调用，
// 耗时任务应由插件排入自己的有界队列，避免阻塞后续事件确认。
type Config struct {
	// ID 是宿主持久化权限、回执和重放游标的归属，发布后应保持稳定。
	ID, Title, Version string
	// URL/Token 可省略，由 EndpointFile 或 LW_PLUGIN_* 环境变量补齐。
	URL, Token, EndpointFile string
	// 只订阅明确列出的事件；不支持的事件会在连接初始化时报错。
	Events         []string
	Filter         Filter
	RequestTimeout time.Duration
	Logger         *slog.Logger
	OnEvent        func(context.Context, *Client, Event) error
	// OnConnect 在每次成功重连后执行，可用于恢复快照；传入 ctx 随会话断开取消。
	OnConnect    func(context.Context, *Client, HostInfo) error
	OnDisconnect func(error)
}

// Client 允许多个 goroutine 并发发起 RPC，底层只有一个读取循环和一个写锁。
// 重连只恢复连接/订阅，不重发在途 API；所有在途请求都会明确返回连接错误。
type Client struct {
	cfg     Config
	mu      sync.Mutex
	conn    *websocket.Conn
	pending map[string]chan frame
	host    HostInfo
	writeMu chan struct{}
	seq     atomic.Uint64
	running atomic.Bool
}
type frame struct {
	Type     string          `json:"type"`
	V        int             `json:"v"`
	ReqID    string          `json:"req_id"`
	ActionID string          `json:"action_id"`
	Ref      string          `json:"ref"`
	OK       bool            `json:"ok"`
	Code     string          `json:"code"`
	Message  string          `json:"message"`
	Data     json.RawMessage `json:"data"`
	Reason   string          `json:"reason"`
}

func New(cfg Config) (*Client, error) {
	if len(cfg.ID) < 1 || len(cfg.ID) > 64 || strings.Trim(cfg.ID, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._-") != "" {
		return nil, errors.New("plugin ID 仅支持 1..64 字节的字母、数字、点、下划线和连字符")
	}
	if cfg.RequestTimeout <= 0 {
		cfg.RequestTimeout = 65 * time.Second
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	cfg.Events = append([]string(nil), cfg.Events...)
	return &Client{cfg: cfg, pending: make(map[string]chan frame), writeMu: make(chan struct{}, 1)}, nil
}
func (c *Client) Connected() bool { c.mu.Lock(); defer c.mu.Unlock(); return c.conn != nil }
func (c *Client) Host() HostInfo {
	c.mu.Lock()
	defer c.mu.Unlock()
	h := c.host
	h.Events = append([]string(nil), h.Events...)
	h.Capabilities = append([]string(nil), h.Capabilities...)
	return h
}

// Run 阻塞至 ctx 取消。网络错误按 1..30 秒退避；取消时立即关闭连接。
func (c *Client) Run(ctx context.Context) error {
	if !c.running.CompareAndSwap(false, true) {
		return errors.New("Client.Run 已在运行")
	}
	defer c.running.Store(false)
	delay := time.Second
	for ctx.Err() == nil {
		started := time.Now()
		err := c.session(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if c.cfg.OnDisconnect != nil {
			c.cfg.OnDisconnect(err)
		}
		c.cfg.Logger.Warn("插件连接中断，等待重连", "error", err, "retry_after", delay)
		if time.Since(started) > time.Minute {
			delay = time.Second
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		if delay < 30*time.Second {
			delay *= 2
			if delay > 30*time.Second {
				delay = 30 * time.Second
			}
		}
	}
	return ctx.Err()
}

func (c *Client) session(parent context.Context) error {
	ep, err := ResolveEndpoint(c.cfg.URL, c.cfg.Token, c.cfg.EndpointFile)
	if err != nil {
		return err
	}
	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second, Proxy: http.ProxyFromEnvironment}
	conn, resp, err := dialer.DialContext(parent, ep.URL, http.Header{"Authorization": []string{"Bearer " + ep.Token}})
	if resp != nil && resp.Body != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		return fmt.Errorf("WS 握手失败: %w", err)
	}
	defer conn.Close()
	conn.SetReadLimit(128 << 20)
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		select {
		case <-ctx.Done():
			conn.Close()
		case <-stop:
		}
	}()
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	if err = c.write(ctx, conn, map[string]any{"v": 2, "type": "hello", "plugin_id": c.cfg.ID, "title": c.cfg.Title, "version": c.cfg.Version}); err != nil {
		return err
	}
	_, raw, err := conn.ReadMessage()
	if err != nil {
		return err
	}
	var hello frame
	if json.Unmarshal(raw, &hello) != nil || hello.Type != "hello.ok" {
		return errors.New("宿主未接受插件 hello；请检查插件是否启用")
	}
	var host HostInfo
	if err = json.Unmarshal(raw, &host); err != nil {
		return err
	}
	for _, want := range c.cfg.Events {
		found := false
		for _, name := range host.Events {
			if name == want {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("宿主不支持事件 %s，请更新宿主", want)
		}
	}
	if err = c.write(ctx, conn, map[string]any{"v": 2, "type": "subscribe", "events": c.cfg.Events, "filter": c.cfg.Filter}); err != nil {
		return err
	}
	// 先完成订阅再触发 OnConnect；重连核对期间收到的事件会排队处理。
	var early []Event
	for {
		_, raw, err = conn.ReadMessage()
		if err != nil {
			return err
		}
		var subscribed frame
		if err = json.Unmarshal(raw, &subscribed); err != nil {
			return err
		}
		if subscribed.Type == "subscribe.ok" {
			break
		}
		if subscribed.Type == "error" || subscribed.Type == "bye" {
			return errors.New("宿主未接受插件事件订阅")
		}
		// 重放队列和实时总线可能先于 subscribe.ok 到达，不能把它当成握手失败。
		if subscribed.Type == "event" {
			var e Event
			if err = json.Unmarshal(raw, &e); err != nil {
				return err
			}
			e.Raw = append(json.RawMessage(nil), raw...)
			early = append(early, e)
			if len(early) > 256 {
				return errors.New("订阅期间事件过多，请缩小订阅范围")
			}
		}
	}
	c.mu.Lock()
	c.conn = conn
	c.host = host
	c.mu.Unlock()
	defer func() {
		cancel()
		conn.Close()
		c.mu.Lock()
		if c.conn == conn {
			c.conn = nil
			for id, ch := range c.pending {
				ch <- frame{Code: "disconnected", Message: "连接已断开；请按原幂等键查询结果"}
				delete(c.pending, id)
			}
		}
		c.mu.Unlock()
	}()
	queue := make(chan Event, 256)
	for _, e := range early {
		queue <- e
	}
	go c.events(ctx, conn, queue, host)
	conn.SetPingHandler(func(data string) error {
		conn.SetReadDeadline(time.Now().Add(90 * time.Second))
		return conn.WriteControl(websocket.PongMessage, []byte(data), time.Now().Add(5*time.Second))
	})
	for {
		conn.SetReadDeadline(time.Now().Add(90 * time.Second))
		_, raw, err = conn.ReadMessage()
		if err != nil {
			return err
		}
		var f frame
		if err = json.Unmarshal(raw, &f); err != nil {
			return fmt.Errorf("无效宿主帧: %w", err)
		}
		switch f.Type {
		case "api.result", "action.ack", "error":
			id := f.ReqID
			if id == "" {
				id = f.ActionID
			}
			if id == "" {
				id = f.Ref
			}
			c.mu.Lock()
			if ch, ok := c.pending[id]; ok {
				delete(c.pending, id)
				ch <- f
			}
			c.mu.Unlock()
		case "event":
			var event Event
			if err = json.Unmarshal(raw, &event); err != nil {
				return err
			}
			event.Raw = append(json.RawMessage(nil), raw...)
			select {
			case queue <- event:
			default:
				return errors.New("事件队列已满，重连后需重新核对业务状态")
			}
		case "bye":
			return fmt.Errorf("宿主关闭连接: %s", f.Reason)
		}
	}
}

func (c *Client) events(ctx context.Context, conn *websocket.Conn, queue <-chan Event, host HostInfo) {
	if c.cfg.OnConnect != nil {
		if err := c.cfg.OnConnect(ctx, c, host); err != nil {
			c.cfg.Logger.Error("连接初始化失败", "error", err)
			conn.Close()
			return
		}
	}
	seen := make(map[string]bool)
	var order []string
	for {
		select {
		case <-ctx.Done():
			return
		case event := <-queue:
			if !seen[event.ID] && c.cfg.OnEvent != nil {
				if err := c.cfg.OnEvent(ctx, c, event); err != nil {
					c.cfg.Logger.Error("事件处理失败，保留未确认游标", "event", event.Name, "error", err)
					conn.Close()
					return
				}
			}
			if event.Cursor != nil {
				if err := c.Request(ctx, "wechat.events.ack", map[string]any{"cursor": *event.Cursor}, nil); err != nil {
					conn.Close()
					return
				}
			}
			if event.ID != "" && !seen[event.ID] {
				seen[event.ID] = true
				order = append(order, event.ID)
				if len(order) > 2048 {
					delete(seen, order[0])
					order = order[1:]
				}
			}
		}
	}
}

func (c *Client) write(ctx context.Context, conn *websocket.Conn, value any) error {
	// 写锁也尊重取消，不能让短超时请求排在慢写后无限等待。
	select {
	case c.writeMu <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-c.writeMu }()
	if err := ctx.Err(); err != nil {
		return err
	}
	deadline := time.Now().Add(10 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	conn.SetWriteDeadline(deadline)
	return conn.WriteJSON(value)
}

// Request 调用任意宿主公开 RPC。RPC ok 与业务 Result.Status 是不同层级。
func (c *Client) Request(ctx context.Context, method string, params any, out any) error {
	return c.request(ctx, map[string]any{"v": 2, "type": "api.call", "method": method, "params": params}, out)
}
func (c *Client) request(parent context.Context, value map[string]any, out any) error {
	ctx, cancel := context.WithTimeout(parent, c.cfg.RequestTimeout)
	defer cancel()
	id := fmt.Sprintf("go_%d", c.seq.Add(1))
	key := "req_id"
	if value["type"] == "action" {
		key = "action_id"
	}
	value[key] = id
	ch := make(chan frame, 1)
	c.mu.Lock()
	conn := c.conn
	if conn == nil {
		c.mu.Unlock()
		return ErrNotConnected
	}
	c.pending[id] = ch
	c.mu.Unlock()
	defer func() { c.mu.Lock(); delete(c.pending, id); c.mu.Unlock() }()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := c.write(ctx, conn, value); err != nil {
		if !errors.Is(err, ctx.Err()) {
			conn.Close()
		}
		return err
	}
	select {
	case <-ctx.Done():
		return &RPCError{Code: "timeout", Message: "等待已结束，业务可能仍在执行，请查询原回执", Cause: ctx.Err()}
	case response := <-ch:
		if !response.OK {
			return &RPCError{Code: response.Code, Message: response.Message}
		}
		if out == nil {
			return nil
		}
		return Decode(response.Data, out)
	}
}

// Log 将普通诊断文字写到宿主日志。详细业务审计仍应由插件自行持久化。
func (c *Client) Log(ctx context.Context, level, message string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()
	if conn == nil {
		return ErrNotConnected
	}
	err := c.write(ctx, conn, map[string]any{"v": 2, "type": "log", "level": level, "message": message})
	if err != nil && !errors.Is(err, ctx.Err()) {
		conn.Close()
	}
	return err
}
