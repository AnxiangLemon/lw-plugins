package lwplugin

import (
	"bytes"
	"encoding/json"
	"fmt"
)

const (
	MessageReceived  = "wechat.message.received"
	MessageSent      = "wechat.message.sent"
	CallUpdated      = "wechat.call.updated"
	AccountOnline    = "account.online"
	AccountOffline   = "account.offline"
	GroupRefreshHint = "wechat.group.refresh_hint"
	SyncHint         = "wechat.sync.hint"
)

// Decode 对动态 JSON 使用 json.Number，保留 64 位 ID 的十进制原值。
func Decode(data json.RawMessage, out any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	return d.Decode(out)
}

type Account struct {
	ID       string `json:"id"`
	Platform string `json:"platform"`
}
type Peer struct {
	ID       string `json:"id"`
	Platform string `json:"platform"`
}
type Segment struct {
	Type string `json:"type"`
	Data any    `json:"data"`
}

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
	Cursor     *int64          `json:"cursor"`
	Account    Account         `json:"account"`
	ReceivedAt int64           `json:"received_at"`
	Message    *Message        `json:"message"`
	Data       json.RawMessage `json:"data"`
	Result     json.RawMessage `json:"result"`
	Raw        json.RawMessage `json:"-"`
}
type GroupHint struct {
	RoomID              string  `json:"room_id"`
	Reason              string  `json:"reason"`
	ChatroomVersion     *uint32 `json:"chatroom_version"`
	ChatroomInfoVersion *uint32 `json:"chatroom_info_version"`
	EraseMemberData     *uint32 `json:"erase_member_data"`
	InfoMask            *uint32 `json:"info_mask"`
}
type SyncMetadata struct {
	Source       string  `json:"source"`
	Selector     *int32  `json:"selector"`
	TriggeredAt  string  `json:"triggered_at"`
	ContinueFlag *uint32 `json:"continue_flag"`
}
type Filter struct {
	Accounts   []string `json:"accounts,omitempty"`
	Platforms  []string `json:"platforms,omitempty"`
	ChatTypes  []string `json:"chat_types,omitempty"`
	TextPrefix []string `json:"text_prefix,omitempty"`
}
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
	Key       string
	AccountID string
	Cause     error
}

func (e *RPCError) Unwrap() error { return e.Cause }

func (e *RPCError) Error() string { return fmt.Sprintf("%s: %s", e.Code, e.Message) }

type BusinessError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Stage   string `json:"stage"`
}
type Result struct {
	CallID       string          `json:"call_id"`
	AccountID    string          `json:"account_id"`
	Method       string          `json:"method"`
	Status       string          `json:"status"`
	WaitTimedOut bool            `json:"wait_timed_out"`
	Data         json.RawMessage `json:"data"`
	Raw          json.RawMessage `json:"raw"`
	Error        *BusinessError  `json:"error"`
	Messages     json.RawMessage `json:"messages"`
}

// Succeeded 只判断宿主调用状态。PB 逐项状态仍应按具体业务协议检查。
func (r Result) Succeeded() bool { return r.Status == "succeeded" }
