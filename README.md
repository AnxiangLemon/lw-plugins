# LW Plugins

LW Tools 的 Lua 插件、Python WebSocket（WS）示例和微信业务接口面板。

面向 LW Tools 插件开发者，提供 Lua 插件、`lwplugin` Python SDK、可运行示例、接口面板和测试。你可以仅凭本仓库安装 SDK、编写插件、运行测试与体验模拟面板；连接实际账号时，需要运行兼容的 LW Desktop / Web 宿主。

宿主负责账号、业务授权和消息存储；插件通过 Lua API 或 WebSocket 使用宿主提供的能力，无需接入宿主内部实现。

## 从这里开始

| 你想做什么 | 入口 |
| --- | --- |
| 编写第一个插件 | [插件开发入门](docs/plugin-development.md) |
| 安装 Lua 插件 | [Lua 安装步骤](#安装-lua-插件) |
| 验证 WS 连接和消息回复 | [Python 运行步骤](#运行-python-示例) |
| 在浏览器中调试微信接口 | [微信接口面板](docs/wechat-panel.md) |
| 编写 Python WS 插件 | [lwplugin SDK](ws/python/README.md) |
| 编写微信业务调用 | [微信业务开发](docs/wechat-business.md) |
| 运行测试或更新接口目录 | [开发与调试](docs/development.md) |

## 快速体验

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

### Lua

| 文件 | 用途 |
| --- | --- |
| [demo.lua](lua/demo.lua) | 收到 `!ping` 后回复 `pong` |
| [message_filter.lua](lua/message_filter.lua) | 按消息类型、发送者、会话和关键词过滤消息 |
| [message_forwarder.lua](lua/message_forwarder.lua) | 根据规则在账号、会话之间转发消息 |
| [archive_clone.lua](lua/archive_clone.lua) | 对接 Archive 服务，配置路由和重置会话 |
| [wechat_business.lua](lua/wechat_business.lua) | 查询微信业务能力与群成员，演示媒体、群和朋友圈调用 |
| [lib/L.lua](lua/lib/L.lua) | 消息识别、会话处理与回复辅助库 |

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
| LW 宿主 | 支持微信插件协议 v2；使用 Lua 时提供 Lua 插件运行环境 |
| Python | 3.9 或更新版本 |
| `lwplugin` | 2.1.0，源码位于 [ws/python/lwplugin](ws/python/lwplugin) |
| Python 依赖 | SDK 安装时自动安装；`panel` 可选依赖包含面板所需包 |

Lua 由宿主直接加载，不需要单独安装 Lua。Python 示例运行在独立进程中；模拟面板只需本仓库与 Python 环境。宿主配置示例见 [lwcli.example.toml](examples/lwcli.example.toml)，请按宿主版本调整后使用。

## 安装 Lua 插件

1. 在宿主插件页打开“插件目录”，确认实际加载位置。
2. 复制需要的 `.lua` 文件，并保留工具库的相对路径。例如安装 Demo：

   ```text
   plugins/
   ├── demo.lua
   └── lib/
       └── L.lua
   ```

3. 点击“扫描插件”，按需启用，并在“插件设置”中填写配置。
4. 如需统一过滤消息，将 `message_filter` 排在业务插件之前。

同一个插件 ID 只保留一份。单独导入 `.lua` 不会自动导入 `lib/L.lua`；修改主脚本、元数据或公共库后，在宿主中重新加载相关插件。

### 直接加载源码目录

支持自定义 Lua 目录的宿主，可以在数据目录的 `config/lwcli.toml` 中配置：

```toml
[plugin_lua]
directory = 'C:/path/to/lw-plugins/lua'
```

将路径替换为本仓库的 `lua/` 目录，重启宿主后扫描并启用插件。相对路径以 `lwcli.toml` 所在目录为基准；未配置时使用宿主默认的 `<data-root>/plugins/`。

多个宿主可读取同一份源码，各自保留启用列表、设置和数据。导入、卸载等文件操作会修改共享目录；仅停用某个宿主中的插件时，关闭该宿主的启用开关即可。Web 宿主需使用服务进程可访问的路径，容器部署时先挂载源码目录。

## 运行 Python 示例

以下命令在本仓库根目录执行，使用 PowerShell。建议先创建虚拟环境：

```powershell
python -m venv .venv
.\.venv\Scripts\Activate.ps1
```

从当前公开仓库安装 SDK 和面板依赖：

```powershell
python -m pip install -e './ws/python[panel]'
python ws/python/wechat_panel.py --demo
```

打开 <http://127.0.0.1:8765> 即可体验，无需实际宿主。只运行 Echo 等基础插件时可用 `python -m pip install -e ./ws/python`，省略面板依赖。原来的 `python -m pip install -r ws/python/requirements.txt` 也会安装本仓库 SDK 与面板依赖。

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

# 使用模拟宿主打开接口面板
python ws/python/wechat_panel.py --demo
```

面板地址为 <http://127.0.0.1:8765>，完整操作见[面板指南](docs/wechat-panel.md)。

Echo 的回复前缀可通过 `LWPLUGIN_ECHO_PREFIX` 修改。Assistant 支持 `/ping`、`/slow`、`/history`、`/search`、`/accounts`、`/whoami`；定时播报使用 `LWPLUGIN_BROADCAST_ACCOUNT` 与 `LWPLUGIN_BROADCAST_PEER` 配置。

## 消息与调用约定

- 普通 `plugin.on` 回调的返回值不控制消息传播，回复消息也不会终止其他插件。拦截需使用显式 prefilter；Lua 示例见 `message_filter` 的 `plugin.filter` 与 `plugin.stop()`。
- 微信业务事件由宿主持久化，重连后可能补发；回调失败可能重复投递，有副作用的调用应使用稳定的幂等键。账号状态等实时事件不补发。
- 业务调用以 `status` 判断结果；等待超时后先查询原回执。返回字段、状态和保留期见[业务指南](docs/wechat-business.md)。

## 常见问题

| 现象 | 检查项 |
| --- | --- |
| Lua 加载失败 | 文件目录、Lua 行号、`lib/L.lua`、`require("L")` 和 traceback |
| `No module named 'lwplugin'` | 在当前虚拟环境中执行 `python -m pip install -e ./ws/python` |
| WS 无法接入 | 服务是否开启、连接文件路径、令牌、协议版本、插件接入权限 |
| 收不到消息 | 账号在线状态、事件订阅、过滤条件、是否被 prefilter 拦截 |
| 接口返回失败或等待超时 | 保存 `call_id`、幂等键和 `error`，查询原调用结果 |

Desktop 插件页的“打开日志”可定位宿主日志；Python traceback 在运行终端中。排障时结合 `plugin_id`、`request_id`、`stage` 和 `hint` 查看具体原因。

## 许可证

本仓库采用 [MIT License](LICENSE)。
