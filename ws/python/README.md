# lwplugin Python SDK

[返回仓库首页](../../README.md) · [Go/Python 协议对照](../../docs/sdk-parity.md) · [微信业务调用](../../docs/wechat-business.md) · [接口面板](../../docs/wechat-panel.md) · [开发与测试](../../docs/development.md)

`lwplugin` 是 LW Tools 的异步 WS 插件客户端 SDK，当前版本 **2.4.0**，要求 Python 3.9+ 和宿主协议 v2。源码、安装配置、测试和可运行示例均在本仓库维护。开发插件只需安装本 SDK，并连接支持相应能力的宿主。

## 安装

在仓库根目录执行：

```powershell
# 基础 SDK：适用于 Echo、Assistant 和业务调用示例
python -m pip install -e ./ws/python

# 同时安装面板依赖
python -m pip install -e './ws/python[panel]'
```

`-e` 表示可编辑安装；部署时也可执行 `python -m pip install ./ws/python` 安装普通副本。安装配置见 [pyproject.toml](pyproject.toml)，SDK 代码位于 [lwplugin/](lwplugin/)。

### 快速验证

```powershell
python -c "import lwplugin; print(lwplugin.__version__)"
python ws/python/wechat_panel.py --demo
```

模拟面板打开 <http://127.0.0.1:8765>。此模式需要 `panel` 可选依赖，无需实际宿主或账号。

## 连接宿主

在宿主中开启 WS 服务，并允许所运行的插件 ID 接入。在仓库根目录设置实际连接文件：

```powershell
$env:LW_PLUGIN_ENDPOINT_FILE = 'C:\path\to\host-data\config\plugin-endpoint.json'
python ws/python/echo.py
```

SDK 优先使用显式参数或环境变量；自动发现先找入口脚本同目录的 `plugin-endpoint.json`、`config/plugin-endpoint.json`，再找当前工作目录及父目录，不搜索相邻宿主仓库。把端点文件放在脚本旁边后，从其他目录启动也能连接。显式指定的端点文件不存在时直接报错，不回退到另一个宿主。远程连接可设置 `LW_PLUGIN_URL` 和 `LW_PLUGIN_TOKEN`；程序也可给 `Plugin` 传入 `endpoint_file` 或 `url` / `token`。宿主接入开关不会自动启动 Python 进程。

## 主动调用与本地图片

`async with plugin.connect(timeout=15)` 等待握手、订阅确认和连接初始化完成后才进入上下文，退出时回收连接，不自动重连。长期事件插件使用 `run()` / `run_async()`。

`wx.send_image(to, path, **options)` 读取插件机器上的图片并以 Base64 提交，保留 `send` 的回执与幂等规则，不把本地路径传给宿主。需要时由业务插件先调用 `cdn.get_dns`，SDK 不隐式执行额外业务。`image_file_segment(path)` 可用于组合消息段，原 `image_segment(base64)` 用法不变。

```python
# 放在 async def 中；wx 来自 plugin.wechat(account_id)。
receipt = await wx.send_image("room@chatroom", "photo.png", idempotency_key="image-job-001")
receipt = await wx.wait_result(receipt, timeout=120)
print(receipt.status)
```

`wait_result` 只查询原调用，直到状态不再为 `queued` / `running`。`unknown` / `partial` / `failed` 原样返回；超时抛出 `asyncio.TimeoutError`，不会取消或重新发送原业务。

## 2.4.0 对齐更新

- 公开完整 10 种事件目录和 `MESSAGE_FILTER`；过滤仍通过 `@plugin.filter` 显式开启。
- 增加 `event.result`、`event.call_id`、`send_text`、`new_key`；保留全部原始字段。
- URL 保留 `wss`、路径、查询参数与 IPv6；实际认证改走 Header，兼容 websockets 12–15。
- `error.ref` 立即返回给原请求；传输失败保留幂等键。成功回调与 ACK 分开记录，去重包含事件名。
- 订阅确认和初始化有超时；无处理器时订阅 `[]`，关闭立即结束重连等待。群提示及持久化事件在接收帧时确定处理顺序。

没有新增依赖。完整端口兼容变化、SDK 对照与源码发布注意事项见[协议对照](../../docs/sdk-parity.md)。

## 群监控支持（与 Go SDK 对齐）

Python 自 2.3.0 起提供 `HostInfo`、`GroupHint`、`SyncMetadata` 和连接生命周期回调。两个 Hint 已统一为按接收顺序处理；初始化期间暂存事件，回调可以调用 RPC，失败则断开以便重新核对。其他实时消息继续使用原有并发方式。

| Python API | 用途 |
| --- | --- |
| `GROUP_REFRESH_HINT` / `SYNC_HINT` | 群资料提示 / 账号同步提示 |
| `event.group_hint` / `event.sync_metadata` | 类型化提示数据，保留缺失字段与 0 的区别 |
| `event.data` / `event.raw` | 完整原始数据，保留未来扩展字段 |
| `plugin.host` / `plugin.connected` | 宿主能力快照 / 已确认订阅的连接状态 |
| `@plugin.on_connect()` | 每次订阅成功后、事件处理前执行；可查询成员名单 |
| `@plugin.on_disconnect()` | 会话结束通知；主动关闭时错误为 `None` |
| `wx.wait_result(receipt, timeout=...)` | 对齐 Go `Await`，只等原调用 |

连接回调支持 `(host, ctx)` 或 `(host)`，断线回调支持 `(error, ctx)` 或 `(error)`；也可直接传函数，例如 `plugin.on_connect(handler)`。`connected` 在初始化回调中为 True，表示此时可以进行 RPC，不表示名单已核对完毕。回调中的异步工作须正常响应取消，SDK 会在断开时取消初始化和未完成的事件任务。

SDK 会检查宿主声明的事件能力。不支持群提示的旧宿主会明确返回 `unsupported_event`，不会静默漏掉群监控。无须改变协议版本或后端 API。

**Hint 只意味着需要核对，不是已确认的入群/退群通知。** 首次完整名单保存、后续名单对比、邀请人统计、入退群日志和可选消息提示仍由群管家业务插件实现。重连时应重新核对已关注群；Hint 没有持久化游标，也没有断线补发保证。字段说明、Go/Python 对照和接入示例见[统一群事件文档](../../docs/group-events.md)。

## 最小插件

保存为 Python 文件并运行。通用 `message.received` 覆盖宿主支持的平台，`ctx.reply` 根据来源事件选择回复平台：

```python
from lwplugin import Plugin

plugin = Plugin("example.ping", title="Ping 示例", version="1.0.0")


@plugin.on_message()
async def receive(event, ctx):
    if event.message.get("direction") == "outbound":
        return
    if event.text.strip() == "/ping":
        result = await ctx.reply(event, "pong")
        print(result)


if __name__ == "__main__":
    plugin.run()
```

## 事件与多平台行为

| API | 行为 |
| --- | --- |
| `@plugin.on_message()` | 订阅通用 `message.received` |
| `@plugin.on("wechat.message.received")` | 显式订阅微信持久化业务事件 |
| `ctx.reply(event, ...)` | 按来源事件平台与目标回复 |
| `ctx.send(...)` / `plugin.send(...)` | 默认 `platform="wechat"`；主动发送建议显式传平台 |
| `plugin.host_capabilities` | `hello.ok` 声明的能力集合，旧宿主或未声明时为空 |

主动发送不会跟随当前事件平台自动切换，多平台插件应写明：

```python
await ctx.reply(event, "回复当前会话")
await ctx.send(
    event.account_id,
    event.peer,
    "主动发送",
    platform=event.platform,
    idempotency_key=f"notice:{event.event_id}",
)
```

微信持久化事件首次订阅从当前位置开始，之后重连补发未确认事件；回调失败可能重复投递。通用事件、Telegram action 和账号状态使用实时通道，断线期间不补发。普通事件回调的返回值不控制其他插件；拦截需使用显式 filter。

## 微信业务调用

使用 `plugin.wechat(account_id)` 或处理函数中的 `ctx.wechat(account_id)` 获取调用对象：

```python
wx = ctx.wechat(event.account_id)
result = await wx.call(
    "group.get_chat_room_member_detail",
    {"chatRoomId": event.peer},
    idempotency_key=f"members:{event.event_id}",
)
print(result.status, result.call_id)
```

| API | 用途 |
| --- | --- |
| `wx.call(method, params, **options)` | 提交并等待结果 |
| `wx.submit(method, params, **options)` | 异步提交，返回受理回执 |
| `wx.send(to, segments, **options)` | 提交微信消息段 |
| `wx.send_text(to, text, **options)` / `send_image(to, path, **options)` | 文本 / 本地图片快捷发送 |
| `wx.wait_result(receipt, timeout=...)` | 等待原调用最终状态，不重发 |
| `wx.calls.get(call_id)` / `wait(call_id)` | 读取或等待原调用 |
| `wx.calls.find(idempotency_key)` | 按原幂等键查找 |
| `wx.calls.list(...)` / `detail(call_id)` / `queue()` | 查询当前插件和账号的调用记录 |

上述方法均为异步 API，需要 `await`。返回结果支持属性和字典访问，例如 `result.status` 与 `result["status"]`。业务错误检查 `status` / `error`；传输错误捕获 `PluginError`，可使用异常携带的 `idempotency_key` 查询是否已受理。

完整字段、调用状态与保留期见[微信业务指南](../../docs/wechat-business.md)。登录、业务授权和内部消息同步由宿主管理。

## 通用调用账本

Telegram 和其他声明通用执行器的平台，发送持久化入队后立即返回 `queued` / `running`，此时 `delivered=false`，不等待远端发送。使用 `call_id` 查询最终结果，仅 `succeeded` 表示服务确认成功。

```python
key = "notice:business-id:1"
receipt = await plugin.send(
    "123456", "987654", "notice",
    platform="telegram", idempotency_key=key,
)
call = await plugin.calls("123456", platform="telegram").get(receipt["call_id"])
print(call.status, call.call_id)
```

`ctx.calls(account_id)` / `plugin.calls(account_id)` 在 Python 事件回调中默认沿用事件平台，并发回调相互隔离；回调外默认 Telegram。后台任务建议始终显式传 `platform`。这与 `send` 默认微信的约定不同。

支持的宿主会声明 `calls.async_submit.v1`、`calls.part_receipts.v1` 和 `calls.owner_queries.v1`。每次提交最多 50 个编译请求段、1 MiB 序列化消息。幂等冲突返回新的 `rejected` 回执并关联原调用，未知结果不自动重放。

## 回执、重试与保留期

微信文件和视频可使用 `file_segment(base64, file_name)`、`video_segment(base64, cover_base64)`，先通过 `cdn.get_dns` 初始化聊天 CDN；视频时长由服务读取。旧 `video_segment` 的第三个时长参数仍可传入，但不再发送。媒体、Finder 和业务参数迁移见[微信业务指南](../../docs/wechat-business.md)。

- 跨超时、重连和重试保存同一业务动作的幂等键；不同动作使用不同键。
- 调用结果与完成事件可能先后交错，用 `call_id` 关联。超时只停止等待，应查询原回执。
- `unknown` / `partial` 需要核对执行结果，不换键盲目重发。
- 宿主默认保留详情 30 天、精简回执与幂等信息 90 天、微信待补发事件 7 天；通用账本不提供事件补发，实际以宿主配置为准。
- 详情过期标记 `details_expired`；回执过期后原幂等键不再保证去重。

## 连接与过滤参数

`request_timeout` 默认 70 秒，`handshake_timeout` 默认 10 秒（包括订阅确认）；`connect(timeout=15)` 另限制初始化等待。运行期间使用 `close()` 可立即中断重连退避。同一个实例不能同时运行 `run_async()` 和 `connect()`。

`event_filter` 的 `accounts`、`platforms`、`chat_types`、`text_prefix` 未提供表示不限，`[]` 表示不匹配任何对象。例如 `{"platforms": ["wechat"], "accounts": ["wxid_bot"]}`。订阅过滤只缩小接收范围，业务拦截使用 `@plugin.filter`。

通用事件和微信专用事件可能描述同一消息，请按功能选择订阅通道。普通事件处理函数的返回值不会阻止其他插件；`MESSAGE_FILTER` 是目录常量，不能用 `on(MESSAGE_FILTER)` 代替过滤回调。

## 测试与许可证

在仓库根目录执行：

```powershell
python -m unittest discover -s ws/python/tests -v
python -m unittest discover -s tests -p test_wechat_panel.py -v
```

测试使用本地模拟服务。SDK 采用 [MIT License](LICENSE)，完整打包流程见[开发与维护](../../docs/development.md#构建安装包)。
