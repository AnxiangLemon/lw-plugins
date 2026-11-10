package lwplugin

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
)

// Options 的 Key 对应一个业务动作。网络超时后保存原 Key，不能换 Key 盲目重发。
type Options struct {
	Key           string `json:"idempotency_key,omitempty"`
	SourceEventID string `json:"source_event_id,omitempty"`
	WaitTimeoutMS int    `json:"wait_timeout_ms,omitempty"`
	// nil 沿用宿主默认等待；指向 false 可让 Call/Send 只提交、不等待执行完成。
	Wait *bool `json:"wait,omitempty"`
}

// NewKey 生成不含业务含义的随机幂等键。一个动作只生成一次，并与本地任务一起保存。
func NewKey() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("无法取得系统随机数: " + err.Error())
	}
	return hex.EncodeToString(b[:])
}

// Wechat 将业务调用与回执查询固定到一个账号，避免复用连接时串用账号。
type Wechat struct {
	client    Caller
	AccountID string
}

// Caller 使业务封装可用于模拟宿主；SDK 与业务测试不必建立真实账号连接。
type Caller interface {
	Request(context.Context, string, any, any) error
}

// NewWechat 可接受真实 Client 或测试用 Caller，业务层无需依赖具体连接实现。
func NewWechat(client Caller, account string) Wechat {
	return Wechat{client: client, AccountID: account}
}

// Wechat 返回当前账号的微信调用句柄；不会建立额外连接。
func (c *Client) Wechat(account string) Wechat { return Wechat{client: c, AccountID: account} }
func (w Wechat) invoke(ctx context.Context, action, method string, params any, opts Options) (Result, error) {
	if w.AccountID == "" {
		return Result{}, errors.New("account_id 不能为空")
	}
	if opts.Key == "" {
		opts.Key = NewKey()
	}
	if opts.SourceEventID == "" {
		opts.SourceEventID = scopeFrom(ctx).id
	}
	if params == nil {
		params = map[string]any{}
	}
	var result Result
	payload := map[string]any{"account_id": w.AccountID, "params": params, "options": opts}
	if action != "wechat.send" {
		payload["method"] = method
	}
	err := w.client.Request(ctx, action, payload, &result)
	var rpc *RPCError
	if err != nil {
		// 写入失败、未连接等错误也携带原 Key。即使调用方省略 Key，
		// 仍可通过 errors.As 取得自动生成的键，再查询原回执。
		if !errors.As(err, &rpc) {
			rpc = &RPCError{Code: "request_error", Message: err.Error(), Cause: err}
			err = rpc
		}
		rpc.Key = opts.Key
		rpc.AccountID = w.AccountID
	}
	return result, err
}

// Call 按宿主默认等待策略执行业务；返回 queued/running 时可继续 Await。
func (w Wechat) Call(ctx context.Context, method string, params any, opts Options) (Result, error) {
	return w.invoke(ctx, "wechat.call", method, params, opts)
}

// Submit 尽快返回入队回执，适用于需要自行管理后续进度的插件。
func (w Wechat) Submit(ctx context.Context, method string, params any, opts Options) (Result, error) {
	return w.invoke(ctx, "wechat.submit", method, params, opts)
}

// Send 通过宿主发送消息段；媒体等高级段保持协议原样，不在 SDK 内另作转换。
func (w Wechat) Send(ctx context.Context, to string, segments []Segment, opts Options) (Result, error) {
	return w.invoke(ctx, "wechat.send", "", map[string]any{"to": to, "segments": segments}, opts)
}

// SendText 是单个文本消息段的便捷封装，幂等与回执规则同 Send。
func (w Wechat) SendText(ctx context.Context, to, text string, opts Options) (Result, error) {
	return w.Send(ctx, to, []Segment{Text(text)}, opts)
}

// Methods 查询宿主当前公开的方法目录，不在 SDK 内复制一份接口白名单。
func (w Wechat) Methods(ctx context.Context) (json.RawMessage, error) {
	if w.AccountID == "" {
		return nil, errors.New("account_id 不能为空")
	}
	var data json.RawMessage
	err := w.client.Request(ctx, "wechat.methods", map[string]any{"account_id": w.AccountID}, &data)
	return data, err
}

// SendImage 读取插件机器上的图片，不把路径发送给宿主；需要时由业务先准备 CDN DNS。
func (w Wechat) SendImage(ctx context.Context, to, path string, opts Options) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	segment, err := ImageFile(path)
	if err != nil {
		return Result{}, err
	}
	return w.Send(ctx, to, []Segment{segment}, opts)
}

// Query 强制使用当前账号，filters 中的 account_id 不能覆盖作用域。
// 支持 get/find/detail/list/queue/wait；out 可为结构体或 json.RawMessage。
func (w Wechat) Query(ctx context.Context, action string, filters map[string]any, out any) error {
	if w.AccountID == "" {
		return errors.New("account_id 不能为空")
	}
	switch action {
	case "get", "find", "detail", "list", "queue", "wait":
	default:
		return errors.New("未知调用记录查询")
	}
	params := make(map[string]any, len(filters)+1)
	for k, v := range filters {
		params[k] = v
	}
	params["account_id"] = w.AccountID
	return w.client.Request(ctx, "wechat.calls."+action, params, out)
}

// Await 只查询原调用，不产生新的业务操作。未知/部分结果由调用方核对。
func (w Wechat) Await(ctx context.Context, result Result) (Result, error) {
	for result.Status == "queued" || result.Status == "running" {
		if result.CallID == "" {
			return result, errors.New("排队结果缺少 call_id，无法查询原回执")
		}
		if err := w.Query(ctx, "wait", map[string]any{"call_id": result.CallID, "options": map[string]any{"wait_timeout_ms": 10000}}, &result); err != nil {
			return result, err
		}
	}
	return result, nil
}
