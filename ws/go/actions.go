package lwplugin

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
)

type eventScopeKey struct{}
type eventScope struct {
	id, platform string
	replies      atomic.Uint64
}

func scopeFrom(ctx context.Context) *eventScope {
	if scope, ok := ctx.Value(eventScopeKey{}).(*eventScope); ok {
		return scope
	}
	return &eventScope{}
}

// SendOptions 显式选择主动发送的平台。空 Platform 默认微信，不沿用入站平台。
// 通用平台仅使用 Key；微信还支持 SourceEventID、Wait 和 WaitTimeoutMS。
type SendOptions struct {
	Platform string
	Options
}

// Send 主动发送：微信走 wechat.send，其他平台走通用 action.send。
// queued/running 只代表已受理，最终结果须通过对应的调用账本查询。
func (c *Client) Send(ctx context.Context, account, to string, segments []Segment, opts SendOptions) (Result, error) {
	platform := opts.Platform
	if platform == "" {
		platform = "wechat"
	}
	if strings.TrimSpace(account) == "" || strings.TrimSpace(to) == "" || strings.TrimSpace(platform) == "" {
		return Result{}, errors.New("account_id、to 和 platform 不能为空")
	}
	if platform == "wechat" {
		return c.Wechat(account).Send(ctx, to, segments, opts.Options)
	}
	return c.action(ctx, map[string]any{"kind": "send", "platform": platform,
		"account_id": account, "to": to, "segments": segments}, opts.Key, account)
}

// Reply 沿用事件平台和目标；在 OnEvent 中使用收到的 ctx 可自动生成稳定的回复序号。
// 在回调外回复时须显式传 Key，避免多个回复错误地共用一个键。
func (c *Client) Reply(ctx context.Context, event Event, segments []Segment, opts Options) (Result, error) {
	if event.ID == "" || event.Account.Platform == "" {
		return Result{}, errors.New("回复需要 event_id 和来源平台")
	}
	scope := scopeFrom(ctx)
	var index uint64
	if scope.id == event.ID {
		index = scope.replies.Add(1) // 显式 Key 的回复也占用序号，与 Python 一致。
	}
	if opts.Key == "" {
		if index == 0 {
			return Result{}, errors.New("回调外回复必须指定幂等键 Key")
		}
		opts.Key = fmt.Sprintf("reply:%s:%d", event.ID, index)
	}
	if event.Account.Platform == "wechat" {
		if event.Message == nil || event.Message.Conversation() == "" {
			return Result{}, errors.New("微信回复缺少会话目标")
		}
		if opts.SourceEventID == "" {
			opts.SourceEventID = event.ID
		}
		return c.Wechat(event.Account.ID).Send(ctx, event.Message.Conversation(), segments, opts)
	}
	return c.action(ctx, map[string]any{"kind": "reply", "platform": event.Account.Platform,
		"event_id": event.ID, "segments": segments}, opts.Key, event.Account.ID)
}

func (c *Client) action(ctx context.Context, payload map[string]any, key, account string) (Result, error) {
	if key == "" {
		key = NewKey()
	}
	payload["v"], payload["type"], payload["idempotency_key"] = 2, "action", key
	var result Result
	err := c.request(ctx, payload, &result)
	if err != nil {
		var rpc *RPCError
		if !errors.As(err, &rpc) {
			rpc = &RPCError{Code: "request_error", Message: err.Error(), Cause: err}
			err = rpc
		}
		rpc.Key, rpc.AccountID = key, account
	}
	return result, err
}

// Calls 是通用平台账本查询句柄；微信业务账本使用 Wechat.Query。
// Platform 留空时，查询沿用事件 ctx 的平台，回调外默认 telegram（兼容 Python）。
type Calls struct {
	client              Caller
	AccountID, Platform string
}

// Calls 绑定账号，可选的 platform 建议在后台任务中显式给出。
func (c *Client) Calls(account string, platform ...string) Calls {
	value := Calls{client: c, AccountID: account}
	if len(platform) > 0 {
		value.Platform = platform[0]
	}
	return value
}

// Query 支持 get/find/detail/list/queue。通用账本没有 wait，不能冒充微信等待接口。
// filters 中的 account_id 和 platform 不得覆盖句柄作用域。
func (q Calls) Query(ctx context.Context, action string, filters map[string]any, out any) error {
	if strings.TrimSpace(q.AccountID) == "" {
		return errors.New("account_id 不能为空")
	}
	switch action {
	case "get", "find", "detail", "list", "queue":
	default:
		return errors.New("未知通用账本查询")
	}
	platform := q.Platform
	if platform == "" {
		platform = scopeFrom(ctx).platform
	}
	if platform == "" {
		platform = "telegram"
	}
	if strings.TrimSpace(platform) == "" {
		return errors.New("platform 不能为空")
	}
	params := make(map[string]any, len(filters)+2)
	for k, v := range filters {
		params[k] = v
	}
	params["account_id"], params["platform"] = q.AccountID, platform
	return q.client.Request(ctx, "calls."+action, params, out)
}
