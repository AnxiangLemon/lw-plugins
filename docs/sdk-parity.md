# Go / Python SDK 协议对照与源码维护

[Go SDK](../ws/go/README.md) · [Python SDK](../ws/python/README.md) · [群事件](group-events.md) · [微信业务](wechat-business.md)

本文以当前 Rust 宿主的 WS v2 协议为准。Python 当前源码版本为 **2.4.0**；Go module 仍位于 `ws/go`，尚未发布新的版本 tag。两套 SDK 共用协议、事件和业务字段；函数命名遵循各自语言惯例，不要求调用代码逐字相同。

## 接口覆盖范围

| 能力 | Go | Python |
| --- | --- | --- |
| 持续运行、自动重连 | `Client.Run(ctx)` | `Plugin.run()` / `run_async()` |
| 单次会话 | `Client.RunOnce(ctx)`，业务写在 `OnConnect` | `async with plugin.connect()` |
| 关闭 | 取消运行用的 `context` | `await plugin.close()` / 退出连接上下文 |
| 连接/断线回调 | `Config.OnConnect` / `OnDisconnect` | `on_connect` / `on_disconnect` |
| 宿主信息/连接状态 | `Host()` / `Connected()` | `host` / `connected` |
| 任意公开 RPC | `Request(ctx, method, params, &out)` | `await api(method, params)` |
| 微信命名方法 | `Wechat(account).Call` / `Submit` | `wechat(account).call` / `submit` |
| 微信消息段/文本/本地图片 | `Wechat.Send` / `SendText` / `SendImage` | `wx.send` / `send_text` / `send_image` |
| 主动多平台发送 | `Client.Send(..., SendOptions)` | `Plugin.send(..., platform=...)` |
| 按来源回复 | `Client.Reply(ctx, event, segments, Options)` | `ctx.reply(event, text/segments)` |
| 微信回执查询 | `Wechat.Query(ctx, action, filters, &out)` | `wx.calls.get/find/detail/list/queue/wait` |
| 等待原微信调用 | `Wechat.Await(ctx, receipt)` | `wx.wait_result(receipt, timeout=...)` |
| 通用平台账本 | `Client.Calls(account, platform).Query(...)` | `plugin.calls(account, platform=...).get/find/detail/list/queue` |
| 显式微信消息过滤 | `Config.OnFilter` | `@plugin.filter` |
| 宿主诊断日志 | `Log(ctx, level, message)` | `await log(message, level)` |
| 随机幂等键 | `NewKey()` | `new_key()` |

微信方法目录由 `wechat.methods` 动态返回。当前仓库面板内置 164 个公开命名方法，两端均通过 `Call/call` 和 `Submit/submit` 原样调用，包括群管理、媒体和支付相关方法。SDK 不复制 164 个薄包装函数，因此宿主增加方法时通常无须更新 SDK。业务参数、权限、逐项返回状态仍以服务端文档和实际宿主支持为准。支付相关路径只做模拟回归，没有实际支付验证。

通用 RPC 覆盖以下当前公开入口，无需额外组件库：

| 命名空间 | 方法 | 用途 |
| --- | --- | --- |
| `platform` | `list` | 宿主已支持的平台 |
| `accounts` | `list` | 账号展示信息和在线状态 |
| `settings` | `get`、`set`、`all` | 当前插件自己的设置 |
| `db` | `query_messages`、`list_peers`、`chat_stats` | 宿主保存的消息、会话和统计 |
| `wechat` | `methods`、`call`、`submit`、`send` | 微信业务目录和调用 |
| `wechat.calls` | `get`、`find`、`detail`、`list`、`queue`、`wait` | 微信调用账本 |
| `calls` | `get`、`find`、`detail`、`list`、`queue` | Telegram 等平台调用账本 |
| `wechat.events` | `cursor`、`poll`、`ack` | SDK/宿主的持久化事件协议 |
| `wechat.filters` | `register`、`decide` | SDK/宿主的过滤协议 |

**通用账本没有 `calls.wait`。** 不把微信的等待 API 套给 Telegram。事件 ACK、poll 和过滤注册属于底层机制；普通插件使用 SDK 回调即可，不要同时启动第二套拉取/确认循环。

## 全部十种事件

| 协议名称 | Go 常量 | Python 常量 | 投递方式 |
| --- | --- | --- | --- |
| `message.received` | `GenericMessageReceived` | `MESSAGE_RECEIVED` | 通用平台，实时 |
| `message.sent` | `GenericMessageSent` | `MESSAGE_SENT` | 通用平台，实时 |
| `wechat.message.received` | `WechatMessageReceived` | `WECHAT_MESSAGE_RECEIVED` | 微信持久化 |
| `wechat.message.sent` | `WechatMessageSent` | `WECHAT_MESSAGE_SENT` | 微信持久化 |
| `wechat.call.updated` | `CallUpdated` | `CALL_UPDATED` | 微信持久化 |
| `wechat.message.filter` | `MessageFilter` | `MESSAGE_FILTER` | 显式过滤链 |
| `wechat.group.refresh_hint` | `GroupRefreshHint` | `GROUP_REFRESH_HINT` | 群资料实时线索 |
| `wechat.sync.hint` | `SyncHint` | `SYNC_HINT` | 账号同步实时线索 |
| `account.online` | `AccountOnline` | `ACCOUNT_ONLINE` | 账号状态实时通知 |
| `account.offline` | `AccountOffline` | `ACCOUNT_OFFLINE` | 账号状态实时通知 |

Go 的旧 `MessageReceived` / `MessageSent` 为兼容已有群管家，继续指向微信专用事件。新代码推荐用表中的明确名称，不要按常量名字猜平台。`AllEvents()` / `ALL_EVENTS` 是目录，不建议一键全订阅：同一微信消息可能同时产生通用和微信专用事件。

事件均保留原始帧；Go 为 `Event.Raw`，Python 为 `event.raw`。`trace_id`、`call_id`、`result`、`delivery`、`status` 等字段可直接读取。群提示的可选版本字段用 Go 指针 / Python `None` 表示缺失，绝不默认成 0。64 位 ID 不转浮点：Go 动态对象用 `Decode`，Python 原生整数保留精度。

## 订阅、连接与恢复

- 两端都在 `hello.ok.events` 中确认能力，然后等待 `subscribe.ok`；不支持的事件返回 `unsupported_event`。
- Go 显式配置 `Events`；Python 未指定 `events` 时按注册的处理器推导。两端没有事件时均发送空数组 `[]`，不会保留宿主默认的全部消息订阅。
- 订阅过滤字段都是 `accounts`、`platforms`、`chat_types`、`text_prefix`。未提供表示不限，空数组表示不匹配任何对象。Go 使用 `nil` 与空切片区分，Python 使用省略键与 `[]` 区分。
- 连接回调在订阅确认后执行，可以查询 RPC；初始化完成前收到的事件先排队。初始化失败则关闭连接。
- 两端默认 RPC 超时 70 秒，握手/订阅超时 10 秒；分别可配置 `RequestTimeout` / `request_timeout`、`HandshakeTimeout` / `handshake_timeout`。Go 的请求 `context` 可以进一步缩短期限。Python `connect(timeout=...)` 还约束整个连接初始化等待时间。
- 长连接按 1、2、4……30 秒退避；连接稳定超过一分钟后恢复 1 秒。关闭会中断等待，重连重新读取端点文件。
- 关闭时取消当前会话工作，旧回调完成清理后才进入下一次初始化。回调须响应 `ctx.Done()` 或 `asyncio.CancelledError`。
- 带 `cursor` 的事件仅在回调成功后确认；后一个游标不能跨过失败事件。成功回调保存在有限内存缓存中，ACK 丢失后的重投只补确认。缓存按“事件名 + 事件 ID”区分；进程重启或缓存淘汰后的重复投递仍需业务幂等处理。
- 两个群 Hint 没有持久化游标，不 ACK、不保证断线补发；重连后由插件重新核对完整成员名单。

保留的语言差异：Go 所有事件回调串行；Python 普通实时消息保留有界并发，持久化事件与两类群提示各自有序。不要依赖群提示与聊天消息处理器之间的全局顺序。Go 以 `context` 传入事件作用域，Python 使用 `ContextVar` 隔离异步回调。

## 发送、过滤与回执

主动发送默认微信，回复沿用事件平台。Go 非微信发送设置 `SendOptions.Platform`；Python 使用 `platform=`。回调中微信调用自动携带 `source_event_id`，显式指定时以调用者参数为准。

同一个业务动作保存同一个幂等键。回调内回复未提供键时生成 `reply:<event_id>:<回复序号>`；Go 必须使用回调收到的 `ctx`，在回调外回复需显式给 `Options.Key`。Python 序号保存在该 `Event` 实例上。长期后台任务也应显式保存键，避免重建事件对象后碰撞。

Go `Options.Wait` 使用 `*bool`，`nil` 表示宿主默认策略，`false` 只提交；Python 为 `wait=False`。`WaitTimeoutMS` / `wait_timeout_ms` 采用毫秒，客户端等待超时不会取消服务端任务。

通用 action 回执是顶层 `action.ack`，不是 `api.result.data`。两端均保留 `call_id`、`status`、`outbox_id`、`delivered`；RPC 错误包含错误码，能取得时还保留原调用 ID、状态、账号与幂等键。`error.ref` 会立即关联到原请求，不再等成一个模糊的超时。

`queued` / `running` 表示受理，`succeeded` 才表示宿主成功；`unknown` / `partial` 需要核对。Go `Result.Original` 和 Python 结果字典保留完整回执，区别于回执本身的 `raw`（服务端原始响应）。连接恢复、等待回执都不会重发业务。

过滤须显式开启，普通消息处理器的返回值不拦截其他插件：

```go
// 配置 OnFilter 后 SDK 自动订阅 MessageFilter，并按“决定 -> ACK”执行。
OnFilter: func(ctx context.Context, c *lwplugin.Client, e lwplugin.Event) (bool, error) {
    return e.Message != nil && strings.HasPrefix(e.Message.Text, "!private"), nil
},
```

```python
@plugin.filter
async def filter_message(event):
    if event.text.startswith("!private"):
        return plugin.stop("由本插件处理")
```

过滤回调应快速返回；拦截能力受宿主等待预算约束。回调失败不推进游标，重连后重新提交决定。不要用 Python `on(MESSAGE_FILTER)` 代替 `filter`。

## 媒体消息段

| 含义 | Go | Python |
| --- | --- | --- |
| 文本 | `Text` | `text_segment` |
| 提及 | `At` | `at_segment` |
| 图片 Base64 | `Image` | `image_segment` |
| 本地图片 | `ImageFile` | `image_file_segment` |
| 语音 | `Voice` | `voice_segment` |
| 视频与封面 | `Video` | `video_segment` |
| 文件内容与文件名 | `File` | `file_segment` |
| 高级微信方法 | `WechatSegment` | `wechat_segment` |

构造函数只组织消息段，不发送网络请求。文件路径指插件机器，SDK 读取后传 Base64。图片不转码，语音格式和时长单位按服务文档；视频 `playLength` 不再发送。某平台不支持的消息段由宿主拒绝；如需 `cdn.get_dns`，业务插件显式调用，不在构造函数里隐藏网络操作。

## 连接配置与兼容变更

两端均按显式参数、环境变量、发现文件补齐 URL/token。支持 `LW_PLUGIN_URL`、`LW_PLUGIN_TOKEN`、`LW_PLUGIN_ENDPOINT_FILE`。发现顺序为程序/入口脚本旁边、其 `config/`，然后工作目录及父目录。显式指定文件找不到时不去其他目录碰运气。

`ws` / `wss`、IPv6、路径及非 token 查询参数均保留。旧 URL 的 `?token=` 仍可导入，显式 token 优先；实际握手统一使用 `Authorization: Bearer ...`，连接 URL 不携带 token。Python 自动适配 websockets 12/13 的 `extra_headers` 与 14/15 的 `additional_headers`，不增加运行依赖。

Python 2.4.0 中完整 URL 未指定端口时按 WS 标准使用 80/443，代替旧版默认 26810；本地宿主请用发现文件或写明端口。`Endpoint(host, port, token)` 旧构造方式仍可用。无效协议、账号密码 URL、片段及越界端口会被拒绝。

## 测试与源码发布

共享协议样例位于 [ws/go/testdata/sdk-contract.json](../ws/go/testdata/sdk-contract.json)，两端测试均读取它。样例包含事件目录、消息段、未来字段、大整数与 RPC 名称；不是运行时依赖，不包含账号凭据。

```powershell
# 仓库根目录
python -m unittest discover -s ws/python/tests -v
python -m unittest discover -s tests -p 'test_*.py' -v

# ws/go 目录
 go test -race ./...
 go vet ./...

# ws/go/group-manager 目录：确认旧插件不因 SDK 更新回归
 go test ./...
```

发布源码时保留 SDK、许可证、依赖锁定文件、README、共享样例及单元测试。排除真实 endpoint、token、数据库、日志、虚拟环境、缓存和临时构建产物。Go 按子目录 module tag（例如未来发布时 `ws/go/v0.2.0`）管理；Python 版本在 `pyproject.toml` 和 `lwplugin.__version__` 同步。仓库未执行 PyPI 上传或创建 Git tag。

后续新增协议时同时更新共享样例、两个 SDK 和本文。业务方法通过动态目录增加时不重复生成接口包装；发生消息段或事件结构变更时补充跨语言回归。模拟测试证明协议与实现一致，不等于已完成每种真实微信业务的外部验证。

## 本次验证记录

- Windows / Python 3.14：45 项 SDK 测试及 13 项面板/目录测试通过。
- websockets 12.0、13.1、14.2、15.0.1：在临时依赖目录验证连接、认证、事件顺序、错误和重连兼容；没有替换用户全局依赖。
- Go：SDK `go test -race ./...`、`go vet ./...` 和现有群管家测试通过。
- 真实 Rust 宿主 + 模拟业务服务：Python 164 个接口入口、示例和重连回执通过；Go 群提示、完整名单差集和 SQLite 入退群日志链路通过。
- Python wheel 构建、干净目录安装及导出检查通过；包内没有测试图片、真实端点或编译产物。测试 wheel 仅在临时目录生成，未上传包仓库。
- Python 3.9 完成语法兼容检查，未在本机安装并执行 3.9 运行时。
