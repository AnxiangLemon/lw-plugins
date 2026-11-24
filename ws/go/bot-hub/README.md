# LW Bot Hub（Go）

单进程、单条 WebSocket 连接的多平台群管理与消息监控转发中心（支持微信与 Telegram），内嵌本地 Web 管理面板（零前端运行依赖，单二进制即可运行）。

[仓库首页](../../README.md) · [Go SDK](../README.md) · [程序入口](main.go)

## 核心能力

1. **微信群管**
   - 同步微信通讯录普通群（`@chatroom`），按账号切换管理与手动添加监控群
   - 监听 `wechat.group.refresh_hint` / `wechat.sync.hint` 与可选定时兜底核对，严格校验完整成员名单后计算入群（含真实邀请人）与离群差集
   - 支持入群欢迎与离群提示模板（`{name}`、`{wxid}`、`{inviter}`、`{group}`、`{count}`），并持久化变动记录
   - 支持查看群成员与邀请来源、本地缓存群成员头像、修改群公告与群备注（仅自己可见）、移出成员
2. **Telegram 群管**
   - 进群人机验证（新成员先禁言 → 发送算术内联按钮 → 答错自动换题并限次 → 点击通过解禁 / 失败或超时移出或保持禁言）
   - 入群欢迎与退群提示（自定义模板 `{name}`、`{group}`，支持定时自动撤回防刷屏）
   - 自动通过进群申请（`telegram.chat_join_request`）
   - 违禁词与外部链接拦截（含 Telegram 隐藏超链接 `url` / `text_link`）+ 自动删消息 + 警告累计禁言或移出（警告次数需管理员手动清零）
   - 群内管理员命令（`/panel`、`/settings`、`/mute`、`/unmute`、`/ban`、`/warn`、`/unwarn`）、内联开关面板（与 Web 面板实时同步）及机器人群权限检测
3. **全平台公共黑名单**
   - 微信与 Telegram 同平台内跨群、跨账号通用：一处拉黑、处处受阻
   - 命中黑名单成员再次入群或提交入群申请时静默拒绝/静默移出，不向群内发提示，仅记录审计日志
   - 支持从 Web 面板手动拉黑（可选立即扫描已管理群移出）与移出黑名单（Telegram 自动对已记录封禁群解除封禁）
4. **跨平台消息监控与防刷屏转发**
   - 同时监听微信与 Telegram 群消息，支持 **微信↔微信**、**TG↔TG**、**微信↔TG** 同平台与跨平台自由转发
   - 支持按被 `@` 目标、可复用「人员分组」、关键词、`@所有人` 触发，或开启「全部消息」转发
   - 支持忽略发送者、忽略机器人、忽略斜杠命令、排除关键词（含 `^前缀` 开头匹配）及消息类型筛选（文字/图片/视频/语音/文件/链接/其他）
   - 支持模板通知与「纯原样转发」两种模式，支持原媒体转发：微信同平台图片/视频/文件走 CDN XML 直转（零上传、不受大小限制），跨平台转发按规则限制大小（默认 10 MB，最高 32 MB）；发视频到微信时若本机安装了 `ffmpeg`，自动抽取视频首帧作为封面
   - 内置冷却节流防刷屏，支持按「群+目标（`target`）」「整群（`chat`）」「规则全局（`rule`）」三种冷却粒度

## 构建与运行

在 `ws/go/bot-hub/` 目录下执行（相邻 Go SDK 通过 `go.mod` 本地 `replace` 引用）：

```powershell
cd ws/go/bot-hub
go build -o dist/bot-hub.exe .
$env:LW_PLUGIN_ENDPOINT_FILE = 'C:\path\to\host-data\config\plugin-endpoint.json'
./dist/bot-hub.exe
```

Linux：

```sh
cd ws/go/bot-hub
go build -o dist/bot-hub .
./dist/bot-hub -endpoint-file /path/to/host-data/config/plugin-endpoint.json
```

远程连接 LW Tools 宿主（无需共享文件系统）：

```powershell
./dist/bot-hub.exe -url "wss://your-host:26810" -token "your-ws-token" -listen "127.0.0.1:8768"
```

启动后在 LW Tools「插件 → WS 插件」中允许 `lw.bot-hub` 接入，浏览器打开 <http://127.0.0.1:8768> 即可可视化配置。

## 启动参数

| 参数 | 默认值 | 说明 |
| --- | --- | --- |
| `-endpoint-file` | 自动发现 | 宿主 `plugin-endpoint.json` 路径，也可通过环境变量 `LW_PLUGIN_ENDPOINT_FILE` 指定 |
| `-url` | 空 | 远程宿主 WS 地址（如 `wss://host:26810`），也可通过 `LW_PLUGIN_URL` 指定 |
| `-token` | 空 | 远程宿主 WS 令牌，也可通过 `LW_PLUGIN_TOKEN` 指定 |
| `-listen` | `127.0.0.1:8768` | 本地 Web 管理面板监听地址 |
| `-data` | `data/bot-hub` | 配置、缓存与审计日志存储目录 |

## 数据目录

运行时数据均保存在 `-data` 目录（默认 `data/bot-hub/`），不修改宿主数据库，也不提交到 Git：

```text
data/bot-hub/
  config.json       # 人员分组、监控转发规则、微信群与 TG 群策略
  blacklist.json    # 全平台公共黑名单
  events.jsonl      # 实时审计日志
  profiles.json     # 宿主账号真实昵称与头像缓存
  tg_cache.json     # TG 群聊权限、当前受限与警告状态缓存
  wx_cache.json     # 微信群成员快照、元信息与已发现群目录缓存
  wx_changes.jsonl  # 微信群成员入退群变动历史
  cache/img/        # 微信群成员头像本地缓存
```

## 测试与检查

```powershell
cd ws/go/bot-hub
go test ./...
go vet ./...
```
