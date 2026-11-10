package lwplugin

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

type contractFixture struct {
	Events   []string        `json:"events"`
	Segments []Segment       `json:"segments"`
	Event    json.RawMessage `json:"event"`
	Result   json.RawMessage `json:"result"`
	RPC      []string        `json:"rpc"`
	Endpoint struct {
		URL, Token string
		CleanURL   string `json:"clean_url"`
	} `json:"endpoint"`
}

func contract(t *testing.T) contractFixture {
	t.Helper()
	b, err := os.ReadFile("testdata/sdk-contract.json")
	if err != nil {
		t.Fatal(err)
	}
	var value contractFixture
	if err := Decode(b, &value); err != nil {
		t.Fatal(err)
	}
	return value
}

// 与 Python 共用样例，防止某个语言只在自己的模拟器中通过。
func TestSharedEventsMediaAndFutureFields(t *testing.T) {
	f := contract(t)
	events := AllEvents()
	sort.Strings(events)
	sort.Strings(f.Events)
	if !reflect.DeepEqual(events, f.Events) {
		t.Fatal("事件目录不同")
	}
	segments := []Segment{Text("你好"), At("wxid_fixture"), Image("AA=="), Voice("AA==", 4, 1000),
		Video("AA==", "AQ=="), File("AA==", "说明.txt"), WechatSegment("msg.send_text_message", map[string]string{"content": "你好"})}
	got, _ := json.Marshal(segments)
	want, _ := json.Marshal(f.Segments)
	if string(got) != string(want) {
		t.Fatalf("媒体消息段不同: %s", got)
	}
	var event Event
	if err := Decode(f.Event, &event); err != nil {
		t.Fatal(err)
	}
	if event.TraceID != "trace-1" || event.CallID != "call-1" || len(event.Delivery) == 0 || len(event.Status) == 0 {
		t.Fatal("事件字段丢失")
	}
	var raw map[string]any
	_ = Decode(event.Raw, &raw)
	if raw["future"].(map[string]any)["value"].(json.Number).String() != "18446744073709551615" {
		t.Fatal("大整数丢失")
	}
	var result Result
	if err := Decode(f.Result, &result); err != nil {
		t.Fatal(err)
	}
	if result.OutboxID == nil || *result.OutboxID != 9223372036854775807 || result.Delivered == nil || *result.Delivered || !result.DetailsExpired || result.Revision != 4 {
		t.Fatal("回执字段丢失")
	}
	_ = Decode(result.Original, &raw)
	if raw["future"] != "preserve" {
		t.Fatal("未来字段丢失")
	}
	endpoint, err := ResolveEndpoint(f.Endpoint.URL, f.Endpoint.Token, "")
	if err != nil || endpoint.URL != f.Endpoint.CleanURL || endpoint.Token != f.Endpoint.Token {
		t.Fatal("端点解析不同", err)
	}
}

func TestEmptyFilterIsNotUnrestricted(t *testing.T) {
	c, err := New(Config{ID: "test.go", Filter: Filter{Accounts: []string{}, Platforms: []string{"wechat"}}})
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(c.cfg.Filter)
	if string(encoded) != `{"accounts":[],"platforms":["wechat"]}` {
		t.Fatalf("空范围被扩大: %s", encoded)
	}
	events, _ := json.Marshal(c.cfg.Events)
	if string(events) != "[]" {
		t.Fatalf("空订阅必须为数组: %s", events)
	}
}

func TestReplySequenceCountsExplicitKeysAndKeepsAccount(t *testing.T) {
	c, _ := New(Config{ID: "test.go"})
	e := Event{ID: "source", Account: Account{ID: "account", Platform: "telegram"}}
	ctx := context.WithValue(context.Background(), eventScopeKey{}, &eventScope{id: e.ID})
	_, _ = c.Reply(ctx, e, []Segment{Text("first")}, Options{Key: "explicit"})
	_, err := c.Reply(ctx, e, []Segment{Text("second")}, Options{})
	var rpc *RPCError
	if !errors.As(err, &rpc) || rpc.Key != "reply:source:2" || rpc.AccountID != "account" {
		t.Fatal("回复序号或账号上下文不一致", err)
	}
}

func TestGenericActionsAndErrorRef(t *testing.T) {
	f := contract(t)
	frames := make(chan map[string]any, 4)
	server := endpointServer(t, func(conn *websocket.Conn) {
		for {
			var request map[string]any
			if conn.ReadJSON(&request) != nil {
				return
			}
			if request["type"] == "action" {
				frames <- request
				conn.WriteJSON(map[string]any{"type": "action.ack", "action_id": request["action_id"], "ok": true, "call_id": "original", "status": "queued", "delivered": false, "outbox_id": int64(9223372036854775807)})
			} else if request["method"] == "reject" {
				conn.WriteJSON(map[string]any{"type": "error", "ref": request["req_id"], "code": "not_permitted", "message": "fixture", "call_id": "original", "status": "unknown"})
			} else {
				conn.WriteJSON(map[string]any{"type": "api.result", "req_id": request["req_id"], "ok": true, "data": request["method"]})
			}
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	checked := make(chan error, 1)
	c, _ := New(Config{ID: "test.go", URL: "ws" + strings.TrimPrefix(server.URL, "http"), Token: "test-token", Logger: quiet(), OnConnect: func(ctx context.Context, c *Client, _ HostInfo) error {
		for _, method := range f.RPC {
			var out string
			if err := c.Request(ctx, method, nil, &out); err != nil {
				checked <- err
				return err
			}
			if out != method {
				checked <- errors.New("RPC method 错配")
				return errors.New("mismatch")
			}
		}
		received := Event{Name: GenericMessageReceived, ID: "tg:1", Account: Account{ID: "bot", Platform: "telegram"}}
		callback := context.WithValue(ctx, eventScopeKey{}, &eventScope{id: received.ID, platform: "telegram"})
		result, err := c.Reply(callback, received, []Segment{Text("pong")}, Options{})
		if err != nil || result.CallID != "original" || result.OutboxID == nil || *result.OutboxID != 9223372036854775807 || result.Delivered == nil || *result.Delivered {
			checked <- errors.New("action 回执未完整解析")
			return err
		}
		_, err = c.Send(ctx, "bot", "room", []Segment{Text("notice")}, SendOptions{Platform: "telegram", Options: Options{Key: "stable"}})
		if err != nil {
			checked <- err
			return err
		}
		err = c.Request(ctx, "reject", nil, nil)
		var rpc *RPCError
		if !errors.As(err, &rpc) || rpc.Code != "not_permitted" || rpc.CallID != "original" || rpc.Status != "unknown" {
			checked <- errors.New("error.ref 未还原原错误")
			return err
		}
		checked <- nil
		cancel()
		return nil
	}})
	_ = c.RunOnce(ctx)
	if err := <-checked; err != nil {
		t.Fatal(err)
	}
	reply, sent := <-frames, <-frames
	if reply["kind"] != "reply" || reply["event_id"] != "tg:1" || reply["idempotency_key"] != "reply:tg:1:1" || sent["platform"] != "telegram" || sent["idempotency_key"] != "stable" {
		t.Fatal("action 参数不一致")
	}
}

func TestGenericLedgerScopeAndWechatOptions(t *testing.T) {
	recorder := &recordCaller{}
	queries := Calls{client: recorder, AccountID: "account", Platform: "matrix"}
	for _, method := range []string{"get", "find", "detail", "list", "queue"} {
		if err := queries.Query(context.Background(), method, map[string]any{"account_id": "other", "platform": "other"}, nil); err != nil {
			t.Fatal(err)
		}
		if recorder.method != "calls."+method || recorder.params["platform"] != "matrix" || recorder.params["account_id"] != "account" {
			t.Fatal("账本作用域可覆盖")
		}
	}
	if queries.Query(context.Background(), "wait", nil, nil) == nil {
		t.Fatal("通用账本不提供 wait")
	}
	ctx := context.WithValue(context.Background(), eventScopeKey{}, &eventScope{id: "incoming", platform: "wechat"})
	wait := false
	_, err := NewWechat(recorder, "account").Call(ctx, "group.get_chat_room_member_detail", nil, Options{Wait: &wait})
	if err != nil {
		t.Fatal(err)
	}
	opts := recorder.params["options"].(Options)
	if opts.SourceEventID != "incoming" || opts.Wait == nil || *opts.Wait {
		t.Fatal("source_event_id/wait 未对齐")
	}
}

func TestFilterDecisionBeforeAck(t *testing.T) {
	received := make(chan string, 2)
	server := endpointServer(t, func(conn *websocket.Conn) {
		conn.WriteJSON(map[string]any{"type": "event", "event": MessageFilter, "event_id": "filter-1", "cursor": 1, "account": Account{ID: "a", Platform: "wechat"}})
		for {
			var request struct {
				Method string
				ReqID  string `json:"req_id"`
				Params map[string]any
			}
			if conn.ReadJSON(&request) != nil {
				return
			}
			received <- request.Method
			if request.Method == "wechat.filters.decide" && request.Params["stop"] != true {
				t.Error("没有提交拦截决定")
			}
			conn.WriteJSON(map[string]any{"type": "api.result", "req_id": request.ReqID, "ok": true, "data": nil})
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, _ := New(Config{ID: "test.go", URL: "ws" + strings.TrimPrefix(server.URL, "http"), Token: "test-token", Logger: quiet(), OnFilter: func(context.Context, *Client, Event) (bool, error) { return true, nil }, OnEvent: func(context.Context, *Client, Event) error { t.Error("过滤事件不应走普通回调"); return nil }})
	if len(c.cfg.Events) != 1 || c.cfg.Events[0] != MessageFilter {
		t.Fatal("未自动订阅过滤事件")
	}
	done := make(chan error, 1)
	go func() { done <- c.RunOnce(ctx) }()
	for _, want := range []string{"wechat.filters.decide", "wechat.events.ack"} {
		select {
		case got := <-received:
			if got != want {
				t.Fatal("顺序不正确", got)
			}
		case <-ctx.Done():
			t.Fatal("超时")
		}
	}
	cancel()
	<-done
}

// ACK 未送达时跨会话只补 ACK；同 ID 的另一种事件仍需正常分发。
func TestAckLossAcrossSessionsAndEventNamespace(t *testing.T) {
	var sessions, callbacks atomic.Int32
	server := endpointServer(t, func(conn *websocket.Conn) {
		session := sessions.Add(1)
		conn.WriteJSON(map[string]any{"type": "event", "event": CallUpdated, "event_id": "same", "cursor": 1})
		var request map[string]any
		if conn.ReadJSON(&request) != nil {
			return
		}
		if request["method"] != "wechat.events.ack" {
			t.Error("没有 ACK")
			return
		}
		if session == 1 {
			return
		}
		conn.WriteJSON(map[string]any{"type": "api.result", "req_id": request["req_id"], "ok": true, "data": nil})
		conn.WriteJSON(map[string]any{"type": "event", "event": GroupRefreshHint, "event_id": "same"})
		if conn.ReadJSON(&request) != nil {
			return
		}
		conn.WriteJSON(map[string]any{"type": "api.result", "req_id": request["req_id"], "ok": true, "data": nil})
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, _ := New(Config{ID: "test.go", URL: "ws" + strings.TrimPrefix(server.URL, "http"), Token: "test-token", Logger: quiet(), OnEvent: func(ctx context.Context, c *Client, e Event) error {
		callbacks.Add(1)
		if e.Name == GroupRefreshHint {
			return c.Request(ctx, "received", nil, nil)
		}
		return nil
	}})
	_ = c.RunOnce(ctx)
	_ = c.RunOnce(ctx)
	if callbacks.Load() != 2 {
		t.Fatalf("回调应为两种事件各一次: %d", callbacks.Load())
	}
}
