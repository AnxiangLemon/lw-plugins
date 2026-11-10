# 群资料刷新事件

[Go SDK](../ws/go/README.md) · [微信群管家](../ws/go/group-manager/README.md)

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

Go 可使用 GroupRefreshHint、SyncHint 及 GroupHint、SyncMetadata DTO；Python 可订阅同名常量，通过 event.data 读取提示对象。当前新增的是 WS 提示事件，宿主不自动建立群名单数据库，不自动发送欢迎消息。

群管家数据面板将完整成员详情中的 `inviterUserName` 单独保存为邀请来源。扫码入群有该字段时同样计入，没有字段时标为未知；当前不从系统提示文案猜测邀请人。首次加载只用于当前成员来源统计，不计入“期间新增”。群名与备注通过 `friend.init_contact` 双游标分页和 `friend.batch_get_contact_brief_info` 补齐，列表刷新失败保留旧资料。详见 [群管家说明](../ws/go/group-manager/README.md)。
