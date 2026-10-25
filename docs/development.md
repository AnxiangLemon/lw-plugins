# 开发与调试

[返回 README](../README.md) · [插件开发入门](plugin-development.md) · [Python SDK](../ws/python/README.md) · [接口面板](wechat-panel.md)

本页说明如何修改插件、测试 SDK、调试调用以及生成安装包。安装、构建和本地模拟测试均可直接使用本仓库完成。

## 目录结构

```text
lw-plugins/
├── docs/                  # 面向插件开发者的文档
├── examples/              # 宿主配置示例
├── lua/                   # 宿主直接加载的 Lua 插件
│   └── lib/L.lua          # 共享辅助库
├── ws/python/
│   ├── pyproject.toml     # lwplugin 安装配置与 panel 可选依赖
│   ├── lwplugin/          # Python WS SDK
│   ├── tests/             # SDK 单元测试与协议测试
│   ├── wechat_panel/      # 面板静态资源与公开接口目录
│   └── *.py              # 可运行插件示例
├── tests/                 # 面板、目录生成器测试与模拟流程 fixture
└── tools/                 # 接口目录生成工具
```

## 安装与本地开发

在仓库根目录执行：

```powershell
python -m venv .venv
.\.venv\Scripts\Activate.ps1
python -m pip install -e './ws/python[panel]'
```

SDK 当前版本为 2.1.0，要求 Python 3.9+、宿主 WS 协议 v2。可编辑安装后，修改 `ws/python/lwplugin/` 立即生效；更新 SDK 后需重启正在运行的插件进程。

只安装基础 SDK 可使用 `python -m pip install -e ./ws/python`。兼容入口 `python -m pip install -r ws/python/requirements.txt` 必须在仓库根目录执行。

## 运行测试

```powershell
python -m unittest discover -s ws/python/tests -v
python -m unittest discover -s tests -p 'test_*.py' -v
```

SDK 测试覆盖业务调用、事件确认、错误日志和多平台 WS 行为。面板测试使用本地模拟宿主，覆盖全部 85 个接口入口、账号选择、参数校验、实时消息、JSON 精度、异步回执、业务失败和跨来源请求；目录生成器测试验证公开方法范围与字段转换。这些测试不需要实际账号或运行中的宿主。

## 调试插件

1. 先运行 `python ws/python/wechat_panel.py --demo`，确认 Python 依赖与面板可正常启动。
2. 连接实际宿主时，显式指定其 `plugin-endpoint.json`，并在宿主中允许插件 ID 接入。
3. 查看插件终端中的连接、订阅日志和 traceback；宿主日志可按插件 ID、请求 ID 或 `call_id` 检索。
4. 收不到消息时，检查在线账号、事件名称、过滤条件和宿主 prefilter。
5. 业务调用失败时检查 `status` / `error`。等待超时后查询原回执，保留幂等键。

模拟模式用于检验协议和调用流程，实际账号的可用方法、权限与业务结果由宿主和所连接服务决定。面板启动方式见[面板指南](wechat-panel.md)，事件和调用语义见 [Python SDK](../ws/python/README.md)。

## 构建安装包

```powershell
python -m pip wheel ./ws/python --no-deps --wheel-dir dist
```

生成的 wheel 包含 `lwplugin` 与 MIT 许可证，可在其他 Python 环境中安装。插件示例和面板静态资源在源码仓库中维护，运行面板请使用完整仓库。本项目尚未向 PyPI 发布，请从本仓库或自行构建的 wheel 安装。

## 更新面板接口目录

日常运行使用内置 [catalog.json](../ws/python/wechat_panel/catalog.json)，无需生成。需要更新参数 schema 时，使用服务提供的 Swagger JSON：

```powershell
python tools/build_wechat_panel_catalog.py --docs-file 'C:\path\to\swagger.json'
```

也可用 `--docs-url 'http://localhost:8081/docs/swagger.json'` 从可访问的服务读取文档，或用 `--output 'C:\path\to\catalog.json'` 先生成单独文件供核对。

生成器默认沿用公开目录中的 `name` / `path` 方法清单，只更新这些方法的参数和返回说明，不会把文档中的所有服务接口加入插件。可通过 `--methods-file` 指定其他包含 `methods` 数组的 JSON；新增方法前应确认宿主能力列表支持它。工具只读取文档，不调用业务接口，也不需要宿主源码。

## 贡献约定

- 使用中文提交说明，描述具体变更及目的。
- Python SDK 代码、独立示例和对应测试集中在本仓库维护。
- 新增插件时提供用途、运行条件、设置说明和最小验证步骤。
- 调整行为或 API 时同步文档，并执行受影响的测试。
- 文档使用一个一级标题、逐级小节、带语言的代码块及仓库内相对链接。
- 示例路径使用占位符，不提交账号凭据、endpoint 文件、日志、数据库、本地备份或构建产物。
