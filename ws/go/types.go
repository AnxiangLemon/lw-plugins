package lwplugin

import (
	"bytes"
	"encoding/json"
	"fmt"
)

const (
	// 旧常量保持微信专用语义；新代码建议明确写 WechatMessageReceived。
	MessageReceived        = "wechat.message.received"
	MessageSent            = "wechat.message.sent"
	WechatMessageReceived  = MessageReceived
	WechatMessageSent      = MessageSent
	GenericMessageReceived = "message.received"
	GenericMessageSent     = "message.sent"
	MessageFilter          = "wechat.message.filter"
	CallUpdated            = "wechat.call.updated"
	AccountOnline          = "account.online"
	AccountOffline         = "account.offline"
	GroupRefreshHint       = "wechat.group.refresh_hint"
	SyncHint               = "wechat.sync.hint"
)

// AllEvents 返回宿主 v2 的完整事件目录副本，不代表应订阅全部事件。
// 微信专用与通用消息可能描述同一条消息，请按需要选择一个通道。
func AllEvents() []string {
	return []string{GroupRefreshHint, SyncHint, WechatMessageReceived, WechatMessageSent,
		CallUpdated, MessageFilter, GenericMessageReceived, GenericMessageSent, AccountOnline, AccountOffline}
}

// Decode 对动态 JSON 使用 json.Number，保留 64 位 ID 的十进制原值。
func Decode(data json.RawMessage, out any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	return d.Decode(out)
}

// Account 是宿主账号标识，ID 必须与 Platform 一起使用。
type Account struct {
	ID       string `json:"id"`
	Platform string `json:"platform"`
}

// Peer 标识消息发送者或接收者，不代表插件登录凭据。
type Peer struct {
	ID       string `json:"id"`
	Platform string `json:"platform"`
}

// Segment 保持宿主 canonical 消息段结构，可用构造函数或直接传扩展字段。
type Segment struct {
	Type string `json:"type"`
	Data any    `json:"data"`
}

// Text 构造纯文本消息段，不发起网络操作。
func Text(text string) Segment { return Segment{Type: "text", Data: map[string]string{"text": text}} }

// Message 与宿主 v2 DTO 对齐。Raw 保留平台原文，业务不要只依赖显示文本。
type Message struct {
	ID        string          `json:"msg_id"`
	Peer      string          `json:"peer"`
	PeerID    string          `json:"peer_id"`
	Platform  string          `json:"platform"`
	ChatType  string          `json:"chat_type"`
	IsGroup   bool            `json:"is_group"`
	From      Peer            `json:"from"`
	To        Peer            `json:"to"`
	Text      string          `json:"text"`
	Timestamp int64           `json:"timestamp"`
	Direction string          `json:"direction"`
	Segments  []Segment       `json:"segments"`
	Raw       json.RawMessage `json:"raw"`
}

// Conversation 兼容 peer_id 和 peer 两种会话字段。
func (m Message) Conversation() string {
	if m.PeerID != "" {
		return m.PeerID
	}
	return m.Peer
}

// Event.Cursor 非空表示持久化事件。只有回调成功，SDK 才确认该游标。
// 刷新提示是实时事件，没有 Cursor；重连后的业务核对由 OnConnect 完成。
type Event struct {
	Name       string          `json:"event"`
	ID         string          `json:"event_id"`
	TraceID    string          `json:"trace_id"`
	CallID     string          `json:"call_id"`
	Cursor     *int64          `json:"cursor"`
	Account    Account         `json:"account"`
	ReceivedAt int64           `json:"received_at"`
	Message    *Message        `json:"message"`
	Data       json.RawMessage `json:"data"`
	Result     json.RawMessage `json:"result"`
	Delivery   json.RawMessage `json:"delivery"`
	Status     json.RawMessage `json:"status"`
	Raw        json.RawMessage `json:"-"`
}

// UnmarshalJSON 保留完整帧，未来新增字段可从 Raw 读取。
func (e *Event) UnmarshalJSON(data []byte) error {
	type plain Event
	var value plain
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	*e = Event(value)
	e.Raw = append(json.RawMessage(nil), data...)
	return nil
}

// GroupHint 仅为核对线索；空指针表示未提供，不能当作版本 0 或全员退群。
type GroupHint struct {
	RoomID              string  `json:"room_id"`
	Reason              string  `json:"reason"`
	ChatroomVersion     *uint32 `json:"chatroom_version"`
	ChatroomInfoVersion *uint32 `json:"chatroom_info_version"`
	EraseMemberData     *uint32 `json:"erase_member_data"`
	InfoMask            *uint32 `json:"info_mask"`
}

// SyncMetadata 是账号级弱提示；有符号 Selector 不等于退出者或群 ID。
type SyncMetadata struct {
	Source       string  `json:"source"`
	Selector     *int32  `json:"selector"`
	TriggeredAt  string  `json:"triggered_at"`
	ContinueFlag *uint32 `json:"continue_flag"`
}

// Filter 仅限制事件订阅。它不是拦截业务消息的 OnFilter。
type Filter struct {
	Accounts   []string `json:"accounts,omitempty"`
	Platforms  []string `json:"platforms,omitempty"`
	ChatTypes  []string `json:"chat_types,omitempty"`
	TextPrefix []string `json:"text_prefix,omitempty"`
}

// MarshalJSON 区分 nil（未限制）和空切片（不匹配任何对象），不能把空账号范围扩成全账号。
func (f Filter) MarshalJSON() ([]byte, error) {
	value := map[string][]string{}
	for key, items := range map[string][]string{"accounts": f.Accounts, "platforms": f.Platforms, "chat_types": f.ChatTypes, "text_prefix": f.TextPrefix} {
		if items != nil {
			value[key] = items
		}
	}
	return json.Marshal(value)
}
func cloneStrings(value []string) []string {
	if value == nil {
		return nil
	}
	out := make([]string, len(value))
	copy(out, value)
	return out
}

// HostInfo 来自 hello.ok；新接口仍应以当前宿主声明的能力为准。
type HostInfo struct {
	Version      string   `json:"host_version"`
	Events       []string `json:"events"`
	Capabilities []string `json:"capabilities"`
}

// RPCError 表示 RPC 未成功返回。超时不等于业务没有执行；查询时沿用 Key。
type RPCError struct {
	Code      string
	Message   string
	CallID    string
	Status    string
	Key       string
	AccountID string
	Cause     error
}

func (e *RPCError) Unwrap() error { return e.Cause }

func (e *RPCError) Error() string { return fmt.Sprintf("%s: %s", e.Code, e.Message) }

// BusinessError 是回执内的业务错误，与 RPC 传输错误分开。
type BusinessError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Stage   string `json:"stage"`
}

// Result 是微信业务或通用 action 的回执；无错误返回不等于 Status 已成功。
type Result struct {
	CallID         string          `json:"call_id"`
	AccountID      string          `json:"account_id"`
	Method         string          `json:"method"`
	Status         string          `json:"status"`
	WaitTimedOut   bool            `json:"wait_timed_out"`
	Data           json.RawMessage `json:"data"`
	Raw            json.RawMessage `json:"raw"`
	Error          *BusinessError  `json:"error"`
	Messages       json.RawMessage `json:"messages"`
	Revision       int64           `json:"revision"`
	OutboxID       *int64          `json:"outbox_id"`
	Delivered      *bool           `json:"delivered"`
	DetailsExpired bool            `json:"details_expired"`
	// Original 保留整个回执，与服务端的 raw（原始业务响应）不是同一字段。
	Original json.RawMessage `json:"-"`
}

// UnmarshalJSON 同时支持微信业务结果和通用 action.ack，不丢弃未知字段。
func (r *Result) UnmarshalJSON(data []byte) error {
	type plain Result
	var value plain
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	*r = Result(value)
	r.Original = append(json.RawMessage(nil), data...)
	return nil
}

// Succeeded 只判断宿主调用状态。PB 逐项状态仍应按具体业务协议检查。
func (r Result) Succeeded() bool { return r.Status == "succeeded" }
