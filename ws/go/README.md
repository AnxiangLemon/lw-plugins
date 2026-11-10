# Go 插件 SDK

轻量的 LW Tools WebSocket 协议 v2 客户端，package 名为 `lwplugin`。Go 1.23+，直接依赖只有 `gorilla/websocket`；数据库和页面均不属于 SDK。编译后的插件运行时不需要安装 Go 或 Python。

[仓库首页](../../README.md) · [Go/Python 协议对照](../../docs/sdk-parity.md) · [微信群管家](group-manager/README.md) · [完整最小示例](examples/echo/main.go)

## 本次对齐更新

已补齐通用平台发送/回复、调用账本查询、显式微信过滤、媒体构造函数及单次会话。公开事件与 Python 对齐为 10 种；错误、回执和事件保留可追踪字段。旧 `MessageReceived` / `MessageSent` 的微信语义不变，现有群管家可以继续编译。

没有新增第三方依赖。源码尚未创建发布 tag；新 API、兼容变化与发布方式见[完整对照](../../docs/sdk-parity.md)。

## 目录与连接

`endpoint.go` 负责端点发现，`client.go` 负责连接/订阅/并发 RPC，`types.go` 定义 DTO，`wechat.go` 封装微信调用和回执，`actions.go` 处理多平台发送与账本，`media.go` 构造媒体消息段。`group-manager/` 是独立 Go module，SQLite 不进入 SDK 模块。

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

## 通用平台消息

订阅 `GenericMessageReceived` 可接收宿主已支持平台的通用消息。回调中的回复沿用来源平台：

```go
// OnEvent 的 ctx 带当前事件作用域；不要换成 context.Background()。
OnEvent: func(ctx context.Context, c *lwplugin.Client, e lwplugin.Event) error {
    if e.Message == nil || e.Message.Text != "!ping" { return nil }
    receipt, err := c.Reply(ctx, e, []lwplugin.Segment{lwplugin.Text("pong")}, lwplugin.Options{})
    if err != nil { return err }
    log.Printf("call_id=%s status=%s", receipt.CallID, receipt.Status)
    return nil
},
```

主动发送默认为微信；其他平台须设置 `SendOptions{Platform: "telegram", Options: lwplugin.Options{Key: "business-key"}}`。通用平台回执通过 `c.Calls(account, "telegram").Query(ctx, "get", map[string]any{"call_id": id}, &out)` 查询。回调中省略平台可从传入 `ctx` 取得来源平台，回调外默认 Telegram；后台任务建议明确写平台。

## API 与结果

| 接口 | 用途 |
| --- | --- |
| `client.Request(ctx, method, params, &out)` | 通用宿主 RPC，例如 accounts.list、settings.get、只读消息查询 |
| `client.Wechat(account).Call/Submit` | 命名微信业务调用/异步提交 |
| `Wechat.Send/SendText/SendImage` | 微信消息段/文本/本地图片发送 |
| `client.Send/Reply` | 多平台主动发送/按事件来源回复 |
| `client.Calls(account, platform).Query` | 通用调用账本 get/find/detail/list/queue |
| `client.RunOnce(ctx)` | 单次会话，不自动重连 |
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

完整事件目录由 `AllEvents()` 返回，包括通用 `GenericMessageReceived/GenericMessageSent`、微信专用 `WechatMessageReceived/WechatMessageSent`、`CallUpdated`、`MessageFilter`、账号状态和两种 Hint。旧 `MessageReceived/MessageSent` 是微信专用事件的兼容别名。`GroupRefreshHint/SyncHint` 对应 `wechat.group.refresh_hint` 和 `wechat.sync.hint`，Data 分别解码为 GroupHint / SyncMetadata，需要新版宿主。

Hint 是需要核对的实时线索，不是已确认入群/退群，也不保证断线持久化重放。OnConnect 在每次重连完成后执行，可用于重新核对本地基线。selector 是有符号位掩码，不等于群 ID 或退出者。

接收循环独立于回调，因此回调中可以 RPC。回调按事件顺序执行；耗时核对应排入自己的有界队列，并尊重传入 context。带 Cursor 的 durable 事件在回调成功后 ACK，失败则重连等待宿主重放；重复事件不会重复执行已成功的回调。队列溢出触发重连，插件需恢复自己的业务状态。成功回调在 ACK 前进入跨会话缓存，去重键包含事件名；不能依赖此有限缓存实现跨进程幂等。

Python SDK 的对应能力、两端事件顺序差异与监控业务边界见[统一群事件文档](../../docs/group-events.md#go--python-能力对照)。

## 过滤、连接参数与消息段

`Config.OnFilter` 接收 `(ctx, client, event)` 并返回 `(stop, error)`，返回 true 拦截后续插件。设置它会自动订阅 `MessageFilter`；SDK 先提交决定再 ACK。普通 `OnEvent` 返回 nil 仅表示本回调成功。

`Filter` 的 nil 切片表示不限，空切片表示不匹配任何对象；不要把 `[]string{}` 当作“全部账号”。`RequestTimeout` 默认 70 秒，`HandshakeTimeout` 默认 10 秒；请求还服从调用者 ctx。`RunOnce` 的业务写在 `OnConnect`，取消运行 ctx 后等待回调退出。回调须响应取消，避免阻塞下一次重连。

消息段提供 `Text`、`At`、`Image`、`ImageFile`、`Voice`、`Video`、`File`、`WechatSegment`，与 Python 结构一致。`ImageFile` 读取插件本地文件，`Wechat.SendImage` 负责提交；如业务需要 CDN DNS 准备，先显式调用 `cdn.get_dns`。视频时长不再作为 `playLength` 发送。

## 测试与发布

```powershell
go test -race ./...
go vet ./...
```

测试使用临时 WS 服务，覆盖认证、回调内 RPC、大整数、乱序响应、重复 ACK、超时不重发和账号范围。群管家是独立模块，需要进入其目录单独测试。

当前源码可在仓库内直接构建。将来发布 SDK module 时使用 `ws/go/v0.2.0` 这种子目录 tag，使用者固定到实际发布版本；不要将尚未发布的 tag 宣称为可安装版本。提交 go.mod/go.sum/LICENSE，保留 `testdata/sdk-contract.json` 和测试；排除端点、凭据、日志、数据库和编译产物。使用 [MIT License](LICENSE)，依赖遵循各自许可证。
