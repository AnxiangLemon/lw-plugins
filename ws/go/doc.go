// Package lwplugin 提供 LW Tools WS 协议 v2 的 Go 客户端。
//
// 宿主负责账号、微信凭据和持久化调用账本；插件只选择账号并传业务参数。
// SDK 不连接 lwsapi，不保存微信 token，也不自动重发超时的业务操作。
//
// Run 负责重连和事件分发。回调可以调用 Request/Wechat，因为读取 RPC 返回的
// 接收循环与回调相互独立。业务返回值使用 json.RawMessage，避免 uint64 ID
// 被解码成 float64 后丢失精度；解码到 map 时可使用本包的 Decode。
package lwplugin
