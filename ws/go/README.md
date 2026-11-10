# Go 插件 SDK

轻量的 LW Tools WebSocket 协议 v2 客户端，package 名为 `lwplugin`。Go 1.23+，直接依赖只有 `gorilla/websocket`；数据库和页面均不属于 SDK。编译后的插件运行时不需要安装 Go 或 Python。

[仓库首页](../../README.md) · [微信群管家](group-manager/README.md) · [完整最小示例](examples/echo/main.go)

## 目录与连接

`endpoint.go` 负责端点发现，`client.go` 负责连接/订阅/并发 RPC，`types.go` 定义 DTO，`wechat.go` 封装微信调用和回执。`group-manager/` 是独立 Go module，SQLite 不进入 SDK 模块。

在 Desktop 的“插件 → WS 插件”启动服务，并允许插件 ID 接入：

```powershell
$env:LW_PLUGIN_ENDPOINT_FILE = 'C:\path\to\host-data\config\plugin-endpoint.json'
go run ./examples/echo
```

Linux 使用 `export LW_PLUGIN_ENDPOINT_FILE=/path/to/plugin-endpoint.json`。远程插件可使用 `LW_PLUGIN_URL`、`LW_PLUGIN_TOKEN`；支持 ws/wss、IPv6，token 放在握手 Authorization Header，不写入 URL。优先级为显式 Config > 环境变量 > 发现文件，每次重连重新读取发现文件。插件 token 与微信访问凭据不同，SDK 不接管微信登录。

无需固定路径的便携用法：把宿主生成的 `plugin-endpoint.json` 放在编译后的插件可执行文件旁边。自动发现先检查可执行文件同目录及其 `config/`，再检查当前工作目录和各级父目录的同名文件与 `config/` 文件。即使通过快捷方式从不同工作目录启动，也能找到程序旁边的文件；复制文件后宿主重置 token 时需重新复制。

## 编写插件

```go
client, err := lwplugin.New(lwplugin.Config{
    ID: "example.my-plugin", Title: "我的插件", Version: "0.1.0",
    Events: []string{lwplugin.MessageReceived},
    OnEvent: func(ctx context.Context, c *lwplugin.Client, e lwplugin.Event) error {
        if e.Message == nil || e.Message.From.ID == e.Account.ID || e.Message.Text != "!ping" {
            return nil
        }
        result, err := c.Wechat(e.Account.ID).SendText(ctx,
            e.Message.Conversation(), "pong",
            lwplugin.Options{Key: "reply:" + e.ID, SourceEventID: e.ID})
        if err != nil { return err }
        log.Printf("call_id=%s status=%s", result.CallID, result.Status)
        return nil
    },
})
if err != nil { return err }
return client.Run(ctx)
```

完整 imports 和退出处理见 [echo](examples/echo/main.go)。ID 是权限、账本和重放游标的归属，发布后保持稳定。`Run` 持续重连直到 context 取消；插件是独立进程，宿主“允许接入”不代替启动进程。

## API 与结果

| 接口 | 用途 |
| --- | --- |
| `client.Request(ctx, method, params, &out)` | 通用宿主 RPC，例如 accounts.list、settings.get、只读消息查询 |
| `client.Wechat(account).Call/Submit` | 命名微信业务调用/异步提交 |
| `Wechat.Send/SendText` | 消息段/文本发送 |
| `Wechat.Methods` | 当前业务方法目录 |
| `Wechat.Query` | get/find/detail/list/queue/wait，强制当前账号范围 |
| `Wechat.Await` | 等待原 call_id，不重新发起业务 |
| `client.Log` | 写宿主诊断日志 |

一个业务动作生成一次 `Options.Key`，跨超时保存。空 Key 会自动生成，但明确的业务任务建议自行保存：

```go
key := lwplugin.NewKey()
wx := client.Wechat("wxid_bot")
result, err := wx.Call(ctx, "group.get_chat_room_member_detail",
    map[string]string{"chatRoomId": "123456@chatroom"}, lwplugin.Options{Key: key})
// 超时或断线时不重发：换一个有效 context，通过原 key 查询。
var receipt *lwplugin.Result
queryErr := wx.Query(queryCtx, "find", map[string]any{"idempotency_key": key}, &receipt)
_ = result; _ = err; _ = queryErr
```

RPC 成功与业务成功不同：`err == nil` 后仍检查 Result.Status，原始 PB 逐项状态按具体业务核对。Data/Raw/Event.Raw 使用 json.RawMessage；动态 map 使用 `lwplugin.Decode`，避免 uint64 ID 变成 float64。RPCError 保留 Key/AccountID，超时支持 errors.Is(err, context.DeadlineExceeded)。重连不会自动重发在途 API。

## 事件与回调

提供 MessageReceived、MessageSent、CallUpdated、AccountOnline、AccountOffline、GroupRefreshHint、SyncHint 常量。后两者对应 `wechat.group.refresh_hint` 和 `wechat.sync.hint`，Data 分别解码为 GroupHint / SyncMetadata，需要新版宿主。

Hint 是需要核对的实时线索，不是已确认入群/退群，也不保证断线持久化重放。OnConnect 在每次重连完成后执行，可用于重新核对本地基线。selector 是有符号位掩码，不等于群 ID 或退出者。

接收循环独立于回调，因此回调中可以 RPC。回调按事件顺序执行；耗时核对应排入自己的有界队列，并尊重传入 context。带 Cursor 的 durable 事件在回调成功后 ACK，失败则重连等待宿主重放；重复事件不会重复执行已成功的回调。队列溢出触发重连，插件需恢复自己的业务状态。

## 测试与发布

```powershell
go test -race ./...
go vet ./...
```

测试使用临时 WS 服务，覆盖认证、回调内 RPC、大整数、乱序响应、重复 ACK、超时不重发和账号范围。群管家是独立模块，需要进入其目录单独测试。

当前源码可在仓库内直接构建。将来发布 SDK module 时使用 `ws/go/v0.1.0` 这种子目录 tag，使用者固定到实际发布版本；不要将尚未发布的 tag 宣称为可安装版本。提交 go.mod/go.sum/LICENSE，排除端点、凭据、日志、数据库和编译产物。使用 [MIT License](LICENSE)，依赖遵循各自许可证。
