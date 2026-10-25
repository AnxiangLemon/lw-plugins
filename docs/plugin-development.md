# 插件开发入门

[返回 README](../README.md) · [Python SDK](../ws/python/README.md) · [微信业务调用](wechat-business.md) · [开发与调试](development.md)

插件运行在 LW Desktop / Web 提供的能力之上。宿主管理账号、登录授权、连接权限和消息存储；插件接收事件、处理自己的业务逻辑，并通过宿主 API 回复或调用业务方法。

## 选择开发方式

| 方式 | 适合场景 | 运行方式 |
| --- | --- | --- |
| Lua | 简单命令、过滤、转发和轻量业务逻辑 | 将脚本安装到宿主插件目录，由宿主加载 |
| Python WS | 异步处理、第三方 Python 库、独立服务或本地 Web 界面 | 安装 `lwplugin`，独立进程通过 WS 连接宿主 |

两种方式都由宿主决定当前账号的可用能力。Python SDK 与完整示例已包含在本仓库，开发插件无需宿主内部服务框架源码。

## Python：第一个回复插件

### 1. 安装 SDK

在仓库根目录执行：

```powershell
python -m pip install -e ./ws/python
```

### 2. 编写插件

将下列内容保存为自己的 `my_plugin.py`。插件 ID 应稳定且唯一，宿主通过它识别权限和调用记录：

```python
import logging

from lwplugin import Plugin

logging.basicConfig(level=logging.INFO)
plugin = Plugin("example.my-ping", title="我的 Ping 插件", version="1.0.0")


@plugin.on_message()
async def receive(event, ctx):
    if event.message.get("direction") == "outbound":
        return
    if event.text.strip() == "/ping":
        receipt = await ctx.reply(event, "pong")
        print(receipt)


if __name__ == "__main__":
    plugin.run()
```

### 3. 连接并验证

在宿主开启 WS 服务，找到它生成的连接文件，并运行插件：

```powershell
$env:LW_PLUGIN_ENDPOINT_FILE = 'C:\path\to\host-data\config\plugin-endpoint.json'
python my_plugin.py
```

在宿主中允许 `example.my-ping` 接入。使用另一个账号向宿主已连接账号发送 `/ping`，应收到 `pong`；终端应显示连接与订阅日志。停止 Python 进程后插件离线，允许接入开关不会代替启动进程。

已有可运行示例：[echo.py](../ws/python/echo.py)、[assistant.py](../ws/python/assistant.py)、[wechat_business.py](../ws/python/wechat_business.py)。

## Lua：安装并修改 Demo

按 [Lua 安装步骤](../README.md#安装-lua-插件)放置 `demo.lua` 和 `lib/L.lua`，扫描并启用后，使用另一个账号发送 `!ping`，应收到 `pong`。

开发自己的插件时，可复制 [demo.lua](../lua/demo.lua)，修改 `PLUGIN_ID`、`PLUGIN_TITLE`、`PLUGIN_VERSION` 和业务处理函数。保留稳定的插件 ID，避免同时加载两个相同 ID 的脚本。修改后在宿主重新加载；共享库更新时也要重新加载引用它的插件。

Lua 使用 `plugin.on("wechat.message.received", handler)` 订阅微信事件，`plugin.wechat(account_id)` 获取业务调用对象。示例通过 `L.reply` 回复，业务 API 见 [wechat_business.lua](../lua/wechat_business.lua) 和[业务指南](wechat-business.md)。

## 事件与发送平台

Python 事件处理函数可以接收 `event`，或 `event, ctx` 两个参数；推荐使用 `async def`，耗时 I/O 应使用异步库，避免在回调中执行阻塞操作。

| 常用字段 | 含义 |
| --- | --- |
| `event.event_id` | 事件唯一标识，可用于构造稳定业务幂等键 |
| `event.platform` / `event.account_id` | 来源平台和当前宿主账号 |
| `event.peer` / `event.sender_id` | 会话对端和发送者 |
| `event.text` / `event.segments` | 消息文本和结构化消息段 |
| `event.raw` | 原始事件 JSON，适合检查附加字段 |

`@plugin.on_message()` 订阅通用消息事件。需要微信持久化投递时，显式订阅 `wechat.message.received`；不要同时订阅两个入口后重复处理同一业务动作。

回复当前会话使用 `ctx.reply(event, ...)`。主动发送时显式指定 `platform=event.platform`；`ctx.send` 的默认平台是微信，不自动跟随来源事件。更多事件、消息段及调用选项见 [SDK 文档](../ws/python/README.md)。

## 过滤与可靠调用

普通回调的 `return False` 或发送消息不会阻止其他插件。确需拦截微信入站消息时使用显式 filter，并保持处理简短：

```python
@plugin.filter
async def drop_blocked(event):
    if event.text.strip() == "/blocked":
        return plugin.stop("blocked command")
```

微信持久化事件在回调失败或重连后可能重复投递；有副作用的动作应使用稳定幂等键。普通实时事件不保证断线补发。调用返回 `queued`、`running` 或等待超时时，继续查询原回执；只有 `succeeded` 表示服务确认成功。详见[调用状态与保留期](wechat-business.md#理解调用结果)。

## 不连接账号的开发方式

安装 `panel` 可选依赖后，运行 `python ws/python/wechat_panel.py --demo`，即可通过模拟账号检查字段、调用片段和 JSON 结果。SDK 与面板的自动化测试也只使用本地模拟服务，运行命令见[开发与调试](development.md#运行测试)。
