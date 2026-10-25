# 微信业务插件开发

[返回 README](../README.md) · [接口面板](wechat-panel.md) · [开发与维护](development.md)

本指南介绍 Lua 与 Python WS 插件共用的微信业务调用方式。运行前请按 [README](../README.md) 安装兼容微信插件协议 v2 的宿主和 `lwplugin` SDK。

## 接口与参数

本仓库的[接口目录](../ws/python/wechat_panel/catalog.json)包含 85 个业务方法，可在面板中查看参数、嵌套 schema 与调用代码。实际可用方法以当前宿主返回的能力列表为准。

- 参数使用服务原字段名，例如 `toWxid`、`chatRoomId`、`base64`、`voiceTime`，不转换旧参数别名。
- 登录、授权和内部消息同步由宿主管理。插件订阅 `wechat.message.received` 接收消息，无需调用 `/Msg/Sync`。
- 媒体输入需符合服务要求的编码和格式。示例中的 `AA==` 仅用于模拟，宿主不负责自动转码。

## Lua：接收事件并调用接口

以下代码可放在 Lua 插件脚本中。收到群内 `!wx-members` 后查询群成员，并将调用结果写入日志：

```lua
PLUGIN_ID = "example.members"
PLUGIN_TITLE = "群成员查询示例"
PLUGIN_VERSION = "1.0.0"

local plugin = require("lw.plugin")
local json = require("json")
local log = require("log")

plugin.on("wechat.message.received", function(event)
    local message = event.message
    if message.text ~= "!wx-members" or not message.is_group then
        return
    end

    local wx = plugin.wechat(event.account_id)
    local result = wx:call(
        "group.get_chat_room_member_detail",
        { chatRoomId = message.peer_id },
        { idempotency_key = "members:" .. event.event_id }
    )
    log.info(json.encode(result))
end)
```

完整示例见 [wechat_business.lua](../lua/wechat_business.lua)，回复辅助函数见 [L.lua](../lua/lib/L.lua)。普通事件回调的返回值不终止其他插件；统一过滤应使用显式 prefilter。

## Python：默认等待与异步提交

在 SDK 事件处理函数中，通过 `ctx.wechat(account_id)` 获取账号调用对象。以下代码中的 `ctx`、`account_id`、`group_id` 和 `event_id` 来自当前业务处理上下文：

```python
wx = ctx.wechat(account_id)
result = await wx.call(
    "group.set_name",
    {"chatRoomId": group_id, "content": "新群名"},
    idempotency_key=f"rename:{event_id}",
)
print(result.status)
```

`call` 默认等待执行结果；`submit` 返回受理回执，再通过 `calls.wait` 等待。以下代码中的 `file_no` 和 `aes_key` 应取自同一条媒体消息或上传结果：

```python
accepted = await wx.submit(
    "cdn.download_image",
    {"fileNo": file_no, "fileAesKey": aes_key},
    idempotency_key=f"download:{event_id}",
)
result = await wx.calls.wait(accepted.call_id)
```

可运行的插件入口及媒体、群公告、朋友圈、公众号示例见 [wechat_business.py](../ws/python/wechat_business.py)。

## 理解调用结果

结果包含 `call_id`、`status`、`data`、`raw`、`messages` 和 `error`。所有业务调用记录日志，发送操作共用宿主的 outbox 和消息库。

| 状态或字段 | 含义与处理 |
| --- | --- |
| `queued` / `running` | 已排队或执行中，可继续查询原回执 |
| `succeeded` | 服务确认成功，不代表接收方已阅读 |
| `failed` / `rejected` | 调用失败或被拒绝，检查 `error` |
| `partial` | 消息段部分成功；保留成功记录，后续段可能取消，需核对明细 |
| `unknown` | 执行结果不确定，先核对原调用，不自动重发 |
| `wait_timed_out` | 仅停止本次等待，不表示原调用未执行或已取消 |

事件可能重复投递。同一个业务动作重试时保持幂等键稳定；不同动作使用不同键。超时后使用 `get`、`wait` 或 `find` 查询原调用，避免重复执行。

## 查询当前插件的调用记录

`calls.list`、`calls.detail`、`calls.queue` 只读取当前插件在指定账号下的记录。`status="needs_review"` 可筛选 `unknown` 及含未知消息段的 `partial`；列表通过 `next_before_id` 翻页。

### Lua

在已获取 `wx = plugin.wechat(account_id)` 的上下文中：

```lua
local page = wx.calls:list({ status = "needs_review", limit = 20 })
for _, row in ipairs(page.items) do
    local detail = wx.calls:detail(row.call_id)
    require("log").info(require("json").encode(detail.result))
end
```

### Python

在已获取 `wx = ctx.wechat(account_id)` 的异步处理函数中：

```python
page = await wx.calls.list(status="needs_review", limit=20)
for row in page["items"]:
    detail = await wx.calls.detail(row.call_id)
    print(detail.result)
```

## 保留期与人工核对

以下为宿主默认值，实际以宿主配置为准；保留期设为 `0` 表示永久保留。

| 记录 | 默认保留期 | 到期行为 |
| --- | --- | --- |
| 业务调用详情 | 30 天 | `data` / `raw` 为 `null`，回执标记 `details_expired` |
| 待补发事件 | 7 天 | 超出补发窗口的事件不再补发 |
| 精简回执与幂等信息 | 调用完成或结案起 90 天 | 原回执不可查询，原幂等键不再保证去重 |

回执到期后，`get`、`wait`、`detail` 无法找到记录，`find` 返回 `null`。查不到记录不等于从未执行；回执过期后重用同一个键可能再次执行业务动作。

宿主“插件 → 调用记录”负责保留策略、取消排队和人工核对。人工结论通过 `wechat.call.updated` 中的 `result.resolution` 提供，原始 `status` 保持不变。插件不负责宿主管理授权或登录恢复。

## 验证示例

SDK、面板和目录生成器均可使用本仓库的本地模拟测试验证，无需实际账号。命令和覆盖范围见[开发与调试](development.md#运行测试)。
