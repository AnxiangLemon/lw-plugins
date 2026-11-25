# LW Plugins

LW Tools 的 Go、Python 和 Rust WebSocket 插件 SDK、Bot Hub 多平台群管与监控中心及微信接口面板。插件是独立进程，通过 WebSocket 使用宿主提供的账号和业务能力。运行测试和模拟面板只需要本仓库，连接实际账号需要运行中的 LW Tools 宿主。

## 从这里开始

| 你想做什么 | 入口 |
| --- | --- |
| 编写第一个插件 | [插件开发入门](docs/plugin-development.md) |
| 验证 WS 连接和消息回复 | [Python 运行步骤](#运行-python-示例) |
| 在浏览器中调试微信接口 | [微信接口面板](docs/wechat-panel.md) |
| 编写 Python WS 插件 | [lwplugin SDK](ws/python/README.md) |
| 编写 Go WS 插件 | [Go SDK](ws/go/README.md) |
| 编写 Rust WS 插件 | [Rust SDK](ws/rust/README.md) |
| 运行多平台群管与监控转发中心 | [LW Bot Hub](ws/go/bot-hub/README.md) |
| 编写微信业务调用 | [微信业务开发](docs/wechat-business.md) |
| 运行测试或更新接口目录 | [开发与调试](docs/development.md) |

## 快速体验微信接口面板

以下命令使用 PowerShell；先确认已安装 Python 3.9+ 和 Git：

```powershell
git clone https://github.com/AnxiangLemon/lw-plugins.git
cd lw-plugins
python -m venv .venv
.\.venv\Scripts\Activate.ps1
python -m pip install -e './ws/python[panel]'
python ws/python/wechat_panel.py --demo
```

打开 <http://127.0.0.1:8765> 即可查看模拟消息、填写接口参数并检查调用结果，不需要运行实际宿主。按 `Ctrl+C` 停止。Linux / macOS 使用 `source .venv/bin/activate` 激活虚拟环境，其余步骤相同。

## 插件一览

### Go / WebSocket

| 目录 | 用途 |
| --- | --- |
| [ws/go](ws/go/README.md) | Go WebSocket 插件 SDK（支持通用消息、微信业务与 Telegram Bot API） |
| [ws/go/bot-hub](ws/go/bot-hub/README.md) | LW Bot Hub：微信/Telegram 群管、全平台黑名单与跨平台消息监控转发中心 |

### Python / WebSocket

| 文件 | 用途 |
| --- | --- |
| [echo.py](ws/python/echo.py) | 收到文本后回复，验证 WS 事件链路 |
| [assistant.py](ws/python/assistant.py) | 命令路由、历史查询、账号状态和后台任务 |
| [wechat_business.py](ws/python/wechat_business.py) | 查询微信业务能力，演示统一业务调用 |
| [wechat_panel.py](ws/python/wechat_panel.py) | 本地 Web 面板：消息、接口表单、JSON 结果和模拟模式 |

## 运行条件

| 组件 | 要求 |
| --- | --- |
| LW 宿主 | LW Tools，支持 WS 插件协议 v2 |
| Python | 3.9 或更新版本 |
| `lwplugin` | 2.4.1，源码位于 [ws/python/lwplugin](ws/python/lwplugin) |
| Python 依赖 | SDK 安装时自动安装；`panel` 可选依赖包含面板所需包 |

插件运行在独立进程中；模拟面板只需本仓库与 Python 环境。

## 运行 Python 示例

在本仓库根目录执行。已按[快速体验](#快速体验微信接口面板)安装 SDK 时，可以直接[连接宿主](#连接宿主)。只运行 Echo 等示例时，安装基础 SDK 即可。

```powershell
python -m venv .venv
.\.venv\Scripts\Activate.ps1
python -m pip install -e ./ws/python
```

SDK API、事件与多平台调用见 [Python SDK 文档](ws/python/README.md)。

### 连接宿主

1. 在宿主“插件 → WS 插件”中开启服务。
2. 找到宿主生成的 `config/plugin-endpoint.json`。
3. 指定该文件，启动一个示例，并在宿主中允许对应插件接入：

   ```powershell
   $env:LW_PLUGIN_ENDPOINT_FILE = 'C:\path\to\host-data\config\plugin-endpoint.json'
   python ws/python/echo.py
   ```

允许接入仅登记连接权限，Python 进程需要自行启动。停止进程后，宿主会显示插件离线。SDK 不会自动搜索相邻宿主仓库的数据目录，因此建议显式设置连接文件。

### 其他入口

```powershell
# 命令和历史查询示例
python ws/python/assistant.py

# 微信业务能力查询示例
python ws/python/wechat_business.py

# 连接实际宿主打开接口面板，需要安装 panel 可选依赖
python ws/python/wechat_panel.py
```

面板地址为 <http://127.0.0.1:8765>，完整操作见[面板指南](docs/wechat-panel.md)。

Echo 的回复前缀可通过 `LWPLUGIN_ECHO_PREFIX` 修改。Assistant 支持 `/ping`、`/slow`、`/history`、`/search`、`/accounts`、`/whoami`；定时播报使用 `LWPLUGIN_BROADCAST_ACCOUNT` 与 `LWPLUGIN_BROADCAST_PEER` 配置。

## 消息与调用约定

- 普通 `plugin.on` 回调的返回值不控制消息传播，回复消息也不会终止其他插件。拦截需使用显式 prefilter（Python 为 `@plugin.filter` 与 `plugin.stop()`）。
- 微信业务事件由宿主持久化，重连后可能补发；回调失败可能重复投递，有副作用的调用应使用稳定的幂等键。账号状态等实时事件不补发。
- 业务调用以 `status` 判断结果；等待超时后先查询原回执。返回字段、状态和保留期见[业务指南](docs/wechat-business.md)。

## 常见问题

| 现象 | 检查项 |
| --- | --- |
| `No module named 'lwplugin'` | 在当前虚拟环境中执行 `python -m pip install -e ./ws/python` |
| WS 无法接入 | 服务是否开启、连接文件路径、令牌、协议版本、插件接入权限 |
| 收不到消息 | 账号在线状态、事件订阅、过滤条件、是否被 prefilter 拦截 |
| 接口返回失败或等待超时 | 保存 `call_id`、幂等键和 `error`，查询原调用结果 |

宿主日志在其数据目录的 `logs/` 下；Python traceback 在运行终端中。排障时结合 `plugin_id`、`request_id`、`stage` 和 `hint` 查看具体原因。

## 许可证

本仓库采用 [MIT License](LICENSE)。Go / Python SDK 的接口、事件、回执语义与源码发布方式见 [Go / Python SDK 协议对照](docs/sdk-parity.md)。
