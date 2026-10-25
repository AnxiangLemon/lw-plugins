# 开发与维护

[返回 README](../README.md) · [微信业务开发](wechat-business.md) · [接口面板](wechat-panel.md)

本页区分公开仓库中的插件代码与依赖私有源码的维护操作。普通使用者按 README 安装宿主及 SDK 即可，无需获取私有仓库源码。

## 目录结构

```text
lw-plugins/
├── docs/                  # 使用和维护文档
├── lua/                   # 宿主直接加载的 Lua 插件
│   └── lib/L.lua          # 共享辅助库
├── ws/python/             # 独立运行的 Python WS 插件
│   └── wechat_panel/      # 面板静态资源与内置接口目录
├── tests/                 # 面板测试与集成测试入口
└── tools/                 # 接口目录生成工具
```

## 从 SDK 源码安装

仅对已有私有 SDK 仓库访问权限的开发者适用。取得源码后，可在本仓库根目录执行以下命令，路径替换为实际 SDK 目录：

```powershell
python -m pip install -e 'C:\path\to\lw-sdk\sdk\python'
python -m pip install -r ws/python/requirements.txt
```

本仓库不分发 SDK wheel，也不提供宿主构建源码。示例需要 `lwplugin` 2.0.0 或经宿主确认兼容的版本。

## 测试

### 面板测试

安装 SDK 与 Python 依赖后即可运行；测试使用本地模拟 WS 宿主，不需要 Rust 宿主源码或实际微信账号：

```powershell
python -m unittest discover -s tests -p test_wechat_panel.py
```

覆盖全部 85 个接口入口、显式账号选择、参数校验、实时消息、JSON 精度、异步回执、业务失败和跨来源请求。

### 完整模拟集成流程

需要私有 SDK 源码、其中的 `plugin_business` 集成测试，以及该源码版本要求的 Rust 构建环境：

```powershell
python tests/run_mock_flow.py --sdk 'C:\path\to\lw-sdk'
```

该入口通过真实 Lua 沙箱、Rust WS 宿主、Python SDK 和面板 HTTP 连接本地模拟业务服务，覆盖业务调用、幂等、等待超时、部分失败与消息存储故障，不调用实际微信账号。未指定 `--sdk` 时，默认查找与本仓库同级的 `lw-sdk`。

## 更新接口目录

内置目录为 [catalog.json](../ws/python/wechat_panel/catalog.json)。生成器从 SDK 的宿主白名单读取开放范围，从服务 Swagger 文档读取字段与 schema，因此需要额外取得这两份维护输入。

从本地服务文档生成：

```powershell
python tools/build_wechat_panel_catalog.py --sdk 'C:\path\to\lw-sdk' --docs-file 'C:\path\to\swagger.json'
```

也可从本地服务文档地址生成：

```powershell
python tools/build_wechat_panel_catalog.py --sdk 'C:\path\to\lw-sdk' --docs-url 'http://localhost:8081/docs/swagger.json'
```

生成器仅读取文档，不调用业务接口。新增业务方法时，同步宿主白名单、目录和测试；发布前核对生成的 `source` 与示例字段，避免写入机器专属路径或实际凭据。

## 提交约定

- 使用中文提交说明，描述具体变更及目的。
- 更新插件行为时，同步对应使用文档与必要的验证结果。
- 文档使用一个一级标题、逐级小节、带语言的代码块及可访问的仓库内链接。
- 示例路径使用占位符；不要将私有文档链接作为公开使用说明的必要入口。
- 日志、endpoint 文件、令牌、数据库及本地迁移备份不提交到公开仓库。
