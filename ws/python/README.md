# lwplugin Python SDK

[返回仓库首页](../../README.md) · [微信业务调用](../../docs/wechat-business.md) · [接口面板](../../docs/wechat-panel.md) · [开发与测试](../../docs/development.md)

`lwplugin` 是 LW Tools 的异步 WS 插件客户端 SDK，当前版本 **2.1.0**，要求 Python 3.9+ 和宿主协议 v2。源码、安装配置、测试和可运行示例均在本仓库维护。开发插件只需安装本 SDK，并连接支持相应能力的宿主。

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

SDK 默认只从当前工作目录及父目录寻找连接信息，不搜索相邻宿主仓库。远程连接可设置 `LW_PLUGIN_URL` 和 `LW_PLUGIN_TOKEN`；程序也可给 `Plugin` 传入 `endpoint_file` 或 `url` / `token`。宿主接入开关不会自动启动 Python 进程。

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

- 跨超时、重连和重试保存同一业务动作的幂等键；不同动作使用不同键。
- 调用结果与完成事件可能先后交错，用 `call_id` 关联。超时只停止等待，应查询原回执。
- `unknown` / `partial` 需要核对执行结果，不换键盲目重发。
- 宿主默认保留详情 30 天、精简回执与幂等信息 90 天、微信待补发事件 7 天；通用账本不提供事件补发，实际以宿主配置为准。
- 详情过期标记 `details_expired`；回执过期后原幂等键不再保证去重。

## 测试与许可证

在仓库根目录执行：

```powershell
python -m unittest discover -s ws/python/tests -v
python -m unittest discover -s tests -p test_wechat_panel.py -v
```

测试使用本地模拟服务。SDK 采用 [MIT License](LICENSE)，完整打包流程见[开发与维护](../../docs/development.md#构建安装包)。
