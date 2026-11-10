# 群资料刷新事件

[Python SDK](../ws/python/README.md) · [Go SDK](../ws/go/README.md) · [微信群管家](../ws/go/group-manager/README.md)

新版 Rust 宿主在 hello.ok.events 中声明两个 WS 实时事件，协议仍为 v2。插件必须检查宿主支持的事件名，再订阅。账号范围和平台订阅过滤继续生效；消息前缀、聊天过滤插件不会拦住没有正文的刷新提示。

## 群资料提示

```json
{
  "v": 2,
  "type": "event",
  "event": "wechat.group.refresh_hint",
  "event_id": "hint:EXAMPLE",
  "received_at": 1794240000,
  "account": {"platform": "wechat", "id": "wxid_example"},
  "data": {
    "room_id": "123456@chatroom",
    "reason": "mod_contact",
    "chatroom_version": 10,
    "chatroom_info_version": 20,
    "erase_member_data": 0,
    "info_mask": 0
  }
}
```

reason=mod_contact 来自同步群资料；reason=system_message 来自该群的系统消息线索。后者不包含正文，也不判断加入或退出。可选版本/标志字段可能为 null，缺失不意味着 0。两种群资料版本不能互相代替，也不能直接当作详情的 serverVersion。

宿主仅投影白名单字段，不转发完整 ModContact、keyBuf、访问凭据。系统消息即使被其他聊天插件过滤，仍可触发此核对线索。`erase_member_data=1` 和非零 info_mask 都不是“全员退出”。

## 账号级弱提示

```json
{
  "v": 2,
  "type": "event",
  "event": "wechat.sync.hint",
  "event_id": "hint:EXAMPLE2",
  "received_at": 1794240000,
  "account": {"platform": "wechat", "id": "wxid_example"},
  "data": {
    "source": "server_push",
    "selector": 4,
    "triggered_at": "2026-11-10T00:00:00Z",
    "continue_flag": 0
  }
}
```

来自已成功解码的 server_push 同步批次，即使没有聊天消息或群 ID 也可以产生。selector 保留原始有符号 int32，continue_flag 保留原始 uint32，二者含义不同。selector 没有成员 wxid 或群 ID，不能把 4、组合值、-1 直接发布为退群。

## 插件责任

Hint 只表达“可能需要核对”，真正的成员净变化由插件持有的两份有效完整名单计算。首次基线不产生全员加入；失败、部分数据、旧响应不推进基线。没有动作证据时原因记录 unknown。昵称变化不构成进退群。

这些事件没有 durable Cursor，不保证断线补发。宿主的提示队列积压或发送队列满时会断开该提示订阅连接，促使插件重连核对；这不提供服务端历史回放。OnConnect 应重新核对关注群，并标记离线净变化。插件不另连 lwsapi 的单账号 /ws/sync，也不并行抢拉 Msg/Sync。

Go 与 Python 统一提供事件常量、类型化提示对象、宿主能力检查和重连初始化入口。宿主与 SDK 不自动建立群名单数据库，也不自动发送欢迎消息。

群管家数据面板将完整成员详情中的 `inviterUserName` 单独保存为邀请来源。扫码入群有该字段时同样计入，没有字段时标为未知；当前不从系统提示文案猜测邀请人。首次加载只用于当前成员来源统计，不计入“期间新增”。群名与备注通过 `friend.init_contact` 双游标分页和 `friend.batch_get_contact_brief_info` 补齐，列表刷新失败保留旧资料。详见 [群管家说明](../ws/go/group-manager/README.md)。

## Go / Python 能力对照

Python SDK 自 2.3.0 起统一下列群监控基础能力，宿主仍使用 WS 协议 v2：

| 能力 | Go | Python |
| --- | --- | --- |
| 群资料线索 | `GroupRefreshHint` | `GROUP_REFRESH_HINT` |
| 账号同步线索 | `SyncHint` | `SYNC_HINT` |
| 类型化数据 | 解码 `Data` 为 `GroupHint` / `SyncMetadata` | `event.group_hint` / `event.sync_metadata` |
| 宿主信息 | `client.Host()` | `plugin.host` (`HostInfo`) |
| 连接状态 | `client.Connected()` | `plugin.connected` |
| 初始化 / 重连恢复 | `Config.OnConnect` | `plugin.on_connect` |
| 断线通知 | `Config.OnDisconnect` | `plugin.on_disconnect` |
| 账号、平台过滤 | `Config.Filter` | `Plugin(event_filter=...)` |
| 等待已受理调用 | `Wechat.Await` | `wx.wait_result` |

两端均先检查 `hello.ok.events` 再订阅；初始化可调用 RPC，事件在初始化完成后处理。两类 Hint 按顺序处理，失败则断开连接供业务重新核对。Go 的所有事件回调串行；Python 保留普通实时消息的并发策略，只有群提示共用顺序锁、持久化事件保持原有 ACK 顺序。不要依赖 Hint 与聊天消息回调之间的全局顺序。

群提示没有 Cursor，不能调用 `wechat.events.ack`；可补发的持久化事件才在回调成功后确认。两端重连均不会重新提交在途业务。`wait_result` / `Await` 原样返回 `unknown`、`partial` 等状态，不能把返回结果一律当作成功。

## Python 接入示例

下面仅展示提示订阅与连接生命周期，默认只打印日志，不发送微信消息。真正的成员监控应在连接回调和提示回调中接入业务自己的完整名单核对逻辑。

```python
import logging
from lwplugin import Plugin, GROUP_REFRESH_HINT, SYNC_HINT

logging.basicConfig(level=logging.INFO)
plugin = Plugin(
    "my.group.monitor",
    events=[GROUP_REFRESH_HINT, SYNC_HINT],
    event_filter={"platforms": ["wechat"]},
)

@plugin.on_connect()
async def connected(host, ctx):
    logging.info("群提示订阅完成，宿主版本：%s", host.version)
    # 业务插件在此读取本地关注群，并 await 核对其完整成员名单。
    # 不要把初次获取的全部成员记成新入群；离线期间仅记录可确认的净变化。
    # 可以 await ctx.api(...) 或 ctx.wechat(account_id).call(...)。

@plugin.on(GROUP_REFRESH_HINT)
async def group_changed(event, ctx):
    hint = event.group_hint
    logging.info("待核对群资料 account=%s room=%s reason=%s",
                 event.account_id, hint.room_id, hint.reason)
    # 使用 event.account_id + hint.room_id 定位该账号下的群，不跨账号混用名单。
    # 合并密集提示、检查完整返回后再推进名单；失败不能记为全员退群。

@plugin.on(SYNC_HINT)
async def sync_changed(event, ctx):
    logging.info("账号同步线索 account=%s selector=%s",
                 event.account_id, event.sync_metadata.selector)
    # 无群 ID 的弱线索，只能按业务节流策略核对该账号已关注的群。

@plugin.on_disconnect()
async def disconnected(error):
    logging.info("群提示连接已断开：%s", error)

plugin.run()
```

入退群日志默认开启、微信消息提示独立开关、邀请人统计等产品设置继续由[微信群管家](../ws/go/group-manager/README.md)维护，不进入语言 SDK。这样两端共用协议约定，不会因复制群管家的数据库和页面逻辑而出现两套实现。

完整事件目录、通用平台消息和媒体接口对照见 [SDK 协议对照](sdk-parity.md)。
