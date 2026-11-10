package lwplugin

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

type recordCaller struct {
	method string
	params map[string]any
	err    error
}

func (r *recordCaller) Request(_ context.Context, m string, p any, out any) error {
	r.method = m
	r.params = p.(map[string]any)
	if r.err != nil {
		return r.err
	}
	if out != nil {
		b, _ := json.Marshal(Result{Status: "succeeded", CallID: "call_1"})
		return json.Unmarshal(b, out)
	}
	return nil
}

// 传输结果不确定时，自动生成的幂等键也必须能恢复，不能只留在局部变量中。
func TestWechatTransportErrorRetainsGeneratedKey(t *testing.T) {
	r := &recordCaller{err: context.DeadlineExceeded}
	_, err := NewWechat(r, "wxid_a").SendText(context.Background(), "room@chatroom", "test", Options{})
	var rpc *RPCError
	if !errors.Is(err, context.DeadlineExceeded) || !errors.As(err, &rpc) {
		t.Fatalf("传输错误没有保留错误链: %v", err)
	}
	if rpc.Key == "" || rpc.Key != r.params["options"].(Options).Key || rpc.AccountID != "wxid_a" {
		t.Fatalf("原回执查询依据丢失: %+v", rpc)
	}
}
func TestWechatScopesAndExactSendShape(t *testing.T) {
	r := &recordCaller{}
	wx := NewWechat(r, "wxid_a")
	ctx := context.Background()
	if _, err := wx.SendText(ctx, "room@chatroom", "欢迎", Options{Key: "notice-1"}); err != nil {
		t.Fatal(err)
	}
	if r.method != "wechat.send" || r.params["account_id"] != "wxid_a" {
		t.Fatal(r)
	}
	options := r.params["options"].(Options)
	if options.Key != "notice-1" {
		t.Fatal("没有保留幂等键")
	}
	if err := wx.Query(ctx, "list", map[string]any{"account_id": "other", "limit": 20}, nil); err != nil {
		t.Fatal(err)
	}
	if r.params["account_id"] != "wxid_a" {
		t.Fatal("查询账号被覆盖")
	}
	if _, err := NewWechat(r, "").Call(ctx, "group.quit", nil, Options{}); err == nil {
		t.Fatal("空账号应拒绝")
	}
}
